import Foundation

class ApiClient: NSObject, URLSessionDownloadDelegate {
    static let shared = ApiClient()
    
    private var baseUrl: String = ""
    private var downloadSession: URLSession?
    private var onDownloadProgress: ((Int) -> Void)?
    private var onDownloadSuccess: ((URL) -> Void)?
    private var onDownloadError: ((String) -> Void)?
    private var activeDownloadTask: URLSessionDownloadTask?
    
    override init() {
        super.init()
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 15
        config.timeoutIntervalForResource = 60
        self.downloadSession = URLSession(configuration: config, delegate: self, delegateQueue: OperationQueue.main)
    }
    
    func setBaseUrl(_ url: String) {
        var cleanUrl = url.trimmingCharacters(in: .whitespacesAndNewlines)
        if cleanUrl.hasSuffix("/") {
            cleanUrl.removeLast()
        }
        self.baseUrl = cleanUrl
    }
    
    func getBaseUrl() -> String {
        return self.baseUrl
    }
    
    // MARK: - Health Check
    func checkHealth(completion: @escaping (Result<HealthResponse, Error>) -> Void) {
        guard let url = URL(string: "\(baseUrl)/api/health") else {
            completion(.failure(NSError(domain: "Invalid URL", code: 0, userInfo: nil)))
            return
        }
        
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        
        let task = URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }
            guard let data = data else {
                completion(.failure(NSError(domain: "Empty Response", code: 0, userInfo: nil)))
                return
            }
            do {
                let health = try JSONDecoder().decode(HealthResponse.self, from: data)
                completion(.success(health))
            } catch {
                completion(.failure(error))
            }
        }
        task.resume()
    }
    
    // MARK: - Get Exams
    func getExams(completion: @escaping (Result<[Exam], Error>) -> Void) {
        guard let url = URL(string: "\(baseUrl)/api/exams") else {
            completion(.failure(NSError(domain: "Invalid URL", code: 0, userInfo: nil)))
            return
        }
        
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        
        let task = URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }
            guard let data = data else {
                completion(.failure(NSError(domain: "Empty Response", code: 0, userInfo: nil)))
                return
            }
            do {
                let res = try JSONDecoder().decode(ExamListResponse.self, from: data)
                completion(.success(res.data))
            } catch {
                completion(.failure(error))
            }
        }
        task.resume()
    }
    
    // MARK: - Get Exam by Token
    func getExamByToken(token: String, completion: @escaping (Result<Exam, Error>) -> Void) {
        guard let url = URL(string: "\(baseUrl)/api/exams/token/\(token)") else {
            completion(.failure(NSError(domain: "Invalid URL", code: 0, userInfo: nil)))
            return
        }
        
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        
        let task = URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }
            
            if let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 404 {
                completion(.failure(NSError(domain: "Not Found", code: 404, userInfo: [NSLocalizedDescriptionKey: "Token tidak valid atau ujian sudah berakhir"])))
                return
            }
            
            guard let data = data else {
                completion(.failure(NSError(domain: "Empty Response", code: 0, userInfo: nil)))
                return
            }
            do {
                let res = try JSONDecoder().decode(TokenExamResponse.self, from: data)
                if let exam = res.data {
                    completion(.success(exam))
                } else {
                    completion(.failure(NSError(domain: "Not Found", code: 404, userInfo: [NSLocalizedDescriptionKey: res.message ?? "Token tidak valid"])))
                }
            } catch {
                completion(.failure(error))
            }
        }
        task.resume()
    }
    
    // MARK: - Download PDF with Progress
    func downloadPdf(examId: Int, onProgress: @escaping (Int) -> Void, onSuccess: @escaping (URL) -> Void, onError: @escaping (String) -> Void) {
        self.onDownloadProgress = onProgress
        self.onDownloadSuccess = onSuccess
        self.onDownloadError = onError
        
        let fileManager = FileManager.default
        let cacheDir = fileManager.urls(for: .cachesDirectory, in: .userDomainMask).first!
        let finalFileUrl = cacheDir.appendingPathComponent("exam_\(examId).pdf")
        
        // Return cache if it exists
        if fileManager.fileExists(atPath: finalFileUrl.path) {
            onProgress(100)
            onSuccess(finalFileUrl)
            return
        }
        
        guard let url = URL(string: "\(baseUrl)/api/exams/\(examId)/pdf") else {
            onError("URL tidak valid")
            return
        }
        
        activeDownloadTask?.cancel()
        activeDownloadTask = downloadSession?.downloadTask(with: url)
        activeDownloadTask?.resume()
    }
    
    func cancelDownload() {
        activeDownloadTask?.cancel()
    }
    
    // MARK: - Submit Exam Answers
    func submitExam(examId: Int, studentName: String, examNumber: String, studentClass: String, answers: [String: Any], completion: @escaping (Result<String, Error>) -> Void) {
        guard let url = URL(string: "\(baseUrl)/api/exams/\(examId)/submit") else {
            completion(.failure(NSError(domain: "Invalid URL", code: 0, userInfo: nil)))
            return
        }
        
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        
        let payload: [String: Any] = [
            "student_name": studentName,
            "exam_number": examNumber,
            "student_class": studentClass,
            "answers": answers
        ]
        
        do {
            let data = try JSONSerialization.data(withJSONObject: payload, options: [])
            request.httpBody = data
        } catch {
            completion(.failure(error))
            return
        }
        
        let task = URLSession.shared.dataTask(with: request) { data, response, error in
            if let error = error {
                completion(.failure(error))
                return
            }
            guard let data = data else {
                completion(.failure(NSError(domain: "Empty Response", code: 0, userInfo: nil)))
                return
            }
            do {
                if let json = try JSONSerialization.jsonObject(with: data, options: []) as? [String: Any] {
                    let success = json["success"] as? Bool ?? false
                    let message = json["message"] as? String ?? "Ujian berhasil dikumpulkan"
                    if success {
                        completion(.success(message))
                    } else {
                        completion(.failure(NSError(domain: "Submit Error", code: 0, userInfo: [NSLocalizedDescriptionKey: message])))
                    }
                } else {
                    completion(.failure(NSError(domain: "Invalid JSON", code: 0, userInfo: nil)))
                }
            } catch {
                completion(.failure(error))
            }
        }
        task.resume()
    }
    
    // MARK: - URLSessionDownloadDelegate implementation
    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didWriteData bytesWritten: Int64, totalBytesWritten: Int64, totalBytesExpectedToWrite: Int64) {
        if totalBytesExpectedToWrite > 0 {
            let progress = Int((Double(totalBytesWritten) / Double(totalBytesExpectedToWrite)) * 100)
            onDownloadProgress?(min(progress, 100))
        }
    }
    
    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {
        guard let response = downloadTask.response as? HTTPURLResponse, response.statusCode == 200 else {
            onDownloadError?("Gagal mengunduh file: HTTP Status \( (downloadTask.response as? HTTPURLResponse)?.statusCode ?? 0)")
            return
        }
        
        let fileManager = FileManager.default
        let cacheDir = fileManager.urls(for: .cachesDirectory, in: .userDomainMask).first!
        
        // Find exam ID from URL path: /api/exams/<id>/pdf
        guard let urlPath = downloadTask.originalRequest?.url?.pathComponents,
              let idIndex = urlPath.firstIndex(of: "exams"),
              idIndex + 1 < urlPath.count,
              let examId = Int(urlPath[idIndex + 1]) else {
            onDownloadError?("Informasi Ujian tidak valid")
            return
        }
        
        let destinationUrl = cacheDir.appendingPathComponent("exam_\(examId).pdf")
        
        try? fileManager.removeItem(at: destinationUrl)
        do {
            try fileManager.moveItem(at: location, to: destinationUrl)
            onDownloadSuccess?(destinationUrl)
        } catch {
            onDownloadError?(error.localizedDescription)
        }
    }
    
    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        if let error = error {
            let nsError = error as NSError
            if nsError.code != NSURLErrorCancelled {
                onDownloadError?(error.localizedDescription)
            }
        }
    }
}
