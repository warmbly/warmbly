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

/// Campaign hub: sky hero with status + start/pause, then one flat, full-bleed
/// sheet (no cards) — headline volume in the hero, rates and secondary counts
/// as bare stat columns, a live chart, then hairline-separated rows to browse
/// (Leads / Sequence / Senders), tune sending inline, and open Schedule /
/// Settings. Sub-pages push on the Campaigns tab's NavigationStack.
struct CampaignDetailView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

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

    init(campaign: Campaign) {
        _store = State(initialValue: CampaignDetailStore(campaign: campaign))
        _dailyLimit = State(initialValue: campaign.dailyLimit ?? 50)
    }

    private var campaign: Campaign { store.campaign }
    private var presenceKey: String { "campaign:\(campaign.id)" }
    private var canManage: Bool { env.session.can(.manageCampaigns) }

    private var sheetShape: UnevenRoundedRectangle {
        UnevenRoundedRectangle(topLeadingRadius: 26, topTrailingRadius: 26, style: .continuous)
    }

    /// Content leading where row text starts (16 inset + 34 tile + 12 gap), so
    /// hairlines tuck under the text column, Gmail-style.
    private static let rowTextLeading: CGFloat = 62

    var body: some View {
        VStack(spacing: 0) {
            hero
                .frame(maxWidth: .infinity, alignment: .leading)
            hubList
                .clipShape(sheetShape)
                .background(
                    sheetShape
                        .fill(Color(.systemBackground))
                        .ignoresSafeArea(edges: .bottom)
                        .shadow(color: .black.opacity(0.12), radius: 18, y: -4)
                )
        }
        .background(alignment: .top) {
            AirSkyWash().ignoresSafeArea(edges: .top)
        }
        .toolbarBackground(.hidden, for: .navigationBar)
        .toolbarColorScheme(.dark, for: .navigationBar)
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbarContent }
        .presenceResource(presenceKey)
        .task {
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
                        Text(campaign.scheduleSummary)
                            .font(.footnote)
                            .monospacedDigit()
                            .foregroundStyle(.white.opacity(0.72))
                            .lineLimit(1)
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
            .listRowBackground(Color(.systemBackground))
    }

    private func plainRow<Content: View>(
        separator: Visibility = .automatic,
        @ViewBuilder _ content: () -> Content
    ) -> some View {
        content()
            .listRowInsets(EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20))
            .listRowSeparator(separator)
            .alignmentGuide(.listRowSeparatorLeading) { _ in Self.rowTextLeading }
            .listRowBackground(Color(.systemBackground))
    }

    // MARK: Performance

    @ViewBuilder
    private var performanceSection: some View {
        let summary = overviewStore.analytics?.summary
        sectionHeader("Performance", top: 14)
        plainRow(separator: .hidden) {
            let bounce = summary?.bounceRate ?? 0
            LazyVGrid(columns: Self.statColumns, alignment: .leading, spacing: 16) {
                miniStat("Open rate", rate(summary?.openRate), tone: .sky)
                miniStat("Reply rate", rate(summary?.replyRate), tone: .emerald)
                miniStat("Bounce rate", rate(summary?.bounceRate), tone: bounce >= 5 ? .rose : (bounce >= 2 ? .amber : nil))
                miniStat("Clicks", WFormat.compact(summary?.uniqueClicks ?? 0))
                miniStat("Unsubs", WFormat.compact(summary?.unsubscribes ?? 0))
                miniStat("In queue", WFormat.compact(summary?.emailsPending ?? 0))
            }
            .padding(.vertical, 12)
        }
        if overviewStore.hasLoaded, !overviewStore.daily.isEmpty {
            plainRow(separator: .hidden) {
                CampaignDailyChartRow(daily: overviewStore.daily)
            }
        }
        if let machine = summary?.machineOpens, machine > 0 {
            plainRow(separator: .hidden) {
                Text("\(WFormat.compact(machine)) automated opens (Apple Mail Privacy et al.) are excluded from the open rate.")
                    .font(.footnote)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                    .padding(.bottom, 8)
            }
        }
    }

    private static let statColumns = [
        GridItem(.flexible(), alignment: .leading),
        GridItem(.flexible(), alignment: .leading),
        GridItem(.flexible(), alignment: .leading),
    ]

    private func miniStat(_ label: String, _ value: String, tone: Tone? = nil) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            EyebrowLabel(label)
            Text(value)
                .font(.system(size: 19, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(tone?.color ?? Color.primary)
                .contentTransition(.numericText())
                .lineLimit(1)
                .minimumScaleFactor(0.75)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func rate(_ value: Double?) -> String {
        String(format: "%.1f%%", value ?? 0)
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
                    IconTile(symbol: "person.2.fill", tone: .sky, size: 34)
                    Text("Leads")
                        .font(.body.weight(.medium))
                        .foregroundStyle(.primary)
                    Spacer(minLength: 8)
                    if let value = overviewStore.analytics?.summary?.totalContacts.map(WFormat.compact) {
                        Text(value)
                            .font(.footnote)
                            .monospacedDigit()
                            .foregroundStyle(.tertiary)
                            .contentTransition(.numericText())
                    }
                    Image(systemName: "chevron.right")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(.tertiary)
                }
                .padding(.vertical, 11)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        }
        plainRow {
            navRow(
                icon: "list.number", tone: .indigo, title: "Sequence",
                value: stepsStore.hasLoaded ? "\(stepsStore.steps.count) step\(stepsStore.steps.count == 1 ? "" : "s")" : nil
            ) {
                CampaignSequencePage(campaignID: campaign.id, store: stepsStore)
            }
        }
        plainRow {
            navRow(
                icon: "tray.full.fill", tone: .emerald, title: "Senders",
                value: sendersStore.hasLoaded ? WFormat.compact(sendersStore.senders.count) : nil
            ) {
                CampaignSendersPageView(campaign: campaign, store: sendersStore)
            }
        }
    }

    private func navRow<Destination: View>(
        icon: String,
        tone: Tone,
        title: String,
        value: String?,
        @ViewBuilder destination: @escaping () -> Destination
    ) -> some View {
        NavigationLink {
            destination()
        } label: {
            HStack(spacing: 12) {
                IconTile(symbol: icon, tone: tone, size: 34)
                Text(title)
                    .font(.body.weight(.medium))
                Spacer(minLength: 8)
                if let value {
                    Text(value)
                        .font(.footnote)
                        .monospacedDigit()
                        .foregroundStyle(.tertiary)
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
                IconTile(symbol: "gauge.with.dots.needle.50percent", tone: .amber, size: 34)
                VStack(alignment: .leading, spacing: 1) {
                    Text("Daily limit")
                        .font(.body.weight(.medium))
                    Text("Emails per mailbox each day")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 8)
                stepper
            }
            .padding(.vertical, 9)
        }
        settingToggle(
            "Stop on reply", subtitle: "Pause a lead when they answer",
            icon: "hand.raised.fill", tone: .emerald,
            value: campaign.stopOnReply ?? true,
            body: { CampaignUpdateBody(stopOnReply: $0) },
            mutate: { c, v in c.stopOnReply = v }
        )
        settingToggle(
            "Open tracking", subtitle: "Count when a lead opens",
            icon: "envelope.open.fill", tone: .sky,
            value: campaign.openTracking ?? true,
            body: { CampaignUpdateBody(openTracking: $0) },
            mutate: { c, v in c.openTracking = v }
        )
        settingToggle(
            "Link tracking", subtitle: "Count when a lead clicks",
            icon: "link", tone: .sky,
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
                .font(.system(size: 17, weight: .semibold))
                .monospacedDigit()
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
                .font(.system(size: 13, weight: .bold))
                .foregroundStyle(enabled ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(Color(.tertiaryLabel)))
                .frame(width: 30, height: 30)
                .background(Tone.slate.background, in: Circle())
        }
        .buttonStyle(TapScaleStyle())
        .disabled(!enabled)
    }

    private func settingToggle(
        _ title: String,
        subtitle: String,
        icon: String,
        tone: Tone,
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
                    IconTile(symbol: icon, tone: tone, size: 34)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(title)
                            .font(.body.weight(.medium))
                        Text(subtitle)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .tint(WTheme.accent)
            .disabled(!canManage)
            .padding(.vertical, 9)
        }
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
            navRow(icon: "calendar", tone: .amber, title: "Schedule", value: nil) {
                CampaignSchedulePage(store: store)
            }
        }
        plainRow {
            navRow(icon: "gearshape.fill", tone: .slate, title: "Settings", value: nil) {
                CampaignSettingsPage(store: store, onDeleted: { dismiss() })
            }
        }
    }

    // MARK: About

    @ViewBuilder
    private var aboutSection: some View {
        if let description = campaign.description, !description.isEmpty {
            plainRow(separator: .hidden) {
                Text(description)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.top, 18)
            }
        }
        if let created = campaign.createdAt {
            plainRow(separator: .hidden) {
                Text("Created \(created.formatted(date: .abbreviated, time: .omitted)) · Updated \(WFormat.relative(campaign.updatedAt ?? created))")
                    .font(.footnote)
                    .foregroundStyle(.tertiary)
                    .padding(.top, 10)
                    .padding(.bottom, 28)
            }
        }
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
