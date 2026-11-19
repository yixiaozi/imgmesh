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
                Section("分享") {
                    if model.session == nil {
                        SignInWithAppleButton(.signIn) { request in
                            request.requestedScopes = [.fullName]
                        } onCompletion: { result in
                            if case .success(let auth) = result, let credential = auth.credential as? ASAuthorizationAppleIDCredential {
                                Task { await model.signIn(credential) }
                            }
                        }
                        .frame(height: 44)
                        .listRowInsets(EdgeInsets(top: 8, leading: 0, bottom: 8, trailing: 0))
                    } else {
                        Text(model.session?.displayName.isEmpty == false ? model.session!.displayName : "已登录")
                        Button("退出登录", role: .destructive) { model.signOut() }
                    }
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
            if share.role == "sent" && !model.events.isEmpty {
                Section("谁打开过") {
                    ForEach(model.events) { event in
                        VStack(alignment: .leading) {
                            Text(eventLabel(event.kind))
                            Text([event.displayName, event.client, event.createdAt].filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
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

    private func eventLabel(_ kind: String) -> String {
        switch kind {
        case "open": return "打开了"
        case "save": return "存进了相册"
        case "remove": return "从 imgmesh 移除了"
        default: return kind
        }
    }
}
