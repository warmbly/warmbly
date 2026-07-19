import SwiftUI

/// Reply / compose sheet. Sends through `POST /v1/unibox/reply` (idempotent),
/// instant or scheduled per `UniboxSendMode`. Gate the caller with
/// `.accessUnibox`; this view assumes the permission was already checked.
struct UniboxComposer: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let context: UniboxComposeContext
    /// Opens the AI writer immediately (the thread's sparkle shortcut).
    var openAIOnAppear: Bool = false
    /// Called after a successful send so the thread can refresh + drop presence.
    var onSent: (UniboxSendResponse) -> Void

    @State private var store = UniboxComposerStore()
    @State private var to: [String]
    @State private var toText = ""
    @State private var cc: [String]
    @State private var ccText = ""
    @State private var bcc: [String] = []
    @State private var bccText = ""
    @State private var subject: String
    @State private var messageBody: String
    @State private var showCcBcc: Bool
    @State private var sendMode: UniboxSendMode = .instant
    @State private var scheduledAt = Date().addingTimeInterval(3600)
    @State private var showSchedulePicker = false
    @State private var showTemplateBrowser = false

    /// AI is inline, not a sheet: a prompt bar above the keyboard plus
    /// rewrite actions on the current draft or just the selected text,
    /// with one-tap undo.
    @State private var aiBarVisible: Bool
    @State private var aiPrompt = ""
    @State private var aiTone: AIWriteTone = .standard
    @State private var isGenerating = false
    @State private var aiUndo: String?
    @State private var selection: TextSelection?
    @FocusState private var aiFocused: Bool

    /// Thread-grounded draft flow: the request lifecycle, the body to restore
    /// on Discard/Retry, and the status/review UI above the keyboard.
    private enum AIDraftPhase { case idle, generating, review }
    @State private var draftPhase: AIDraftPhase = .idle
    @State private var draftStage = 0
    @State private var draftUsage: String?
    @State private var preDraftBody: String?
    @State private var draftTask: Task<Void, Never>?
    @State private var adjustOpen = false
    @State private var adjustText = ""
    @FocusState private var adjustFocused: Bool
    @State private var draftDonePulse = 0
    @State private var reviewActionPulse = 0

    /// Selection captured for a custom instruction ("Editing selection" mode);
    /// carries the body it was captured from so a stale range never slices.
    private struct EditingSelection {
        var range: Range<String.Index>
        var body: String
    }

    @State private var editingSelection: EditingSelection?

    /// Transient result chip floating over the helper bar.
    private struct AINotice: Equatable {
        var text: String
        var undo: Bool
    }

    @State private var notice: AINotice?
    @State private var noticeID = 0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// Template names inline in the dropdown; the rest through the browser.
    @State private var templates = TemplatesStore()

    init(context: UniboxComposeContext, openAIOnAppear: Bool = false, onSent: @escaping (UniboxSendResponse) -> Void) {
        self.context = context
        self.openAIOnAppear = openAIOnAppear
        self.onSent = onSent
        _to = State(initialValue: context.to)
        _cc = State(initialValue: context.cc)
        _subject = State(initialValue: context.subject)
        _messageBody = State(initialValue: "")
        _showCcBcc = State(initialValue: !context.cc.isEmpty)
        _aiBarVisible = State(initialValue: openAIOnAppear)
    }

    private var canAI: Bool { env.session.can(.useAI) }
    private var canTemplates: Bool { env.session.can(.viewCampaigns) }

    private var canSend: Bool {
        !recipients.isEmpty
            && !subject.trimmingCharacters(in: .whitespaces).isEmpty
            && !messageBody.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && !store.isSending
    }

    private var recipients: [String] {
        to + addresses(toText)
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                labeledRow("From") {
                    Text(context.accountEmail ?? "This mailbox")
                        .font(.subheadline)
                        .lineLimit(1)
                }
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
            .navigationTitle("Reply")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbarContent }
            .safeAreaInset(edge: .bottom) { helperBar }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
        .interactiveDismissDisabled(store.isSending)
        .sensoryFeedback(.impact(weight: .light), trigger: draftDonePulse)
        .sensoryFeedback(.selection, trigger: reviewActionPulse)
        .onDisappear { draftTask?.cancel() }
        .sheet(isPresented: $showSchedulePicker) {
            ComposeScheduleSheet(isPresented: $showSchedulePicker, scheduledAt: $scheduledAt) {
                withAnimation(.snappy) { sendMode = .scheduled }
            }
        }
        .sheet(isPresented: $showTemplateBrowser) {
            UniboxTemplatePicker { template in
                if let body = template.bodyPlain, !body.isEmpty {
                    insert(body)
                }
            }
        }
        .task {
            if canTemplates { await templates.load(env.api) }
        }
    }

    /// Inserts assistant/template text: replaces an empty draft, appends after
    /// a blank line otherwise.
    private func insert(_ text: String) {
        let trimmed = messageBody.trimmingCharacters(in: .whitespacesAndNewlines)
        withAnimation(.snappy) {
            messageBody = trimmed.isEmpty ? text : messageBody + "\n\n" + text
        }
    }

    // MARK: Toolbar

    /// Gmail-style chrome: X to close, paperplane to send, and a menu holding
    /// the when-to-send choice plus the helpers.
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
                .accessibilityLabel(sendLabel)
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
                if canTemplates {
                    Button {
                        showTemplateBrowser = true
                    } label: {
                        Label("Templates", systemImage: "doc.on.doc")
                    }
                }
                if canAI {
                    Button {
                        withAnimation(.snappy) { aiBarVisible = true }
                    } label: {
                        Label("Write with AI", systemImage: "sparkles")
                    }
                }
            } label: {
                Image(systemName: "ellipsis")
            }
            .sensoryFeedback(.selection, trigger: sendMode)
        }
    }

    // MARK: Field rows

    private func labeledRow(_ label: String, @ViewBuilder value: () -> some View) -> some View {
        HStack(spacing: 12) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 44, alignment: .leading)
            value()
        }
        .padding(.horizontal, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .frame(minHeight: 46)
    }

    private var hairline: some View {
        Divider()
    }

    /// Gmail-style schedule strip under the fields: names the chosen timing,
    /// tap to change it, x to just send now.
    private var scheduleBanner: some View {
        HStack(spacing: 8) {
            Image(systemName: sendMode == .scheduled ? "clock" : "wand.and.stars")
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(WTheme.accent)
            Text(
                sendMode == .scheduled
                    ? "Scheduled for \(scheduledAt.formatted(date: .abbreviated, time: .shortened))"
                    : "Smart send: goes out at the next safe slot for this mailbox"
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

    /// The draft takes all remaining space, borderless, aligned with the rows.
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
                if messageBody.count > UniboxComposerStore.bodyLimit {
                    messageBody = String(messageBody.prefix(UniboxComposerStore.bodyLimit))
                }
            }
    }

    // MARK: Helper bar

    /// Quiet helpers above the keyboard. The AI prompt bar swaps in here
    /// instead of opening a sheet, so the draft stays visible while asking;
    /// the thread-draft status and review rows take over the same slot.
    @ViewBuilder
    private var helperBar: some View {
        VStack(spacing: 0) {
            if let notice {
                noticeChip(notice)
            }
            VStack(spacing: 0) {
                Divider()
                switch draftPhase {
                case .generating:
                    draftStatusRow
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
    }

    /// "Rewritten · 1 credit · 300 tok" style capsule that floats over the
    /// helper bar for a few seconds, with a one-tap Undo when applicable.
    private func noticeChip(_ notice: AINotice) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "sparkles")
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(Tone.indigo.color)
            Text(notice.text)
                .font(.footnote.weight(.medium))
                .foregroundStyle(.secondary)
                .lineLimit(1)
            if notice.undo, aiUndo != nil {
                Button("Undo") {
                    withAnimation(.snappy) {
                        messageBody = aiUndo ?? messageBody
                        aiUndo = nil
                        self.notice = nil
                    }
                    selection = nil
                }
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Tone.indigo.color)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 7)
        .background(.bar, in: Capsule())
        .overlay(Capsule().strokeBorder(Color(.systemGray5), lineWidth: 1))
        .shadow(color: .black.opacity(0.08), radius: 8, y: 2)
        .padding(.bottom, 8)
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    // MARK: Thread draft UI

    private static let draftStages = ["Reading the thread…", "Writing your reply…", "Polishing…"]

    /// Replaces the helper bar while the draft request runs: pulsing sparkles,
    /// a staged shimmer label, and a cancel that abandons the result.
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
                cancelDraft()
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

    /// Springs in when the draft lands: usage summary plus Keep / Adjust /
    /// Retry / Discard, with an inline instruction field behind Adjust.
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
                reviewButton("Keep", prominent: true) { keepDraft() }
                reviewButton("Adjust") {
                    withAnimation(.snappy) { adjustOpen.toggle() }
                    adjustFocused = true
                }
                reviewButton("Retry") { retryDraft() }
                reviewButton("Discard") { discardDraft() }
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

    private var helperRow: some View {
        HStack(spacing: 18) {
            if canAI { aiMenu }
            if canTemplates { templatesMenu }
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
            if isGenerating {
                ProgressView().controlSize(.small)
            } else {
                Text("\(messageBody.count) / \(UniboxComposerStore.bodyLimit)")
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

    /// AI dropdown: thread-grounded draft, draft from a prompt, rewrite the
    /// selection when there is one, or rewrite the whole draft.
    private var aiMenu: some View {
        Menu {
            if context.threadID != nil {
                Button {
                    startDraftReply(instruction: nil)
                } label: {
                    Label("Draft a reply from this thread", systemImage: "sparkles")
                }
            }
            Button {
                withAnimation(.snappy) { aiBarVisible = true }
            } label: {
                Label("Draft with AI", systemImage: "square.and.pencil")
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
                        Task { await rewriteDraft("Improve the writing of this email reply; keep the meaning and roughly the same length") }
                    }
                    Button("Shorten") {
                        Task { await rewriteDraft("Make this email reply shorter and tighter without losing the point") }
                    }
                    Button("More formal") {
                        Task { await rewriteDraft("Rewrite this email reply in a more formal, professional tone") }
                    }
                    Button("More casual") {
                        Task { await rewriteDraft("Rewrite this email reply in a relaxed, casual tone") }
                    }
                    Button("Fix spelling & grammar") {
                        Task { await rewriteDraft("Fix the spelling and grammar of this email reply; change nothing else") }
                    }
                }
            }
        } label: {
            Label("AI", systemImage: "sparkles")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Tone.indigo.color)
        }
        .disabled(isGenerating || draftPhase != .idle)
    }

    /// Templates dropdown: the first few by name for one-tap insert, then the
    /// searchable browser for big libraries.
    private var templatesMenu: some View {
        Menu {
            ForEach(templates.templates.prefix(8)) { template in
                Button(template.name) {
                    if let body = template.bodyPlain, !body.isEmpty { insert(body) }
                }
            }
            if !templates.templates.isEmpty { Divider() }
            Button {
                showTemplateBrowser = true
            } label: {
                Label(
                    templates.templates.count > 8 ? "All templates (\(templates.templates.count))" : "Browse templates",
                    systemImage: "magnifyingglass"
                )
            }
        } label: {
            Label("Templates", systemImage: "doc.on.doc")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(WTheme.accent)
        }
    }

    /// Inline prompt bar: sparkle, prompt field, tone, go, no sheet. Shows a
    /// thread-draft quick action when the body is empty, and an "Editing
    /// selection" chip when a custom selection edit is in flight.
    private var aiPromptBar: some View {
        VStack(spacing: 0) {
            if editingSelection == nil, context.threadID != nil,
               messageBody.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                Button {
                    startDraftReply(instruction: nil)
                } label: {
                    HStack(spacing: 8) {
                        Image(systemName: "text.bubble")
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundStyle(Tone.indigo.color)
                        Text("Draft a reply from this thread")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(.primary)
                        Spacer(minLength: 8)
                        Image(systemName: "chevron.right")
                            .font(.system(size: 11, weight: .semibold))
                            .foregroundStyle(.tertiary)
                    }
                    .padding(.horizontal, 16)
                    .frame(minHeight: 40)
                    .contentShape(Rectangle())
                }
                .buttonStyle(TapScaleStyle())
                Divider()
            }
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
                    editingSelection == nil ? "Tell AI what to write" : "Describe the edit",
                    text: $aiPrompt,
                    axis: .vertical
                )
                .font(.subheadline)
                .lineLimit(1 ... 3)
                .focused($aiFocused)
                .onSubmit { Task { await submitPromptBar() } }
                Menu {
                    Picker("Tone", selection: $aiTone) {
                        ForEach(AIWriteTone.allCases) { option in
                            Text(option.label).tag(option)
                        }
                    }
                } label: {
                    Image(systemName: "slider.horizontal.3")
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundStyle(aiTone == .standard ? Color.secondary : Tone.indigo.color)
                        .frame(width: 28, height: 28)
                }
                .accessibilityLabel("Tone")
                if isGenerating {
                    ProgressView().controlSize(.small)
                } else {
                    Button {
                        Task { await submitPromptBar() }
                    } label: {
                        Image(systemName: "arrow.up.circle.fill")
                            .font(.system(size: 24))
                            .foregroundStyle(
                                aiPrompt.trimmingCharacters(in: .whitespaces).isEmpty
                                    ? Color(.systemGray3) : Tone.indigo.color
                            )
                    }
                    .disabled(aiPrompt.trimmingCharacters(in: .whitespaces).isEmpty)
                    .accessibilityLabel(editingSelection == nil ? "Generate draft" : "Rewrite selection")
                }
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
        }
        .task { aiFocused = true }
    }

    // MARK: AI generation

    /// Routes the prompt bar's go action: a custom selection edit when a
    /// range is captured, a fresh generation otherwise.
    private func submitPromptBar() async {
        let prompt = aiPrompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !prompt.isEmpty, !isGenerating else { return }
        if let editing = editingSelection {
            guard messageBody == editing.body else {
                store.setError("The selection changed. Highlight the text again.")
                editingSelection = nil
                return
            }
            if await rewriteSelection(prompt, editing.range) {
                editingSelection = nil
                withAnimation(.snappy) {
                    aiBarVisible = false
                    aiPrompt = ""
                }
            }
        } else {
            await draftFromPrompt()
        }
    }

    private func draftFromPrompt() async {
        let prompt = aiPrompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !prompt.isEmpty, !isGenerating else { return }
        let cursor = insertionOffset
        guard let response = await aiText(prompt: prompt) else { return }
        withAnimation(.snappy) {
            aiUndo = messageBody
            insertGenerated(response.text, at: cursor)
            aiBarVisible = false
            aiPrompt = ""
        }
        if let usage = response.usage {
            showNotice(usage, undo: false)
        }
    }

    /// Rewrites the whole draft through /generation/edit (the subject rides
    /// along as context); Undo brings the old text back.
    private func rewriteDraft(_ instruction: String) async {
        let original = messageBody
        let subjectLine = subject.trimmingCharacters(in: .whitespaces)
        guard let response = await aiEdit(
            text: original,
            instruction: instruction,
            editContext: subjectLine.isEmpty ? nil : "Subject: \(subjectLine)"
        ) else { return }
        guard messageBody == original else {
            store.setError("The draft changed while AI was working. Try again.")
            return
        }
        withAnimation(.snappy) {
            aiUndo = original
            messageBody = response.text
            selection = nil
        }
        showNotice(usageLine("Rewritten", response.usage), undo: true)
    }

    /// Rewrites just the highlighted range through /generation/edit; the full
    /// draft goes along as fenced context. The range is only valid against
    /// the draft it was captured from, so bail out if the text changed while
    /// the request was in flight.
    @discardableResult
    private func rewriteSelection(_ instruction: String, _ range: Range<String.Index>) async -> Bool {
        let original = messageBody
        let fragment = String(original[range])
        let subjectLine = subject.trimmingCharacters(in: .whitespaces)
        let contextBlock = subjectLine.isEmpty ? original : "Subject: \(subjectLine)\n\n\(original)"
        guard let response = await aiEdit(
            text: fragment, instruction: instruction, editContext: contextBlock
        ) else { return false }
        guard messageBody == original else {
            store.setError("The draft changed while AI was working. Try again.")
            return false
        }
        withAnimation(.snappy) {
            aiUndo = original
            messageBody.replaceSubrange(range, with: response.text)
            selection = nil
        }
        showNotice(usageLine("Rewritten", response.usage), undo: true)
        return true
    }

    private func aiText(prompt: String) async -> AIWriteResponse? {
        isGenerating = true
        defer { isGenerating = false }
        do {
            var body = ["prompt": prompt]
            if aiTone != .standard { body["tone"] = aiTone.rawValue }
            return try await env.api.post("generation/write", body: body, idempotent: true)
        } catch {
            handleAIError(error)
        }
        return nil
    }

    /// The passage is fenced server-side as untrusted content, so quoted
    /// email text inside a selection can never steer the model.
    private func aiEdit(text: String, instruction: String, editContext: String?) async -> AIWriteResponse? {
        isGenerating = true
        defer { isGenerating = false }
        do {
            return try await env.api.post(
                "generation/edit",
                body: AIEditRequest(
                    text: text,
                    instruction: instruction,
                    context: editContext,
                    tone: aiTone == .standard ? nil : aiTone.rawValue
                ),
                idempotent: true
            )
        } catch {
            handleAIError(error)
        }
        return nil
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

    private func usageLine(_ prefix: String, _ usage: String?) -> String {
        usage.map { "\(prefix) · \($0)" } ?? prefix
    }

    private func showNotice(_ text: String, undo: Bool) {
        noticeID += 1
        let shown = noticeID
        withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
            notice = AINotice(text: text, undo: undo)
        }
        Task {
            try? await Task.sleep(for: .seconds(4))
            guard shown == noticeID else { return }
            withAnimation(.snappy) { notice = nil }
        }
    }

    // MARK: Insert at cursor

    /// Caret position as a character offset when the editor has a plain
    /// insertion point (no range selected).
    private var insertionOffset: Int? {
        guard let selection, selection.isInsertion,
              case let .selection(range) = selection.indices else { return nil }
        return messageBody.distance(from: messageBody.startIndex, to: range.lowerBound)
    }

    /// Inserts generated text at the captured caret with whitespace joining;
    /// appends after a blank line when there is no usable caret.
    private func insertGenerated(_ text: String, at offset: Int?) {
        defer { selection = nil }
        let trimmed = messageBody.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            messageBody = text
            return
        }
        guard let offset, offset >= 0, offset <= messageBody.count,
              let index = messageBody.index(
                  messageBody.startIndex, offsetBy: offset, limitedBy: messageBody.endIndex
              )
        else {
            messageBody += "\n\n" + text
            return
        }
        var insertion = text
        if index > messageBody.startIndex,
           !messageBody[messageBody.index(before: index)].isWhitespace {
            insertion = " " + insertion
        }
        if index < messageBody.endIndex, !messageBody[index].isWhitespace {
            insertion += " "
        }
        messageBody.insert(contentsOf: insertion, at: index)
    }

    // MARK: Thread draft flow

    /// Kicks off the thread-grounded draft: remembers the current body for
    /// Discard/Retry, swaps the helper bar for the status row, and reveals
    /// the result with a typewriter before the review bar springs in.
    private func startDraftReply(instruction: String?) {
        guard let threadID = context.threadID, draftPhase != .generating else { return }
        selection = nil
        preDraftBody = messageBody
        withAnimation(.snappy) {
            aiBarVisible = false
            adjustOpen = false
            draftPhase = .generating
            draftStage = 0
        }
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
                let response: AIWriteResponse = try await env.api.post(
                    "unibox/reply/draft",
                    body: AIDraftReplyRequest(threadID: threadID, instruction: instruction),
                    idempotent: true
                )
                guard !Task.isCancelled else { return }
                cycler.cancel()
                draftUsage = response.usage
                await revealDraft(response.text)
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

    /// Types the draft into the body: ~5ms/char in frame-sized chunks, capped
    /// at ~1.2s total; instant under reduce-motion.
    private func revealDraft(_ text: String) async {
        selection = nil
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

    /// Abandons the in-flight draft (or a half-typed reveal) and restores the
    /// pre-draft body.
    private func cancelDraft() {
        draftTask?.cancel()
        draftTask = nil
        if let preDraftBody { messageBody = preDraftBody }
        preDraftBody = nil
        draftUsage = nil
        selection = nil
        withAnimation(.snappy) {
            draftPhase = .idle
            adjustOpen = false
        }
    }

    private func keepDraft() {
        reviewActionPulse += 1
        preDraftBody = nil
        withAnimation(.snappy) {
            draftPhase = .idle
            adjustOpen = false
        }
    }

    private func discardDraft() {
        reviewActionPulse += 1
        if let preDraftBody { messageBody = preDraftBody }
        preDraftBody = nil
        draftUsage = nil
        selection = nil
        withAnimation(.snappy) {
            draftPhase = .idle
            adjustOpen = false
        }
    }

    private func retryDraft() {
        if let preDraftBody { messageBody = preDraftBody }
        selection = nil
        withAnimation(.snappy) { draftPhase = .idle }
        startDraftReply(instruction: nil)
    }

    private func submitAdjust() {
        let instruction = adjustText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !instruction.isEmpty else { return }
        adjustText = ""
        if let preDraftBody { messageBody = preDraftBody }
        selection = nil
        withAnimation(.snappy) {
            draftPhase = .idle
            adjustOpen = false
        }
        startDraftReply(instruction: instruction)
    }

    // MARK: Labels

    private var sendLabel: String {
        switch sendMode {
        case .instant: return "Send"
        case .smart: return "Queue"
        case .scheduled: return "Schedule"
        }
    }

    // MARK: Send

    private func send() async {
        let request = UniboxReplyRequest(
            emailAccountID: context.accountID,
            to: recipients,
            cc: showCcBcc ? (cc + addresses(ccText)).nilIfEmpty : nil,
            bcc: showCcBcc ? (bcc + addresses(bccText)).nilIfEmpty : nil,
            subject: subject.trimmingCharacters(in: .whitespaces),
            bodyHTML: htmlBody,
            bodyPlain: messageBody,
            inReplyTo: context.inReplyTo.nilIfEmpty,
            threadID: context.threadID,
            sendMode: sendMode.rawValue,
            scheduledAt: sendMode == .scheduled ? scheduledAt : nil
        )
        do {
            let response = try await store.send(env.api, request: request)
            onSent(response)
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

    private func addresses(_ raw: String) -> [String] {
        raw
            .split(whereSeparator: { $0 == "," || $0 == ";" || $0 == "\n" })
            .map { UniboxAddress.bare(String($0).trimmingCharacters(in: .whitespaces)) }
            .filter { !$0.isEmpty }
    }
}

private extension Array where Element == String {
    var nilIfEmpty: [String]? { isEmpty ? nil : self }
}

/// Selection quick actions; instruction parity with the web composer.
/// Shared by the reply composer and the compose window.
enum AISelectionAction: String, CaseIterable, Identifiable {
    case improve, shorten, expand, fixGrammar, friendlier, moreFormal

    var id: String { rawValue }

    var label: String {
        switch self {
        case .improve: return "Improve"
        case .shorten: return "Shorten"
        case .expand: return "Expand"
        case .fixGrammar: return "Fix grammar"
        case .friendlier: return "Friendlier"
        case .moreFormal: return "More formal"
        }
    }

    var instruction: String {
        switch self {
        case .improve:
            return "Improve the writing: clearer, smoother, better flow. Keep the meaning and roughly the same length."
        case .shorten:
            return "Make this more concise. Cut filler and keep the meaning."
        case .expand:
            return "Expand this slightly with more substance and specificity. No fluff."
        case .fixGrammar:
            return "Fix spelling, grammar, and punctuation only. Change nothing else."
        case .friendlier:
            return "Make the tone warmer and friendlier without getting sappy."
        case .moreFormal:
            return "Make the tone more professional and polished."
        }
    }
}

// MARK: - Template picker

/// Browser for big template libraries: server-side search, name + snippet
/// rows, tap to insert. The quick dropdown covers the first few; this covers
/// the rest.
struct UniboxTemplatePicker: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    var onPick: (EmailTemplate) -> Void

    @State private var store = TemplatesStore()
    @State private var query = ""

    var body: some View {
        NavigationStack {
            Group {
                if store.isLoading, !store.hasLoaded {
                    ProgressView()
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if store.templates.isEmpty {
                    EmptyStateView(
                        title: query.isEmpty ? "No templates yet" : "No matches",
                        message: query.isEmpty
                            ? "Save reply templates from the More tab or the web dashboard."
                            : "No template matches \"\(query)\"."
                    )
                } else {
                    List {
                        ForEach(store.templates) { template in
                            Button {
                                onPick(template)
                                dismiss()
                            } label: {
                                VStack(alignment: .leading, spacing: 3) {
                                    Text(template.name)
                                        .font(.body.weight(.medium))
                                        .foregroundStyle(.primary)
                                    if let subject = template.subject, !subject.isEmpty {
                                        Text(subject)
                                            .font(.footnote.weight(.medium))
                                            .foregroundStyle(.secondary)
                                            .lineLimit(1)
                                    }
                                    if let snippet = template.bodyPlain?
                                        .trimmingCharacters(in: .whitespacesAndNewlines), !snippet.isEmpty {
                                        Text(snippet)
                                            .font(.footnote)
                                            .foregroundStyle(.tertiary)
                                            .lineLimit(2)
                                    }
                                }
                                .padding(.vertical, 2)
                            }
                        }
                    }
                    .listStyle(.plain)
                }
            }
            .navigationTitle("Templates")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(
                text: $query,
                placement: .navigationBarDrawer(displayMode: .always),
                prompt: "Search templates"
            )
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") { dismiss() }
                }
            }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
        .task(id: query) {
            // Debounce keystrokes; the store searches server-side (`q`).
            if store.hasLoaded {
                try? await Task.sleep(for: .milliseconds(300))
            }
            guard !Task.isCancelled else { return }
            store.query = query
            await store.load(env.api)
        }
    }
}
