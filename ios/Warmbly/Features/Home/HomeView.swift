import Charts
import SwiftUI

// MARK: - Routes

enum HomeRoute: Hashable {
    case activity
}

/// How the top of Home carries the sky photo.
enum HomeHeroStyle: String {
    /// Full-bleed photo with the page sliding over it as a sheet.
    case immersive
    /// A large photo card holding the greeting and Remie.
    case card
    /// A tall photo fading into the page.
    case fade

    static var current: HomeHeroStyle {
        #if DEBUG
        if let raw = ProcessInfo.processInfo.environment["WARMBLY_DEBUG_HOME"], let style = HomeHeroStyle(rawValue: raw) {
            return style
        }
        #endif
        return .immersive
    }
}

// MARK: - View

/// Home: the sky photo with the greeting and the week's headline number,
/// Remie one tap away, then the numbers that matter with their trends, what
/// is due today, mailbox health, campaigns and activity.
struct HomeView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.switchTab) private var switchTab
    @Environment(\.colorScheme) private var colorScheme
    @State private var store = HomeStore()
    @State private var path: [HomeRoute] = []
    @State private var appeared = false
    @State private var showMailboxes = false
    @State private var showWorkspaces = false
    @State private var showAnalytics = false
    @State private var showTasks = false
    @State private var showMeetings = false
    @State private var assistantPrompt: AssistantLaunch?
    @State private var scrolled = false

    private let style = HomeHeroStyle.current

    private var canAnalytics: Bool { env.session.can(.viewAnalytics) }
    private var canEmails: Bool { env.session.can(.manageEmails) }
    private var canCampaigns: Bool { env.session.can(.viewCampaigns) }
    private var canAssistant: Bool { env.session.can(.useAI) }
    private var canCRM: Bool { env.session.can(.viewContacts) }

    /// Counts animate up once data has arrived and the page is on screen.
    private var shown: Bool { appeared && store.hasLoaded }

    var body: some View {
        NavigationStack(path: $path) {
            GeometryReader { outer in
                let topInset = outer.safeAreaInsets.top
                let side = Self.sideInsets(outer.safeAreaInsets)
                let wide = outer.size.width >= Self.twoColumnWidth
                ScrollView(showsIndicators: false) {
                    VStack(alignment: .leading, spacing: 0) {
                        hero(topInset: topInset, side: side)
                        sections(wide: wide)
                            .frame(maxWidth: Self.maxContentWidth, alignment: .leading)
                            .frame(maxWidth: .infinity)
                            .padding(.horizontal, 20)
                            .padding(side)
                            .padding(.top, style == .immersive ? 26 : 28)
                            .padding(.bottom, 36)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background {
                                if style == .immersive {
                                    UnevenRoundedRectangle(topLeadingRadius: 30, topTrailingRadius: 30, style: .continuous)
                                        .fill(WTheme.canvas)
                                        .shadow(color: .black.opacity(0.06), radius: 18, y: -6)
                                }
                            }
                            .padding(.top, style == .immersive ? -30 : 0)
                    }
                }
                // Runs the photo and the page under side insets too, like the Duo cover's status strip.
                .ignoresSafeArea(edges: [.top, .horizontal])
                .defaultScrollAnchor(Self.debugAnchor)
                .onScrollGeometryChange(for: Bool.self) { geo in
                    geo.contentOffset.y + geo.contentInsets.top > 380
                } action: { _, past in
                    withAnimation(.easeOut(duration: 0.2)) { scrolled = past }
                }
                .background(WTheme.canvas)
                .overlay(alignment: .top) {
                    // Keeps the status bar legible once the page covers the photo.
                    LinearGradient(colors: [WTheme.canvas, WTheme.canvas.opacity(0)], startPoint: .top, endPoint: .bottom)
                        .frame(height: topInset + 18)
                        .ignoresSafeArea(edges: .top)
                        .opacity(scrolled ? 1 : 0)
                        .allowsHitTesting(false)
                }
                .refreshable { await load() }
            }
            .toolbarVisibility(.hidden, for: .navigationBar)
            .navigationDestination(for: HomeRoute.self) { route in
                switch route {
                case .activity: AuditLogView()
                }
            }
            .navigationDestination(for: EmailAccount.self) { account in
                MailboxDetailView(account: account)
            }
            .fullScreenCover(isPresented: $showMailboxes) {
                MailboxesRootView(onClose: { showMailboxes = false })
            }
            .fullScreenCover(isPresented: $showAnalytics) {
                AnalyticsRootView(onClose: { showAnalytics = false })
            }
            .sheet(isPresented: $showWorkspaces) { MoreWorkspaceSwitcherSheet() }
            .fullScreenCover(isPresented: $showTasks) { CRMTasksView() }
            .fullScreenCover(isPresented: $showMeetings) { CRMMeetingsView() }
            .fullScreenCover(item: $assistantPrompt) { launch in
                AssistantView(page: "home", initialPrompt: launch.prompt)
            }
        }
        .task { await load() }
        .onChange(of: env.realtime.pulse(for: .analytics)) { Task { await load() } }
        .onChange(of: env.realtime.pulse(for: .emailAccounts)) { Task { await load() } }
        .onAppear { appeared = true }
    }

    /// Scripted debug runs can open Home scrolled (WARMBLY_DEBUG_SCROLL=bottom|center).
    private static var debugAnchor: UnitPoint? {
        #if DEBUG
        switch ProcessInfo.processInfo.environment["WARMBLY_DEBUG_SCROLL"] {
        case "bottom": return .bottom
        case "center": return .center
        default: break
        }
        #endif
        return nil
    }

    private func load() async {
        await store.load(env.api, analytics: canAnalytics, emails: canEmails, crm: canCRM)
    }

    // MARK: Hero

    @ViewBuilder
    private func hero(topInset: CGFloat, side: EdgeInsets) -> some View {
        switch style {
        case .immersive:
            VStack(alignment: .leading, spacing: 0) {
                topBar
                greeting.padding(.top, 34)
                headline.padding(.top, 24)
                insights.padding(.top, 18)
                if canAssistant {
                    askBar.padding(.top, 16)
                }
            }
            .frame(maxWidth: Self.maxContentWidth, alignment: .leading)
            .frame(maxWidth: .infinity)
            .padding(.horizontal, 20)
            .padding(side)
            .padding(.top, topInset + 4)
            .padding(.bottom, 58)
            .background { stretchyPhoto(fadeTo: nil) }

        case .card:
            VStack(alignment: .leading, spacing: 0) {
                topBar
                    .frame(maxWidth: Self.maxContentWidth)
                    .frame(maxWidth: .infinity)
                    .padding(.horizontal, 20)
                    .padding(side)
                    .padding(.top, topInset + 4)
                VStack(alignment: .leading, spacing: 0) {
                    greeting
                    headline.padding(.top, 24)
                    if canAssistant {
                        askBar.padding(.top, 26)
                    }
                }
                .padding(20)
                .padding(.top, 8)
                .background {
                    GeometryReader { proxy in
                        photo.frame(width: proxy.size.width, height: proxy.size.height).clipped()
                    }
                    .clipShape(RoundedRectangle(cornerRadius: 32, style: .continuous))
                }
                // The card runs 8pt past the sections on each side, as it does on a phone.
                .frame(maxWidth: Self.maxContentWidth + 16)
                .frame(maxWidth: .infinity)
                .padding(.horizontal, 12)
                .padding(side)
                .padding(.top, 16)
                .scaleEffect(appeared ? 1 : 0.97)
                .opacity(appeared ? 1 : 0)
                .animation(.spring(response: 0.7, dampingFraction: 0.86), value: appeared)
            }

        case .fade:
            VStack(alignment: .leading, spacing: 0) {
                topBar
                greeting.padding(.top, 60)
                headline.padding(.top, 28)
                insights.padding(.top, 18)
                if canAssistant {
                    askBar.padding(.top, 16)
                }
            }
            .frame(maxWidth: Self.maxContentWidth, alignment: .leading)
            .frame(maxWidth: .infinity)
            .padding(.horizontal, 20)
            .padding(side)
            .padding(.top, topInset + 4)
            .padding(.bottom, 8)
            .background { stretchyPhoto(fadeTo: WTheme.canvas) }
        }
    }

    /// Solid secondary text for the photo; translucent ink picks up its texture.
    private let onPhotoSecondary = Color.dynamic(light: 0x3E4756, dark: 0xD5D9E1)

    private var photo: some View {
        Image("SheerSky")
            .resizable()
            .scaledToFill()
            .overlay {
                // A veil behind the text so it reads on the folds; the night photo needs less.
                let veil: [Double] = colorScheme == .dark ? [0.32, 0.08, 0] : [0.62, 0.28, 0.08]
                LinearGradient(
                    colors: [WTheme.canvas.opacity(veil[0]), WTheme.canvas.opacity(veil[1]), WTheme.canvas.opacity(veil[2])],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                )
            }
            .allowsHitTesting(false)
    }

    /// Grows with a pull, drifts slower than the page on scroll.
    private func stretchyPhoto(fadeTo: Color?) -> some View {
        GeometryReader { proxy in
            let y = proxy.frame(in: .scrollView).minY
            let pull = max(0, y)
            photo
                .frame(width: proxy.size.width, height: proxy.size.height + pull + (fadeTo == nil ? 0 : 140))
                .clipped()
                .overlay {
                    if let fadeTo {
                        LinearGradient(
                            stops: [
                                .init(color: fadeTo.opacity(0), location: 0.45),
                                .init(color: fadeTo.opacity(0.85), location: 0.82),
                                .init(color: fadeTo, location: 1),
                            ],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    }
                }
                .offset(y: y > 0 ? -pull : -y * 0.35)
        }
    }

    // MARK: Top bar

    private var topBar: some View {
        HStack(spacing: 10) {
            orgMenu
            Spacer()
            PresenceAvatars()
        }
        .frame(height: 44)
    }

    private var orgMenu: some View {
        Menu {
            ForEach(env.session.memberships) { membership in
                Button {
                    Task { try? await env.session.switchOrganization(membership.organizationID) }
                } label: {
                    if membership.organizationID == env.session.currentOrgID {
                        Label(membership.organization?.name ?? "Workspace", systemImage: "checkmark")
                    } else {
                        Text(membership.organization?.name ?? "Workspace")
                    }
                }
            }
            Divider()
            Button {
                showWorkspaces = true
            } label: {
                Label("Manage workspaces", systemImage: "square.stack")
            }
        } label: {
            HStack(spacing: 9) {
                WAvatar(
                    name: env.session.currentOrg?.name ?? "W",
                    imageURL: env.session.currentOrg?.avatarURL,
                    seed: env.session.currentOrgID ?? "org",
                    size: 30
                )
                Text(env.session.currentOrg?.name ?? "Workspace")
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                Image(systemName: "chevron.down")
                    .font(.system(size: 10.5, weight: .semibold))
                    .foregroundStyle(WTheme.ink.opacity(0.45))
            }
            .padding(.leading, 4)
            .padding(.trailing, 12)
            .frame(height: 38)
            .glassEffect(.regular.interactive(), in: Capsule())
        }
    }

    // MARK: Greeting and headline

    private var greeting: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(Date.now.formatted(.dateTime.weekday(.wide).month(.wide).day()))
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(onPhotoSecondary)
            TwoToneTitle(lead: salutation + ",", title: firstName, size: 40, leadColor: onPhotoSecondary)
        }
        .opacity(appeared ? 1 : 0)
        .offset(y: appeared ? 0 : 10)
        .animation(.spring(response: 0.7, dampingFraction: 0.86), value: appeared)
    }

    private var salutation: String {
        let hour = Calendar.current.component(.hour, from: Date())
        return switch hour {
        case 5 ..< 12: "Good morning"
        case 12 ..< 18: "Good afternoon"
        default: "Good evening"
        }
    }

    private var firstName: String {
        if let name = env.session.user?.firstName, !name.isEmpty { return "\(name)." }
        return "welcome back."
    }

    /// The week in one number, replies when analytics are visible (else
    /// unread mail), with the two numbers behind it.
    private var headline: some View {
        let week = store.thisWeek
        let value = canAnalytics ? week.replies : env.badges.uniboxUnread
        let delta = DeltaLabel.percent(week.replies, store.lastWeek.replies)
        return HStack(alignment: .bottom, spacing: 16) {
            VStack(alignment: .leading, spacing: 2) {
                CountUpText(value: shown ? Double(value) : 0) { WFormat.compact(Int($0.rounded())) }
                    .font(.system(size: 60, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.ink)
                    .animation(.easeOut(duration: 1.1), value: shown)
                HStack(spacing: 8) {
                    Text(canAnalytics ? (value == 1 ? "reply this week" : "replies this week") : "unread in your inbox")
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(onPhotoSecondary)
                    if canAnalytics, store.hasLoaded, delta != nil {
                        DeltaLabel(change: delta)
                    }
                }
            }
            Spacer(minLength: 0)
            if canAnalytics {
                VStack(alignment: .trailing, spacing: 12) {
                    heroStat(Double(week.sent), label: "sent") { WFormat.compact(Int($0.rounded())) }
                    heroStat(week.replyRate ?? 0, label: "reply rate") { String(format: "%.1f%%", $0) }
                }
                .padding(.bottom, 4)
            }
        }
        .opacity(appeared ? 1 : 0)
        .offset(y: appeared ? 0 : 12)
        .animation(.spring(response: 0.7, dampingFraction: 0.86).delay(0.05), value: appeared)
    }

    private func heroStat(_ value: Double, label: String, format: @escaping (Double) -> String) -> some View {
        VStack(alignment: .trailing, spacing: 0) {
            CountUpText(value: shown ? value : 0, format: format)
                .font(.system(size: 22, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(WTheme.ink)
                .animation(.easeOut(duration: 1.1).delay(0.1), value: shown)
            Text(label)
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(onPhotoSecondary)
        }
    }

    /// The week as a few facts, each a tap away from where it is handled.
    private var insights: some View {
        let replyDelta = DeltaLabel.percent(store.thisWeek.replies, store.lastWeek.replies)
        let dueToday = store.tasks.count + store.meetings.count
        let bold: (String) -> Text = { Text($0).fontWeight(.semibold) }
        return ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                if store.issueCount > 0 {
                    HomeInsightPill(symbol: "exclamationmark.triangle.fill", text: Text("\(bold("\(store.issueCount)")) need attention"), tint: WTheme.negative) {
                        showMailboxes = true
                    }
                }
                if canAnalytics, let replyDelta, abs(replyDelta) >= 1 {
                    HomeInsightPill(
                        symbol: replyDelta > 0 ? "arrow.up.right" : "arrow.down.right",
                        text: Text("Replies \(replyDelta > 0 ? "up" : "down") \(bold(String(format: "%.0f%%", abs(replyDelta))))"),
                        tint: replyDelta > 0 ? WTheme.positive : WTheme.negative
                    ) { showAnalytics = true }
                }
                if env.badges.uniboxUnread > 0 {
                    HomeInsightPill(symbol: "tray.full.fill", text: Text("\(bold("\(env.badges.uniboxUnread)")) unread"), tint: WTheme.accent) {
                        switchTab(.inbox)
                    }
                }
                if canEmails, store.warmingCount > 0 {
                    HomeInsightPill(symbol: "flame.fill", text: Text("\(bold("\(store.warmingCount)")) warming up"), tint: Tone.orange.color) {
                        showMailboxes = true
                    }
                }
                if canCRM {
                    HomeInsightPill(
                        symbol: dueToday > 0 ? "checklist" : "checkmark.circle.fill",
                        text: dueToday > 0 ? Text("\(bold("\(dueToday)")) due today") : Text("Nothing due today"),
                        tint: dueToday > 0 ? WTheme.special : WTheme.positive
                    ) { showTasks = true }
                }
            }
            .padding(.horizontal, 20)
        }
        .scrollClipDisabled()
        .padding(.horizontal, -20)
        .opacity(shown ? 1 : 0)
        .offset(y: shown ? 0 : 10)
        .animation(.spring(response: 0.6, dampingFraction: 0.86).delay(0.15), value: shown)
    }

    // MARK: Remie

    private var askBar: some View {
        Button {
            assistantPrompt = AssistantLaunch(prompt: nil)
        } label: {
            HStack(spacing: 12) {
                RemieMark(size: 24)
                Text("Ask Remie anything")
                    .font(.system(size: 16))
                    .foregroundStyle(onPhotoSecondary)
                Spacer(minLength: 8)
                Image(systemName: "arrow.up")
                    .font(.system(size: 14, weight: .bold))
                    .foregroundStyle(WTheme.inkInverse)
                    .frame(width: 36, height: 36)
                    .background(WTheme.ink, in: Circle())
            }
            .padding(.leading, 14)
            .padding(.trailing, 8)
            .frame(height: 54)
            .contentShape(Capsule())
        }
        .buttonStyle(TapScaleStyle())
        .glassEffect(.regular.interactive(), in: Capsule())
        .opacity(appeared ? 1 : 0)
        .offset(y: appeared ? 0 : 14)
        .animation(.spring(response: 0.7, dampingFraction: 0.86).delay(0.1), value: appeared)
    }

    private static let starters: [(title: String, symbol: String, prompt: String)] = [
        ("Review my week", "chart.line.uptrend.xyaxis", "How are my campaigns doing this week?"),
        ("Mailbox health", "heart.text.square", "Which mailboxes need attention?"),
        ("Draft a follow-up", "arrowshape.turn.up.left", "Draft a follow-up for my latest reply"),
        ("Warm leads", "flame", "Which contacts replied positively in the last week?"),
    ]

    private var starterChips: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(Self.starters, id: \.prompt) { starter in
                    Button {
                        assistantPrompt = AssistantLaunch(prompt: starter.prompt)
                    } label: {
                        HStack(spacing: 7) {
                            Image(systemName: starter.symbol)
                                .font(.system(size: 12.5, weight: .medium))
                                .foregroundStyle(WTheme.inkSecondary)
                            Text(starter.title)
                                .font(.system(size: 14, weight: .medium))
                                .foregroundStyle(WTheme.ink)
                        }
                        .padding(.horizontal, 14)
                        .frame(height: 36)
                        .background(WTheme.surface, in: Capsule())
                    }
                    .buttonStyle(TapScaleStyle())
                }
            }
            .padding(.horizontal, 20)
        }
        .scrollClipDisabled()
        .padding(.horizontal, -20)
    }

    // MARK: Sections

    /// Past this width Home reads as a dashboard: numbers on the left, the day on the right.
    private static let twoColumnWidth: CGFloat = 840
    private static let maxContentWidth: CGFloat = 1180

    /// Only the side insets: the top one is handled by the hero's own padding.
    private static func sideInsets(_ insets: EdgeInsets) -> EdgeInsets {
        EdgeInsets(top: 0, leading: insets.leading, bottom: 0, trailing: insets.trailing)
    }

    private func sections(wide: Bool) -> some View {
        VStack(alignment: .leading, spacing: 34) {
            if canAssistant {
                starterChips.reveal(shown, 0)
            }
            if store.hasLoaded {
                if wide, hasPerformanceSections {
                    HStack(alignment: .top, spacing: 36) {
                        VStack(alignment: .leading, spacing: 34) { performanceSections }
                            .frame(maxWidth: .infinity, alignment: .leading)
                        VStack(alignment: .leading, spacing: 34) { daySections }
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                } else {
                    performanceSections
                    daySections
                }
            } else {
                loadingCards
            }
        }
    }

    /// Without these the left column would stand empty, so Home stays one column.
    private var hasPerformanceSections: Bool {
        canAnalytics || (canEmails && store.warmingCount > 0)
    }

    @ViewBuilder
    private var performanceSections: some View {
        if canAnalytics {
            kpiGrid.reveal(shown, 1)
        }
        if canAnalytics, store.hasTrend {
            trendSection.reveal(shown, 2)
        }
        if canEmails, store.warmingCount > 0 {
            warmupSection.reveal(shown, 3)
        }
    }

    @ViewBuilder
    private var daySections: some View {
        if canCRM {
            todaySection.reveal(shown, 4)
        }
        if canEmails {
            mailboxSection.reveal(shown, 4)
        }
        if canCampaigns, let top = store.dashboard?.topCampaigns, !top.isEmpty {
            campaignSection(top).reveal(shown, 5)
        }
        if canAnalytics, let activity = store.dashboard?.recentActivity, !activity.isEmpty {
            activitySection(activity).reveal(shown, 6)
        }
    }

    private var loadingCards: some View {
        VStack(alignment: .leading, spacing: 18) {
            ForEach(0 ..< 4, id: \.self) { _ in
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .fill(WTheme.fill)
                    .frame(height: 74)
            }
        }
        .skeletonShimmer()
    }

    // MARK: KPIs

    private struct KPI {
        let label: String
        let value: Double
        let format: (Double) -> String
        let delta: Double?
        let points: Bool
        let series: [Double]
        let tint: Color
    }

    private var kpis: [KPI] {
        let now = store.thisWeek, before = store.lastWeek
        let days = store.days
        func rate(_ part: (TrendDay) -> Int) -> [Double] {
            days.map { $0.sent > 0 ? Double(part($0)) / Double($0.sent) * 100 : 0 }
        }
        let pct: (Double) -> String = { String(format: "%.1f%%", $0) }
        return [
            KPI(label: "Sent", value: Double(now.sent), format: { WFormat.compact(Int($0.rounded())) },
                delta: DeltaLabel.percent(now.sent, before.sent), points: false,
                series: days.map { Double($0.sent) }, tint: WTheme.accent),
            KPI(label: "Open rate", value: now.openRate ?? 0, format: pct,
                delta: DeltaLabel.points(now.openRate, before.openRate), points: true,
                series: rate(\.opens), tint: WTheme.special),
            KPI(label: "Reply rate", value: now.replyRate ?? 0, format: pct,
                delta: DeltaLabel.points(now.replyRate, before.replyRate), points: true,
                series: rate(\.replies), tint: WTheme.positive),
            KPI(label: "Click rate", value: now.clickRate ?? 0, format: pct,
                delta: DeltaLabel.points(now.clickRate, before.clickRate), points: true,
                series: rate(\.clicks), tint: Tone.orange.color),
        ]
    }

    /// Four open numbers with their fortnight, no tiles: rules between them.
    private var kpiGrid: some View {
        let items = kpis
        return VStack(alignment: .leading, spacing: 14) {
            AirSectionHeader(title: "Last 7 days", actionTitle: "Analytics") { showAnalytics = true }
            VStack(spacing: 0) {
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
            }
            factsLine
                .padding(.top, 4)
        }
    }

    private func kpiCell(_ kpi: KPI, index: Int) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Circle().fill(kpi.tint).frame(width: 6, height: 6)
                Text(kpi.label)
                    .font(.system(size: 13.5, weight: .medium))
                    .foregroundStyle(WTheme.inkSecondary)
                Spacer(minLength: 4)
                DeltaLabel(change: kpi.delta, points: kpi.points)
            }
            CountUpText(value: shown ? kpi.value : 0, format: kpi.format)
                .font(.system(size: 28, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(WTheme.ink)
                .animation(.easeOut(duration: 1).delay(Double(index) * 0.05), value: shown)
            Sparkline(values: kpi.series, color: kpi.tint, delay: 0.25 + Double(index) * 0.08)
                .frame(height: 30)
        }
        .padding(.vertical, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// The rest of the week in one readable line.
    private var factsLine: some View {
        let stats = store.dashboard?.overallStats
        var parts: [Text] = []
        if let campaigns = stats?.activeCampaigns {
            parts.append(Text("\(Text("\(campaigns)").foregroundStyle(WTheme.ink).fontWeight(.semibold)) active \(campaigns == 1 ? "campaign" : "campaigns")"))
        }
        if canEmails {
            let total = store.accountTotal ?? store.accounts.count
            parts.append(Text("\(Text("\(total)").foregroundStyle(WTheme.ink).fontWeight(.semibold)) \(total == 1 ? "mailbox" : "mailboxes")"))
            parts.append(Text("\(Text("\(store.warmingCount)").foregroundStyle(WTheme.ink).fontWeight(.semibold)) warming"))
        }
        if let bounce = stats?.bounceRate {
            parts.append(Text("\(Text(String(format: "%.1f%%", bounce)).foregroundStyle(WTheme.ink).fontWeight(.semibold)) bounced"))
        }
        let line = parts.enumerated().reduce(Text("")) { acc, item in
            item.offset == 0 ? item.element : Text("\(acc)  ·  \(item.element)")
        }
        return line
            .font(.system(size: 13.5))
            .foregroundStyle(WTheme.inkSecondary)
            .lineSpacing(3)
    }

    // MARK: Today

    private var todaySection: some View {
        VStack(alignment: .leading, spacing: 10) {
            AirSectionHeader(title: "Today", actionTitle: "Tasks") { showTasks = true }
            if store.tasks.isEmpty, store.meetings.isEmpty {
                HStack(spacing: 10) {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 17))
                        .foregroundStyle(WTheme.positive)
                    Text("Nothing due today.")
                        .font(.system(size: 15))
                        .foregroundStyle(WTheme.inkSecondary)
                }
                .padding(.vertical, 8)
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(store.meetings.prefix(3))) { meeting in
                        Button { showMeetings = true } label: { meetingRow(meeting) }
                            .buttonStyle(TapScaleStyle())
                    }
                    ForEach(Array(store.tasks.prefix(5 - min(3, store.meetings.count)))) { task in
                        Button { showTasks = true } label: { taskRow(task) }
                            .buttonStyle(TapScaleStyle())
                    }
                }
            }
        }
    }

    private func agendaRow(time: String, timeColor: Color, tint: Color, title: String, subtitle: String, symbol: String) -> some View {
        HStack(spacing: 12) {
            Text(time)
                .font(.system(size: 13, weight: .semibold))
                .monospacedDigit()
                .foregroundStyle(timeColor)
                .frame(width: 50, alignment: .leading)
            Capsule().fill(tint).frame(width: 3, height: 36)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                Text(subtitle)
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            Image(systemName: symbol)
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(WTheme.inkTertiary)
        }
        .padding(.vertical, 9)
        .contentShape(Rectangle())
    }

    private func meetingRow(_ meeting: CRMMeeting) -> some View {
        agendaRow(
            time: meeting.scheduledFor?.formatted(date: .omitted, time: .shortened) ?? "Today",
            timeColor: WTheme.ink,
            tint: WTheme.special,
            title: meeting.displayTitle,
            subtitle: [meeting.inviteeLabel, meeting.sourceLabel].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "),
            symbol: "video"
        )
    }

    private func taskRow(_ task: CRMTask) -> some View {
        let overdue = task.isOverdue && !(task.dueDate.map(Calendar.current.isDateInToday) ?? false)
        let time: String = {
            guard let due = task.dueDate else { return "Today" }
            if overdue { return due.formatted(.dateTime.month(.abbreviated).day()) }
            return due.formatted(date: .omitted, time: .shortened)
        }()
        return agendaRow(
            time: time,
            timeColor: overdue ? WTheme.negative : WTheme.ink,
            tint: overdue ? WTheme.negative : WTheme.accent,
            title: task.title,
            subtitle: overdue ? "Overdue task" : (task.type.map { $0.capitalized } ?? "Task"),
            symbol: "checklist"
        )
    }

    // MARK: Mailboxes

    private var healthCounts: (healthy: Int, watch: Int, failing: Int) {
        if let health = store.dashboard?.accountHealth, (health.totalAccounts ?? 0) > 0 {
            return (health.healthyAccounts ?? 0, health.warningAccounts ?? 0, health.errorAccounts ?? 0)
        }
        let all = store.accounts.compactMap { store.statuses[$0.id]?.health?.status }
        return (all.count { $0 != "warning" && $0 != "error" }, all.count { $0 == "warning" }, all.count { $0 == "error" })
    }

    private var mailboxSection: some View {
        let counts = healthCounts
        let total = max(1, counts.healthy + counts.watch + counts.failing)
        return VStack(alignment: .leading, spacing: 14) {
            AirSectionHeader(title: "Mailboxes", actionTitle: "All \(store.accountTotal ?? store.accounts.count)") {
                showMailboxes = true
            }
            if store.accounts.isEmpty {
                Button {
                    showMailboxes = true
                } label: {
                    HStack(spacing: 12) {
                        IconTile(symbol: "plus", tone: .slate)
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Connect your first mailbox")
                                .font(.system(size: 15.5, weight: .medium))
                                .foregroundStyle(WTheme.ink)
                            Text("Start warming and sending in minutes.")
                                .font(.system(size: 13.5))
                                .foregroundStyle(WTheme.inkSecondary)
                        }
                        Spacer()
                        Image(systemName: "arrow.right")
                            .font(.system(size: 13, weight: .medium))
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                }
                .buttonStyle(TapScaleStyle())
            } else {
                GeometryReader { proxy in
                    let width = proxy.size.width - 8
                    HStack(spacing: 4) {
                        ForEach(Array([
                            (counts.healthy, WTheme.positive),
                            (counts.watch, WTheme.warning),
                            (counts.failing, WTheme.negative),
                        ].enumerated()), id: \.offset) { _, part in
                            if part.0 > 0 {
                                Capsule()
                                    .fill(part.1)
                                    .frame(width: shown ? max(8, width * CGFloat(part.0) / CGFloat(total)) : 8)
                            }
                        }
                        Spacer(minLength: 0)
                    }
                    .animation(.spring(response: 0.9, dampingFraction: 0.9).delay(0.2), value: shown)
                }
                .frame(height: 8)

                HStack(spacing: 16) {
                    healthLegend(counts.healthy, "healthy", WTheme.positive)
                    if counts.watch > 0 { healthLegend(counts.watch, "watch", WTheme.warning) }
                    if counts.failing > 0 { healthLegend(counts.failing, "failing", WTheme.negative) }
                    healthLegend(store.warmingCount, "warming", Tone.orange.color)
                }

                let flagged = store.accountsByUrgency.filter { store.statuses[$0.id]?.health?.hasIssue == true }
                if flagged.isEmpty {
                    HStack(spacing: 10) {
                        Image(systemName: "checkmark.seal.fill")
                            .font(.system(size: 17))
                            .foregroundStyle(WTheme.positive)
                        Text("Every mailbox is healthy.")
                            .font(.system(size: 15))
                            .foregroundStyle(WTheme.inkSecondary)
                    }
                    .padding(.top, 4)
                }
                VStack(spacing: 0) {
                    let rows = Array(flagged.prefix(3))
                    ForEach(Array(rows.enumerated()), id: \.element.id) { index, account in
                        NavigationLink(value: account) {
                            homeMailboxRow(account)
                        }
                        .buttonStyle(TapScaleStyle())
                        if index < rows.count - 1 {
                            Hairline().padding(.leading, 50)
                        }
                    }
                }
                .padding(.top, 2)
            }
        }
    }

    private func healthLegend(_ count: Int, _ label: String, _ color: Color) -> some View {
        HStack(spacing: 6) {
            Circle().fill(color).frame(width: 7, height: 7)
            Text("\(Text("\(count)").foregroundStyle(WTheme.ink).fontWeight(.semibold)) \(label)")
                .font(.system(size: 13.5))
                .foregroundStyle(WTheme.inkSecondary)
        }
    }

    private func homeMailboxRow(_ account: EmailAccount) -> some View {
        let health = store.statuses[account.id]?.health
        return HStack(spacing: 12) {
            MailboxAvatar(email: account.email, avatarURL: account.avatarURL, brand: account.brand, seed: account.id, size: 36, ring: WTheme.raised)
            VStack(alignment: .leading, spacing: 3) {
                Text(account.email)
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(WTheme.ink)
                    // Two lines, so a long address still fits a narrower two-column layout.
                    .lineLimit(2)
                HStack(spacing: 5) {
                    if account.isWarmingActive {
                        LiveDot(color: Tone.orange.color, size: 5, pulsing: false)
                        Text("Warming")
                            .foregroundStyle(Tone.orange.color)
                    } else {
                        Text(account.providerLabel)
                            .foregroundStyle(WTheme.inkSecondary)
                    }
                }
                .font(.system(size: 13, weight: .medium))
            }
            Spacer()
            if let health {
                HealthRing(score: health.score, tone: health.tone, size: 32)
            } else {
                Image(systemName: "chevron.right")
                    .font(.system(size: 12, weight: .medium))
                    .foregroundStyle(WTheme.inkTertiary)
            }
        }
        .padding(.vertical, 11)
        .contentShape(Rectangle())
    }


    // MARK: Trend chart

    private var trendSection: some View {
        VStack(alignment: .leading, spacing: 14) {
            AirSectionHeader(title: "Trends", actionTitle: "Analytics") { showAnalytics = true }
            TrendChart(days: store.days, shown: shown)
        }
    }

    // MARK: Warmup

    private var warmupSection: some View {
        let warming = store.accounts.filter(\.isWarmingActive)
        let sentToday = warming.reduce(0) { $0 + (store.statuses[$1.id]?.dailyUsage?.warmupSent ?? 0) }
        return VStack(alignment: .leading, spacing: 6) {
            AirSectionHeader(title: "Warming up", actionTitle: "Mailboxes") { showMailboxes = true }
            Text("\(Text("\(warming.count)").foregroundStyle(WTheme.ink).fontWeight(.semibold)) \(warming.count == 1 ? "inbox" : "inboxes") building reputation  ·  \(Text("\(sentToday)").foregroundStyle(WTheme.ink).fontWeight(.semibold)) warmup emails today")
                .font(.system(size: 13.5))
                .foregroundStyle(WTheme.inkSecondary)
                .padding(.bottom, 4)
            VStack(spacing: 0) {
                let rows = Array(warming.prefix(4))
                ForEach(Array(rows.enumerated()), id: \.element.id) { index, account in
                    NavigationLink(value: account) {
                        HomeWarmupRow(account: account, status: store.statuses[account.id], shown: shown, delay: Double(index) * 0.1)
                    }
                    .buttonStyle(TapScaleStyle())
                    if index < rows.count - 1 {
                        Hairline().padding(.leading, 48)
                    }
                }
            }
        }
    }

    private func legendDot(color: Color, label: String) -> some View {
        HStack(spacing: 6) {
            Circle().fill(color).frame(width: 7, height: 7)
            Text(label)
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(WTheme.inkSecondary)
        }
    }

    private func percentText(_ value: Double?) -> String {
        guard let value else { return "–" }
        return String(format: "%.0f%%", value)
    }

    // MARK: Campaigns

    private func campaignSection(_ campaigns: [AnalyticsTopCampaign]) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            AirSectionHeader(title: "Campaigns", actionTitle: "See all") {
                switchTab(.campaigns)
            }
            VStack(spacing: 0) {
                ForEach(Array(campaigns.prefix(3).enumerated()), id: \.element.id) { index, campaign in
                    Button {
                        switchTab(.campaigns)
                    } label: {
                        homeCampaignRow(campaign)
                    }
                    .buttonStyle(TapScaleStyle())
                    if index < min(campaigns.count, 3) - 1 {
                        Hairline()
                    }
                }
            }
        }
    }

    private func homeCampaignRow(_ campaign: AnalyticsTopCampaign) -> some View {
        let tone = campaignTone(campaign.status)
        return HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 7) {
                    LiveDot(color: tone.color, size: 6, pulsing: tone == .emerald)
                    Text(campaign.name ?? "Campaign")
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                }
                Text("\(WFormat.compact(campaign.emailsSent ?? 0)) sent")
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.inkSecondary)
                    .padding(.leading, 13)
            }
            Spacer()
            HStack(spacing: 16) {
                miniRate(percentText(campaign.openRate), label: "open")
                miniRate(percentText(campaign.replyRate), label: "reply")
            }
        }
        .padding(.vertical, 13)
        .contentShape(Rectangle())
    }

    private func miniRate(_ value: String, label: String) -> some View {
        VStack(alignment: .trailing, spacing: 2) {
            Text(value)
                .font(.system(size: 15, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(WTheme.ink)
            Text(label)
                .font(.system(size: 11.5, weight: .medium))
                .foregroundStyle(WTheme.inkTertiary)
        }
    }

    private func campaignTone(_ status: String?) -> Tone {
        switch status {
        case "running", "active": .emerald
        case "paused": .amber
        case "finished", "completed": .sky
        default: .slate
        }
    }

    // MARK: Activity

    /// A timeline: one rail, a tinted node per event.
    private func activitySection(_ items: [AnalyticsActivityItem]) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            AirSectionHeader(title: "Latest activity", actionTitle: "Analytics") {
                showAnalytics = true
            }
            VStack(spacing: 0) {
                let shown = Array(items.prefix(5))
                ForEach(Array(shown.enumerated()), id: \.offset) { index, item in
                    activityRow(item, first: index == 0, last: index == shown.count - 1)
                }
            }
        }
    }

    private func activityRow(_ item: AnalyticsActivityItem, first: Bool, last: Bool) -> some View {
        let meta = activityMeta(item.type)
        return HStack(alignment: .center, spacing: 12) {
            ZStack {
                VStack(spacing: 0) {
                    Rectangle().fill(first ? .clear : WTheme.hairlineStrong).frame(width: 1)
                    Rectangle().fill(last ? .clear : WTheme.hairlineStrong).frame(width: 1)
                }
                Image(systemName: meta.symbol)
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(meta.tone.color)
                    .frame(width: 26, height: 26)
                    .background(meta.tone.background, in: Circle())
                    .background(WTheme.canvas, in: Circle())
            }
            .frame(width: 26)
            VStack(alignment: .leading, spacing: 2) {
                Text(meta.title)
                    .font(.system(size: 14.5, weight: .medium))
                    .foregroundStyle(WTheme.ink)
                Text([item.contactEmail, item.campaignName].compactMap(\.self).joined(separator: " · "))
                    .font(.system(size: 12.5))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
            }
            .padding(.vertical, 10)
            Spacer()
            if let time = item.timestamp {
                Text(WFormat.relative(time))
                    .font(.system(size: 12.5))
                    .foregroundStyle(WTheme.inkTertiary)
            }
        }
    }

    private func activityMeta(_ type: String?) -> (symbol: String, tone: Tone, title: String) {
        switch type {
        case "open", "opened": ("envelope.open.fill", .sky, "Email opened")
        case "click", "clicked": ("cursorarrow.click.2", .indigo, "Link clicked")
        case "reply", "replied": ("arrowshape.turn.up.left.fill", .emerald, "New reply")
        case "bounce", "bounced": ("exclamationmark.triangle.fill", .rose, "Email bounced")
        case "send", "sent": ("paperplane.fill", .sky, "Email sent")
        case "unsubscribe", "unsubscribed": ("person.fill.xmark", .amber, "Unsubscribed")
        default: ("sparkles", .slate, (type ?? "Event").capitalized)
        }
    }
}

/// One way into the assistant from Home: blank, or with a starter prompt.
struct AssistantLaunch: Identifiable {
    let id = UUID()
    let prompt: String?
}
