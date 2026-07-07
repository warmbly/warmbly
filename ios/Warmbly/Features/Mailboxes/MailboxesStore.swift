import Foundation
import SwiftUI

/// List-screen store: the mailbox rows plus the per-account health snapshot
/// from `/analytics/accounts`, merged by id.
@MainActor
@Observable
final class MailboxesStore {
    private(set) var accounts: [EmailAccount] = []
    private(set) var statuses: [String: AccountAnalytics] = [:]
    private(set) var hasLoaded = false
    private(set) var isLoading = false
    private(set) var isLoadingMore = false
    private(set) var loadError: String?
    private(set) var nextCursor: String?
    private(set) var totalCount: Int?
    var actionError: String?
    var query = ""

    // MARK: Derived stats

    /// Warming = warmup anchor set and not paused; never a status string.
    var warmingCount: Int { accounts.filter(\.isWarmingActive).count }

    var issueCount: Int {
        accounts.filter { statuses[$0.id]?.health?.hasIssue == true }.count
    }

    // MARK: Loading

    func load(_ api: APIClient, includeStatuses: Bool) async {
        isLoading = true
        do {
            let page: ListResponse<EmailAccount> = try await api.get("emails", query: listParams(cursor: nil))
            withAnimation {
                accounts = page.data
                if let total = page.pagination?.total {
                    totalCount = Int(total)
                } else {
                    totalCount = page.data.count
                }
            }
            nextCursor = page.pagination?.nextCursor
            loadError = nil
        } catch {
            if Task.isCancelled {
                isLoading = false
                return
            }
            loadError = error.localizedDescription
        }
        isLoading = false
        hasLoaded = true
        if includeStatuses { await loadStatuses(api) }
    }

    func loadMore(_ api: APIClient) async {
        guard let cursor = nextCursor, !isLoadingMore else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page: ListResponse<EmailAccount> = try await api.get("emails", query: listParams(cursor: cursor))
            let known = Set(accounts.map(\.id))
            withAnimation { accounts += page.data.filter { !known.contains($0.id) } }
            nextCursor = page.pagination?.nextCursor
        } catch {
            nextCursor = nil
        }
    }

    func loadStatuses(_ api: APIClient) async {
        do {
            let page: ListResponse<AccountAnalytics> = try await api.get("analytics/accounts")
            withAnimation {
                statuses = Dictionary(page.data.map { ($0.id, $0) }, uniquingKeysWith: { _, latest in latest })
            }
        } catch {
            // Missing view analytics permission or a transient failure; the
            // list stays usable without health data.
        }
    }

    private func listParams(cursor: String?) -> [String: String?] {
        var params: [String: String?] = ["limit": "200"]
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty { params["q"] = trimmed }
        if let cursor { params["cursor"] = cursor }
        return params
    }

    // MARK: Row actions

    /// action: "start" | "pause" | "resume" | "stop"; returns the updated row.
    func warmupAction(_ api: APIClient, id: String, action: String) async {
        do {
            let updated: EmailAccount = try await api.post("emails/\(id)/warmup/\(action)")
            if let index = accounts.firstIndex(where: { $0.id == id }) {
                withAnimation { accounts[index] = updated }
            }
        } catch {
            actionError = error.localizedDescription
        }
    }

    func deleteAccount(_ api: APIClient, id: String) async {
        do {
            let _: EmptyBody = try await api.delete("emails/\(id)")
            withAnimation {
                accounts.removeAll { $0.id == id }
                statuses.removeValue(forKey: id)
                if let total = totalCount { totalCount = max(0, total - 1) }
            }
        } catch {
            actionError = error.localizedDescription
        }
    }
}
