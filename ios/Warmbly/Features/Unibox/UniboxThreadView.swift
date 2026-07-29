import SwiftUI

/// Pushed conversation view: a flat, full-width Gmail-style stack. Messages
/// share one background, separated by hairline dividers, connected by a left
/// rail between avatars with "Replying to" references resolved from
/// `In-Reply-To`; unread messages are marked in red. Tools live per message
/// (reply / copy / mark unread) — no bottom action bar; AI stays a helper
/// inside the composer.
struct UniboxThreadView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

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

    /// Jumps straight into the reply composer once the thread loads
    /// (the list row's "Reply" quick action).
    let openComposerOnAppear: Bool

    init(thread: UniboxThread, openComposerOnAppear: Bool = false) {
        _store = State(initialValue: UniboxThreadStore(thread: thread))
        self.openComposerOnAppear = openComposerOnAppear
    }

    private var presenceKey: String { "thread:\(store.thread.key)" }
    private var canReply: Bool { env.session.can(.accessUnibox) }

    var body: some View {
        content
            .navigationTitle(store.subject)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbarContent }
            .presenceResource(presenceKey, action: showComposer ? "replying" : "viewing")
            .task {
                await store.load(env.api)
                if openComposerOnAppear, !autoReplyConsumed, canReply, composeContext != nil {
                    autoReplyConsumed = true
                    showComposer = true
                }
            }
            .onChange(of: env.realtime.pulse(for: .unibox)) {
                Task { await store.load(env.api) }
            }
            .overlay(alignment: .bottom) {
                VStack(spacing: 8) {
                    if let toast {
                        Text(toast)
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(.white)
                            .padding(.horizontal, 14)
                            .padding(.vertical, 9)
                            .background(.black.opacity(0.82), in: Capsule())
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
            .sheet(isPresented: $showComposer, onDismiss: { restoreSnapshot = nil }) {
                if let context = composeContext {
                    UniboxComposer(context: context, restore: restoreSnapshot) { response, snapshot in
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
            .sheet(isPresented: $showLabelEditor) {
                UniboxLabelPicker(mode: .thread(store))
            }
            .sheet(isPresented: $showContactPanel) {
                UniboxContactPanel(email: store.counterpartyAddress ?? "")
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
        ToolbarItemGroup(placement: .topBarTrailing) {
            ResourceViewers(resource: presenceKey)
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
                        if !store.labels.isEmpty { labelStrip }
                        ForEach(Array(store.messages.enumerated()), id: \.element.id) { index, message in
                            UniboxMessageRow(
                                message: message,
                                store: store,
                                isFirst: index == 0,
                                isLast: index == store.messages.count - 1,
                                repliedTo: store.repliedToMessage(before: index),
                                onReply: canReply && composeContext != nil ? {
                                    showComposer = true
                                } : nil,
                                onJump: { id in
                                    withAnimation(.snappy) { proxy.scrollTo(id, anchor: .top) }
                                }
                            )
                            .id(message.id)
                        }
                        if !store.scheduled.isEmpty { scheduledStrip }
                    }
                    .padding(.vertical, 4)
                }
                .background(Color(.systemBackground))
                .refreshable { await store.load(env.api) }
                .onChange(of: store.hasLoaded) {
                    // Land on the newest message like a mail client.
                    if let last = store.messages.last?.id {
                        proxy.scrollTo(last, anchor: .top)
                    }
                }
            }
        }
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
                        Image(systemName: "plus")
                            .font(.system(size: 11, weight: .semibold))
                            .foregroundStyle(.secondary)
                            .padding(6)
                            .background(Tone.slate.background, in: Circle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Edit labels")
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 2)
        }
        .padding(.bottom, 8)
    }

    // MARK: Scheduled inline

    private var scheduledStrip: some View {
        VStack(alignment: .leading, spacing: 10) {
            Divider()
            EyebrowLabel("Scheduled")
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

    // MARK: Actions

    private func snooze(until: Date) async {
        do {
            try await store.snooze(env.api, until: until)
            snoozed.toggle()
            dismiss()
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
            showComposer = true
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
}

// MARK: - Message row

/// One flat, full-width message: avatar hangs on a connector rail that links
/// the conversation top to bottom, a hairline divider separates rows, a
/// "Replying to" reference names the quoted message, unread is marked in red,
/// and expanded messages get their own tool row (reply / copy / read state).
private struct UniboxMessageRow: View {
    @Environment(AppEnvironment.self) private var env
    let message: UniboxMessage
    let store: UniboxThreadStore
    let isFirst: Bool
    let isLast: Bool
    let repliedTo: UniboxMessage?
    var onReply: (() -> Void)?
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
                            .lineSpacing(3)
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
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.vertical, 8)
                }
            } else {
                Text(message.snippet ?? "")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
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
                Divider().padding(.leading, Self.contentInset)
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
            .fill(Color(.systemGray4))
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
                    .font(.system(size: 9, weight: .bold))
                Text("Replying to \(Self.fromMe(target, store: store) ? "you" : target.senderDisplay)")
                    .lineLimit(1)
            }
            .font(.caption2.weight(.medium))
            .foregroundStyle(.secondary)
        }
        .buttonStyle(.plain)
    }

    private var header: some View {
        HStack(alignment: .top, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(isFromMe ? "You" : message.senderDisplay)
                        .font(.subheadline.weight(isUnread ? .bold : .semibold))
                        .lineLimit(1)
                    if isFromMe {
                        Image(systemName: "arrowshape.turn.up.left.fill")
                            .font(.system(size: 10, weight: .semibold))
                            .foregroundStyle(WTheme.accent)
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
                                .font(.system(size: 8, weight: .bold))
                                .rotationEffect(.degrees(showDetails ? 180 : 0))
                        }
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    }
                    .buttonStyle(.plain)
                } else {
                    Text(message.recipientBare.isEmpty ? message.senderBare : "to \(message.recipientBare)")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 6)
            VStack(alignment: .trailing, spacing: 5) {
                Text(UniboxFormat.listTime(message.internalDate))
                    .font(.footnote)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                if isUnread {
                    Circle().fill(WTheme.negative).frame(width: 8, height: 8)
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
                    HStack(spacing: 5) {
                        Image(systemName: "arrowshape.turn.up.left")
                            .font(.system(size: 11, weight: .semibold))
                        Text("Reply")
                            .font(.footnote.weight(.semibold))
                    }
                    .foregroundStyle(WTheme.accent)
                    .padding(.horizontal, 13)
                    .padding(.vertical, 7)
                    .background(Tone.sky.background, in: Capsule())
                }
                .buttonStyle(TapScaleStyle())
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
            .font(.system(size: 13, weight: .semibold))
            .foregroundStyle(.secondary)
            .frame(width: 32, height: 32)
            .background(Tone.slate.background, in: Circle())
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
        .background(Tone.slate.background, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .transition(.opacity.combined(with: .move(edge: .top)))
    }

    @ViewBuilder
    private func detailRow(_ label: String, _ value: String?) -> some View {
        if let value, !value.isEmpty {
            HStack(alignment: .top, spacing: 8) {
                Text(label)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .frame(width: 48, alignment: .leading)
                Text(value)
                    .font(.caption)
                    .foregroundStyle(.secondary)
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
                            .font(.body.weight(.semibold))
                        Text(email)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
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
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(tint)
        }
        .padding(.horizontal, 9)
        .padding(.vertical, 5)
        .background(tint.opacity(0.12), in: Capsule())
    }
}
