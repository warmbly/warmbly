import SwiftUI

// MARK: - Chat

/// One open assistant conversation: sends messages over the SSE stream
/// (`POST /ai/sessions/:id/messages`), folds text deltas and tool steps into
/// turn/block state as they arrive, pauses on approval_required, and resumes
/// through `/approve`. Cancelling the stream task aborts the request, which
/// cancels the run server-side (the stop mechanism).
@MainActor
@Observable
final class AssistantChatStore {
    struct Block: Identifiable {
        enum Kind { case text, tool }

        let id = UUID()
        var kind: Kind
        var text = ""
        /// Still receiving deltas; the closing full-text event clears it.
        var live = false
        var tool: String?
        var argsSummary: String?
        var result: String?
        var entityType: String?
        var entityID: String?
        var openURL: String?
        var done = false
    }

    struct Turn: Identifiable {
        let id = UUID()
        var role: String
        var blocks: [Block]
    }

    private(set) var session: AgentSession?
    private(set) var title: String?
    private(set) var turns: [Turn] = []
    private(set) var pending: AgentPendingTool?
    private(set) var isStreaming = false
    private(set) var isLoadingTranscript = false
    private(set) var freeModel = false
    private(set) var creditsRemaining: Int?
    private(set) var errorMessage: String?

    private var streamTask: Task<Void, Never>?

    var isEmpty: Bool { turns.isEmpty && session == nil }

    // MARK: Lifecycle

    func newChat() {
        stop()
        session = nil
        title = nil
        turns = []
        pending = nil
        freeModel = false
        errorMessage = nil
    }

    /// Rehydrates a conversation from its persisted transcript, matching the
    /// shape the live stream would have produced.
    func open(_ api: APIClient, session: AgentSession) async {
        stop()
        self.session = session
        title = session.displayTitle
        turns = []
        pending = nil
        errorMessage = nil
        isLoadingTranscript = true
        defer { isLoadingTranscript = false }
        do {
            let transcript: AgentTranscriptResponse = try await api.get("ai/sessions/\(session.id)/messages")
            title = transcript.title ?? title
            pending = transcript.pending
            freeModel = transcript.freeModel ?? false
            turns = (transcript.turns ?? []).map { turn in
                Turn(role: turn.role ?? "assistant", blocks: (turn.blocks ?? []).map { block in
                    Block(
                        kind: block.kind == "tool" ? .tool : .text,
                        text: block.text ?? "",
                        tool: block.tool,
                        argsSummary: block.argsSummary,
                        result: block.result,
                        entityType: block.entityType,
                        entityID: block.entityID,
                        openURL: block.openURL,
                        done: block.done ?? true
                    )
                })
            }
        } catch {
            errorMessage = friendlyError(error)
        }
    }

    func stop() {
        streamTask?.cancel()
        streamTask = nil
        markStreamEnded()
    }

    // MARK: Sending

    func send(_ api: APIClient, text: String, page: String? = nil) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, !isStreaming else { return }
        errorMessage = nil
        withAnimation(.snappy) {
            turns.append(Turn(role: "user", blocks: [Block(kind: .text, text: trimmed, done: true)]))
        }
        isStreaming = true
        streamTask = Task {
            do {
                if session == nil {
                    session = try await api.post(
                        "ai/sessions",
                        body: AgentSessionCreateRequest(page: page, resource: nil)
                    )
                }
                guard let sessionID = session?.id else { throw APIError.notConfigured }
                let stream = try await api.stream(
                    "ai/sessions/\(sessionID)/messages",
                    body: AgentMessageRequest(
                        messageID: UUID().uuidString,
                        text: trimmed,
                        page: page,
                        resource: nil
                    )
                )
                await consume(stream)
            } catch is CancellationError {
                // Stop already cleaned up.
            } catch {
                guard !Task.isCancelled else { return }
                errorMessage = friendlyError(error)
            }
            markStreamEnded()
        }
    }

    /// Resolves the paused tool: approve, deny, or always_allow. The same SSE
    /// stream shape continues the run from the approval point.
    func decide(_ api: APIClient, decision: String) {
        guard let sessionID = session?.id, !isStreaming else { return }
        withAnimation(.snappy) { pending = nil }
        errorMessage = nil
        isStreaming = true
        streamTask = Task {
            do {
                let stream = try await api.stream(
                    "ai/sessions/\(sessionID)/approve",
                    body: AgentApproveRequest(decision: decision)
                )
                await consume(stream)
            } catch is CancellationError {
            } catch {
                guard !Task.isCancelled else { return }
                errorMessage = friendlyError(error)
            }
            markStreamEnded()
        }
    }

    // MARK: Event folding

    private func consume(_ stream: AsyncThrowingStream<Data, Error>) async {
        do {
            for try await data in stream {
                guard !Task.isCancelled else { return }
                guard let event = try? APIClient.decoder.decode(AgentStreamEvent.self, from: data) else { continue }
                apply(event)
            }
        } catch {
            guard !Task.isCancelled else { return }
            if errorMessage == nil { errorMessage = friendlyError(error) }
        }
    }

    private func apply(_ event: AgentStreamEvent) {
        switch event.type {
        case "text_delta":
            appendDelta(event.text ?? "")
        case "text":
            closeTextBlock(with: event.text ?? "")
        case "tool_start":
            withAnimation(.snappy) {
                appendBlock(Block(
                    kind: .tool,
                    tool: event.tool,
                    argsSummary: event.argsSummary
                ))
            }
        case "tool_result":
            completeTool(event)
        case "approval_required":
            withAnimation(.snappy) {
                pending = AgentPendingTool(
                    toolCallID: event.toolCallID,
                    toolName: event.tool,
                    risk: event.risk,
                    argsSummary: event.argsSummary
                )
            }
        case "iteration":
            if let credits = event.creditsRemaining { creditsRemaining = credits }
        case "done":
            if let credits = event.creditsRemaining { creditsRemaining = credits }
            if let free = event.freeModel { freeModel = free }
        case "error":
            errorMessage = event.message ?? "Something went wrong."
        default:
            break
        }
    }

    private func ensureAssistantTurn() -> Int {
        if let last = turns.indices.last, turns[last].role == "assistant" {
            return last
        }
        turns.append(Turn(role: "assistant", blocks: []))
        return turns.indices.last!
    }

    private func appendBlock(_ block: Block) {
        let turn = ensureAssistantTurn()
        turns[turn].blocks.append(block)
    }

    private func appendDelta(_ delta: String) {
        let turn = ensureAssistantTurn()
        if let last = turns[turn].blocks.indices.last,
           turns[turn].blocks[last].kind == .text, turns[turn].blocks[last].live {
            turns[turn].blocks[last].text += delta
            return
        }
        var block = Block(kind: .text, text: delta)
        block.live = true
        turns[turn].blocks.append(block)
    }

    /// The authoritative full text always closes a block; it replaces the
    /// delta-built text so chunky providers still end up exact.
    private func closeTextBlock(with text: String) {
        let turn = ensureAssistantTurn()
        if let last = turns[turn].blocks.indices.last,
           turns[turn].blocks[last].kind == .text, turns[turn].blocks[last].live {
            turns[turn].blocks[last].text = text
            turns[turn].blocks[last].live = false
            turns[turn].blocks[last].done = true
            return
        }
        var block = Block(kind: .text, text: text)
        block.done = true
        turns[turn].blocks.append(block)
    }

    private func completeTool(_ event: AgentStreamEvent) {
        let turn = ensureAssistantTurn()
        let index = turns[turn].blocks.lastIndex { block in
            block.kind == .tool && !block.done && (event.tool == nil || block.tool == event.tool)
        }
        guard let index else { return }
        withAnimation(.snappy) {
            turns[turn].blocks[index].result = event.result
            turns[turn].blocks[index].entityType = event.entityType
            turns[turn].blocks[index].entityID = event.entityID
            turns[turn].blocks[index].openURL = event.openURL
            turns[turn].blocks[index].done = true
        }
    }

    /// Ends the streaming state and settles any half-open blocks.
    private func markStreamEnded() {
        isStreaming = false
        guard let turn = turns.indices.last, turns[turn].role == "assistant" else { return }
        for index in turns[turn].blocks.indices where turns[turn].blocks[index].live {
            turns[turn].blocks[index].live = false
            turns[turn].blocks[index].done = true
        }
    }

    private func friendlyError(_ error: Error) -> String {
        if let apiError = error as? APIError {
            switch apiError {
            case let .server(status, payload):
                if status == 402 { return "You're out of AI credits for this billing period." }
                return payload?.message ?? apiError.localizedDescription
            case .rateLimited:
                return "AI usage limit reached, try again later."
            default:
                return apiError.localizedDescription
            }
        }
        return error.localizedDescription
    }
}

// MARK: - History

/// The member's conversation list (`GET /ai/sessions`, newest first), grouped
/// into Today / Yesterday / This week / Earlier with a client-side title
/// filter — the mobile mirror of the web history rail.
@MainActor
@Observable
final class AssistantHistoryStore {
    private(set) var sessions: [AgentSession] = []
    private(set) var nextCursor: String?
    private(set) var hasMore = false
    private(set) var isLoading = false
    private(set) var hasLoaded = false
    private(set) var errorMessage: String?

    var query = ""

    func load(_ api: APIClient) async {
        isLoading = true
        do {
            let page: AgentSessionsPage = try await api.get("ai/sessions", query: ["limit": "50"])
            withAnimation(.snappy) {
                sessions = page.data ?? []
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
            errorMessage = nil
            hasLoaded = true
        } catch {
            if !hasLoaded { errorMessage = error.localizedDescription }
        }
        isLoading = false
    }

    func loadMore(_ api: APIClient) async {
        guard hasMore, let cursor = nextCursor, !isLoading else { return }
        do {
            let page: AgentSessionsPage = try await api.get(
                "ai/sessions",
                query: ["limit": "50", "cursor": cursor]
            )
            let fresh = (page.data ?? []).filter { new in !sessions.contains(where: { $0.id == new.id }) }
            withAnimation(.snappy) {
                sessions.append(contentsOf: fresh)
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
        } catch {
            // Sentinel row retries on next appear.
        }
    }

    func delete(_ api: APIClient, id: String) async {
        struct DeleteResponse: Codable { var message: String? }
        let _: DeleteResponse? = try? await api.delete("ai/sessions/\(id)")
        withAnimation(.snappy) { sessions.removeAll { $0.id == id } }
    }

    /// Wipes every conversation the member owns in this workspace.
    func clearAll(_ api: APIClient) async {
        struct ClearResponse: Codable { var deleted: Int? }
        let _: ClearResponse? = try? await api.delete("ai/sessions")
        withAnimation(.snappy) {
            sessions = []
            nextCursor = nil
            hasMore = false
        }
    }

    var filtered: [AgentSession] {
        let trimmed = query.trimmingCharacters(in: .whitespaces).lowercased()
        guard !trimmed.isEmpty else { return sessions }
        return sessions.filter { $0.displayTitle.lowercased().contains(trimmed) }
    }

    /// Today / Yesterday / This week / Earlier, in that order.
    var grouped: [(title: String, sessions: [AgentSession])] {
        let calendar = Calendar.current
        var buckets: [String: [AgentSession]] = [:]
        for session in filtered {
            let date = session.updatedAt ?? session.createdAt ?? .distantPast
            let key: String
            if calendar.isDateInToday(date) {
                key = "Today"
            } else if calendar.isDateInYesterday(date) {
                key = "Yesterday"
            } else if let weekAgo = calendar.date(byAdding: .day, value: -7, to: .now), date > weekAgo {
                key = "This week"
            } else {
                key = "Earlier"
            }
            buckets[key, default: []].append(session)
        }
        return ["Today", "Yesterday", "This week", "Earlier"].compactMap { key in
            guard let group = buckets[key], !group.isEmpty else { return nil }
            return (title: key, sessions: group)
        }
    }
}
