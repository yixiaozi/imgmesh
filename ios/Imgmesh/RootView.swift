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
                        TextField("改成新名称", text: $model.draftName)
                        Button("保存名称") { Task { await model.rename(to: model.draftName) } }
                            .disabled(model.draftName.trimmingCharacters(in: .whitespaces).isEmpty)
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

    var body: some View {
        List {
            Section {
                if model.gallery.isEmpty {
                    Text(model.status.isEmpty ? "正在打开" : model.status)
                } else {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 96), spacing: 8)], spacing: 8) {
                        ForEach(model.gallery) { photo in
                            photoCell(photo)
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
    }

    @ViewBuilder
    private func photoCell(_ photo: AppModel.GalleryPhoto) -> some View {
        if let preview = photo.preview, let image = UIImage(data: preview) {
            Image(uiImage: image).resizable().scaledToFill().frame(height: 96).clipped().cornerRadius(8)
        } else {
            ZStack {
                RoundedRectangle(cornerRadius: 8).fill(Color(red: 0.90, green: 0.93, blue: 0.92))
                Text(photo.mime == "image/heic" ? "HEIC" : "照片").font(.caption)
            }
            .frame(height: 96)
        }
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
