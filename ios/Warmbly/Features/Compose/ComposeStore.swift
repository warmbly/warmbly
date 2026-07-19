import SwiftUI

/// Backs one open compose window: mailbox candidates for the From picker,
/// the send call, and the autosaved working copy (client-generated draft id,
/// idempotent PUT on a debounce — web parity).
@MainActor
@Observable
final class ComposeStore {
    // MARK: Candidates

    private(set) var candidates: [ComposeCandidate] = []
    private(set) var recommendedAccountID: String?
    private(set) var recommendedReason: String?
    private(set) var contact: Contact?
    private(set) var suppression: ComposeSuppression?
    private(set) var candidatesLoaded = false

    private var candidatesGeneration = 0

    // MARK: Send / errors

    private(set) var isSending = false
    private(set) var errorMessage: String?

    // MARK: Draft autosave

    enum SaveState { case idle, saving, saved }

    /// Client-generated id; the same id on every PUT makes autosave idempotent.
    private(set) var draftID: String
    private(set) var saveState: SaveState = .idle
    private var lastSavedPayload: ComposeDraftPayload?
    private var draftPersisted: Bool
    private var saveTask: Task<Void, Never>?

    static let bodyLimit = 4000

    init(draft: ComposeDraft? = nil) {
        draftID = draft?.id ?? UUID().uuidString.lowercased()
        draftPersisted = draft != nil
        if let draft {
            lastSavedPayload = ComposeDraftPayload(
                emailAccountID: draft.emailAccountID ?? "",
                to: draft.to ?? [],
                cc: draft.cc ?? [],
                bcc: draft.bcc ?? [],
                subject: draft.subject ?? "",
                body: draft.body ?? ""
            )
            saveState = .saved
        }
    }

    func setError(_ message: String) { errorMessage = message }

    /// Scores every active mailbox against the recipient. Guarded by a
    /// generation counter so a stale response never overwrites a newer one.
    func loadCandidates(_ api: APIClient, to address: String) async {
        candidatesGeneration += 1
        let gen = candidatesGeneration
        do {
            let response: ComposeCandidatesResponse = try await api.get(
                "unibox/compose/candidates",
                query: ["to": address.isEmpty ? nil : address]
            )
            guard gen == candidatesGeneration else { return }
            withAnimation(.snappy) {
                candidates = response.accounts ?? []
                recommendedAccountID = response.recommendedAccountID
                recommendedReason = response.recommendedReason
                contact = response.contact
                suppression = response.suppression
                candidatesLoaded = true
            }
        } catch {
            guard gen == candidatesGeneration else { return }
            // Candidates are advisory; the picker just shows what it has.
            candidatesLoaded = true
        }
    }

    var recommendedCandidate: ComposeCandidate? {
        candidates.first { $0.id == recommendedAccountID } ?? candidates.first { $0.recommended == true }
    }

    // MARK: Autosave

    /// Debounced upsert; the empty draft is deleted instead of saved so
    /// abandoned windows don't litter the Drafts list.
    func scheduleAutosave(_ api: APIClient, payload: ComposeDraftPayload) {
        guard payload != lastSavedPayload else { return }
        saveTask?.cancel()
        saveTask = Task {
            try? await Task.sleep(for: .milliseconds(1200))
            guard !Task.isCancelled else { return }
            await save(api, payload: payload)
        }
    }

    /// Immediate flush for window close: persists the pending edit (or
    /// deletes an emptied draft) without waiting out the debounce.
    func flush(_ api: APIClient, payload: ComposeDraftPayload) {
        saveTask?.cancel()
        guard payload != lastSavedPayload else { return }
        Task { await save(api, payload: payload) }
    }

    private func save(_ api: APIClient, payload: ComposeDraftPayload) async {
        if payload.isEmpty {
            await deleteDraft(api)
            return
        }
        saveState = .saving
        do {
            struct UpsertResponse: Codable { var id: String? }
            let _: UpsertResponse = try await api.put("unibox/drafts/\(draftID)", body: payload)
            lastSavedPayload = payload
            draftPersisted = true
            saveState = .saved
        } catch {
            // Autosave failures are silent; the next edit retries.
            saveState = .idle
        }
    }

    /// Drops the working copy (after send, or when the draft was emptied).
    func deleteDraft(_ api: APIClient) async {
        saveTask?.cancel()
        lastSavedPayload = nil
        saveState = .idle
        guard draftPersisted else { return }
        draftPersisted = false
        struct DeleteResponse: Codable { var deleted: Bool? }
        let _: DeleteResponse? = try? await api.delete("unibox/drafts/\(draftID)")
    }

    // MARK: Send

    func send(_ api: APIClient, request: ComposeSendRequest) async throws -> ComposeSendResponse {
        isSending = true
        errorMessage = nil
        defer { isSending = false }
        let response: ComposeSendResponse = try await api.post(
            "unibox/compose",
            body: request,
            idempotent: true
        )
        await deleteDraft(api)
        return response
    }

    // MARK: Grounded AI draft

    func aiDraft(
        _ api: APIClient, to: String?, subject: String?, instruction: String?
    ) async throws -> ComposeAIDraftResponse {
        try await api.post(
            "unibox/compose/draft",
            body: ComposeAIDraftRequest(to: to, subject: subject, instruction: instruction),
            idempotent: true
        )
    }
}

// MARK: - Drafts list

/// Backs the Drafts screen: the member's autosaved compose drafts, newest
/// first (`GET /v1/unibox/drafts`, capped at 100 server-side).
@MainActor
@Observable
final class ComposeDraftsStore {
    private(set) var drafts: [ComposeDraft] = []
    private(set) var isLoading = false
    private(set) var hasLoaded = false
    private(set) var errorMessage: String?

    func load(_ api: APIClient) async {
        isLoading = true
        do {
            let page: ComposeDraftsPage = try await api.get("unibox/drafts")
            withAnimation(.snappy) { drafts = page.data ?? [] }
            errorMessage = nil
            hasLoaded = true
        } catch {
            if !hasLoaded { errorMessage = error.localizedDescription }
        }
        isLoading = false
    }

    func delete(_ api: APIClient, id: String) async {
        struct DeleteResponse: Codable { var deleted: Bool? }
        let _: DeleteResponse? = try? await api.delete("unibox/drafts/\(id)")
        withAnimation(.snappy) { drafts.removeAll { $0.id == id } }
    }
}
