import SwiftUI

/// Mailboxes browser, presented as a full-screen cover from Home or More
/// (like the CRM browsers): a slide-in drawer (sky hero + warmup status
/// scopes with live counts + sliding tone capsule + edge swipe), a search
/// pill, bare fleet stat columns that drive the same scope, multi-select with
/// bulk warmup and remove, cursor pagination, and realtime pulse reloads.
/// Owns its NavigationStack; dismissal goes through `onClose` (the
/// environment DismissAction is unreliable in this app's cover contexts).
struct MailboxesRootView: View {
    var onClose: () -> Void = {}

    @Environment(AppEnvironment.self) private var env
    @State private var store = MailboxesStore()
    @State private var searchText = ""
    @State private var scope: MailboxScope = .all
    @State private var sidebarOpen = false
    @State private var sidebarDrag: CGFloat = 0
    @State private var showConnect = false
    @State private var pendingRemove: EmailAccount?
    @State private var confirmBulkRemove = false
    @FocusState private var searchFocused: Bool

    private static let sidebarWidth: CGFloat = 300

    private var canManage: Bool { env.session.can(.manageEmails) }
    private var canViewAnalytics: Bool { env.session.can(.viewAnalytics) }
    private var isSearching: Bool { !searchText.trimmingCharacters(in: .whitespaces).isEmpty }
    private var connectedCount: Int { store.allCount }

    /// The spine bumps emailAccounts and analytics together for account
    /// events; one summed key avoids double reloads.
    private var reloadPulse: Int {
        env.realtime.pulse(for: .emailAccounts) &+ env.realtime.pulse(for: .analytics)
    }

    /// The loaded rows narrowed to the active scope.
    private var visibleAccounts: [EmailAccount] {
        switch scope {
        case .all:
            return store.accounts
        case .warming:
            return store.accounts.filter(\.isWarmingActive)
        case .paused:
            return store.accounts.filter(\.isWarmupPaused)
        case .issues:
            return store.accounts.filter { store.statuses[$0.id]?.health?.hasIssue == true }
        case .off:
            return store.accounts.filter { $0.warmup == nil }
        }
    }

    var body: some View {
        NavigationStack {
            browser
        }
    }

    private var browser: some View {
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
        // Own chrome, like the other drawer browsers: the drawer hero runs to
        // the top and the search pill row carries the close button.
        .toolbarVisibility(.hidden, for: .navigationBar)
        .navigationDestination(for: EmailAccount.self) { account in
            MailboxDetailView(account: account)
        }
        .task(id: searchText) { await runSearch() }
        .onChange(of: reloadPulse) {
            Task { await store.load(env.api, includeStatuses: canViewAnalytics) }
        }
        .onChange(of: scope) { store.exitSelection() }
        .sensoryFeedback(.selection, trigger: scope)
        .sensoryFeedback(.impact(weight: .light), trigger: sidebarOpen)
        .sensoryFeedback(.impact(weight: .medium), trigger: store.isSelecting)
        .fullScreenCover(isPresented: $showConnect) {
            MailboxConnectFlow(onClose: { showConnect = false }, onConnected: { account in
                store.insert(account)
            })
        }
        .confirmationDialog(
            "Remove \(pendingRemove?.email ?? "this mailbox")?",
            isPresented: Binding(
                get: { pendingRemove != nil },
                set: { if !$0 { pendingRemove = nil } }
            ),
            titleVisibility: .visible,
            presenting: pendingRemove
        ) { account in
            Button("Remove account", role: .destructive) {
                Task { await store.deleteAccount(env.api, id: account.id) }
            }
        } message: { account in
            Text("Sending stops immediately and \(account.email) leaves all warmup pools.")
        }
        .confirmationDialog(
            "Remove \(store.selectedCount) mailbox\(store.selectedCount == 1 ? "" : "es")?",
            isPresented: $confirmBulkRemove,
            titleVisibility: .visible
        ) {
            Button("Remove \(store.selectedCount)", role: .destructive) {
                Task { await store.bulkRemove(env.api) }
            }
        } message: {
            Text("This disconnects them from Warmbly.")
        }
        .alert(
            "Something went wrong",
            isPresented: Binding(
                get: { store.actionError != nil },
                set: { if !$0 { store.actionError = nil } }
            )
        ) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(store.actionError ?? "")
        }
    }

    // MARK: Main pane

    private var mainPane: some View {
        VStack(spacing: 0) {
            if store.isSelecting { selectionHeader } else { searchBar }
            if store.hasLoaded, !isSearching, !store.accounts.isEmpty {
                statsHeader
            }
            scopeCaption
            listArea
        }
        .background(Color(.systemBackground))
        .overlay(alignment: .bottom) {
            if store.isSelecting {
                MailboxSelectionBar(
                    count: store.selectedCount,
                    onStart: { Task { await store.bulkWarmup(env.api, action: "start") } },
                    onPause: { Task { await store.bulkWarmup(env.api, action: "pause") } },
                    onRemove: { confirmBulkRemove = true },
                    onClear: { store.exitSelection() }
                )
                .padding(.bottom, 10)
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .simultaneousGesture(
            DragGesture(minimumDistance: 25)
                .onEnded { value in
                    // Gmail's edge swipe: open the drawer from the left edge.
                    if !sidebarOpen, !store.isSelecting, value.startLocation.x < 44, value.translation.width > 70 {
                        openSidebar()
                    }
                }
        )
    }

    // MARK: Search pill

    private var searchBar: some View {
        HStack(spacing: 10) {
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
                .accessibilityLabel("Open mailboxes menu")

                TextField("Search by email", text: $searchText)
                    .font(.subheadline)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.search)
                    .focused($searchFocused)

                if !searchText.isEmpty {
                    Button {
                        searchText = ""
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

            if canManage {
                Button {
                    showConnect = true
                } label: {
                    Image(systemName: "plus")
                        .font(.system(size: 17, weight: .medium))
                        .foregroundStyle(.primary)
                        .frame(width: 44, height: 44)
                        .background(Color(.secondarySystemBackground), in: Circle())
                }
                .buttonStyle(TapScaleStyle())
                .accessibilityLabel("Connect a mailbox")
            }

            Button {
                onClose()
            } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(.primary)
                    .frame(width: 44, height: 44)
                    .background(Color(.secondarySystemBackground), in: Circle())
            }
            .buttonStyle(TapScaleStyle())
            .accessibilityLabel("Close mailboxes")
        }
        .padding(.horizontal, 12)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }

    // MARK: Selection header

    private var selectionHeader: some View {
        let visibleIDs = visibleAccounts.map(\.id)
        return HStack(spacing: 12) {
            Button("Done") { store.exitSelection() }
                .fontWeight(.semibold)
            Spacer()
            Text("\(store.selectedCount) selected")
                .font(.subheadline.weight(.semibold))
                .monospacedDigit()
                .contentTransition(.numericText())
            Spacer()
            Button(store.allSelected(of: visibleIDs) ? "Clear" : "Select all") {
                store.selectAll(visibleIDs)
            }
        }
        .padding(.horizontal, 16)
        .frame(height: 44)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }

    // MARK: Fleet stats

    /// Bare stat columns over a hairline; each drives the same scope state as
    /// the drawer.
    private var statsHeader: some View {
        VStack(spacing: 10) {
            HStack(spacing: 10) {
                fleetStat("Connected", count: connectedCount, tone: nil, target: .all)
                fleetStat("Warming", count: store.warmingCount, tone: store.warmingCount > 0 ? .orange : nil, target: .warming)
                fleetStat(
                    "Issues",
                    count: store.issueCount,
                    tone: store.issueCount > 0 ? .rose : nil,
                    target: .issues,
                    enabled: canViewAnalytics
                )
            }
            Divider()
        }
        .padding(.horizontal, 20)
        .padding(.top, 6)
    }

    private func fleetStat(_ label: String, count: Int, tone: Tone?, target: MailboxScope, enabled: Bool = true) -> some View {
        let selected = scope == target && target != .all
        return Button {
            withAnimation(.snappy) { scope = selected ? .all : target }
        } label: {
            VStack(alignment: .leading, spacing: 3) {
                EyebrowLabel(label)
                Text("\(count)")
                    .font(.system(size: 22, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(tone?.color ?? Color.primary)
                    .contentTransition(.numericText())
                Capsule()
                    .fill(selected ? (tone?.color ?? WTheme.accent) : Color.clear)
                    .frame(width: 26, height: 3)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!enabled)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(label), \(count)")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    // MARK: Scope caption

    private var scopeCaption: some View {
        HStack(spacing: 6) {
            Text(captionTitle)
                .font(.caption.weight(.semibold))
                .tracking(0.9)
                .foregroundStyle(.secondary)
            if !isSearching, store.count(for: scope) > 0 {
                Text(WFormat.compact(store.count(for: scope)))
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
                    .contentTransition(.numericText())
            }
            Spacer()
            if isSearching, store.hasLoaded {
                Text("\(store.totalCount ?? store.accounts.count) found")
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                    .contentTransition(.numericText())
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, 8)
        .padding(.bottom, 2)
    }

    private var captionTitle: String {
        if isSearching { return "SEARCH RESULTS" }
        return scope.title.uppercased()
    }

    // MARK: List

    @ViewBuilder
    private var listArea: some View {
        if !store.hasLoaded {
            ScrollView { SkeletonRows(rows: 10) }
        } else if let error = store.loadError, store.accounts.isEmpty {
            ErrorStateView(title: "Couldn't load mailboxes", message: error) {
                await store.load(env.api, includeStatuses: canViewAnalytics)
            }
        } else if visibleAccounts.isEmpty {
            emptyState
        } else {
            List {
                ForEach(visibleAccounts) { account in
                    row(account)
                }
                if store.nextCursor != nil {
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
                    .task { await store.loadMore(env.api) }
                } else {
                    endMarker
                }
            }
            .listStyle(.plain)
            .scrollDismissesKeyboard(.immediately)
            .refreshable { await store.load(env.api, includeStatuses: canViewAnalytics) }
        }
    }

    @ViewBuilder
    private func row(_ account: EmailAccount) -> some View {
        Group {
            if store.isSelecting {
                Button {
                    store.toggleSelected(account.id)
                } label: {
                    MailboxRowView(
                        account: account,
                        status: store.statuses[account.id],
                        selecting: true,
                        selected: store.isSelected(account.id)
                    )
                }
                .buttonStyle(.plain)
            } else {
                MailboxRowView(account: account, status: store.statuses[account.id])
                    .background(NavigationLink(value: account) { EmptyView() }.opacity(0))
            }
        }
        .listRowBackground(store.isSelected(account.id) ? Tone.sky.background : Color(.systemBackground))
        .listRowInsets(EdgeInsets(top: 4, leading: 14, bottom: 4, trailing: 16))
        .simultaneousGesture(
            LongPressGesture(minimumDuration: 0.4).onEnded { _ in
                if !store.isSelecting, canManage { store.enterSelection(with: account.id) }
            }
        )
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if !store.isSelecting {
                swipeButtons(for: account)
            }
        }
    }

    /// End-of-list marker: the exact total for the current scope or search.
    private var endMarker: some View {
        let count = scope == .all ? connectedCount : visibleAccounts.count
        return HStack {
            Spacer()
            Text("\(count) mailbox\(count == 1 ? "" : "es")")
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
        if isSearching {
            EmptyStateView(
                title: "No matches",
                message: "No mailbox matches \"\(searchText)\"."
            )
        } else {
            switch scope {
            case .warming:
                EmptyStateView(
                    title: "Nothing warming",
                    message: "Start warmup on a mailbox and it shows up here."
                )
            case .paused:
                EmptyStateView(
                    title: "Nothing paused",
                    message: "Mailboxes with warmup on hold show up here."
                )
            case .issues:
                EmptyStateView(
                    title: "No issues",
                    message: "Every mailbox with health data looks fine right now."
                )
            case .off:
                EmptyStateView(
                    title: "Everything is warming",
                    message: "Mailboxes that never started warmup show up here."
                )
            case .all:
                connectEmptyState
            }
        }
    }

    private var connectEmptyState: some View {
        VStack(spacing: 10) {
            Image(systemName: "envelope.open")
                .font(.system(size: 18))
                .foregroundStyle(WTheme.accent)
                .frame(width: 36, height: 36)
                .background(Tone.sky.background, in: RoundedRectangle(cornerRadius: 8))
            Text("No mailboxes yet")
                .font(.system(size: 14, weight: .medium))
            Text("Connect your first mailbox to start warming up and sending campaigns.")
                .font(.system(size: 12))
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 260)
            if canManage {
                Button("Add mailbox") { showConnect = true }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .padding(.top, 6)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(.vertical, 48)
    }

    @ViewBuilder
    private func swipeButtons(for account: EmailAccount) -> some View {
        if canManage {
            Button {
                pendingRemove = account
            } label: {
                Label("Remove", systemImage: "trash")
            }
            .tint(WTheme.negative)

            switch account.warmupState {
            case .active:
                Button {
                    Task { await store.warmupAction(env.api, id: account.id, action: "pause") }
                } label: {
                    Label("Pause", systemImage: "pause.fill")
                }
                .tint(WTheme.warning)
            case .paused:
                Button {
                    Task { await store.warmupAction(env.api, id: account.id, action: "resume") }
                } label: {
                    Label("Resume", systemImage: "flame.fill")
                }
                .tint(Tone.orange.color)
            case .off:
                Button {
                    Task { await store.warmupAction(env.api, id: account.id, action: "start") }
                } label: {
                    Label("Warm up", systemImage: "flame.fill")
                }
                .tint(Tone.orange.color)
            }
        }
    }

    // MARK: Drawer

    private func drawer(topInset: CGFloat) -> some View {
        MailboxesSidebar(
            store: store,
            selection: scope,
            topInset: topInset,
            revealed: sidebarOpen
        ) { newScope in
            withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { scope = newScope }
            // Let the highlight capsule slide to the tapped row before closing.
            Task {
                try? await Task.sleep(for: .milliseconds(280))
                closeSidebar()
            }
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

    // MARK: Search + initial load

    private func runSearch() async {
        if store.hasLoaded {
            try? await Task.sleep(for: .milliseconds(350))
            if Task.isCancelled { return }
        }
        // Searching runs server-side across the whole fleet; drop any filter.
        if isSearching, scope != .all {
            withAnimation(.snappy) { scope = .all }
        }
        store.query = searchText
        await store.load(env.api, includeStatuses: canViewAnalytics)
    }
}

// MARK: - Row

struct MailboxRowView: View {
    let account: EmailAccount
    let status: AccountAnalytics?
    var selecting: Bool = false
    var selected: Bool = false

    private var health: MailboxHealth? { status?.health }

    var body: some View {
        HStack(spacing: 12) {
            if selecting {
                Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                    .font(.system(size: 22))
                    .foregroundStyle(selected ? WTheme.accent : Color(.tertiaryLabel))
                    .transition(.scale.combined(with: .opacity))
            }
            WAvatar(name: account.email, seed: account.id, size: 42)
                .overlay(alignment: .bottomTrailing) {
                    if account.isWarmingActive {
                        Image(systemName: "flame.fill")
                            .font(.system(size: 9, weight: .bold))
                            .foregroundStyle(.white)
                            .frame(width: 16, height: 16)
                            .background(Tone.orange.color, in: Circle())
                            .overlay(Circle().strokeBorder(Color(.systemBackground), lineWidth: 1.5))
                            .offset(x: 3, y: 3)
                    }
                }
            VStack(alignment: .leading, spacing: 3) {
                Text(account.email)
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                HStack(spacing: 5) {
                    Text(account.providerLabel)
                        .foregroundStyle(.secondary)
                    warmupCaption
                }
                .font(.footnote)
            }
            Spacer(minLength: 8)
            if let health {
                HealthRing(score: health.score, tone: health.tone)
                    .modifier(PingEffect(active: health.hasIssue, color: health.tone.color))
            }
        }
        .padding(.vertical, 6)
        .contentShape(Rectangle())
    }

    @ViewBuilder
    private var warmupCaption: some View {
        switch account.warmupState {
        case .active:
            Text("· \(volumeText)")
                .monospacedDigit()
                .foregroundStyle(Tone.orange.color)
        case .paused:
            Text("· warmup paused")
                .foregroundStyle(WTheme.warning)
        case .off:
            if let sent = status?.dailyUsage?.campaignSent {
                Text("· \(sent) sent today")
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
            }
        }
    }

    private var volumeText: String {
        if let ws = status?.warmupStatus {
            return "\(ws.currentVolume ?? 0)/\(ws.targetVolume ?? 0) warm"
        }
        return "warming"
    }
}

// MARK: - Selection bar

/// Floating bottom-center bar shown while mailboxes are multi-selected:
/// count plus bulk warmup start/pause and remove.
struct MailboxSelectionBar: View {
    let count: Int
    let onStart: () -> Void
    let onPause: () -> Void
    let onRemove: () -> Void
    let onClear: () -> Void

    var body: some View {
        HStack(spacing: 8) {
            Text("\(count)")
                .font(.subheadline.weight(.bold))
                .monospacedDigit()
                .foregroundStyle(.white)
                .frame(minWidth: 26, minHeight: 26)
                .background(WTheme.accent, in: Circle())
                .contentTransition(.numericText())

            Spacer(minLength: 6)

            Button(action: onStart) {
                Label("Start", systemImage: "flame.fill")
                    .font(.subheadline.weight(.semibold))
            }
            .buttonStyle(.borderedProminent)
            .tint(Tone.orange.color)
            .controlSize(.small)
            .disabled(count == 0)
            .accessibilityLabel("Start warmup for \(count) mailboxes")

            Button(action: onPause) {
                Label("Pause", systemImage: "pause.fill")
                    .font(.subheadline.weight(.semibold))
            }
            .buttonStyle(.bordered)
            .tint(WTheme.warning)
            .controlSize(.small)
            .disabled(count == 0)
            .accessibilityLabel("Pause warmup for \(count) mailboxes")

            Button(role: .destructive, action: onRemove) {
                Image(systemName: "trash")
                    .font(.system(size: 15, weight: .semibold))
                    .frame(width: 30, height: 30)
            }
            .buttonStyle(.bordered)
            .tint(WTheme.negative)
            .controlSize(.small)
            .disabled(count == 0)
            .accessibilityLabel("Remove \(count) mailboxes")

            Button(action: onClear) {
                Image(systemName: "xmark")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(.secondary)
                    .frame(width: 30, height: 30)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Clear selection")
        }
        .padding(.leading, 12)
        .padding(.trailing, 8)
        .padding(.vertical, 8)
        .background(.regularMaterial, in: Capsule())
        .overlay(Capsule().strokeBorder(Color(.separator).opacity(0.35), lineWidth: 1))
        .shadow(color: .black.opacity(0.14), radius: 14, y: 4)
        .padding(.horizontal, 20)
    }
}
