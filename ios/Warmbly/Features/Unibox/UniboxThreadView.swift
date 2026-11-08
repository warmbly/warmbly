import SwiftUI

/// Pushed conversation view: a flat, full-width Gmail-style stack. Messages
/// share one background, separated by hairline dividers, connected by a left
/// rail between avatars with "Replying to" references resolved from
/// `In-Reply-To`; unread messages carry a sky dot. Tools live per message
/// (reply / copy / mark unread), with no bottom action bar. AI shows up as the
/// inbox agent's drafted reply at the foot, a one-tap drafted reply, and Remie.
struct UniboxThreadView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    @Environment(\.closeDetail) private var closeDetail
    @Environment(\.threadReplyRequest) private var replyRequest

    @State private var store: UniboxThreadStore
    @State private var showComposer = false
    @State private var showLabelEditor = false
    @State private var showContactPanel = false
    @State private var cancelTarget: ScheduledSend?
    @State private var actionError: String?
    @State private var snoozed = false
    @State private var autoReplyConsumed = false
    /// Instant reply inside its undo window.
    @State private var undoSend: ReplyUndoSend?
    /// Composer fields to restore after an undo re-presents the sheet.
    @State private var restoreSnapshot: UniboxReplySnapshot?
    @State private var toast: String?
    @State private var toastPulse = 0
    /// Opens the composer already drafting a reply from the thread.
    @State private var composerAutoDraft = false
    @State private var composerAgentDraft: AgentThreadDraft?
    @State private var agentDraft: AgentThreadDraft?
    @State private var agentDraftText = ""
    @State private var agentDraftBusy = false
    @State private var confirmDiscardDraft = false
    @State private var remiePrompt: AssistantLaunch?

    /// Jumps straight into the reply composer once the thread loads
    /// (the list row's "Reply" quick action).
    let openComposerOnAppear: Bool

    init(thread: UniboxThread, openComposerOnAppear: Bool = false) {
        _store = State(initialValue: UniboxThreadStore(thread: thread))
        _undoSend = State(initialValue: ReplyUndoSend.pending(for: thread.key))
        self.openComposerOnAppear = openComposerOnAppear
    }

    private var presenceKey: String { "thread:\(store.thread.key)" }
    private var canReply: Bool { env.session.can(.accessUnibox) }
    private var canAI: Bool { env.session.can(.useAI) }

    var body: some View {
        content
            .navigationTitle(store.subject)
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(WTheme.canvas, for: .navigationBar)
            .toolbar { toolbarContent }
            .presenceResource(presenceKey, action: showComposer ? "replying" : "viewing")
            // A conversation reads full height, like Mail; its actions live in the thread.
            .tabBarHidden(true)
            .task {
                async let draft: Void = loadAgentDraft()
                await store.load(env.api)
                await draft
                if openComposerOnAppear, !autoReplyConsumed, canReply, composeContext != nil {
                    autoReplyConsumed = true
                    showComposer = true
                }
            }
            .onChange(of: env.realtime.pulse(for: .unibox)) {
                Task {
                    await store.load(env.api)
                    await loadAgentDraft()
                }
            }
            .onChange(of: replyRequest) {
                if canReply, composeContext != nil { openComposer(drafting: false) }
            }
            .onChange(of: undoSend?.taskID) {
                ReplyUndoSend.remember(undoSend, for: store.thread.key)
            }
            .overlay(alignment: .bottom) {
                VStack(spacing: 8) {
                    if let toast {
                        Text(toast)
                            .font(.system(size: 13.5, weight: .medium))
                            .foregroundStyle(WTheme.inkInverse)
                            .padding(.horizontal, 16)
                            .frame(height: 38)
                            .background(WTheme.ink, in: Capsule())
                            .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                    if let undoSend {
                        UndoSendBanner(scheduledAt: undoSend.scheduledAt) {
                            await undoReplySend(undoSend)
                        } onExpired: {
                            withAnimation(.snappy) { self.undoSend = nil }
                            Task { await store.load(env.api) }
                        }
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                }
                .padding(.bottom, 12)
            }
            .sensoryFeedback(.success, trigger: toastPulse)
            .sheet(isPresented: $showComposer, onDismiss: {
                restoreSnapshot = nil
                composerAutoDraft = false
                composerAgentDraft = nil
            }) {
                if let context = composeContext {
                    UniboxComposer(
                        context: context,
                        autoDraft: composerAutoDraft,
                        restore: restoreSnapshot,
                        agentDraft: composerAgentDraft,
                        onKeepDraft: composerAgentDraft == nil ? nil : { text in agentDraftText = text }
                    ) { response, snapshot in
                        if composerAgentDraft != nil {
                            withAnimation(.snappy) { agentDraft = nil }
                        }
                        Task { await store.load(env.api) }
                        switch response.sendMode {
                        case "smart", "scheduled":
                            break
                        default:
                            // Instant replies queue a short undo window server-side.
                            if let taskID = response.taskID, let scheduledAt = response.scheduledAt {
                                withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                                    undoSend = ReplyUndoSend(taskID: taskID, scheduledAt: scheduledAt, snapshot: snapshot)
                                }
                            }
                        }
                    }
                }
            }
            .fullScreenCover(item: $remiePrompt) { launch in
                AssistantView(page: "unibox", initialPrompt: launch.prompt)
            }
            .confirmationDialog("Discard this drafted reply?", isPresented: $confirmDiscardDraft, titleVisibility: .visible) {
                Button("Discard", role: .destructive) {
                    Task { await discardAgentDraft() }
                }
            } message: {
                Text("Nothing was sent. You can still write a reply yourself.")
            }
            .sheet(isPresented: $showLabelEditor) {
                UniboxLabelPicker(mode: .thread(store))
            }
            .sheet(isPresented: $showContactPanel) {
                UniboxContactPanel(email: store.counterpartyAddress ?? "")
                    // The panel is its own sheet; leaving it must not close the thread behind it.
                    .environment(\.closeDetail, nil)
            }
            .sensoryFeedback(.impact(weight: .light), trigger: snoozed)
            .confirmationDialog(
                "Cancel scheduled send?",
                isPresented: Binding(
                    get: { cancelTarget != nil },
                    set: { if !$0 { cancelTarget = nil } }
                ),
                titleVisibility: .visible,
                presenting: cancelTarget
            ) { item in
                Button("Cancel send", role: .destructive) {
                    Task { await cancelScheduled(item) }
                }
            } message: { _ in
                Text("This send won't go out.")
            }
            .alert("Something went wrong", isPresented: Binding(
                get: { actionError != nil },
                set: { if !$0 { actionError = nil } }
            )) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actionError ?? "")
            }
    }

    // MARK: Toolbar

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        // The subject lives in the content header; the bar carries a quiet count.
        ToolbarItem(placement: .principal) {
            if store.hasLoaded, !store.messages.isEmpty {
                Text("\(store.messages.count) message\(store.messages.count == 1 ? "" : "s")")
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(WTheme.inkTertiary)
                    .contentTransition(.numericText())
            }
        }
        ToolbarItemGroup(placement: .topBarTrailing) {
            ResourceViewers(resource: presenceKey)
            if canReply, canAI, composeContext != nil, agentDraft == nil {
                Button {
                    openComposer(drafting: true)
                } label: {
                    Image(systemName: "sparkles")
                }
                .accessibilityLabel("Draft a reply with AI")
            }
            if canReply, composeContext != nil {
                Button {
                    showComposer = true
                } label: {
                    Image(systemName: "arrowshape.turn.up.left")
                }
                .accessibilityLabel("Reply")
            }
            Menu {
                if store.counterpartyAddress != nil {
                    Button {
                        showContactPanel = true
                    } label: {
                        Label("View contact", systemImage: "person.crop.circle")
                    }
                }
                if canReply {
                    Button {
                        showLabelEditor = true
                    } label: {
                        Label("Edit labels", systemImage: "tag")
                    }
                    Menu {
                        ForEach(Array(UniboxSnoozePreset.allCases.enumerated()), id: \.offset) { _, preset in
                            Button(preset.label) {
                                Task { await snooze(until: preset.date) }
                            }
                        }
                    } label: {
                        Label("Snooze", systemImage: "moon.zzz")
                    }
                }
                if canAI {
                    Button {
                        remiePrompt = AssistantLaunch(prompt: remieSummaryPrompt)
                    } label: {
                        Label("Summarize with Remie", systemImage: "sparkle")
                    }
                }
                Button {
                    Task { await store.load(env.api) }
                } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
            } label: {
                Image(systemName: "ellipsis.circle")
            }
        }
    }

    // MARK: Content

    @ViewBuilder
    private var content: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { SkeletonRows(rows: 4) }
        } else if let error = store.errorMessage, store.messages.isEmpty {
            ErrorStateView(title: "Couldn't load conversation", message: error) {
                await store.load(env.api)
            }
        } else if store.messages.isEmpty {
            EmptyStateView(title: "No messages", message: "This conversation is empty.")
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        subjectHeader
                        if !store.labels.isEmpty || canReply { labelStrip }
                        Hairline()
                        ForEach(Array(store.messages.enumerated()), id: \.element.id) { index, message in
                            UniboxMessageRow(
                                message: message,
                                store: store,
                                isFirst: index == 0,
                                isLast: index == store.messages.count - 1,
                                repliedTo: store.repliedToMessage(before: index),
                                onReply: canReply && composeContext != nil ? {
                                    openComposer(drafting: false)
                                } : nil,
                                onAIReply: canReply && canAI && composeContext != nil && agentDraft == nil
                                    && index == store.messages.count - 1 ? {
                                    openComposer(drafting: true)
                                } : nil,
                                onJump: { id in
                                    withAnimation(.snappy) { proxy.scrollTo(id, anchor: .top) }
                                }
                            )
                            .id(message.id)
                        }
                        if let agentDraft, canReply {
                            AgentDraftCard(
                                draft: agentDraft,
                                text: agentDraftText,
                                busy: agentDraftBusy,
                                onSend: { Task { await sendAgentDraft() } },
                                onEdit: { editAgentDraft(agentDraft) },
                                onRevert: { withAnimation(.snappy) { agentDraftText = agentDraft.body } },
                                onDiscard: { confirmDiscardDraft = true }
                            )
                            .padding(.horizontal, 16)
                            .padding(.top, 6)
                            .padding(.bottom, 14)
                            .id("agent-draft")
                            .transition(.opacity.combined(with: .move(edge: .bottom)))
                        }
                        if !store.scheduled.isEmpty { scheduledStrip }
                        Color.clear.frame(height: 1).id("thread-end")
                    }
                    .animation(.spring(response: 0.4, dampingFraction: 0.86), value: agentDraft?.id)
                    .padding(.vertical, 4)
                }
                .background(WTheme.canvas)
                .refreshable { await store.load(env.api) }
                // This view only exists once loaded, so land here rather than on a load change.
                .task {
                    // Land on the newest message like a mail client.
                    if store.messages.count > 1, let last = store.messages.last?.id {
                        await Task.yield()
                        proxy.scrollTo(last, anchor: .top)
                    }
                    #if DEBUG
                    if ProcessInfo.processInfo.environment["WARMBLY_DEBUG_THREAD"] == "end" {
                        Task {
                            try? await Task.sleep(for: .seconds(1.5))
                            proxy.scrollTo("thread-end", anchor: .bottom)
                        }
                    }
                    if ProcessInfo.processInfo.environment["WARMBLY_DEBUG_THREAD"] == "edit" {
                        Task {
                            try? await Task.sleep(for: .seconds(1.5))
                            if let agentDraft { editAgentDraft(agentDraft) }
                        }
                    }
                    #endif
                }
            }
        }
    }

    // MARK: Header

    private var subjectHeader: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let mailbox = store.thread.mailboxEmail ?? store.replyAccountEmail {
                Text(mailbox)
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(WTheme.inkTertiary)
                    .lineLimit(1)
            }
            Text(store.subject.isEmpty ? "(no subject)" : store.subject)
                .displayFont(24)
                .foregroundStyle(WTheme.ink)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
        }
        .padding(.horizontal, 16)
        .padding(.top, 10)
        .padding(.bottom, 12)
    }

    // MARK: Labels

    private var labelStrip: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(store.labels) { label in
                    LabelChip(label: label)
                }
                if canReply {
                    Button {
                        showLabelEditor = true
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: store.labels.isEmpty ? "tag" : "plus")
                                .font(.system(size: 10, weight: .medium))
                            if store.labels.isEmpty {
                                Text("Add label")
                                    .font(.system(size: 12, weight: .medium))
                            }
                        }
                        .foregroundStyle(WTheme.inkSecondary)
                        .padding(.horizontal, store.labels.isEmpty ? 10 : 0)
                        .frame(minWidth: 26, minHeight: 26)
                        .background(WTheme.raised, in: Capsule())
                        .overlay(
                            Capsule().strokeBorder(style: StrokeStyle(lineWidth: 1, dash: [3, 3]))
                                .foregroundStyle(WTheme.hairlineStrong)
                        )
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Edit labels")
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 2)
        }
        .padding(.bottom, 14)
    }

    // MARK: Scheduled inline

    private var scheduledStrip: some View {
        VStack(alignment: .leading, spacing: 10) {
            Hairline()
            Text("Scheduled")
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(WTheme.inkTertiary)
                .padding(.horizontal, 16)
                .padding(.top, 8)
            ForEach(store.scheduled) { item in
                ScheduledRow(item: item, showThread: false) {
                    cancelTarget = item
                }
                .padding(.horizontal, 16)
            }
        }
        .padding(.bottom, 12)
    }

    // MARK: Compose context

    private var composeContext: UniboxComposeContext? {
        guard let accountID = store.replyAccountID else { return nil }
        let counterparty = store.counterpartyAddress
        let baseSubject = store.subject
        let subject: String = {
            let lowered = baseSubject.lowercased()
            if lowered.hasPrefix("re:") || lowered.hasPrefix("fwd:") { return baseSubject }
            return baseSubject.isEmpty ? "Re:" : "Re: \(baseSubject)"
        }()
        let inReplyTo = store.latestMessageIDHeader.map { [$0] } ?? []
        return UniboxComposeContext(
            accountID: accountID,
            accountEmail: store.replyAccountEmail,
            to: counterparty.map { [$0] } ?? [],
            cc: [],
            subject: subject,
            threadID: store.thread.key,
            inReplyTo: inReplyTo
        )
    }

    // MARK: AI

    private func openComposer(drafting: Bool) {
        composerAutoDraft = drafting
        showComposer = true
    }

    /// Opens the full composer on the draft, with every AI tool, instead of editing in the card.
    private func editAgentDraft(_ draft: AgentThreadDraft) {
        restoreSnapshot = UniboxReplySnapshot(to: [draft.toAddr], cc: [], bcc: [], subject: draft.subject, body: agentDraftText)
        composerAgentDraft = draft
        composerAutoDraft = false
        showComposer = true
    }

    /// Sent to Remie as the first message; the agent reads the thread itself.
    private var remieSummaryPrompt: String {
        let who = store.counterpartyAddress.map { " with \($0)" } ?? ""
        let subject = store.subject.isEmpty ? "" : " (subject \"\(store.subject)\")"
        return "Summarize my email conversation\(who)\(subject), thread \(store.thread.key). Tell me where it stands and what I should do next."
    }

    private struct DataList<T: Decodable>: Decodable { var data: [T] }

    /// The inbox agent's pending draft for this thread, if any.
    private func loadAgentDraft() async {
        guard canReply else { return }
        guard let list: DataList<AgentThreadDraft> = try? await env.api.get("unibox/agent-drafts") else { return }
        let match = list.data.first { $0.threadID == store.thread.key && $0.status == "pending" }
        if match?.id != agentDraft?.id { agentDraftText = match?.body ?? "" }
        agentDraft = match
    }

    private func sendAgentDraft() async {
        guard let draft = agentDraft, !agentDraftBusy else { return }
        agentDraftBusy = true
        defer { agentDraftBusy = false }
        let text = agentDraftText.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            let response: UniboxSendResponse = try await env.api.post(
                "unibox/agent-drafts/\(draft.id)/approve",
                body: AgentDraftApproveBody(body: text == draft.body ? nil : text),
                idempotent: true
            )
            withAnimation(.snappy) { agentDraft = nil }
            await store.load(env.api)
            let snapshot = UniboxReplySnapshot(to: [draft.toAddr], cc: [], bcc: [], subject: draft.subject, body: text)
            if response.sendMode == "instant" || response.sendMode == nil,
               let taskID = response.taskID, let scheduledAt = response.scheduledAt {
                withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                    undoSend = ReplyUndoSend(taskID: taskID, scheduledAt: scheduledAt, snapshot: snapshot)
                }
            } else {
                showToast("Reply queued")
            }
        } catch let error as APIError where error.code == "conflict" {
            withAnimation(.snappy) { agentDraft = nil }
            showToast("This draft was already handled")
        } catch {
            actionError = error.localizedDescription
        }
    }

    private func discardAgentDraft() async {
        guard let draft = agentDraft, !agentDraftBusy else { return }
        agentDraftBusy = true
        defer { agentDraftBusy = false }
        do {
            let _: EmptyBody = try await env.api.post("unibox/agent-drafts/\(draft.id)/discard")
            withAnimation(.snappy) { agentDraft = nil }
            showToast("Draft discarded")
        } catch APIError.server(let status, _) where status == 404 {
            withAnimation(.snappy) { agentDraft = nil }
        } catch {
            actionError = error.localizedDescription
        }
    }

    // MARK: Actions

    /// Back to the list: pops the stack, or empties the detail column beside it.
    private func leave() {
        if let closeDetail { closeDetail() } else { dismiss() }
    }

    private func snooze(until: Date) async {
        do {
            try await store.snooze(env.api, until: until)
            snoozed.toggle()
            leave()
        } catch {
            actionError = error.localizedDescription
        }
    }

    private func cancelScheduled(_ item: ScheduledSend) async {
        try? await store.cancelScheduled(env.api, taskID: item.taskID)
    }

    /// Cancels the queued instant reply and re-presents the composer with the
    /// sent fields restored. 404 means the send already fired.
    private func undoReplySend(_ undo: ReplyUndoSend) async {
        do {
            let _: EmptyBody = try await env.api.delete("unibox/scheduled/\(undo.taskID)")
            withAnimation(.snappy) { undoSend = nil }
            restoreSnapshot = undo.snapshot
            openComposer(drafting: false)
        } catch APIError.server(let status, _) where status == 404 {
            withAnimation(.snappy) { undoSend = nil }
            showToast("Already sent")
            Task { await store.load(env.api) }
        } catch {
            // Keep the banner so the user can retry within the window.
            showToast(error.localizedDescription)
        }
    }

    private func showToast(_ text: String) {
        toastPulse += 1
        withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) { toast = text }
        let shown = toastPulse
        Task {
            try? await Task.sleep(for: .seconds(2.5))
            guard shown == toastPulse else { return }
            withAnimation(.snappy) { toast = nil }
        }
    }
}

/// An instant reply waiting out its undo window: the queued task, the moment
/// it fires, and the composer snapshot to restore on undo.
private struct ReplyUndoSend {
    let taskID: String
    let scheduledAt: Date
    let snapshot: UniboxReplySnapshot

    /// Kept per thread, so a view rebuilt mid-window (a resize across the split) still offers undo.
    @MainActor private static var byThread: [String: ReplyUndoSend] = [:]

    @MainActor static func pending(for threadKey: String) -> ReplyUndoSend? {
        guard let undo = byThread[threadKey], undo.scheduledAt > .now else { return nil }
        return undo
    }

    @MainActor static func remember(_ undo: ReplyUndoSend?, for threadKey: String) {
        byThread[threadKey] = undo
    }
}

extension EnvironmentValues {
    /// Bumped by the list to open the composer of the thread already on screen.
    @Entry var threadReplyRequest = 0
}

// MARK: - Message row

/// One flat, full-width message: avatar hangs on a connector rail that links
/// the conversation top to bottom, a hairline divider separates rows, a
/// "Replying to" reference names the quoted message, unread carries a sky dot,
/// and expanded messages get their own tool row (reply / copy / read state).
private struct UniboxMessageRow: View {
    @Environment(AppEnvironment.self) private var env
    let message: UniboxMessage
    let store: UniboxThreadStore
    let isFirst: Bool
    let isLast: Bool
    let repliedTo: UniboxMessage?
    var onReply: (() -> Void)?
    var onAIReply: (() -> Void)?
    let onJump: (String) -> Void

    @State private var showDetails = false
    @State private var copied = false

    /// Rail geometry hangs off the 36pt avatar at (16, 14).
    private static let avatarSize: CGFloat = 36
    private static let contentInset: CGFloat = 16 + avatarSize + 12
    private static let railX: CGFloat = 16 + avatarSize / 2

    private var detail: UniboxMessageDetail? { store.bodies[message.id] }
    private var isUnread: Bool { message.seen == false }
    private var isFromMe: Bool { Self.fromMe(message, store: store) }
    private var expanded: Bool { store.expandedIDs.contains(message.id) }

    /// A message sent from the org's own mailbox reads as "You".
    static func fromMe(_ message: UniboxMessage, store: UniboxThreadStore) -> Bool {
        let sender = message.senderBare.lowercased()
        guard !sender.isEmpty else { return false }
        let mine = (store.thread.mailboxEmail ?? store.replyAccountEmail ?? "").lowercased()
        return !mine.isEmpty && sender == mine
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let repliedTo { referenceLink(repliedTo) }
            header
                .contentShape(Rectangle())
                .onTapGesture { toggle() }
            if expanded {
                if showDetails, let detail {
                    detailBlock(detail)
                }
                if let detail {
                    if let native = UniboxWebView.nativeText(html: detail.bodyHTML, plain: detail.bodyPlain) {
                        Text(native)
                            .font(.system(size: 15))
                            .foregroundStyle(WTheme.ink)
                            .lineSpacing(4)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.vertical, 2)
                    } else {
                        UniboxWebView(
                            html: detail.bodyHTML,
                            plain: detail.bodyPlain,
                            height: Binding(
                                get: { store.bodyHeights[message.id] ?? 40 },
                                set: { store.bodyHeights[message.id] = $0 }
                            )
                        )
                        .frame(height: store.bodyHeights[message.id] ?? 40)
                    }
                    toolsRow
                } else {
                    HStack(spacing: 8) {
                        ProgressView().controlSize(.small)
                        Text("Loading message")
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.vertical, 8)
                }
            } else {
                Text(message.snippet ?? "")
                    .font(.system(size: 14))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(2)
                    .contentShape(Rectangle())
                    .onTapGesture { toggle() }
            }
        }
        .padding(.leading, Self.contentInset)
        .padding(.trailing, 16)
        .padding(.vertical, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(alignment: .topLeading) {
            WAvatar(name: message.senderDisplay, seed: message.senderBare, size: Self.avatarSize)
                .padding(.leading, 16)
                .padding(.top, 14)
        }
        .background(alignment: .leading) { rail }
        .overlay(alignment: .bottom) {
            if !isLast {
                Hairline().padding(.leading, Self.contentInset)
            }
        }
        .task(id: expanded) {
            if expanded { await store.loadBody(env.api, messageID: message.id) }
        }
    }

    /// The connector between avatars. Segments stop at the avatar's edges
    /// (the avatar fill is translucent, so a through-line would show inside it):
    /// a stub from the row top to the avatar unless first, and a run from the
    /// avatar to the row bottom unless last. Skipped for singletons.
    @ViewBuilder
    private var rail: some View {
        if !(isFirst && isLast) {
            VStack(spacing: 0) {
                railSegment
                    .frame(height: 14)
                    .opacity(isFirst ? 0 : 1)
                Color.clear.frame(width: 2, height: Self.avatarSize)
                if isLast {
                    Spacer(minLength: 0)
                } else {
                    railSegment.frame(maxHeight: .infinity)
                }
            }
            .padding(.leading, Self.railX - 1)
        }
    }

    private var railSegment: some View {
        Rectangle()
            .fill(WTheme.hairlineStrong)
            .frame(width: 2)
    }

    /// Small "Replying to X" reference naming the quoted message; tapping
    /// scrolls the stack to it.
    private func referenceLink(_ target: UniboxMessage) -> some View {
        Button {
            onJump(target.id)
        } label: {
            HStack(spacing: 5) {
                Image(systemName: "arrow.turn.up.left")
                    .font(.system(size: 9, weight: .medium))
                Text("Replying to \(Self.fromMe(target, store: store) ? "you" : target.senderDisplay)")
                    .lineLimit(1)
            }
            .font(.system(size: 12.5, weight: .medium))
            .foregroundStyle(WTheme.inkTertiary)
        }
        .buttonStyle(.plain)
    }

    private var header: some View {
        HStack(alignment: .top, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(isFromMe ? "You" : message.senderDisplay)
                        .font(.system(size: 15, weight: isUnread ? .semibold : .medium))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                    if isFromMe {
                        Image(systemName: "arrowshape.turn.up.left.fill")
                            .font(.system(size: 9.5, weight: .medium))
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                }
                if expanded {
                    Button {
                        withAnimation(.snappy) { showDetails.toggle() }
                    } label: {
                        HStack(spacing: 4) {
                            Text(message.recipientBare.isEmpty ? message.senderBare : "to \(message.recipientBare)")
                                .lineLimit(1)
                            Image(systemName: "chevron.down")
                                .font(.system(size: 8, weight: .semibold))
                                .rotationEffect(.degrees(showDetails ? 180 : 0))
                        }
                        .font(.system(size: 13))
                        .foregroundStyle(WTheme.inkSecondary)
                    }
                    .buttonStyle(.plain)
                } else {
                    Text(message.recipientBare.isEmpty ? message.senderBare : "to \(message.recipientBare)")
                        .font(.system(size: 13))
                        .foregroundStyle(WTheme.inkSecondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 6)
            VStack(alignment: .trailing, spacing: 5) {
                Text(UniboxFormat.listTime(message.internalDate))
                    .font(.system(size: 13, weight: isUnread ? .semibold : .regular))
                    .monospacedDigit()
                    .foregroundStyle(isUnread ? WTheme.ink : WTheme.inkTertiary)
                if isUnread {
                    Circle().fill(WTheme.accent).frame(width: 7, height: 7)
                }
            }
        }
    }

    /// Per-message tools under the body: reply as the primary, then quiet
    /// copy and read-state controls. Replaces the old bottom action bar.
    private var toolsRow: some View {
        HStack(spacing: 10) {
            if let onReply {
                Button(action: onReply) {
                    HStack(spacing: 6) {
                        Image(systemName: "arrowshape.turn.up.left")
                            .font(.system(size: 11.5, weight: .medium))
                        Text("Reply")
                    }
                }
                .buttonStyle(OutlineButtonStyle(size: .small))
            }
            if let onAIReply {
                Button(action: onAIReply) {
                    HStack(spacing: 6) {
                        Image(systemName: "sparkles")
                            .font(.system(size: 11.5, weight: .medium))
                            .foregroundStyle(WTheme.accent)
                        Text("Draft with AI")
                    }
                }
                .buttonStyle(OutlineButtonStyle(size: .small))
            }
            Spacer(minLength: 0)
            Button(action: copyBody) {
                toolIcon(copied ? "checkmark" : "doc.on.doc")
            }
            .buttonStyle(TapScaleStyle())
            .accessibilityLabel("Copy message text")
            Menu {
                Button {
                    Task { await store.setSeen(env.api, messageID: message.id, seen: isUnread) }
                } label: {
                    Label(
                        isUnread ? "Mark as read" : "Mark as unread",
                        systemImage: isUnread ? "envelope.open" : "envelope.badge"
                    )
                }
                Button {
                    withAnimation(.snappy) { showDetails.toggle() }
                } label: {
                    Label(showDetails ? "Hide details" : "View details", systemImage: "info.circle")
                }
                Button(action: copyBody) {
                    Label("Copy text", systemImage: "doc.on.doc")
                }
            } label: {
                toolIcon("ellipsis")
            }
            .accessibilityLabel("More message actions")
        }
    }

    private func toolIcon(_ symbol: String) -> some View {
        Image(systemName: symbol)
            .font(.system(size: 12.5, weight: .medium))
            .foregroundStyle(WTheme.inkSecondary)
            .frame(width: 34, height: 34)
            .background(WTheme.raised, in: Circle())
            .overlay(Circle().strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
    }

    private func copyBody() {
        UIPasteboard.general.string = detail?.bodyPlain ?? message.snippet ?? ""
        withAnimation(.snappy) { copied = true }
        Task {
            try? await Task.sleep(for: .seconds(1.6))
            withAnimation(.snappy) { copied = false }
        }
    }

    /// Gmail-style "details" block: full addressing + exact timestamp.
    private func detailBlock(_ detail: UniboxMessageDetail) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            detailRow("From", detail.from?.joined(separator: ", "))
            detailRow("To", detail.to?.joined(separator: ", "))
            detailRow("Cc", detail.cc?.joined(separator: ", "))
            detailRow("Date", UniboxFormat.absoluteTime(detail.internalDate ?? detail.date))
            detailRow("Subject", detail.subject)
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(WTheme.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .strokeBorder(WTheme.hairlineStrong, lineWidth: 1)
        )
        .transition(.opacity.combined(with: .move(edge: .top)))
    }

    @ViewBuilder
    private func detailRow(_ label: String, _ value: String?) -> some View {
        if let value, !value.isEmpty {
            HStack(alignment: .top, spacing: 8) {
                Text(label)
                    .font(.system(size: 12.5, weight: .medium))
                    .foregroundStyle(WTheme.inkTertiary)
                    .frame(width: 56, alignment: .leading)
                    .padding(.top, 1.5)
                Text(value)
                    .font(.system(size: 12.5))
                    .foregroundStyle(WTheme.inkSecondary)
                    .textSelection(.enabled)
            }
        }
    }

    private func toggle() {
        withAnimation(.snappy) {
            if expanded {
                store.expandedIDs.remove(message.id)
            } else {
                store.expandedIDs.insert(message.id)
            }
        }
    }
}

// MARK: - Contact panel

/// CRM side panel: resolves the counterparty address to a contact and shows
/// the full contact profile inline; offers nothing when there is no match.
private struct UniboxContactPanel: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    let email: String

    @State private var contact: Contact?
    @State private var isLoading = true
    @State private var notFound = false

    var body: some View {
        NavigationStack {
            Group {
                if isLoading {
                    ProgressView()
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if let contact {
                    ContactDetailView(contact: contact) { _ in } onDeleted: { _ in
                        dismiss()
                    }
                } else {
                    VStack(spacing: 14) {
                        IconTile(symbol: "person.crop.circle.badge.questionmark", tone: .slate, size: 54)
                        Text("Not in contacts")
                            .displayFont(17)
                            .foregroundStyle(WTheme.ink)
                        Text(email)
                            .font(.system(size: 14))
                            .foregroundStyle(WTheme.inkSecondary)
                            .textSelection(.enabled)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .background(WTheme.canvas)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
        .presentationDragIndicator(.visible)
        .task { await lookup() }
    }

    private func lookup() async {
        defer { isLoading = false }
        do {
            let envelope: ContactLookupEnvelope = try await env.api.get(
                "contacts/lookup", query: ["email": email]
            )
            contact = envelope.contact
            notFound = contact == nil
        } catch {
            notFound = true
        }
    }
}

// MARK: - Label chip

struct LabelChip: View {
    let label: UniboxLabel

    private var tint: Color { Color(uniboxHex: label.color) ?? WTheme.accent }

    var body: some View {
        HStack(spacing: 5) {
            Circle().fill(tint).frame(width: 7, height: 7)
            Text(label.title ?? "Label")
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(WTheme.ink)
        }
        .padding(.horizontal, 10)
        .frame(height: 26)
        .background(WTheme.raised, in: Capsule())
        .overlay(Capsule().strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
    }
}
