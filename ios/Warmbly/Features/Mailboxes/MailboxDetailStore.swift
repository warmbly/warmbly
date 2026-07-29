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
    /// True once a local edit or PATCH response refreshed the row, so views
    /// can stop preferring analytics for fields the list response omits.
    private(set) var rowEdited = false
    var actionError: String?

    // Debounce state for stepper bursts: one snapshot per burst, one body.
    private var pendingBody = MailboxUpdateBody()
    private var pendingSnapshot: EmailAccount?
    private var editGeneration = 0

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
            rowEdited = true
            apply(updated)
            return true
        } catch {
            actionError = error.localizedDescription
            return false
        }
    }

    /// Optimistic partial update: apply the local mutation immediately, PATCH,
    /// then adopt the server's row (or roll back and surface the error).
    func update(_ api: APIClient, body: MailboxUpdateBody, mutate: (inout EmailAccount) -> Void) async {
        let previous = account
        rowEdited = true
        withAnimation(.snappy) { mutate(&account) }
        do {
            let updated: EmailAccount = try await api.patch("emails/\(account.id)", body: body)
            withAnimation(.snappy) { account = updated }
        } catch {
            withAnimation(.snappy) { account = previous }
            actionError = error.localizedDescription
        }
    }

    /// Stepper variant: every tap applies locally right away, but rapid taps
    /// coalesce into one PATCH after 600ms of quiet. A failed PATCH rolls the
    /// whole burst back to the pre-burst row.
    func updateDebounced(
        _ api: APIClient,
        mutateBody: (inout MailboxUpdateBody) -> Void,
        mutate: (inout EmailAccount) -> Void
    ) async {
        if pendingSnapshot == nil { pendingSnapshot = account }
        mutateBody(&pendingBody)
        rowEdited = true
        withAnimation(.snappy) { mutate(&account) }
        editGeneration += 1
        let generation = editGeneration
        try? await Task.sleep(for: .milliseconds(600))
        guard generation == editGeneration else { return }
        let body = pendingBody
        let snapshot = pendingSnapshot
        pendingBody = MailboxUpdateBody()
        pendingSnapshot = nil
        do {
            let updated: EmailAccount = try await api.patch("emails/\(account.id)", body: body)
            withAnimation(.snappy) { account = updated }
        } catch {
            if let snapshot {
                withAnimation(.snappy) { account = snapshot }
            }
            actionError = error.localizedDescription
        }
    }

    // MARK: Disconnect

    /// DELETE /emails/:id (204); true on success so the view can pop.
    func deleteAccount(_ api: APIClient) async -> Bool {
        do {
            let _: EmptyBody = try await api.delete("emails/\(account.id)")
            return true
        } catch {
            actionError = error.localizedDescription
            return false
        }
    }
}
