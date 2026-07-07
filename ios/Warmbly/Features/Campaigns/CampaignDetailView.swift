import SwiftUI

enum CampaignDetailTab: String, CaseIterable, Identifiable {
    case overview, leads, steps, senders

    var id: String { rawValue }

    var title: String {
        switch self {
        case .overview: return "Overview"
        case .leads: return "Leads"
        case .steps: return "Steps"
        case .senders: return "Senders"
        }
    }

    var icon: String {
        switch self {
        case .overview: return "chart.bar.fill"
        case .leads: return "person.2.fill"
        case .steps: return "list.number"
        case .senders: return "envelope.fill"
        }
    }
}

struct CampaignToggleAction {
    let title: String
    let icon: String
    let isStart: Bool
}

@MainActor
@Observable
final class CampaignDetailStore {
    private(set) var campaign: Campaign
    private(set) var isToggling = false
    var actionError: String?

    init(campaign: Campaign) {
        self.campaign = campaign
    }

    func refresh(_ api: APIClient) async {
        if let fresh: Campaign = try? await api.get("campaigns/\(campaign.id)") {
            withAnimation(.snappy) { campaign = fresh }
        }
    }

    func toggle(_ api: APIClient, start: Bool) async {
        isToggling = true
        do {
            let _: CampaignActionResponse = try await api.post("campaigns/\(campaign.id)/\(start ? "start" : "stop")")
        } catch {
            // Preconditions (60s cooldown, readiness, plan gate) come back as
            // 400s with a specific message; show it verbatim.
            actionError = error.localizedDescription
        }
        // Refresh either way: failed starts can flip status server-side
        // (paused_no_accounts, completed).
        await refresh(api)
        isToggling = false
    }

    /// DELETE is creator-only; teammates get 404 even with manage permission.
    func delete(_ api: APIClient) async throws {
        do {
            let _: EmptyBody = try await api.delete("campaigns/\(campaign.id)")
        } catch let error as APIError {
            if case let .server(status, _) = error, status == 404 {
                throw CampaignActionError.creatorOnly("Only the campaign creator can delete this campaign.")
            }
            throw error
        }
    }
}

/// Campaign detail: status header with start/stop, segmented tabs for
/// Overview / Leads / Steps / Senders, presence claim on the record.
struct CampaignDetailView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    @State private var store: CampaignDetailStore
    @State private var tab: CampaignDetailTab = .overview
    @State private var overviewStore = CampaignOverviewStore()
    @State private var leadsStore = CampaignLeadsStore()
    @State private var stepsStore = CampaignStepsStore()
    @State private var sendersStore = CampaignSendersStore()
    @State private var confirmToggle = false
    @State private var confirmDelete = false
    @State private var deleteError: String?

    init(campaign: Campaign) {
        _store = State(initialValue: CampaignDetailStore(campaign: campaign))
    }

    private var campaign: Campaign { store.campaign }
    private var presenceKey: String { "campaign:\(campaign.id)" }

    var body: some View {
        AirDetailScaffold(
            tabs: CampaignDetailTab.allCases.map { AirTabItem(id: $0.rawValue, title: $0.title, icon: $0.icon) },
            selection: Binding(
                get: { tab.rawValue },
                set: { tab = CampaignDetailTab(rawValue: $0) ?? .overview }
            )
        ) {
            hero
        } content: {
            content
        }
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbarContent }
        .presenceResource(presenceKey)
        .task {
            await store.refresh(env.api)
            await overviewStore.load(env.api, campaignID: campaign.id)
        }
        .onChange(of: env.realtime.pulse(for: .campaigns)) {
            Task { await store.refresh(env.api) }
        }
        .confirmationDialog(
            toggleAction?.isStart == true ? "Start this campaign?" : "Pause this campaign?",
            isPresented: $confirmToggle,
            titleVisibility: .visible
        ) {
            if let action = toggleAction {
                Button(action.title) {
                    Task { await store.toggle(env.api, start: action.isStart) }
                }
            }
        } message: {
            Text(toggleAction?.isStart == true
                ? "Sending begins on the campaign schedule."
                : "Sending stops until you start it again.")
        }
        .confirmationDialog(
            "Delete campaign?",
            isPresented: $confirmDelete,
            titleVisibility: .visible
        ) {
            Button("Delete \(campaign.name)", role: .destructive) {
                Task { await deleteCampaign() }
            }
        } message: {
            Text("Leads stay in your contacts. This can't be undone.")
        }
        .alert("Couldn't update campaign", isPresented: Binding(
            get: { store.actionError != nil },
            set: { if !$0 { store.actionError = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(store.actionError ?? "")
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

    // MARK: Hero

    private var hero: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .top, spacing: 10) {
                VStack(alignment: .leading, spacing: 6) {
                    Text(campaign.name)
                        .font(.title2.bold())
                        .foregroundStyle(.white)
                        .lineLimit(2)
                        .minimumScaleFactor(0.75)
                    HStack(spacing: 8) {
                        heroStatusPill
                        if let schedule = scheduleLine {
                            Text(schedule)
                                .font(.footnote)
                                .monospacedDigit()
                                .foregroundStyle(.white.opacity(0.72))
                                .lineLimit(1)
                        }
                    }
                    ResourceViewers(resource: presenceKey)
                }
                Spacer(minLength: 8)
                toggleButton
            }
            heroStats
        }
        .padding(.horizontal, 20)
        .padding(.top, 2)
        .padding(.bottom, 18)
    }

    private var heroStatusPill: some View {
        let info = campaign.statusInfo
        return HStack(spacing: 5) {
            Circle()
                .fill(info.tone.color)
                .frame(width: 7, height: 7)
                .modifier(PingEffect(active: info.isLive, color: info.tone.color))
            Text(info.label)
                .font(.caption.weight(.semibold))
                .foregroundStyle(.white)
        }
        .padding(.horizontal, 9)
        .padding(.vertical, 4)
        .background(.white.opacity(0.16), in: Capsule())
    }

    private var heroStats: some View {
        let summary = overviewStore.analytics?.summary
        return HStack(spacing: 10) {
            AirStatChip(
                value: WFormat.compact(summary?.emailsSent ?? 0),
                label: "Sent",
                symbol: "paperplane.fill"
            )
            AirStatChip(
                value: WFormat.compact(summary?.uniqueOpens ?? 0),
                label: "Opens",
                symbol: "envelope.open.fill"
            )
            AirStatChip(
                value: WFormat.compact(summary?.replies ?? 0),
                label: "Replies",
                symbol: "arrowshape.turn.up.left.fill"
            )
        }
    }

    private var scheduleLine: String? {
        var parts: [String] = []
        if let start = campaign.startTime, let end = campaign.endTime, !start.isEmpty, !end.isEmpty {
            // Times arrive as "08:00:00.000000"; show HH:MM.
            parts.append("\(start.prefix(5))–\(end.prefix(5))")
        }
        if let timezone = campaign.timezone, !timezone.isEmpty {
            parts.append(timezone.split(separator: "/").last.map(String.init) ?? timezone)
        }
        parts.append("\(campaign.dailyLimit ?? 50)/day")
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    private var toggleAction: CampaignToggleAction? {
        if campaign.canPause {
            return CampaignToggleAction(title: "Pause", icon: "pause.fill", isStart: false)
        }
        if campaign.canStart {
            let resuming = campaign.statusBucket == .paused
            return CampaignToggleAction(title: resuming ? "Resume" : "Start", icon: "play.fill", isStart: true)
        }
        return nil
    }

    @ViewBuilder
    private var toggleButton: some View {
        if env.session.can(.sendCampaigns), let action = toggleAction {
            TimelineView(.periodic(from: .now, by: 1)) { context in
                let remaining = cooldownRemaining(at: context.date)
                Button {
                    confirmToggle = true
                } label: {
                    Group {
                        if store.isToggling {
                            ProgressView()
                                .controlSize(.small)
                                .tint(.white)
                        } else if remaining > 0 {
                            Label("wait \(remaining)s", systemImage: "clock")
                                .monospacedDigit()
                        } else {
                            Label(action.title, systemImage: action.icon)
                        }
                    }
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.white)
                    .padding(.horizontal, 15)
                    .padding(.vertical, 9)
                    .background(.white.opacity(0.18), in: Capsule())
                    .overlay(Capsule().strokeBorder(.white.opacity(0.4), lineWidth: 1))
                }
                .buttonStyle(TapScaleStyle())
                .disabled(store.isToggling || remaining > 0)
                .opacity(remaining > 0 && !store.isToggling ? 0.6 : 1)
            }
        }
    }

    /// Server enforces a 60s cooldown between status changes.
    private func cooldownRemaining(at date: Date) -> Int {
        guard let changed = campaign.lastStatusChangeAt else { return 0 }
        return max(0, 60 - Int(date.timeIntervalSince(changed)))
    }

    // MARK: Toolbar

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
                Button {
                    Task { await store.refresh(env.api) }
                } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
                if env.session.can(.manageCampaigns) {
                    Button(role: .destructive) {
                        confirmDelete = true
                    } label: {
                        Label("Delete campaign", systemImage: "trash")
                    }
                }
            } label: {
                Image(systemName: "ellipsis.circle")
            }
        }
    }

    // MARK: Content

    /// Horizontal page swipe between tabs, synced with the pill bar.
    private var content: some View {
        TabView(selection: $tab) {
            CampaignOverviewTab(campaign: campaign, store: overviewStore)
                .tag(CampaignDetailTab.overview)
            CampaignLeadsTab(campaignID: campaign.id, store: leadsStore)
                .tag(CampaignDetailTab.leads)
            CampaignStepsTab(campaignID: campaign.id, store: stepsStore)
                .tag(CampaignDetailTab.steps)
            CampaignSendersTab(campaign: campaign, store: sendersStore)
                .tag(CampaignDetailTab.senders)
        }
        .tabViewStyle(.page(indexDisplayMode: .never))
        .animation(.snappy, value: tab)
    }

    private func deleteCampaign() async {
        do {
            try await store.delete(env.api)
            dismiss()
        } catch {
            deleteError = error.localizedDescription
        }
    }
}
