import SwiftUI

/// Unibox tab root, Gmail-style: a compact search-pill header with a hamburger
/// that slides in the navigation drawer (scopes, mailboxes, labels), a dense
/// conversation list, avatar multi-select with bulk actions, and read/unread +
/// snooze swipes. Backed by `GET /v1/unibox` + `GET /v1/unibox/overview`.
struct UniboxRootView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = UniboxStore()
    @State private var rail = UniboxRailStore()
    @State private var path: [UniboxRoute] = []
    @State private var sidebarOpen = false
    @State private var selectedIDs: Set<String> = []
    /// Row whose labels are being edited from the context menu.
    @State private var labelTarget: UniboxMessage?
    @State private var showBulkLabels = false
    @State private var toast: String?
    @State private var toastPulse = 0
    @State private var composeItem: ComposeSheetItem?
    /// Instant send inside its undo window; replaces the sent toast.
    @State private var undoSend: ComposeUndoSend?
    @FocusState private var searchFocused: Bool
    /// What the detail column shows when the list and detail sit side by side.
    @State private var openRoute: UniboxRoute?
    @State private var layout = SplitMetrics.stacked
    /// Bumped when Reply is chosen for the thread already open in the detail column.
    @State private var replyRequest = 0

    private static let sidebarWidth: CGFloat = 320

    private var inSelectionMode: Bool { !selectedIDs.isEmpty }
    private var canAct: Bool { env.session.can(.accessUnibox) }
    /// Compose sends need the unibox plus mailbox send rights (server gate).
    private var canCompose: Bool { canAct && env.session.can(.manageEmails) }

    var body: some View {
        ListDetailShell(
            path: $path,
            openItem: $openRoute,
            sidebarOpen: $sidebarOpen,
            layout: $layout,
            sidebarWidth: Self.sidebarWidth,
            placeholder: DetailPlaceholder(
                title: "No conversation open",
                message: "Pick a conversation to read it here.",
                symbol: "tray"
            )
        ) {
            mainPane
                .toolbar(.hidden, for: .navigationBar)
                .tabBarHidden(sidebarOpen)
                .navigationDestination(for: UniboxRoute.self) { route in
                    destination(route)
                }
        } drawer: { topInset, revealed in
            sidebar(topInset: topInset, revealed: revealed)
        } detail: { route in
            destination(route)
                .environment(\.threadReplyRequest, replyRequest)
        }
        .task(id: store.query) {
            store.includeSentStats = env.session.can(.viewAnalytics)
            store.knownLabels = drawerLabels
            if store.hasLoaded {
                try? await Task.sleep(for: .milliseconds(350))
            }
            guard !Task.isCancelled else { return }
            Task { await rail.load(env.api, owner: "\(env.session.user?.id ?? ""):\(env.session.currentOrgID ?? "")") }
            await store.load(env.api, badges: env.badges)
        }
        .onChange(of: store.scope) {
            withAnimation(.snappy) { selectedIDs.removeAll() }
            store.knownLabels = drawerLabels
            Task { await store.load(env.api, badges: env.badges) }
        }
        .onChange(of: env.realtime.pulse(for: .unibox)) {
            Task { await store.load(env.api, badges: env.badges) }
        }
        #if DEBUG
        // Scripted debug runs can open the drawer (WARMBLY_DEBUG_DRAWER=1).
        .onAppear {
            if ProcessInfo.processInfo.environment["WARMBLY_DEBUG_DRAWER"] != nil { openSidebar() }
        }
        // WARMBLY_DEBUG_THREAD=1 opens the newest conversation once the list loads.
        .onChange(of: store.messages.first?.id) {
            guard path.isEmpty, openRoute == nil, ProcessInfo.processInfo.environment["WARMBLY_DEBUG_THREAD"] != nil,
                  let first = store.messages.first else { return }
            open(.thread(threadValue(first), reply: false))
        }
        #endif
        .sensoryFeedback(.impact(weight: .light), trigger: sidebarOpen)
        .sensoryFeedback(.selection, trigger: selectedIDs.count)
        .sensoryFeedback(.selection, trigger: store.scope)
        .sensoryFeedback(.success, trigger: toastPulse)
        .sheet(item: $labelTarget) { message in
            UniboxLabelPicker(mode: .standalone(
                threadKey: message.threadKey,
                initial: message.labels ?? [],
                onChanged: { labels in
                    store.updateLocalLabels(threadKey: message.threadKey, labels: labels)
                }
            ))
        }
        .sheet(isPresented: $showBulkLabels) {
            UniboxLabelPicker(mode: .bulk(count: selectedIDs.count, onApply: { ids in
                await bulkLabel(ids)
            }))
        }
        .sheet(item: $composeItem) { item in
            ComposeView(draft: item.draft) { response, payload in
                switch response.sendMode {
                case "smart": showToast("Queued for smart send")
                case "scheduled": showToast("Scheduled")
                default:
                    // Instant sends queue a short undo window server-side.
                    if let taskID = response.taskID, let scheduledAt = response.scheduledAt {
                        withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                            undoSend = ComposeUndoSend(taskID: taskID, scheduledAt: scheduledAt, payload: payload)
                        }
                    } else {
                        showToast(response.auto == true ? "Sent from \(response.accountEmail ?? "best mailbox")" : "Sent")
                    }
                }
            }
        }
    }

    // MARK: Columns

    @ViewBuilder
    private func destination(_ route: UniboxRoute) -> some View {
        switch route {
        case let .thread(thread, reply): UniboxThreadView(thread: thread, openComposerOnAppear: reply)
        case .scheduled: UniboxScheduledView()
        case .drafts:
            ComposeDraftsView { draft in
                composeItem = ComposeSheetItem(id: draft.id, draft: draft)
            }
        }
    }

    /// Pushes on a narrow window, fills the detail column on a wide one.
    private func open(_ route: UniboxRoute) {
        if layout.showsDetail {
            // Already open beside the list: ask that thread for its composer instead of reopening it.
            if case let .thread(thread, reply) = route, case let .thread(current, _) = openRoute, current.key == thread.key {
                if reply { replyRequest += 1 }
                return
            }
            openRoute = route
        } else {
            path.append(route)
        }
    }

    private func isOpen(_ message: UniboxMessage) -> Bool {
        guard layout.showsDetail, case let .thread(thread, _) = openRoute else { return false }
        return thread.key == message.threadKey
    }

    // MARK: Main pane

    private var mainPane: some View {
        @Bindable var store = store
        return VStack(spacing: 0) {
            topBar
                .animation(.snappy, value: inSelectionMode)
            if searchFocused {
                operatorHints
                    .transition(.move(edge: .top).combined(with: .opacity))
            }
            scopeCaption
            list
        }
        .animation(.snappy, value: searchFocused)
        .background(WTheme.canvas)
        .tabBarAction("Compose email", symbol: "square.and.pencil", enabled: canCompose && !inSelectionMode) {
            composeItem = ComposeSheetItem(id: UUID().uuidString, draft: nil)
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
                        await undoComposeSend(undoSend)
                    } onExpired: {
                        withAnimation(.snappy) { self.undoSend = nil }
                    }
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }
            }
            .padding(.bottom, 12)
        }
        .simultaneousGesture(
            DragGesture(minimumDistance: 25)
                .onEnded { value in
                    // Gmail's edge swipe: open the drawer from the left edge.
                    if !sidebarOpen, value.startLocation.x < 44, value.translation.width > 70 {
                        openSidebar()
                    }
                }
        )
    }

    // MARK: Top bar

    @ViewBuilder
    private var topBar: some View {
        if inSelectionMode {
            selectionBar
                .transition(.move(edge: .top).combined(with: .opacity))
        } else {
            searchBar
                .transition(.move(edge: .top).combined(with: .opacity))
        }
    }

    private var searchBar: some View {
        @Bindable var store = store
        return GlassHeaderBar(
            title: scopeTitle,
            subtitle: store.unreadCount > 0
                ? Text("\(Text(WFormat.compact(store.unreadCount)).foregroundStyle(WTheme.accent).fontWeight(.semibold)) unread")
                : Text("All caught up"),
            query: $store.query,
            searchPrompt: "Search \(scopeTitle.lowercased())",
            searchFocused: $searchFocused,
            menuLabel: "Open inbox menu"
        ) {
            openSidebar()
        }
    }

    private var scopeTitle: String { store.scope.title }

    private var selectionBar: some View {
        HStack(spacing: 4) {
            Button {
                withAnimation(.snappy) { selectedIDs.removeAll() }
            } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 16, weight: .medium))
                    .frame(width: 38, height: 38)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Exit selection")

            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text("\(selectedIDs.count)")
                    .displayFont(22)
                    .monospacedDigit()
                    .contentTransition(.numericText())
                Text("selected")
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(WTheme.inkTertiary)
            }

            Spacer()

            Button {
                Task { await bulkMarkSeen(true) }
            } label: {
                Image(systemName: "envelope.open")
                    .font(.system(size: 16, weight: .medium))
                    .frame(width: 40, height: 38)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Mark read")

            Button {
                showBulkLabels = true
            } label: {
                Image(systemName: "tag")
                    .font(.system(size: 16, weight: .medium))
                    .frame(width: 40, height: 38)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Label selected")

            Menu {
                ForEach(Array(UniboxSnoozePreset.allCases.enumerated()), id: \.offset) { _, preset in
                    Button {
                        Task { await bulkSnooze(until: preset.date) }
                    } label: {
                        Label(preset.label, systemImage: "moon.zzz")
                    }
                }
            } label: {
                Image(systemName: "moon.zzz")
                    .font(.system(size: 16, weight: .medium))
                    .frame(width: 40, height: 38)
                    .contentShape(Rectangle())
            }
            .accessibilityLabel("Snooze selected")

            Menu {
                Button {
                    Task { await bulkMarkSeen(false) }
                } label: {
                    Label("Mark as unread", systemImage: "envelope.badge")
                }
                Button {
                    withAnimation(.snappy) { selectedIDs = Set(store.messages.map(\.id)) }
                } label: {
                    Label("Select all", systemImage: "checklist")
                }
            } label: {
                Image(systemName: "ellipsis")
                    .font(.system(size: 16, weight: .medium))
                    .frame(width: 40, height: 38)
                    .contentShape(Rectangle())
            }
            .accessibilityLabel("More actions")
        }
        .foregroundStyle(WTheme.ink)
        .padding(.horizontal, 12)
        .frame(height: 54)
        .background(WTheme.canvas)
        .overlay(alignment: .bottom) { Hairline() }
    }

    /// Under the search field while searching: how many results matched.
    @ViewBuilder
    private var scopeCaption: some View {
        if store.isSearching, store.hasLoaded {
            Text("\(store.messages.count)\(store.hasMore ? "+" : "") \(store.messages.count == 1 ? "result" : "results")")
                .font(.system(size: 13.5, weight: .medium))
                .monospacedDigit()
                .foregroundStyle(WTheme.inkSecondary)
                .contentTransition(.numericText())
                .padding(.horizontal, 20)
                .padding(.top, 4)
                .padding(.bottom, 6)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    // MARK: Search operator hints

    /// Gmail-style operator shortcuts shown while the search field is focused.
    private var operatorHints: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                hintChip("from:")
                hintChip("subject:")
                hintChip("is:unread")
                hintChip("is:snoozed")
                hintChip("is:awaiting")
                hintChip("is:uncategorized")
                ForEach(drawerLabels) { label in
                    if let title = label.title {
                        hintChip("label:\(title.lowercased().contains(" ") ? "\"\(title.lowercased())\"" : title.lowercased())")
                    }
                }
                hintChip("after:2026-01-01")
                hintChip("before:2026-12-31")
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 6)
        }
    }

    private func hintChip(_ token: String) -> some View {
        Button {
            var query = store.query
            if !query.isEmpty, !query.hasSuffix(" ") { query += " " }
            query += token
            store.query = query
        } label: {
            Text(token)
                .font(.system(size: 13.5, weight: .medium))
                .foregroundStyle(WTheme.inkSecondary)
                .padding(.horizontal, 10)
                .frame(height: 28)
                .background(WTheme.raised, in: Capsule())
                .overlay(Capsule().strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
        }
        .buttonStyle(TapScaleStyle())
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { SkeletonRows(rows: 10) }
        } else if let error = store.errorMessage, store.messages.isEmpty {
            ErrorStateView(title: "Couldn't load inbox", message: error) {
                await store.load(env.api, badges: env.badges)
            }
        } else if store.messages.isEmpty {
            emptyState
        } else {
            List {
                ForEach(store.messages) { message in
                    row(message)
                }
                if store.hasMore {
                    HStack(spacing: 8) {
                        Spacer()
                        ProgressView().controlSize(.small)
                        Text("Loading more…")
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkSecondary)
                        Spacer()
                    }
                    .padding(.vertical, 6)
                    .listRowSeparator(.hidden)
                    .listRowBackground(WTheme.canvas)
                    .onAppear { Task { await store.loadMore(env.api) } }
                } else {
                    endMarker
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .background(WTheme.canvas)
            .scrollDismissesKeyboard(.immediately)
            .refreshable { await store.load(env.api, badges: env.badges) }
        }
    }

    /// End-of-list marker: the total loaded, doubling as the result count.
    private var endMarker: some View {
        let count = store.messages.count
        let noun = store.isSearching ? "result" : "conversation"
        return HStack {
            Spacer()
            Text("\(count) \(noun)\(count == 1 ? "" : "s")")
                .font(.system(size: 13))
                .monospacedDigit()
                .foregroundStyle(WTheme.inkTertiary)
            Spacer()
        }
        .padding(.top, 14)
        .padding(.bottom, 90)
        .listRowSeparator(.hidden)
        .listRowBackground(WTheme.canvas)
    }

    @ViewBuilder
    private var emptyState: some View {
        let searching = !store.query.trimmingCharacters(in: .whitespaces).isEmpty
        if searching {
            EmptyStateView(title: "No matches", message: "Try a different search.")
        } else {
            switch store.scope {
            case .unread:
                EmptyStateView(title: "All caught up", message: "No unread conversations.")
            case .awaiting:
                EmptyStateView(title: "Nothing awaiting", message: "No threads are waiting on the other side.")
            case .snoozed:
                EmptyStateView(title: "No snoozed threads", message: "Snoozed conversations show up here.")
            default:
                EmptyStateView(title: "Inbox is empty", message: "Incoming replies land here in real time.")
            }
        }
    }

    // MARK: Row

    private func row(_ message: UniboxMessage) -> some View {
        let selected = selectedIDs.contains(message.id)
        let viewers = env.realtime.presence.viewers(of: "thread:\(message.threadKey)", excluding: env.session.user?.id)
        return Button {
            if inSelectionMode {
                toggleSelection(message)
            } else {
                open(.thread(threadValue(message), reply: false))
            }
        } label: {
            rowLabel(message, selected: selected, viewers: viewers)
        }
        .buttonStyle(.plain)
        .listRowBackground(selected ? WTheme.fill : isOpen(message) ? WTheme.accentSoft : WTheme.canvas)
        .listRowSeparatorTint(WTheme.hairline)
        .listRowInsets(EdgeInsets(top: 5, leading: 16, bottom: 5, trailing: 18))
        .contextMenu {
            if canAct {
                Button {
                    open(.thread(threadValue(message), reply: true))
                } label: {
                    Label("Reply", systemImage: "arrowshape.turn.up.left")
                }
                Button {
                    Task { await store.markSeen(env.api, ids: [message.id], seen: message.isUnread) }
                } label: {
                    Label(
                        message.isUnread ? "Mark as read" : "Mark as unread",
                        systemImage: message.isUnread ? "envelope.open" : "envelope.badge"
                    )
                }
                Menu {
                    ForEach(Array(UniboxSnoozePreset.allCases.enumerated()), id: \.offset) { _, preset in
                        Button(preset.label) {
                            Task { await snooze(message, until: preset.date) }
                        }
                    }
                } label: {
                    Label("Snooze", systemImage: "moon.zzz")
                }
                Button {
                    labelTarget = message
                } label: {
                    Label("Labels…", systemImage: "tag")
                }
                Button {
                    toggleSelection(message)
                } label: {
                    Label("Select", systemImage: "checkmark.circle")
                }
            }
        }
        .swipeActions(edge: .leading, allowsFullSwipe: true) {
            if canAct {
                Button {
                    Task { await store.markSeen(env.api, ids: [message.id], seen: message.isUnread) }
                } label: {
                    Label(
                        message.isUnread ? "Read" : "Unread",
                        systemImage: message.isUnread ? "envelope.open" : "envelope.badge"
                    )
                }
                .tint(WTheme.accent)
            }
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if canAct {
                snoozeActions(message)
            }
        }
    }

    /// True when the thread's latest message went out from one of the org's
    /// own mailboxes: the row then names the counterparty and marks the
    /// snippet "You:", like Gmail's "Me".
    private func isFromMe(_ message: UniboxMessage) -> Bool {
        let sender = message.senderBare.lowercased()
        guard !sender.isEmpty else { return false }
        return store.mailboxes.contains { ($0.email ?? "").lowercased() == sender }
    }

    private func rowLabel(_ message: UniboxMessage, selected: Bool, viewers: [PresenceMember]) -> some View {
        let fromMe = isFromMe(message)
        let counterparty = message.toAddr?.first.map(UniboxAddress.display)
        let displayName = fromMe ? (counterparty ?? message.senderDisplay) : message.senderDisplay
        let avatarSeed = fromMe ? message.recipientBare : message.senderBare
        return HStack(alignment: .top, spacing: 12) {
            avatarZone(message, name: displayName, seed: avatarSeed, selected: selected, viewers: viewers)
            VStack(alignment: .leading, spacing: 2.5) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(displayName)
                        .font(.system(size: 15.5, weight: message.isUnread ? .semibold : .medium))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    Text(UniboxFormat.listTime(message.internalDate))
                        .font(.system(size: 13, weight: message.isUnread ? .semibold : .regular))
                        .monospacedDigit()
                        .foregroundStyle(message.isUnread ? WTheme.ink : WTheme.inkTertiary)
                }
                HStack(alignment: .top, spacing: 8) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(message.subject?.isEmpty == false ? (message.subject ?? "") : "(no subject)")
                            .font(.system(size: 14.5, weight: message.isUnread ? .medium : .regular))
                            .foregroundStyle(message.isUnread ? WTheme.ink : WTheme.inkSecondary)
                            .lineLimit(1)
                        if let snippet = message.snippet, !snippet.isEmpty {
                            HStack(alignment: .top, spacing: 4) {
                                if fromMe {
                                    Image(systemName: "arrowshape.turn.up.left.fill")
                                        .font(.system(size: 9, weight: .medium))
                                        .foregroundStyle(WTheme.inkTertiary)
                                        .padding(.top, 4)
                                }
                                Text(fromMe ? "You: \(snippet)" : snippet)
                                    .font(.system(size: 14))
                                    .foregroundStyle(WTheme.inkTertiary)
                                    .lineLimit(2)
                            }
                        }
                    }
                    if message.isUnread {
                        Circle()
                            .fill(WTheme.accent)
                            .frame(width: 7, height: 7)
                            .padding(.top, 6)
                    }
                }
                if let labels = message.labels, !labels.isEmpty {
                    labelRow(labels)
                }
            }
        }
        .padding(.vertical, 6)
        .contentShape(Rectangle())
    }

    /// Gmail behavior: tapping the avatar toggles selection; in selection mode
    /// the avatar flips to a checkmark disc. Multi-message threads carry a
    /// message-count bubble on the avatar so a pile reads as a conversation.
    private func avatarZone(
        _ message: UniboxMessage,
        name: String,
        seed: String,
        selected: Bool,
        viewers: [PresenceMember]
    ) -> some View {
        ZStack {
            if selected {
                Circle()
                    .fill(WTheme.ink)
                    .frame(width: 44, height: 44)
                Image(systemName: "checkmark")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(WTheme.inkInverse)
            } else {
                WAvatar(name: name, seed: seed, size: 44)
                    .overlay(alignment: .topTrailing) {
                        if !viewers.isEmpty {
                            liveDot(replying: viewers.contains { $0.primary?.action == "replying" })
                                .offset(x: 1, y: -1)
                        }
                    }
                    .overlay(alignment: .bottomTrailing) {
                        if let count = message.messageCount, count > 1 {
                            Text("\(count)")
                                .font(.system(size: 10.5, weight: .semibold, design: .rounded))
                                .monospacedDigit()
                                .foregroundStyle(WTheme.ink)
                                .padding(.horizontal, 4.5)
                                .frame(minWidth: 18, minHeight: 18)
                                .background(WTheme.raised, in: Capsule())
                                .overlay(Capsule().strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
                                .offset(x: 4, y: 3)
                        }
                    }
            }
        }
        .animation(.snappy(duration: 0.2), value: selected)
        .onTapGesture {
            if canAct { toggleSelection(message) }
        }
        .accessibilityLabel(selected ? "Deselect conversation" : "Select conversation")
    }

    private func liveDot(replying: Bool) -> some View {
        Circle()
            .fill(replying ? WTheme.warning : WTheme.positive)
            .frame(width: 10, height: 10)
            .overlay(Circle().strokeBorder(WTheme.canvas, lineWidth: 1.5))
            .modifier(PingEffect(active: true, color: replying ? WTheme.warning : WTheme.positive))
    }

    /// Quiet label chips: the category color stays a small dot; the chip
    /// itself is neutral so rows don't turn into a color salad.
    private func labelRow(_ labels: [UniboxLabel]) -> some View {
        HStack(spacing: 4) {
            ForEach(labels.prefix(3)) { label in
                HStack(spacing: 4) {
                    Circle()
                        .fill(Color(uniboxHex: label.color) ?? WTheme.accent)
                        .frame(width: 6, height: 6)
                    Text(label.title ?? "")
                        .font(.system(size: 11.5, weight: .medium))
                        .foregroundStyle(WTheme.inkSecondary)
                }
                .padding(.horizontal, 7)
                .frame(height: 20)
                .background(WTheme.raised, in: Capsule())
                .overlay(Capsule().strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
            }
            if labels.count > 3 {
                Text("+\(labels.count - 3)")
                    .font(.system(size: 12, weight: .medium))
                    .foregroundStyle(WTheme.inkTertiary)
            }
        }
        .padding(.top, 2)
    }

    @ViewBuilder
    private func snoozeActions(_ message: UniboxMessage) -> some View {
        if store.scope == .snoozed {
            Button {
                Task { await unsnooze(message) }
            } label: {
                Label("Unsnooze", systemImage: "bell")
            }
            .tint(WTheme.accent)
        } else {
            ForEach(Array(UniboxSnoozePreset.allCases.enumerated()), id: \.offset) { _, preset in
                Button {
                    Task { await snooze(message, until: preset.date) }
                } label: {
                    Label(preset.label, systemImage: "moon.zzz")
                }
                .tint(WTheme.warning)
            }
        }
    }

    // MARK: Drawer

    /// Every session category, in position order, carrying the overview's
    /// unread counts — so labels show in the drawer even before they have mail.
    private var drawerLabels: [UniboxGroupCount] {
        let counted = Dictionary(store.categories.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let session = (env.session.user?.categories ?? []).sorted { ($0.position ?? 0) < ($1.position ?? 0) }
        guard !session.isEmpty else { return store.categories }
        return session.map { category in
            UniboxGroupCount(
                id: category.id,
                title: category.name,
                color: category.color,
                unread: counted[category.id]?.unread,
                total: counted[category.id]?.total
            )
        }
    }

    private func sidebar(topInset: CGFloat, revealed: Bool) -> some View {
        UniboxSidebar(
            store: store,
            rail: rail,
            labels: drawerLabels,
            selection: store.scope,
            topInset: topInset,
            revealed: revealed,
            canCompose: canCompose,
            canMarkRead: canAct
        ) { scope in
            withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.scope = scope }
            // Let the highlight capsule slide to the tapped row before closing.
            Task {
                try? await Task.sleep(for: .milliseconds(280))
                closeSidebar()
            }
        } onScheduled: {
            closeSidebar()
            open(.scheduled)
        } onDrafts: {
            closeSidebar()
            open(.drafts)
        }
    }

    private func openSidebar() {
        guard !layout.pinsSidebar else { return }
        searchFocused = false
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) { sidebarOpen = true }
    }

    private func closeSidebar() {
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) {
            sidebarOpen = false
        }
    }

    // MARK: Selection + actions

    private func toggleSelection(_ message: UniboxMessage) {
        withAnimation(.snappy) {
            if selectedIDs.contains(message.id) {
                selectedIDs.remove(message.id)
            } else {
                selectedIDs.insert(message.id)
            }
        }
    }

    private func bulkMarkSeen(_ seen: Bool) async {
        let ids = Array(selectedIDs)
        withAnimation(.snappy) { selectedIDs.removeAll() }
        await store.markSeen(env.api, ids: ids, seen: seen)
        await store.load(env.api, badges: env.badges)
    }

    /// Applies the picked labels on top of each selected thread's existing
    /// set (union), one PUT per thread, then reports once.
    private func bulkLabel(_ categoryIDs: [String]) async {
        guard !categoryIDs.isEmpty else { return }
        let targets = store.messages.filter { selectedIDs.contains($0.id) }
        withAnimation(.snappy) { selectedIDs.removeAll() }
        var labeled = 0
        for message in targets {
            let union = Set((message.labels ?? []).map(\.id)).union(categoryIDs)
            if (try? await store.setThreadLabels(
                env.api, threadKey: message.threadKey, categoryIDs: Array(union)
            )) != nil {
                labeled += 1
            }
        }
        await store.loadOverview(env.api)
        showToast("Labeled \(labeled) conversation\(labeled == 1 ? "" : "s")")
    }

    /// Cancels the queued instant send and reopens the composer pre-filled
    /// with what was about to go out. 404 means the send already fired.
    private func undoComposeSend(_ undo: ComposeUndoSend) async {
        do {
            let _: EmptyBody = try await env.api.delete("unibox/scheduled/\(undo.taskID)")
            withAnimation(.snappy) { undoSend = nil }
            composeItem = ComposeSheetItem(
                id: UUID().uuidString,
                draft: ComposeDraft(
                    id: UUID().uuidString.lowercased(),
                    emailAccountID: undo.payload.emailAccountID,
                    to: undo.payload.to,
                    cc: undo.payload.cc,
                    bcc: undo.payload.bcc,
                    subject: undo.payload.subject,
                    body: undo.payload.body
                )
            )
        } catch APIError.server(let status, _) where status == 404 {
            withAnimation(.snappy) { undoSend = nil }
            showToast("Already sent")
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

    private func bulkSnooze(until: Date) async {
        let targets = store.messages.filter { selectedIDs.contains($0.id) }
        withAnimation(.snappy) { selectedIDs.removeAll() }
        for message in targets {
            try? await store.snooze(env.api, threadKey: message.threadKey, until: until)
            dropLocal(message.threadKey)
        }
        await store.loadOverview(env.api)
    }

    private func snooze(_ message: UniboxMessage, until: Date) async {
        try? await store.snooze(env.api, threadKey: message.threadKey, until: until)
        dropLocal(message.threadKey)
        await store.loadOverview(env.api)
    }

    private func unsnooze(_ message: UniboxMessage) async {
        try? await store.unsnooze(env.api, threadKey: message.threadKey)
        dropLocal(message.threadKey)
        await store.loadOverview(env.api)
    }

    /// Drops a thread from the list and from the detail column if it is open there.
    private func dropLocal(_ threadKey: String) {
        store.removeLocal(threadKey: threadKey)
        if case let .thread(thread, _) = openRoute, thread.key == threadKey { openRoute = nil }
    }

    private func threadValue(_ message: UniboxMessage) -> UniboxThread {
        UniboxThread(
            key: message.threadKey,
            mailboxID: message.emailID,
            mailboxEmail: message.recipientBare.isEmpty ? nil : message.recipientBare,
            subject: message.subject
        )
    }
}

// MARK: - Navigation route

enum UniboxRoute: Hashable {
    case thread(UniboxThread, reply: Bool)
    case scheduled
    case drafts
}

/// One presented compose window; the id keys the sheet so reopening a draft
/// (or tapping the FAB again) always builds a fresh composer.
struct ComposeSheetItem: Identifiable {
    let id: String
    var draft: ComposeDraft?
}

/// An instant compose send waiting out its undo window: the queued task, the
/// moment it fires, and the payload to restore into a composer on undo.
struct ComposeUndoSend {
    let taskID: String
    let scheduledAt: Date
    let payload: ComposeDraftPayload
}
