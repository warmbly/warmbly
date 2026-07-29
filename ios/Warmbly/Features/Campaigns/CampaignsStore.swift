import SwiftUI

/// Server-backed browse scope: status buckets map to the list endpoint's
/// `status` param, folders to `folder`. Every scope pages server-side, so
/// infinite scroll and result totals are correct in all of them.
enum CampaignListScope: Equatable, Hashable {
    case all
    case running
    case paused
    case draft
    case finished
    case folder(id: String, label: String)

    var statusParam: String? {
        switch self {
        case .running: "active"
        case .paused: "paused"
        case .draft: "draft"
        case .finished: "completed"
        case .all, .folder: nil
        }
    }

    var folderParam: String? {
        if case let .folder(id, _) = self { return id }
        return nil
    }

    var title: String {
        switch self {
        case .all: "All campaigns"
        case .running: "Sending"
        case .paused: "Paused"
        case .draft: "Drafts"
        case .finished: "Finished"
        case let .folder(_, label): label
        }
    }
}

@MainActor
@Observable
final class CampaignsStore {
    private(set) var campaigns: [Campaign] = []
    /// Sent/opened/replied per campaign id, derived from the owner-scoped
    /// compare endpoint; teammates' campaigns simply have no entry.
    private(set) var rowStats: [String: CampaignRowStats] = [:]
    /// Count matching the current scope + search (`pagination.total`).
    private(set) var totalCount: Int?
    /// Drawer counts from `GET /campaigns-overview`.
    private(set) var overview: CampaignsOverview?
    private(set) var nextCursor: String?
    private(set) var hasMore = false
    private(set) var isLoading = false
    private(set) var isLoadingMore = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    var query = ""
    var scope: CampaignListScope = .all
    /// Row-level start/pause failures (cooldown, readiness, plan gate).
    var actionError: String?

    private var generation = 0

    // MARK: Derived

    var isSearching: Bool {
        !query.trimmingCharacters(in: .whitespaces).isEmpty
    }

    var allCount: Int { overview?.total ?? campaigns.count }
    var runningCount: Int { overview?.active ?? 0 }
    var pausedCount: Int { overview?.paused ?? 0 }
    var draftCount: Int { overview?.draft ?? 0 }
    var finishedCount: Int { overview?.completed ?? 0 }

    func folderCount(_ id: String) -> Int {
        overview?.folders?.first { $0.folderID == id }?.total ?? 0
    }

    private func listParams(cursor: String? = nil) -> [String: String?] {
        var params: [String: String?] = ["limit": "50"]
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        if !trimmed.isEmpty { params["q"] = trimmed }
        if let status = scope.statusParam { params["status"] = status }
        if let folder = scope.folderParam { params["folder"] = folder }
        if let cursor { params["cursor"] = cursor }
        return params
    }

    // MARK: Loading

    func load(_ api: APIClient) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            let page: CampaignListPage = try await api.get("campaigns", query: listParams())
            guard gen == generation else { return }
            withAnimation(.snappy) {
                campaigns = page.data ?? []
                totalCount = (page.pagination?.total).map(Int.init)
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
            errorMessage = nil
            hasLoaded = true
            isLoading = false
            await loadOverview(api)
            await enrichStats(api, ids: campaigns.map(\.id), generation: gen)
        } catch {
            guard gen == generation else { return }
            errorMessage = error.localizedDescription
            isLoading = false
        }
    }

    func loadOverview(_ api: APIClient) async {
        if let fresh: CampaignsOverview = try? await api.get("campaigns-overview") {
            withAnimation(.snappy) { overview = fresh }
        }
    }

    func loadMore(_ api: APIClient) async {
        guard hasMore, !isLoadingMore, let cursor = nextCursor else { return }
        let gen = generation
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page: CampaignListPage = try await api.get("campaigns", query: listParams(cursor: cursor))
            guard gen == generation else { return }
            let fresh = (page.data ?? []).filter { new in !campaigns.contains(where: { $0.id == new.id }) }
            withAnimation(.snappy) {
                campaigns.append(contentsOf: fresh)
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
            await enrichStats(api, ids: fresh.map(\.id), generation: gen)
        } catch {
            // Keep the list; the sentinel row will retry on next appear.
        }
    }

    /// Best-effort row counters from the compare endpoint (10 ids per call).
    /// All-time window; failures and missing rows are silently ignored.
    private func enrichStats(_ api: APIClient, ids: [String], generation gen: Int) async {
        guard !ids.isEmpty else { return }
        let to = CampaignDates.param(Date().addingTimeInterval(86_400))
        var index = 0
        while index < ids.count {
            guard gen == generation else { return }
            let chunk = Array(ids[index ..< min(index + 10, ids.count)])
            index += 10
            let result: CampaignComparisonResult? = try? await api.get(
                "analytics/campaigns/compare",
                query: ["ids": chunk.joined(separator: ","), "from": "2000-01-01", "to": to]
            )
            guard gen == generation, let items = result?.campaigns else { continue }
            withAnimation(.snappy) {
                for item in items {
                    guard let cid = item.campaignID else { continue }
                    let sent = item.emailsSent ?? 0
                    rowStats[cid] = CampaignRowStats(
                        sent: sent,
                        opened: Int((Double(sent) * (item.openRate ?? 0) / 100).rounded()),
                        replied: Int((Double(sent) * (item.replyRate ?? 0) / 100).rounded())
                    )
                }
            }
        }
    }

    // MARK: Mutations

    /// Quick start/pause straight from the list row. Preconditions (60s
    /// cooldown, readiness, plan gate) come back as 400s with a specific
    /// message; surface it verbatim. Refresh the row either way, since a
    /// failed start can still flip status server-side (paused_no_accounts).
    func toggle(_ api: APIClient, campaign: Campaign, start: Bool) async {
        do {
            let _: CampaignActionResponse = try await api.post("campaigns/\(campaign.id)/\(start ? "start" : "stop")")
        } catch {
            actionError = error.localizedDescription
        }
        if let fresh: Campaign = try? await api.get("campaigns/\(campaign.id)"),
           let index = campaigns.firstIndex(where: { $0.id == campaign.id }) {
            withAnimation(.snappy) { campaigns[index] = fresh }
        }
        await loadOverview(api)
    }

    func create(_ api: APIClient, body: CampaignCreateBody) async throws -> Campaign {
        let campaign: Campaign = try await api.post(
            "campaigns",
            body: body,
            idempotent: true
        )
        withAnimation(.snappy) {
            campaigns.insert(campaign, at: 0)
            if let total = totalCount { totalCount = total + 1 }
        }
        await loadOverview(api)
        return campaign
    }

    /// DELETE is creator-only server-side; a teammate gets 404 even with
    /// manage permission, so translate that into a clear message.
    func delete(_ api: APIClient, campaign: Campaign) async throws {
        do {
            let _: EmptyBody = try await api.delete("campaigns/\(campaign.id)")
        } catch let error as APIError {
            if case let .server(status, _) = error, status == 404 {
                throw CampaignActionError.creatorOnly("Only the campaign creator can delete this campaign.")
            }
            throw error
        }
        withAnimation(.snappy) {
            campaigns.removeAll { $0.id == campaign.id }
            if let total = totalCount, total > 0 { totalCount = total - 1 }
        }
        await loadOverview(api)
    }
}
