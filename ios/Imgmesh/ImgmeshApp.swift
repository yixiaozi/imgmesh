import SwiftUI

@main
struct ImgmeshApp: App {
    @StateObject private var model = AppModel()

    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(model)
                .onOpenURL { model.accept($0) }
                .onChange(of: scenePhase) { _, phase in
                    if phase == .active {
                        Task { await model.refreshFeed() }
                    }
                }
                .onContinueUserActivity(NSUserActivityTypeBrowsingWeb) { activity in
                    if let url = activity.webpageURL {
                        model.accept(url)
                    }
                }
        }
    }
}
