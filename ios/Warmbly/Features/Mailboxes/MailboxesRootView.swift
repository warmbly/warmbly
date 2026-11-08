import SwiftUI

/// Mailboxes browser, presented as a full-screen cover from Home or More
/// (like the CRM browsers): a slide-in drawer (ink header + warmup status
/// scopes with live counts + sliding fill + edge swipe), a search pill, a
/// ruled fleet stat strip that drives the same scope, multi-select with
/// bulk warmup and remove, cursor pagination, and realtime pulse reloads.
/// Owns its NavigationStack; dismissal goes through `onClose` (the
/// environment DismissAction is unreliable in this app's cover contexts).
struct MailboxesRootView: View {
    /// Which bulk tag sheet is open: add applies add_tags, remove remove_tags.
    private enum BulkTagMode: String, Identifiable {
        case add, remove
        var id: String { rawValue }
    }

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
    @State private var bulkTagMode: BulkTagMode?
    @State private var tagApplyPulse = 0
    @FocusState private var searchFocused: Bool
    @State private var tilesShown = false

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
                    Color.black.opacity(0.24)
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
        .sensoryFeedback(.impact(weight: .light), trigger: tagApplyPulse)
        #if DEBUG
        // Scripted runs open the connect flow (WARMBLY_DEBUG_CONNECT) to see it.
        .onAppear {
            if ProcessInfo.processInfo.environment["WARMBLY_DEBUG_CONNECT"] != nil { showConnect = true }
        }
        #endif
        .fullScreenCover(isPresented: $showConnect) {
            MailboxConnectFlow(onClose: { showConnect = false }, onConnected: { account in
                store.insert(account)
            }, onImported: {
                Task { await store.load(env.api, includeStatuses: canViewAnalytics) }
            })
        }
        .sheet(item: $bulkTagMode) { mode in
            MailboxTagPickerSheet(
                title: mode == .add ? "Add tags" : "Remove tags",
                confirmLabel: mode == .add ? "Add" : "Remove",
                requiresSelection: true
            ) { tagIDs in
                await applyBulkTags(mode, tagIDs: tagIDs)
            }
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
            listArea
        }
        .background(WTheme.groupedPage)
        .overlay(alignment: .bottom) {
            if store.isSelecting {
                MailboxSelectionBar(
                    count: store.selectedCount,
                    onStart: { Task { await store.bulkWarmup(env.api, action: "start") } },
                    onPause: { Task { await store.bulkWarmup(env.api, action: "pause") } },
                    onAddTags: { bulkTagMode = .add },
                    onRemoveTags: { bulkTagMode = .remove },
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

    // MARK: Header

    private var searchBar: some View {
        GlassHeaderBar(
            title: "Mailboxes",
            subtitle: store.hasLoaded ? Text("\(WFormat.compact(connectedCount)) connected") : nil,
            query: $searchText,
            searchPrompt: "Search by email",
            searchFocused: $searchFocused,
            menuLabel: "Open mailboxes menu",
            onMenu: openSidebar
        ) {
            if canManage {
                HeaderPrimaryButton(symbol: "plus", label: "Connect a mailbox") { showConnect = true }
            }
            GlassCircleButton(symbol: "xmark", label: "Close mailboxes") { onClose() }
        }
    }

    // MARK: Selection header

    private var selectionHeader: some View {
        let visibleIDs = visibleAccounts.map(\.id)
        return HStack(spacing: 12) {
            Button("Done") { store.exitSelection() }
                .buttonStyle(InkButtonStyle(size: .small))
            Spacer()
            Text("\(store.selectedCount) selected")
                .font(WFont.meta(12, weight: .medium))
                .monospacedDigit()
                .foregroundStyle(WTheme.ink)
                .contentTransition(.numericText())
            Spacer()
            Button(store.allSelected(of: visibleIDs) ? "Clear" : "Select all") {
                store.selectAll(visibleIDs)
            }
            .buttonStyle(OutlineButtonStyle(size: .small))
        }
        .padding(.horizontal, 16)
        .frame(height: 44)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }

    // MARK: Fleet stats

    /// Three tiles that filter the list, the same scope the drawer sets.
    private var statsHeader: some View {
        HStack(spacing: 10) {
            fleetTile("Connected", symbol: "envelope.fill", count: connectedCount, color: WTheme.accent, target: .all, index: 0)
            fleetTile("Warming", symbol: "flame.fill", count: store.warmingCount, color: Tone.orange.color, target: .warming, index: 1)
            fleetTile(
                "Issues",
                symbol: "exclamationmark.triangle.fill",
                count: store.issueCount,
                color: store.issueCount > 0 ? WTheme.negative : WTheme.positive,
                target: .issues,
                index: 2,
                enabled: canViewAnalytics
            )
        }
    }

    private func fleetTile(_ label: String, symbol: String, count: Int, color: Color, target: MailboxScope, index: Int, enabled: Bool = true) -> some View {
        let selected = scope == target
        return Button {
            withAnimation(.spring(response: 0.4, dampingFraction: 0.82)) { scope = selected ? .all : target }
        } label: {
            VStack(alignment: .leading, spacing: 0) {
                SettingsIcon(symbol: symbol, color: color, size: 30)
                    .symbolEffect(.bounce, value: selected)
                Spacer(minLength: 12)
                CountUpText(value: tilesShown ? Double(count) : 0, format: { "\(Int($0.rounded()))" })
                    .font(.system(size: 26, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.ink)
                    .animation(.easeOut(duration: 0.9).delay(Double(index) * 0.08), value: tilesShown)
                    .animation(.snappy, value: count)
                Text(label)
                    .font(.system(size: 13.5, weight: .medium))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
            }
            .padding(14)
            .frame(maxWidth: .infinity, minHeight: 108, alignment: .topLeading)
            .background(WTheme.groupedCard, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 20, style: .continuous)
                    .strokeBorder(color, lineWidth: selected && target != .all ? 2 : 0)
            }
            .contentShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
        }
        .buttonStyle(TapScaleStyle())
        .disabled(!enabled)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(label), \(count)")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    // MARK: Scope caption

    @ViewBuilder
    private var scopeCaption: some View {
        let canSelect = canManage && store.hasLoaded && !store.isSelecting && !visibleAccounts.isEmpty
        if store.hasLoaded {
            HStack(spacing: 6) {
                if isSearching {
                    let count = store.totalCount ?? store.accounts.count
                    Text("\(count) \(count == 1 ? "result" : "results")")
                        .font(.system(size: 13.5, weight: .medium))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.inkSecondary)
                        .contentTransition(.numericText())
                } else {
                    Text(scope.title)
                        .displayFont(22, weight: .semibold)
                        .foregroundStyle(WTheme.ink)
                        .contentTransition(.opacity)
                }
                Spacer()
                if canSelect {
                    Button("Select") { store.enterSelection() }
                        .font(.system(size: 14.5, weight: .medium))
                        .foregroundStyle(WTheme.accent)
                        .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 4)
            .padding(.top, 6)
        }
    }

    // MARK: List

    @ViewBuilder
    private var listArea: some View {
        if !store.hasLoaded {
            ScrollView { AnalyticsLoadingCards().padding(16) }
        } else if let error = store.loadError, store.accounts.isEmpty {
            ErrorStateView(title: "Couldn't load mailboxes", message: error) {
                await store.load(env.api, includeStatuses: canViewAnalytics)
            }
        } else if visibleAccounts.isEmpty {
            emptyState
        } else {
            List {
                Section {
                    VStack(spacing: 14) {
                        if !isSearching, !store.accounts.isEmpty {
                            statsHeader
                        }
                        scopeCaption
                    }
                    .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 0, trailing: 0))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
                }
                Section {
                    ForEach(visibleAccounts) { account in
                        row(account)
                    }
                }
                if store.nextCursor != nil {
                    HStack(spacing: 8) {
                        Spacer()
                        ProgressView().controlSize(.small)
                        Text("Loading more…")
                            .font(WFont.meta(11.5))
                            .foregroundStyle(WTheme.inkSecondary)
                        Spacer()
                    }
                    .padding(.vertical, 6)
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color.clear)
                    .task { await store.loadMore(env.api) }
                } else {
                    endMarker
                }
            }
            .listStyle(.insetGrouped)
            .readableMargins()
            .listSectionSpacing(14)
            .contentMargins(.horizontal, 16, for: .scrollContent)
            .contentMargins(.top, 0, for: .scrollContent)
            .scrollContentBackground(.hidden)
            .background(WTheme.groupedPage)
            .scrollDismissesKeyboard(.immediately)
            .defaultScrollAnchor(SettingsDebug.anchor)
            .onAppear { tilesShown = true }
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
        .listRowBackground(store.isSelected(account.id) ? WTheme.fill : WTheme.groupedCard)
        .listRowSeparatorTint(WTheme.hairline)
        .alignmentGuide(.listRowSeparatorLeading) { _ in 66 }
        .listRowInsets(EdgeInsets(top: 6, leading: 14, bottom: 6, trailing: 14))
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
                .font(.system(size: 13))
                .monospacedDigit()
                .foregroundStyle(WTheme.inkTertiary)
            Spacer()
        }
        .padding(.top, 4)
        .padding(.bottom, 90)
        .listRowSeparator(.hidden)
        .listRowBackground(Color.clear)
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
        VStack(spacing: 0) {
            Image(systemName: "envelope.open")
                .font(.system(size: 18, weight: .regular))
                .foregroundStyle(WTheme.inkSecondary)
                .frame(width: 46, height: 46)
                .background(WTheme.raised, in: RoundedRectangle(cornerRadius: 13, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 13, style: .continuous).strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
                .padding(.bottom, 16)
            TwoToneTitle(lead: "No mailboxes yet.", title: "Connect one.", size: 22)
                .multilineTextAlignment(.center)
            Text("Connect your first mailbox to start warming up and sending campaigns.")
                .font(.system(size: 14))
                .foregroundStyle(WTheme.inkSecondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 270)
                .padding(.top, 8)
            if canManage {
                Button {
                    showConnect = true
                } label: {
                    Label("Add mailbox", systemImage: "plus")
                }
                .buttonStyle(InkButtonStyle(size: .regular))
                .padding(.top, 20)
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
        .background(WTheme.canvas)
        .overlay(alignment: .trailing) {
            Rectangle().fill(WTheme.hairlineStrong).frame(width: 1)
        }
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

    // MARK: Bulk tags

    /// Bulk add or remove one tag set across the selection, then reload the
    /// rows and leave select mode with a light tap.
    private func applyBulkTags(_ mode: BulkTagMode, tagIDs: [String]) async {
        guard !tagIDs.isEmpty else { return }
        let ok = await store.bulkTags(
            env.api,
            add: mode == .add ? tagIDs : [],
            remove: mode == .remove ? tagIDs : []
        )
        guard ok else { return }
        await store.load(env.api, includeStatuses: canViewAnalytics)
        tagApplyPulse += 1
        store.exitSelection()
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
                    .font(.system(size: 21, weight: .light))
                    .foregroundStyle(selected ? WTheme.ink : WTheme.inkTertiary)
                    .transition(.scale.combined(with: .opacity))
            }
            MailboxAvatar(email: account.email, avatarURL: account.avatarURL, brand: account.brand, seed: account.id, size: 44)
            VStack(alignment: .leading, spacing: 4) {
                Text(account.email)
                    .font(.system(size: 15.5, weight: .medium))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                Text(account.providerLabel)
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
                HStack(spacing: 8) {
                    if account.warmupState == .active, let ramp {
                        WarmupBar(progress: ramp)
                            .frame(width: 64, height: 4)
                    }
                    warmupCaption
                }
                .font(.system(size: 12.5, weight: .medium))
                .lineLimit(1)
                .padding(.top, 1)
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
            Text(volumeText)
                .monospacedDigit()
                .foregroundStyle(Tone.orange.color)
        case .paused:
            Text("Warmup paused")
                .foregroundStyle(WTheme.warning)
        case .off:
            if let sent = status?.dailyUsage?.campaignSent {
                Text("\(sent) sent today")
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkTertiary)
            }
        }
    }

    private var volumeText: String {
        if let ws = status?.warmupStatus, let target = ws.targetVolume, target > 0 {
            return "Warming, \(target) a day"
        }
        return "Warming"
    }

    /// Today's warmup target toward its ceiling.
    private var ramp: Double? {
        guard let ws = status?.warmupStatus, let target = ws.targetVolume, let ceiling = ws.maxVolume, ceiling > 0 else { return nil }
        return min(1, Double(target) / Double(ceiling))
    }
}

// MARK: - Selection bar

/// Floating bottom-center ink bar shown while mailboxes are multi-selected:
/// count plus bulk warmup start/pause, tag add/remove, and remove.
struct MailboxSelectionBar: View {
    let count: Int
    let onStart: () -> Void
    let onPause: () -> Void
    let onAddTags: () -> Void
    let onRemoveTags: () -> Void
    let onRemove: () -> Void
    let onClear: () -> Void

    var body: some View {
        HStack(spacing: 6) {
            Text("\(count)")
                .font(WFont.meta(13, weight: .semibold))
                .monospacedDigit()
                .foregroundStyle(WTheme.ink)
                .frame(minWidth: 26, minHeight: 26)
                .background(WTheme.inkInverse, in: Circle())
                .contentTransition(.numericText())

            Spacer(minLength: 6)

            Button(action: onStart) {
                Label("Start", systemImage: "flame.fill")
                    .font(.system(size: 13.5, weight: .medium))
                    .foregroundStyle(WTheme.ink)
                    .padding(.horizontal, 12)
                    .frame(height: 32)
                    .background(WTheme.inkInverse, in: Capsule())
            }
            .buttonStyle(TapScaleStyle())
            .disabled(count == 0)
            .accessibilityLabel("Start warmup for \(count) mailboxes")

            Button(action: onPause) {
                Label("Pause", systemImage: "pause.fill")
                    .font(.system(size: 13.5, weight: .medium))
                    .foregroundStyle(WTheme.inkInverse)
                    .padding(.horizontal, 12)
                    .frame(height: 32)
                    .overlay(Capsule().strokeBorder(WTheme.inkInverse.opacity(0.28), lineWidth: 1))
            }
            .buttonStyle(TapScaleStyle())
            .disabled(count == 0)
            .accessibilityLabel("Pause warmup for \(count) mailboxes")

            // One compact entry for both tag actions; the bar is already full.
            Menu {
                Button(action: onAddTags) {
                    Label("Add tags", systemImage: "tag")
                }
                Button(action: onRemoveTags) {
                    Label("Remove tags", systemImage: "tag.slash")
                }
            } label: {
                Image(systemName: "tag")
                    .font(.system(size: 14, weight: .medium))
                    .foregroundStyle(WTheme.inkInverse)
                    .frame(width: 32, height: 32)
                    .contentShape(Rectangle())
            }
            .disabled(count == 0)
            .accessibilityLabel("Tag \(count) mailboxes")

            Button(role: .destructive, action: onRemove) {
                Image(systemName: "trash")
                    .font(.system(size: 14, weight: .medium))
                    .foregroundStyle(Color.dynamic(light: 0xFCA5A5, dark: 0xDC2626))
                    .frame(width: 32, height: 32)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(count == 0)
            .accessibilityLabel("Remove \(count) mailboxes")

            Button(action: onClear) {
                Image(systemName: "xmark")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(WTheme.inkInverse.opacity(0.6))
                    .frame(width: 30, height: 30)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Clear selection")
        }
        .padding(.leading, 8)
        .padding(.trailing, 6)
        .padding(.vertical, 7)
        .background(WTheme.ink, in: Capsule())
        .shadow(color: Color.black.opacity(0.12), radius: 16, y: 6)
        .padding(.horizontal, 20)
    }
}
