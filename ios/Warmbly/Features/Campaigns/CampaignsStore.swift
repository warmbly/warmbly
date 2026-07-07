import SwiftUI

/// Client-side status filter driven by the tappable stat strip.
enum CampaignListScope: Equatable {
    case all
    case running
    case paused
    case finished

    func matches(_ campaign: Campaign) -> Bool {
        switch self {
        case .all: return true
        case .running: return campaign.statusBucket == .running
        case .paused: return campaign.statusBucket == .paused
        case .finished: return campaign.statusBucket == .finished
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
    private(set) var totalCount: Int?
    private(set) var nextCursor: String?
    private(set) var hasMore = false
    private(set) var isLoading = false
    private(set) var isLoadingMore = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    var query = ""
    var scope: CampaignListScope = .all

    private var generation = 0

    // MARK: Derived

    var filtered: [Campaign] {
        scope == .all ? campaigns : campaigns.filter { scope.matches($0) }
    }

    var runningCount: Int { campaigns.filter { $0.statusBucket == .running }.count }
    var pausedCount: Int { campaigns.filter { $0.statusBucket == .paused }.count }
    var finishedCount: Int { campaigns.filter { $0.statusBucket == .finished }.count }
    var displayTotal: Int { totalCount ?? campaigns.count }

    // MARK: Loading

    func load(_ api: APIClient) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            var params: [String: String?] = ["limit": "100"]
            let trimmed = query.trimmingCharacters(in: .whitespaces)
            if !trimmed.isEmpty { params["q"] = trimmed }
            let page: CampaignListPage = try await api.get("campaigns", query: params)
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
            await enrichStats(api, ids: campaigns.map(\.id), generation: gen)
        } catch {
            guard gen == generation else { return }
            errorMessage = error.localizedDescription
            isLoading = false
        }
    }

    func loadMore(_ api: APIClient) async {
        guard hasMore, !isLoadingMore, let cursor = nextCursor else { return }
        let gen = generation
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            var params: [String: String?] = ["limit": "100", "cursor": cursor]
            let trimmed = query.trimmingCharacters(in: .whitespaces)
            if !trimmed.isEmpty { params["q"] = trimmed }
            let page: CampaignListPage = try await api.get("campaigns", query: params)
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

    func create(_ api: APIClient, name: String) async throws -> Campaign {
        let campaign: Campaign = try await api.post(
            "campaigns",
            body: CampaignCreateBody(name: name),
            idempotent: true
        )
        withAnimation(.snappy) {
            campaigns.insert(campaign, at: 0)
            if let total = totalCount { totalCount = total + 1 }
        }
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
    }
}
