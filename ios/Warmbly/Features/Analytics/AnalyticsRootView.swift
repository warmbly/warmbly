import SwiftUI

// MARK: - Scopes

/// Drawer sections of the analytics browser.
enum AnalyticsScope: String, CaseIterable, Identifiable {
    case overview, deliverability, warmup, accounts

    var id: String { rawValue }

    var title: String {
        switch self {
        case .overview: "Overview"
        case .deliverability: "Deliverability"
        case .warmup: "Warmup"
        case .accounts: "Mailboxes"
        }
    }

    var icon: String {
        switch self {
        case .overview: "chart.bar.fill"
        case .deliverability: "checkmark.shield.fill"
        case .warmup: "flame.fill"
        case .accounts: "envelope.fill"
        }
    }

    var tone: Tone {
        switch self {
        case .overview: .sky
        case .deliverability: .emerald
        case .warmup: .orange
        case .accounts: .indigo
        }
    }
}

extension AnalyticsPeriod {
    var label: String {
        switch self {
        case .week: "7 days"
        case .month: "30 days"
        case .quarter: "90 days"
        }
    }
}

// MARK: - View

/// Analytics browser, presented full screen from Home or Settings: a glass
/// header with the drawer of sections, the period and campaign scope, and the
/// selected section as cards on the grouped page. Owns its NavigationStack;
/// dismissal goes through `onClose`.
struct AnalyticsRootView: View {
    var onClose: () -> Void = {}

    @Environment(AppEnvironment.self) private var env
    @State private var store = AnalyticsStore()
    @State private var scope: AnalyticsScope = .overview
    @State private var sidebarOpen = false
    @State private var sidebarDrag: CGFloat = 0
    @State private var filterOpen = false
    @State private var shown = false

    private static let sidebarWidth: CGFloat = 300

    var body: some View {
        NavigationStack {
            if env.session.can(.viewAnalytics) {
                browser
            } else {
                noAccess
            }
        }
    }

    // MARK: No access

    private var noAccess: some View {
        VStack(spacing: 0) {
            CoverHeader(title: "Analytics", onClose: onClose)
            Spacer()
            CoverEmptyState(
                symbol: "lock.fill",
                color: WTheme.inkSecondary,
                title: "No access",
                message: "Ask a workspace admin for the view analytics permission."
            )
            Spacer()
        }
        .background(WTheme.groupedPage)
        .toolbarVisibility(.hidden, for: .navigationBar)
    }

    // MARK: Browser shell

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
        .toolbarVisibility(.hidden, for: .navigationBar)
        .task {
            #if DEBUG
            if let raw = ProcessInfo.processInfo.environment["WARMBLY_DEBUG_ANALYTICS"], let debug = AnalyticsScope(rawValue: raw) {
                scope = debug
            }
            #endif
            await store.load(env.api)
            shown = true
        }
        .onChange(of: env.realtime.pulse(for: .analytics)) {
            Task { await store.load(env.api) }
        }
        .onChange(of: store.period) {
            Task { await store.load(env.api) }
        }
        .onChange(of: store.campaignFilter) {
            Task { await store.reloadDashboard(env.api) }
        }
        .sheet(isPresented: $filterOpen) {
            AnalyticsCampaignFilterSheet(initial: store.campaignFilter) { picked in
                store.campaignFilter = picked
            }
        }
        .sensoryFeedback(.selection, trigger: scope)
        .sensoryFeedback(.impact(weight: .light), trigger: sidebarOpen)
    }

    // MARK: Main pane

    private var mainPane: some View {
        VStack(spacing: 0) {
            CoverHeader(
                title: scope == .overview ? "Analytics" : scope.title,
                subtitle: scope == .accounts
                    ? "\(store.accounts.count) \(store.accounts.count == 1 ? "mailbox" : "mailboxes")"
                    : "Last \(store.period.label)",
                onMenu: openSidebar,
                onClose: onClose
            )
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    controls
                    Group {
                        switch scope {
                        case .overview: overview
                        case .deliverability: AnalyticsDeliverabilityPage(store: store, shown: shown)
                        case .warmup: AnalyticsWarmupPage(store: store, shown: shown)
                        case .accounts: AnalyticsAccountsPage(store: store, shown: shown)
                        }
                    }
                    .id(scope)
                    .transition(.asymmetric(
                        insertion: .opacity.combined(with: .offset(y: 14)),
                        removal: .opacity
                    ))
                }
                .padding(.horizontal, 16)
                .padding(.top, 4)
                .padding(.bottom, 60)
            }
            .scrollIndicators(.hidden)
            .defaultScrollAnchor(SettingsDebug.anchor)
            .refreshable { await store.load(env.api) }
            .readableMargins(900)
        }
        .background(WTheme.groupedPage)
        .simultaneousGesture(
            DragGesture(minimumDistance: 25)
                .onEnded { value in
                    if !sidebarOpen, value.startLocation.x < 44, value.translation.width > 70 {
                        openSidebar()
                    }
                }
        )
    }

    // MARK: Controls

    private var scopeLabel: String {
        store.scopeLabel { id in env.session.user?.folders?.first { $0.id == id }?.name }
    }

    private var controls: some View {
        VStack(alignment: .leading, spacing: 10) {
            if scope != .accounts {
                PillSwitcher(
                    options: AnalyticsPeriod.allCases,
                    selection: Binding(get: { store.period }, set: { store.period = $0 }),
                    label: \.label
                )
            }
            if scope == .overview {
                filterChip
            }
        }
    }

    private var filterChip: some View {
        let scoped = !store.campaignFilter.isEmpty
        return HStack(spacing: 8) {
            Button {
                filterOpen = true
            } label: {
                HStack(spacing: 7) {
                    Image(systemName: "megaphone.fill")
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(scoped ? WTheme.accent : WTheme.inkSecondary)
                    Text(scopeLabel)
                        .font(.system(size: 14, weight: .medium))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                    Image(systemName: "chevron.down")
                        .font(.system(size: 10, weight: .bold))
                        .foregroundStyle(WTheme.inkTertiary)
                }
                .padding(.horizontal, 13)
                .frame(height: 34)
                .background(scoped ? WTheme.accentSoft : WTheme.groupedCard, in: Capsule())
            }
            .buttonStyle(TapScaleStyle())
            .accessibilityLabel("Campaign filter: \(scopeLabel)")
            if scoped {
                Button {
                    withAnimation(.snappy) { store.campaignFilter = AnalyticsCampaignFilter() }
                } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 11, weight: .bold))
                        .foregroundStyle(WTheme.inkSecondary)
                        .frame(width: 34, height: 34)
                        .background(WTheme.groupedCard, in: Circle())
                }
                .buttonStyle(TapScaleStyle())
                .transition(.scale.combined(with: .opacity))
                .accessibilityLabel("Show all campaigns")
            }
            Spacer(minLength: 0)
        }
        .animation(.snappy, value: scoped)
    }

    // MARK: Overview

    @ViewBuilder
    private var overview: some View {
        if store.dashboard != nil {
            VStack(alignment: .leading, spacing: 16) {
                trendCard.reveal(shown, 0)
                kpiCard.reveal(shown, 1)
                funnelCard.reveal(shown, 2)
                if !(store.dashboard?.topCampaigns ?? []).isEmpty {
                    campaignsCard.reveal(shown, 3)
                }
                if !(store.dashboard?.recentActivity ?? []).isEmpty {
                    activityCard.reveal(shown, 4)
                }
            }
        } else if let error = store.dashboardError {
            ErrorStateView(title: "Couldn't load analytics", message: error) {
                await store.load(env.api)
            }
            .padding(.top, 40)
        } else {
            AnalyticsLoadingCards()
        }
    }

    private var trendDays: [TrendDay] {
        var values: [String: TrendDay] = [:]
        for point in store.dashboard?.dailyTrend ?? [] {
            guard let raw = point.date else { continue }
            values[String(raw.prefix(10))] = TrendDay(
                date: .now,
                sent: point.sent ?? 0,
                opens: point.opens ?? 0,
                clicks: point.clicks ?? 0,
                replies: point.replies ?? 0
            )
        }
        return TrendDay.lastDays(store.period.days, values: values)
    }

    private var trendCard: some View {
        TrendChart(days: trendDays, shown: shown, caption: "Last \(store.period.label), drag to see a day")
            .coverCard()
    }

    // MARK: KPIs

    private struct KPI {
        let label: String
        let value: Double
        let format: (Double) -> String
        let delta: Double?
        let points: Bool
        var lowerIsBetter = false
        let series: [Double]
        let tint: Color
    }

    private var current: AnalyticsTotals {
        let stats = store.dashboard?.overallStats
        return AnalyticsTotals(
            sent: stats?.totalEmailsSent ?? 0,
            opens: stats?.totalOpens ?? 0,
            clicks: stats?.totalClicks ?? 0,
            replies: stats?.totalReplies ?? 0
        )
    }

    private var kpis: [KPI] {
        let now = current
        let before = store.previous
        let days = trendDays
        func rate(_ part: (TrendDay) -> Int) -> [Double] {
            days.map { $0.sent > 0 ? Double(part($0)) / Double($0.sent) * 100 : 0 }
        }
        let pct: (Double) -> String = { String(format: "%.1f%%", $0) }
        let stats = store.dashboard?.overallStats
        return [
            KPI(label: "Sent", value: Double(now.sent), format: { WFormat.compact(Int($0.rounded())) },
                delta: before.flatMap { DeltaLabel.percent(now.sent, $0.sent) }, points: false,
                series: days.map { Double($0.sent) }, tint: WTheme.accent),
            KPI(label: "Open rate", value: stats?.openRate ?? 0, format: pct,
                delta: DeltaLabel.points(stats?.openRate, before?.openRate), points: true,
                series: rate(\.opens), tint: WTheme.special),
            KPI(label: "Reply rate", value: stats?.replyRate ?? 0, format: pct,
                delta: DeltaLabel.points(stats?.replyRate, before?.replyRate), points: true,
                series: rate(\.replies), tint: WTheme.positive),
            KPI(label: "Click rate", value: stats?.clickRate ?? 0, format: pct,
                delta: DeltaLabel.points(stats?.clickRate, before?.clickRate), points: true,
                series: rate(\.clicks), tint: Tone.orange.color),
        ]
    }

    private var kpiCard: some View {
        let items = kpis
        return VStack(alignment: .leading, spacing: 0) {
            ForEach(0 ..< 2, id: \.self) { row in
                HStack(spacing: 0) {
                    kpiCell(items[row * 2], index: row * 2)
                        .padding(.trailing, 16)
                    Rectangle().fill(WTheme.hairline).frame(width: 1)
                    kpiCell(items[row * 2 + 1], index: row * 2 + 1)
                        .padding(.leading, 16)
                }
                .fixedSize(horizontal: false, vertical: true)
                if row == 0 { Hairline() }
            }
            Hairline().padding(.top, 2)
            factsLine
                .padding(.top, 14)
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 6)
        .padding(.bottom, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(WTheme.groupedCard, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
    }

    private func kpiCell(_ kpi: KPI, index: Int) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Circle().fill(kpi.tint).frame(width: 6, height: 6)
                Text(kpi.label)
                    .font(.system(size: 13.5, weight: .medium))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
            }
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                CountUpText(value: shown ? kpi.value : 0, format: kpi.format)
                    .font(.system(size: 28, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                    .animation(.easeOut(duration: 1).delay(Double(index) * 0.05), value: shown)
                    .animation(.easeOut(duration: 0.6), value: kpi.value)
                Spacer(minLength: 0)
                if store.previous != nil {
                    DeltaLabel(change: kpi.delta, points: kpi.points, lowerIsBetter: kpi.lowerIsBetter)
                        .lineLimit(1)
                        .fixedSize()
                        .transition(.opacity)
                }
            }
            Sparkline(values: kpi.series, color: kpi.tint, delay: 0.25 + Double(index) * 0.08)
                .frame(height: 30)
                .id("\(store.period.rawValue)-\(index)")
        }
        .padding(.vertical, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var factsLine: some View {
        let stats = store.dashboard?.overallStats
        let campaigns = stats?.activeCampaigns ?? 0
        let mailboxes = stats?.activeAccounts ?? 0
        let bounce = stats?.bounceRate ?? 0
        let bold: (String) -> Text = { Text($0).foregroundStyle(WTheme.ink).fontWeight(.semibold) }
        let bounceText = Text(String(format: "%.1f%%", bounce))
            .foregroundStyle(bounce >= 5 ? WTheme.negative : bounce >= 2 ? WTheme.warning : WTheme.ink)
            .fontWeight(.semibold)
        return Text("\(bold("\(campaigns)")) active \(campaigns == 1 ? "campaign" : "campaigns")  ·  \(bold("\(mailboxes)")) sending \(mailboxes == 1 ? "mailbox" : "mailboxes")  ·  \(bounceText) bounced")
            .font(.system(size: 13.5))
            .foregroundStyle(WTheme.inkSecondary)
            .lineSpacing(3)
    }

    // MARK: Funnel

    private var funnelCard: some View {
        let now = current
        let steps: [(label: String, symbol: String, value: Int, color: Color)] = [
            ("Sent", "paperplane.fill", now.sent, WTheme.accent),
            ("Opened", "envelope.open.fill", now.opens, WTheme.special),
            ("Clicked", "cursorarrow.click", now.clicks, Tone.orange.color),
            ("Replied", "arrowshape.turn.up.left.fill", now.replies, WTheme.positive),
        ]
        let base = max(1, now.sent)
        return VStack(alignment: .leading, spacing: 16) {
            CardTitle("From send to reply", caption: "How far each email got")
            VStack(spacing: 14) {
                ForEach(Array(steps.enumerated()), id: \.offset) { index, step in
                    VStack(alignment: .leading, spacing: 7) {
                        HStack(spacing: 8) {
                            Image(systemName: step.symbol)
                                .font(.system(size: 12, weight: .semibold))
                                .foregroundStyle(step.color)
                                .frame(width: 18)
                            Text(step.label)
                                .font(.system(size: 15))
                                .foregroundStyle(WTheme.ink)
                            Spacer()
                            CountUpText(value: shown ? Double(step.value) : 0, format: { WFormat.compact(Int($0.rounded())) })
                                .font(.system(size: 15, weight: .semibold))
                                .monospacedDigit()
                                .foregroundStyle(WTheme.ink)
                                .animation(.easeOut(duration: 0.9).delay(0.2 + Double(index) * 0.1), value: shown)
                            Text(index == 0 ? "100%" : WFormat.percent(step.value, of: now.sent))
                                .font(.system(size: 13))
                                .monospacedDigit()
                                .foregroundStyle(WTheme.inkTertiary)
                                .frame(width: 46, alignment: .trailing)
                        }
                        GrowBar(
                            fraction: Double(step.value) / Double(base),
                            color: step.color,
                            shown: shown,
                            delay: 0.2 + Double(index) * 0.12,
                            height: 8
                        )
                        .padding(.leading, 26)
                    }
                }
            }
        }
        .coverCard()
    }

    // MARK: Campaigns

    private var campaignsCard: some View {
        let campaigns = Array((store.dashboard?.topCampaigns ?? []).prefix(5))
        return VStack(alignment: .leading, spacing: 4) {
            CardTitle("Top campaigns", caption: "Ranked by sends this period")
                .padding(.bottom, 8)
            ForEach(Array(campaigns.enumerated()), id: \.element.id) { index, campaign in
                if index > 0 { Hairline().padding(.leading, 52) }
                AnalyticsCampaignRow(campaign: campaign, rank: index + 1, shown: shown)
            }
        }
        .coverCard()
    }

    // MARK: Activity

    private var activityCard: some View {
        let items = Array((store.dashboard?.recentActivity ?? []).prefix(8))
        return VStack(alignment: .leading, spacing: 4) {
            CardTitle("Latest from your recipients")
                .padding(.bottom, 8)
            ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                if index > 0 { Hairline().padding(.leading, 52) }
                AnalyticsActivityRow(item: item)
            }
        }
        .coverCard()
    }

    // MARK: Drawer

    private func drawer(topInset: CGFloat) -> some View {
        AnalyticsSidebar(
            store: store,
            selection: scope,
            topInset: topInset,
            revealed: sidebarOpen
        ) { newScope in
            withAnimation(.spring(response: 0.45, dampingFraction: 0.86)) { scope = newScope }
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
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) { sidebarOpen = true }
    }

    private func closeSidebar() {
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) {
            sidebarOpen = false
            sidebarDrag = 0
        }
    }
}

// MARK: - Rows

/// A ranked campaign: its status, sends, and reply rate as a small ring.
struct AnalyticsCampaignRow: View {
    let campaign: AnalyticsTopCampaign
    let rank: Int
    var shown: Bool

    var body: some View {
        let status = CampaignStatusInfo.from(campaign.status)
        let reply = campaign.replyRate ?? 0
        return HStack(spacing: 12) {
            Text("\(rank)")
                .font(.system(size: 15, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(rank == 1 ? WTheme.accent : WTheme.inkSecondary)
                .frame(width: 34, height: 34)
                .background(rank == 1 ? WTheme.accentSoft : WTheme.fill, in: Circle())
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 7) {
                    Text(campaign.name ?? "Untitled campaign")
                        .font(.system(size: 15.5, weight: .medium))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                    StatusPill(text: status.label.prefix(1).uppercased() + status.label.dropFirst(), tone: status.tone)
                        .fixedSize()
                }
                Text("\(WFormat.compact(campaign.emailsSent ?? 0)) sent  ·  \(AnalyticsFmt.rate(campaign.openRate)) opened")
                    .font(.system(size: 13))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 6)
            SweepRing(fraction: reply / 100, color: WTheme.positive, shown: shown, lineWidth: 4, delay: 0.3 + Double(rank) * 0.08) {
                Text("\(Int(reply.rounded()))%")
                    .font(.system(size: 11, weight: .semibold))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.ink)
            }
            .frame(width: 40, height: 40)
            .accessibilityLabel("\(Int(reply.rounded())) percent replied")
        }
        .padding(.vertical, 10)
    }
}

/// One thing a recipient did, with what it was and when.
struct AnalyticsActivityRow: View {
    let item: AnalyticsActivityItem

    private var kind: (verb: String, symbol: String, color: Color) {
        switch item.type {
        case "opened": ("opened", "envelope.open.fill", WTheme.special)
        case "clicked": ("clicked a link", "cursorarrow.click", Tone.orange.color)
        case "replied": ("replied", "arrowshape.turn.up.left.fill", WTheme.positive)
        case "bounced": ("bounced", "exclamationmark.triangle.fill", WTheme.negative)
        default: ("was sent an email", "paperplane.fill", WTheme.accent)
        }
    }

    private var name: String {
        let email = item.contactEmail ?? "Someone"
        let local = email.split(separator: "@").first.map(String.init) ?? email
        return local.split(whereSeparator: { $0 == "." || $0 == "_" || $0 == "-" })
            .map { $0.prefix(1).uppercased() + $0.dropFirst() }
            .joined(separator: " ")
    }

    var body: some View {
        let kind = kind
        return HStack(spacing: 12) {
            WAvatar(name: name, seed: item.contactID ?? item.contactEmail ?? "", size: 38)
                .overlay(alignment: .bottomTrailing) {
                    Image(systemName: kind.symbol)
                        .font(.system(size: 8.5, weight: .bold))
                        .foregroundStyle(.white)
                        .frame(width: 18, height: 18)
                        .background(kind.color, in: Circle())
                        .overlay(Circle().stroke(WTheme.groupedCard, lineWidth: 2))
                        .offset(x: 4, y: 4)
                }
            VStack(alignment: .leading, spacing: 3) {
                Text("\(Text(name).fontWeight(.semibold).foregroundStyle(WTheme.ink)) \(kind.verb)")
                    .font(.system(size: 15))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
                Text(item.campaignName ?? item.contactEmail ?? "")
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.inkTertiary)
                    .lineLimit(1)
            }
            Spacer(minLength: 6)
            if let timestamp = item.timestamp {
                Text(WFormat.ago(timestamp))
                    .font(.system(size: 13))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkTertiary)
            }
        }
        .padding(.vertical, 10)
        .accessibilityElement(children: .combine)
    }
}

/// Grey placeholder cards with a shimmer, while the first load runs.
struct AnalyticsLoadingCards: View {
    var body: some View {
        VStack(spacing: 16) {
            ForEach([240.0, 190, 170], id: \.self) { height in
                RoundedRectangle(cornerRadius: 22, style: .continuous)
                    .fill(WTheme.groupedCard)
                    .frame(height: height)
                    .overlay(alignment: .topLeading) {
                        VStack(alignment: .leading, spacing: 12) {
                            RoundedRectangle(cornerRadius: 6).fill(WTheme.fill).frame(width: 120, height: 14)
                            RoundedRectangle(cornerRadius: 6).fill(WTheme.fill).frame(width: 80, height: 26)
                        }
                        .padding(18)
                    }
            }
        }
        .skeletonShimmer()
    }
}
