import SwiftUI

@main
struct WarmblyApp: App {
    @UIApplicationDelegateAdaptor(PushAppDelegate.self) private var pushDelegate
    @State private var env = AppEnvironment()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(env)
                .tint(WTheme.accent)
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { env.realtime.nudge() }
        }
    }
}
