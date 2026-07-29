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

    func startLogin(email: String, password: String) async throws -> String {
        let body = [
            "email": email,
            "password": password,
            "turnstile": AppConfig.turnstileToken,
        ]
        let session: AuthSession = try await api.post("auth/login", body: body, authorized: false)
        return session.session
    }

    func confirmLogin(session: String, code: String) async throws -> LoginOutcome {
        let body = [
            "session": session,
            "code": code,
            "turnstile": AppConfig.turnstileToken,
        ]
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
    func signInWithApple(identityToken: String, firstName: String?, lastName: String?) async throws {
        var body = ["identity_token": identityToken]
        if let firstName, !firstName.isEmpty { body["first_name"] = firstName }
        if let lastName, !lastName.isEmpty { body["last_name"] = lastName }
        let token: AuthToken = try await api.post("auth/apple", body: body, authorized: false)
        try await completeLogin(with: token)
    }

    func signInWithGoogle(idToken: String) async throws {
        let token: AuthToken = try await api.post("auth/google", body: ["id_token": idToken], authorized: false)
        try await completeLogin(with: token)
    }

    // MARK: - Registration

    func startRegister(email: String, password: String) async throws -> String {
        let body = [
            "email": email,
            "password": password,
            "turnstile": AppConfig.turnstileToken,
        ]
        let session: AuthSession = try await api.post("auth/register", body: body, authorized: false)
        return session.session
    }

    /// Registration confirm returns 204 (no tokens); chain into the login flow after.
    func confirmRegister(session: String, code: String) async throws {
        let body = [
            "session": session,
            "code": code,
            "turnstile": AppConfig.turnstileToken,
        ]
        let _: EmptyBody = try await api.post("auth/register/confirm", body: body, authorized: false)
    }

    func requestPasswordReset(email: String) async throws {
        let body = ["email": email, "turnstile": AppConfig.turnstileToken]
        let _: EmptyBody = try await api.post("auth/reset-password", body: body, authorized: false)
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
