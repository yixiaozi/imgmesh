import CryptoKit
import Foundation
import Security

enum SealError: Error {
    case length
    case random
}

enum ImgmeshSeal {
    static func random(_ count: Int) throws -> Data {
        var data = Data(count: count)
        let status = data.withUnsafeMutableBytes { SecRandomCopyBytes(kSecRandomDefault, count, $0.baseAddress!) }
        guard status == errSecSuccess else { throw SealError.random }
        return data
    }

    static func derive(master: Data, shareID: Data, info: String) throws -> SymmetricKey {
        guard master.count == 32, shareID.count == 16 else { throw SealError.length }
        return HKDF<SHA256>.deriveKey(
            inputKeyMaterial: SymmetricKey(data: master),
            salt: shareID,
            info: Data(info.utf8),
            outputByteCount: 32
        )
    }

    static func accessTag(master: Data, shareID: Data) throws -> Data {
        guard master.count == 32, shareID.count == 16 else { throw SealError.length }
        var message = Data("imgmesh-access-v1".utf8)
        message.append(shareID)
        return Data(HMAC<SHA256>.authenticationCode(for: message, using: SymmetricKey(data: master)))
    }

    static func aad(shareID: Data, objectID: Data) throws -> Data {
        guard shareID.count == 16, objectID.count == 16 else { throw SealError.length }
        var out = Data()
        out.append(shareID)
        out.append(objectID)
        return out
    }

    static func encrypt(key: SymmetricKey, nonce: Data, aad: Data, plain: Data) throws -> Data {
        let box = try AES.GCM.seal(plain, using: key, nonce: try AES.GCM.Nonce(data: nonce), authenticating: aad)
        return box.ciphertext + box.tag
    }

    static func decrypt(key: SymmetricKey, nonce: Data, aad: Data, ciphertext: Data) throws -> Data {
        guard ciphertext.count >= 16 else { throw SealError.length }
        let box = try AES.GCM.SealedBox(
            nonce: try AES.GCM.Nonce(data: nonce),
            ciphertext: ciphertext.dropLast(16),
            tag: ciphertext.suffix(16)
        )
        return try AES.GCM.open(box, using: key, authenticating: aad)
    }

    static func encode(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    static func decode(_ value: String) throws -> Data {
        var text = value.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        text += String(repeating: "=", count: (4 - text.count % 4) % 4)
        guard let data = Data(base64Encoded: text), encode(data) == value else { throw SealError.length }
        return data
    }
}

struct Manifest: Codable {
    var v: Int
    var title: String
    var items: [ManifestItem]
}

struct ManifestItem: Codable, Identifiable {
    var id: String
    var name: String?
    var mime: String
    var bytes: Int
    var nonce: String
    var thumb: ManifestThumb?
}

struct ManifestThumb: Codable {
    var id: String
    var bytes: Int
    var nonce: String
}

struct ShareURLParts {
    var id: String
    var masterKey: String
}

enum ShareURL {
    static func parse(_ text: String) -> ShareURLParts? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let url = URL(string: trimmed), let host = url.host?.lowercased() else { return nil }
        guard host == "imgmesh.com" || host == "www.imgmesh.com" else { return nil }
        let parts = url.path.split(separator: "/").map(String.init)
        guard parts.count == 2, parts[0] == "s" else { return nil }
        guard let fragment = url.fragment,
              let key = fragment.split(separator: "&").compactMap({ pair -> String? in
                  let bits = pair.split(separator: "=", maxSplits: 1).map(String.init)
                  return bits.count == 2 && bits[0] == "k" ? bits[1] : nil
              }).first
        else { return nil }
        return ShareURLParts(id: parts[1], masterKey: key)
    }

    static func make(id: String, masterKey: String) -> String {
        "https://imgmesh.com/s/\(id)#k=\(masterKey)"
    }
}
