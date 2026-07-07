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
    @State private var subject: String
    @State private var messageBody: String
    @State private var showCC: Bool
    @State private var sendMode: UniboxSendMode = .instant
    @State private var scheduledAt = Date().addingTimeInterval(3600)
    @State private var showAIWriter = false
    @State private var showTemplates = false

    init(context: UniboxComposeContext, openAIOnAppear: Bool = false, onSent: @escaping (UniboxSendResponse) -> Void) {
        self.context = context
        self.openAIOnAppear = openAIOnAppear
        self.onSent = onSent
        _to = State(initialValue: context.to.joined(separator: ", "))
        _cc = State(initialValue: context.cc.joined(separator: ", "))
        _subject = State(initialValue: context.subject)
        _messageBody = State(initialValue: "")
        _showCC = State(initialValue: !context.cc.isEmpty)
        _showAIWriter = State(initialValue: openAIOnAppear)
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
            ScrollView {
                VStack(spacing: 14) {
                    addressCard
                    messageCard
                    sendOptionsCard
                    if let error = store.errorMessage {
                        Text(error)
                            .font(.subheadline)
                            .foregroundStyle(WTheme.negative)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 4)
                    }
                }
                .padding(16)
                .padding(.bottom, 8)
            }
            .background(Color(.systemGroupedBackground))
            .scrollDismissesKeyboard(.interactively)
            .navigationTitle("Reply")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .safeAreaInset(edge: .bottom) { sendButton }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
        .interactiveDismissDisabled(store.isSending)
        .sheet(isPresented: $showAIWriter) {
            UniboxAIWriterSheet(replySubject: subject) { text in
                insert(text)
            }
        }
        .sheet(isPresented: $showTemplates) {
            UniboxTemplatePicker { template in
                if let body = template.bodyPlain, !body.isEmpty {
                    insert(body)
                }
            }
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

    // MARK: Assist row

    private var assistRow: some View {
        HStack(spacing: 8) {
            if canAI {
                Button {
                    showAIWriter = true
                } label: {
                    Label("Write with AI", systemImage: "sparkles")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Tone.indigo.color)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 7)
                        .background(Tone.indigo.background, in: Capsule())
                }
                .buttonStyle(TapScaleStyle())
            }
            if canTemplates {
                Button {
                    showTemplates = true
                } label: {
                    Label("Templates", systemImage: "doc.on.doc")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(WTheme.accent)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 7)
                        .background(Tone.sky.background, in: Capsule())
                }
                .buttonStyle(TapScaleStyle())
            }
            Spacer()
        }
        .listRowSeparator(.hidden)
    }

    // MARK: Cards

    /// Settings-style addressing card: labeled hairline rows in one surface.
    private var addressCard: some View {
        VStack(spacing: 0) {
            addressRow("From") {
                HStack(spacing: 8) {
                    WAvatar(name: context.accountEmail ?? "M", seed: context.accountID, size: 24)
                    Text(context.accountEmail ?? "This mailbox")
                        .font(.subheadline.weight(.medium))
                        .lineLimit(1)
                    Spacer(minLength: 0)
                }
            }
            cardDivider
            addressRow("To") {
                TextField("name@example.com", text: $to)
                    .font(.subheadline)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.emailAddress)
            }
            if showCC {
                cardDivider
                addressRow("Cc") {
                    TextField("name@example.com", text: $cc)
                        .font(.subheadline)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.emailAddress)
                }
            }
            cardDivider
            addressRow("Subject") {
                TextField("Subject", text: $subject)
                    .font(.subheadline)
            }
        }
        .airCard(padding: 0)
        .overlay(alignment: .topTrailing) {
            if !showCC {
                Button("Cc") {
                    withAnimation(.snappy) { showCC = true }
                }
                .font(.footnote.weight(.semibold))
                .foregroundStyle(WTheme.accent)
                .padding(.top, 13)
                .padding(.trailing, 16)
            }
        }
    }

    private func addressRow(_ label: String, @ViewBuilder value: () -> some View) -> some View {
        HStack(spacing: 12) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 56, alignment: .leading)
            value()
        }
        .padding(.horizontal, 16)
        .frame(minHeight: 46)
    }

    private var cardDivider: some View {
        Divider().padding(.leading, 84)
    }

    /// Message card: assist chips + the draft editor in one surface.
    private var messageCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            if canAI || canTemplates {
                assistRow
            }
            TextEditor(text: $messageBody)
                .frame(minHeight: 180)
                .font(.body)
                .scrollContentBackground(.hidden)
                .overlay(alignment: .topLeading) {
                    if messageBody.isEmpty {
                        Text("Write a reply")
                            .font(.body)
                            .foregroundStyle(.tertiary)
                            .padding(.top, 8)
                            .padding(.leading, 5)
                            .allowsHitTesting(false)
                    }
                }
                .onChange(of: messageBody) {
                    if messageBody.count > UniboxComposerStore.bodyLimit {
                        messageBody = String(messageBody.prefix(UniboxComposerStore.bodyLimit))
                    }
                }
            HStack {
                Spacer()
                Text("\(messageBody.count) / \(UniboxComposerStore.bodyLimit)")
                    .font(.caption)
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
            }
        }
        .airCard()
    }

    /// Send timing card: segmented mode + the schedule picker when relevant.
    private var sendOptionsCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            EyebrowLabel("When to send")
            Picker("When", selection: $sendMode) {
                ForEach(UniboxSendMode.allCases) { mode in
                    Text(mode.label).tag(mode)
                }
            }
            .pickerStyle(.segmented)
            .sensoryFeedback(.selection, trigger: sendMode)
            if sendMode == .scheduled {
                DatePicker(
                    "Send at",
                    selection: $scheduledAt,
                    in: scheduleBounds,
                    displayedComponents: [.date, .hourAndMinute]
                )
                .font(.subheadline)
            } else if sendMode == .smart {
                Text("Sends at the next safe slot for this mailbox.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .airCard()
    }

    /// The auth-flow CTA carried into the composer: one full-width gradient
    /// capsule that names what will happen.
    private var sendButton: some View {
        Button {
            Task { await send() }
        } label: {
            HStack(spacing: 8) {
                if store.isSending {
                    ProgressView().tint(.white)
                    Text("Sending")
                } else {
                    Image(systemName: sendMode == .scheduled ? "clock.badge.checkmark" : "paperplane.fill")
                        .font(.system(size: 15, weight: .semibold))
                    Text(sendLabel)
                }
            }
            .font(.body.weight(.semibold))
            .foregroundStyle(.white)
            .frame(maxWidth: .infinity)
            .frame(height: 50)
            .background(
                canSend ? AnyShapeStyle(WTheme.accent.gradient) : AnyShapeStyle(Color(.systemGray3)),
                in: Capsule()
            )
        }
        .buttonStyle(TapScaleStyle())
        .disabled(!canSend)
        .padding(.horizontal, 16)
        .padding(.top, 8)
        .padding(.bottom, 6)
        .background(.bar)
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
            cc: showCC ? addresses(cc).nilIfEmpty : nil,
            bcc: nil,
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

// MARK: - AI writer

/// "Write with AI": drafts reply copy from a short prompt via
/// `POST /v1/generation/write`. One credit per generation; a 402 means the
/// org's balance is spent.
struct UniboxAIWriterSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let replySubject: String
    var onInsert: (String) -> Void

    @State private var prompt = ""
    @State private var tone: AIWriteTone = .standard
    @State private var result: AIWriteResponse?
    @State private var isGenerating = false
    @State private var errorMessage: String?
    @State private var generated = 0
    @FocusState private var promptFocused: Bool

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    promptCard
                    toneRow
                    generateButton
                    if let errorMessage {
                        Text(errorMessage)
                            .font(.subheadline)
                            .foregroundStyle(WTheme.negative)
                    }
                    if let result {
                        resultCard(result)
                    }
                }
                .padding(18)
            }
            .background(Color(.systemGroupedBackground))
            .navigationTitle("Write with AI")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") { dismiss() }
                }
            }
            .sensoryFeedback(.impact(weight: .light), trigger: generated)
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
        .onAppear { promptFocused = true }
    }

    private var promptCard: some View {
        VStack(alignment: .leading, spacing: 8) {
            EyebrowLabel("What should it say?")
            TextField(
                "e.g. Thank them and suggest a call on Thursday afternoon",
                text: $prompt,
                axis: .vertical
            )
            .font(.body)
            .lineLimit(3 ... 6)
            .focused($promptFocused)
        }
        .airCard()
    }

    private var toneRow: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 7) {
                ForEach(AIWriteTone.allCases) { option in
                    let selected = tone == option
                    Button {
                        withAnimation(.snappy) { tone = option }
                    } label: {
                        Text(option.label)
                            .font(.subheadline.weight(selected ? .semibold : .medium))
                            .padding(.horizontal, 13)
                            .padding(.vertical, 7)
                            .foregroundStyle(selected ? Color.white : Color.primary)
                            .background(
                                selected ? AnyShapeStyle(Tone.indigo.color.gradient) : AnyShapeStyle(Tone.slate.background),
                                in: Capsule()
                            )
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 2)
        }
        .sensoryFeedback(.selection, trigger: tone)
    }

    private var generateButton: some View {
        Button {
            Task { await generate() }
        } label: {
            HStack(spacing: 8) {
                if isGenerating {
                    ProgressView().tint(.white)
                    Text("Writing")
                } else {
                    Image(systemName: "sparkles")
                    Text(result == nil ? "Generate draft" : "Try again")
                }
            }
            .font(.body.weight(.semibold))
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .tint(Tone.indigo.color)
        .controlSize(.large)
        .disabled(prompt.trimmingCharacters(in: .whitespaces).isEmpty || isGenerating)
    }

    private func resultCard(_ result: AIWriteResponse) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(result.text)
                .font(.body)
                .textSelection(.enabled)
            HStack {
                if let credits = result.creditsRemaining {
                    Label("\(credits) credits left", systemImage: "bolt.fill")
                        .font(.caption.weight(.medium))
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Button {
                    onInsert(result.text)
                    dismiss()
                } label: {
                    Label("Insert", systemImage: "text.insert")
                        .font(.subheadline.weight(.semibold))
                }
                .buttonStyle(.borderedProminent)
                .tint(WTheme.accent)
            }
        }
        .airCard()
        .transition(.opacity.combined(with: .move(edge: .bottom)))
    }

    private func generate() async {
        isGenerating = true
        errorMessage = nil
        do {
            var body = ["prompt": prompt.trimmingCharacters(in: .whitespacesAndNewlines)]
            if tone != .standard { body["tone"] = tone.rawValue }
            let response: AIWriteResponse = try await env.api.post(
                "generation/write", body: body, idempotent: true
            )
            withAnimation(.snappy) { result = response }
            generated += 1
        } catch let error as APIError {
            if case let .server(status, _) = error, status == 402 {
                errorMessage = "You're out of AI credits for this billing period."
            } else {
                errorMessage = error.localizedDescription
            }
        } catch {
            errorMessage = error.localizedDescription
        }
        isGenerating = false
    }
}

// MARK: - Template picker

/// Inserts a reply template's plain body into the draft.
struct UniboxTemplatePicker: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    var onPick: (EmailTemplate) -> Void

    @State private var store = TemplatesStore()

    var body: some View {
        NavigationStack {
            Group {
                if store.isLoading, !store.hasLoaded {
                    ProgressView()
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if store.templates.isEmpty {
                    EmptyStateView(
                        title: "No templates yet",
                        message: "Save reply templates from the More tab or the web dashboard."
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
                                    if let snippet = template.bodyPlain?
                                        .trimmingCharacters(in: .whitespacesAndNewlines), !snippet.isEmpty {
                                        Text(snippet)
                                            .font(.footnote)
                                            .foregroundStyle(.secondary)
                                            .lineLimit(2)
                                    }
                                }
                                .padding(.vertical, 2)
                            }
                        }
                    }
                }
            }
            .navigationTitle("Templates")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .task { await store.load(env.api) }
    }
}
