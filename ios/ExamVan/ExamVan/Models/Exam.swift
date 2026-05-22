import Foundation

// MARK: - Exam Model
struct Exam: Codable {
    let id: Int
    let name: String
    let status: String
    let size_mb: Double
    let token: String?
    let questions: [[String: AnyCodable]]?
    let created_at: String
}

struct ExamListResponse: Codable {
    let success: Bool
    let data: [Exam]
}

struct TokenExamResponse: Codable {
    let success: Bool
    let data: Exam?
    let error: String?
    let message: String?
}

struct HealthResponse: Codable {
    let status: String
    let version: String
    let lan_mode: Bool
    let timestamp: String?
    let server_time_utc: String?
}

struct SubmitResponse: Codable {
    let success: Bool
    let message: String?
    let score: Double?
}

// MARK: - AnyCodable (type-erased Codable)
struct AnyCodable: Codable {
    let value: Any

    init(_ value: Any) { self.value = value }

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { value = NSNull() }
        else if let v = try? c.decode(Bool.self) { value = v }
        else if let v = try? c.decode(Int.self) { value = v }
        else if let v = try? c.decode(Double.self) { value = v }
        else if let v = try? c.decode(String.self) { value = v }
        else if let v = try? c.decode([AnyCodable].self) { value = v.map { $0.value } }
        else if let v = try? c.decode([String: AnyCodable].self) { value = v.mapValues { $0.value } }
        else { throw DecodingError.dataCorruptedError(in: c, debugDescription: "Unsupported") }
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch value {
        case is NSNull: try c.encodeNil()
        case let v as Bool: try c.encode(v)
        case let v as Int: try c.encode(v)
        case let v as Double: try c.encode(v)
        case let v as String: try c.encode(v)
        case let v as [Any]: try c.encode(v.map { AnyCodable($0) })
        case let v as [String: Any]: try c.encode(v.mapValues { AnyCodable($0) })
        default: try c.encodeNil()
        }
    }

    var stringValue: String? { value as? String }
    var intValue: Int? {
        if let i = value as? Int { return i }
        if let d = value as? Double { return Int(d) }
        return nil
    }
    var doubleValue: Double? { value as? Double }
    var boolValue: Bool? { value as? Bool }
    var arrayValue: [Any]? { value as? [Any] }
    var dictValue: [String: Any]? { value as? [String: Any] }
}
