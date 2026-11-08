import SwiftUI

/// Campaigns tab root, matching the unibox browser: a compact search-pill
/// header with a hamburger that slides in the navigation drawer (status
/// scopes + folders with counts), a dense full-width hairline list, and
/// server-side filtering so every scope has correct totals and infinite
/// scroll. Rows carry their own tools (start/pause + delete swipes, context
/// menu) and live signals (pulsing status dot, teammate presence dot).
struct CampaignsRootView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = CampaignsStore()
    @State private var path: [Campaign] = []
    @State private var sidebarOpen = false
    /// What the detail column shows when the list and detail sit side by side.
    @State private var openCampaign: Campaign?
    @State private var layout = SplitMetrics.stacked
    @State private var showCreate = false
    @State private var pendingDelete: Campaign?
    @State private var deleteError: String?
    @State private var rowsShown = false
    @FocusState private var searchFocused: Bool

    private static let sidebarWidth: CGFloat = 300

    private var canManage: Bool { env.session.can(.manageCampaigns) }
    private var canSend: Bool { env.session.can(.sendCampaigns) }

    private var pulseKey: Int {
        env.realtime.pulse(for: .campaigns) &+ env.realtime.pulse(for: .analytics)
    }

    /// The member's campaign folders (session), in position order.
    private var drawerFolders: [UserGroup] {
        (env.session.user?.folders ?? []).sorted { ($0.position ?? 0) < ($1.position ?? 0) }
    }

    var body: some View {
        ListDetailShell(
            path: $path,
            openItem: $openCampaign,
            sidebarOpen: $sidebarOpen,
            layout: $layout,
            sidebarWidth: Self.sidebarWidth,
            drawerRadius: 26,
            dimOpacity: 0.28,
            placeholder: DetailPlaceholder(
                title: "No campaign open",
                message: "Pick a campaign to see its progress, leads and schedule here.",
                symbol: "paperplane"
            )
        ) {
            mainPane
                .toolbar(.hidden, for: .navigationBar)
                .tabBarHidden(sidebarOpen)
                .navigationDestination(for: Campaign.self) { campaign in
                    CampaignDetailView(campaign: campaign)
                }
        } drawer: { topInset, revealed in
            sidebar(topInset: topInset, revealed: revealed)
        } detail: { campaign in
            CampaignDetailView(campaign: campaign)
        }
        .fullScreenCover(isPresented: $showCreate) {
            CampaignCreateFlow(store: store) { campaign in
                open(campaign)
            }
        }
        .confirmationDialog(
            "Delete campaign?",
            isPresented: Binding(
                get: { pendingDelete != nil },
                set: { if !$0 { pendingDelete = nil } }
            ),
            titleVisibility: .visible,
            presenting: pendingDelete
        ) { campaign in
            Button("Delete \(campaign.name)", role: .destructive) {
                Task { await deleteCampaign(campaign) }
            }
        } message: { _ in
            Text("Leads stay in your contacts. This can't be undone.")
        }
        .alert("Couldn't delete campaign", isPresented: Binding(
            get: { deleteError != nil },
            set: { if !$0 { deleteError = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(deleteError ?? "")
        }
        .alert("Couldn't update campaign", isPresented: Binding(
            get: { store.actionError != nil },
            set: { if !$0 { store.actionError = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(store.actionError ?? "")
        }
        .task(id: store.query) {
            // First run loads immediately; subsequent runs debounce typing.
            if store.hasLoaded {
                try? await Task.sleep(for: .milliseconds(350))
            }
            guard !Task.isCancelled else { return }
            await store.load(env.api)
        }
        .onChange(of: store.scope) {
            Task { await store.load(env.api) }
        }
        .onChange(of: pulseKey) {
            Task { await store.load(env.api) }
        }
        .sensoryFeedback(.impact(weight: .light), trigger: sidebarOpen)
        .sensoryFeedback(.selection, trigger: store.scope)
    }

    // MARK: Main pane

    private var mainPane: some View {
        VStack(spacing: 0) {
            searchBar
            scopeCaption
            list
        }
        #if DEBUG
        // Scripted debug runs can open the drawer (WARMBLY_DEBUG_DRAWER=1).
        .onAppear {
            if ProcessInfo.processInfo.environment["WARMBLY_DEBUG_DRAWER"] != nil { openSidebar() }
        }
        // WARMBLY_DEBUG_CAMPAIGN opens the campaign whose name contains it.
        .onChange(of: store.hasLoaded) { _, loaded in
            guard loaded, path.isEmpty, openCampaign == nil, let match = ProcessInfo.processInfo.environment["WARMBLY_DEBUG_CAMPAIGN"],
                  let campaign = store.campaigns.first(where: { $0.name.localizedCaseInsensitiveContains(match) }) ?? store.campaigns.first
            else { return }
            open(campaign)
        }
        #endif
        .background(WTheme.canvas)
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

    // MARK: Header

    private var searchBar: some View {
        @Bindable var store = store
        return GlassHeaderBar(
            title: store.scope.title,
            subtitle: subtitle,
            query: $store.query,
            searchPrompt: "Search campaigns",
            searchFocused: $searchFocused,
            menuLabel: "Open campaigns menu",
            onMenu: openSidebar
        ) {
            if canManage {
                HeaderPrimaryButton(symbol: "plus", label: "New campaign") { showCreate = true }
            }
        }
    }

    /// "2 campaigns · 1 sending", the live count in green.
    private var subtitle: Text? {
        guard let count = store.totalCount else { return nil }
        let total = Text("\(WFormat.compact(count)) campaign\(count == 1 ? "" : "s")")
        guard store.scope == .all, store.runningCount > 0 else { return total }
        let sending = Text("\(WFormat.compact(store.runningCount)) sending").foregroundStyle(WTheme.positive).fontWeight(.semibold)
        return Text("\(total)  ·  \(sending)")
    }

    @ViewBuilder
    private var scopeCaption: some View {
        if store.isSearching, store.hasLoaded {
            SearchResultsLine(count: store.totalCount ?? store.campaigns.count)
        }
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if !store.hasLoaded, store.errorMessage == nil {
            ScrollView { SkeletonRows(rows: 10) }
        } else if let error = store.errorMessage, store.campaigns.isEmpty {
            ErrorStateView(title: "Couldn't load campaigns", message: error) {
                await store.load(env.api)
            }
        } else if store.campaigns.isEmpty {
            emptyState
        } else {
            List {
                ForEach(Array(store.sortedCampaigns.enumerated()), id: \.element.id) { index, campaign in
                    row(campaign)
                        .reveal(rowsShown, min(index, 8))
                }
                if store.hasMore {
                    HStack(spacing: 8) {
                        Spacer()
                        ProgressView().controlSize(.small)
                        Text("Loading more")
                            .font(WFont.meta(11.5))
                            .foregroundStyle(WTheme.inkTertiary)
                        Spacer()
                    }
                    .padding(.vertical, 6)
                    .listRowSeparator(.hidden)
                    .listRowBackground(WTheme.canvas)
                    .onAppear {
                        Task { await store.loadMore(env.api) }
                    }
                } else {
                    endMarker
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .scrollDismissesKeyboard(.immediately)
            .onAppear { rowsShown = true }
            .refreshable { await store.load(env.api) }
        }
    }

    /// End-of-list marker: the exact total for this scope/search.
    private var endMarker: some View {
        let count = store.totalCount ?? store.campaigns.count
        return HStack {
            Spacer()
            Text("\(count) campaign\(count == 1 ? "" : "s")")
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
        if store.isSearching {
            EmptyStateView(title: "No matching campaigns", message: "Try a different search.", symbol: "magnifyingglass")
        } else {
            switch store.scope {
            case .running:
                EmptyStateView(title: "Nothing is sending", message: "Start a campaign and it shows up here.", symbol: "paperplane")
            case .paused:
                EmptyStateView(title: "Nothing paused", message: "Paused campaigns show up here.", symbol: "pause")
            case .draft:
                EmptyStateView(title: "No drafts", message: "Campaigns that haven't started yet show up here.", symbol: "doc.text")
            case .finished:
                EmptyStateView(title: "Nothing finished yet", message: "Completed campaigns show up here.", symbol: "checkmark")
            case .folder:
                EmptyStateView(title: "Folder is empty", message: "File campaigns into this folder on the web dashboard.", symbol: "folder")
            case .all:
                if canManage {
                    EmptyStateView(
                        title: "No campaigns yet",
                        message: "Create a campaign here, then finish steps and senders on the web dashboard.",
                        symbol: "paperplane",
                        ctaTitle: "New campaign"
                    ) {
                        showCreate = true
                    }
                } else {
                    EmptyStateView(
                        title: "No campaigns yet",
                        message: "Campaigns your team creates will show up here.",
                        symbol: "paperplane"
                    )
                }
            }
        }
    }

    // MARK: Row

    private func row(_ campaign: Campaign) -> some View {
        Button {
            open(campaign)
        } label: {
            rowLabel(campaign)
        }
        .buttonStyle(TapScaleStyle())
        .listRowBackground(isOpen(campaign) ? WTheme.accentSoft : WTheme.canvas)
        .listRowSeparatorTint(WTheme.hairline)
        .listRowInsets(EdgeInsets(top: 4, leading: 18, bottom: 4, trailing: 18))
        .contextMenu {
            if canSend, let action = quickAction(campaign) {
                Button {
                    Task { await store.toggle(env.api, campaign: campaign, start: action.isStart) }
                } label: {
                    Label(action.title, systemImage: action.icon)
                }
            }
            if canManage {
                Button(role: .destructive) {
                    pendingDelete = campaign
                } label: {
                    Label("Delete", systemImage: "trash")
                }
            }
        }
        .swipeActions(edge: .leading, allowsFullSwipe: true) {
            if canSend, let action = quickAction(campaign) {
                Button {
                    Task { await store.toggle(env.api, campaign: campaign, start: action.isStart) }
                } label: {
                    Label(action.title, systemImage: action.icon)
                }
                .tint(action.isStart ? WTheme.positive : WTheme.warning)
            }
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if canManage {
                Button(role: .destructive) {
                    pendingDelete = campaign
                } label: {
                    Label("Delete", systemImage: "trash")
                }
            }
        }
    }

    private func quickAction(_ campaign: Campaign) -> CampaignToggleAction? {
        if campaign.canPause {
            return CampaignToggleAction(title: "Pause", icon: "pause.fill", isStart: false)
        }
        if campaign.canStart {
            let resuming = campaign.statusBucket == .paused
            return CampaignToggleAction(title: resuming ? "Resume" : "Start", icon: "play.fill", isStart: true)
        }
        return nil
    }

    /// Name and status, how far through its leads it is, then results.
    private func rowLabel(_ campaign: Campaign) -> some View {
        let info = campaign.statusInfo
        let viewers = env.realtime.presence.viewers(of: "campaign:\(campaign.id)", excluding: env.session.user?.id)
        let stats = store.rowStats[campaign.id]
        let leads = store.progress[campaign.id]
        return VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .center, spacing: 8) {
                Text(campaign.name)
                    .font(.system(size: 16.5, weight: .semibold))
                    .tracking(-0.2)
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                if !viewers.isEmpty {
                    Circle()
                        .fill(viewers.contains { $0.primary?.action == "editing" } ? WTheme.presenceEditing : WTheme.presenceViewing)
                        .frame(width: 7, height: 7)
                        .accessibilityLabel("A teammate is here")
                }
                Spacer(minLength: 6)
                statusCapsule(info)
            }
            // Late-arriving data swaps a line: the old one leaves at once so
            // the two never overlap mid-fade.
            if let leads, let progress = leads.progress {
                progressBlock(progress, leads: leads, info: info)
                    .transition(Self.swap)
            } else if campaign.description?.isEmpty == false {
                Text(campaign.description ?? "")
                    .font(.system(size: 14))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
                    .transition(Self.swap)
            }
            Group {
                if let stats, stats.sent > 0 {
                    HStack(spacing: 6) {
                        Text("\(Text(WFormat.compact(stats.sent)).foregroundStyle(WTheme.ink).fontWeight(.semibold)) sent")
                        Text("·")
                        Text("\(Text(rate(stats.opened, of: stats.sent)).foregroundStyle(WTheme.special).fontWeight(.semibold)) opened")
                        Text("·")
                        Text("\(Text(rate(stats.replied, of: stats.sent)).foregroundStyle(WTheme.positive).fontWeight(.semibold)) replied")
                    }
                    .transition(Self.swap)
                } else {
                    HStack(spacing: 6) {
                        Text(leads?.total ?? 0 > 0 ? "No sends yet" : "No leads yet")
                        Text("·")
                        Text("\(campaign.dailyLimit ?? 50) a day")
                    }
                    .transition(Self.swap)
                }
            }
            .font(.system(size: 13))
            .monospacedDigit()
            .foregroundStyle(WTheme.inkSecondary)
            .lineLimit(1)
        }
        .padding(.vertical, 10)
        .contentShape(Rectangle())
    }

    private static let swap = AnyTransition.asymmetric(insertion: .opacity, removal: .identity)

    private func statusCapsule(_ info: CampaignStatusInfo) -> some View {
        HStack(spacing: 5) {
            if info.isLive {
                LiveDot(color: info.tone.color, size: 6)
            }
            Text(info.label.prefix(1).uppercased() + info.label.dropFirst())
                .font(.system(size: 12.5, weight: .semibold))
        }
        .foregroundStyle(info.tone.color)
        .padding(.horizontal, 9)
        .frame(height: 24)
        .background(info.tone.color.opacity(0.12), in: Capsule())
    }

    private func rate(_ part: Int, of total: Int) -> String {
        guard total > 0 else { return "0%" }
        return "\(Int((Double(part) / Double(total) * 100).rounded()))%"
    }

    /// "72% done" with a bar in the status tone and the lead count behind it.
    private func progressBlock(_ progress: Double, leads: CampaignLeadCounts, info: CampaignStatusInfo) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                CampaignPercentText(progress: progress)
                Spacer(minLength: 6)
                Text("\(WFormat.compact(leads.finished)) of \(WFormat.compact(leads.total ?? 0)) leads")
                    .font(.system(size: 12.5))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkTertiary)
            }
            CampaignProgressBar(progress: progress, tone: info.tone.color, live: info.isLive)
        }
        .accessibilityElement(children: .combine)
    }

    // MARK: Drawer

    private func sidebar(topInset: CGFloat, revealed: Bool) -> some View {
        CampaignsSidebar(
            store: store,
            folders: drawerFolders,
            selection: store.scope,
            topInset: topInset,
            revealed: revealed
        ) { scope in
            withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.scope = scope }
            // Let the highlight capsule slide to the tapped row before closing.
            Task {
                try? await Task.sleep(for: .milliseconds(280))
                closeSidebar()
            }
        }
    }

    /// Pushes on a narrow window, fills the detail column on a wide one.
    private func open(_ campaign: Campaign) {
        if layout.showsDetail {
            openCampaign = campaign
        } else {
            path.append(campaign)
        }
    }

    private func isOpen(_ campaign: Campaign) -> Bool {
        layout.showsDetail && openCampaign?.id == campaign.id
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

    private func deleteCampaign(_ campaign: Campaign) async {
        do {
            try await store.delete(env.api, campaign: campaign)
            path.removeAll { $0.id == campaign.id }
            if openCampaign?.id == campaign.id { openCampaign = nil }
        } catch {
            deleteError = error.localizedDescription
        }
    }
}
