import SwiftUI

/// Server-backed browse scope for contacts, mirroring the campaigns browser:
/// each scope maps to POST /contacts/search filter fields, so every scope pages
/// server-side with a correct `pagination.total`. Categories are the "folders".
enum ContactBrowseScope: Equatable, Hashable {
    case all
    case subscribed
    case unsubscribed
    case inCampaign
    case notContacted
    case category(id: String, label: String)

    var title: String {
        switch self {
        case .all: "All contacts"
        case .subscribed: "Subscribed"
        case .unsubscribed: "Unsubscribed"
        case .inCampaign: "In a campaign"
        case .notContacted: "Not contacted"
        case let .category(_, label): label
        }
    }

    var categoryID: String? {
        if case let .category(id, _) = self { return id }
        return nil
    }
}

/// Contacts browse store: POST /contacts/search with an opaque cursor and a
/// server-side scope, keeping stale rows visible during reloads and mirroring
/// the campaigns browser. `pagination.total` is the exact count for the current
/// scope + search; the `counts` block (first page) drives the drawer badges.
@MainActor
@Observable
final class ContactsStore {
    private(set) var contacts: [Contact] = []
    /// Exact count for the current scope + search (`pagination.total`).
    private(set) var totalCount: Int?
    /// Org-wide facet counts for the drawer, from the search `counts` block.
    private(set) var counts: ContactsCounts?
    private(set) var nextCursor: String?
    private(set) var hasMore = false
    private(set) var isLoading = false
    private(set) var isLoadingMore = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    var scope: ContactBrowseScope = .all
    var query = ""
    /// Advanced criteria from the filter sheet, layered on the scope + search.
    var filters = ContactAdvancedFilters()

    // Multi-select
    var selectionMode = false
    private(set) var selected: Set<String> = []

    /// Campaigns for the bulk add/remove pickers (fetched lazily).
    private(set) var campaignOptions: [ContactMiniCampaign] = []

    private var generation = 0

    private static let isoDate = Date.ISO8601FormatStyle()

    var displayTotal: Int { counts?.total ?? totalCount ?? contacts.count }
    var isSearching: Bool { !query.trimmingCharacters(in: .whitespaces).isEmpty }
    var hasFilters: Bool { filters.isActive }

    // MARK: Selection

    var selectedCount: Int { selected.count }
    var allLoadedSelected: Bool { !contacts.isEmpty && selected.count >= contacts.count }

    func isSelected(_ id: String) -> Bool { selected.contains(id) }

    func toggleSelected(_ id: String) {
        if selected.contains(id) { selected.remove(id) } else { selected.insert(id) }
    }

    func enterSelection(with id: String? = nil) {
        withAnimation(.snappy) {
            selectionMode = true
            if let id { selected.insert(id) }
        }
    }

    func exitSelection() {
        withAnimation(.snappy) {
            selectionMode = false
            selected.removeAll()
        }
    }

    func selectAllLoaded() {
        withAnimation(.snappy) {
            if allLoadedSelected { selected.removeAll() } else { selected = Set(contacts.map(\.id)) }
        }
    }

    // MARK: Drawer counts

    var allCount: Int { counts?.total ?? 0 }
    var subscribedCount: Int { counts?.subscribed ?? 0 }
    var unsubscribedCount: Int { counts?.unsubscribed ?? 0 }
    var inCampaignCount: Int { counts?.inCampaign ?? 0 }
    var notContactedCount: Int { counts?.notContacted ?? 0 }

    func categoryCount(_ id: String) -> Int {
        counts?.categories.first { $0.categoryID == id }?.count ?? 0
    }

    // MARK: Loading

    private func searchBody() -> ContactSearchBody {
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        var body = ContactSearchBody(query: trimmed.isEmpty ? nil : trimmed)
        var categoryIDs = Set<String>()

        switch scope {
        case .all: break
        case .subscribed: body.subscribed = true
        case .unsubscribed: body.subscribed = false
        case .inCampaign: body.minCampaigns = 1
        case .notContacted: body.maxCampaigns = 0
        case let .category(id, _): categoryIDs.insert(id)
        }

        // Advanced criteria layer on top of the scope (they win on overlap).
        let f = filters
        let completeFields = f.customFields.filter(\.isComplete)
        if !completeFields.isEmpty {
            body.customFieldFilters = completeFields.map {
                ContactFieldFilterPayload(
                    name: $0.name.trimmingCharacters(in: .whitespaces),
                    value: $0.value.trimmingCharacters(in: .whitespaces),
                    type: $0.type.rawValue
                )
            }
        }
        categoryIDs.formUnion(f.categoryIDs)
        if !categoryIDs.isEmpty { body.categoryIDs = Array(categoryIDs) }
        if let s = f.subscribed { body.subscribed = s }
        if let mn = f.minCampaigns { body.minCampaigns = mn }
        if let mx = f.maxCampaigns { body.maxCampaigns = mx }
        body.createdAfter = f.createdAfter.map { Self.isoDate.format($0) }
        body.createdBefore = f.createdBefore.map { Self.isoDate.format($0) }
        body.updatedAfter = f.updatedAfter.map { Self.isoDate.format($0) }
        body.updatedBefore = f.updatedBefore.map { Self.isoDate.format($0) }
        body.sortBy = f.sortBy.rawValue
        body.reverse = f.reverse
        return body
    }

    func load(_ api: APIClient) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            let page: ContactSearchPage = try await api.post(
                "contacts/search",
                body: searchBody(),
                query: ["limit": "50"]
            )
            guard gen == generation else { return }
            withAnimation(.snappy) {
                contacts = page.data
                totalCount = (page.pagination?.total).map(Int.init)
                // counts ride the first page; hold the last good block otherwise.
                if let fresh = page.counts { counts = fresh }
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
            errorMessage = nil
            hasLoaded = true
            isLoading = false
        } catch {
            guard gen == generation else { return }
            if !hasLoaded { errorMessage = error.localizedDescription }
            isLoading = false
        }
    }

    func loadMore(_ api: APIClient) async {
        guard hasMore, !isLoadingMore, let cursor = nextCursor else { return }
        let gen = generation
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page: ContactSearchPage = try await api.post(
                "contacts/search",
                body: searchBody(),
                query: ["limit": "50", "cursor": cursor]
            )
            guard gen == generation else { return }
            let fresh = page.data.filter { new in !contacts.contains(where: { $0.id == new.id }) }
            withAnimation(.snappy) {
                contacts.append(contentsOf: fresh)
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
        } catch {
            // Keep the list; the sentinel row retries on next appear.
        }
    }

    // MARK: Mutations

    /// POST /contacts takes an ARRAY of AddContact; email is required.
    func create(_ api: APIClient, body: ContactCreateBody) async throws -> Contact {
        let created: [Contact] = try await api.post(
            "contacts",
            body: [body],
            idempotent: true
        )
        guard let contact = created.first else {
            throw APIError.decoding(URLError(.cannotParseResponse))
        }
        withAnimation(.snappy) {
            contacts.insert(contact, at: 0)
            if let total = totalCount { totalCount = total + 1 }
            bumpCounts(created: true)
        }
        return contact
    }

    /// POST /contacts with MANY at once (paste-a-list onboarding). Returns the
    /// created contacts; prepends the ones that belong to the current view.
    @discardableResult
    func createMany(_ api: APIClient, bodies: [ContactCreateBody]) async throws -> [Contact] {
        guard !bodies.isEmpty else { return [] }
        let created: [Contact] = try await api.post("contacts", body: bodies, idempotent: true)
        withAnimation(.snappy) {
            let existing = Set(contacts.map(\.id))
            let fresh = created.filter { !existing.contains($0.id) }
            contacts.insert(contentsOf: fresh, at: 0)
            if let total = totalCount { totalCount = total + fresh.count }
            for _ in fresh { bumpCounts(created: true) }
        }
        return created
    }

    func delete(_ api: APIClient, id: String) async throws {
        let _: EmptyBody = try await api.delete("contacts/\(id)")
        withAnimation(.snappy) {
            contacts.removeAll { $0.id == id }
            if let total = totalCount, total > 0 { totalCount = total - 1 }
            bumpCounts(created: false)
        }
    }

    /// Fold an edited contact back into the visible list so the row updates
    /// without waiting for the realtime pulse.
    func apply(_ updated: Contact) {
        guard let index = contacts.firstIndex(where: { $0.id == updated.id }) else { return }
        withAnimation(.snappy) { contacts[index] = updated }
    }

    /// Drop a row that was deleted elsewhere (e.g. from the detail screen).
    func remove(id: String) {
        withAnimation(.snappy) {
            contacts.removeAll { $0.id == id }
            if let total = totalCount, total > 0 { totalCount = total - 1 }
            bumpCounts(created: false)
        }
    }

    // MARK: Bulk

    /// Campaigns for the bulk add/remove pickers (id + name), fetched once.
    func loadCampaignOptions(_ api: APIClient) async {
        guard campaignOptions.isEmpty else { return }
        if let page: ListResponse<ContactMiniCampaign> = try? await api.get("campaigns", query: ["limit": "100"]) {
            campaignOptions = page.data
        }
    }

    /// PATCH /contacts (bulk): apply membership/subscription changes to the ids.
    func bulkEdit(_ api: APIClient, body: ContactBulkEditBody) async throws {
        let _: [Contact] = try await api.patch("contacts", body: body)
    }

    /// DELETE /contacts (bulk) takes a bare array of ids, then drops them local.
    func bulkDelete(_ api: APIClient, ids: [String]) async throws {
        let _: EmptyBody = try await api.delete("contacts", body: ids)
        let removed = Set(ids)
        withAnimation(.snappy) {
            contacts.removeAll { removed.contains($0.id) }
            if let total = totalCount { totalCount = max(0, total - removed.count) }
        }
    }

    /// Optimistically nudge the drawer's total so the badge moves immediately;
    /// the next realtime reload reconciles every facet exactly. A new contact
    /// is subscribed and campaign-less by default, so it also lifts those.
    private func bumpCounts(created: Bool) {
        guard var c = counts else { return }
        if created {
            c.total += 1
            c.subscribed += 1
            c.notContacted += 1
        } else {
            c.total = max(0, c.total - 1)
        }
        counts = c
    }
}

// MARK: - Categories

/// The category list is not a standalone endpoint; it rides GET /auth/me.
/// The shared `User.categories` uses `name`, but the wire key is `title`, so
/// this refetches with the correct model.
@MainActor
@Observable
final class ContactCategoryStore {
    private(set) var categories: [ContactCategory] = []
    private(set) var isLoading = false

    func load(_ api: APIClient) async {
        isLoading = true
        defer { isLoading = false }
        if let me: ContactsMePayload = try? await api.get("auth/me") {
            let next = (me.categories ?? []).sorted {
                ($0.position ?? 0, $0.title ?? "") < ($1.position ?? 0, $1.title ?? "")
            }
            withAnimation(.snappy) { categories = next }
        }
    }
}

// MARK: - Lead status presentation

/// campaign_lead.status -> label + tone, mirroring the web leads pills.
enum ContactLeadStatus {
    static func label(_ status: String?) -> String {
        switch status {
        case "pending": return "Queued"
        case "active": return "Processing"
        case "replied": return "Replied"
        case "bounced": return "Bounced"
        case "unsubscribed": return "Unsubscribed"
        default: return status?.capitalized ?? "–"
        }
    }

    static func tone(_ status: String?) -> Tone {
        switch status {
        case "pending": return .slate
        case "active": return .sky
        case "replied": return .emerald
        case "bounced": return .rose
        case "unsubscribed": return .slate
        default: return .slate
        }
    }

    static func isLive(_ status: String?) -> Bool { status == "active" }
}

// MARK: - Category color parsing

enum ContactColor {
    /// Parses a `#rrggbb` hex string into a Color; falls back to a seeded tint.
    static func dot(_ hex: String?, seed: String) -> Color {
        if var raw = hex?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty {
            if raw.hasPrefix("#") { raw.removeFirst() }
            if raw.count == 6, let value = UInt32(raw, radix: 16) {
                return Color(hex: value)
            }
        }
        return WTheme.avatarColor(for: seed)
    }
}
