import Foundation

/// Loads the four analytics surfaces in parallel; each section fails
/// independently so one broken endpoint never blanks the whole screen.
@MainActor
@Observable
final class AnalyticsStore {
    var period: AnalyticsPeriod = .week
    /// Narrows the overview's campaign sections; mailbox health stays workspace-wide.
    var campaignFilter = AnalyticsCampaignFilter()

    private(set) var dashboard: DashboardAnalytics?
    private(set) var deliverability: DeliverabilitySummary?
    private(set) var warmup: WarmupAnalytics?
    private(set) var accounts: [AccountHealthRow] = []

    private(set) var isLoading = false
    private(set) var dashboardError: String?

    /// Accounts actively warming, for the drawer's live badge.
    var warmingCount: Int {
        accounts.count { $0.warmupStatus?.enabled == true && $0.warmupStatus?.paused != true }
    }

    /// Accounts whose health checks flagged problems.
    var issueCount: Int {
        accounts.count { $0.health?.status == "warning" || $0.health?.status == "error" }
    }

    private(set) var deliverabilityError: String?
    private(set) var warmupError: String?
    private(set) var accountsError: String?

    func load(_ api: APIClient) async {
        if isLoading { return }
        isLoading = true
        async let dashboardDone: Void = loadDashboard(api)
        async let deliverabilityDone: Void = loadDeliverability(api)
        async let warmupDone: Void = loadWarmup(api)
        async let accountsDone: Void = loadAccounts(api)
        _ = await (dashboardDone, deliverabilityDone, warmupDone, accountsDone)
        isLoading = false
    }

    /// Reloads only the overview, for a filter change.
    func reloadDashboard(_ api: APIClient) async {
        await loadDashboard(api)
    }

    /// "All campaigns", a folder or campaign name, or a count.
    func scopeLabel(folderName: (String) -> String?) -> String {
        let filter = campaignFilter
        if filter.isEmpty { return "All campaigns" }
        let scope = dashboard?.scope
        let count = scope?.campaignCount
        let total = count.map { " · \($0) campaign\($0 == 1 ? "" : "s")" } ?? ""
        if filter.campaigns.isEmpty, filter.folders.count == 1, let id = filter.folders.first {
            let name = scope?.folders?.first { $0.id == id }?.name ?? folderName(id)
            return (name ?? "1 folder") + total
        }
        if filter.folders.isEmpty, filter.campaigns.count == 1, let id = filter.campaigns.first {
            return scope?.campaigns?.first { $0.id == id }?.name ?? "1 campaign"
        }
        if filter.folders.isEmpty {
            let n = count ?? filter.campaigns.count
            return "\(n) campaign\(n == 1 ? "" : "s")"
        }
        var parts = ["\(filter.folders.count) folder\(filter.folders.count == 1 ? "" : "s")"]
        if !filter.campaigns.isEmpty {
            parts.append("\(filter.campaigns.count) campaign\(filter.campaigns.count == 1 ? "" : "s")")
        }
        return parts.joined(separator: " + ") + total
    }

    private func loadDashboard(_ api: APIClient) async {
        let filter = campaignFilter
        var query: [String: String?] = ["period": period.rawValue]
        if !filter.campaigns.isEmpty { query["campaign_ids"] = filter.campaigns.sorted().joined(separator: ",") }
        if !filter.folders.isEmpty { query["folder_ids"] = filter.folders.sorted().joined(separator: ",") }
        do {
            let result: DashboardAnalytics = try await api.get("analytics/dashboard", query: query)
            // A newer filter is already on its way; this answer describes the old one.
            guard filter == campaignFilter else { return }
            dashboard = result
            dashboardError = nil
            // Drop selections the server no longer recognises (deleted, or another workspace's).
            if let scope = result.scope {
                let campaigns = Set((scope.campaigns ?? []).map(\.id)).intersection(filter.campaigns)
                let folders = Set((scope.folders ?? []).map(\.id)).intersection(filter.folders)
                if campaigns != filter.campaigns || folders != filter.folders {
                    campaignFilter = AnalyticsCampaignFilter(campaigns: campaigns, folders: folders)
                }
            }
        } catch {
            guard filter == campaignFilter else { return }
            dashboardError = error.localizedDescription
        }
    }

    private func loadDeliverability(_ api: APIClient) async {
        // from/to are RFC3339 here (unlike the warmup endpoint).
        let to = Date()
        let from = Calendar.current.date(byAdding: .day, value: -period.days, to: to) ?? to
        let iso = ISO8601DateFormatter()
        do {
            let result: DeliverabilitySummary = try await api.get(
                "analytics/deliverability",
                query: ["from": iso.string(from: from), "to": iso.string(from: to)]
            )
            deliverability = result
            deliverabilityError = nil
        } catch {
            deliverabilityError = error.localizedDescription
        }
    }

    private func loadWarmup(_ api: APIClient) async {
        // from/to are required, YYYY-MM-DD; no email_id = all accounts.
        let to = Date()
        let from = Calendar.current.date(byAdding: .day, value: -(period.days - 1), to: to) ?? to
        do {
            let result: WarmupAnalytics = try await api.get(
                "analytics/warmup",
                query: ["from": AnalyticsDay.string(from: from), "to": AnalyticsDay.string(from: to)]
            )
            warmup = result
            warmupError = nil
        } catch {
            warmupError = error.localizedDescription
        }
    }

    private func loadAccounts(_ api: APIClient) async {
        do {
            let envelope: AnalyticsDataEnvelope<AccountHealthRow> = try await api.get("analytics/accounts")
            accounts = envelope.data ?? []
            accountsError = nil
        } catch {
            accountsError = error.localizedDescription
        }
    }
}
