import Foundation

enum SessionPhase: Equatable {
    case launching
    case loggedOut
    /// A stored session exists but the backend can't be reached: keep the
    /// login and retry instead of bouncing the user to sign-in.
    case unreachable
    case onboarding
    case selectOrg
    case ready
}

enum LoginOutcome {
    case loggedIn
    case needsTwoFA(pendingToken: String)
    /// A code was emailed and must be confirmed with `confirmLogin`. Not every
    /// deployment sends one: self-hosted installs default to signing in without
    /// it, so a login can resolve on the first call.
    case needsCode(session: String)
    /// The password was an issued initial one; `chooseInitialPassword` replaces it and continues.
    case needsNewPassword(pendingToken: String)
}

/// A federated sign-in on an address that already has a password: nothing is
/// linked until that password reaches `confirmSSOLink`.
struct SSOLinkChallenge: Equatable, Sendable {
    var pendingToken: String
    var email: String
    var provider: String
}

enum SocialOutcome {
    case loggedIn
    case needsTwoFA(pendingToken: String)
    case needsLink(SSOLinkChallenge)
}

/// Owns the auth lifecycle and the active-organization context.
///
/// The active org is server-side session state (`sessions.current_organization_id`),
/// so this store re-POSTs `/organization/switch/:id` once per launch before
/// entering `.ready`, mirroring the web's OrgGate.
@MainActor
@Observable
final class SessionStore {
    let api: APIClient
    private let appAttest: AppAttestClient

    private(set) var phase: SessionPhase = .launching
    /// True when the last sign-out was forced by an expired/revoked session
    /// (not a manual logout); the auth screen shows a notice explaining it.
    private(set) var wasSignedOutBySession = false
    private(set) var user: User?
    private(set) var memberships: [OrganizationMember] = []
    private(set) var currentOrgID: String?
    private(set) var currentOrg: OrganizationCurrent?
    private(set) var permissions: OrgPermissions = []
    private(set) var role: String?

    var onReady: ((_ userID: String, _ orgID: String) -> Void)?
    var onLoggedOut: (() -> Void)?

    init(api: APIClient) {
        self.api = api
        self.appAttest = AppAttestClient(api: api)
        // The outer task is one-shot registration (strong self is fine); the
        // stored handler captures weakly to avoid an APIClient<->store cycle.
        Task { [api, self] in
            await api.onAuthFailure { [weak self] in
                guard let self else { return }
                Task { @MainActor in self.handleAuthFailure() }
            }
        }
    }

    var isOwner: Bool {
        guard let user, let currentOrg else { return false }
        return currentOrg.ownerUserID == user.id
    }

    func can(_ permission: OrgPermissions) -> Bool {
        isOwner || permissions.contains(permission)
    }

    // MARK: - Launch

    func bootstrap() async {
        #if DEBUG
        // Scripted runs can open a signed-out or first-run screen; the stored session is left alone.
        switch ProcessInfo.processInfo.environment["WARMBLY_DEBUG_PHASE"] {
        case "launching":
            return
        case "signedout":
            phase = .loggedOut
            return
        case "onboarding":
            try? await loadIdentity()
            phase = .onboarding
            return
        case "workspace":
            try? await loadIdentity()
            memberships = []
            phase = .selectOrg
            return
        default:
            break
        }
        #endif
        guard await api.hasToken else {
            phase = .loggedOut
            return
        }
        do {
            try await loadIdentity()
            await enterApp()
        } catch let error as APIError {
            if case .unauthorized = error {
                // The token really is dead; only then drop to sign-in. The
                // user believed they were signed in, so say why they aren't.
                wasSignedOutBySession = true
                await clearSession()
            } else {
                phase = .unreachable
            }
        } catch {
            phase = .unreachable
        }
    }

    private func loadIdentity() async throws {
        user = try await api.get("auth/me")
        let orgs: ListResponse<OrganizationMember> = try await api.get("organization")
        memberships = orgs.data.filter { $0.organization != nil }
    }

    /// Reloads `/auth/me` (profile edits, label group changes).
    func refreshUser() async {
        user = (try? await api.get("auth/me")) ?? user
    }

    /// Reloads `/organization/current` (limits, counts, presence flags).
    func refreshCurrentOrg() async {
        guard phase == .ready else { return }
        currentOrg = (try? await api.get("organization/current")) ?? currentOrg
    }

    /// Post-identity routing: the onboarding questionnaire gates the app
    /// (mirrors the web's /onboarding redirect), then org resolution.
    private func enterApp() async {
        if user?.onboardingCompletedAt == nil {
            phase = .onboarding
            return
        }
        await resolveOrganization()
    }

    /// Submits the onboarding questionnaire, then continues into the app.
    func completeOnboarding(firstName: String, lastName: String, referralSource: String, role: String?, teamSize: String?) async throws {
        var body: [String: String] = [
            "first_name": firstName,
            "last_name": lastName,
            "referral_source": referralSource,
        ]
        if let role { body["role"] = role }
        if let teamSize { body["team_size"] = teamSize }
        let _: EmptyBody = try await api.patch("auth/me/onboarding", body: body)
        await refreshUser()
        await resolveOrganization()
    }

    private func resolveOrganization() async {
        let persisted = AppConfig.lastOrganizationID
        let candidate = memberships.first { $0.organizationID == persisted }
            ?? (memberships.count == 1 ? memberships.first : nil)
        if let candidate {
            do {
                try await selectOrganization(candidate.organizationID)
                return
            } catch {
                // fall through to the picker
            }
        }
        // Zero orgs -> create screen; several -> picker. Both render as .selectOrg.
        phase = .selectOrg
    }

    // MARK: - Org switching

    func selectOrganization(_ orgID: String) async throws {
        let _: SwitchOrgResponse = try await api.post("organization/switch/\(orgID)")
        currentOrgID = orgID
        AppConfig.lastOrganizationID = orgID
        if let membership = memberships.first(where: { $0.organizationID == orgID }) {
            permissions = membership.orgPermissions
            role = membership.role
        }
        currentOrg = try? await api.get("organization/current")
        phase = .ready
        if let user {
            onReady?(user.id, orgID)
        }
    }

    /// Switch initiated from inside the app (workspace switcher).
    func switchOrganization(_ orgID: String) async throws {
        guard orgID != currentOrgID else { return }
        try await selectOrganization(orgID)
    }

    func createOrganization(name: String) async throws {
        let org: Organization = try await api.post("organization", body: ["name": name])
        try await loadIdentity()
        try await selectOrganization(org.id)
    }

    // MARK: - Login flow

    func startLogin(email: String, password: String) async throws -> LoginOutcome {
        let result: AuthSession = try await verified(.login) { check in
            try await self.api.post("auth/login", body: PasswordAuthBody(email: email, password: password, check: check, supportsPasswordChange: true), authorized: false)
        }
        return try await resolve(result)
    }

    /// Replaces an issued initial password with the holder's own, then continues the sign-in.
    func chooseInitialPassword(pendingToken: String, password: String) async throws -> LoginOutcome {
        let body = ["pending_token": pendingToken, "password": password]
        let result: AuthSession = try await api.post("auth/login/initial-password", body: body, authorized: false)
        return try await resolve(result)
    }

    // MARK: - Human check

    /// What stands in for a captcha on the email paths: nothing, the dev stack's bypass token, or an App Attest proof.
    struct HumanCheck: Sendable {
        var turnstile: String?
        var appAttest: AppAttestClient.Proof?
    }

    enum HumanCheckError: LocalizedError {
        case emailSignInOff

        var errorDescription: String? {
            "This server only allows email sign-in on the web. Sign in with Apple, Google or a passkey instead."
        }
    }

    private struct DeploymentChecks: Decodable {
        var captcha: Bool?
        var appAttest: Bool?

        enum CodingKeys: String, CodingKey {
            case captcha
            case appAttest = "app_attest"
        }
    }

    private struct PasswordAuthBody: Encodable {
        var email: String
        var password: String
        var turnstile: String?
        var appAttest: AppAttestClient.Proof?
        /// Login only: this build can take a `password_change_required` answer.
        var supportsPasswordChange: Bool?

        init(email: String, password: String, check: HumanCheck, supportsPasswordChange: Bool? = nil) {
            self.email = email
            self.password = password
            turnstile = check.turnstile
            appAttest = check.appAttest
            self.supportsPasswordChange = supportsPasswordChange
        }

        enum CodingKeys: String, CodingKey {
            case email, password, turnstile
            case appAttest = "app_attest"
            case supportsPasswordChange = "supports_password_change"
        }
    }

    private struct ResetBody: Encodable {
        var email: String
        var turnstile: String?
        var appAttest: AppAttestClient.Proof?

        enum CodingKeys: String, CodingKey {
            case email, turnstile
            case appAttest = "app_attest"
        }
    }

    private func humanCheck(_ action: AppAttestClient.Action) async throws -> HumanCheck {
        let bypass = AppConfig.turnstileToken.trimmingCharacters(in: .whitespaces)
        if !bypass.isEmpty { return HumanCheck(turnstile: bypass) }
        let checks: DeploymentChecks? = try? await api.get("auth/config", authorized: false)
        guard let checks, checks.captcha == true else { return HumanCheck() }
        guard checks.appAttest == true else { throw HumanCheckError.emailSignInOff }
        return HumanCheck(appAttest: try await appAttest.proof(for: action))
    }

    /// Runs a captcha-gated call, registering this install's key again once if the server lost it.
    private func verified<T>(_ action: AppAttestClient.Action, _ send: (HumanCheck) async throws -> T) async throws -> T {
        let check = try await humanCheck(action)
        do {
            return try await send(check)
        } catch let error as APIError where error.code == "app_attest_key_unknown" && check.appAttest != nil {
            await appAttest.forgetKey()
            return try await send(try await humanCheck(action))
        }
    }

    /// Maps a start-of-login response onto an outcome, completing the sign-in
    /// when the backend already resolved it.
    private func resolve(_ result: AuthSession) async throws -> LoginOutcome {
        // Carries code_required false and no token, so it is read first.
        if result.passwordChangeRequired == true {
            guard let pending = result.pendingToken else {
                throw APIError.decoding(URLError(.cannotParseResponse))
            }
            return .needsNewPassword(pendingToken: pending)
        }
        if result.needsCode {
            guard let session = result.session else {
                throw APIError.decoding(URLError(.cannotParseResponse))
            }
            return .needsCode(session: session)
        }
        if result.twoFARequired == true, let pending = result.pendingToken {
            return .needsTwoFA(pendingToken: pending)
        }
        guard let token = result.token else {
            throw APIError.decoding(URLError(.cannotParseResponse))
        }
        try await completeLogin(with: token)
        return .loggedIn
    }

    func confirmLogin(session: String, code: String) async throws -> LoginOutcome {
        let body = ["session": session, "code": code]
        let result: LoginResult = try await api.post("auth/login/confirm", body: body, authorized: false)
        if result.twoFARequired == true, let pending = result.pendingToken {
            return .needsTwoFA(pendingToken: pending)
        }
        guard let token = result.token else {
            throw APIError.decoding(URLError(.cannotParseResponse))
        }
        try await completeLogin(with: token)
        return .loggedIn
    }

    func verifyTwoFA(pendingToken: String, code: String) async throws {
        let body = ["pending_token": pendingToken, "code": code]
        let token: AuthToken = try await api.post("auth/2fa/verify", body: body, authorized: false)
        try await completeLogin(with: token)
    }

    private func completeLogin(with token: AuthToken) async throws {
        await api.setToken(token)
        wasSignedOutBySession = false
        try await loadIdentity()
        await enterApp()
    }

    // MARK: - Social sign-in

    /// Which social providers this backend supports (drives button visibility).
    func fetchAuthProviders() async -> AuthProvidersInfo? {
        try? await api.get("auth/providers", authorized: false)
    }

    /// Apple shares the user's name only with the app, never inside the
    /// identity token, so it rides along for first-sign-in profile prefill.
    func signInWithApple(identityToken: String, firstName: String?, lastName: String?) async throws -> SocialOutcome {
        var body = ["identity_token": identityToken]
        if let firstName, !firstName.isEmpty { body["first_name"] = firstName }
        if let lastName, !lastName.isEmpty { body["last_name"] = lastName }
        let result: LoginResult = try await api.post("auth/apple", body: body, authorized: false)
        return try await resolve(social: result)
    }

    func signInWithGoogle(idToken: String) async throws -> SocialOutcome {
        let result: LoginResult = try await api.post("auth/google", body: ["id_token": idToken], authorized: false)
        return try await resolve(social: result)
    }

    private struct BrowserSignInConfig: Decodable {
        var providers: [String]?
        var appURL: String?

        enum CodingKeys: String, CodingKey {
            case providers
            case appURL = "app_url"
        }
    }

    /// The native providers plus, when this backend's dashboard is on a host
    /// the app is associated with, the website's own Google sign-in.
    func fetchSignInProviders() async -> AuthProvidersInfo? {
        async let native = fetchAuthProviders()
        async let config: BrowserSignInConfig? = try? api.get("auth/config", authorized: false)
        var browserHost: String?
        if let config = await config,
           config.providers?.contains("google") == true,
           let url = config.appURL.flatMap({ URL(string: $0) }),
           url.scheme?.lowercased() == "https",
           let host = url.host()?.lowercased(),
           AppConfig.associatedWebHosts.contains(host)
        {
            browserHost = host
        }
        // Unknown native providers keep Apple optimistic, as a nil answer does.
        var info = await native ?? (browserHost == nil ? nil : AuthProvidersInfo(apple: .init(enabled: true)))
        info?.googleBrowserHost = browserHost
        return info
    }

    struct BrowserSignIn: Decodable, Sendable {
        var url: URL
        /// Stays on the device and is presented again at the exchange.
        var binding: String
    }

    /// The website's sign-in for `provider`; the backend's callback ends on `<app_url>/auth/sso?code=`.
    func beginBrowserSignIn(_ provider: String) async throws -> BrowserSignIn {
        try await api.post("auth/\(provider)/begin", authorized: false)
    }

    func finishBrowserSignIn(code: String, binding: String) async throws -> SocialOutcome {
        let result: LoginResult = try await api.post("auth/sso/exchange", body: ["code": code, "binding": binding], authorized: false)
        return try await resolve(social: result)
    }

    func confirmSSOLink(pendingToken: String, password: String) async throws -> SocialOutcome {
        let body = ["pending_token": pendingToken, "password": password]
        let result: LoginResult = try await api.post("auth/sso/link", body: body, authorized: false)
        return try await resolve(social: result)
    }

    /// Social sign-in runs the same 2FA gate as password login, and an address
    /// that already has a password asks for it before the identity is linked.
    private func resolve(social result: LoginResult) async throws -> SocialOutcome {
        if result.twoFARequired == true, let pending = result.pendingToken {
            return .needsTwoFA(pendingToken: pending)
        }
        if result.linkRequired == true, let pending = result.pendingToken {
            return .needsLink(SSOLinkChallenge(pendingToken: pending, email: result.linkEmail ?? "", provider: result.linkProvider ?? ""))
        }
        guard let token = result.token else {
            throw APIError.decoding(URLError(.cannotParseResponse))
        }
        try await completeLogin(with: token)
        return .loggedIn
    }

    // MARK: - Registration

    /// Returns the session to confirm, or nil when the deployment has email
    /// verification off and the account already exists.
    func startRegister(email: String, password: String) async throws -> String? {
        let result: AuthSession = try await verified(.register) { check in
            try await self.api.post("auth/register", body: PasswordAuthBody(email: email, password: password, check: check), authorized: false)
        }
        return result.needsCode ? result.session : nil
    }

    /// Registration confirm returns 204 (no tokens); chain into the login flow after.
    func confirmRegister(session: String, code: String) async throws {
        let body = ["session": session, "code": code]
        let _: EmptyBody = try await api.post("auth/register/confirm", body: body, authorized: false)
    }

    func requestPasswordReset(email: String) async throws {
        let _: EmptyBody = try await verified(.resetPassword) { check in
            try await self.api.post("auth/reset-password", body: ResetBody(email: email, turnstile: check.turnstile, appAttest: check.appAttest), authorized: false)
        }
    }

    // MARK: - Logout

    func logout() async {
        // Treat a 401 on logout as already logged out.
        let _: EmptyBody? = try? await api.post("auth/logout")
        wasSignedOutBySession = false
        await clearSession()
    }

    private func handleAuthFailure() {
        Task {
            // Only flag it when the user was actually inside the app; a failed
            // bootstrap already lands on sign-in without needing a banner.
            if phase == .ready || phase == .selectOrg || phase == .onboarding {
                wasSignedOutBySession = true
            }
            await clearSession()
        }
    }

    private func clearSession() async {
        await api.setToken(nil)
        user = nil
        memberships = []
        currentOrgID = nil
        currentOrg = nil
        permissions = []
        role = nil
        phase = .loggedOut
        onLoggedOut?()
    }
}
