import Foundation
import SwiftUI

/// Detail-screen store: keeps the mailbox row fresh, pulls the per-account
/// health snapshot (`/analytics/accounts/:id`), the on-demand domain auth
/// check, the warmup ban status, and drives the lifecycle + settings PATCH.
///
/// `GET /emails/:id` is broken server-side (500), so detail is rendered from
/// the list row plus `/analytics/accounts/:id`, exactly as the web does.
@MainActor
@Observable
final class MailboxDetailStore {
    private(set) var account: EmailAccount
    private(set) var analytics: AccountAnalytics?
    private(set) var authCheck: AuthCheckResult?
    private(set) var banStatus: MailboxBanStatus?
    private(set) var isCheckingAuth = false
    private(set) var isSaving = false
    var actionError: String?

    init(account: EmailAccount) {
        self.account = account
    }

    // Matches the web's presence resource key so iOS and web viewers merge.
    var presenceKey: String { "mailbox:\(account.id)" }

    func apply(_ updated: EmailAccount) {
        withAnimation { account = updated }
    }

    // MARK: Loading

    func loadAnalytics(_ api: APIClient) async {
        do {
            let status: AccountAnalytics = try await api.get("analytics/accounts/\(account.id)")
            withAnimation { analytics = status }
        } catch {
            // Missing analytics permission or transient; the header still renders.
        }
    }

    func loadBanStatus(_ api: APIClient) async {
        banStatus = try? await api.get("emails/\(account.id)/warmup/ban-status")
    }

    func runAuthCheck(_ api: APIClient) async {
        isCheckingAuth = true
        defer { isCheckingAuth = false }
        do {
            let result: AuthCheckResult = try await api.get("emails/\(account.id)/auth-check")
            withAnimation { authCheck = result }
        } catch {
            actionError = error.localizedDescription
        }
    }

    // MARK: Warmup lifecycle

    /// action: "start" | "pause" | "resume" | "stop"; returns the updated row.
    func warmupAction(_ api: APIClient, _ action: String) async {
        do {
            let updated: EmailAccount = try await api.post("emails/\(account.id)/warmup/\(action)")
            apply(updated)
            await loadBanStatus(api)
        } catch {
            actionError = error.localizedDescription
        }
    }

    // MARK: Settings PATCH

    func save(_ api: APIClient, _ body: MailboxUpdateBody) async -> Bool {
        isSaving = true
        defer { isSaving = false }
        do {
            let updated: EmailAccount = try await api.patch("emails/\(account.id)", body: body)
            apply(updated)
            return true
        } catch {
            actionError = error.localizedDescription
            return false
        }
    }
}
