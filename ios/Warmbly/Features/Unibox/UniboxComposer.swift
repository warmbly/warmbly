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
    @State private var to: String
    @State private var cc: String
    @State private var bcc = ""
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

    /// Template names inline in the dropdown; the rest through the browser.
    @State private var templates = TemplatesStore()

    init(context: UniboxComposeContext, openAIOnAppear: Bool = false, onSent: @escaping (UniboxSendResponse) -> Void) {
        self.context = context
        self.openAIOnAppear = openAIOnAppear
        self.onSent = onSent
        _to = State(initialValue: context.to.joined(separator: ", "))
        _cc = State(initialValue: context.cc.joined(separator: ", "))
        _subject = State(initialValue: context.subject)
        _messageBody = State(initialValue: "")
        _showCcBcc = State(initialValue: !context.cc.isEmpty)
        _aiBarVisible = State(initialValue: openAIOnAppear)
    }

    private var canAI: Bool { env.session.can(.manageCampaigns) }
    private var canTemplates: Bool { env.session.can(.viewCampaigns) }

    private var canSend: Bool {
        !recipients.isEmpty
            && !subject.trimmingCharacters(in: .whitespaces).isEmpty
            && !messageBody.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && !store.isSending
    }

    private var recipients: [String] {
        addresses(to)
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
                labeledRow("To") {
                    TextField("", text: $to)
                        .font(.subheadline)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.emailAddress)
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
                    labeledRow("Cc") {
                        TextField("", text: $cc)
                            .font(.subheadline)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .keyboardType(.emailAddress)
                    }
                    hairline
                    labeledRow("Bcc") {
                        TextField("", text: $bcc)
                            .font(.subheadline)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .keyboardType(.emailAddress)
                    }
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
        .sheet(isPresented: $showSchedulePicker) { scheduleSheet }
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
    /// instead of opening a sheet, so the draft stays visible while asking.
    @ViewBuilder
    private var helperBar: some View {
        VStack(spacing: 0) {
            Divider()
            if aiBarVisible {
                aiPromptBar
            } else {
                helperRow
            }
        }
        .background(.bar)
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

    /// AI dropdown: draft from a prompt, rewrite the selection when there is
    /// one, or rewrite the whole draft.
    private var aiMenu: some View {
        Menu {
            Button {
                withAnimation(.snappy) { aiBarVisible = true }
            } label: {
                Label("Draft with AI", systemImage: "square.and.pencil")
            }
            // Capture the range at menu build: opening the menu can drop the
            // editor's focus and clear the live selection state.
            if let range = selectedTextRange {
                Section("Selected text") {
                    Button("Rephrase") {
                        Task { await rewriteSelection("Rephrase this text from an email reply", range) }
                    }
                    Button("Shorten") {
                        Task { await rewriteSelection("Make this text from an email reply shorter and tighter", range) }
                    }
                    Button("Expand") {
                        Task { await rewriteSelection("Expand this text from an email reply with a bit more detail", range) }
                    }
                    Button("More formal") {
                        Task { await rewriteSelection("Rewrite this text from an email reply in a more formal tone", range) }
                    }
                    Button("Fix spelling & grammar") {
                        Task { await rewriteSelection("Fix the spelling and grammar of this text; change nothing else", range) }
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
        .disabled(isGenerating)
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

    /// Inline prompt bar: sparkle, prompt field, tone, go — no sheet.
    private var aiPromptBar: some View {
        HStack(spacing: 10) {
            Image(systemName: "sparkles")
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(Tone.indigo.color)
            TextField("Tell AI what to write", text: $aiPrompt, axis: .vertical)
                .font(.subheadline)
                .lineLimit(1 ... 3)
                .focused($aiFocused)
                .onSubmit { Task { await draftFromPrompt() } }
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
                    Task { await draftFromPrompt() }
                } label: {
                    Image(systemName: "arrow.up.circle.fill")
                        .font(.system(size: 24))
                        .foregroundStyle(
                            aiPrompt.trimmingCharacters(in: .whitespaces).isEmpty
                                ? Color(.systemGray3) : Tone.indigo.color
                        )
                }
                .disabled(aiPrompt.trimmingCharacters(in: .whitespaces).isEmpty)
                .accessibilityLabel("Generate draft")
            }
            Button {
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

    // MARK: AI generation

    private func draftFromPrompt() async {
        let prompt = aiPrompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !prompt.isEmpty, !isGenerating else { return }
        guard let text = await aiText(prompt: prompt) else { return }
        withAnimation(.snappy) {
            aiUndo = messageBody
            insert(text)
            aiBarVisible = false
            aiPrompt = ""
        }
    }

    /// Rewrites the whole draft; Undo brings the old text back.
    private func rewriteDraft(_ instruction: String) async {
        let original = messageBody
        let prompt = "\(instruction). Reply with only the rewritten email body, no commentary.\n\n\(original)"
        guard let text = await aiText(prompt: prompt) else { return }
        withAnimation(.snappy) {
            aiUndo = original
            messageBody = text
        }
    }

    /// Rewrites just the highlighted range. The range is only valid against
    /// the draft it was captured from, so bail out if the text changed while
    /// the request was in flight.
    private func rewriteSelection(_ instruction: String, _ range: Range<String.Index>) async {
        let original = messageBody
        let fragment = String(original[range])
        let prompt = "\(instruction). Reply with only the rewritten text, no commentary.\n\n\(fragment)"
        guard let text = await aiText(prompt: prompt) else { return }
        guard messageBody == original else {
            store.setError("The draft changed while AI was working. Try again.")
            return
        }
        withAnimation(.snappy) {
            aiUndo = original
            messageBody.replaceSubrange(range, with: text)
            selection = nil
        }
    }

    private func aiText(prompt: String) async -> String? {
        isGenerating = true
        defer { isGenerating = false }
        do {
            var body = ["prompt": prompt]
            if aiTone != .standard { body["tone"] = aiTone.rawValue }
            let response: AIWriteResponse = try await env.api.post(
                "generation/write", body: body, idempotent: true
            )
            return response.text
        } catch let error as APIError {
            if case let .server(status, _) = error, status == 402 {
                store.setError("You're out of AI credits for this billing period.")
            } else {
                store.setError(error.localizedDescription)
            }
        } catch {
            store.setError(error.localizedDescription)
        }
        return nil
    }

    // MARK: Schedule sheet

    private var scheduleSheet: some View {
        NavigationStack {
            VStack(spacing: 0) {
                DatePicker(
                    "Send at",
                    selection: $scheduledAt,
                    in: scheduleBounds,
                    displayedComponents: [.date, .hourAndMinute]
                )
                .datePickerStyle(.graphical)
                .padding(.horizontal, 12)
                Spacer(minLength: 0)
            }
            .navigationTitle("Schedule send")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { showSchedulePicker = false }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Set") {
                        withAnimation(.snappy) { sendMode = .scheduled }
                        showSchedulePicker = false
                    }
                    .fontWeight(.semibold)
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    // MARK: Labels + bounds

    private var sendLabel: String {
        switch sendMode {
        case .instant: return "Send"
        case .smart: return "Queue"
        case .scheduled: return "Schedule"
        }
    }

    /// Scheduled sends must be > now+5s and <= now+29 days (server rule).
    private var scheduleBounds: ClosedRange<Date> {
        let lower = Date().addingTimeInterval(120)
        let upper = Date().addingTimeInterval(29 * 24 * 3600 - 3600)
        return lower ... upper
    }

    // MARK: Send

    private func send() async {
        let request = UniboxReplyRequest(
            emailAccountID: context.accountID,
            to: recipients,
            cc: showCcBcc ? addresses(cc).nilIfEmpty : nil,
            bcc: showCcBcc ? addresses(bcc).nilIfEmpty : nil,
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
