import SwiftUI
import UIKit
import UserNotifications
import os

/// APNs registration + device-token sync with the backend.
///
/// The OS permission prompt fires once after login; the resulting token is
/// POSTed to `auth/me/device-tokens` (an upsert, safe to repeat every launch)
/// and DELETEd on logout so a shared device stops receiving the previous
/// account's pushes. Batching lives server-side (first event pushes
/// immediately, the rest digest at the window edge), so the client stays
/// simple: register, unregister, mirror the unread badge.
@MainActor
@Observable
final class PushManager: NSObject {
    static let shared = PushManager()

    private(set) var authorization: UNAuthorizationStatus = .notDetermined

    private var api: APIClient?
    private let log = Logger(subsystem: "com.warmbly.app", category: "push")

    private var storedToken: String {
        get { UserDefaults.standard.string(forKey: "push.deviceToken") ?? "" }
        set { UserDefaults.standard.set(newValue, forKey: "push.deviceToken") }
    }

    /// Debug builds carry the sandbox aps-environment; the backend routes the
    /// token to the matching APNs host.
    private var apnsEnvironment: String {
        #if DEBUG
        "development"
        #else
        "production"
        #endif
    }

    func configure(api: APIClient) {
        self.api = api
    }

    /// Call once a session is live: asks for permission (first time only) and
    /// (re)registers with APNs. Server-side registration is an upsert.
    func sessionReady() {
        Task { await requestAndRegister() }
    }

    func refreshAuthorization() async {
        authorization = await UNUserNotificationCenter.current().notificationSettings().authorizationStatus
    }

    private func requestAndRegister() async {
        let center = UNUserNotificationCenter.current()
        var status = await center.notificationSettings().authorizationStatus
        if status == .notDetermined {
            let granted = (try? await center.requestAuthorization(options: [.alert, .badge, .sound])) ?? false
            status = granted ? .authorized : .denied
        }
        authorization = status
        guard status == .authorized || status == .provisional || status == .ephemeral else { return }
        UIApplication.shared.registerForRemoteNotifications()
    }

    /// Sign-out: the backend forgets this device, then local state clears.
    func sessionEnded() {
        let token = storedToken
        storedToken = ""
        guard !token.isEmpty, let api else { return }
        Task {
            let _: MoreOkResponse? = try? await api.delete("auth/me/device-tokens/\(token)")
        }
    }

    /// Mirror the server-tracked unread feed count onto the app icon.
    func setBadge(_ count: Int) {
        UNUserNotificationCenter.current().setBadgeCount(count, withCompletionHandler: nil)
    }

    // MARK: AppDelegate entry points

    func didRegister(deviceToken: Data) {
        let token = deviceToken.map { String(format: "%02x", $0) }.joined()
        storedToken = token
        guard let api else { return }
        let environment = apnsEnvironment
        Task {
            struct Body: Encodable {
                let token: String
                let platform: String
                let environment: String
            }
            let _: MoreOkResponse? = try? await api.post(
                "auth/me/device-tokens",
                body: Body(token: token, platform: "ios", environment: environment)
            )
        }
    }

    func didFailToRegister(_ error: Error) {
        log.warning("APNs registration failed: \(error.localizedDescription)")
    }
}

extension PushManager: UNUserNotificationCenterDelegate {
    /// Foreground pushes stay quiet — realtime already updates the feed and
    /// badge counts in-app; only the icon badge is applied.
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.badge])
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        completionHandler()
    }
}

/// Bridges the UIKit launch + APNs callbacks into PushManager.
final class PushAppDelegate: NSObject, UIApplicationDelegate {
    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = PushManager.shared
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        Task { @MainActor in PushManager.shared.didRegister(deviceToken: deviceToken) }
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        Task { @MainActor in PushManager.shared.didFailToRegister(error) }
    }
}
