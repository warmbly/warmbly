import Foundation

/// Server configuration, persisted in UserDefaults (thread-safe) so it can be
/// read from any isolation domain without shared mutable state. Values are
/// editable from the login screen's connection sheet.
enum AppConfig {
    /// Shipped builds default to the hosted SaaS API; debug builds default to
    /// the local dev backend. Self-hosters override the origin in-app, so one
    /// binary serves the SaaS, a self-hosted instance, or a local stack.
    #if DEBUG
    static let defaultOrigin = "http://localhost:8080"
    static let defaultTurnstileToken = "warmbly-local-turnstile-bypass"
    #else
    static let defaultOrigin = "https://api.warmbly.com"
    static let defaultTurnstileToken = ""
    #endif

    /// Bare origin, no path. The `/v1` prefix is applied in APIClient,
    /// mirroring the web's `API_BASE_URL = ${API_URL}/v1`.
    static var serverOrigin: String {
        get { UserDefaults.standard.string(forKey: "server_origin") ?? defaultOrigin }
        set { UserDefaults.standard.set(newValue, forKey: "server_origin") }
    }

    /// Turnstile value sent on captcha-gated auth calls. In dev the backend
    /// accepts the literal bypass token; production needs a real widget token.
    static var turnstileToken: String {
        get { UserDefaults.standard.string(forKey: "turnstile_token") ?? defaultTurnstileToken }
        set { UserDefaults.standard.set(newValue, forKey: "turnstile_token") }
    }

    /// Optional realtime origin override (advanced). Normally the backend's
    /// `/getaway` bootstrap decides where the websocket connects; this swaps
    /// only the scheme/host/port of that URL for split-service self-hosting.
    static var realtimeOriginOverride: String {
        get { UserDefaults.standard.string(forKey: "realtime_origin_override") ?? "" }
        set { UserDefaults.standard.set(newValue, forKey: "realtime_origin_override") }
    }

    /// The org the user last worked in, re-selected on next launch.
    static var lastOrganizationID: String? {
        get { UserDefaults.standard.string(forKey: "last_org_id") }
        set { UserDefaults.standard.set(newValue, forKey: "last_org_id") }
    }

    static var apiBaseURL: URL? {
        URL(string: serverOrigin.trimmingCharacters(in: .whitespaces))?.appending(path: "v1")
    }

    /// Cleans a user-typed origin: trims, strips trailing slashes, and adds
    /// the missing scheme (http for clearly-local hosts like localhost, LAN
    /// IPs, and .local names; https for everything else).
    static func normalizeOrigin(_ raw: String) -> String {
        var value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        while value.hasSuffix("/") { value.removeLast() }
        guard !value.isEmpty, !value.contains("://") else { return value }
        let host = value.split(separator: "/").first?.split(separator: ":").first.map { $0.lowercased() } ?? ""
        let isLocal = host == "localhost" || host.hasSuffix(".local")
            || (!host.isEmpty && host.allSatisfy { $0.isNumber || $0 == "." })
        return (isLocal ? "http://" : "https://") + value
    }
}
