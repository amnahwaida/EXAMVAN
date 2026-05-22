import UIKit

class ServerConfigViewController: UIViewController {
    
    private let titleLabel: UILabel = {
        let label = UILabel()
        label.text = "EXAMVAN"
        label.font = UIFont.systemFont(ofSize: 32, weight: .bold)
        label.textColor = .white
        label.textAlignment = .center
        label.translatesAutoresizingMaskIntoConstraints = false
        return label
    }()
    
    private let subtitleLabel: UILabel = {
        let label = UILabel()
        label.text = "Aplikasi Ujian Aman"
        label.font = UIFont.systemFont(ofSize: 16, weight: .medium)
        label.textColor = .lightGray
        label.textAlignment = .center
        label.translatesAutoresizingMaskIntoConstraints = false
        return label
    }()
    
    private let cardView: UIView = {
        let view = UIView()
        view.backgroundColor = UIColor(white: 1.0, opacity: 0.05)
        view.layer.cornerRadius = 16
        view.layer.borderWidth = 1
        view.layer.borderColor = UIColor(white: 1.0, opacity: 0.1).cgColor
        view.translatesAutoresizingMaskIntoConstraints = false
        return view
    }()
    
    private let urlLabel: UILabel = {
        let label = UILabel()
        label.text = "URL Server"
        label.font = UIFont.systemFont(ofSize: 14, weight: .semibold)
        label.textColor = .lightGray
        label.translatesAutoresizingMaskIntoConstraints = false
        return label
    }()
    
    private let urlTextField: UITextField = {
        let tf = UITextField()
        tf.placeholder = "http://192.168.1.13:5000"
        tf.backgroundColor = UIColor(white: 1.0, opacity: 0.08)
        tf.borderStyle = .none
        tf.layer.cornerRadius = 8
        tf.textColor = .white
        tf.tintColor = .systemIndigo
        tf.keyboardType = .URL
        tf.autocorrectionType = .no
        tf.autocapitalizationType = .none
        // Add left padding
        let paddingView = UIView(frame: CGRect(x: 0, y: 0, width: 12, height: 44))
        tf.leftView = paddingView
        tf.leftViewMode = .always
        tf.translatesAutoresizingMaskIntoConstraints = false
        return tf
    }()
    
    private let tokenLabel: UILabel = {
        let label = UILabel()
        label.text = "Token Ujian"
        label.font = UIFont.systemFont(ofSize: 14, weight: .semibold)
        label.textColor = .lightGray
        label.translatesAutoresizingMaskIntoConstraints = false
        return label
    }()
    
    private let tokenTextField: UITextField = {
        let tf = UITextField()
        tf.placeholder = "Contoh: ABCDEF"
        tf.backgroundColor = UIColor(white: 1.0, opacity: 0.08)
        tf.borderStyle = .none
        tf.layer.cornerRadius = 8
        tf.textColor = .white
        tf.tintColor = .systemIndigo
        tf.autocorrectionType = .no
        tf.autocapitalizationType = .allCharacters
        let paddingView = UIView(frame: CGRect(x: 0, y: 0, width: 12, height: 44))
        tf.leftView = paddingView
        tf.leftViewMode = .always
        tf.translatesAutoresizingMaskIntoConstraints = false
        return tf
    }()
    
    private let rememberContainer: UIStackView = {
        let stack = UIStackView()
        stack.axis = .horizontal
        stack.spacing = 10
        stack.alignment = .center
        stack.translatesAutoresizingMaskIntoConstraints = false
        return stack
    }()
    
    private let rememberSwitch: UISwitch = {
        let s = UISwitch()
        s.isOn = true
        s.onTintColor = .systemIndigo
        return s
    }()
    
    private let rememberLabel: UILabel = {
        let label = UILabel()
        label.text = "Ingat URL & Token"
        label.font = UIFont.systemFont(ofSize: 14, weight: .regular)
        label.textColor = .lightGray
        return label
    }()
    
    private let connectButton: UIButton = {
        let btn = UIButton(type: .system)
        btn.setTitle("Mulai Ujian", for: .normal)
        btn.titleLabel?.font = UIFont.systemFont(ofSize: 16, weight: .bold)
        btn.setTitleColor(.white, for: .normal)
        btn.backgroundColor = .systemIndigo
        btn.layer.cornerRadius = 10
        btn.translatesAutoresizingMaskIntoConstraints = false
        return btn
    }()
    
    private let activityIndicator: UIActivityIndicatorView = {
        let ai = UIActivityIndicatorView(style: .medium)
        ai.color = .white
        ai.hidesWhenStopped = true
        ai.translatesAutoresizingMaskIntoConstraints = false
        return ai
    }()
    
    private let errorLabel: UILabel = {
        let label = UILabel()
        label.font = UIFont.systemFont(ofSize: 13, weight: .medium)
        label.textColor = .systemRed
        label.textAlignment = .center
        label.numberOfLines = 0
        label.isHidden = true
        label.translatesAutoresizingMaskIntoConstraints = false
        return label
    }()
    
    // MARK: - Lifecycle
    override func viewDidLoad() {
        super.viewDidLoad()
        setupUI()
        loadSavedConfig()
    }
    
    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        self.navigationController?.setNavigationBarHidden(true, animated: false)
    }
    
    private func setupUI() {
        view.backgroundColor = UIColor(red: 0.05, green: 0.04, blue: 0.1, alpha: 1.0)
        
        view.addSubview(titleLabel)
        view.addSubview(subtitleLabel)
        view.addSubview(cardView)
        
        cardView.addSubview(urlLabel)
        cardView.addSubview(urlTextField)
        cardView.addSubview(tokenLabel)
        cardView.addSubview(tokenTextField)
        
        rememberContainer.addArrangedSubview(rememberSwitch)
        rememberContainer.addArrangedSubview(rememberLabel)
        cardView.addSubview(rememberContainer)
        
        cardView.addSubview(connectButton)
        cardView.addSubview(activityIndicator)
        cardView.addSubview(errorLabel)
        
        connectButton.addTarget(self, action: #selector(connectTapped), for: .touchUpInside)
        
        NSLayoutConstraint.activate([
            titleLabel.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 40),
            titleLabel.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 20),
            titleLabel.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -20),
            
            subtitleLabel.topAnchor.constraint(equalTo: titleLabel.bottomAnchor, constant: 8),
            subtitleLabel.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 20),
            subtitleLabel.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -20),
            
            cardView.topAnchor.constraint(equalTo: subtitleLabel.bottomAnchor, constant: 40),
            cardView.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 24),
            cardView.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -24),
            
            urlLabel.topAnchor.constraint(equalTo: cardView.topAnchor, constant: 24),
            urlLabel.leadingAnchor.constraint(equalTo: cardView.leadingAnchor, constant: 20),
            urlLabel.trailingAnchor.constraint(equalTo: cardView.trailingAnchor, constant: -20),
            
            urlTextField.topAnchor.constraint(equalTo: urlLabel.bottomAnchor, constant: 8),
            urlTextField.leadingAnchor.constraint(equalTo: cardView.leadingAnchor, constant: 20),
            urlTextField.trailingAnchor.constraint(equalTo: cardView.trailingAnchor, constant: -20),
            urlTextField.heightAnchor.constraint(equalToConstant: 44),
            
            tokenLabel.topAnchor.constraint(equalTo: urlTextField.bottomAnchor, constant: 16),
            tokenLabel.leadingAnchor.constraint(equalTo: cardView.leadingAnchor, constant: 20),
            tokenLabel.trailingAnchor.constraint(equalTo: cardView.trailingAnchor, constant: -20),
            
            tokenTextField.topAnchor.constraint(equalTo: tokenLabel.bottomAnchor, constant: 8),
            tokenTextField.leadingAnchor.constraint(equalTo: cardView.leadingAnchor, constant: 20),
            tokenTextField.trailingAnchor.constraint(equalTo: cardView.trailingAnchor, constant: -20),
            tokenTextField.heightAnchor.constraint(equalToConstant: 44),
            
            rememberContainer.topAnchor.constraint(equalTo: tokenTextField.bottomAnchor, constant: 16),
            rememberContainer.leadingAnchor.constraint(equalTo: cardView.leadingAnchor, constant: 20),
            
            errorLabel.topAnchor.constraint(equalTo: rememberContainer.bottomAnchor, constant: 12),
            errorLabel.leadingAnchor.constraint(equalTo: cardView.leadingAnchor, constant: 20),
            errorLabel.trailingAnchor.constraint(equalTo: cardView.trailingAnchor, constant: -20),
            
            connectButton.topAnchor.constraint(equalTo: errorLabel.bottomAnchor, constant: 12),
            connectButton.leadingAnchor.constraint(equalTo: cardView.leadingAnchor, constant: 20),
            connectButton.trailingAnchor.constraint(equalTo: cardView.trailingAnchor, constant: -20),
            connectButton.heightAnchor.constraint(equalToConstant: 48),
            connectButton.bottomAnchor.constraint(equalTo: cardView.bottomAnchor, constant: -24),
            
            activityIndicator.centerY.constraint(equalTo: connectButton.centerY),
            activityIndicator.trailingAnchor.constraint(equalTo: connectButton.trailingAnchor, constant: -16)
        ])
    }
    
    private func loadSavedConfig() {
        let prefs = UserDefaults.standard
        let remember = prefs.bool(forKey: "remember_url")
        rememberSwitch.isOn = remember
        
        if remember {
            if let savedUrl = prefs.string(forKey: "server_url") {
                urlTextField.text = savedUrl
            }
            if let savedToken = prefs.string(forKey: "exam_token") {
                tokenTextField.text = savedToken
            }
        }
    }
    
    @objc private func connectTapped() {
        guard let url = urlTextField.text?.trimmingCharacters(in: .whitespacesAndNewlines), !url.isEmpty else {
            showError("URL tidak boleh kosong")
            return
        }
        guard url.hasPrefix("http://") || url.hasPrefix("https://") else {
            showError("Format URL tidak valid (harus diawali http:// atau https://)")
            return
        }
        guard let token = tokenTextField.text?.trimmingCharacters(in: .whitespacesAndNewlines).uppercased(), !token.isEmpty else {
            showError("Token tidak boleh kosong")
            return
        }
        guard token.count == 6 else {
            showError("Token harus terdiri dari 6 karakter")
            return
        }
        
        setLoading(true)
        hideError()
        
        ApiClient.shared.setBaseUrl(url)
        
        // 1. Health check server
        ApiClient.shared.checkHealth { [weak self] result in
            guard let self = self else { return }
            
            switch result {
            case .success(let health):
                // 2. Validate token and get exam
                ApiClient.shared.getExamByToken(token: token) { [weak self] tokenResult in
                    guard let self = self else { return }
                    DispatchQueue.main.async {
                        self.setLoading(false)
                        switch tokenResult {
                        case .success(let exam):
                            self.savePreferences(url: url, token: token)
                            self.showStudentIdentityDialog(exam: exam, serverUrl: url)
                        case .failure(let error):
                            self.showError(error.localizedDescription)
                        }
                    }
                }
            case .failure(let error):
                DispatchQueue.main.async {
                    self.setLoading(false)
                    self.showError("Tidak dapat terhubung ke server: \(error.localizedDescription)")
                }
            }
        }
    }
    
    private func savePreferences(url: String, token: String) {
        let prefs = UserDefaults.standard
        if rememberSwitch.isOn {
            prefs.set(url, forKey: "server_url")
            prefs.set(token, forKey: "exam_token")
            prefs.set(true, forKey: "remember_url")
        } else {
            prefs.removeObject(forKey: "server_url")
            prefs.removeObject(forKey: "exam_token")
            prefs.set(false, forKey: "remember_url")
        }
        prefs.synchronize()
    }
    
    private func showStudentIdentityDialog(exam: Exam, serverUrl: String) {
        let alert = UIAlertController(title: "Identitas Siswa", message: "Masukkan nama, nomor ujian, dan kelas untuk memulai.", preferredStyle: .alert)
        
        alert.addTextField { tf in
            tf.placeholder = "Nama Lengkap"
            tf.autocapitalizationType = .words
            tf.text = UserDefaults.standard.string(forKey: "student_name")
        }
        alert.addTextField { tf in
            tf.placeholder = "Nomor Peserta Ujian"
            tf.keyboardType = .numbersAndPunctuation
            tf.text = UserDefaults.standard.string(forKey: "student_number")
        }
        alert.addTextField { tf in
            tf.placeholder = "Kelas (contoh: XI-IPA-1)"
            tf.autocapitalizationType = .allCharacters
            tf.text = UserDefaults.standard.string(forKey: "student_class")
        }
        
        let confirmAction = UIAlertAction(title: "Mulai", style: .default) { [weak self, weak alert] _ in
            guard let self = self,
                  let fields = alert?.textFields,
                  let name = fields[0].text?.trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty,
                  let number = fields[1].text?.trimmingCharacters(in: .whitespacesAndNewlines), !number.isEmpty,
                  let studentClass = fields[2].text?.trimmingCharacters(in: .whitespacesAndNewlines), !studentClass.isEmpty else {
                
                // Show again if empty fields
                self?.showStudentIdentityDialog(exam: exam, serverUrl: serverUrl)
                return
            }
            
            // Save student data
            let prefs = UserDefaults.standard
            prefs.set(name, forKey: "student_name")
            prefs.set(number, forKey: "student_number")
            prefs.set(studentClass, forKey: "student_class")
            
            // Cache questions JSON
            if let questions = exam.questions,
               let data = try? JSONEncoder().encode(questions) {
                prefs.set(data, forKey: "cached_questions")
            } else {
                prefs.removeObject(forKey: "cached_questions")
            }
            prefs.synchronize()
            
            // Open ExamViewerViewController
            let viewerVC = ExamViewerViewController()
            viewerVC.examId = exam.id
            viewerVC.examName = exam.name
            viewerVC.studentName = name
            viewerVC.studentNumber = number
            viewerVC.studentClass = studentClass
            viewerVC.serverUrl = serverUrl
            
            self.navigationController?.pushViewController(viewerVC, animated: true)
        }
        
        let cancelAction = UIAlertAction(title: "Batal", style: .cancel, handler: nil)
        
        alert.addAction(confirmAction)
        alert.addAction(cancelAction)
        
        present(alert, animated: true, completion: nil)
    }
    
    private func setLoading(_ loading: Bool) {
        if loading {
            activityIndicator.startAnimating()
            connectButton.isEnabled = false
            connectButton.setTitle("Menghubungkan...", for: .normal)
        } else {
            activityIndicator.stopAnimating()
            connectButton.isEnabled = true
            connectButton.setTitle("Mulai Ujian", for: .normal)
        }
    }
    
    private func showError(_ msg: String) {
        errorLabel.text = msg
        errorLabel.isHidden = false
    }
    
    private func hideError() {
        errorLabel.isHidden = true
    }
}
