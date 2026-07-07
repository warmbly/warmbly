import SwiftUI

/// Owns the contact header (GET /contacts/:id, hydrated with engagement +
/// suppression) plus the four section loaders. Each section paginates
/// independently and keeps stale rows during reloads.
@MainActor
@Observable
final class ContactDetailStore {
    private(set) var contact: Contact
    private(set) var headerLoaded = false

    init(contact: Contact) {
        self.contact = contact
    }

    var id: String { contact.id }

    /// GET /contacts/:id returns the ContactDetail (contact fields + engagement
    /// + suppression). Fold it into the header.
    func refreshHeader(_ api: APIClient) async {
        if let detail: Contact = try? await api.get("contacts/\(contact.id)") {
            withAnimation(.snappy) { contact = detail }
            headerLoaded = true
        }
    }

    func applyLocal(_ updated: Contact) {
        withAnimation(.snappy) { contact = updated }
    }

    // MARK: Emails (GET /contacts/:id/emails)

    private(set) var emails: [ContactEmailActivity] = []
    private(set) var emailsCursor: (at: String, id: String)?
    private(set) var emailsHasMore = false
    private(set) var emailsLoading = false
    private(set) var emailsLoaded = false
    private(set) var emailsError: String?

    func loadEmails(_ api: APIClient, reset: Bool = true) async {
        if reset { emailsLoading = true }
        do {
            var params: [String: String?] = ["limit": "50"]
            if !reset, let cursor = emailsCursor {
                params["before_at"] = cursor.at
                params["before_id"] = cursor.id
            }
            let page: ListResponse<ContactEmailActivity> = try await api.get(
                "contacts/\(contact.id)/emails",
                query: params
            )
            withAnimation(.snappy) {
                if reset {
                    emails = page.data
                } else {
                    let fresh = page.data.filter { new in !emails.contains(where: { $0.taskID == new.taskID }) }
                    emails.append(contentsOf: fresh)
                }
                emailsHasMore = page.pagination?.hasMore ?? false
                if let last = emails.last, let at = last.sentAtRaw {
                    emailsCursor = (at, last.taskID)
                } else {
                    emailsCursor = nil
                }
            }
            emailsError = nil
            emailsLoaded = true
        } catch {
            if !emailsLoaded { emailsError = error.localizedDescription }
        }
        emailsLoading = false
    }

    // MARK: Timeline (GET /contacts/:id/timeline)

    private(set) var timeline: [ContactTimelineEntry] = []
    private(set) var timelineBefore: String?
    private(set) var timelineHasMore = false
    private(set) var timelineLoading = false
    private(set) var timelineLoaded = false
    private(set) var timelineError: String?

    func loadTimeline(_ api: APIClient, reset: Bool = true) async {
        if reset { timelineLoading = true }
        do {
            var params: [String: String?] = ["limit": "50"]
            if !reset, let before = timelineBefore { params["before"] = before }
            let result: ContactTimelineResult = try await api.get(
                "contacts/\(contact.id)/timeline",
                query: params
            )
            let rows = result.data ?? []
            withAnimation(.snappy) {
                if reset {
                    timeline = rows
                } else {
                    timeline.append(contentsOf: rows)
                }
                timelineHasMore = result.hasMore ?? false
                timelineBefore = timeline.last?.atRaw
            }
            timelineError = nil
            timelineLoaded = true
        } catch {
            if !timelineLoaded { timelineError = error.localizedDescription }
        }
        timelineLoading = false
    }

    // MARK: Notes (GET/POST/PATCH/DELETE /contacts/:id/notes)

    private(set) var notes: [ContactNote] = []
    private(set) var notesCursor: String?
    private(set) var notesHasMore = false
    private(set) var notesLoading = false
    private(set) var notesLoaded = false
    private(set) var notesError: String?

    func loadNotes(_ api: APIClient, reset: Bool = true) async {
        if reset { notesLoading = true }
        do {
            var params: [String: String?] = ["limit": "50"]
            if !reset, let cursor = notesCursor { params["cursor"] = cursor }
            let page: ListResponse<ContactNote> = try await api.get(
                "contacts/\(contact.id)/notes",
                query: params
            )
            withAnimation(.snappy) {
                if reset {
                    notes = page.data
                } else {
                    let fresh = page.data.filter { new in !notes.contains(where: { $0.id == new.id }) }
                    notes.append(contentsOf: fresh)
                }
                notesCursor = page.pagination?.nextCursor
                notesHasMore = page.pagination?.hasMore ?? false
            }
            notesError = nil
            notesLoaded = true
        } catch {
            if !notesLoaded { notesError = error.localizedDescription }
        }
        notesLoading = false
    }

    func addNote(_ api: APIClient, content: String) async throws {
        let note: ContactNote = try await api.post(
            "contacts/\(contact.id)/notes",
            body: ContactNoteBody(content: content),
            idempotent: true
        )
        withAnimation(.snappy) { notes.insert(note, at: 0) }
    }

    func updateNote(_ api: APIClient, noteID: String, content: String) async throws {
        let note: ContactNote = try await api.patch(
            "contacts/\(contact.id)/notes/\(noteID)",
            body: ContactNoteBody(content: content)
        )
        if let index = notes.firstIndex(where: { $0.id == noteID }) {
            withAnimation(.snappy) { notes[index] = note }
        }
    }

    func deleteNote(_ api: APIClient, noteID: String) async throws {
        let _: EmptyBody = try await api.delete("contacts/\(contact.id)/notes/\(noteID)")
        withAnimation(.snappy) { notes.removeAll { $0.id == noteID } }
    }

    // MARK: Deals (GET /contacts/:id/deals, read-only bare array)

    private(set) var deals: [ContactDeal] = []
    private(set) var dealsLoading = false
    private(set) var dealsLoaded = false
    private(set) var dealsError: String?

    func loadDeals(_ api: APIClient) async {
        dealsLoading = true
        do {
            let rows: [ContactDeal] = try await api.get("contacts/\(contact.id)/deals")
            withAnimation(.snappy) { deals = rows }
            dealsError = nil
            dealsLoaded = true
        } catch {
            if !dealsLoaded { dealsError = error.localizedDescription }
        }
        dealsLoading = false
    }
}

// MARK: - Section enum

enum ContactDetailTab: String, CaseIterable, Identifiable {
    case emails, timeline, notes, deals

    var id: String { rawValue }

    var title: String {
        switch self {
        case .emails: return "Emails"
        case .timeline: return "Timeline"
        case .notes: return "Notes"
        case .deals: return "Deals"
        }
    }
}
