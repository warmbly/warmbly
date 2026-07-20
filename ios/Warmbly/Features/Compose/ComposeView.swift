import SwiftUI
import UIKit

/// New-email composer, the iOS port of the web compose window. Sends through
/// `POST /v1/unibox/compose` (instant, smart, or scheduled) with backend auto
/// mailbox selection by default; the From picker shows every mailbox scored
/// against the recipient. The draft autosaves as a per-user working copy
/// (close keeps the draft, send deletes it), and the AI bar produces grounded
/// drafts that can come back as a clarifying question instead of a pitch.
struct ComposeView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// Resumed draft; nil starts a fresh window.
    var draft: ComposeDraft?
    /// Called after a successful send with the snapshot of what went out, so
    /// the caller can toast/refresh and offer undo-with-restore.
    var onSent: (ComposeSendResponse, ComposeDraftPayload) -> Void

    @State private var store: ComposeStore
    @State private var to: [String]
    @State private var toText = ""
    @State private var cc: [String]
    @State private var ccText = ""
    @State private var bcc: [String]
    @State private var bccText = ""
    @State private var subject: String
    @State private var messageBody: String
    @State private var showCcBcc: Bool
    /// Selected sender id; empty means Auto (backend picks the best mailbox).
    @State private var fromAccountID: String
    /// Tag scope for Auto: the backend picks among mailboxes carrying this
    /// tag. Session-only (not persisted with the draft), web parity.
    @State private var autoTagID: String?
    @State private var sendMode: UniboxSendMode = .instant
    @State private var scheduledAt = Date().addingTimeInterval(3600)
    @State private var showSchedulePicker = false
    @State private var showFromPicker = false
    /// Send succeeded: skip the close-flush so the deleted draft stays gone.
    @State private var sentOrDiscarded = false

    /// Grounded AI draft flow: request lifecycle, the clarifying-question
    /// phase, and the body snapshot restored on Discard/Retry.
    private enum AIDraftPhase { case idle, generating, question, review }
    @State private var draftPhase: AIDraftPhase = .idle
    @State private var draftStage = 0
    @State private var draftUsage: String?
    @State private var draftGrounding: String?
    @State private var pendingQuestion: String?
    @State private var questionAnswer = ""
    @State private var baseInstruction: String?
    @State private var preDraftBody: String?
    @State private var draftTask: Task<Void, Never>?
    @State private var adjustOpen = false
    @State private var adjustText = ""
    @State private var draftDonePulse = 0
    @FocusState private var answerFocused: Bool
    @FocusState private var adjustFocused: Bool

    /// Inline instruction bar (the "Tell AI what this email should do" field).
    @State private var aiBarVisible = false
    @State private var aiPrompt = ""
    @State private var isRewriting = false
    @State private var aiUndo: String?
    @State private var selection: TextSelection?
    @FocusState private var aiFocused: Bool

    /// Selection captured for a custom instruction ("Editing selection" mode);
    /// carries the body it was captured from so a stale range never slices.
    private struct EditingSelection {
        var range: Range<String.Index>
        var body: String
    }

    @State private var editingSelection: EditingSelection?

    init(draft: ComposeDraft? = nil, onSent: @escaping (ComposeSendResponse, ComposeDraftPayload) -> Void = { _, _ in }) {
        self.draft = draft
        self.onSent = onSent
        _store = State(initialValue: ComposeStore(draft: draft))
        _to = State(initialValue: draft?.to ?? [])
        _cc = State(initialValue: draft?.cc ?? [])
        _bcc = State(initialValue: draft?.bcc ?? [])
        _subject = State(initialValue: draft?.subject ?? "")
        _messageBody = State(initialValue: draft?.body ?? "")
        _showCcBcc = State(initialValue: !(draft?.cc ?? []).isEmpty || !(draft?.bcc ?? []).isEmpty)
        _fromAccountID = State(initialValue: draft?.emailAccountID ?? "")
    }

    private var canAI: Bool { env.session.can(.useAI) }

    private var allTo: [String] { to + parse(toText) }
    private var allCc: [String] { cc + parse(ccText) }
    private var allBcc: [String] { bcc + parse(bccText) }

    /// First recipient; drives candidate scoring and AI grounding.
    private var primaryRecipient: String { allTo.first ?? "" }

    private var canSend: Bool {
        !allTo.isEmpty
            && !subject.trimmingCharacters(in: .whitespaces).isEmpty
            && !messageBody.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && !store.isSending
    }

    private var draftPayload: ComposeDraftPayload {
        ComposeDraftPayload(
            emailAccountID: fromAccountID,
            to: allTo,
            cc: allCc,
            bcc: allBcc,
            subject: subject,
            body: messageBody
        )
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                fromRow
                hairline
                RecipientField(label: "To", addresses: $to, text: $toText) {
                    if !showCcBcc {
                        Button {
                            withAnimation(.snappy) { showCcBcc = true }
                        } label: {
                            Image(systemName: "chevron.down")
                                .font(.system(size: 12, weight: .semibold))
                                .foregroundStyle(.secondary)
                                .frame(width: 28, height: 28)
                        }
                        .accessibilityLabel("Add Cc and Bcc")
                    }
                }
                hairline
                if showCcBcc {
                    RecipientField(label: "Cc", addresses: $cc, text: $ccText)
                    hairline
                    RecipientField(label: "Bcc", addresses: $bcc, text: $bccText)
                    hairline
                }
                TextField("Subject", text: $subject)
                    .font(.subheadline.weight(.medium))
                    .padding(.horizontal, 16)
                    .frame(minHeight: 46)
                hairline
                if let suppression = store.suppression, !primaryRecipient.isEmpty {
                    suppressionBanner(suppression)
                    hairline
                }
                if sendMode != .instant {
                    scheduleBanner
                    hairline
                }
                if let error = store.errorMessage {
                    Text(error)
                        .font(.footnote)
                        .foregroundStyle(WTheme.negative)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 8)
                    hairline
                }
                editor
            }
            .background(Color(.systemBackground))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbarContent }
            .safeAreaInset(edge: .bottom) { helperBar }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
        .interactiveDismissDisabled(store.isSending)
        .sensoryFeedback(.impact(weight: .light), trigger: draftDonePulse)
        .sheet(isPresented: $showSchedulePicker) {
            ComposeScheduleSheet(isPresented: $showSchedulePicker, scheduledAt: $scheduledAt) {
                withAnimation(.snappy) { sendMode = .scheduled }
            }
        }
        .sheet(isPresented: $showFromPicker) {
            ComposeFromPicker(store: store, selection: $fromAccountID, autoTagID: $autoTagID)
        }
        .task(id: primaryRecipient) {
            // Debounced rescoring as the recipient settles.
            if store.candidatesLoaded {
                try? await Task.sleep(for: .milliseconds(400))
            }
            guard !Task.isCancelled else { return }
            await store.loadCandidates(env.api, to: primaryRecipient)
        }
        .onChange(of: draftPayload) {
            guard !sentOrDiscarded else { return }
            store.scheduleAutosave(env.api, payload: draftPayload)
        }
        .onDisappear {
            draftTask?.cancel()
            guard !sentOrDiscarded else { return }
            // Close keeps the draft: flush the pending edit immediately.
            store.flush(env.api, payload: draftPayload)
        }
    }

    // MARK: Toolbar

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            Button {
                dismiss()
            } label: {
                Image(systemName: "xmark")
            }
            .accessibilityLabel("Close")
        }
        ToolbarItem(placement: .principal) {
            VStack(spacing: 0) {
                Text("New email")
                    .font(.headline)
                if store.saveState != .idle {
                    Text(store.saveState == .saving ? "Saving…" : "Saved")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .contentTransition(.opacity)
                }
            }
        }
        ToolbarItemGroup(placement: .topBarTrailing) {
            if store.isSending {
                ProgressView()
            } else {
                Button {
                    Task { await send() }
                } label: {
                    Image(systemName: sendMode == .scheduled ? "clock.badge.checkmark" : "paperplane.fill")
                }
                .disabled(!canSend)
                .accessibilityLabel(sendMode == .instant ? "Send" : sendMode == .smart ? "Queue" : "Schedule")
            }
            Menu {
                Picker("When to send", selection: $sendMode) {
                    Label("Send now", systemImage: "paperplane").tag(UniboxSendMode.instant)
                    Label("Smart send", systemImage: "wand.and.stars").tag(UniboxSendMode.smart)
                    if sendMode == .scheduled {
                        Label("Scheduled", systemImage: "clock").tag(UniboxSendMode.scheduled)
                    }
                }
                Button {
                    showSchedulePicker = true
                } label: {
                    Label(
                        sendMode == .scheduled ? "Change send time" : "Schedule send",
                        systemImage: "clock"
                    )
                }
                if canAI {
                    Button {
                        withAnimation(.snappy) { aiBarVisible = true }
                    } label: {
                        Label("Write with AI", systemImage: "sparkles")
                    }
                }
                Button(role: .destructive) {
                    sentOrDiscarded = true
                    Task { await store.deleteDraft(env.api) }
                    dismiss()
                } label: {
                    Label("Discard draft", systemImage: "trash")
                }
            } label: {
                Image(systemName: "ellipsis")
            }
            .sensoryFeedback(.selection, trigger: sendMode)
        }
    }

    // MARK: Rows

    private var hairline: some View { Divider() }

    /// From row: the chosen sender, or the Auto pick with the live
    /// recommendation as it scores against the typed recipient.
    private var fromRow: some View {
        Button {
            showFromPicker = true
        } label: {
            HStack(spacing: 12) {
                Text("From")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .frame(width: 44, alignment: .leading)
                if fromAccountID.isEmpty {
                    HStack(spacing: 6) {
                        Image(systemName: "wand.and.stars")
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundStyle(WTheme.accent)
                        Text(autoLabel)
                            .font(.subheadline)
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                    }
                } else {
                    Text(selectedCandidate?.email ?? "Mailbox")
                        .font(.subheadline)
                        .foregroundStyle(.primary)
                        .lineLimit(1)
                }
                Spacer(minLength: 8)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 16)
            .frame(minHeight: 46)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Choose sending mailbox")
    }

    private var selectedCandidate: ComposeCandidate? {
        store.candidates.first { $0.id == fromAccountID }
    }

    private var autoLabel: String {
        if let autoTagID,
           let tag = env.session.user?.tags?.first(where: { $0.id == autoTagID }) {
            let name = tag.name ?? "tag"
            if let best = store.bestCandidate(inTag: autoTagID) {
                return "Auto in \(name) · \(best.email)"
            }
            return "Auto in \(name)"
        }
        if let recommended = store.recommendedCandidate {
            return "Auto · \(recommended.email)"
        }
        return "Auto · best mailbox"
    }

    private func suppressionBanner(_ suppression: ComposeSuppression) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(WTheme.negative)
            Text("\(primaryRecipient) is suppressed\(suppression.reason.map { " (\($0))" } ?? "") — sending will be rejected")
                .font(.footnote)
                .foregroundStyle(WTheme.negative)
                .lineLimit(2)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 16)
        .frame(minHeight: 40)
    }

    private var scheduleBanner: some View {
        HStack(spacing: 8) {
            Image(systemName: sendMode == .scheduled ? "clock" : "wand.and.stars")
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(WTheme.accent)
            Text(
                sendMode == .scheduled
                    ? "Scheduled for \(scheduledAt.formatted(date: .abbreviated, time: .shortened))"
                    : "Smart send: goes out at the next safe slot for the mailbox"
            )
            .font(.footnote)
            .foregroundStyle(.secondary)
            .lineLimit(1)
            Spacer(minLength: 8)
            Button {
                withAnimation(.snappy) { sendMode = .instant }
            } label: {
                Image(systemName: "xmark.circle.fill")
                    .font(.system(size: 15))
                    .foregroundStyle(.tertiary)
            }
            .accessibilityLabel("Send now instead")
        }
        .padding(.horizontal, 16)
        .frame(minHeight: 40)
        .contentShape(Rectangle())
        .onTapGesture {
            if sendMode == .scheduled { showSchedulePicker = true }
        }
    }

    // MARK: Editor

    private var editor: some View {
        TextEditor(text: $messageBody, selection: $selection)
            .font(.body)
            .scrollContentBackground(.hidden)
            .padding(.horizontal, 11)
            .padding(.top, 6)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .overlay(alignment: .topLeading) {
                if messageBody.isEmpty {
                    Text("Compose email")
                        .font(.body)
                        .foregroundStyle(.tertiary)
                        .padding(.top, 14)
                        .padding(.leading, 16)
                        .allowsHitTesting(false)
                }
            }
            .onChange(of: messageBody) {
                if messageBody.count > ComposeStore.bodyLimit {
                    messageBody = String(messageBody.prefix(ComposeStore.bodyLimit))
                }
            }
    }

    // MARK: Helper bar

    @ViewBuilder
    private var helperBar: some View {
        VStack(spacing: 0) {
            Divider()
            switch draftPhase {
            case .generating:
                draftStatusRow
            case .question:
                questionBar
            case .review:
                draftReviewBar
            case .idle:
                if aiBarVisible {
                    aiPromptBar
                } else {
                    helperRow
                }
            }
        }
        .background(.bar)
    }

    private var helperRow: some View {
        HStack(spacing: 18) {
            if canAI { aiMenu }
            if aiUndo != nil {
                Button {
                    withAnimation(.snappy) {
                        messageBody = aiUndo ?? messageBody
                        aiUndo = nil
                    }
                } label: {
                    Label("Undo", systemImage: "arrow.uturn.backward")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(TapScaleStyle())
                .transition(.opacity)
            }
            Spacer(minLength: 0)
            if isRewriting {
                ProgressView().controlSize(.small)
            } else {
                Text("\(messageBody.count) / \(ComposeStore.bodyLimit)")
                    .font(.caption2)
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
    }

    /// Text the user has highlighted in the editor, if any — AI actions then
    /// target just that range instead of the whole draft.
    private var selectedTextRange: Range<String.Index>? {
        guard let selection, !selection.isInsertion,
              case let .selection(range) = selection.indices,
              !range.isEmpty else { return nil }
        return range
    }

    /// AI dropdown: grounded draft, selection rewrites when text is
    /// highlighted, and quick rewrites of the whole body otherwise.
    private var aiMenu: some View {
        Menu {
            Button {
                startAIDraft(instruction: nil)
            } label: {
                Label("Write this email for me", systemImage: "sparkles")
            }
            Button {
                withAnimation(.snappy) { aiBarVisible = true }
            } label: {
                Label("Write from instructions", systemImage: "square.and.pencil")
            }
            // Capture the range at menu build: opening the menu can drop the
            // editor's focus and clear the live selection state.
            if let range = selectedTextRange {
                Section("Selected text") {
                    ForEach(AISelectionAction.allCases) { action in
                        Button(action.label) {
                            Task { await rewriteSelection(action.instruction, range) }
                        }
                    }
                    Button("Custom…") {
                        editingSelection = EditingSelection(range: range, body: messageBody)
                        withAnimation(.snappy) { aiBarVisible = true }
                    }
                }
            } else if !messageBody.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                Section("Rewrite draft") {
                    Button("Improve writing") {
                        Task { await rewriteDraft("Improve the writing of this email; keep the meaning and roughly the same length") }
                    }
                    Button("Shorten") {
                        Task { await rewriteDraft("Make this email shorter and tighter without losing the point") }
                    }
                    Button("More formal") {
                        Task { await rewriteDraft("Rewrite this email in a more formal, professional tone") }
                    }
                    Button("Fix spelling & grammar") {
                        Task { await rewriteDraft("Fix the spelling and grammar of this email; change nothing else") }
                    }
                }
            }
        } label: {
            Label("AI", systemImage: "sparkles")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Tone.indigo.color)
        }
        .disabled(isRewriting || draftPhase != .idle)
    }

    /// Instruction bar: what the email should accomplish (feeds the grounded
    /// draft endpoint), or the edit to make when a selection is captured.
    private var aiPromptBar: some View {
        HStack(spacing: 10) {
            Image(systemName: "sparkles")
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(Tone.indigo.color)
            if editingSelection != nil {
                Text("Editing selection")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(Tone.indigo.color)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(Tone.indigo.background, in: Capsule())
                    .fixedSize()
            }
            TextField(
                editingSelection == nil ? "What should this email do?" : "Describe the edit",
                text: $aiPrompt,
                axis: .vertical
            )
            .font(.subheadline)
            .lineLimit(1 ... 3)
            .focused($aiFocused)
            .onSubmit { submitPrompt() }
            Button {
                submitPrompt()
            } label: {
                Image(systemName: "arrow.up.circle.fill")
                    .font(.system(size: 24))
                    .foregroundStyle(
                        aiPrompt.trimmingCharacters(in: .whitespaces).isEmpty
                            ? Color(.systemGray3) : Tone.indigo.color
                    )
            }
            .disabled(aiPrompt.trimmingCharacters(in: .whitespaces).isEmpty)
            .accessibilityLabel(editingSelection == nil ? "Draft email" : "Rewrite selection")
            Button {
                editingSelection = nil
                withAnimation(.snappy) { aiBarVisible = false }
            } label: {
                Image(systemName: "xmark.circle.fill")
                    .font(.system(size: 17))
                    .foregroundStyle(.tertiary)
            }
            .accessibilityLabel("Close AI")
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 9)
        .task { aiFocused = true }
    }

    private static let draftStages = ["Reading contact history…", "Writing your email…", "Polishing…"]

    private var draftStatusRow: some View {
        HStack(spacing: 10) {
            Image(systemName: "sparkles")
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(Tone.indigo.color)
                .symbolEffect(.pulse, isActive: !reduceMotion)
            Group {
                if reduceMotion {
                    Text(Self.draftStages[draftStage])
                } else {
                    Text(Self.draftStages[draftStage])
                        .skeletonShimmer()
                }
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
            .id(draftStage)
            .transition(.opacity)
            Spacer(minLength: 8)
            Button {
                cancelAIDraft()
            } label: {
                Image(systemName: "xmark.circle.fill")
                    .font(.system(size: 17))
                    .foregroundStyle(.tertiary)
            }
            .accessibilityLabel("Cancel draft")
        }
        .padding(.horizontal, 16)
        .frame(minHeight: 44)
    }

    /// The model asked instead of inventing a pitch: show the question with
    /// an inline answer field; answering re-runs the draft with the context.
    private var questionBar: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: "questionmark.bubble")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(Tone.indigo.color)
                Text(pendingQuestion ?? "")
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(.primary)
                Spacer(minLength: 0)
                Button {
                    dismissQuestion()
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 17))
                        .foregroundStyle(.tertiary)
                }
                .accessibilityLabel("Dismiss question")
            }
            HStack(spacing: 8) {
                TextField("Answer to continue…", text: $questionAnswer, axis: .vertical)
                    .font(.subheadline)
                    .lineLimit(1 ... 3)
                    .focused($answerFocused)
                    .onSubmit { submitAnswer() }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .background(
                        Color(.secondarySystemBackground),
                        in: RoundedRectangle(cornerRadius: 10, style: .continuous)
                    )
                Button {
                    submitAnswer()
                } label: {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.system(size: 24))
                        .foregroundStyle(
                            questionAnswer.trimmingCharacters(in: .whitespaces).isEmpty
                                ? Color(.systemGray3) : Tone.indigo.color
                        )
                }
                .disabled(questionAnswer.trimmingCharacters(in: .whitespaces).isEmpty)
                .accessibilityLabel("Answer and draft")
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .task { answerFocused = true }
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    private var draftReviewBar: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "sparkles")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(Tone.indigo.color)
                Text(draftUsage.map { "Draft ready · \($0)" } ?? "Draft ready")
                    .font(.footnote.weight(.semibold))
                Spacer(minLength: 0)
            }
            if let draftGrounding {
                Text(draftGrounding)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            if adjustOpen {
                HStack(spacing: 8) {
                    TextField("shorter, mention pricing…", text: $adjustText)
                        .font(.subheadline)
                        .focused($adjustFocused)
                        .onSubmit { submitAdjust() }
                        .padding(.horizontal, 10)
                        .frame(height: 34)
                        .background(
                            Color(.secondarySystemBackground),
                            in: RoundedRectangle(cornerRadius: 10, style: .continuous)
                        )
                    Button {
                        submitAdjust()
                    } label: {
                        Image(systemName: "arrow.up.circle.fill")
                            .font(.system(size: 24))
                            .foregroundStyle(
                                adjustText.trimmingCharacters(in: .whitespaces).isEmpty
                                    ? Color(.systemGray3) : Tone.indigo.color
                            )
                    }
                    .disabled(adjustText.trimmingCharacters(in: .whitespaces).isEmpty)
                    .accessibilityLabel("Regenerate with adjustment")
                }
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            HStack(spacing: 8) {
                reviewButton("Keep", prominent: true) { keepAIDraft() }
                reviewButton("Adjust") {
                    withAnimation(.snappy) { adjustOpen.toggle() }
                    adjustFocused = true
                }
                reviewButton("Retry") { retryAIDraft() }
                reviewButton("Discard") { discardAIDraft() }
                Spacer(minLength: 0)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    private func reviewButton(_ title: String, prominent: Bool = false, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(prominent ? Color.white : Tone.indigo.color)
                .padding(.horizontal, 14)
                .padding(.vertical, 7)
                .background(
                    prominent ? AnyShapeStyle(Tone.indigo.color) : AnyShapeStyle(Tone.indigo.background),
                    in: Capsule()
                )
        }
        .buttonStyle(TapScaleStyle())
    }

    // MARK: AI flow

    /// Routes the prompt bar's go action: a custom selection edit when a
    /// range is captured, the grounded draft otherwise.
    private func submitPrompt() {
        let instruction = aiPrompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !instruction.isEmpty else { return }
        if let editing = editingSelection {
            guard messageBody == editing.body else {
                store.setError("The selection changed. Highlight the text again.")
                editingSelection = nil
                return
            }
            Task {
                if await rewriteSelection(instruction, editing.range) {
                    editingSelection = nil
                    withAnimation(.snappy) {
                        aiBarVisible = false
                        aiPrompt = ""
                    }
                }
            }
            return
        }
        withAnimation(.snappy) {
            aiBarVisible = false
            aiPrompt = ""
        }
        startAIDraft(instruction: instruction)
    }

    /// Rewrites just the highlighted range through /generation/edit; the full
    /// draft rides along as fenced context. Bails out if the body changed
    /// while the request was in flight (the range would slice wrong).
    @discardableResult
    private func rewriteSelection(_ instruction: String, _ range: Range<String.Index>) async -> Bool {
        let original = messageBody
        let fragment = String(original[range])
        isRewriting = true
        defer { isRewriting = false }
        let subjectLine = subject.trimmingCharacters(in: .whitespaces)
        let contextBlock = subjectLine.isEmpty ? original : "Subject: \(subjectLine)\n\n\(original)"
        do {
            let response: AIWriteResponse = try await env.api.post(
                "generation/edit",
                body: AIEditRequest(text: fragment, instruction: instruction, context: contextBlock, tone: nil),
                idempotent: true
            )
            guard messageBody == original else {
                store.setError("The draft changed while AI was working. Try again.")
                return false
            }
            withAnimation(.snappy) {
                aiUndo = original
                messageBody.replaceSubrange(range, with: response.text)
                selection = nil
            }
            return true
        } catch {
            handleAIError(error)
            return false
        }
    }

    /// Kicks off the grounded compose draft. A `question` result pauses into
    /// the answer phase; a `text` result types in and springs the review bar.
    private func startAIDraft(instruction: String?) {
        guard draftPhase == .idle || draftPhase == .question else { return }
        if draftPhase == .idle { baseInstruction = instruction }
        selection = nil
        preDraftBody = preDraftBody ?? messageBody
        withAnimation(.snappy) {
            aiBarVisible = false
            adjustOpen = false
            pendingQuestion = nil
            draftPhase = .generating
            draftStage = 0
        }
        let effectiveInstruction = instruction
        draftTask?.cancel()
        draftTask = Task {
            let cycler = Task {
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(1.5))
                    guard !Task.isCancelled else { return }
                    withAnimation(.snappy) {
                        draftStage = (draftStage + 1) % Self.draftStages.count
                    }
                }
            }
            defer { cycler.cancel() }
            do {
                let response = try await store.aiDraft(
                    env.api,
                    to: primaryRecipient.isEmpty ? nil : primaryRecipient,
                    subject: subject.trimmingCharacters(in: .whitespaces).isEmpty ? nil : subject,
                    instruction: effectiveInstruction
                )
                guard !Task.isCancelled else { return }
                cycler.cancel()
                if let question = response.question, !question.isEmpty {
                    withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                        pendingQuestion = question
                        questionAnswer = ""
                        draftPhase = .question
                    }
                    return
                }
                draftUsage = response.usage
                draftGrounding = response.groundingLine
                await revealDraft(response.text ?? "")
                guard !Task.isCancelled else { return }
                draftDonePulse += 1
                withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                    draftPhase = .review
                }
            } catch is CancellationError {
                // Cancel already restored the body.
            } catch {
                guard !Task.isCancelled else { return }
                preDraftBody = nil
                withAnimation(.snappy) { draftPhase = .idle }
                handleAIError(error)
            }
        }
    }

    private func submitAnswer() {
        let answer = questionAnswer.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !answer.isEmpty, let question = pendingQuestion else { return }
        var combined = "Question you asked: \(question)\nAnswer: \(answer)"
        if let base = baseInstruction, !base.isEmpty {
            combined = base + "\n" + combined
        }
        questionAnswer = ""
        startAIDraft(instruction: combined)
    }

    private func dismissQuestion() {
        pendingQuestion = nil
        preDraftBody = nil
        withAnimation(.snappy) { draftPhase = .idle }
    }

    /// Types the draft into the body: ~5ms/char in frame-sized chunks, capped
    /// at ~1.2s total; instant under reduce-motion.
    private func revealDraft(_ text: String) async {
        let trimmed = messageBody.trimmingCharacters(in: .whitespacesAndNewlines)
        let prefix = trimmed.isEmpty ? "" : messageBody + "\n\n"
        if reduceMotion {
            messageBody = prefix + text
            return
        }
        let chars = Array(text)
        let stepInterval = 0.03
        let total = min(Double(chars.count) * 0.005, 1.2)
        let steps = max(1, Int(total / stepInterval))
        let perStep = max(1, Int((Double(chars.count) / Double(steps)).rounded(.up)))
        var index = 0
        while index < chars.count {
            guard !Task.isCancelled else { return }
            index = min(index + perStep, chars.count)
            messageBody = prefix + String(chars[0 ..< index])
            if index < chars.count {
                try? await Task.sleep(for: .seconds(stepInterval))
            }
        }
    }

    private func cancelAIDraft() {
        draftTask?.cancel()
        draftTask = nil
        if let preDraftBody { messageBody = preDraftBody }
        preDraftBody = nil
        draftUsage = nil
        draftGrounding = nil
        pendingQuestion = nil
        withAnimation(.snappy) {
            draftPhase = .idle
            adjustOpen = false
        }
    }

    private func keepAIDraft() {
        preDraftBody = nil
        withAnimation(.snappy) {
            draftPhase = .idle
            adjustOpen = false
        }
    }

    private func discardAIDraft() {
        if let preDraftBody { messageBody = preDraftBody }
        preDraftBody = nil
        draftUsage = nil
        draftGrounding = nil
        withAnimation(.snappy) {
            draftPhase = .idle
            adjustOpen = false
        }
    }

    private func retryAIDraft() {
        if let preDraftBody { messageBody = preDraftBody }
        withAnimation(.snappy) { draftPhase = .idle }
        startAIDraft(instruction: baseInstruction)
    }

    private func submitAdjust() {
        let instruction = adjustText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !instruction.isEmpty else { return }
        adjustText = ""
        if let preDraftBody { messageBody = preDraftBody }
        withAnimation(.snappy) {
            draftPhase = .idle
            adjustOpen = false
        }
        var combined = instruction
        if let base = baseInstruction, !base.isEmpty {
            combined = base + "\nAdjustment: " + instruction
        }
        startAIDraft(instruction: combined)
    }

    /// Rewrites the whole body through /generation/edit (subject as context).
    private func rewriteDraft(_ instruction: String) async {
        let original = messageBody
        isRewriting = true
        defer { isRewriting = false }
        let subjectLine = subject.trimmingCharacters(in: .whitespaces)
        do {
            let response: AIWriteResponse = try await env.api.post(
                "generation/edit",
                body: AIEditRequest(
                    text: original,
                    instruction: instruction,
                    context: subjectLine.isEmpty ? nil : "Subject: \(subjectLine)",
                    tone: nil
                ),
                idempotent: true
            )
            guard messageBody == original else {
                store.setError("The draft changed while AI was working. Try again.")
                return
            }
            withAnimation(.snappy) {
                aiUndo = original
                messageBody = response.text
                selection = nil
            }
        } catch {
            handleAIError(error)
        }
    }

    private func handleAIError(_ error: Error) {
        if let apiError = error as? APIError {
            switch apiError {
            case let .server(status, _) where status == 402:
                store.setError("You're out of AI credits for this billing period.")
            case .rateLimited:
                store.setError("AI usage limit reached, try again later.")
            default:
                store.setError(apiError.localizedDescription)
            }
        } else {
            store.setError(error.localizedDescription)
        }
    }

    // MARK: Send

    private func send() async {
        let request = ComposeSendRequest(
            emailAccountID: fromAccountID,
            fromTagID: fromAccountID.isEmpty ? autoTagID : nil,
            to: allTo,
            cc: allCc.isEmpty ? nil : allCc,
            bcc: allBcc.isEmpty ? nil : allBcc,
            subject: subject.trimmingCharacters(in: .whitespaces),
            bodyHTML: htmlBody,
            bodyPlain: messageBody,
            sendMode: sendMode.rawValue,
            scheduledAt: sendMode == .scheduled ? scheduledAt : nil
        )
        do {
            let sentPayload = draftPayload
            let response = try await store.send(env.api, request: request)
            sentOrDiscarded = true
            onSent(response, sentPayload)
            dismiss()
        } catch {
            store.setError(error.localizedDescription)
        }
    }

    /// `body_html` is the plaintext with newlines turned into `<br />` (web parity).
    private var htmlBody: String {
        messageBody
            .replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
            .replacingOccurrences(of: "\n", with: "<br />")
    }

    private func parse(_ raw: String) -> [String] {
        raw
            .split(whereSeparator: { $0 == "," || $0 == ";" || $0 == "\n" })
            .map { UniboxAddress.bare(String($0).trimmingCharacters(in: .whitespaces)) }
            .filter { !$0.isEmpty }
    }
}

// MARK: - From picker

/// Sender picker fed by `/unibox/compose/candidates`: the Auto row first,
/// then every active mailbox with auth state, today's budget, history with
/// the recipient, and the scorer's reasons. A compact tag menu (every tag
/// the user has) filters the list and scopes the Auto pick, web parity.
struct ComposeFromPicker: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let store: ComposeStore
    @Binding var selection: String
    /// Tag scope for Auto: set together with an empty selection when the
    /// user picks "Auto in <tag>"; cleared by plain Auto or an explicit pick.
    @Binding var autoTagID: String?

    @State private var query = ""
    @State private var activeTagID: String?

    init(store: ComposeStore, selection: Binding<String>, autoTagID: Binding<String?>) {
        self.store = store
        _selection = selection
        _autoTagID = autoTagID
        // Open on the scope that is already in effect so Auto shows checked.
        _activeTagID = State(initialValue: autoTagID.wrappedValue)
    }

    /// Every tag the user has, sorted by position; a tag created moments ago
    /// is immediately selectable even before any mailbox carries it.
    private var allTags: [UserGroup] {
        (env.session.user?.tags ?? []).sorted { ($0.position ?? 0) < ($1.position ?? 0) }
    }

    private var activeTag: UserGroup? {
        allTags.first { $0.id == activeTagID }
    }

    private var filtered: [ComposeCandidate] {
        var candidates = store.candidates
        if let activeTagID {
            candidates = candidates.filter { store.accountTags[$0.id]?.contains(activeTagID) == true }
        }
        let trimmed = query.trimmingCharacters(in: .whitespaces).lowercased()
        guard !trimmed.isEmpty else { return candidates }
        return candidates.filter {
            $0.email.lowercased().contains(trimmed)
                || ($0.name ?? "").lowercased().contains(trimmed)
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Button {
                    selection = ""
                    autoTagID = activeTagID
                    dismiss()
                } label: {
                    HStack(spacing: 12) {
                        Image(systemName: "wand.and.stars")
                            .font(.system(size: 16, weight: .medium))
                            .foregroundStyle(WTheme.accent)
                            .frame(width: 28)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(activeTag.map { "Auto in \($0.name ?? "tag")" } ?? "Auto")
                                .font(.body.weight(.medium))
                                .foregroundStyle(.primary)
                            Text(autoSubtitle)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                                .lineLimit(2)
                        }
                        Spacer(minLength: 8)
                        if selection.isEmpty, autoTagID == activeTagID {
                            Image(systemName: "checkmark")
                                .font(.system(size: 14, weight: .semibold))
                                .foregroundStyle(WTheme.accent)
                        }
                    }
                    .padding(.vertical, 2)
                }

                ForEach(filtered) { candidate in
                    Button {
                        selection = candidate.id
                        autoTagID = nil
                        dismiss()
                    } label: {
                        candidateRow(candidate)
                    }
                }

                if store.candidatesLoaded, filtered.isEmpty, !query.isEmpty || activeTagID != nil {
                    Text(
                        query.isEmpty
                            ? "No mailbox carries this tag."
                            : "No mailbox matches \"\(query)\"."
                    )
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                }
            }
            .listStyle(.plain)
            .navigationTitle("Send from")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(
                text: $query,
                placement: .navigationBarDrawer(displayMode: .always),
                prompt: "Search mailboxes"
            )
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") { dismiss() }
                }
                if !allTags.isEmpty {
                    ToolbarItem(placement: .topBarTrailing) {
                        tagFilterMenu
                    }
                }
            }
            .overlay {
                if !store.candidatesLoaded {
                    ProgressView()
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .task {
            // Tag membership comes from the account directory; the picker
            // degrades to search-only when the member can't read it.
            await store.loadAccountTags(env.api)
        }
    }

    /// Compact filter: "All tags" plus every tag with its color dot; the
    /// trigger names the active tag and takes its tint.
    private var tagFilterMenu: some View {
        Menu {
            Picker("Filter by tag", selection: $activeTagID.animation(.snappy)) {
                Text("All tags").tag(String?.none)
                ForEach(allTags) { tag in
                    Label {
                        Text(tag.name ?? "Tag")
                    } icon: {
                        Image.menuDot(Color(uniboxHex: tag.color) ?? WTheme.accent)
                    }
                    .tag(String?.some(tag.id))
                }
            }
        } label: {
            HStack(spacing: 4) {
                Image(systemName: "line.3.horizontal.decrease")
                    .font(.system(size: 12, weight: .semibold))
                Text(activeTag.map { $0.name ?? "Tag" } ?? "All tags")
                    .font(.footnote.weight(.medium))
                    .lineLimit(1)
            }
            .foregroundStyle(
                activeTag.map { Color(uniboxHex: $0.color) ?? WTheme.accent } ?? Color.secondary
            )
        }
        .accessibilityLabel("Filter by tag")
    }

    private var autoSubtitle: String {
        if let activeTag {
            guard let best = store.bestCandidate(inTag: activeTag.id) else {
                return "No mailbox carries this tag"
            }
            return best.email
        }
        if let reason = store.recommendedReason, !reason.isEmpty,
           let recommended = store.recommendedCandidate {
            return "\(recommended.email) · \(reason)"
        }
        return "Pick the best mailbox for the recipient"
    }

    private func candidateRow(_ candidate: ComposeCandidate) -> some View {
        HStack(spacing: 12) {
            Circle()
                .fill(authTone(candidate).color)
                .frame(width: 9, height: 9)
                .frame(width: 28)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(candidate.email)
                        .font(.body.weight(.medium))
                        .foregroundStyle(.primary)
                        .lineLimit(1)
                    if candidate.recommended == true {
                        Text("Best")
                            .font(.system(size: 10, weight: .semibold))
                            .foregroundStyle(Tone.emerald.color)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Tone.emerald.background, in: Capsule())
                    }
                }
                if let subtitle = subtitle(candidate) {
                    Text(subtitle)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 2) {
                Text("\(candidate.sentToday ?? 0)/\(candidate.dailyLimit ?? 0)")
                    .font(.footnote.weight(.medium))
                    .monospacedDigit()
                    .foregroundStyle(budgetTone(candidate).color)
                Text("today")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
            if selection == candidate.id {
                Image(systemName: "checkmark")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(WTheme.accent)
            }
        }
        .padding(.vertical, 2)
    }

    private func subtitle(_ candidate: ComposeCandidate) -> String? {
        var parts: [String] = []
        if let history = candidate.historyMessages, history > 0 {
            parts.append("\(history) past message\(history == 1 ? "" : "s")")
        }
        if let reasons = candidate.reasons, !reasons.isEmpty {
            parts.append(contentsOf: reasons.prefix(2))
        }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    private func authTone(_ candidate: ComposeCandidate) -> Tone {
        switch candidate.authState {
        case "passing": return .emerald
        case "failing": return .rose
        default: return .slate
        }
    }

    private func budgetTone(_ candidate: ComposeCandidate) -> Tone {
        let remaining = candidate.remainingToday ?? 0
        if remaining <= 0 { return .rose }
        if remaining <= 5 { return .amber }
        return .slate
    }
}

extension Image {
    /// Colored dot for Menu rows: menus strip SwiftUI tints, so the color is
    /// baked into the UIImage with `.alwaysOriginal`.
    static func menuDot(_ tint: Color) -> Image {
        let config = UIImage.SymbolConfiguration(pointSize: 9, weight: .regular)
        guard let dot = UIImage(systemName: "circle.fill", withConfiguration: config)?
            .withTintColor(UIColor(tint), renderingMode: .alwaysOriginal)
        else { return Image(systemName: "circle.fill") }
        return Image(uiImage: dot)
    }
}
