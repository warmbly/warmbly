import SwiftUI

// The four contact-detail sections. Each: SkeletonRows on first load, stale
// data during reloads, ErrorStateView on failure, EmptyStateView when empty,
// .refreshable, and reload on the relevant realtime pulse.

// MARK: - Emails

struct ContactEmailsSection: View {
    @Environment(AppEnvironment.self) private var env
    let store: ContactDetailStore

    var body: some View {
        Group {
            if store.emailsLoading, !store.emailsLoaded {
                ScrollView { SkeletonRows(rows: 6) }
            } else if let error = store.emailsError, store.emails.isEmpty {
                ErrorStateView(title: "Couldn't load emails", message: error) {
                    await store.loadEmails(env.api)
                }
            } else if store.emails.isEmpty {
                EmptyStateView(title: "No emails", message: "Sent emails to this contact will show here.")
            } else {
                List {
                    ForEach(store.emails) { email in
                        ContactEmailRow(email: email)
                    }
                    if store.emailsHasMore {
                        loadMoreRow { await store.loadEmails(env.api, reset: false) }
                    }
                }
                .listStyle(.plain)
                .refreshable { await store.loadEmails(env.api) }
            }
        }
        .task { if !store.emailsLoaded { await store.loadEmails(env.api) } }
        .onChange(of: env.realtime.pulse(for: .campaigns)) {
            Task { await store.loadEmails(env.api) }
        }
    }
}

struct ContactEmailRow: View {
    let email: ContactEmailActivity

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(email.subject?.isEmpty == false ? email.subject! : "(no subject)")
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                Spacer(minLength: 8)
                if let sent = email.sentAt {
                    Text(WFormat.relative(sent))
                        .font(.footnote)
                        .monospacedDigit()
                        .foregroundStyle(.tertiary)
                }
            }
            HStack(spacing: 6) {
                if let account = email.emailAccountEmail, !account.isEmpty {
                    Text(account)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                if let campaign = email.campaignName, !campaign.isEmpty {
                    Text("·").foregroundStyle(.tertiary).font(.subheadline)
                    Text(campaign)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer(minLength: 4)
            }
            engagementChips
        }
        .padding(.vertical, 6)
    }

    @ViewBuilder
    private var engagementChips: some View {
        HStack(spacing: 6) {
            if email.bouncedAt != nil {
                miniChip("Bounced", tone: .rose)
            }
            if email.repliedAt != nil {
                miniChip("Replied", tone: .emerald)
            }
            if email.clickedAt != nil {
                miniChip("Clicked", tone: .sky)
            }
            if email.openedAt != nil {
                miniChip("Opened", tone: .sky)
            }
            if email.openedAt == nil, email.clickedAt == nil, email.repliedAt == nil, email.bouncedAt == nil {
                miniChip("Sent", tone: .slate)
            }
            Spacer(minLength: 0)
        }
    }

    private func miniChip(_ text: String, tone: Tone) -> some View {
        Text(text)
            .font(.system(size: 11, weight: .semibold))
            .foregroundStyle(tone.color)
            .padding(.horizontal, 7)
            .padding(.vertical, 2.5)
            .background(tone.background, in: Capsule())
    }
}

// MARK: - Timeline

struct ContactTimelineSection: View {
    @Environment(AppEnvironment.self) private var env
    let store: ContactDetailStore

    var body: some View {
        Group {
            if store.timelineLoading, !store.timelineLoaded {
                ScrollView { SkeletonRows(rows: 6) }
            } else if let error = store.timelineError, store.timeline.isEmpty {
                ErrorStateView(title: "Couldn't load timeline", message: error) {
                    await store.loadTimeline(env.api)
                }
            } else if store.timeline.isEmpty {
                EmptyStateView(title: "No activity", message: "This contact's activity will appear here over time.")
            } else {
                List {
                    ForEach(Array(store.timeline.enumerated()), id: \.offset) { _, entry in
                        ContactTimelineRow(entry: entry)
                    }
                    if store.timelineHasMore {
                        loadMoreRow { await store.loadTimeline(env.api, reset: false) }
                    }
                }
                .listStyle(.plain)
                .refreshable { await store.loadTimeline(env.api) }
            }
        }
        .task { if !store.timelineLoaded { await store.loadTimeline(env.api) } }
        .onChange(of: env.realtime.pulse(for: .contacts)) {
            Task { await store.loadTimeline(env.api) }
        }
    }
}

struct ContactTimelineRow: View {
    let entry: ContactTimelineEntry

    private var icon: (name: String, tone: Tone) {
        switch entry.type {
        case "email_sent": return ("paperplane", .slate)
        case "email_opened": return ("envelope.open", .sky)
        case "email_clicked": return ("cursorarrow.rays", .sky)
        case "email_replied", "reply_received": return ("arrowshape.turn.up.left", .emerald)
        case "email_bounced": return ("exclamationmark.triangle", .rose)
        case "deliverability": return ("shield", .amber)
        case "suppressed": return ("nosign", .rose)
        case "note": return ("note.text", .indigo)
        case "meeting_booked": return ("calendar.badge.plus", .emerald)
        case "meeting_rescheduled": return ("calendar.badge.clock", .amber)
        case "meeting_canceled": return ("calendar.badge.minus", .rose)
        default: return ("circle", .slate)
        }
    }

    private var title: String {
        switch entry.type {
        case "email_sent": return "Email sent"
        case "email_opened": return "Email opened"
        case "email_clicked": return "Link clicked"
        case "email_replied", "reply_received": return "Reply received"
        case "email_bounced": return "Bounced"
        case "deliverability": return "Deliverability"
        case "suppressed": return "Suppressed"
        case "note": return "Note added"
        case "meeting_booked": return "Meeting booked"
        case "meeting_rescheduled": return "Meeting rescheduled"
        case "meeting_canceled": return "Meeting canceled"
        default: return entry.type?.replacingOccurrences(of: "_", with: " ").capitalized ?? "Activity"
        }
    }

    private var detail: String? {
        if let subject = entry.subject, !subject.isEmpty { return subject }
        if let content = entry.content, !content.isEmpty { return content }
        if let campaign = entry.campaignName, !campaign.isEmpty { return campaign }
        if let reason = entry.reason, !reason.isEmpty { return reason }
        return entry.emailAccountEmail
    }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            IconTile(symbol: icon.name, tone: icon.tone, size: 34)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(title)
                        .font(.body.weight(.medium))
                    Spacer(minLength: 8)
                    if let at = entry.at {
                        Text(WFormat.relative(at))
                            .font(.footnote)
                            .monospacedDigit()
                            .foregroundStyle(.tertiary)
                    }
                }
                if let detail {
                    Text(detail)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }
        }
        .padding(.vertical, 4)
    }
}

// MARK: - Notes

struct ContactNotesSection: View {
    @Environment(AppEnvironment.self) private var env
    let store: ContactDetailStore

    @State private var draft = ""
    @State private var editing: ContactNote?
    @State private var editText = ""
    @State private var pendingDelete: ContactNote?
    @State private var actionError: String?
    @State private var submitting = false

    private var canManage: Bool { env.session.can(.manageContacts) }

    var body: some View {
        VStack(spacing: 0) {
            list
            if canManage {
                Divider()
                composer
            }
        }
        .task { if !store.notesLoaded { await store.loadNotes(env.api) } }
        .onChange(of: env.realtime.pulse(for: .crm)) {
            Task { await store.loadNotes(env.api) }
        }
        .alert("Delete note?", isPresented: Binding(
            get: { pendingDelete != nil },
            set: { if !$0 { pendingDelete = nil } }
        )) {
            Button("Delete", role: .destructive) {
                if let note = pendingDelete { Task { await delete(note) } }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This can't be undone.")
        }
        .alert("Couldn't save note", isPresented: Binding(
            get: { actionError != nil },
            set: { if !$0 { actionError = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(actionError ?? "")
        }
        .sheet(item: $editing) { note in
            editSheet(note)
        }
    }

    @ViewBuilder
    private var list: some View {
        if store.notesLoading, !store.notesLoaded {
            ScrollView { SkeletonRows(rows: 4) }
        } else if let error = store.notesError, store.notes.isEmpty {
            ErrorStateView(title: "Couldn't load notes", message: error) {
                await store.loadNotes(env.api)
            }
        } else if store.notes.isEmpty {
            EmptyStateView(
                title: "No notes",
                message: canManage ? "Add the first note below." : "Notes your team adds will show here."
            )
        } else {
            List {
                ForEach(store.notes) { note in
                    ContactNoteRow(note: note)
                        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                            if canManage {
                                Button(role: .destructive) {
                                    pendingDelete = note
                                } label: {
                                    Label("Delete", systemImage: "trash")
                                }
                                Button {
                                    editText = note.content ?? ""
                                    editing = note
                                } label: {
                                    Label("Edit", systemImage: "pencil")
                                }
                                .tint(WTheme.accent)
                            }
                        }
                }
                if store.notesHasMore {
                    loadMoreRow { await store.loadNotes(env.api, reset: false) }
                }
            }
            .listStyle(.plain)
            .refreshable { await store.loadNotes(env.api) }
        }
    }

    private var composer: some View {
        HStack(alignment: .bottom, spacing: 8) {
            TextField("Add a note", text: $draft, axis: .vertical)
                .font(.body)
                .lineLimit(1 ... 4)
                .padding(.horizontal, 12)
                .padding(.vertical, 9)
                .background(Tone.slate.background, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            Button {
                Task { await submit() }
            } label: {
                if submitting {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.system(size: 30))
                        .foregroundStyle(canSubmit ? WTheme.accent : Color(.tertiaryLabel))
                }
            }
            .disabled(!canSubmit || submitting)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
    }

    private var canSubmit: Bool {
        !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func editSheet(_ note: ContactNote) -> some View {
        NavigationStack {
            VStack {
                TextField("Note", text: $editText, axis: .vertical)
                    .font(.body)
                    .lineLimit(3 ... 12)
                    .padding(12)
                    .background(Tone.slate.background, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                    .padding(16)
                Spacer()
            }
            .navigationTitle("Edit note")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { editing = nil }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task { await saveEdit(note) }
                    }
                    .disabled(editText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
        }
        .presentationDetents([.medium])
        .presentationDragIndicator(.visible)
    }

    private func submit() async {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        submitting = true
        defer { submitting = false }
        do {
            try await store.addNote(env.api, content: text)
            draft = ""
        } catch {
            actionError = error.localizedDescription
        }
    }

    private func saveEdit(_ note: ContactNote) async {
        let text = editText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        do {
            try await store.updateNote(env.api, noteID: note.id, content: text)
            editing = nil
        } catch {
            actionError = error.localizedDescription
        }
    }

    private func delete(_ note: ContactNote) async {
        do {
            try await store.deleteNote(env.api, noteID: note.id)
        } catch {
            actionError = error.localizedDescription
        }
    }
}

struct ContactNoteRow: View {
    let note: ContactNote

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(note.content ?? "")
                .font(.body)
                .foregroundStyle(.primary)
            if let date = note.updatedAt ?? note.createdAt {
                Text(WFormat.relative(date))
                    .font(.footnote)
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.vertical, 5)
    }
}

// MARK: - Deals (read-only)

struct ContactDealsSection: View {
    @Environment(AppEnvironment.self) private var env
    let store: ContactDetailStore

    var body: some View {
        Group {
            if store.dealsLoading, !store.dealsLoaded {
                ScrollView { SkeletonRows(rows: 4) }
            } else if let error = store.dealsError, store.deals.isEmpty {
                ErrorStateView(title: "Couldn't load deals", message: error) {
                    await store.loadDeals(env.api)
                }
            } else if store.deals.isEmpty {
                EmptyStateView(title: "No deals", message: "Deals linked to this contact will show here.")
            } else {
                List {
                    ForEach(store.deals) { deal in
                        ContactDealRow(deal: deal)
                    }
                }
                .listStyle(.plain)
                .refreshable { await store.loadDeals(env.api) }
            }
        }
        .task { if !store.dealsLoaded { await store.loadDeals(env.api) } }
        .onChange(of: env.realtime.pulse(for: .crm)) {
            Task { await store.loadDeals(env.api) }
        }
    }
}

struct ContactDealRow: View {
    let deal: ContactDeal

    private var tone: Tone {
        switch deal.status {
        case "won": return .emerald
        case "lost": return .rose
        default: return .sky
        }
    }

    private var valueLabel: String? {
        guard let value = deal.value else { return nil }
        return value.formatted(.currency(code: (deal.currency?.isEmpty == false ? deal.currency! : "USD")).precision(.fractionLength(value.truncatingRemainder(dividingBy: 1) == 0 ? 0 : 2)))
    }

    var body: some View {
        HStack(spacing: 12) {
            IconTile(symbol: "briefcase", tone: tone, size: 34)
            VStack(alignment: .leading, spacing: 3) {
                Text(deal.name?.isEmpty == false ? deal.name! : "Untitled deal")
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                if let value = valueLabel {
                    Text(value)
                        .font(.footnote)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 8)
            StatusPill(text: (deal.status ?? "open").capitalized, tone: tone)
        }
        .padding(.vertical, 5)
    }
}

// MARK: - Shared load-more row

@ViewBuilder
func loadMoreRow(_ action: @escaping @Sendable @MainActor () async -> Void) -> some View {
    HStack {
        Spacer()
        ProgressView().controlSize(.small)
        Spacer()
    }
    .listRowSeparator(.hidden)
    .onAppear { Task { @MainActor in await action() } }
}
