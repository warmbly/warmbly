import SwiftUI

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

    /// Optimistic partial update: apply the local mutation immediately, PATCH,
    /// then adopt the server's row (or roll back and surface the error).
    func update(_ api: APIClient, body: CampaignUpdateBody, mutate: (inout Campaign) -> Void) async {
        let previous = campaign
        withAnimation(.snappy) { mutate(&campaign) }
        do {
            let fresh: Campaign = try await api.patch("campaigns/\(campaign.id)", body: body)
            withAnimation(.snappy) { campaign = fresh }
        } catch {
            withAnimation(.snappy) { campaign = previous }
            actionError = error.localizedDescription
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

/// Campaign hub: a canvas header with status + start/pause and headline
/// volume, then a flat full-bleed list: rates as bare stat columns, a live
/// chart, and hairline rows to browse (Leads / Sequence / Senders), tune
/// sending inline, and open Schedule / Settings. Sub-pages push on the
/// Campaigns tab's NavigationStack.
struct CampaignDetailView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    @Environment(\.closeDetail) private var closeDetail

    @State private var store: CampaignDetailStore
    @State private var overviewStore = CampaignOverviewStore()
    @State private var stepsStore = CampaignStepsStore()
    @State private var sendersStore = CampaignSendersStore()
    @State private var leadsStore = CampaignLeadsStore()
    @State private var showLeads = false
    @State private var confirmToggle = false
    @State private var confirmDelete = false
    @State private var deleteError: String?
    @State private var dailyLimit: Int = 50
    @State private var limitSaveTask: Task<Void, Never>?
    @State private var appeared = false

    init(campaign: Campaign) {
        _store = State(initialValue: CampaignDetailStore(campaign: campaign))
        _dailyLimit = State(initialValue: campaign.dailyLimit ?? 50)
    }

    private var campaign: Campaign { store.campaign }
    /// Numbers count up and the chart draws once the first load lands.
    private var shown: Bool { appeared && overviewStore.hasLoaded }
    private var presenceKey: String { "campaign:\(campaign.id)" }
    private var canManage: Bool { env.session.can(.manageCampaigns) }

    /// Content leading where row text starts (20 inset + 34 tile + 12 gap), so
    /// hairlines tuck under the text column, Gmail-style.
    private static let rowTextLeading: CGFloat = 66

    var body: some View {
        page
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
        .fullScreenCover(isPresented: $showLeads, onDismiss: { leadsStore.exitSelection() }) {
            CampaignLeadsPageView(campaignID: campaign.id, campaignName: campaign.name, store: leadsStore)
        }
    }

    /// The page and what keeps it current; `body` adds the dialogs.
    private var page: some View {
        VStack(spacing: 0) {
            hero
                .frame(maxWidth: .infinity, alignment: .leading)
            Hairline()
            hubList
        }
        .background(alignment: .top) {
            AirSkyWash()
                .frame(height: 320)
                .ignoresSafeArea(edges: .top)
        }
        .background(WTheme.canvas)
        .toolbarBackground(.hidden, for: .navigationBar)
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbarContent }
        .presenceResource(presenceKey)
        .task {
            appeared = true
            await store.refresh(env.api)
            dailyLimit = store.campaign.dailyLimit ?? 50
            // Counts for the browse rows load alongside the analytics.
            async let overview: Void = overviewStore.load(env.api, campaignID: campaign.id)
            async let steps: Void = stepsStore.load(env.api, campaignID: campaign.id)
            async let senders: Void = sendersStore.load(env.api, campaignID: campaign.id)
            _ = await (overview, steps, senders)
        }
        .onChange(of: env.realtime.pulse(for: .campaigns)) {
            Task {
                await store.refresh(env.api)
                dailyLimit = store.campaign.dailyLimit ?? 50
            }
        }
        .onChange(of: env.realtime.pulse(for: .analytics)) {
            Task { await overviewStore.load(env.api, campaignID: campaign.id) }
        }
        .onChange(of: dailyLimit) { _, newValue in
            guard newValue != campaign.dailyLimit else { return }
            saveDailyLimitDebounced(newValue)
        }
    }

    // MARK: Hero

    private var hero: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack(alignment: .top, spacing: 12) {
                VStack(alignment: .leading, spacing: 10) {
                    EyebrowLabel("Campaign")
                    Text(campaign.name)
                        .displayFont(28)
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(2)
                        .minimumScaleFactor(0.75)
                    HStack(spacing: 8) {
                        heroStatusPill
                        Text(campaign.scheduleSummary)
                            .font(WFont.meta(11.5))
                            .foregroundStyle(WTheme.inkTertiary)
                            .lineLimit(1)
                    }
                    ResourceViewers(resource: presenceKey)
                }
                Spacer(minLength: 8)
                toggleButton
                    .padding(.top, 18)
            }
            heroStats
        }
        .padding(.horizontal, 20)
        .padding(.top, 4)
        .padding(.bottom, 18)
    }

    private var heroStatusPill: some View {
        let info = campaign.statusInfo
        return HStack(spacing: 6) {
            LiveDot(color: info.tone.color, size: 6, pulsing: info.isLive)
            Text(info.label.prefix(1).uppercased() + info.label.dropFirst())
                .font(.system(size: 12.5, weight: .medium))
                .foregroundStyle(WTheme.ink)
        }
        .padding(.horizontal, 10)
        .frame(height: 26)
        .background(WTheme.raised, in: Capsule())
        .overlay(Capsule().strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
    }

    /// How far through its leads the campaign is, split by how each ended.
    @ViewBuilder
    private var heroStats: some View {
        if let counts = overviewStore.leadCounts, (counts.total ?? 0) > 0 {
            CampaignProgressHero(counts: counts, live: campaign.statusInfo.isLive)
                .transition(.opacity.combined(with: .move(edge: .bottom)))
        } else if overviewStore.hasLoaded {
            Text("No leads yet. Add them from Leads below and progress shows here.")
                .font(.system(size: 14))
                .foregroundStyle(WTheme.inkSecondary)
        }
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
                let label = Group {
                    if store.isToggling {
                        ProgressView()
                            .controlSize(.small)
                            .tint(action.isStart ? WTheme.inkInverse : WTheme.ink)
                    } else if remaining > 0 {
                        Label("wait \(remaining)s", systemImage: "clock")
                            .font(WFont.meta(12.5, weight: .medium))
                    } else {
                        Label(action.title, systemImage: action.icon)
                    }
                }
                .labelStyle(ToggleLabelStyle())
                // Start/resume is the primary ink pill; pause is a hairline pill.
                Group {
                    if action.isStart {
                        Button { confirmToggle = true } label: { label }
                            .buttonStyle(InkButtonStyle(size: .small))
                    } else {
                        Button { confirmToggle = true } label: { label }
                            .buttonStyle(OutlineButtonStyle(size: .small))
                            .opacity(remaining > 0 && !store.isToggling ? 0.6 : 1)
                    }
                }
                .disabled(store.isToggling || remaining > 0)
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
                    Task {
                        await store.refresh(env.api)
                        await overviewStore.load(env.api, campaignID: campaign.id)
                    }
                } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
                if canManage {
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

    // MARK: Hub list (flat, full-bleed, no cards)

    private var hubList: some View {
        List {
            performanceSection
            browseSection
            sendingSection
            setupSection
            aboutSection
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .environment(\.defaultMinListRowHeight, 0)
        .refreshable {
            await store.refresh(env.api)
            dailyLimit = store.campaign.dailyLimit ?? 50
            await overviewStore.load(env.api, campaignID: campaign.id)
        }
    }

    /// Eyebrow caption as a bare row — never a grouped `Section` header (that
    /// reintroduces the sticky gray band). Whitespace above is the separator.
    private func sectionHeader(_ title: String, top: CGFloat = 20) -> some View {
        EyebrowLabel(title)
            .padding(.horizontal, 20)
            .padding(.top, top)
            .padding(.bottom, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .listRowInsets(EdgeInsets())
            .listRowSeparator(.hidden)
            .listRowBackground(WTheme.canvas)
    }

    private func plainRow<Content: View>(
        separator: Visibility = .automatic,
        @ViewBuilder _ content: () -> Content
    ) -> some View {
        content()
            .listRowInsets(EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20))
            .listRowSeparator(separator)
            .listRowSeparatorTint(WTheme.hairline)
            .alignmentGuide(.listRowSeparatorLeading) { _ in Self.rowTextLeading }
            .listRowBackground(WTheme.canvas)
    }

    // MARK: Performance

    @ViewBuilder
    private var performanceSection: some View {
        let summary = overviewStore.analytics?.summary
        if overviewStore.hasLoaded {
            plainRow(separator: .hidden) {
                CampaignTrendSection(days: overviewStore.days, shown: shown)
                    .padding(.top, 16)
                    .padding(.bottom, 6)
                    .reveal(shown, 0)
            }
        }
        sectionHeader("Performance")
        plainRow(separator: .hidden) {
            let bounce = summary?.bounceRate ?? 0
            LazyVGrid(columns: Self.statColumns, alignment: .leading, spacing: 16) {
                miniStat("Open rate", summary?.openRate, percent: true, tone: .sky, index: 0)
                miniStat("Reply rate", summary?.replyRate, percent: true, tone: .emerald, index: 1)
                miniStat("Positive rate", summary?.positiveReplyRate, percent: true, tone: .emerald, index: 2)
                miniStat("Bounce rate", summary?.bounceRate, percent: true, tone: bounce >= 5 ? .rose : (bounce >= 2 ? .amber : nil), index: 3)
                miniStat("Clicks", (summary?.uniqueClicks).map(Double.init), index: 4)
                miniStat("Interested", (summary?.interestedLeads).map(Double.init), index: 5)
                miniStat("Positive", (summary?.positiveReplies).map(Double.init), index: 6)
                miniStat("Unsubs", (summary?.unsubscribes).map(Double.init), index: 7)
                miniStat("In queue", (summary?.emailsPending).map(Double.init), index: 8)
            }
            .padding(.vertical, 12)
        }
        if let machine = summary?.machineOpens, machine > 0 {
            plainRow(separator: .hidden) {
                Text("\(WFormat.compact(machine)) automated opens (Apple Mail Privacy et al.) are excluded from the open rate.")
                    .font(.system(size: 12.5))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkTertiary)
                    .padding(.bottom, 8)
            }
        }
    }

    private static let statColumns = [
        GridItem(.flexible(), alignment: .leading),
        GridItem(.flexible(), alignment: .leading),
        GridItem(.flexible(), alignment: .leading),
    ]

    /// A stat that counts up from zero on arrival, one beat after the last.
    private func miniStat(_ label: String, _ value: Double?, percent: Bool = false, tone: Tone? = nil, index: Int) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            EyebrowLabel(label)
            CountUpText(value: shown ? (value ?? 0) : 0) { current in
                percent ? String(format: "%.1f%%", current) : WFormat.compact(Int(current.rounded()))
            }
            .font(.system(size: 21, weight: .regular))
            .tracking(-0.7)
            .monospacedDigit()
            .foregroundStyle(tone?.color ?? WTheme.ink)
            .lineLimit(1)
            .minimumScaleFactor(0.75)
            .animation(.easeOut(duration: 1).delay(0.3 + Double(index) * 0.05), value: shown)
            .animation(.snappy, value: value)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: Browse

    @ViewBuilder
    private var browseSection: some View {
        sectionHeader("Manage")
        plainRow {
            // Leads opens the full browser as a cover (its own drawer + filters),
            // not a push, so it gets the same browsing surface as the Contacts tab.
            Button {
                showLeads = true
            } label: {
                HStack(spacing: 12) {
                    rowTile("person.2")
                    Text("Leads")
                        .font(.system(size: 15.5))
                        .foregroundStyle(WTheme.ink)
                    Spacer(minLength: 8)
                    if let value = overviewStore.analytics?.summary?.totalContacts.map(WFormat.compact) {
                        Text(value)
                            .font(WFont.meta(12))
                            .foregroundStyle(WTheme.inkTertiary)
                            .contentTransition(.numericText())
                    }
                    Image(systemName: "chevron.right")
                        .font(.system(size: 12, weight: .medium))
                        .foregroundStyle(WTheme.inkTertiary)
                }
                .padding(.vertical, 11)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        }
        plainRow {
            navRow(
                icon: "list.number", title: "Sequence",
                value: stepsStore.hasLoaded ? "\(stepsStore.steps.count) step\(stepsStore.steps.count == 1 ? "" : "s")" : nil
            ) {
                CampaignSequencePage(campaignID: campaign.id, store: stepsStore)
            }
        }
        plainRow {
            navRow(
                icon: "tray.full", title: "Senders",
                value: sendersStore.hasLoaded ? WFormat.compact(sendersStore.senders.count) : nil
            ) {
                CampaignSendersPageView(campaign: campaign, store: sendersStore)
            }
        }
    }

    private func navRow<Destination: View>(
        icon: String,
        title: String,
        value: String?,
        @ViewBuilder destination: @escaping () -> Destination
    ) -> some View {
        NavigationLink {
            destination()
        } label: {
            HStack(spacing: 12) {
                rowTile(icon)
                Text(title)
                    .font(.system(size: 15.5))
                    .foregroundStyle(WTheme.ink)
                Spacer(minLength: 8)
                if let value {
                    Text(value)
                        .font(WFont.meta(12))
                        .foregroundStyle(WTheme.inkTertiary)
                        .contentTransition(.numericText())
                }
            }
            .padding(.vertical, 11)
            .contentShape(Rectangle())
        }
    }

    // MARK: Sending (inline editable)

    @ViewBuilder
    private var sendingSection: some View {
        sectionHeader("Sending")
        plainRow {
            HStack(spacing: 12) {
                rowTile("gauge.with.dots.needle.50percent")
                VStack(alignment: .leading, spacing: 1) {
                    Text("Daily limit")
                        .font(.system(size: 15.5))
                        .foregroundStyle(WTheme.ink)
                    Text("Emails per mailbox each day")
                        .font(.system(size: 13))
                        .foregroundStyle(WTheme.inkSecondary)
                }
                Spacer(minLength: 8)
                stepper
            }
            .padding(.vertical, 9)
        }
        settingToggle(
            "Stop on reply", subtitle: "Pause a lead when they answer",
            icon: "hand.raised",
            value: campaign.stopOnReply ?? true,
            body: { CampaignUpdateBody(stopOnReply: $0) },
            mutate: { c, v in c.stopOnReply = v }
        )
        settingToggle(
            "Open tracking", subtitle: "Count when a lead opens",
            icon: "envelope.open",
            value: campaign.openTracking ?? true,
            body: { CampaignUpdateBody(openTracking: $0) },
            mutate: { c, v in c.openTracking = v }
        )
        settingToggle(
            "Link tracking", subtitle: "Count when a lead clicks",
            icon: "link",
            value: campaign.linkTracking ?? true,
            body: { CampaignUpdateBody(linkTracking: $0) },
            mutate: { c, v in c.linkTracking = v }
        )
    }

    /// Two-disc themed stepper (never the native boxed `Stepper`); clamps to
    /// the server-validated 1...100 campaign-limit range.
    private var stepper: some View {
        HStack(spacing: 12) {
            stepButton("minus", enabled: canManage && dailyLimit > 1) {
                dailyLimit = max(1, dailyLimit - 1)
            }
            Text("\(dailyLimit)")
                .font(WFont.meta(16, weight: .medium))
                .foregroundStyle(WTheme.ink)
                .frame(minWidth: 32)
                .contentTransition(.numericText())
            stepButton("plus", enabled: canManage && dailyLimit < 100) {
                dailyLimit = min(100, dailyLimit + 1)
            }
        }
    }

    private func stepButton(_ symbol: String, enabled: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(enabled ? WTheme.ink : WTheme.inkTertiary)
                .frame(width: 30, height: 30)
                .background(WTheme.raised, in: Circle())
                .overlay(Circle().strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
                .opacity(enabled ? 1 : 0.6)
        }
        .buttonStyle(TapScaleStyle())
        .disabled(!enabled)
    }

    private func settingToggle(
        _ title: String,
        subtitle: String,
        icon: String,
        value: Bool,
        body: @escaping (Bool) -> CampaignUpdateBody,
        mutate: @escaping (inout Campaign, Bool) -> Void
    ) -> some View {
        plainRow {
            Toggle(isOn: Binding(
                get: { value },
                set: { newValue in
                    Task {
                        await store.update(env.api, body: body(newValue)) { mutate(&$0, newValue) }
                    }
                }
            )) {
                HStack(spacing: 12) {
                    rowTile(icon)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(title)
                            .font(.system(size: 15.5))
                            .foregroundStyle(WTheme.ink)
                        Text(subtitle)
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkSecondary)
                    }
                }
            }
            .disabled(!canManage)
            .padding(.vertical, 9)
        }
    }

    /// Neutral hairline tile leading a hub row; color is kept for state.
    private func rowTile(_ symbol: String) -> some View {
        Image(systemName: symbol)
            .font(.system(size: 14, weight: .regular))
            .foregroundStyle(WTheme.ink)
            .frame(width: 34, height: 34)
            .background(WTheme.raised, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .strokeBorder(WTheme.hairlineStrong, lineWidth: 1)
            )
    }

    /// Steppers fire per tap; coalesce into one PATCH after the taps stop.
    private func saveDailyLimitDebounced(_ value: Int) {
        limitSaveTask?.cancel()
        limitSaveTask = Task {
            try? await Task.sleep(for: .milliseconds(700))
            guard !Task.isCancelled else { return }
            await store.update(env.api, body: CampaignUpdateBody(dailyLimit: value)) {
                $0.dailyLimit = value
            }
            dailyLimit = store.campaign.dailyLimit ?? value
        }
    }

    // MARK: Setup

    @ViewBuilder
    private var setupSection: some View {
        sectionHeader("Setup")
        plainRow {
            navRow(icon: "calendar", title: "Schedule", value: nil) {
                CampaignSchedulePage(store: store)
            }
        }
        plainRow {
            navRow(icon: "gearshape", title: "Settings", value: nil) {
                CampaignSettingsPage(store: store, onDeleted: { leave() })
            }
        }
    }

    // MARK: About

    @ViewBuilder
    private var aboutSection: some View {
        if let description = campaign.description, !description.isEmpty {
            plainRow(separator: .hidden) {
                Text(description)
                    .font(.system(size: 14.5))
                    .foregroundStyle(WTheme.inkSecondary)
                    .padding(.top, 18)
            }
        }
        if let created = campaign.createdAt {
            plainRow(separator: .hidden) {
                Text("Created \(created.formatted(date: .abbreviated, time: .omitted)) · Updated \(WFormat.relative(campaign.updatedAt ?? created))")
                    .font(WFont.meta(11))
                    .foregroundStyle(WTheme.inkTertiary)
                    .padding(.top, 10)
                    .padding(.bottom, 28)
            }
        }
    }

    /// Back to the list: pops the stack, or empties the detail column beside it.
    private func leave() {
        if let closeDetail { closeDetail() } else { dismiss() }
    }

    private func deleteCampaign() async {
        do {
            try await store.delete(env.api)
            leave()
        } catch {
            deleteError = error.localizedDescription
        }
    }
}

/// Compact icon + title for the hero's start/pause pill.
private struct ToggleLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 6) {
            configuration.icon
                .font(.system(size: 11, weight: .semibold))
            configuration.title
        }
    }
}
