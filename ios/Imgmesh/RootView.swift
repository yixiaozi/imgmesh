import AuthenticationServices
import SwiftUI
import UIKit

struct RootView: View {
    @EnvironmentObject private var model: AppModel
    @State private var title = ""
    @State private var showPicker = false
    @State private var pasted = ""
    @State private var path: [String] = []

    var body: some View {
        NavigationStack(path: $path) {
            List {
                Section("这台设备") {
                    if model.session == nil {
                        Text("每台设备只能注册一次，名称不能和别人重复。")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        TextField("名称，例如小林", text: $model.draftName)
                        Button("注册") { Task { await model.register() } }
                            .disabled(model.draftName.trimmingCharacters(in: .whitespaces).isEmpty)
                    } else {
                        Text(model.session?.displayName ?? "")
                        TextField("名称", text: $model.draftName)
                        TextField("介绍", text: $model.bio)
                        TextField("城市", text: $model.city)
                        TextField("联系方式", text: $model.contact)
                        Button("保存资料") { Task { await model.saveProfile() } }
                        if model.session?.appleLinked == true {
                            Text("已连接 Apple，可以分享和撤销。")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        } else {
                            SignInWithAppleButton(.signIn) { request in
                                request.requestedScopes = [.fullName]
                            } onCompletion: { result in
                                if case .success(let auth) = result, let credential = auth.credential as? ASAuthorizationAppleIDCredential {
                                    Task { await model.signIn(credential) }
                                }
                            }
                            .frame(height: 44)
                            .listRowInsets(EdgeInsets(top: 8, leading: 0, bottom: 8, trailing: 0))
                            Text("分享和撤销需要 Apple 登录。查看和保存只用上面的名称。")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                Section("找人") {
                    TextField("名称、城市或联系方式", text: $model.peopleQuery)
                    Button("查找") { Task { await model.findPeople() } }
                        .disabled(model.session == nil || model.peopleQuery.trimmingCharacters(in: .whitespaces).isEmpty)
                    ForEach(model.people) { person in
                        VStack(alignment: .leading, spacing: 2) {
                            Text(person.displayName)
                            Text("\(person.city.isEmpty ? "未填城市" : person.city) · \(person.contact.isEmpty ? "未填联系方式" : person.contact)")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                            Text(person.bio.isEmpty ? "还没有介绍" : person.bio)
                                .font(.footnote)
                            Text("分享了 \(person.sharedCount) 张，保存了 \(person.savedCount) 张")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                Section("分享") {
                    TextField("标题，例如三亚", text: $title)
                    Button("选择照片") { showPicker = true }
                        .disabled(model.busy || model.session == nil)
                    if model.busy { ProgressView(model.status.isEmpty ? "正在处理" : model.status) }
                    if !model.link.isEmpty {
                        Text(model.link).font(.footnote).textSelection(.enabled)
                        ShareLink(item: URL(string: model.link)!) { Label("发出链接", systemImage: "square.and.arrow.up") }
                        Button("复制链接") {
                            UIPasteboard.general.string = model.link
                            model.status = "已复制。发给对方时保留井号后的密钥。"
                        }
                    }
                }
                Section("打开一份分享") {
                    TextField("粘贴完整链接", text: $pasted)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Button("打开") { model.accept(text: pasted) }
                        .disabled(pasted.trimmingCharacters(in: .whitespaces).isEmpty)
                }
                Section("我的链接") {
                    if model.shares.isEmpty {
                        Text("还没有分享。").foregroundStyle(.secondary)
                    }
                    ForEach(model.shares) { share in
                        NavigationLink(share.title, value: share.id)
                            .badge(share.role == "sent" ? "发出" : "收到")
                    }
                }
            }
            .navigationTitle("imgmesh")
            .navigationDestination(for: String.self) { id in
                if let share = model.shares.first(where: { $0.id == id }) {
                    GalleryView(share: share)
                }
            }
            .task { await model.restore() }
            .overlay(alignment: .bottom) {
                if !model.errorMessage.isEmpty {
                    Text(model.errorMessage)
                        .font(.footnote)
                        .padding(12)
                        .frame(maxWidth: .infinity)
                        .background(.thinMaterial)
                }
            }
            .safeAreaInset(edge: .bottom) {
                if !model.status.isEmpty && model.link.isEmpty {
                    Text(model.status).font(.footnote).padding(.bottom, 8)
                }
            }
        }
        .sheet(isPresented: $showPicker) {
            PhotoPicker { providers in
                showPicker = false
                Task { await model.createShare(providers: providers, title: title) }
            }
            .ignoresSafeArea()
        }
        .onChange(of: path) { _, newPath in
            model.selectedID = newPath.last
        }
        .onChange(of: model.selectedID) { _, id in
            guard let id else { return }
            if path.last != id { path.append(id) }
        }
    }
}

struct GalleryView: View {
    @EnvironmentObject private var model: AppModel
    let share: LocalShare
    @State private var openID: String?

    var body: some View {
        List {
            Section {
                if model.gallery.isEmpty {
                    Text(model.status.isEmpty ? "正在打开" : model.status)
                } else {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 96), spacing: 8)], spacing: 8) {
                        ForEach(model.gallery) { photo in
                            Button {
                                openID = photo.id
                            } label: {
                                photoCell(photo)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    .padding(.vertical, 4)
                }
            }
            Section {
                Button("保存到相册") { Task { await model.saveGallery(share) } }
                    .disabled(model.gallery.isEmpty || model.busy)
                Button("从 imgmesh 移除", role: .destructive) { Task { await model.remove(share) } }
                if share.role == "sent" {
                    Button("撤销链接", role: .destructive) { Task { await model.revoke(share) } }
                }
            }
            if share.role == "sent" {
                activitySection("谁查看过", model.activity.viewed)
                activitySection("谁下载过", model.activity.downloaded)
                activitySection("在这些手机里", model.activity.phones)
            }
        }
        .navigationTitle(share.title)
        .task { await model.loadGallery(share) }
        .sheet(isPresented: Binding(get: { openID != nil }, set: { if !$0 { openID = nil } })) {
            if let openID, let photo = model.gallery.first(where: { $0.id == openID }) {
                PhotoBrowser(photo: photo, share: share)
            }
        }
    }

    @ViewBuilder
    private func photoCell(_ photo: AppModel.GalleryPhoto) -> some View {
        ZStack(alignment: .bottomLeading) {
            if let preview = photo.preview, let image = UIImage(data: preview) {
                Image(uiImage: image).resizable().scaledToFill().frame(height: 110).clipped()
            } else {
                ZStack {
                    Rectangle().fill(Color(red: 0.90, green: 0.93, blue: 0.92))
                    Text(photo.mime == "image/heic" ? "HEIC" : "照片").font(.caption)
                }
                .frame(height: 110)
            }
            Text(tileLabel(photo))
                .font(.caption2)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Color(red: 0.05, green: 0.42, blue: 0.43), in: Capsule())
                .foregroundStyle(.white)
                .padding(6)
        }
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private func tileLabel(_ photo: AppModel.GalleryPhoto) -> String {
        guard let sha = photo.item.sha256, let card = model.cards[sha] else { return "照片" }
        let count = card.holders.count
        if !card.sharedBy.isEmpty { return count > 0 ? "已分享 · \(count) 人有" : "已分享" }
        return count > 0 ? "\(count) 人有" : "未分享"
    }

    @ViewBuilder
    private func activitySection(_ title: String, _ rows: [APIClient.ActivityRow]) -> some View {
        Section(title) {
            if rows.isEmpty {
                Text("还没有记录。").foregroundStyle(.secondary)
            }
            ForEach(rows) { row in
                VStack(alignment: .leading) {
                    Text(row.displayName.isEmpty ? "未命名" : row.displayName)
                    Text(activityDetail(row))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private func activityDetail(_ row: APIClient.ActivityRow) -> String {
        let place = row.client == "ios" ? "iPhone" : "浏览器"
        if let savedAt = row.savedAt, !savedAt.isEmpty {
            return "\(place) · 已存进相册"
        }
        if let count = row.count {
            return "\(place) · \(count) 次"
        }
        return place
    }
}

struct PhotoBrowser: View {
    @EnvironmentObject private var model: AppModel
    let photo: AppModel.GalleryPhoto
    let share: LocalShare
    @State private var index = 0

    private var current: AppModel.GalleryPhoto {
        model.gallery.indices.contains(index) ? model.gallery[index] : photo
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                if let preview = current.preview, let image = UIImage(data: preview) {
                    Image(uiImage: image)
                        .resizable()
                        .scaledToFit()
                        .frame(maxHeight: 360)
                        .clipShape(RoundedRectangle(cornerRadius: 12))
                        .padding(.horizontal)
                }
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(Array(model.gallery.enumerated()), id: \.element.id) { offset, item in
                            Button {
                                index = offset
                            } label: {
                                thumb(item)
                                    .overlay(RoundedRectangle(cornerRadius: 6).stroke(offset == index ? Color(red: 0.05, green: 0.42, blue: 0.43) : .clear, lineWidth: 2))
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    .padding(.horizontal)
                }
                identity(current)
                    .padding()
                HStack {
                    Button("上一张") { move(-1) }
                    Button("下一张") { move(1) }
                    Button("保存到相册") { Task { await model.saveGallery(share) } }
                }
                .padding(.bottom)
            }
            .navigationTitle(current.item.sha256.map(shortID) ?? "照片")
            .navigationBarTitleDisplayMode(.inline)
        }
        .onAppear {
            index = model.gallery.firstIndex(where: { $0.id == photo.id }) ?? 0
        }
    }

    private func move(_ delta: Int) {
        let count = model.gallery.count
        guard count > 0 else { return }
        index = (index + delta + count) % count
    }

    @ViewBuilder
    private func thumb(_ item: AppModel.GalleryPhoto) -> some View {
        if let preview = item.preview, let image = UIImage(data: preview) {
            Image(uiImage: image).resizable().scaledToFill().frame(width: 64, height: 64).clipped().clipShape(RoundedRectangle(cornerRadius: 6))
        } else {
            RoundedRectangle(cornerRadius: 6).fill(Color(red: 0.90, green: 0.93, blue: 0.92)).frame(width: 64, height: 64)
        }
    }

    @ViewBuilder
    private func identity(_ item: AppModel.GalleryPhoto) -> some View {
        let card = item.item.sha256.flatMap { model.cards[$0] }
        VStack(alignment: .leading, spacing: 8) {
            Text("身份证").font(.title2)
            if let sha = item.item.sha256 {
                Text(shortID(sha)).font(.title3)
                Text(sha).font(.caption2).foregroundStyle(.secondary).textSelection(.enabled)
            } else {
                Text("保存到相册后会生成身份证。").foregroundStyle(.secondary)
            }
            if let card {
                Text(card.sharedBy.isEmpty ? "还没分享" : "已分享")
                Text("谁分享了").font(.headline)
                Text(card.sharedBy.isEmpty ? "还没有人分享。" : card.sharedBy.map(\.displayName).joined(separator: "、"))
                Text("谁有这张").font(.headline)
                ForEach(card.holders) { holder in
                    Text(holderLine(holder))
                }
                Text("谁保存了").font(.headline)
                if card.savers.isEmpty {
                    Text("还没有人保存。").foregroundStyle(.secondary)
                }
                ForEach(card.savers) { saver in
                    Text(saverLine(saver))
                }
            } else {
                Text("这张照片还没有身份证记录。").foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func holderLine(_ holder: APIClient.PhotoHolder) -> String {
        let action = holder.saved == true ? "保存了" : "分享了"
        if holder.saved == true, let from = holder.from, from.id != holder.id {
            return "\(holder.displayName) \(action)，保存自 \(from.displayName)"
        }
        return "\(holder.displayName) \(action)"
    }

    private func saverLine(_ saver: APIClient.PhotoHolder) -> String {
        if let from = saver.from, from.id != saver.id {
            return "\(saver.displayName)，保存自 \(from.displayName)"
        }
        return "\(saver.displayName)，自己留下的"
    }
}

private func shortID(_ id: String) -> String {
    let head = id.prefix(8)
    let split = head.index(head.startIndex, offsetBy: min(4, head.count))
    return "IM-\(head[..<split]) \(head[split...])"
}
