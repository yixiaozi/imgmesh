import AuthenticationServices
import Foundation

@MainActor
final class AppModel: ObservableObject {
    @Published var session: Session?
    @Published var shares: [LocalShare] = []
    @Published var status = ""
    @Published var errorMessage = ""
    @Published var link = ""
    @Published var busy = false
    @Published var selectedID: String?
    @Published var gallery: [GalleryPhoto] = []
    @Published var activity = APIClient.ShareActivity(viewed: [], downloaded: [], phones: [])
    @Published var draftName = ""
    @Published var bio = ""
    @Published var city = ""
    @Published var contact = ""
    @Published var peopleQuery = ""
    @Published var people: [APIClient.PublicUser] = []
    @Published var cards: [String: APIClient.PhotoCard] = [:]

    let api = APIClient()
    private var deviceID = KeychainStore.deviceID()

    struct GalleryPhoto: Identifiable {
        var id: String
        var name: String
        var mime: String
        var preview: Data?
        var original: Data?
        var item: ManifestItem
    }

    init() {
        session = KeychainStore.loadSession()
        shares = ShareLibrary.load()
    }

    func restore() async {
        deviceID = KeychainStore.deviceID()
        if session != nil { return }
        do {
            let resumed = try await api.resume(deviceID: deviceID)
            store(resumed)
        } catch let error as APIError {
            if case .code("not_found") = error { return }
            errorMessage = apiMessage(error)
        } catch {
            errorMessage = apiMessage(error)
        }
    }

    func register() async {
        let name = draftName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty else {
            errorMessage = "先写一个名称。"
            return
        }
        do {
            let created = try await api.register(deviceID: deviceID, displayName: name)
            store(created)
            status = created.alreadyRegistered == true
                ? "这台设备已经注册为 \(created.user.displayName)。名称保持第一次填写的。"
                : "已注册为 \(created.user.displayName)。"
            errorMessage = ""
        } catch {
            errorMessage = apiMessage(error)
        }
    }

    func saveProfile() async {
        guard let token = session?.token else { return }
        let name = draftName.trimmingCharacters(in: .whitespacesAndNewlines)
        let displayName = name.isEmpty ? (session?.displayName ?? "") : name
        do {
            let user = try await api.saveProfile(token: token, displayName: displayName, bio: bio, city: city, contact: contact)
            session?.displayName = user.displayName
            bio = user.bio ?? ""
            city = user.city ?? ""
            contact = user.contact ?? ""
            if let session { KeychainStore.saveSession(session) }
            status = "资料已保存。别人可以用名称、城市或联系方式找到你。"
            errorMessage = ""
        } catch {
            errorMessage = apiMessage(error)
        }
    }

    func findPeople() async {
        guard let token = session?.token else { return }
        let query = peopleQuery.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !query.isEmpty else {
            people = []
            return
        }
        do {
            people = try await api.searchUsers(token: token, query: query)
            if people.isEmpty { status = "没有找到这个人。" }
            errorMessage = ""
        } catch {
            errorMessage = apiMessage(error)
        }
    }

    func signIn(_ credential: ASAuthorizationAppleIDCredential) async {
        guard let tokenData = credential.identityToken, let token = String(data: tokenData, encoding: .utf8) else {
            errorMessage = "没有拿到 Apple 登录凭证。"
            return
        }
        guard session != nil else {
            errorMessage = "先登记这台设备的名称。"
            return
        }
        let name = [credential.fullName?.familyName, credential.fullName?.givenName].compactMap { $0 }.joined()
        do {
            let loggedIn = try await api.login(existingToken: session?.token, identityToken: token, displayName: name)
            store(loggedIn)
            status = "Apple 登录已连到 \(loggedIn.user.displayName)。现在可以分享照片。"
            errorMessage = ""
        } catch {
            errorMessage = apiMessage(error)
        }
    }

    private func store(_ account: APIClient.Account) {
        let stored = Session(
            token: account.token,
            displayName: account.user.displayName,
            appleLinked: account.user.appleLinked,
            deviceID: account.user.deviceId.isEmpty ? deviceID : account.user.deviceId
        )
        session = stored
        bio = account.user.bio ?? ""
        city = account.user.city ?? ""
        contact = account.user.contact ?? ""
        KeychainStore.saveSession(stored)
    }

    func createShare(providers: [NSItemProvider], title: String) async {
        guard let session else {
            errorMessage = "先登记这台设备的名称。"
            return
        }
        guard session.appleLinked else {
            errorMessage = "先用 Apple 登录，再分享照片。"
            return
        }
        guard !providers.isEmpty else { return }
        guard providers.count <= 200 else {
            errorMessage = "一次最多 200 张。"
            return
        }
        busy = true
        link = ""
        errorMessage = ""
        status = "正在读取照片"
        defer { busy = false }
        do {
            let shareID = try ImgmeshSeal.random(16)
            let master = try ImgmeshSeal.random(32)
            let shareIDText = ImgmeshSeal.encode(shareID)
            let masterText = ImgmeshSeal.encode(master)
            let tag = ImgmeshSeal.encode(try ImgmeshSeal.accessTag(master: master, shareID: shareID))
            try await api.createShare(token: session.token, id: shareIDText, accessTag: tag)

            let fileKey = try ImgmeshSeal.derive(master: master, shareID: shareID, info: "imgmesh-file-v1")
            var items: [ManifestItem] = []
            var blobs: [String: Data] = [:]
            var total = 0
            for (index, provider) in providers.enumerated() {
                status = "正在加密 \(index + 1)/\(providers.count)"
                let photo = try await PhotoIO.load(provider)
                total += photo.data.count
                if total > 2 * 1024 * 1024 * 1024 { throw APIError.transport("这一组照片超过 2GB。") }
                let objectID = try ImgmeshSeal.random(16)
                let nonce = try ImgmeshSeal.random(12)
                let objectText = ImgmeshSeal.encode(objectID)
                let cipher = try ImgmeshSeal.encrypt(
                    key: fileKey,
                    nonce: nonce,
                    aad: try ImgmeshSeal.aad(shareID: shareID, objectID: objectID),
                    plain: photo.data
                )
                blobs[objectText] = cipher
                var thumb: ManifestThumb?
                if let jpeg = PhotoIO.thumbnail(from: photo.data) {
                    let thumbID = try ImgmeshSeal.random(16)
                    let thumbNonce = try ImgmeshSeal.random(12)
                    let thumbText = ImgmeshSeal.encode(thumbID)
                    blobs[thumbText] = try ImgmeshSeal.encrypt(
                        key: fileKey,
                        nonce: thumbNonce,
                        aad: try ImgmeshSeal.aad(shareID: shareID, objectID: thumbID),
                        plain: jpeg
                    )
                    thumb = ManifestThumb(id: thumbText, bytes: jpeg.count, nonce: ImgmeshSeal.encode(thumbNonce))
                }
                items.append(ManifestItem(
                    id: objectText,
                    name: photo.name,
                    mime: photo.mime,
                    bytes: photo.data.count,
                    nonce: ImgmeshSeal.encode(nonce),
                    sha256: ImgmeshSeal.fingerprint(photo.data),
                    thumb: thumb
                ))
            }

            let manifest = Manifest(v: 1, title: title.isEmpty ? "旅行照片" : title, items: items)
            let manifestPlain = try JSONEncoder().encode(manifest)
            let manifestID = try ImgmeshSeal.random(16)
            let manifestNonce = try ImgmeshSeal.random(12)
            let manifestText = ImgmeshSeal.encode(manifestID)
            let manifestKey = try ImgmeshSeal.derive(master: master, shareID: shareID, info: "imgmesh-manifest-v1")
            blobs[manifestText] = try ImgmeshSeal.encrypt(
                key: manifestKey,
                nonce: manifestNonce,
                aad: try ImgmeshSeal.aad(shareID: shareID, objectID: manifestID),
                plain: manifestPlain
            )

            var objects: [[String: Any]] = []
            for item in items {
                objects.append(objectPayload(id: item.id, kind: "original", plain: item.bytes, cipher: blobs[item.id]?.count ?? 0))
                if let thumb = item.thumb {
                    objects.append(objectPayload(id: thumb.id, kind: "thumb", plain: thumb.bytes, cipher: blobs[thumb.id]?.count ?? 0))
                }
            }
            objects.append(objectPayload(id: manifestText, kind: "manifest", plain: manifestPlain.count, cipher: blobs[manifestText]?.count ?? 0))

            status = "正在上传"
            let plans = try await api.reserve(token: session.token, shareID: shareIDText, objects: objects)
            var completed: [[String: Any]] = []
            for plan in plans {
                guard let payload = blobs[plan.id] else { throw APIError.transport("上传计划对不上。") }
                var parts: [[String: Any]] = []
                if !plan.ready {
                    var offset = 0
                    let chunk = plan.partSize ?? payload.count
                    for part in plan.parts {
                        let end: Int
                        if part.partNumber == 0 {
                            end = payload.count
                        } else {
                            end = min(offset + max(chunk, 1), payload.count)
                        }
                        let slice = payload.subdata(in: offset..<end)
                        guard let uploadURL = URL(string: part.url) else { throw APIError.transport("上传地址无效") }
                        let etag = try await api.put(url: uploadURL, data: slice)
                        parts.append(["partNumber": part.partNumber, "etag": etag])
                        if part.partNumber > 0 { offset = end }
                    }
                }
                completed.append(["id": plan.id, "byteSize": payload.count, "parts": parts])
            }
            try await api.complete(
                token: session.token,
                shareID: shareIDText,
                objects: completed,
                manifestID: manifestText,
                nonce: ImgmeshSeal.encode(manifestNonce)
            )
            let claims = items.compactMap { item -> [String: String]? in
                guard let sha = item.sha256 else { return nil }
                return ["sha256": sha, "kind": "shared"]
            }
            try? await api.claim(token: session.token, shareID: shareIDText, access: tag, items: claims)
            let local = LocalShare(
                id: shareIDText,
                masterKey: masterText,
                title: manifest.title,
                role: "sent",
                photoCount: items.count,
                createdAt: Date()
            )
            shares.removeAll { $0.id == local.id }
            shares.insert(local, at: 0)
            ShareLibrary.save(shares)
            link = ShareURL.make(id: shareIDText, masterKey: masterText)
            status = "链接已经准备好。发出去时要带上井号后面的密钥。以后还可以往这条链接里加照片。"
        } catch {
            status = ""
            errorMessage = apiMessage(error)
        }
    }

    func addPhotos(providers: [NSItemProvider], to share: LocalShare) async {
        guard let session else {
            errorMessage = "先登记这台设备的名称。"
            return
        }
        guard session.appleLinked else {
            errorMessage = "先用 Apple 登录，再添加照片。"
            return
        }
        guard !providers.isEmpty else { return }
        busy = true
        errorMessage = ""
        defer { busy = false }
        if gallery.isEmpty {
            await loadGallery(share)
        }
        guard gallery.isEmpty == false || share.photoCount == 0 else {
            errorMessage = "先等原来的照片打开，再添加。"
            return
        }
        do {
            let shareID = try ImgmeshSeal.decode(share.id)
            let master = try ImgmeshSeal.decode(share.masterKey)
            let tag = ImgmeshSeal.encode(try ImgmeshSeal.accessTag(master: master, shareID: shareID))
            let fileKey = try ImgmeshSeal.derive(master: master, shareID: shareID, info: "imgmesh-file-v1")
            var items = gallery.map(\.item)
            guard items.count + providers.count <= 200 else {
                errorMessage = "一条链接最多 200 张。"
                return
            }
            var blobs: [String: Data] = [:]
            var claims: [[String: String]] = []
            for (index, provider) in providers.enumerated() {
                status = "正在加密新增 \(index + 1)/\(providers.count)"
                let photo = try await PhotoIO.load(provider)
                let objectID = try ImgmeshSeal.random(16)
                let nonce = try ImgmeshSeal.random(12)
                let objectText = ImgmeshSeal.encode(objectID)
                let cipher = try ImgmeshSeal.encrypt(
                    key: fileKey,
                    nonce: nonce,
                    aad: try ImgmeshSeal.aad(shareID: shareID, objectID: objectID),
                    plain: photo.data
                )
                blobs[objectText] = cipher
                var thumb: ManifestThumb?
                if let jpeg = PhotoIO.thumbnail(from: photo.data) {
                    let thumbID = try ImgmeshSeal.random(16)
                    let thumbNonce = try ImgmeshSeal.random(12)
                    let thumbText = ImgmeshSeal.encode(thumbID)
                    blobs[thumbText] = try ImgmeshSeal.encrypt(
                        key: fileKey,
                        nonce: thumbNonce,
                        aad: try ImgmeshSeal.aad(shareID: shareID, objectID: thumbID),
                        plain: jpeg
                    )
                    thumb = ManifestThumb(id: thumbText, bytes: jpeg.count, nonce: ImgmeshSeal.encode(thumbNonce))
                }
                let fingerprint = ImgmeshSeal.fingerprint(photo.data)
                claims.append(["sha256": fingerprint, "kind": "shared"])
                items.append(ManifestItem(
                    id: objectText,
                    name: photo.name,
                    mime: photo.mime,
                    bytes: photo.data.count,
                    nonce: ImgmeshSeal.encode(nonce),
                    sha256: fingerprint,
                    thumb: thumb
                ))
            }
            let manifest = Manifest(v: 1, title: share.title, items: items)
            let manifestPlain = try JSONEncoder().encode(manifest)
            let manifestID = try ImgmeshSeal.random(16)
            let manifestNonce = try ImgmeshSeal.random(12)
            let manifestText = ImgmeshSeal.encode(manifestID)
            let manifestKey = try ImgmeshSeal.derive(master: master, shareID: shareID, info: "imgmesh-manifest-v1")
            blobs[manifestText] = try ImgmeshSeal.encrypt(
                key: manifestKey,
                nonce: manifestNonce,
                aad: try ImgmeshSeal.aad(shareID: shareID, objectID: manifestID),
                plain: manifestPlain
            )
            var objects: [[String: Any]] = []
            for item in items.suffix(providers.count) {
                objects.append(objectPayload(id: item.id, kind: "original", plain: item.bytes, cipher: blobs[item.id]?.count ?? 0))
                if let thumb = item.thumb {
                    objects.append(objectPayload(id: thumb.id, kind: "thumb", plain: thumb.bytes, cipher: blobs[thumb.id]?.count ?? 0))
                }
            }
            objects.append(objectPayload(id: manifestText, kind: "manifest", plain: manifestPlain.count, cipher: blobs[manifestText]?.count ?? 0))
            status = "正在上传"
            let plans = try await api.reserve(token: session.token, shareID: share.id, objects: objects)
            var completed: [[String: Any]] = []
            for plan in plans {
                guard let payload = blobs[plan.id] else { throw APIError.transport("上传计划对不上。") }
                var parts: [[String: Any]] = []
                if !plan.ready {
                    var offset = 0
                    let chunk = plan.partSize ?? payload.count
                    for part in plan.parts {
                        let end = part.partNumber == 0 ? payload.count : min(offset + max(chunk, 1), payload.count)
                        let slice = payload.subdata(in: offset..<end)
                        guard let uploadURL = URL(string: part.url) else { throw APIError.transport("上传地址无效") }
                        let etag = try await api.put(url: uploadURL, data: slice)
                        parts.append(["partNumber": part.partNumber, "etag": etag])
                        if part.partNumber > 0 { offset = end }
                    }
                }
                completed.append(["id": plan.id, "byteSize": payload.count, "parts": parts])
            }
            try await api.complete(
                token: session.token,
                shareID: share.id,
                objects: completed,
                manifestID: manifestText,
                nonce: ImgmeshSeal.encode(manifestNonce)
            )
            try? await api.claim(token: session.token, shareID: share.id, access: tag, items: claims)
            status = "已经加进原来的链接。选过自动保存的人再打开时会收到，后来打开的人也可以直接保存。"
            await loadGallery(share)
        } catch {
            status = ""
            errorMessage = apiMessage(error)
        }
    }

    func accept(_ url: URL) {
        guard let parsed = ShareURL.parse(url.absoluteString) else {
            errorMessage = "这个链接里没有密钥。请粘贴完整链接，井号后面的部分也要保留。"
            return
        }
        if !shares.contains(where: { $0.id == parsed.id }) {
            shares.insert(LocalShare(id: parsed.id, masterKey: parsed.masterKey, title: "收到的照片", role: "received", photoCount: 0, createdAt: Date()), at: 0)
            ShareLibrary.save(shares)
        }
        selectedID = parsed.id
        errorMessage = ""
    }

    func accept(text: String) {
        guard let url = URL(string: text.trimmingCharacters(in: .whitespacesAndNewlines)) else {
            errorMessage = "链接格式不对。"
            return
        }
        accept(url)
    }

    func loadGallery(_ share: LocalShare) async {
        gallery = []
        activity = APIClient.ShareActivity(viewed: [], downloaded: [], phones: [])
        status = "正在解密"
        errorMessage = ""
        guard let token = session?.token else {
            errorMessage = "先登记这台设备的名称。"
            return
        }
        do {
            let shareID = try ImgmeshSeal.decode(share.id)
            let master = try ImgmeshSeal.decode(share.masterKey)
            let access = ImgmeshSeal.encode(try ImgmeshSeal.accessTag(master: master, shareID: shareID))
            let listed = try await api.manifest(token: token, shareID: share.id, access: access)
            let manifestKey = try ImgmeshSeal.derive(master: master, shareID: shareID, info: "imgmesh-manifest-v1")
            let manifestCipher = try await api.download(listed.url)
            let plain = try ImgmeshSeal.decrypt(
                key: manifestKey,
                nonce: try ImgmeshSeal.decode(listed.nonce),
                aad: try ImgmeshSeal.aad(shareID: shareID, objectID: try ImgmeshSeal.decode(listed.id)),
                ciphertext: manifestCipher
            )
            let manifest = try JSONDecoder().decode(Manifest.self, from: plain)
            let fileKey = try ImgmeshSeal.derive(master: master, shareID: shareID, info: "imgmesh-file-v1")
            var photos: [GalleryPhoto] = []
            for item in manifest.items {
                var preview: Data?
                if let thumb = item.thumb {
                    let cipher = try await api.download(try await api.objectURL(token: token, shareID: share.id, objectID: thumb.id, access: access))
                    preview = try? ImgmeshSeal.decrypt(
                        key: fileKey,
                        nonce: try ImgmeshSeal.decode(thumb.nonce),
                        aad: try ImgmeshSeal.aad(shareID: shareID, objectID: try ImgmeshSeal.decode(thumb.id)),
                        ciphertext: cipher
                    )
                }
                photos.append(GalleryPhoto(id: item.id, name: item.name ?? "照片", mime: item.mime, preview: preview, original: nil, item: item))
            }
            gallery = photos
            if let index = shares.firstIndex(where: { $0.id == share.id }) {
                shares[index].photoCount = photos.count
                shares[index].title = manifest.title
                ShareLibrary.save(shares)
            }
            status = "\(photos.count) 张照片"
            try? await api.record(token: token, shareID: share.id, access: access, kind: "view")
            if share.role == "sent" {
                activity = (try? await api.activity(token: token, shareID: share.id)) ?? activity
            }
            await loadCards(token: token, photos: photos)
            await absorbNewPhotos(share)
        } catch {
            status = ""
            errorMessage = apiMessage(error)
        }
    }

    func saveGallery(_ share: LocalShare) async {
        busy = true
        defer { busy = false }
        do {
            let images = try await originals(share)
            try await PhotoIO.saveToLibrary(images)
            let shareID = try ImgmeshSeal.decode(share.id)
            let master = try ImgmeshSeal.decode(share.masterKey)
            let access = ImgmeshSeal.encode(try ImgmeshSeal.accessTag(master: master, shareID: shareID))
            var claims: [[String: String]] = []
            for index in images.indices {
                let sha = gallery[index].item.sha256?.isEmpty == false ? gallery[index].item.sha256! : ImgmeshSeal.fingerprint(images[index])
                gallery[index].item.sha256 = sha
                claims.append(["sha256": sha, "kind": "saved"])
            }
            if let token = session?.token {
                try? await api.record(token: token, shareID: share.id, access: access, kind: "save")
                try? await api.claim(token: token, shareID: share.id, access: access, items: claims)
                try? await api.follow(token: token, shareID: share.id, access: access, on: true)
                await loadCards(token: token, photos: gallery)
            }
            if let index = shares.firstIndex(where: { $0.id == share.id }) {
                shares[index].autoSave = true
                shares[index].savedIDs = gallery.map(\.id)
                ShareLibrary.save(shares)
            }
            status = "已经存进相册。以后这条链接再增加照片，打开时会自动保存。"
            errorMessage = ""
        } catch {
            errorMessage = apiMessage(error)
        }
    }

    func remove(_ share: LocalShare) async {
        if let shareID = try? ImgmeshSeal.decode(share.id), let master = try? ImgmeshSeal.decode(share.masterKey),
           let access = try? ImgmeshSeal.encode(ImgmeshSeal.accessTag(master: master, shareID: shareID)) {
            if let token = session?.token {
                try? await api.record(token: token, shareID: share.id, access: access, kind: "remove")
            }
        }
        shares.removeAll { $0.id == share.id }
        ShareLibrary.save(shares)
        if selectedID == share.id { selectedID = nil }
    }

    func revoke(_ share: LocalShare) async {
        guard let token = session?.token else {
            errorMessage = "先登录再撤销。"
            return
        }
        do {
            try await api.revoke(token: token, shareID: share.id)
            await remove(share)
            status = "链接已经失效。对方相册里已经保存的照片还在。"
        } catch {
            errorMessage = apiMessage(error)
        }
    }

    func setAutoSave(_ share: LocalShare, _ on: Bool) async {
        guard let token = session?.token else {
            errorMessage = "先登记这台设备的名称。"
            return
        }
        do {
            let shareID = try ImgmeshSeal.decode(share.id)
            let master = try ImgmeshSeal.decode(share.masterKey)
            let access = ImgmeshSeal.encode(try ImgmeshSeal.accessTag(master: master, shareID: shareID))
            try await api.follow(token: token, shareID: share.id, access: access, on: on)
            guard let index = shares.firstIndex(where: { $0.id == share.id }) else { return }
            shares[index].autoSave = on
            ShareLibrary.save(shares)
            status = on ? "已打开自动保存。以后新增的照片会在打开时存进相册。" : "已关闭自动保存。"
            errorMessage = ""
            if on { await absorbNewPhotos(shares[index]) }
        } catch {
            errorMessage = apiMessage(error)
        }
    }

    private func absorbNewPhotos(_ share: LocalShare) async {
        guard share.autoSave, let index = shares.firstIndex(where: { $0.id == share.id }) else { return }
        let known = Set(shares[index].savedIDs)
        let fresh = gallery.filter { !known.contains($0.id) }
        guard !fresh.isEmpty else { return }
        do {
            let images = try await originals(share, items: fresh.map(\.item))
            try await PhotoIO.saveToLibrary(images)
            shares[index].savedIDs.append(contentsOf: fresh.map(\.id))
            ShareLibrary.save(shares)
            let shareID = try ImgmeshSeal.decode(share.id)
            let master = try ImgmeshSeal.decode(share.masterKey)
            let access = ImgmeshSeal.encode(try ImgmeshSeal.accessTag(master: master, shareID: shareID))
            if let token = session?.token {
                var claims: [[String: String]] = []
                for (offset, photo) in fresh.enumerated() {
                    let sha = photo.item.sha256?.isEmpty == false ? photo.item.sha256! : ImgmeshSeal.fingerprint(images[offset])
                    claims.append(["sha256": sha, "kind": "saved"])
                }
                try? await api.record(token: token, shareID: share.id, access: access, kind: "save")
                try? await api.claim(token: token, shareID: share.id, access: access, items: claims)
            }
            status = "新的 \(fresh.count) 张已经自动存进相册。"
            errorMessage = ""
        } catch {
            errorMessage = apiMessage(error)
        }
    }

    private func loadCards(token: String, photos: [GalleryPhoto]) async {
        let ids = Array(Set(photos.compactMap(\.item.sha256).filter { !$0.isEmpty }))
        guard !ids.isEmpty else { return }
        guard let list = try? await api.photoCards(token: token, ids: ids) else { return }
        var next: [String: APIClient.PhotoCard] = [:]
        for card in list { next[card.id] = card }
        cards = next
    }

    private func originals(_ share: LocalShare, items: [ManifestItem]? = nil) async throws -> [Data] {
        guard let token = session?.token else { throw APIError.transport("先登记这台设备的名称。") }
        let shareID = try ImgmeshSeal.decode(share.id)
        let master = try ImgmeshSeal.decode(share.masterKey)
        let access = ImgmeshSeal.encode(try ImgmeshSeal.accessTag(master: master, shareID: shareID))
        let fileKey = try ImgmeshSeal.derive(master: master, shareID: shareID, info: "imgmesh-file-v1")
        let chosen = items ?? gallery.map(\.item)
        var images: [Data] = []
        for item in chosen {
            let cipher = try await api.download(try await api.objectURL(token: token, shareID: share.id, objectID: item.id, access: access))
            images.append(try ImgmeshSeal.decrypt(
                key: fileKey,
                nonce: try ImgmeshSeal.decode(item.nonce),
                aad: try ImgmeshSeal.aad(shareID: shareID, objectID: try ImgmeshSeal.decode(item.id)),
                ciphertext: cipher
            ))
        }
        return images
    }

    private func objectPayload(id: String, kind: String, plain: Int, cipher: Int) -> [String: Any] {
        ["id": id, "kind": kind, "plaintextBytes": plain, "ciphertextBytes": cipher]
    }
}
