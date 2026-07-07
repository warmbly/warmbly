import SwiftUI

/// Campaigns tab root: stat strip (tappable filters), dense hairline list,
/// realtime pulse reloads, search, create sheet.
struct CampaignsRootView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = CampaignsStore()
    @State private var path: [Campaign] = []
    @State private var showCreate = false
    @State private var pendingDelete: Campaign?
    @State private var deleteError: String?

    private var pulseKey: Int {
        env.realtime.pulse(for: .campaigns) &+ env.realtime.pulse(for: .analytics)
    }

    var body: some View {
        @Bindable var store = store
        NavigationStack(path: $path) {
            VStack(spacing: 0) {
                statStrip
                list
            }
            .background(Color(.systemGroupedBackground))
            .navigationTitle("Campaigns")
            .toolbar {
                ToolbarItemGroup(placement: .topBarTrailing) {
                    PresenceAvatars()
                    if env.session.can(.manageCampaigns) {
                        Button {
                            showCreate = true
                        } label: {
                            Image(systemName: "plus")
                        }
                        .accessibilityLabel("New campaign")
                    }
                }
            }
            .navigationDestination(for: Campaign.self) { campaign in
                CampaignDetailView(campaign: campaign)
            }
            .searchable(text: $store.query, prompt: "Search campaigns")
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
    }

    // MARK: Scope chips

    private var statStrip: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                scopeChip("All", count: store.displayTotal, scope: .all)
                scopeChip("Sending", count: store.runningCount, scope: .running, dot: .emerald)
                scopeChip("Paused", count: store.pausedCount, scope: .paused, dot: .amber)
                scopeChip("Finished", count: store.finishedCount, scope: .finished, dot: .slate)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 9)
        }
    }

    private func scopeChip(_ label: String, count: Int, scope: CampaignListScope, dot: Tone? = nil) -> some View {
        let selected = store.scope == scope
        return Button {
            withAnimation(.snappy) {
                store.scope = store.scope == scope ? .all : scope
            }
        } label: {
            HStack(spacing: 6) {
                if let dot {
                    Circle().fill(selected ? Color.white : dot.color).frame(width: 7, height: 7)
                }
                Text(label)
                    .font(.subheadline.weight(selected ? .semibold : .medium))
                if count > 0 {
                    Text(WFormat.compact(count))
                        .font(.footnote.weight(.semibold))
                        .monospacedDigit()
                        .foregroundStyle(selected ? Color.white.opacity(0.9) : .secondary)
                        .contentTransition(.numericText())
                }
            }
            .padding(.horizontal, 13)
            .padding(.vertical, 7.5)
            .background(
                selected ? AnyShapeStyle(WTheme.accent.gradient) : AnyShapeStyle(Tone.slate.background),
                in: Capsule()
            )
            .foregroundStyle(selected ? Color.white : Color.primary)
        }
        .buttonStyle(TapScaleStyle())
        .sensoryFeedback(.selection, trigger: selected)
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
                    HStack {
                        Spacer()
                        ProgressView().controlSize(.small)
                        Spacer()
                    }
                    .listRowSeparator(.hidden)
                    .onAppear {
                        Task { await store.loadMore(env.api) }
                    }
                }
            }
            .listStyle(.insetGrouped)
            .scrollContentBackground(.hidden)
            .refreshable { await store.load(env.api) }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        let searching = !store.query.trimmingCharacters(in: .whitespaces).isEmpty
        if searching || store.scope != .all {
            EmptyStateView(
                title: "No matching campaigns",
                message: searching ? "Try a different search." : "Nothing in this status right now."
            )
        } else if env.session.can(.manageCampaigns) {
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

    private func row(_ campaign: Campaign) -> some View {
        NavigationLink(value: campaign) {
            let info = campaign.statusInfo
            HStack(spacing: 12) {
                IconTile(symbol: statusSymbol(campaign), tone: info.tone, size: 42)
                VStack(alignment: .leading, spacing: 3) {
                    Text(campaign.name)
                        .font(.body.weight(.medium))
                        .lineLimit(1)
                    secondaryLine(campaign)
                }
                Spacer(minLength: 8)
                VStack(alignment: .trailing, spacing: 4) {
                    StatusPill(text: info.label, tone: info.tone, pulsing: info.isLive)
                    if let created = campaign.createdAt {
                        Text(WFormat.relative(created))
                            .font(.caption)
                            .monospacedDigit()
                            .foregroundStyle(.tertiary)
                    }
                }
            }
            .padding(.vertical, 6)
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if env.session.can(.manageCampaigns) {
                Button(role: .destructive) {
                    pendingDelete = campaign
                } label: {
                    Label("Delete", systemImage: "trash")
                }
            }
        }
    }

    /// Status-shaped leading symbol; the tone tile carries the color.
    private func statusSymbol(_ campaign: Campaign) -> String {
        switch campaign.statusBucket {
        case .running: "paperplane.fill"
        case .paused: "pause.fill"
        case .finished: "checkmark"
        default: "doc.text.fill"
        }
    }

    @ViewBuilder
    private func secondaryLine(_ campaign: Campaign) -> some View {
        if let stats = store.rowStats[campaign.id] {
            Text("\(WFormat.compact(stats.sent)) sent · \(WFormat.compact(stats.opened)) open · \(WFormat.compact(stats.replied)) reply")
                .font(.footnote)
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .contentTransition(.numericText())
        } else if let description = campaign.description, !description.isEmpty {
            Text(description)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        } else {
            Text("\(campaign.dailyLimit ?? 50) / day")
                .font(.footnote)
                .monospacedDigit()
                .foregroundStyle(.secondary)
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
