import Foundation

struct Request: Decodable {
    let id: Int64
    let method: String
    let params: Params?
}

struct Params: Decodable {
    let pid: Int?
    let maxDepth: Int?
    let semanticBudget: Int?
    let filter: String?
    let path: String?
    let expectedRole: String?
    let value: String?
    let appName: String?
    let query: String?
    let role: String?
    let identifier: String?
    let type: String?
    let x: Double?
    let y: Double?
    let button: String?
    let clicks: Int?
    let key: String?
    let modifiers: [String]?
    let dx: Int?
    let dy: Int?
    let windowTitle: String?
    let verify: Bool?
    let condition: String?
    let timeout: Double?
    let interval: Double?
    let roles: [String]?
    let maxLabels: Int?

    enum CodingKeys: String, CodingKey {
        case pid, filter, path, value, query, role, identifier, type
        case x, y, button, clicks, key, modifiers, dx, dy
        case condition, timeout, interval, verify, roles
        case maxDepth = "max_depth"
        case semanticBudget = "semantic_budget"
        case expectedRole = "expected_role"
        case appName = "app_name"
        case windowTitle = "window_title"
        case maxLabels = "max_labels"
    }
}

struct Response: Encodable {
    let id: Int64
    var result: AnyCodable?
    var error: ErrorInfo?
}

struct ErrorInfo: Encodable {
    let code: Int
    let message: String
}

struct Element: Encodable {
    let ref: String
    let role: String
    var subrole: String?
    var title: String?
    var desc: String?
    var value: String?
    var enabled: Bool?
    var selected: Bool?
    var children: [Element]?
}

struct ReadTreeResult: Encodable {
    let app: String
    let pid: Int
    let window: String
    let elements: [Element]
    let refPaths: [String: RefEntry]

    enum CodingKeys: String, CodingKey {
        case app, pid, window, elements
        case refPaths = "ref_paths"
    }
}

struct RefEntry: Encodable {
    let path: String
    let role: String
}

struct AnnotationEntry: Encodable {
    let label: Int
    let ref: String
    let role: String
    var title: String?
    let x: Double
    let y: Double
    let width: Double
    let height: Double
}

struct AnnotateResult: Encodable {
    let app: String
    let pid: Int
    let window: String
    let annotations: [AnnotationEntry]
    let refPaths: [String: RefEntry]

    enum CodingKeys: String, CodingKey {
        case app, pid, window, annotations
        case refPaths = "ref_paths"
    }
}

struct FindResult: Encodable {
    let path: String
    let role: String
    let title: String
    var desc: String?
    var value: String?
}

struct AppContext: Encodable {
    let app: String
    let window: String
    var url: String?
    var focusedElement: String?

    enum CodingKeys: String, CodingKey {
        case app, window, url
        case focusedElement = "focused_element"
    }
}

struct ActionResult: Encodable {
    let result: String
    var role: String?
    var context: AppContext?
}

/// Type-erased Encodable wrapper for JSON responses.
struct AnyCodable: Encodable {
    private let _encode: (Encoder) throws -> Void

    init<T: Encodable>(_ value: T) {
        _encode = { encoder in
            try value.encode(to: encoder)
        }
    }

    func encode(to encoder: Encoder) throws {
        try _encode(encoder)
    }
}
