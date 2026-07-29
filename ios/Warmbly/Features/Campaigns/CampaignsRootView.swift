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
    @State private var sidebarDrag: CGFloat = 0
    @State private var showCreate = false
    @State private var pendingDelete: Campaign?
    @State private var deleteError: String?
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
            .navigationDestination(for: Campaign.self) { campaign in
                CampaignDetailView(campaign: campaign)
            }
            .fullScreenCover(isPresented: $showCreate) {
                CampaignCreateFlow(store: store) { campaign in
                    path.append(campaign)
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

    // MARK: Search pill

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
                .accessibilityLabel("Open campaigns menu")

                TextField("Search campaigns", text: $store.query)
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

            if canManage {
                Button {
                    showCreate = true
                } label: {
                    Image(systemName: "plus")
                        .font(.system(size: 17, weight: .medium))
                        .foregroundStyle(.primary)
                        .frame(width: 44, height: 44)
                        .background(Color(.secondarySystemBackground), in: Circle())
                }
                .buttonStyle(TapScaleStyle())
                .accessibilityLabel("New campaign")
            }
        }
        .padding(.horizontal, 12)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }

    // MARK: Scope caption

    private var scopeCaption: some View {
        HStack(spacing: 6) {
            Text(store.isSearching ? "SEARCH RESULTS" : store.scope.title.uppercased())
                .font(.caption.weight(.semibold))
                .tracking(0.9)
                .foregroundStyle(.secondary)
            if !store.isSearching, let count = store.totalCount, count > 0 {
                Text(WFormat.compact(count))
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
                    .contentTransition(.numericText())
            }
            Spacer()
            if store.isSearching {
                if store.hasLoaded, let count = store.totalCount {
                    Text("\(count) found")
                        .font(.caption.weight(.semibold))
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                        .contentTransition(.numericText())
                }
            } else if store.scope == .all, store.runningCount > 0 {
                Text("\(WFormat.compact(store.runningCount)) sending")
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.positive)
                    .contentTransition(.numericText())
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, 8)
        .padding(.bottom, 2)
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { SkeletonRows(rows: 10) }
        } else if let error = store.errorMessage, store.campaigns.isEmpty {
            ErrorStateView(title: "Couldn't load campaigns", message: error) {
                await store.load(env.api)
            }
        } else if store.campaigns.isEmpty {
            emptyState
        } else {
            List {
                ForEach(store.campaigns) { campaign in
                    row(campaign)
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
                    .onAppear {
                        Task { await store.loadMore(env.api) }
                    }
                } else {
                    endMarker
                }
            }
            .listStyle(.plain)
            .scrollDismissesKeyboard(.immediately)
            .refreshable { await store.load(env.api) }
        }
    }

    /// End-of-list marker: the exact total for this scope/search.
    private var endMarker: some View {
        let count = store.totalCount ?? store.campaigns.count
        return HStack {
            Spacer()
            Text("\(count) campaign\(count == 1 ? "" : "s")")
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
        if store.isSearching {
            EmptyStateView(title: "No matching campaigns", message: "Try a different search.")
        } else {
            switch store.scope {
            case .running:
                EmptyStateView(title: "Nothing is sending", message: "Start a campaign and it shows up here.")
            case .paused:
                EmptyStateView(title: "Nothing paused", message: "Paused campaigns show up here.")
            case .draft:
                EmptyStateView(title: "No drafts", message: "Campaigns that haven't started yet show up here.")
            case .finished:
                EmptyStateView(title: "Nothing finished yet", message: "Completed campaigns show up here.")
            case .folder:
                EmptyStateView(title: "Folder is empty", message: "File campaigns into this folder on the web dashboard.")
            case .all:
                if canManage {
                    EmptyStateView(
                        title: "No campaigns yet",
                        message: "Create a campaign here, then finish steps and senders on the web dashboard.",
                        ctaTitle: "New campaign"
                    ) {
                        showCreate = true
                    }
                } else {
                    EmptyStateView(
                        title: "No campaigns yet",
                        message: "Campaigns your team creates will show up here."
                    )
                }
            }
        }
    }

    // MARK: Row

    private func row(_ campaign: Campaign) -> some View {
        Button {
            path.append(campaign)
        } label: {
            rowLabel(campaign)
        }
        .buttonStyle(.plain)
        .listRowBackground(Color(.systemBackground))
        .listRowInsets(EdgeInsets(top: 4, leading: 14, bottom: 4, trailing: 16))
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

    private func rowLabel(_ campaign: Campaign) -> some View {
        let info = campaign.statusInfo
        let viewers = env.realtime.presence.viewers(of: "campaign:\(campaign.id)", excluding: env.session.user?.id)
        return HStack(alignment: .top, spacing: 12) {
            statusAvatar(campaign, info: info, viewers: viewers)
            VStack(alignment: .leading, spacing: 2.5) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(campaign.name)
                        .font(.body.weight(.medium))
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    if let created = campaign.createdAt {
                        Text(WFormat.relative(created))
                            .font(.footnote)
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                }
                secondaryLine(campaign)
                HStack(spacing: 6) {
                    HStack(spacing: 4) {
                        Circle()
                            .fill(info.tone.color)
                            .frame(width: 6, height: 6)
                            .modifier(PingEffect(active: info.isLive, color: info.tone.color))
                        Text(info.label)
                            .font(.caption.weight(.medium))
                            .foregroundStyle(info.tone.color)
                    }
                    Text("·")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                    Text("\(campaign.dailyLimit ?? 50)/day")
                        .font(.caption)
                        .monospacedDigit()
                        .foregroundStyle(.tertiary)
                }
                .padding(.top, 1)
            }
        }
        .padding(.vertical, 6)
        .contentShape(Rectangle())
    }

    /// Status-toned disc in the avatar slot; a presence dot appears when a
    /// teammate is on this campaign, like the unibox thread rows.
    private func statusAvatar(_ campaign: Campaign, info: CampaignStatusInfo, viewers: [PresenceMember]) -> some View {
        ZStack {
            Circle().fill(info.tone.background)
            Image(systemName: statusSymbol(campaign))
                .font(.system(size: 16, weight: .semibold))
                .foregroundStyle(info.tone.color)
        }
        .frame(width: 44, height: 44)
        .overlay(alignment: .topTrailing) {
            if !viewers.isEmpty {
                Circle()
                    .fill(viewers.contains { $0.primary?.action == "editing" } ? WTheme.presenceEditing : WTheme.presenceViewing)
                    .frame(width: 10, height: 10)
                    .overlay(Circle().strokeBorder(Color(.systemBackground), lineWidth: 1.5))
                    .offset(x: 1, y: -1)
            }
        }
    }

    /// Status-shaped symbol; the disc's tone carries the color.
    private func statusSymbol(_ campaign: Campaign) -> String {
        switch campaign.statusBucket {
        case .running: "paperplane.fill"
        case .paused: "pause.fill"
        case .finished: "checkmark"
        case .draft: "doc.text.fill"
        }
    }

    @ViewBuilder
    private func secondaryLine(_ campaign: Campaign) -> some View {
        if let stats = store.rowStats[campaign.id] {
            Text("\(WFormat.compact(stats.sent)) sent · \(WFormat.compact(stats.opened)) open\(stats.opened == 1 ? "" : "s") · \(WFormat.compact(stats.replied)) \(stats.replied == 1 ? "reply" : "replies")")
                .font(.subheadline)
                .monospacedDigit()
                .foregroundStyle(.tertiary)
                .lineLimit(1)
                .contentTransition(.numericText())
        } else if let description = campaign.description, !description.isEmpty {
            Text(description)
                .font(.subheadline)
                .foregroundStyle(.tertiary)
                .lineLimit(1)
        } else {
            Text("No sends yet")
                .font(.subheadline)
                .foregroundStyle(.tertiary)
        }
    }

    // MARK: Drawer

    private func drawer(topInset: CGFloat) -> some View {
        CampaignsSidebar(
            store: store,
            folders: drawerFolders,
            selection: store.scope,
            topInset: topInset,
            revealed: sidebarOpen
        ) { scope in
            withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.scope = scope }
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

    private func deleteCampaign(_ campaign: Campaign) async {
        do {
            try await store.delete(env.api, campaign: campaign)
            path.removeAll { $0.id == campaign.id }
        } catch {
            deleteError = error.localizedDescription
        }
    }
}
