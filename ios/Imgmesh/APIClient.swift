import Foundation

enum APIError: Error {
    case code(String)
    case transport(String)
}

struct APIClient {
    var baseURL = URL(string: "https://imgmesh.com")!

    func login(identityToken: String, displayName: String) async throws -> (token: String, name: String) {
        struct Body: Decodable {
            var token: String
            var user: User
            struct User: Decodable { var displayName: String }
        }
        let body: Body = try await send(path: "/v1/auth/apple", method: "POST", token: nil, access: nil, payload: [
            "identityToken": identityToken, "displayName": displayName,
        ])
        return (body.token, body.user.displayName)
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

    struct RemoteEvent: Decodable, Identifiable {
        var kind: String
        var client: String
        var displayName: String
        var createdAt: String
        var id: String { kind + createdAt + displayName + client }
    }

    func events(token: String, shareID: String) async throws -> [RemoteEvent] {
        struct Body: Decodable { var events: [RemoteEvent] }
        let body: Body = try await send(path: "/v1/shares/\(shareID)/events", method: "GET", token: token, access: nil, payload: nil)
        return body.events
    }

    struct ListedManifest: Decodable {
        var id: String
        var nonce: String
        var url: String
    }

    func manifest(shareID: String, access: String) async throws -> ListedManifest {
        try await send(path: "/v1/shares/\(shareID)/manifest", method: "GET", token: nil, access: access, payload: nil)
    }

    func objectURL(shareID: String, objectID: String, access: String) async throws -> URL {
        struct Body: Decodable { var url: String }
        let body: Body = try await send(path: "/v1/shares/\(shareID)/objects/\(objectID)", method: "GET", token: nil, access: access, payload: nil)
        guard let url = URL(string: body.url) else { throw APIError.transport("下载地址无效") }
        return url
    }

    func record(shareID: String, access: String, kind: String, name: String, deviceID: String) async throws {
        try await sendEmpty(path: "/v1/shares/\(shareID)/events", method: "POST", token: nil, access: access, payload: [
            "kind": kind, "client": "ios", "displayName": name, "deviceId": deviceID,
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
        default: return "请求没有完成。"
        }
    }
    if case APIError.transport(let message) = error { return message }
    return "没有完成，可以再试一次。"
}
