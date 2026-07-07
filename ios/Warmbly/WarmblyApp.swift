import SwiftUI

@main
struct WarmblyApp: App {
    @State private var env = AppEnvironment()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(env)
                .tint(WTheme.accent)
        }
    }
}
