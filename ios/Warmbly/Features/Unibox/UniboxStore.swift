import SwiftUI

// MARK: - List page envelope

/// `GET /v1/unibox` envelope: `{ data, pagination { next_cursor, has_more } }`.
/// No `total`; the cursor is a plain message uuid.
private struct UniboxListPage: Codable, Sendable {
    var data: [UniboxMessage]?
    var pagination: UniboxCPagination?
}

/// `models.CPagination` — only `next_cursor` + `has_more`.
struct UniboxCPagination: Codable, Sendable {
    var nextCursor: String?
    var hasMore: Bool?

    enum CodingKeys: String, CodingKey {
        case nextCursor = "next_cursor"
        case hasMore = "has_more"
    }
}

private struct UniboxDataEnvelope<T: Codable & Sendable>: Codable, Sendable {
    var data: [T]?
}

// MARK: - Conversation list store

@MainActor
@Observable
final class UniboxStore {
    private(set) var overview: UniboxOverview?
    private(set) var messages: [UniboxMessage] = []
    private(set) var nextCursor: String?
    private(set) var hasMore = false
    private(set) var isLoading = false
    private(set) var isLoadingMore = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    var query = ""
    var scope: UniboxScope = .all

    /// Drawer hero stat; only fetched when the member can view analytics.
    var includeSentStats = false
    private(set) var sentToday: Int?

    /// All session categories (set by the root), used to resolve `label:`
    /// search operators to category ids.
    var knownLabels: [UniboxGroupCount] = []

    var isSearching: Bool { !query.trimmingCharacters(in: .whitespaces).isEmpty }

    private var generation = 0

    private static let pageLimit = 50

    // MARK: Derived counts

    var unreadCount: Int { overview?.unread ?? 0 }
    var allCount: Int { overview?.total ?? 0 }
    var todayCount: Int { overview?.today ?? 0 }
    var weekCount: Int { overview?.week ?? 0 }
    var awaitingCount: Int { overview?.awaitingReply ?? 0 }
    var snoozedCount: Int { overview?.snoozed ?? 0 }
    var scheduledCount: Int { overview?.scheduledPending ?? 0 }
    var mailboxes: [UniboxMailboxCount] { overview?.mailboxes ?? [] }
    var categories: [UniboxGroupCount] { overview?.categories ?? [] }

    // MARK: Query building

    /// Scope + search -> `GET /v1/unibox` params. The scope sets the base
    /// view; Gmail-style operators in the query add to or override it.
    private func params(cursor: String?) -> [String: String?] {
        var params: [String: String?] = ["limit": String(Self.pageLimit)]
        if let cursor { params["cursor"] = cursor }

        switch scope {
        case .all:
            break
        case .unread:
            params["unseen"] = "true"
        case .today:
            params["since"] = UniboxFormat.dayParam(daysBack: 0)
        case .week:
            params["since"] = UniboxFormat.dayParam(daysBack: 6)
        case .awaiting:
            params["awaiting_reply"] = "true"
        case .snoozed:
            params["snoozed"] = "true"
        case .scheduled:
            break // handled by the scheduled screen, not the list query
        case .uncategorized:
            params["uncategorized"] = "true"
        case let .mailbox(id, _):
            params["email_ids"] = id
        case let .category(id, _, _):
            params["category_ids"] = id
        }

        let parsed = UniboxQueryParser.parse(query)
        if !parsed.text.isEmpty { params["subject"] = parsed.text }
        if let subject = parsed.subject { params["subject"] = subject }
        if let from = parsed.from { params["from"] = from }
        if parsed.unread { params["unseen"] = "true" }
        if parsed.snoozed { params["snoozed"] = "true" }
        if parsed.awaiting { params["awaiting_reply"] = "true" }
        if parsed.uncategorized { params["uncategorized"] = "true" }
        if let after = parsed.after { params["since"] = after }
        if let before = parsed.before { params["until"] = before }

        if !parsed.labelNames.isEmpty {
            let ids = parsed.labelNames.compactMap { name in
                knownLabels.first { ($0.title ?? "").lowercased().hasPrefix(name) }?.id
            }
            if !ids.isEmpty {
                let existing = (params["category_ids"] ?? nil).map { [$0] } ?? []
                params["category_ids"] = (existing + ids).joined(separator: ",")
            }
        }
        if !parsed.mailboxTerms.isEmpty {
            let ids = mailboxes.filter { mailbox in
                parsed.mailboxTerms.contains { term in
                    (mailbox.email ?? "").lowercased().contains(term)
                        || (mailbox.name ?? "").lowercased().contains(term)
                }
            }.map(\.id)
            if !ids.isEmpty {
                let existing = (params["email_ids"] ?? nil).map { [$0] } ?? []
                params["email_ids"] = (existing + ids).joined(separator: ",")
            }
        }
        return params
    }

    // MARK: Loading

    func loadOverview(_ api: APIClient) async {
        overview = (try? await api.get("unibox/overview")) ?? overview
    }

    /// Fleet-wide sends today (campaign + warmup), summed from
    /// `GET /v1/analytics/accounts` for the drawer hero.
    func loadSentStats(_ api: APIClient) async {
        guard includeSentStats else { return }
        guard let page: ListResponse<AccountAnalytics> = try? await api.get("analytics/accounts") else { return }
        let total = page.data.reduce(0) {
            $0 + ($1.dailyUsage?.campaignSent ?? 0) + ($1.dailyUsage?.warmupSent ?? 0)
        }
        withAnimation(.snappy) { sentToday = total }
    }

    func load(_ api: APIClient, badges: AppBadges) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }

        await loadOverview(api)
        if let unread = overview?.unread { badges.uniboxUnread = unread }
        Task { await loadSentStats(api) }

        do {
            let page: UniboxListPage = try await api.get("unibox", query: params(cursor: nil))
            guard gen == generation else { return }
            withAnimation(.snappy) {
                messages = page.data ?? []
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
            errorMessage = nil
            hasLoaded = true
            isLoading = false
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
            let page: UniboxListPage = try await api.get("unibox", query: params(cursor: cursor))
            guard gen == generation else { return }
            let fresh = (page.data ?? []).filter { new in !messages.contains(where: { $0.id == new.id }) }
            withAnimation(.snappy) {
                messages.append(contentsOf: fresh)
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
        } catch {
            // Keep the list; the sentinel row retries on next appear.
        }
    }

    // MARK: Seen

    /// Bulk read/unread toggle (`PATCH /v1/unibox/seen`), optimistic. List rows
    /// carry the thread's newest message id, so this is per-row, not per-thread;
    /// opening a thread still clears every unseen message in it.
    func markSeen(_ api: APIClient, ids: [String], seen: Bool) async {
        withAnimation(.snappy) {
            for index in messages.indices where ids.contains(messages[index].id) {
                messages[index].seen = seen
                messages[index].hasUnread = !seen
            }
        }
        let _: UniboxMarkSeenRequest? = try? await api.patch(
            "unibox/seen",
            body: UniboxMarkSeenRequest(emailIDs: ids, seen: seen)
        )
        await loadOverview(api)
    }

    // MARK: Snooze / unsnooze

    func snooze(_ api: APIClient, threadKey: String, until: Date) async throws {
        let _: UniboxSnooze = try await api.post(
            "unibox/snooze",
            body: UniboxSnoozeRequest(threadID: threadKey, snoozedUntil: until),
            idempotent: false
        )
    }

    func unsnooze(_ api: APIClient, threadKey: String) async throws {
        let _: EmptyBody = try await api.delete("unibox/snooze", query: ["thread_id": threadKey])
    }

    /// Optimistically drop a row that no longer belongs in the current scope.
    func removeLocal(threadKey: String) {
        withAnimation(.snappy) {
            messages.removeAll { $0.threadKey == threadKey }
        }
    }
}

// MARK: - Thread store

@MainActor
@Observable
final class UniboxThreadStore {
    let thread: UniboxThread

    private(set) var messages: [UniboxMessage] = []
    private(set) var labels: [UniboxLabel] = []
    private(set) var bodies: [String: UniboxMessageDetail] = [:]
    private(set) var scheduled: [ScheduledSend] = []
    private(set) var isLoading = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false
    private(set) var didMarkSeen = false

    /// Expansion + measured body heights live here, not in row `@State`, so
    /// LazyVStack recycling can't silently collapse a message or lose its height.
    var expandedIDs: Set<String> = []
    var bodyHeights: [String: CGFloat] = [:]

    private var generation = 0

    init(thread: UniboxThread) {
        self.thread = thread
    }

    var subject: String {
        if let subject = thread.subject, !subject.isEmpty { return subject }
        return messages.first?.subject ?? "Conversation"
    }

    /// Sending mailbox for a reply: the receiving account of the newest message.
    var replyAccountID: String? {
        thread.mailboxID ?? messages.last?.emailID ?? messages.first?.emailID
    }

    var replyAccountEmail: String? {
        thread.mailboxEmail ?? messages.last?.recipientBare
    }

    /// The other party's address, for prefilling the reply `to`.
    var counterpartyAddress: String? {
        messages.last(where: { !($0.senderBare.isEmpty) })?.senderBare
            ?? messages.first?.senderBare
    }

    var latestMessageIDHeader: String? {
        bodies[messages.last?.id ?? ""]?.messageID
    }

    // MARK: Loading

    func load(_ api: APIClient) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            var query: [String: String?] = ["thread_id": thread.key]
            if let mailboxID = thread.mailboxID { query["email_id"] = mailboxID }
            let page: UniboxListPage = try await api.get("unibox/thread", query: query)
            guard gen == generation else { return }
            let rows = page.data ?? []
            withAnimation(.snappy) { messages = rows }
            // Open on the newest message, like a mail client.
            if expandedIDs.isEmpty, let last = rows.last?.id {
                expandedIDs.insert(last)
            }
            errorMessage = nil
            hasLoaded = true
            isLoading = false
            await loadLabels(api)
            await loadScheduled(api)
            await markSeen(api, rows: rows)
            await prefetchBodies(api)
        } catch {
            guard gen == generation else { return }
            errorMessage = error.localizedDescription
            isLoading = false
        }
    }

    func loadLabels(_ api: APIClient) async {
        let envelope: UniboxDataEnvelope<UniboxLabel>? = try? await api.get(
            "unibox/thread/labels", query: ["thread_id": thread.key]
        )
        if let data = envelope?.data { labels = data }
    }

    func loadScheduled(_ api: APIClient) async {
        let envelope: UniboxDataEnvelope<ScheduledSend>? = try? await api.get(
            "unibox/scheduled", query: ["thread_id": thread.key]
        )
        if let data = envelope?.data { scheduled = data }
    }

    /// Fetch the full HTML body for one message (`GET /v1/unibox/:id`), lazily.
    func loadBody(_ api: APIClient, messageID: String) async {
        guard bodies[messageID] == nil else { return }
        if let detail: UniboxMessageDetail = try? await api.get("unibox/\(messageID)") {
            bodies[messageID] = detail
        }
    }

    /// Warm every body so expansion is instant and `In-Reply-To` headers can
    /// resolve the "Replying to" references. Threads are small; sequential is fine.
    func prefetchBodies(_ api: APIClient) async {
        for id in messages.map(\.id) {
            await loadBody(api, messageID: id)
        }
    }

    /// Resolves which earlier message this one replied to: exact `In-Reply-To`
    /// header match when the bodies are loaded, chronological neighbor otherwise.
    func repliedToMessage(before index: Int) -> UniboxMessage? {
        guard index > 0, messages.indices.contains(index) else { return nil }
        if let headers = bodies[messages[index].id]?.inReplyTo {
            for header in headers {
                if let match = messages.first(where: {
                    $0.id != messages[index].id && bodies[$0.id]?.messageID == header
                }) {
                    return match
                }
            }
        }
        return messages[index - 1]
    }

    // MARK: Seen

    /// Opening a thread marks its unseen messages read (org-wide).
    private func markSeen(_ api: APIClient, rows: [UniboxMessage]) async {
        guard !didMarkSeen else { return }
        let unseen = rows.filter { $0.seen == false }.map(\.id)
        guard !unseen.isEmpty else { didMarkSeen = true; return }
        let _: UniboxMarkSeenRequest? = try? await api.patch(
            "unibox/seen",
            body: UniboxMarkSeenRequest(emailIDs: unseen, seen: true)
        )
        didMarkSeen = true
    }

    /// Per-message read/unread toggle from the message tools, optimistic.
    func setSeen(_ api: APIClient, messageID: String, seen: Bool) async {
        withAnimation(.snappy) {
            if let index = messages.firstIndex(where: { $0.id == messageID }) {
                messages[index].seen = seen
            }
        }
        let _: UniboxMarkSeenRequest? = try? await api.patch(
            "unibox/seen",
            body: UniboxMarkSeenRequest(emailIDs: [messageID], seen: seen)
        )
    }

    func cancelScheduled(_ api: APIClient, taskID: String) async throws {
        let _: EmptyBody = try await api.delete("unibox/scheduled/\(taskID)")
        withAnimation(.snappy) { scheduled.removeAll { $0.taskID == taskID } }
    }

    // MARK: Labels + snooze

    /// PUT semantics: replaces the conversation's full label set.
    func setLabels(_ api: APIClient, categoryIDs: [String]) async throws {
        let envelope: UniboxDataEnvelope<UniboxLabel> = try await api.put(
            "unibox/thread/labels",
            body: UniboxSetLabelsRequest(threadID: thread.key, categoryIDs: categoryIDs)
        )
        withAnimation(.snappy) { labels = envelope.data ?? [] }
    }

    func snooze(_ api: APIClient, until: Date) async throws {
        let _: UniboxSnooze = try await api.post(
            "unibox/snooze",
            body: UniboxSnoozeRequest(threadID: thread.key, snoozedUntil: until),
            idempotent: false
        )
    }
}

// MARK: - Scheduled sends store

@MainActor
@Observable
final class UniboxScheduledStore {
    private(set) var items: [ScheduledSend] = []
    private(set) var isLoading = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    private var generation = 0

    func load(_ api: APIClient) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            let envelope: UniboxDataEnvelope<ScheduledSend> = try await api.get("unibox/scheduled")
            guard gen == generation else { return }
            withAnimation(.snappy) { items = envelope.data ?? [] }
            errorMessage = nil
            hasLoaded = true
            isLoading = false
        } catch {
            guard gen == generation else { return }
            errorMessage = error.localizedDescription
            isLoading = false
        }
    }

    func cancel(_ api: APIClient, taskID: String) async throws {
        do {
            let _: EmptyBody = try await api.delete("unibox/scheduled/\(taskID)")
        } catch let error as APIError {
            // A repeat cancel (or one already fired) 404s; treat as gone.
            if case let .server(status, _) = error, status == 404 {
                withAnimation(.snappy) { items.removeAll { $0.taskID == taskID } }
                return
            }
            throw error
        }
        withAnimation(.snappy) { items.removeAll { $0.taskID == taskID } }
    }
}

// MARK: - Composer store

@MainActor
@Observable
final class UniboxComposerStore {
    private(set) var isSending = false
    private(set) var errorMessage: String?

    /// Web composer cap; keeps the plaintext body reasonable.
    static let bodyLimit = 4000

    func send(_ api: APIClient, request: UniboxReplyRequest) async throws -> UniboxSendResponse {
        isSending = true
        errorMessage = nil
        defer { isSending = false }
        let response: UniboxSendResponse = try await api.post(
            "unibox/reply",
            body: request,
            idempotent: true
        )
        return response
    }

    func setError(_ message: String) { errorMessage = message }
}
