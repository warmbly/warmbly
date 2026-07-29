import SwiftUI
import Charts

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
        case .accounts: "Accounts"
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

// MARK: - Trend metric

/// Chart metric, driven by tapping the stat strip cells (bounces are not in
/// the daily trend payload, so the bounced cell is display-only).
enum AnalyticsTrendMetric: String, CaseIterable {
    case sent, opens, clicks, replies

    var title: String {
        switch self {
        case .sent: "Sent"
        case .opens: "Opens"
        case .clicks: "Clicks"
        case .replies: "Replies"
        }
    }

    var color: Color {
        switch self {
        case .sent: WTheme.accent
        case .opens: WTheme.positive
        case .clicks: Tone.indigo.color
        case .replies: Tone.emerald.color
        }
    }

    func value(_ point: AnalyticsTrendPoint) -> Int {
        switch self {
        case .sent: point.sent ?? 0
        case .opens: point.opens ?? 0
        case .clicks: point.clicks ?? 0
        case .replies: point.replies ?? 0
        }
    }
}

struct AnalyticsChartPoint: Identifiable {
    let id: Int
    let day: Date
    let value: Int
}

// MARK: - View

/// Analytics browser, presented as a full-screen cover from Home or More
/// (like the mailboxes and CRM browsers): a sky hero with the period picker
/// and headline chips, a slide-in drawer of sections, and the selected
/// section as a flat full-bleed list on a rounded white sheet. Owns its
/// NavigationStack; dismissal goes through `onClose` (the environment
/// DismissAction is unreliable in this app's cover contexts).
struct AnalyticsRootView: View {
    var onClose: () -> Void = {}

    @Environment(AppEnvironment.self) private var env
    @State private var store = AnalyticsStore()
    @State private var scope: AnalyticsScope = .overview
    @State private var metric: AnalyticsTrendMetric = .sent
    @State private var sidebarOpen = false
    @State private var sidebarDrag: CGFloat = 0

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
            HStack {
                Spacer()
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
                .accessibilityLabel("Close analytics")
            }
            .padding(.horizontal, 12)
            .padding(.top, 4)
            EmptyStateView(
                title: "No access",
                message: "You need the view analytics permission to see this page."
            )
        }
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
        // Own chrome: the sky hero runs to the top edge and carries the
        // hamburger + close buttons.
        .toolbarVisibility(.hidden, for: .navigationBar)
        .task { await store.load(env.api) }
        .onChange(of: env.realtime.pulse(for: .analytics)) {
            Task { await store.load(env.api) }
        }
        .onChange(of: store.period) {
            Task { await store.load(env.api) }
        }
        .sensoryFeedback(.selection, trigger: scope)
        .sensoryFeedback(.selection, trigger: store.period)
        .sensoryFeedback(.selection, trigger: metric)
        .sensoryFeedback(.impact(weight: .light), trigger: sidebarOpen)
    }

    // MARK: Main pane

    private var mainPane: some View {
        VStack(spacing: 0) {
            hero
            sheet
        }
        .background(alignment: .top) {
            AirSkyWash().ignoresSafeArea(edges: .top)
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

    // MARK: Hero

    private var hero: some View {
        VStack(alignment: .leading, spacing: 14) {
            topRow
            periodPicker
            heroStats
        }
        .padding(.horizontal, 16)
        .padding(.top, 2)
        .padding(.bottom, 18)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var topRow: some View {
        HStack(spacing: 12) {
            skyCircleButton("line.3.horizontal", label: "Open analytics menu") {
                openSidebar()
            }
            HStack(spacing: 8) {
                WarmblyLogo()
                    .fill(.white)
                    .frame(width: 21, height: 21 * (764 / 746))
                Text("Analytics")
                    .font(.system(size: 20, weight: .heavy))
                    .tracking(-0.4)
                    .foregroundStyle(.white)
                    .fixedSize()
            }
            Spacer()
            PresenceAvatars()
            skyCircleButton("xmark", label: "Close analytics", size: 15) {
                onClose()
            }
        }
    }

    private func skyCircleButton(
        _ symbol: String,
        label: String,
        size: CGFloat = 17,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: size, weight: .semibold))
                .foregroundStyle(.white)
                .frame(width: 40, height: 40)
                .background(.white.opacity(0.16), in: Circle())
        }
        .buttonStyle(TapScaleStyle())
        .accessibilityLabel(label)
    }

    /// White-glass capsule row bound to the store period; every load reacts.
    private var periodPicker: some View {
        HStack(spacing: 7) {
            ForEach(AnalyticsPeriod.allCases) { period in
                let selected = store.period == period
                Button {
                    withAnimation(.snappy) { store.period = period }
                } label: {
                    Text(period.rawValue)
                        .font(.footnote.weight(selected ? .semibold : .medium))
                        .monospacedDigit()
                        .foregroundStyle(selected ? .white : .white.opacity(0.7))
                        .padding(.horizontal, 13)
                        .padding(.vertical, 6)
                        .background(.white.opacity(selected ? 0.22 : 0.10), in: Capsule())
                }
                .buttonStyle(TapScaleStyle())
                .accessibilityLabel("Last \(period.days) days")
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
        }
        .padding(.horizontal, 4)
    }

    private var heroStats: some View {
        let stats = store.dashboard?.overallStats
        return HStack(spacing: 10) {
            AirStatChip(
                value: stats.map { WFormat.compact($0.totalEmailsSent ?? 0) } ?? "–",
                label: "Sent",
                symbol: "paperplane.fill"
            )
            AirStatChip(
                value: stats?.openRate.map { String(format: "%.0f%%", $0) } ?? "–",
                label: "Open rate",
                symbol: "envelope.open.fill"
            )
            AirStatChip(
                value: stats.map { WFormat.compact($0.totalReplies ?? 0) } ?? "–",
                label: "Replies",
                symbol: "arrowshape.turn.up.left.fill"
            )
        }
        .padding(.horizontal, 4)
    }

    // MARK: Sheet

    private var sheetShape: UnevenRoundedRectangle {
        UnevenRoundedRectangle(topLeadingRadius: 26, topTrailingRadius: 26, style: .continuous)
    }

    private var sheet: some View {
        sectionList
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .clipShape(sheetShape)
            .background(
                sheetShape
                    .fill(Color(.systemBackground))
                    .ignoresSafeArea(edges: .bottom)
                    .shadow(color: .black.opacity(0.12), radius: 18, y: -4)
            )
    }

    @ViewBuilder
    private var sectionList: some View {
        switch scope {
        case .overview: overviewList.transition(.opacity)
        case .deliverability: deliverabilityList.transition(.opacity)
        case .warmup: warmupList.transition(.opacity)
        case .accounts: accountsList.transition(.opacity)
        }
    }

    /// Full-bleed flat row (the row content manages its own gutters).
    private func fullBleedRow<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        content()
            .listRowInsets(EdgeInsets())
            .listRowSeparator(.hidden)
            .listRowBackground(Color(.systemBackground))
    }

    /// Breathing room after the last row of each section.
    private var sectionFooter: some View {
        Color.clear
            .frame(height: 24)
            .listRowInsets(EdgeInsets())
            .listRowSeparator(.hidden)
            .listRowBackground(Color(.systemBackground))
    }

    /// Tertiary trailing text for the scope caption rows.
    private var periodCaption: some View {
        Text("last \(store.period.rawValue)")
            .font(.caption.weight(.medium))
            .monospacedDigit()
            .foregroundStyle(.tertiary)
            .contentTransition(.numericText())
    }

    // MARK: Overview

    @ViewBuilder
    private var overviewList: some View {
        if store.dashboard != nil {
            List {
                AnalyticsSectionCaption("Overview", top: 18) { periodCaption }
                fullBleedRow { statStrip }
                fullBleedRow { activeRow }
                fullBleedRow { chartSection }
                AnalyticsTopCampaignsSection(campaigns: store.dashboard?.topCampaigns ?? [])
                AnalyticsActivitySection(items: store.dashboard?.recentActivity ?? [])
                sectionFooter
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .environment(\.defaultMinListRowHeight, 0)
            .refreshable { await store.load(env.api) }
        } else if let error = store.dashboardError {
            ErrorStateView(title: "Couldn't load analytics", message: error) {
                await store.load(env.api)
            }
        } else {
            ScrollView { SkeletonRows(rows: 10) }
        }
    }

    // MARK: Deliverability

    private var deliverabilityList: some View {
        List {
            AnalyticsDeliverabilitySection(
                summary: store.deliverability,
                error: store.deliverabilityError,
                captionTop: 18
            )
            sectionFooter
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .environment(\.defaultMinListRowHeight, 0)
        .refreshable { await store.load(env.api) }
    }

    // MARK: Warmup

    private var warmupList: some View {
        List {
            AnalyticsWarmupSection(
                warmup: store.warmup,
                error: store.warmupError,
                periodLabel: store.period.rawValue,
                captionTop: 18
            )
            sectionFooter
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .environment(\.defaultMinListRowHeight, 0)
        .refreshable { await store.load(env.api) }
    }

    // MARK: Accounts

    private var accountsList: some View {
        List {
            AnalyticsAccountsSection(
                accounts: store.accounts,
                counts: store.dashboard?.accountHealth,
                error: store.accountsError,
                isLoading: store.isLoading,
                captionTop: 18
            )
            sectionFooter
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .environment(\.defaultMinListRowHeight, 0)
        .refreshable { await store.load(env.api) }
    }

    // MARK: Stat strip

    private var statStrip: some View {
        let stats = store.dashboard?.overallStats
        let sent = stats?.totalEmailsSent ?? 0
        let opens = stats?.totalOpens ?? 0
        let machine = stats?.machineOpens ?? 0
        let clicks = stats?.totalClicks ?? 0
        let replies = stats?.totalReplies ?? 0
        let bounces = stats?.totalBounces ?? 0
        let bounceRate = stats?.bounceRate ?? 0

        var openCaption = WFormat.percent(opens, of: sent)
        if machine > 0 { openCaption += " · \(WFormat.compact(machine)) auto" }

        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 0) {
                statButton(.sent, label: "Sent", value: WFormat.compact(sent), caption: "last \(store.period.rawValue)")
                stripDivider
                statButton(.opens, label: "Opened", value: WFormat.compact(opens), caption: openCaption)
                stripDivider
                statButton(.clicks, label: "Clicked", value: WFormat.compact(clicks), caption: WFormat.percent(clicks, of: sent))
                stripDivider
                statButton(.replies, label: "Replied", value: WFormat.compact(replies), caption: WFormat.percent(replies, of: sent))
                stripDivider
                AnalyticsStatCell(
                    label: "Bounced",
                    value: WFormat.compact(bounces),
                    caption: WFormat.percent(bounces, of: sent),
                    tone: bounceRate >= 5 ? .rose : (bounceRate >= 2 ? .amber : nil)
                )
            }
            // Cells carry 14pt internal padding; 6 more lands the first
            // column on the shared 20pt gutter.
            .padding(.horizontal, 6)
        }
    }

    private func statButton(_ target: AnalyticsTrendMetric, label: String, value: String, caption: String) -> some View {
        Button {
            withAnimation(.snappy) { metric = target }
        } label: {
            AnalyticsStatCell(label: label, value: value, caption: caption, selected: metric == target)
        }
        .buttonStyle(.plain)
    }

    private var stripDivider: some View {
        Divider().frame(height: 40)
    }

    private var activeRow: some View {
        let stats = store.dashboard?.overallStats
        return Text("\(stats?.activeCampaigns ?? 0) active campaigns · \(stats?.activeAccounts ?? 0) sending accounts")
            .font(.footnote.weight(.medium))
            .monospacedDigit()
            .foregroundStyle(.secondary)
            .padding(.horizontal, 20)
            .padding(.top, 10)
    }

    // MARK: Trend chart

    private var chartPoints: [AnalyticsChartPoint] {
        let trend = store.dashboard?.dailyTrend ?? []
        return trend.enumerated().compactMap { index, point in
            guard let day = AnalyticsDay.parse(point.date) else { return nil }
            return AnalyticsChartPoint(id: index, day: day, value: metric.value(point))
        }
    }

    /// Catmull-Rom line + gradient area, matching the Home trend card.
    private var chartSection: some View {
        let points = chartPoints
        return VStack(alignment: .leading, spacing: 10) {
            EyebrowLabel("\(metric.title) per day")
                .padding(.horizontal, 20)
            if points.count > 1 {
                Chart(points) { point in
                    AreaMark(
                        x: .value("Day", point.day),
                        y: .value(metric.title, point.value)
                    )
                    .interpolationMethod(.catmullRom)
                    .foregroundStyle(
                        LinearGradient(
                            colors: [metric.color.opacity(0.32), metric.color.opacity(0.02)],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )
                    LineMark(
                        x: .value("Day", point.day),
                        y: .value(metric.title, point.value)
                    )
                    .interpolationMethod(.catmullRom)
                    .lineStyle(StrokeStyle(lineWidth: 2.5, lineCap: .round))
                    .foregroundStyle(metric.color)
                }
                .chartXAxis {
                    AxisMarks(values: .automatic(desiredCount: 4)) { _ in
                        AxisGridLine().foregroundStyle(Color(.separator).opacity(0.4))
                        AxisValueLabel(format: .dateTime.month(.abbreviated).day())
                            .font(.system(size: 10, weight: .medium))
                            .foregroundStyle(Color.secondary)
                    }
                }
                .chartYAxis {
                    AxisMarks(position: .trailing, values: .automatic(desiredCount: 3)) { _ in
                        AxisGridLine().foregroundStyle(Color(.separator).opacity(0.4))
                        AxisValueLabel()
                            .font(.system(size: 10))
                            .foregroundStyle(Color.secondary)
                    }
                }
                .frame(height: 180)
                .padding(.horizontal, 20)
            } else {
                Text("Not enough activity to chart yet.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, minHeight: 120)
            }
        }
        .padding(.top, 14)
        .padding(.bottom, 14)
    }

    // MARK: Drawer

    private func drawer(topInset: CGFloat) -> some View {
        AnalyticsSidebar(
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
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) { sidebarOpen = true }
    }

    private func closeSidebar() {
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) {
            sidebarOpen = false
            sidebarDrag = 0
        }
    }
}

// MARK: - Stat strip cell

/// StatCell plus a rate caption and a selected underline (acts as the chart
/// metric filter).
struct AnalyticsStatCell: View {
    let label: String
    let value: String
    let caption: String
    var tone: Tone? = nil
    var selected: Bool = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            EyebrowLabel(label)
            Text(value)
                .font(.system(size: 22, weight: .bold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(tone?.color ?? Color.primary)
                .contentTransition(.numericText())
            Text(caption)
                .font(.caption.weight(.medium))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .contentTransition(.numericText())
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .frame(minWidth: 96, alignment: .leading)
        .overlay(alignment: .bottom) {
            if selected {
                RoundedRectangle(cornerRadius: 1)
                    .fill(WTheme.accent)
                    .frame(height: 2)
                    .padding(.horizontal, 14)
            }
        }
        .contentShape(Rectangle())
    }
}

/// Compact labeled number used inside the section grids.
struct AnalyticsMiniStat: View {
    let label: String
    let value: String
    var tone: Tone? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            EyebrowLabel(label)
            Text(value)
                .font(.system(size: 18, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(tone?.color ?? Color.primary)
                .contentTransition(.numericText())
                .lineLimit(1)
                .minimumScaleFactor(0.75)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
