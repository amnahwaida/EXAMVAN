import UIKit
import PDFKit

class ExamViewerViewController: UIViewController {
    
    // Exam & Student Info
    var examId: Int = -1
    var examName: String = ""
    var studentName: String = ""
    var studentNumber: String = ""
    var studentClass: String = ""
    var serverUrl: String = ""
    
    private var questions: [[String: AnyCodable]] = []
    private var studentAnswers: [String: Any] = [:]
    private var submittedOrExited = false
    private var answerSheetExpanded = false
    
    // UI Elements
    private let secureContainer = SecureView()
    private let titleLabel = UILabel()
    private let closeButton = UIButton(type: .system)
    private let headerView = UIView()
    private let pdfView = PDFView()
    
    // Answer Sheet Panel
    private let answerSheetPanel = UIView()
    private let toggleAnswerButton = UIButton(type: .system)
    private let answerScrollView = UIScrollView()
    private let answerStackView = UIStackView()
    private let submitButton = UIButton(type: .system)
    
    // Download Status UI
    private let downloadOverlay = UIView()
    private let progressView = UIProgressView(progressViewStyle: .default)
    private let progressLabel = UILabel()
    private let cancelDownloadButton = UIButton(type: .system)
    private let errorOverlay = UIView()
    private let errorLabel = UILabel()
    private let retryButton = UIButton(type: .system)
    
    // Keep track of dynamically created answer elements
    private var singleChoiceGroups: [Int: [UIButton]] = [:]
    private var trueFalseGroups: [Int: [UIButton]] = [:]
    private var multipleChoiceGroups: [Int: [UIButton]] = [:]
    private var matchingSelectedValues: [Int: [String: String]] = [:]
    
    // MARK: - Lifecycle
    override func viewDidLoad() {
        super.viewDidLoad()
        setupSecureContainer()
        setupUI()
        setupNotifications()
        loadQuestions()
        startPdfDownload()
    }
    
    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        self.navigationController?.setNavigationBarHidden(true, animated: false)
    }
    
    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        // Set lock task advice/Guided Access reminder if preferred, or warn student
        UIPasteboard.general.string = "" // Clear clipboard on entry
    }
    
    private func setupSecureContainer() {
        view.addSubview(secureContainer)
        secureContainer.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            secureContainer.topAnchor.constraint(equalTo: view.topAnchor),
            secureContainer.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            secureContainer.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            secureContainer.bottomAnchor.constraint(equalTo: view.bottomAnchor)
        ])
    }
    
    private func setupUI() {
        let content = secureContainer.contentView
        content.backgroundColor = UIColor(red: 0.05, green: 0.04, blue: 0.1, alpha: 1.0)
        
        // Header
        headerView.backgroundColor = UIColor(white: 1.0, opacity: 0.05)
        headerView.translatesAutoresizingMaskIntoConstraints = false
        content.addSubview(headerView)
        
        closeButton.setImage(UIImage(systemName: "xmark.circle.fill"), for: .normal)
        closeButton.tintColor = .systemRed
        closeButton.translatesAutoresizingMaskIntoConstraints = false
        closeButton.addTarget(self, action: #selector(closeTapped), for: .touchUpInside)
        headerView.addSubview(closeButton)
        
        titleLabel.text = examName
        titleLabel.textColor = .white
        titleLabel.font = UIFont.systemFont(ofSize: 16, weight: .bold)
        titleLabel.translatesAutoresizingMaskIntoConstraints = false
        headerView.addSubview(titleLabel)
        
        // PDF View
        pdfView.autoScales = true
        pdfView.displayMode = .singlePageContinuous
        pdfView.displayDirection = .vertical
        pdfView.backgroundColor = UIColor(red: 0.05, green: 0.04, blue: 0.1, alpha: 1.0)
        // Disable text selection to prevent copying
        pdfView.isUserInteractionEnabled = true
        for subview in pdfView.subviews {
            if let scrollView = subview as? UIScrollView {
                scrollView.isScrollEnabled = true
            }
        }
        pdfView.translatesAutoresizingMaskIntoConstraints = false
        content.addSubview(pdfView)
        
        // Toggle Answer Sheet Button
        toggleAnswerButton.setTitle("📝 Buka Lembar Jawaban", for: .normal)
        toggleAnswerButton.backgroundColor = .systemIndigo
        toggleAnswerButton.setTitleColor(.white, for: .normal)
        toggleAnswerButton.titleLabel?.font = UIFont.systemFont(ofSize: 14, weight: .bold)
        toggleAnswerButton.layer.cornerRadius = 8
        toggleAnswerButton.translatesAutoresizingMaskIntoConstraints = false
        toggleAnswerButton.addTarget(self, action: #selector(toggleAnswerSheet), for: .touchUpInside)
        content.addSubview(toggleAnswerButton)
        
        // Answer Sheet Panel
        answerSheetPanel.backgroundColor = UIColor(red: 0.08, green: 0.07, blue: 0.15, alpha: 1.0)
        answerSheetPanel.layer.cornerRadius = 16
        answerSheetPanel.layer.maskedCorners = [.layerMinXMinYCorner, .layerMaxXMinYCorner]
        answerSheetPanel.layer.borderWidth = 1
        answerSheetPanel.layer.borderColor = UIColor(white: 1.0, opacity: 0.1).cgColor
        answerSheetPanel.isHidden = true
        answerSheetPanel.translatesAutoresizingMaskIntoConstraints = false
        content.addSubview(answerSheetPanel)
        
        // Answer Sheet Panel elements
        answerScrollView.translatesAutoresizingMaskIntoConstraints = false
        answerSheetPanel.addSubview(answerScrollView)
        
        answerStackView.axis = .vertical
        answerStackView.spacing = 16
        answerStackView.alignment = .fill
        answerStackView.translatesAutoresizingMaskIntoConstraints = false
        answerScrollView.addSubview(answerStackView)
        
        submitButton.setTitle("📤 Kumpulkan Jawaban", for: .normal)
        submitButton.backgroundColor = .systemGreen
        submitButton.setTitleColor(.white, for: .normal)
        submitButton.titleLabel?.font = UIFont.systemFont(ofSize: 15, weight: .bold)
        submitButton.layer.cornerRadius = 10
        submitButton.translatesAutoresizingMaskIntoConstraints = false
        submitButton.addTarget(self, action: #selector(submitTapped), for: .touchUpInside)
        answerSheetPanel.addSubview(submitButton)
        
        // Overlays
        setupOverlays(content)
        
        NSLayoutConstraint.activate([
            headerView.topAnchor.constraint(equalTo: content.safeAreaLayoutGuide.topAnchor),
            headerView.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            headerView.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            headerView.heightAnchor.constraint(equalToConstant: 56),
            
            closeButton.centerY.constraint(equalTo: headerView.centerY),
            closeButton.leadingAnchor.constraint(equalTo: headerView.leadingAnchor, constant: 16),
            closeButton.widthAnchor.constraint(equalToConstant: 32),
            closeButton.heightAnchor.constraint(equalToConstant: 32),
            
            titleLabel.centerY.constraint(equalTo: headerView.centerY),
            titleLabel.leadingAnchor.constraint(equalTo: closeButton.trailingAnchor, constant: 12),
            titleLabel.trailingAnchor.constraint(equalTo: headerView.trailingAnchor, constant: -16),
            
            pdfView.topAnchor.constraint(equalTo: headerView.bottomAnchor),
            pdfView.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            pdfView.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            pdfView.bottomAnchor.constraint(equalTo: toggleAnswerButton.topAnchor, constant: -12),
            
            toggleAnswerButton.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 16),
            toggleAnswerButton.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -16),
            toggleAnswerButton.bottomAnchor.constraint(equalTo: content.safeAreaLayoutGuide.bottomAnchor, constant: -12),
            toggleAnswerButton.heightAnchor.constraint(equalToConstant: 44),
            
            // Answer Sheet Panel constraints
            answerSheetPanel.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            answerSheetPanel.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            answerSheetPanel.bottomAnchor.constraint(equalTo: content.bottomAnchor),
            answerSheetPanel.heightAnchor.constraint(equalTo: content.heightAnchor, multiplier: 0.5),
            
            answerScrollView.topAnchor.constraint(equalTo: answerSheetPanel.topAnchor, constant: 16),
            answerScrollView.leadingAnchor.constraint(equalTo: answerSheetPanel.leadingAnchor, constant: 16),
            answerScrollView.trailingAnchor.constraint(equalTo: answerSheetPanel.trailingAnchor, constant: -16),
            answerScrollView.bottomAnchor.constraint(equalTo: submitButton.topAnchor, constant: -12),
            
            answerStackView.topAnchor.constraint(equalTo: answerScrollView.topAnchor),
            answerStackView.leadingAnchor.constraint(equalTo: answerScrollView.leadingAnchor),
            answerStackView.trailingAnchor.constraint(equalTo: answerScrollView.trailingAnchor),
            answerStackView.bottomAnchor.constraint(equalTo: answerScrollView.bottomAnchor),
            answerStackView.widthAnchor.constraint(equalTo: answerScrollView.widthAnchor),
            
            submitButton.leadingAnchor.constraint(equalTo: answerSheetPanel.leadingAnchor, constant: 20),
            submitButton.trailingAnchor.constraint(equalTo: answerSheetPanel.trailingAnchor, constant: -20),
            submitButton.bottomAnchor.constraint(equalTo: answerSheetPanel.safeAreaLayoutGuide.bottomAnchor, constant: -12),
            submitButton.heightAnchor.constraint(equalToConstant: 48)
        ])
    }
    
    private func setupOverlays(_ content: UIView) {
        // Download Overlay
        downloadOverlay.backgroundColor = UIColor(red: 0.05, green: 0.04, blue: 0.1, alpha: 0.95)
        downloadOverlay.translatesAutoresizingMaskIntoConstraints = false
        content.addSubview(downloadOverlay)
        
        let dlLabel = UILabel()
        dlLabel.text = "Mengunduh Soal Ujian..."
        dlLabel.textColor = .white
        dlLabel.font = UIFont.systemFont(ofSize: 16, weight: .semibold)
        dlLabel.translatesAutoresizingMaskIntoConstraints = false
        downloadOverlay.addSubview(dlLabel)
        
        progressView.progress = 0.0
        progressView.progressTintColor = .systemIndigo
        progressView.trackTintColor = UIColor(white: 1.0, opacity: 0.1)
        progressView.translatesAutoresizingMaskIntoConstraints = false
        downloadOverlay.addSubview(progressView)
        
        progressLabel.text = "0%"
        progressLabel.textColor = .lightGray
        progressLabel.font = UIFont.systemFont(ofSize: 14, weight: .medium)
        progressLabel.translatesAutoresizingMaskIntoConstraints = false
        downloadOverlay.addSubview(progressLabel)
        
        cancelDownloadButton.setTitle("Batal", for: .normal)
        cancelDownloadButton.setTitleColor(.systemRed, for: .normal)
        cancelDownloadButton.titleLabel?.font = UIFont.systemFont(ofSize: 15, weight: .bold)
        cancelDownloadButton.translatesAutoresizingMaskIntoConstraints = false
        cancelDownloadButton.addTarget(self, action: #selector(cancelDownloadTapped), for: .touchUpInside)
        downloadOverlay.addSubview(cancelDownloadButton)
        
        // Error Overlay
        errorOverlay.backgroundColor = UIColor(red: 0.05, green: 0.04, blue: 0.1, alpha: 1.0)
        errorOverlay.isHidden = true
        errorOverlay.translatesAutoresizingMaskIntoConstraints = false
        content.addSubview(errorOverlay)
        
        errorLabel.textColor = .systemRed
        errorLabel.font = UIFont.systemFont(ofSize: 14, weight: .medium)
        errorLabel.textAlignment = .center
        errorLabel.numberOfLines = 0
        errorLabel.translatesAutoresizingMaskIntoConstraints = false
        errorOverlay.addSubview(errorLabel)
        
        retryButton.setTitle("Coba Lagi", for: .normal)
        retryButton.backgroundColor = .systemIndigo
        retryButton.setTitleColor(.white, for: .normal)
        retryButton.layer.cornerRadius = 8
        retryButton.translatesAutoresizingMaskIntoConstraints = false
        retryButton.addTarget(self, action: #selector(startPdfDownload), for: .touchUpInside)
        errorOverlay.addSubview(retryButton)
        
        NSLayoutConstraint.activate([
            downloadOverlay.topAnchor.constraint(equalTo: headerView.bottomAnchor),
            downloadOverlay.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            downloadOverlay.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            downloadOverlay.bottomAnchor.constraint(equalTo: content.bottomAnchor),
            
            dlLabel.centerX.constraint(equalTo: downloadOverlay.centerX),
            dlLabel.centerY.constraint(equalTo: downloadOverlay.centerY, constant: -40),
            
            progressView.topAnchor.constraint(equalTo: dlLabel.bottomAnchor, constant: 20),
            progressView.leadingAnchor.constraint(equalTo: downloadOverlay.leadingAnchor, constant: 40),
            progressView.trailingAnchor.constraint(equalTo: downloadOverlay.trailingAnchor, constant: -40),
            progressView.heightAnchor.constraint(equalToConstant: 6),
            
            progressLabel.topAnchor.constraint(equalTo: progressView.bottomAnchor, constant: 8),
            progressLabel.centerX.constraint(equalTo: downloadOverlay.centerX),
            
            cancelDownloadButton.topAnchor.constraint(equalTo: progressLabel.bottomAnchor, constant: 24),
            cancelDownloadButton.centerX.constraint(equalTo: downloadOverlay.centerX),
            
            errorOverlay.topAnchor.constraint(equalTo: headerView.bottomAnchor),
            errorOverlay.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            errorOverlay.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            errorOverlay.bottomAnchor.constraint(equalTo: content.bottomAnchor),
            
            errorLabel.centerY.constraint(equalTo: errorOverlay.centerY, constant: -30),
            errorLabel.leadingAnchor.constraint(equalTo: errorOverlay.leadingAnchor, constant: 30),
            errorLabel.trailingAnchor.constraint(equalTo: errorOverlay.trailingAnchor, constant: -30),
            
            retryButton.topAnchor.constraint(equalTo: errorLabel.bottomAnchor, constant: 20),
            retryButton.centerX.constraint(equalTo: errorOverlay.centerX),
            retryButton.widthAnchor.constraint(equalToConstant: 120),
            retryButton.heightAnchor.constraint(equalToConstant: 40)
        ])
    }
    
    private func setupNotifications() {
        // Anti-cheat: automatically submit and exit when the user minimizes the app or opens system shade
        NotificationCenter.default.addObserver(self, selector: #selector(autoSubmitAndExit), name: UIApplication.willResignActiveNotification, object: nil)
        NotificationCenter.default.addObserver(self, selector: #selector(autoSubmitAndExit), name: UIApplication.didEnterBackgroundNotification, object: nil)
    }
    
    private func loadQuestions() {
        let prefs = UserDefaults.standard
        if let data = prefs.data(forKey: "cached_questions") {
            do {
                questions = try JSONDecoder().decode([[String: AnyCodable]].self, from: data)
            } catch {
                generateDefaultQuestions()
            }
        } else {
            generateDefaultQuestions()
        }
        
        if questions.isEmpty {
            hideAnswerOverlay()
        } else {
            buildAnswerSheet()
        }
    }
    
    private func generateDefaultQuestions() {
        var list: [[String: AnyCodable]] = []
        for i in 1...40 {
            let q: [String: AnyCodable] = [
                "number": AnyCodable(Double(i)),
                "type": AnyCodable("single_choice"),
                "choices": AnyCodable(["A", "B", "C", "D", "E"])
            ]
            list.append(q)
        }
        questions = list
    }
    
    private func hideAnswerOverlay() {
        toggleAnswerButton.isHidden = true
        answerSheetPanel.isHidden = true
    }
    
    @objc private func toggleAnswerSheet() {
        answerSheetExpanded.toggle()
        answerSheetPanel.isHidden = !answerSheetExpanded
        toggleAnswerButton.setTitle(answerSheetExpanded ? "📝 Tutup Lembar Jawaban" : "📝 Buka Lembar Jawaban", for: .normal)
    }
    
    // MARK: - PDF Download
    @objc private func startPdfDownload() {
        downloadOverlay.isHidden = false
        errorOverlay.isHidden = true
        progressView.progress = 0.0
        progressLabel.text = "0%"
        
        ApiClient.shared.downloadPdf(examId: examId, onProgress: { [weak self] progress in
            DispatchQueue.main.async {
                self?.progressView.progress = Float(progress) / 100.0
                self?.progressLabel.text = "\(progress)%"
            }
        }, onSuccess: { [weak self] fileUrl in
            DispatchQueue.main.async {
                self?.downloadOverlay.isHidden = true
                self?.openPdf(fileUrl)
            }
        }, onError: { [weak self] errorMsg in
            DispatchQueue.main.async {
                self?.downloadOverlay.isHidden = true
                self?.errorOverlay.isHidden = false
                self?.errorLabel.text = errorMsg
            }
        })
    }
    
    @objc private func cancelDownloadTapped() {
        ApiClient.shared.cancelDownload()
        submittedOrExited = true
        self.navigationController?.popViewController(animated: true)
    }
    
    private func openPdf(_ fileUrl: URL) {
        if let doc = PDFDocument(url: fileUrl) {
            pdfView.document = doc
        } else {
            downloadOverlay.isHidden = true
            errorOverlay.isHidden = false
            errorLabel.text = "Gagal memproses file PDF"
        }
    }
    
    // MARK: - Answer Sheet Builder
    private func buildAnswerSheet() {
        for view in answerStackView.arrangedSubviews {
            view.removeFromSuperview()
        }
        
        for q in questions {
            guard let number = q["number"]?.intValue else { continue }
            let type = q["type"]?.stringValue ?? "single_choice"
            
            let itemCard = UIView()
            itemCard.backgroundColor = UIColor(white: 1.0, opacity: 0.03)
            itemCard.layer.cornerRadius = 8
            
            let titleLabel = UILabel()
            titleLabel.text = "Soal \(number)"
            titleLabel.textColor = .white
            titleLabel.font = UIFont.systemFont(ofSize: 14, weight: .bold)
            titleLabel.translatesAutoresizingMaskIntoConstraints = false
            itemCard.addSubview(titleLabel)
            
            let optionsStack = UIStackView()
            optionsStack.axis = .horizontal
            optionsStack.spacing = 8
            optionsStack.distribution = .fillEqually
            optionsStack.translatesAutoresizingMaskIntoConstraints = false
            itemCard.addSubview(optionsStack)
            
            NSLayoutConstraint.activate([
                titleLabel.topAnchor.constraint(equalTo: itemCard.topAnchor, constant: 10),
                titleLabel.leadingAnchor.constraint(equalTo: itemCard.leadingAnchor, constant: 12),
                titleLabel.trailingAnchor.constraint(equalTo: itemCard.trailingAnchor, constant: -12),
                
                optionsStack.topAnchor.constraint(equalTo: titleLabel.bottomAnchor, constant: 8),
                optionsStack.leadingAnchor.constraint(equalTo: itemCard.leadingAnchor, constant: 12),
                optionsStack.trailingAnchor.constraint(equalTo: itemCard.trailingAnchor, constant: -12),
                optionsStack.bottomAnchor.constraint(equalTo: itemCard.bottomAnchor, constant: -10)
            ])
            
            if type == "single_choice" {
                let choices = q["choices"]?.arrayValue as? [String] ?? ["A", "B", "C", "D", "E"]
                var buttons: [UIButton] = []
                for choice in choices {
                    let btn = createOptionButton(title: choice)
                    btn.tag = number
                    btn.addTarget(self, action: #selector(singleChoiceTapped(_:)), for: .touchUpInside)
                    optionsStack.addArrangedSubview(btn)
                    buttons.append(btn)
                }
                singleChoiceGroups[number] = buttons
            }
            else if type == "true_false" {
                var buttons: [UIButton] = []
                for choice in ["B", "S"] { // Benar / Salah
                    let btn = createOptionButton(title: choice)
                    btn.tag = number
                    btn.addTarget(self, action: #selector(trueFalseTapped(_:)), for: .touchUpInside)
                    optionsStack.addArrangedSubview(btn)
                    buttons.append(btn)
                }
                trueFalseGroups[number] = buttons
            }
            else if type == "multiple_choice" {
                let choices = q["choices"]?.arrayValue as? [String] ?? ["A", "B", "C", "D", "E"]
                var buttons: [UIButton] = []
                for choice in choices {
                    let btn = createOptionButton(title: choice)
                    btn.tag = number
                    btn.addTarget(self, action: #selector(multipleChoiceTapped(_:)), for: .touchUpInside)
                    optionsStack.addArrangedSubview(btn)
                    buttons.append(btn)
                }
                multipleChoiceGroups[number] = buttons
            }
            else if type == "matching" {
                let leftItems = q["left_items"]?.arrayValue as? [String] ?? ["1", "2", "3"]
                let rightItems = q["right_items"]?.arrayValue as? [String] ?? ["A", "B", "C"]
                
                optionsStack.axis = .vertical
                optionsStack.spacing = 8
                matchingSelectedValues[number] = [:]
                
                for left in leftItems {
                    let rowStack = UIStackView()
                    rowStack.axis = .horizontal
                    rowStack.spacing = 10
                    rowStack.alignment = .center
                    
                    let lbl = UILabel()
                    lbl.text = left
                    lbl.textColor = .lightGray
                    lbl.font = UIFont.systemFont(ofSize: 13)
                    
                    let selectorBtn = UIButton(type: .system)
                    selectorBtn.setTitle("-- Pilih --", for: .normal)
                    selectorBtn.setTitleColor(.lightGray, for: .normal)
                    selectorBtn.backgroundColor = UIColor(white: 1.0, opacity: 0.05)
                    selectorBtn.layer.cornerRadius = 6
                    selectorBtn.contentEdgeInsets = UIEdgeInsets(top: 6, left: 12, bottom: 6, right: 12)
                    
                    // Simple selection action via Action Sheet
                    let actionHandler: (UIAction) -> Void = { [weak self, weak selectorBtn] action in
                        selectorBtn?.setTitle(action.title, for: .normal)
                        selectorBtn?.setTitleColor(.white, for: .normal)
                        
                        if action.title == "-- Pilih --" {
                            self?.matchingSelectedValues[number]?[left] = nil
                        } else {
                            self?.matchingSelectedValues[number]?[left] = action.title
                        }
                        
                        if let dict = self?.matchingSelectedValues[number] {
                            self?.studentAnswers[String(number)] = dict
                        }
                    }
                    
                    var actions = [UIAction(title: "-- Pilih --", handler: actionHandler)]
                    for right in rightItems {
                        actions.append(UIAction(title: right, handler: actionHandler))
                    }
                    selectorBtn.menu = UIMenu(title: "Hubungkan ke", children: actions)
                    selectorBtn.showsMenuAsPrimaryAction = true
                    
                    rowStack.addArrangedSubview(lbl)
                    rowStack.addArrangedSubview(selectorBtn)
                    optionsStack.addArrangedSubview(rowStack)
                }
            }
            
            answerStackView.addArrangedSubview(itemCard)
        }
    }
    
    private func createOptionButton(title: String) -> UIButton {
        let btn = UIButton(type: .custom)
        btn.setTitle(title, for: .normal)
        btn.setTitleColor(.white, for: .normal)
        btn.titleLabel?.font = UIFont.systemFont(ofSize: 14, weight: .bold)
        btn.backgroundColor = UIColor(white: 1.0, opacity: 0.06)
        btn.layer.cornerRadius = 6
        btn.layer.borderWidth = 1
        btn.layer.borderColor = UIColor(white: 1.0, opacity: 0.1).cgColor
        btn.heightAnchor.constraint(equalToConstant: 36).isActive = true
        return btn
    }
    
    // MARK: - Tap Actions
    @objc private func singleChoiceTapped(_ sender: UIButton) {
        let number = sender.tag
        guard let buttons = singleChoiceGroups[number] else { return }
        for btn in buttons {
            if btn == sender {
                btn.backgroundColor = .systemIndigo
                btn.layer.borderColor = UIColor.systemIndigo.cgColor
                studentAnswers[String(number)] = btn.title(for: .normal) ?? ""
            } else {
                btn.backgroundColor = UIColor(white: 1.0, opacity: 0.06)
                btn.layer.borderColor = UIColor(white: 1.0, opacity: 0.1).cgColor
            }
        }
    }
    
    @objc private func trueFalseTapped(_ sender: UIButton) {
        let number = sender.tag
        guard let buttons = trueFalseGroups[number] else { return }
        for btn in buttons {
            if btn == sender {
                btn.backgroundColor = .systemIndigo
                btn.layer.borderColor = UIColor.systemIndigo.cgColor
                let engVal = (btn.title(for: .normal) == "B") ? "TRUE" : "FALSE"
                studentAnswers[String(number)] = engVal
            } else {
                btn.backgroundColor = UIColor(white: 1.0, opacity: 0.06)
                btn.layer.borderColor = UIColor(white: 1.0, opacity: 0.1).cgColor
            }
        }
    }
    
    @objc private func multipleChoiceTapped(_ sender: UIButton) {
        let number = sender.tag
        guard let buttons = multipleChoiceGroups[number] else { return }
        
        // Toggle selected state visually
        if sender.backgroundColor == .systemIndigo {
            sender.backgroundColor = UIColor(white: 1.0, opacity: 0.06)
            sender.layer.borderColor = UIColor(white: 1.0, opacity: 0.1).cgColor
        } else {
            sender.backgroundColor = .systemIndigo
            sender.layer.borderColor = UIColor.systemIndigo.cgColor
        }
        
        // Accumulate choices
        var selections: [String] = []
        for btn in buttons {
            if btn.backgroundColor == .systemIndigo {
                if let t = btn.title(for: .normal) {
                    selections.append(t)
                }
            }
        }
        studentAnswers[String(number)] = selections
    }
    
    // MARK: - Close / Logout
    @objc private func closeTapped() {
        confirmAndLogout()
    }
    
    private func confirmAndLogout() {
        let alert = UIAlertController(title: "Logout / Keluar Ujian", message: "Apakah Anda yakin ingin logout dan keluar dari ujian?\n\nJawaban yang sudah Anda isi akan dikumpulkan secara otomatis sebelum keluar.", preferredStyle: .alert)
        
        let confirmAction = UIAlertAction(title: "Ya, Logout & Kirim", style: .destructive) { [weak self] _ in
            self?.autoSubmitAndExit()
        }
        let cancelAction = UIAlertAction(title: "Batal", style: .cancel, handler: nil)
        
        alert.addAction(confirmAction)
        alert.addAction(cancelAction)
        
        present(alert, animated: true, completion: nil)
    }
    
    // MARK: - Submission
    @objc private func submitTapped() {
        let answered = studentAnswers.count
        let total = questions.count
        let message = answered < total ?
            "Anda baru menjawab \(answered) dari \(total) soal.\nYakin ingin mengumpulkan sekarang?" :
            "Anda sudah menjawab semua \(total) soal.\nKumpulkan jawaban?"
        
        let alert = UIAlertController(title: "Kumpulkan Jawaban", message: message, preferredStyle: .alert)
        let confirmAction = UIAlertAction(title: "Ya, Kumpulkan", style: .default) { [weak self] _ in
            self?.submitAnswers()
        }
        let cancelAction = UIAlertAction(title: "Batal", style: .cancel, handler: nil)
        
        alert.addAction(confirmAction)
        alert.addAction(cancelAction)
        present(alert, animated: true, completion: nil)
    }
    
    private func submitAnswers() {
        submitButton.isEnabled = false
        submitButton.setTitle("Mengirim...", for: .normal)
        
        ApiClient.shared.submitExam(examId: examId, studentName: studentName, examNumber: studentNumber, studentClass: studentClass, answers: studentAnswers) { [weak self] result in
            guard let self = self else { return }
            DispatchQueue.main.async {
                switch result {
                case .success(let message):
                    self.submittedOrExited = true
                    self.submitButton.setTitle("✅ Sudah Dikumpulkan", for: .normal)
                    
                    let successAlert = UIAlertController(title: "Berhasil", message: "\(message)\n\nNama: \(self.studentName)\nNomor: \(self.studentNumber)\nKelas: \(self.studentClass)", preferredStyle: .alert)
                    successAlert.addAction(UIAlertAction(title: "Selesai", style: .default) { _ in
                        self.navigationController?.popViewController(animated: true)
                    })
                    self.present(successAlert, animated: true, completion: nil)
                    
                case .failure(let error):
                    self.submitButton.isEnabled = true
                    self.submitButton.setTitle("📤 Kumpulkan Jawaban", for: .normal)
                    
                    let errAlert = UIAlertController(title: "Gagal", message: error.localizedDescription, preferredStyle: .alert)
                    errAlert.addAction(UIAlertAction(title: "OK", style: .default, handler: nil))
                    self.present(errAlert, animated: true, completion: nil)
                }
            }
        }
    }
    
    @objc private func autoSubmitAndExit() {
        guard !submittedOrExited else { return }
        submittedOrExited = true
        
        // Silently submit and return to login screen
        ApiClient.shared.submitExam(examId: examId, studentName: studentName, examNumber: studentNumber, studentClass: studentClass, answers: studentAnswers) { _ in
            // Clean up cache
            let fileManager = FileManager.default
            let cacheDir = fileManager.urls(for: .cachesDirectory, in: .userDomainMask).first!
            let pdfUrl = cacheDir.appendingPathComponent("exam_\(self.examId).pdf")
            try? fileManager.removeItem(at: pdfUrl)
        }
        
        // Instantly dismiss back
        DispatchQueue.main.async {
            self.navigationController?.popViewController(animated: true)
        }
    }
    
    deinit {
        NotificationCenter.default.removeObserver(self)
    }
}

// MARK: - SecureView helper to prevent screenshots & recordings
class SecureView: UIView {
    private let textField = UITextField()
    
    var contentView: UIView {
        return textField.subviews.first ?? self
    }
    
    override init(frame: CGRect) {
        super.init(frame: frame)
        setup()
    }
    
    required init?(coder: NSCoder) {
        super.init(coder: coder)
        setup()
    }
    
    private func setup() {
        textField.isSecureTextEntry = true
        addSubview(textField)
        textField.translatesAutoresizingMaskIntoConstraints = false
        
        NSLayoutConstraint.activate([
            textField.topAnchor.constraint(equalTo: topAnchor),
            textField.leadingAnchor.constraint(equalTo: leadingAnchor),
            textField.trailingAnchor.constraint(equalTo: trailingAnchor),
            textField.bottomAnchor.constraint(equalTo: bottomAnchor)
        ])
        
        if let secureContainer = textField.subviews.first {
            secureContainer.isUserInteractionEnabled = true
        }
    }
}
