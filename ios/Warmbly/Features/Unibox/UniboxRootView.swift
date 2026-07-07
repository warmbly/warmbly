import SwiftUI

/// Unibox tab root, Gmail-style: a compact search-pill header with a hamburger
/// that slides in the navigation drawer (scopes, mailboxes, labels), a dense
/// conversation list, avatar multi-select with bulk actions, and read/unread +
/// snooze swipes. Backed by `GET /v1/unibox` + `GET /v1/unibox/overview`.
struct UniboxRootView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = UniboxStore()
    @State private var path: [UniboxRoute] = []
    @State private var sidebarOpen = false
    @State private var sidebarDrag: CGFloat = 0
    @State private var selectedIDs: Set<String> = []
    @FocusState private var searchFocused: Bool

    private static let sidebarWidth: CGFloat = 300

    private var inSelectionMode: Bool { !selectedIDs.isEmpty }
    private var canAct: Bool { env.session.can(.accessUnibox) }

    var body: some View {
        NavigationStack(path: $path) {
            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    mainPane
                        .scaleEffect(sidebarOpen ? 0.97 : 1, anchor: .trailing)
                    if sidebarOpen {
                        Color.black.opacity(0.32)
                            .ignoresSafeArea()
                            .transition(.opacity)
                            .onTapGesture { closeSidebar() }
                    }
                    drawer(topInset: geo.safeAreaInsets.top)
                }
            }
            .toolbar(.hidden, for: .navigationBar)
            .toolbarVisibility(sidebarOpen ? .hidden : .automatic, for: .tabBar)
            .navigationDestination(for: UniboxRoute.self) { route in
                switch route {
                case let .thread(thread, reply): UniboxThreadView(thread: thread, openComposerOnAppear: reply)
                case .scheduled: UniboxScheduledView()
                }
            }
        }
        .task(id: store.query) {
            store.includeSentStats = env.session.can(.viewAnalytics)
            store.knownLabels = drawerLabels
            if store.hasLoaded {
                try? await Task.sleep(for: .milliseconds(350))
            }
            guard !Task.isCancelled else { return }
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
        .sensoryFeedback(.impact(weight: .light), trigger: sidebarOpen)
        .sensoryFeedback(.selection, trigger: selectedIDs.count)
        .sensoryFeedback(.selection, trigger: store.scope)
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
        .background(Color(.systemBackground))
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
        return HStack(spacing: 10) {
            HStack(spacing: 6) {
                Button {
                    openSidebar()
                } label: {
                    Image(systemName: "line.3.horizontal")
                        .font(.system(size: 17, weight: .medium))
                        .foregroundStyle(.primary)
                        .frame(width: 38, height: 38)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Open inbox menu")

                TextField("Search in emails", text: $store.query)
                    .font(.subheadline)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.search)
                    .focused($searchFocused)

                if !store.query.isEmpty {
                    Button {
                        store.query = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .font(.system(size: 16))
                            .foregroundStyle(.tertiary)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Clear search")
                }
                PresenceAvatars()
                    .padding(.trailing, 8)
            }
            .frame(height: 44)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 22, style: .continuous))
        }
        .padding(.horizontal, 12)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }

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

            Text("\(selectedIDs.count)")
                .font(.title3.weight(.semibold))
                .monospacedDigit()
                .contentTransition(.numericText())

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
        .foregroundStyle(.primary)
        .padding(.horizontal, 12)
        .frame(height: 54)
        .background(Tone.sky.background)
    }

    private var scopeCaption: some View {
        HStack(spacing: 6) {
            Text(scopeCaptionTitle)
                .font(.caption.weight(.semibold))
                .tracking(0.9)
                .foregroundStyle(.secondary)
            if !store.isSearching, let count = scopeCount, count > 0 {
                Text(WFormat.compact(count))
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
                    .contentTransition(.numericText())
            }
            Spacer()
            if store.isSearching {
                if store.hasLoaded {
                    Text("\(store.messages.count)\(store.hasMore ? "+" : "") result\(store.messages.count == 1 ? "" : "s")")
                        .font(.caption.weight(.semibold))
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                        .contentTransition(.numericText())
                }
            } else if store.unreadCount > 0 {
                Text("\(WFormat.compact(store.unreadCount)) unread")
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.negative)
                    .contentTransition(.numericText())
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, 8)
        .padding(.bottom, 2)
    }

    private var scopeCaptionTitle: String {
        if store.isSearching { return "SEARCH RESULTS" }
        switch store.scope {
        case .all: return "ALL INBOXES"
        default: return store.scope.title.uppercased()
        }
    }

    /// The current view's total, from the overview where it knows the scope;
    /// otherwise the loaded count.
    private var scopeCount: Int? {
        switch store.scope {
        case .all: return store.allCount
        case .unread: return store.unreadCount
        case .today: return store.todayCount
        case .week: return store.weekCount
        case .awaiting: return store.awaitingCount
        case .snoozed: return store.snoozedCount
        case .scheduled: return store.scheduledCount
        case let .mailbox(id, _): return store.mailboxes.first { $0.id == id }?.total
        case let .category(id, _, _): return store.categories.first { $0.id == id }?.total
        case .uncategorized: return store.hasLoaded ? store.messages.count : nil
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
                .font(.footnote.weight(.medium))
                .monospaced()
                .foregroundStyle(WTheme.accent)
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(Tone.sky.background, in: Capsule())
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
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        Spacer()
                    }
                    .padding(.vertical, 6)
                    .listRowSeparator(.hidden)
                    .onAppear { Task { await store.loadMore(env.api) } }
                } else {
                    endMarker
                }
            }
            .listStyle(.plain)
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
                .font(.footnote)
                .monospacedDigit()
                .foregroundStyle(.tertiary)
            Spacer()
        }
        .padding(.vertical, 10)
        .listRowSeparator(.hidden)
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
                path.append(.thread(threadValue(message), reply: false))
            }
        } label: {
            rowLabel(message, selected: selected, viewers: viewers)
        }
        .buttonStyle(.plain)
        .listRowBackground(selected ? Tone.sky.background : Color(.systemBackground))
        .listRowInsets(EdgeInsets(top: 4, leading: 14, bottom: 4, trailing: 16))
        .contextMenu {
            if canAct {
                Button {
                    path.append(.thread(threadValue(message), reply: true))
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
    /// own mailboxes — the row then names the counterparty and marks the
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
                        .font(.body.weight(message.isUnread ? .semibold : .medium))
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    Text(UniboxFormat.listTime(message.internalDate))
                        .font(.footnote.weight(message.isUnread ? .semibold : .regular))
                        .monospacedDigit()
                        .foregroundStyle(message.isUnread ? WTheme.negative : .secondary)
                }
                HStack(alignment: .top, spacing: 8) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(message.subject?.isEmpty == false ? (message.subject ?? "") : "(no subject)")
                            .font(.subheadline.weight(message.isUnread ? .medium : .regular))
                            .foregroundStyle(message.isUnread ? Color.primary : .secondary)
                            .lineLimit(1)
                        if let snippet = message.snippet, !snippet.isEmpty {
                            HStack(alignment: .top, spacing: 4) {
                                if fromMe {
                                    Image(systemName: "arrowshape.turn.up.left.fill")
                                        .font(.system(size: 9, weight: .semibold))
                                        .foregroundStyle(.tertiary)
                                        .padding(.top, 4)
                                }
                                Text(fromMe ? "You: \(snippet)" : snippet)
                                    .font(.subheadline)
                                    .foregroundStyle(.tertiary)
                                    .lineLimit(2)
                            }
                        }
                    }
                    if message.isUnread {
                        Circle()
                            .fill(WTheme.negative)
                            .frame(width: 8, height: 8)
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
                    .fill(WTheme.accent)
                    .frame(width: 44, height: 44)
                Image(systemName: "checkmark")
                    .font(.system(size: 16, weight: .bold))
                    .foregroundStyle(.white)
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
                                .font(.system(size: 10, weight: .bold))
                                .monospacedDigit()
                                .foregroundStyle(.white)
                                .padding(.horizontal, 4.5)
                                .frame(minWidth: 17, minHeight: 17)
                                .background(Color(.systemGray), in: Capsule())
                                .overlay(Capsule().strokeBorder(Color(.systemBackground), lineWidth: 1.5))
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
            .overlay(Circle().strokeBorder(Color(.systemBackground), lineWidth: 1.5))
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
                        .font(.caption2.weight(.medium))
                        .foregroundStyle(.secondary)
                }
                .padding(.horizontal, 7)
                .padding(.vertical, 2.5)
                .background(Tone.slate.background, in: Capsule())
            }
            if labels.count > 3 {
                Text("+\(labels.count - 3)")
                    .font(.caption2.weight(.medium))
                    .foregroundStyle(.tertiary)
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

    private func drawer(topInset: CGFloat) -> some View {
        UniboxSidebar(store: store, labels: drawerLabels, selection: store.scope, topInset: topInset, revealed: sidebarOpen) { scope in
            withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.scope = scope }
            // Let the highlight capsule slide to the tapped row before closing.
            Task {
                try? await Task.sleep(for: .milliseconds(280))
                closeSidebar()
            }
        } onScheduled: {
            closeSidebar()
            path.append(.scheduled)
        }
        .frame(width: Self.sidebarWidth)
        .frame(maxHeight: .infinity)
        .background(Color(.systemBackground))
        .clipShape(UnevenRoundedRectangle(bottomTrailingRadius: 26, topTrailingRadius: 26, style: .continuous))
        .shadow(color: .black.opacity(sidebarOpen ? 0.22 : 0), radius: 30, x: 6, y: 0)
        .ignoresSafeArea()
        .offset(x: drawerOffset)
        .gesture(
            DragGesture()
                .onChanged { value in
                    sidebarDrag = min(0, value.translation.width)
                }
                .onEnded { value in
                    if value.translation.width < -80 || value.predictedEndTranslation.width < -160 {
                        closeSidebar()
                    } else {
                        withAnimation(.spring(response: 0.32, dampingFraction: 0.86)) { sidebarDrag = 0 }
                    }
                }
        )
    }

    private var drawerOffset: CGFloat {
        (sidebarOpen ? 0 : -Self.sidebarWidth - 40) + sidebarDrag
    }

    private func openSidebar() {
        searchFocused = false
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) { sidebarOpen = true }
    }

    private func closeSidebar() {
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) {
            sidebarOpen = false
            sidebarDrag = 0
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

    private func bulkSnooze(until: Date) async {
        let targets = store.messages.filter { selectedIDs.contains($0.id) }
        withAnimation(.snappy) { selectedIDs.removeAll() }
        for message in targets {
            try? await store.snooze(env.api, threadKey: message.threadKey, until: until)
            store.removeLocal(threadKey: message.threadKey)
        }
        await store.loadOverview(env.api)
    }

    private func snooze(_ message: UniboxMessage, until: Date) async {
        try? await store.snooze(env.api, threadKey: message.threadKey, until: until)
        store.removeLocal(threadKey: message.threadKey)
        await store.loadOverview(env.api)
    }

    private func unsnooze(_ message: UniboxMessage) async {
        try? await store.unsnooze(env.api, threadKey: message.threadKey)
        store.removeLocal(threadKey: message.threadKey)
        await store.loadOverview(env.api)
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
}
