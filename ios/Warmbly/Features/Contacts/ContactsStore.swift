import SwiftUI

/// Contacts list store: POST /contacts/search with an opaque cursor, keeping
/// stale rows visible during reloads and mirroring the campaigns pattern.
///
/// `pagination.total` is only computed on the first page (no cursor), so the
/// stat strip reads it from the first response and holds it across pages.
@MainActor
@Observable
final class ContactsStore {
    private(set) var contacts: [Contact] = []
    private(set) var totalCount: Int?
    private(set) var nextCursor: String?
    private(set) var hasMore = false
    private(set) var isLoading = false
    private(set) var isLoadingMore = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    /// Active category filter (single-select from the picker); nil = all.
    var categoryFilter: ContactCategory?
    var query = ""

    private var generation = 0

    var displayTotal: Int { totalCount ?? contacts.count }

    // MARK: Loading

    private func searchBody() -> ContactSearchBody {
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        return ContactSearchBody(
            query: trimmed.isEmpty ? nil : trimmed,
            categoryIDs: categoryFilter.map { [$0.id] }
        )
    }

    func load(_ api: APIClient) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            let page: ListResponse<Contact> = try await api.post(
                "contacts/search",
                body: searchBody(),
                query: ["limit": "50"]
            )
            guard gen == generation else { return }
            withAnimation(.snappy) {
                contacts = page.data
                // total is present only on the first page; hold what we have.
                totalCount = (page.pagination?.total).map(Int.init)
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
            let page: ListResponse<Contact> = try await api.post(
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
        }
        return contact
    }

    func delete(_ api: APIClient, id: String) async throws {
        let _: EmptyBody = try await api.delete("contacts/\(id)")
        withAnimation(.snappy) {
            contacts.removeAll { $0.id == id }
            if let total = totalCount, total > 0 { totalCount = total - 1 }
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
        }
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
