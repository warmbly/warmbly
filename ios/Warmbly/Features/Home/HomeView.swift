import Charts
import SwiftUI

// MARK: - Store

/// Home pulse: the week's dashboard analytics plus the mailbox fleet, loaded
/// side by side. Sections degrade gracefully when a permission is missing.
@MainActor
@Observable
final class HomeStore {
    private(set) var dashboard: DashboardAnalytics?
    private(set) var accounts: [EmailAccount] = []
    private(set) var statuses: [String: AccountAnalytics] = [:]
    private(set) var accountTotal: Int?
    private(set) var hasLoaded = false

    var warmingCount: Int { accounts.filter(\.isWarmingActive).count }
    var issueCount: Int {
        accounts.filter { statuses[$0.id]?.health?.hasIssue == true }.count
    }

    func load(_ api: APIClient, analytics: Bool, emails: Bool) async {
        async let dashboardTask: DashboardAnalytics? = analytics
            ? try? api.get("analytics/dashboard", query: ["period": "week"])
            : nil
        async let accountsTask: ListResponse<EmailAccount>? = emails
            ? try? api.get("emails", query: ["limit": "200"])
            : nil
        async let statusTask: ListResponse<AccountAnalytics>? = analytics
            ? try? api.get("analytics/accounts")
            : nil

        let (dash, accountPage, statusPage) = await (dashboardTask, accountsTask, statusTask)
        withAnimation(.snappy) {
            dashboard = dash
            if let accountPage {
                accounts = accountPage.data
                accountTotal = (accountPage.pagination?.total).map(Int.init) ?? accountPage.data.count
            }
            if let statusPage {
                statuses = Dictionary(statusPage.data.map { ($0.id, $0) }, uniquingKeysWith: { _, latest in latest })
            }
            hasLoaded = true
        }
    }
}

// MARK: - Routes

enum HomeRoute: Hashable {
    case activity
}

// MARK: - View

/// Home tab: the auth screen's sky carried into the app. A gradient hero with
/// greeting + live stats, and a white sheet of glanceable cards below.
struct HomeView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.switchTab) private var switchTab
    @State private var store = HomeStore()
    @State private var path: [HomeRoute] = []
    @State private var appeared = false
    @State private var showMailboxes = false
    @State private var showAnalytics = false
    @State private var showAssistant = false

    private var canAnalytics: Bool { env.session.can(.viewAnalytics) }
    private var canEmails: Bool { env.session.can(.manageEmails) }
    private var canCampaigns: Bool { env.session.can(.viewCampaigns) }
    private var canAssistant: Bool { env.session.can(.useAI) }

    var body: some View {
        NavigationStack(path: $path) {
            GeometryReader { proxy in
                ZStack(alignment: .top) {
                    SkyBackdrop()
                        .ignoresSafeArea()
                    ScrollView(showsIndicators: false) {
                        VStack(spacing: 0) {
                            hero
                            sheet
                                .frame(minHeight: proxy.size.height - 190, alignment: .top)
                        }
                    }
                    .refreshable { await load() }
                }
            }
            .toolbarVisibility(.hidden, for: .navigationBar)
            .navigationDestination(for: HomeRoute.self) { route in
                switch route {
                case .activity: AuditLogView()
                }
            }
            // Mailboxes and analytics are drawer browsers; like the CRM
            // browsers they own their chrome as covers instead of fighting
            // the push back gestures.
            .fullScreenCover(isPresented: $showMailboxes) {
                MailboxesRootView(onClose: { showMailboxes = false })
            }
            .fullScreenCover(isPresented: $showAnalytics) {
                AnalyticsRootView(onClose: { showAnalytics = false })
            }
            .fullScreenCover(isPresented: $showAssistant) {
                AssistantView(page: "home")
            }
            .navigationDestination(for: EmailAccount.self) { account in
                MailboxDetailView(account: account)
            }
        }
        .task { await load() }
        .onChange(of: env.realtime.pulse(for: .analytics)) { Task { await load() } }
        .onChange(of: env.realtime.pulse(for: .emailAccounts)) { Task { await load() } }
        .onAppear {
            withAnimation(.spring(response: 0.7, dampingFraction: 0.85)) { appeared = true }
        }
    }

    private func load() async {
        await store.load(env.api, analytics: canAnalytics, emails: canEmails)
    }

    // MARK: Hero

    private var hero: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack(alignment: .center, spacing: 12) {
                VStack(alignment: .leading, spacing: 3) {
                    Text(Date.now.formatted(.dateTime.weekday(.wide).month(.wide).day()).uppercased())
                        .font(.system(size: 11, weight: .semibold))
                        .tracking(1.3)
                        .foregroundStyle(.white.opacity(0.65))
                    Text(greeting)
                        .font(.system(size: 28, weight: .bold))
                        .foregroundStyle(.white)
                        .lineLimit(1)
                        .minimumScaleFactor(0.7)
                }
                Spacer()
                if canAssistant {
                    assistantButton
                }
                PresenceAvatars()
                orgMenu
            }
            statChips
        }
        .padding(.horizontal, 20)
        .padding(.top, 6)
        .padding(.bottom, 22)
        .opacity(appeared ? 1 : 0)
        .offset(y: appeared ? 0 : 10)
    }

    private var greeting: String {
        let hour = Calendar.current.component(.hour, from: Date())
        let salutation: String = switch hour {
        case 5 ..< 12: "Good morning"
        case 12 ..< 18: "Good afternoon"
        default: "Good evening"
        }
        if let name = env.session.user?.firstName, !name.isEmpty {
            return "\(salutation), \(name)"
        }
        return salutation
    }

    private var assistantButton: some View {
        Button {
            showAssistant = true
        } label: {
            Image(systemName: "sparkles")
                .font(.system(size: 16, weight: .semibold))
                .foregroundStyle(.white)
                .frame(width: 38, height: 38)
                .background(.white.opacity(0.16), in: Circle())
                .overlay(Circle().strokeBorder(.white.opacity(0.25), lineWidth: 1))
        }
        .buttonStyle(TapScaleStyle())
        .accessibilityLabel("AI assistant")
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
                switchTab(.more)
            } label: {
                Label("Workspace settings", systemImage: "gearshape")
            }
        } label: {
            WAvatar(
                name: env.session.currentOrg?.name ?? "W",
                imageURL: env.session.currentOrg?.avatarURL,
                seed: env.session.currentOrgID ?? "org",
                size: 38
            )
            .overlay(Circle().strokeBorder(.white.opacity(0.55), lineWidth: 1.5))
        }
    }

    @ViewBuilder
    private var statChips: some View {
        let stats = store.dashboard?.overallStats
        HStack(spacing: 10) {
            if canAnalytics, let stats {
                AirStatChip(
                    value: WFormat.compact(stats.totalEmailsSent ?? 0),
                    label: "Sent this week",
                    symbol: "paperplane.fill"
                )
                AirStatChip(
                    value: percentText(stats.openRate),
                    label: "Open rate",
                    symbol: "envelope.open.fill"
                )
                AirStatChip(
                    value: WFormat.compact(stats.totalReplies ?? 0),
                    label: "Replies",
                    symbol: "arrowshape.turn.up.left.fill"
                )
            } else {
                AirStatChip(
                    value: "\(store.accountTotal ?? store.accounts.count)",
                    label: "Mailboxes",
                    symbol: "envelope.fill"
                )
                AirStatChip(
                    value: "\(store.warmingCount)",
                    label: "Warming now",
                    symbol: "flame.fill"
                )
                AirStatChip(
                    value: "\(env.badges.uniboxUnread)",
                    label: "Unread",
                    symbol: "tray.fill"
                )
            }
        }
    }

    private func percentText(_ value: Double?) -> String {
        guard let value else { return "–" }
        return String(format: "%.0f%%", value)
    }

    // MARK: Sheet

    private var sheet: some View {
        VStack(alignment: .leading, spacing: 24) {
            if store.hasLoaded {
                if store.issueCount > 0 {
                    attentionCard
                }
                if canEmails {
                    mailboxSection
                }
                if canAnalytics, let trend = store.dashboard?.dailyTrend,
                   trend.count(where: { ($0.sent ?? 0) > 0 || ($0.opens ?? 0) > 0 }) >= 2 {
                    trendSection(trend)
                }
                if canCampaigns, let top = store.dashboard?.topCampaigns, !top.isEmpty {
                    campaignSection(top)
                }
                if canAnalytics, let activity = store.dashboard?.recentActivity, !activity.isEmpty {
                    activitySection(activity)
                }
            } else {
                loadingCards
            }
            Spacer(minLength: 0)
        }
        .padding(20)
        .padding(.bottom, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            UnevenRoundedRectangle(topLeadingRadius: 30, topTrailingRadius: 30, style: .continuous)
                .fill(Color(.systemGroupedBackground))
                .padding(.bottom, -600)
                .ignoresSafeArea(edges: .bottom)
                .shadow(color: .black.opacity(0.12), radius: 22, y: -4)
        )
    }

    private var loadingCards: some View {
        VStack(spacing: 16) {
            ForEach(0 ..< 3, id: \.self) { _ in
                RoundedRectangle(cornerRadius: 22, style: .continuous)
                    .fill(Color(.secondarySystemGroupedBackground))
                    .frame(height: 120)
            }
        }
        .redacted(reason: .placeholder)
        .padding(.top, 4)
    }

    // MARK: Attention

    private var attentionCard: some View {
        Button {
            showMailboxes = true
        } label: {
            HStack(spacing: 12) {
                IconTile(symbol: "exclamationmark.triangle.fill", tone: .rose)
                VStack(alignment: .leading, spacing: 2) {
                    Text(store.issueCount == 1 ? "1 mailbox needs attention" : "\(store.issueCount) mailboxes need attention")
                        .font(.body.weight(.semibold))
                        .foregroundStyle(.primary)
                    Text("Health checks flagged delivery issues.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Image(systemName: "chevron.right")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(.tertiary)
            }
            .airCard()
        }
        .buttonStyle(TapScaleStyle())
    }

    // MARK: Mailboxes

    private var mailboxSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            AirSectionHeader(title: "Mailboxes", actionTitle: "All \(store.accountTotal ?? store.accounts.count)") {
                showMailboxes = true
            }
            if store.accounts.isEmpty {
                Button {
                    showMailboxes = true
                } label: {
                    HStack(spacing: 12) {
                        IconTile(symbol: "plus", tone: .sky)
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Connect your first mailbox")
                                .font(.body.weight(.semibold))
                                .foregroundStyle(.primary)
                            Text("Start warming and sending in minutes.")
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        Image(systemName: "chevron.right")
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundStyle(.tertiary)
                    }
                    .airCard()
                }
                .buttonStyle(TapScaleStyle())
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(store.accounts.prefix(3).enumerated()), id: \.element.id) { index, account in
                        NavigationLink(value: account) {
                            homeMailboxRow(account)
                        }
                        .buttonStyle(TapScaleStyle())
                        if index < min(store.accounts.count, 3) - 1 {
                            Divider().padding(.leading, 62)
                        }
                    }
                }
                .airCard(padding: 6)
            }
        }
    }

    private func homeMailboxRow(_ account: EmailAccount) -> some View {
        let health = store.statuses[account.id]?.health
        return HStack(spacing: 12) {
            WAvatar(name: account.email, seed: account.id, size: 40)
            VStack(alignment: .leading, spacing: 2) {
                Text(account.email)
                    .font(.body.weight(.medium))
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                HStack(spacing: 5) {
                    if account.isWarmingActive {
                        Image(systemName: "flame.fill")
                            .font(.system(size: 10))
                            .foregroundStyle(Tone.orange.color)
                        Text("Warming")
                            .foregroundStyle(Tone.orange.color)
                    } else {
                        Text(account.providerLabel)
                            .foregroundStyle(.secondary)
                    }
                }
                .font(.footnote.weight(.medium))
            }
            Spacer()
            if let health {
                HealthRing(score: health.score, tone: health.tone)
            } else {
                Image(systemName: "chevron.right")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 10)
        .contentShape(Rectangle())
    }

    // MARK: Trend chart

    private func trendSection(_ trend: [AnalyticsTrendPoint]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            AirSectionHeader(title: "This week", actionTitle: "Analytics") {
                showAnalytics = true
            }
            VStack(alignment: .leading, spacing: 14) {
                HStack(spacing: 18) {
                    legendDot(color: WTheme.accent, label: "Sent")
                    legendDot(color: WTheme.positive, label: "Opens")
                    Spacer()
                }
                Chart {
                    ForEach(Array(trend.enumerated()), id: \.offset) { _, point in
                        AreaMark(
                            x: .value("Day", trendDay(point)),
                            y: .value("Sent", point.sent ?? 0)
                        )
                        .foregroundStyle(
                            LinearGradient(
                                colors: [WTheme.accent.opacity(0.32), WTheme.accent.opacity(0.02)],
                                startPoint: .top,
                                endPoint: .bottom
                            )
                        )
                        .interpolationMethod(.catmullRom)
                        LineMark(
                            x: .value("Day", trendDay(point)),
                            y: .value("Sent", point.sent ?? 0)
                        )
                        .foregroundStyle(WTheme.accent)
                        .lineStyle(StrokeStyle(lineWidth: 2.5, lineCap: .round))
                        .interpolationMethod(.catmullRom)
                        LineMark(
                            x: .value("Day", trendDay(point)),
                            y: .value("Opens", point.opens ?? 0),
                            series: .value("Series", "Opens")
                        )
                        .foregroundStyle(WTheme.positive)
                        .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, dash: [4, 5]))
                        .interpolationMethod(.catmullRom)
                    }
                }
                .chartXAxis {
                    AxisMarks { _ in
                        AxisValueLabel()
                            .font(.system(size: 10, weight: .medium))
                            .foregroundStyle(Color.secondary)
                    }
                }
                .chartYAxis {
                    AxisMarks { _ in
                        AxisGridLine().foregroundStyle(Color(.separator).opacity(0.4))
                        AxisValueLabel()
                            .font(.system(size: 10))
                            .foregroundStyle(Color.secondary)
                    }
                }
                .frame(height: 150)
            }
            .airCard()
        }
    }

    private func legendDot(color: Color, label: String) -> some View {
        HStack(spacing: 5) {
            Circle().fill(color).frame(width: 7, height: 7)
            Text(label)
                .font(.footnote.weight(.medium))
                .foregroundStyle(.secondary)
        }
    }

    private func trendDay(_ point: AnalyticsTrendPoint) -> String {
        guard let raw = point.date else { return "" }
        let formatter = DateFormatter()
        formatter.dateFormat = "yyyy-MM-dd"
        if let date = formatter.date(from: String(raw.prefix(10))) {
            return date.formatted(.dateTime.weekday(.abbreviated))
        }
        return String(raw.suffix(5))
    }

    // MARK: Campaigns

    private func campaignSection(_ campaigns: [AnalyticsTopCampaign]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
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
                        Divider().padding(.leading, 62)
                    }
                }
            }
            .airCard(padding: 6)
        }
    }

    private func homeCampaignRow(_ campaign: AnalyticsTopCampaign) -> some View {
        HStack(spacing: 12) {
            IconTile(symbol: "megaphone.fill", tone: campaignTone(campaign.status))
            VStack(alignment: .leading, spacing: 2) {
                Text(campaign.name ?? "Campaign")
                    .font(.body.weight(.medium))
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                Text("\(WFormat.compact(campaign.emailsSent ?? 0)) sent")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            HStack(spacing: 14) {
                miniRate(percentText(campaign.openRate), label: "open")
                miniRate(percentText(campaign.replyRate), label: "reply")
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 10)
        .contentShape(Rectangle())
    }

    private func miniRate(_ value: String, label: String) -> some View {
        VStack(alignment: .trailing, spacing: 1) {
            Text(value)
                .font(.subheadline.weight(.semibold))
                .monospacedDigit()
                .foregroundStyle(.primary)
            Text(label)
                .font(.system(size: 10.5))
                .foregroundStyle(.tertiary)
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

    private func activitySection(_ items: [AnalyticsActivityItem]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            AirSectionHeader(title: "Latest activity", actionTitle: "Analytics") {
                showAnalytics = true
            }
            VStack(spacing: 0) {
                ForEach(Array(items.prefix(4).enumerated()), id: \.offset) { index, item in
                    activityRow(item)
                    if index < min(items.count, 4) - 1 {
                        Divider().padding(.leading, 62)
                    }
                }
            }
            .airCard(padding: 6)
        }
    }

    private func activityRow(_ item: AnalyticsActivityItem) -> some View {
        let meta = activityMeta(item.type)
        return HStack(spacing: 12) {
            IconTile(symbol: meta.symbol, tone: meta.tone, size: 38)
            VStack(alignment: .leading, spacing: 2) {
                Text(meta.title)
                    .font(.body.weight(.medium))
                Text([item.contactEmail, item.campaignName].compactMap(\.self).joined(separator: " · "))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer()
            if let time = item.timestamp {
                Text(WFormat.relative(time))
                    .font(.footnote)
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 10)
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
