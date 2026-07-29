import Foundation

/// Loads the four analytics surfaces in parallel; each section fails
/// independently so one broken endpoint never blanks the whole screen.
@MainActor
@Observable
final class AnalyticsStore {
    var period: AnalyticsPeriod = .week

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

    private func loadDashboard(_ api: APIClient) async {
        do {
            let result: DashboardAnalytics = try await api.get(
                "analytics/dashboard",
                query: ["period": period.rawValue]
            )
            dashboard = result
            dashboardError = nil
        } catch {
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
