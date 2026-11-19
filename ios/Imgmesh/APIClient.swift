import Foundation

enum APIError: Error {
    case code(String)
    case transport(String)
}

struct APIClient {
    var baseURL = URL(string: "https://imgmesh.com")!

    struct Account: Decodable {
        var token: String
        var alreadyRegistered: Bool?
        var user: AccountUser
    }

    struct AccountUser: Decodable {
        var id: String
        var displayName: String
        var bio: String?
        var city: String?
        var contact: String?
        var appleLinked: Bool
        var deviceId: String
    }

    struct PublicUser: Decodable, Identifiable {
        var id: String
        var displayName: String
        var bio: String
        var city: String
        var contact: String
        var sharedCount: Int
        var savedCount: Int
    }

    struct NamedUser: Decodable, Identifiable {
        var id: String
        var displayName: String
        var city: String?
    }

    struct PhotoHolder: Decodable, Identifiable {
        var id: String
        var displayName: String
        var city: String?
        var shared: Bool?
        var saved: Bool?
        var from: NamedUser?
    }

    struct PhotoCard: Decodable {
        var id: String
        var sharedBy: [NamedUser]
        var holders: [PhotoHolder]
        var savers: [PhotoHolder]
        var likeCount: Int
        var liked: Bool
        var likedBy: [NamedUser]
    }

    struct SocialUser: Decodable, Identifiable {
        var id: String
        var displayName: String
        var bio: String
        var city: String
        var contact: String
        var sharedCount: Int
        var savedCount: Int
        var unread: Int?
        var lastBody: String?
        var lastAt: String?

        var profile: PublicUser {
            PublicUser(id: id, displayName: displayName, bio: bio, city: city, contact: contact, sharedCount: sharedCount, savedCount: savedCount)
        }
    }

    struct DirectMessage: Decodable, Identifiable {
        var id: String
        var senderId: String
        var recipientId: String
        var body: String
        var createdAt: String
    }

    func register(deviceID: String, displayName: String) async throws -> Account {
        try await send(path: "/v1/register", method: "POST", token: nil, access: nil, payload: [
            "deviceId": deviceID, "displayName": displayName, "platform": "ios",
        ])
    }

    func resume(deviceID: String) async throws -> Account {
        try await send(path: "/v1/devices/session", method: "POST", token: nil, access: nil, payload: [
            "deviceId": deviceID,
        ])
    }

    func saveProfile(token: String, displayName: String, bio: String, city: String, contact: String) async throws -> AccountUser {
        struct Body: Decodable { var user: AccountUser }
        let body: Body = try await send(path: "/v1/me", method: "PATCH", token: token, access: nil, payload: [
            "displayName": displayName, "bio": bio, "city": city, "contact": contact,
        ])
        return body.user
    }

    func searchUsers(token: String, query: String) async throws -> [PublicUser] {
        struct Body: Decodable { var users: [PublicUser] }
        let escaped = query.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? query
        let body: Body = try await send(path: "/v1/users?q=\(escaped)", method: "GET", token: token, access: nil, payload: nil)
        return body.users
    }

    func setLike(token: String, photoID: String, on: Bool) async throws {
        try await sendEmpty(path: "/v1/photos/\(photoID)/like", method: on ? "POST" : "DELETE", token: token, access: nil, payload: nil)
    }

    struct FriendLists: Decodable {
        var friends: [SocialUser]
        var incoming: [SocialUser]
        var outgoing: [SocialUser]
    }

    func friends(token: String) async throws -> FriendLists {
        try await send(path: "/v1/friends", method: "GET", token: token, access: nil, payload: nil)
    }

    func requestFriend(token: String, userID: String) async throws -> String {
        struct Body: Decodable { var status: String }
        let body: Body = try await send(path: "/v1/friends", method: "POST", token: token, access: nil, payload: ["userId": userID])
        return body.status
    }

    func respondFriend(token: String, userID: String, action: String) async throws {
        if action == "remove" {
            try await sendEmpty(path: "/v1/friends/\(userID)", method: "DELETE", token: token, access: nil, payload: nil)
            return
        }
        try await sendEmpty(path: "/v1/friends/\(userID)/\(action)", method: "POST", token: token, access: nil, payload: nil)
    }

    func messages(token: String, userID: String) async throws -> [DirectMessage] {
        struct Body: Decodable { var messages: [DirectMessage] }
        let body: Body = try await send(path: "/v1/messages?with=\(userID)", method: "GET", token: token, access: nil, payload: nil)
        return body.messages
    }

    func sendMessage(token: String, to: String, body: String) async throws -> DirectMessage {
        try await send(path: "/v1/messages", method: "POST", token: token, access: nil, payload: ["to": to, "body": body])
    }

    func photoCards(token: String, ids: [String]) async throws -> [PhotoCard] {
        struct Body: Decodable { var photos: [PhotoCard] }
        let query = ids.map { "id=\($0)" }.joined(separator: "&")
        let body: Body = try await send(path: "/v1/photos?\(query)", method: "GET", token: token, access: nil, payload: nil)
        return body.photos
    }

    func claim(token: String, shareID: String, access: String, items: [[String: String]]) async throws {
        if items.isEmpty { return }
        try await sendEmpty(path: "/v1/shares/\(shareID)/photos", method: "POST", token: token, access: access, payload: [
            "items": items,
        ])
    }

    func login(existingToken: String?, identityToken: String, displayName: String) async throws -> Account {
        try await send(path: "/v1/auth/apple", method: "POST", token: existingToken, access: nil, payload: [
            "identityToken": identityToken, "displayName": displayName,
        ])
    }

    func createShare(token: String, id: String, accessTag: String) async throws {
        let _: [String: String] = try await send(path: "/v1/shares", method: "POST", token: token, access: nil, payload: [
            "id": id, "accessTag": accessTag,
        ])
    }

    struct UploadPlan: Decodable {
        var id: String
        var kind: String
        var ready: Bool
        var partSize: Int?
        var parts: [Part]
        struct Part: Decodable {
            var partNumber: Int
            var url: String
        }
    }

    func reserve(token: String, shareID: String, objects: [[String: Any]]) async throws -> [UploadPlan] {
        struct Body: Decodable { var uploads: [UploadPlan] }
        let body: Body = try await send(path: "/v1/shares/\(shareID)/uploads", method: "POST", token: token, access: nil, payload: [
            "objects": objects,
        ])
        return body.uploads
    }

    func put(url: URL, data: Data) async throws -> String {
        var request = URLRequest(url: url)
        request.httpMethod = "PUT"
        request.setValue("application/octet-stream", forHTTPHeaderField: "Content-Type")
        let (responseData, response) = try await URLSession.shared.upload(for: request, from: data)
        guard let http = response as? HTTPURLResponse else { throw APIError.transport("上传没有响应") }
        guard (200..<300).contains(http.statusCode) else {
            throw APIError.code(errorCode(responseData) ?? "upload_failed")
        }
        return http.value(forHTTPHeaderField: "ETag") ?? ""
    }

    func complete(token: String, shareID: String, objects: [[String: Any]], manifestID: String, nonce: String) async throws {
        try await sendEmpty(path: "/v1/shares/\(shareID)/uploads/complete", method: "POST", token: token, access: nil, payload: [
            "objects": objects,
            "manifestObjectId": manifestID,
            "manifestNonce": nonce,
        ])
    }

    func revoke(token: String, shareID: String) async throws {
        try await sendEmpty(path: "/v1/shares/\(shareID)", method: "DELETE", token: token, access: nil, payload: nil)
    }

    struct ActivityRow: Decodable, Identifiable {
        var displayName: String
        var client: String
        var count: Int?
        var at: String?
        var savedAt: String?
        var id: String { displayName + client + (at ?? "") + (savedAt ?? "") + String(count ?? 0) }
    }

    struct ShareActivity: Decodable {
        var viewed: [ActivityRow]
        var downloaded: [ActivityRow]
        var phones: [ActivityRow]
        var followers: [ActivityRow]? = nil
    }

    func follow(token: String, shareID: String, access: String, on: Bool) async throws {
        try await sendEmpty(path: "/v1/shares/\(shareID)/follow", method: on ? "POST" : "DELETE", token: token, access: access, payload: nil)
    }

    func activity(token: String, shareID: String) async throws -> ShareActivity {
        try await send(path: "/v1/shares/\(shareID)/events", method: "GET", token: token, access: nil, payload: nil)
    }

    struct ListedManifest: Decodable {
        var id: String
        var nonce: String
        var url: String
    }

    func manifest(token: String, shareID: String, access: String) async throws -> ListedManifest {
        try await send(path: "/v1/shares/\(shareID)/manifest", method: "GET", token: token, access: access, payload: nil)
    }

    func objectURL(token: String, shareID: String, objectID: String, access: String) async throws -> URL {
        struct Body: Decodable { var url: String }
        let body: Body = try await send(path: "/v1/shares/\(shareID)/objects/\(objectID)", method: "GET", token: token, access: access, payload: nil)
        guard let url = URL(string: body.url) else { throw APIError.transport("下载地址无效") }
        return url
    }

    func record(token: String, shareID: String, access: String, kind: String) async throws {
        try await sendEmpty(path: "/v1/shares/\(shareID)/events", method: "POST", token: token, access: access, payload: [
            "kind": kind, "client": "ios",
        ])
    }

    func download(_ url: URL) async throws -> Data {
        let (data, response) = try await URLSession.shared.data(from: url)
        guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
            throw APIError.transport("密文没有下载下来")
        }
        return data
    }

    private func send<T: Decodable>(path: String, method: String, token: String?, access: String?, payload: Any?) async throws -> T {
        let (data, http) = try await request(path: path, method: method, token: token, access: access, payload: payload)
        guard (200..<300).contains(http.statusCode) else { throw APIError.code(errorCode(data) ?? "request_failed") }
        do {
            return try JSONDecoder().decode(T.self, from: data)
        } catch {
            throw APIError.transport("响应读不出来")
        }
    }

    private func sendEmpty(path: String, method: String, token: String?, access: String?, payload: Any?) async throws {
        let (data, http) = try await request(path: path, method: method, token: token, access: access, payload: payload)
        guard (200..<300).contains(http.statusCode) else { throw APIError.code(errorCode(data) ?? "request_failed") }
    }

    private func request(path: String, method: String, token: String?, access: String?, payload: Any?) async throws -> (Data, HTTPURLResponse) {
        guard let url = URL(string: path, relativeTo: baseURL)?.absoluteURL else {
            throw APIError.transport("地址无效")
        }
        var request = URLRequest(url: url)
        request.httpMethod = method
        if let token {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        if let access {
            request.setValue(access, forHTTPHeaderField: "X-Imgmesh-Access")
        }
        if let payload {
            request.httpBody = try JSONSerialization.data(withJSONObject: payload)
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            guard let http = response as? HTTPURLResponse else { throw APIError.transport("没有响应") }
            return (data, http)
        } catch let error as APIError {
            throw error
        } catch {
            throw APIError.transport("网络中断了，可以再试一次。")
        }
    }

    private func errorCode(_ data: Data) -> String? {
        (try? JSONDecoder().decode([String: String].self, from: data))?["error"]
    }
}

func apiMessage(_ error: Error) -> String {
    if case APIError.code(let code) = error {
        switch code {
        case "quota": return "空间已经用完。"
        case "too_large": return "照片太多，或单张超过 50MB。"
        case "unauthorized": return "登录已失效，请重新登录。"
        case "access_denied": return "链接不完整，或密钥对不上。"
        case "revoked": return "这份分享已经撤销。"
        case "expired": return "这份分享已经过期。"
        case "not_found": return "没有找到这份分享。"
        case "name_taken": return "这个名称已经有人用了，换一个。"
        case "name_required": return "先登记一个名称。"
        case "apple_required": return "先用 Apple 登录，再分享照片。"
        case "not_friends": return "先成为好友，才能发消息。"
        default: return "请求没有完成。"
        }
    }
    if case APIError.transport(let message) = error { return message }
    return "没有完成，可以再试一次。"
}
