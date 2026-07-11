import Foundation
import SwiftUI

// MARK: - Scope

/// Which slice of the fleet the list shows. Every scope is a client-side
/// filter over the loaded rows; Issues additionally needs analytics statuses.
enum MailboxScope: Hashable, CaseIterable {
    case all, warming, paused, issues, off

    var title: String {
        switch self {
        case .all: "All mailboxes"
        case .warming: "Warming"
        case .paused: "Paused"
        case .issues: "Issues"
        case .off: "Warmup off"
        }
    }

    var icon: String {
        switch self {
        case .all: "tray.full"
        case .warming: "flame.fill"
        case .paused: "pause.circle"
        case .issues: "exclamationmark.triangle"
        case .off: "moon.zzz"
        }
    }

    var tone: Tone {
        switch self {
        case .all: .sky
        case .warming: .orange
        case .paused: .amber
        case .issues: .rose
        case .off: .slate
        }
    }
}

// MARK: - Store

/// List-screen store: the mailbox rows plus the per-account health snapshot
/// from `/analytics/accounts`, merged by id. Owns multi-select and the bulk
/// warmup/remove actions behind the floating selection bar.
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

    var allCount: Int { totalCount ?? accounts.count }

    /// Warming = warmup anchor set and not paused; never a status string.
    var warmingCount: Int { accounts.filter(\.isWarmingActive).count }

    var pausedCount: Int { accounts.filter(\.isWarmupPaused).count }

    var issueCount: Int {
        accounts.filter { statuses[$0.id]?.health?.hasIssue == true }.count
    }

    /// Warmup never enabled: no ramp anchor at all.
    var offCount: Int { accounts.filter { $0.warmup == nil }.count }

    func count(for scope: MailboxScope) -> Int {
        switch scope {
        case .all: allCount
        case .warming: warmingCount
        case .paused: pausedCount
        case .issues: issueCount
        case .off: offCount
        }
    }

    // MARK: Selection

    private(set) var selectedIDs: Set<String> = []
    var isSelecting = false

    var selectedCount: Int { selectedIDs.count }

    func isSelected(_ id: String) -> Bool { selectedIDs.contains(id) }

    func toggleSelected(_ id: String) {
        if selectedIDs.contains(id) { selectedIDs.remove(id) } else { selectedIDs.insert(id) }
    }

    func enterSelection(with id: String? = nil) {
        withAnimation(.snappy) {
            isSelecting = true
            if let id { selectedIDs.insert(id) }
        }
    }

    func exitSelection() {
        withAnimation(.snappy) {
            isSelecting = false
            selectedIDs.removeAll()
        }
    }

    /// Toggle select-all over the rows currently visible in the list.
    func selectAll(_ ids: [String]) {
        withAnimation(.snappy) {
            if allSelected(of: ids) { selectedIDs.removeAll() } else { selectedIDs = Set(ids) }
        }
    }

    func allSelected(of ids: [String]) -> Bool {
        !ids.isEmpty && ids.allSatisfy { selectedIDs.contains($0) }
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

    /// Optimistic insert from the connect flow; realtime refreshes the rest.
    func insert(_ account: EmailAccount) {
        withAnimation {
            if let index = accounts.firstIndex(where: { $0.id == account.id }) {
                accounts[index] = account
            } else {
                accounts.insert(account, at: 0)
                if let total = totalCount { totalCount = total + 1 }
            }
        }
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

    // MARK: Bulk actions

    /// action: "start" | "pause". Applies per-row results as they land and
    /// reports partial failures in one line.
    func bulkWarmup(_ api: APIClient, action: String) async {
        let ids = Array(selectedIDs)
        guard !ids.isEmpty else { return }
        var failures = 0
        for id in ids {
            do {
                let updated: EmailAccount = try await api.post("emails/\(id)/warmup/\(action)")
                if let index = accounts.firstIndex(where: { $0.id == id }) {
                    withAnimation { accounts[index] = updated }
                }
            } catch {
                failures += 1
            }
        }
        if failures > 0 {
            actionError = "\(failures) of \(ids.count) mailboxes couldn't \(action)"
        }
        exitSelection()
    }

    func bulkRemove(_ api: APIClient) async {
        let ids = Array(selectedIDs)
        guard !ids.isEmpty else { return }
        var failures = 0
        for id in ids {
            do {
                let _: EmptyBody = try await api.delete("emails/\(id)")
                withAnimation {
                    accounts.removeAll { $0.id == id }
                    statuses.removeValue(forKey: id)
                    if let total = totalCount { totalCount = max(0, total - 1) }
                }
            } catch {
                failures += 1
            }
        }
        if failures > 0 {
            actionError = "\(failures) of \(ids.count) mailboxes couldn't be removed"
        }
        exitSelection()
    }
}
