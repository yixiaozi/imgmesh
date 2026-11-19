import Foundation
import Security

struct Session: Codable {
    var token: String
    var displayName: String
    var appleLinked: Bool
    var deviceID: String

    init(token: String, displayName: String, appleLinked: Bool, deviceID: String) {
        self.token = token
        self.displayName = displayName
        self.appleLinked = appleLinked
        self.deviceID = deviceID
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        token = try container.decode(String.self, forKey: .token)
        displayName = try container.decodeIfPresent(String.self, forKey: .displayName) ?? ""
        appleLinked = try container.decodeIfPresent(Bool.self, forKey: .appleLinked) ?? true
        deviceID = try container.decodeIfPresent(String.self, forKey: .deviceID) ?? ""
    }
}

struct LocalShare: Codable, Identifiable {
    var id: String
    var masterKey: String
    var title: String
    var role: String
    var photoCount: Int
    var createdAt: Date
    var autoSave: Bool
    var savedIDs: [String]

    init(id: String, masterKey: String, title: String, role: String, photoCount: Int, createdAt: Date, autoSave: Bool = false, savedIDs: [String] = []) {
        self.id = id
        self.masterKey = masterKey
        self.title = title
        self.role = role
        self.photoCount = photoCount
        self.createdAt = createdAt
        self.autoSave = autoSave
        self.savedIDs = savedIDs
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        masterKey = try container.decode(String.self, forKey: .masterKey)
        title = try container.decode(String.self, forKey: .title)
        role = try container.decode(String.self, forKey: .role)
        photoCount = try container.decode(Int.self, forKey: .photoCount)
        createdAt = try container.decode(Date.self, forKey: .createdAt)
        autoSave = try container.decodeIfPresent(Bool.self, forKey: .autoSave) ?? false
        savedIDs = try container.decodeIfPresent([String].self, forKey: .savedIDs) ?? []
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(masterKey, forKey: .masterKey)
        try container.encode(title, forKey: .title)
        try container.encode(role, forKey: .role)
        try container.encode(photoCount, forKey: .photoCount)
        try container.encode(createdAt, forKey: .createdAt)
        try container.encode(autoSave, forKey: .autoSave)
        try container.encode(savedIDs, forKey: .savedIDs)
    }

    private enum CodingKeys: String, CodingKey {
        case id, masterKey, title, role, photoCount, createdAt, autoSave, savedIDs
    }
}

enum KeychainStore {
    private static let service = "com.imgmesh.app"

    static func saveSession(_ session: Session) {
        guard let data = try? JSONEncoder().encode(session) else { return }
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: "session",
        ]
        SecItemDelete(query as CFDictionary)
        var add = query
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(add as CFDictionary, nil)
    }

    static func loadSession() -> Session? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: "session",
            kSecReturnData as String: true,
        ]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
              let data = item as? Data
        else { return nil }
        return try? JSONDecoder().decode(Session.self, from: data)
    }

    static func clearSession() {
        delete("session")
    }

    static func deviceID() -> String {
        if let existing = loadString("device"), existing.count >= 16 { return existing }
        let created = UUID().uuidString.lowercased()
        saveString("device", created)
        return created
    }

    private static func saveString(_ account: String, _ value: String) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        SecItemDelete(query as CFDictionary)
        var add = query
        add[kSecValueData as String] = Data(value.utf8)
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(add as CFDictionary, nil)
    }

    private static func loadString(_ account: String) -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
        ]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
              let data = item as? Data
        else { return nil }
        return String(data: data, encoding: .utf8)
    }

    private static func delete(_ account: String) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        SecItemDelete(query as CFDictionary)
    }
}

enum ShareLibrary {
    private static var fileURL: URL {
        let root = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("imgmesh", isDirectory: true)
        try? FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        return root.appendingPathComponent("library.json")
    }

    static func load() -> [LocalShare] {
        guard let data = try? Data(contentsOf: fileURL) else { return [] }
        return (try? JSONDecoder().decode([LocalShare].self, from: data)) ?? []
    }

    static func save(_ shares: [LocalShare]) {
        guard let data = try? JSONEncoder().encode(shares) else { return }
        try? data.write(to: fileURL, options: [.atomic, .completeFileProtection])
    }
}
