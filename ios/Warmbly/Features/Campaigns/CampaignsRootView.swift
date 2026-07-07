import SwiftUI

/// Campaigns tab root, matching the unibox language: a compact search-pill
/// header (nav bar hidden), flat underline scope tabs, and a dense full-width
/// hairline list on the plain background. Rows carry their own tools
/// (start/pause + delete swipes, context menu) and live signals (pulsing
/// status dot, presence dot when a teammate is on the record).
struct CampaignsRootView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = CampaignsStore()
    @State private var path: [Campaign] = []
    @State private var showCreate = false
    @State private var pendingDelete: Campaign?
    @State private var deleteError: String?
    @FocusState private var searchFocused: Bool
    @Namespace private var tabUnderline

    private var canManage: Bool { env.session.can(.manageCampaigns) }
    private var canSend: Bool { env.session.can(.sendCampaigns) }

    private var pulseKey: Int {
        env.realtime.pulse(for: .campaigns) &+ env.realtime.pulse(for: .analytics)
    }

    var body: some View {
        @Bindable var store = store
        NavigationStack(path: $path) {
            VStack(spacing: 0) {
                searchBar
                scopeTabs
                scopeCaption
                list
            }
            .background(Color(.systemBackground))
            .toolbar(.hidden, for: .navigationBar)
            .navigationDestination(for: Campaign.self) { campaign in
                CampaignDetailView(campaign: campaign)
            }
            .sheet(isPresented: $showCreate) {
                CampaignCreateSheet(store: store) { campaign in
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
        .onChange(of: pulseKey) {
            Task { await store.load(env.api) }
        }
        .sensoryFeedback(.selection, trigger: store.scope)
    }

    // MARK: Search pill

    private var searchBar: some View {
        @Bindable var store = store
        return HStack(spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass")
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(.secondary)
                    .padding(.leading, 14)

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
                    .padding(.trailing, 10)
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

    // MARK: Scope tabs

    private var scopeTabs: some View {
        HStack(spacing: 0) {
            scopeTab("All", count: store.displayTotal, scope: .all)
            scopeTab("Sending", count: store.runningCount, scope: .running, dot: .emerald)
            scopeTab("Paused", count: store.pausedCount, scope: .paused, dot: .amber)
            scopeTab("Finished", count: store.finishedCount, scope: .finished, dot: .slate)
        }
        .padding(.horizontal, 8)
        .overlay(alignment: .bottom) { Divider() }
    }

    private func scopeTab(_ label: String, count: Int, scope: CampaignListScope, dot: Tone? = nil) -> some View {
        let selected = store.scope == scope
        return Button {
            withAnimation(.snappy) { store.scope = scope }
        } label: {
            VStack(spacing: 0) {
                HStack(spacing: 5) {
                    if let dot {
                        Circle().fill(dot.color).frame(width: 6, height: 6)
                    }
                    Text(label)
                        .font(.subheadline.weight(selected ? .semibold : .regular))
                        .foregroundStyle(selected ? Color.primary : .secondary)
                        .lineLimit(1)
                        .minimumScaleFactor(0.85)
                    if count > 0 {
                        Text(WFormat.compact(count))
                            .font(.caption.weight(.semibold))
                            .monospacedDigit()
                            .foregroundStyle(selected ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(.tertiary))
                            .contentTransition(.numericText())
                    }
                }
                .frame(maxWidth: .infinity)
                .frame(height: 40)
                ZStack {
                    if selected {
                        Capsule()
                            .fill(WTheme.accent)
                            .frame(height: 3)
                            .matchedGeometryEffect(id: "scope", in: tabUnderline)
                    } else {
                        Color.clear.frame(height: 3)
                    }
                }
                .padding(.horizontal, 14)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private var scopeCaption: some View {
        HStack(spacing: 6) {
            Text(store.isSearching ? "SEARCH RESULTS" : scopeTitle.uppercased())
                .font(.caption.weight(.semibold))
                .tracking(0.9)
                .foregroundStyle(.secondary)
            Spacer()
            if store.isSearching, store.hasLoaded {
                Text("\(store.filtered.count) result\(store.filtered.count == 1 ? "" : "s")")
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                    .contentTransition(.numericText())
            } else if store.runningCount > 0 {
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

    private var scopeTitle: String {
        switch store.scope {
        case .all: "All campaigns"
        case .running: "Sending"
        case .paused: "Paused"
        case .finished: "Finished"
        }
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
        } else if store.filtered.isEmpty {
            emptyState
        } else {
            List {
                ForEach(store.filtered) { campaign in
                    row(campaign)
                }
                if store.hasMore, store.scope == .all {
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

    /// End-of-list marker: the visible count, doubling as the result count.
    private var endMarker: some View {
        let count = store.filtered.count
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
        let searching = !store.query.trimmingCharacters(in: .whitespaces).isEmpty
        if searching {
            EmptyStateView(title: "No matching campaigns", message: "Try a different search.")
        } else {
            switch store.scope {
            case .running:
                EmptyStateView(title: "Nothing is sending", message: "Start a campaign and it shows up here.")
            case .paused:
                EmptyStateView(title: "Nothing paused", message: "Paused campaigns show up here.")
            case .finished:
                EmptyStateView(title: "Nothing finished yet", message: "Completed campaigns show up here.")
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

    private func deleteCampaign(_ campaign: Campaign) async {
        do {
            try await store.delete(env.api, campaign: campaign)
            path.removeAll { $0.id == campaign.id }
        } catch {
            deleteError = error.localizedDescription
        }
    }
}
