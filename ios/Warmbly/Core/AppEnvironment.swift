import Foundation

/// Live tab-badge counts, updated by feature stores.
@MainActor
@Observable
final class AppBadges {
    var uniboxUnread: Int = 0
}

/// Root object graph, injected once via `.environment(...)`.
@MainActor
@Observable
final class AppEnvironment {
    let api: APIClient
    let session: SessionStore
    let realtime: RealtimeService
    let badges = AppBadges()

    init() {
        let api = APIClient()
        self.api = api
        session = SessionStore(api: api)
        realtime = RealtimeService(api: api)
        PushManager.shared.configure(api: api)

        session.onReady = { [weak self] userID, orgID in
            self?.realtime.connect(userID: userID, orgID: orgID)
            PushManager.shared.sessionReady()
        }
        session.onLoggedOut = { [weak self] in
            self?.realtime.disconnect()
            self?.badges.uniboxUnread = 0
            PushManager.shared.sessionEnded()
        }
    }
}
