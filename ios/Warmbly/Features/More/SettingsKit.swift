import SwiftUI

// The settings language shared by the hub and every settings page:
// grouped cards, icon rows, toggles, the appearance picker and the hub's data.

// MARK: - Navigation

/// Pages pushed from the hub.
enum SettingsDestination: Hashable {
    case profile, notifications, security, billing, team, templates, activity
}

/// Browsers the hub opens over everything.
enum SettingsCover: String, Identifiable {
    case mailboxes, analytics, deals, tasks, meetings

    var id: String { rawValue }
}

/// What a hub layout can ask of the tab around it.
struct SettingsHubContext {
    let store: SettingsHubStore
    let present: (SettingsCover) -> Void
    let switchWorkspace: () -> Void
    let logout: () -> Void
}

// MARK: - Palette

/// One colour per destination, the same in every layout.
enum SettingsPalette {
    static let mailboxes = WTheme.accent
    static let analytics = WTheme.special
    static let activity = Color.dynamic(light: 0x64748B, dark: 0x94A3B8)
    static let deals = Tone.orange.color
    static let tasks = WTheme.positive
    static let meetings = WTheme.negative
    static let templates = Tone.indigo.color
    static let members = Color.dynamic(light: 0x0D9488, dark: 0x2DD4BF)
    static let profile = Color.dynamic(light: 0x8E8E96, dark: 0x8E8E96)
    static let notifications = Color.dynamic(light: 0xF0384A, dark: 0xFF5A6A)
    static let security = Color.dynamic(light: 0x2563EB, dark: 0x60A5FA)
    static let billing = WTheme.warning
    static let appearance = Color.dynamic(light: 0x3A3A40, dark: 0x5A5A62)
    static let server = Color.dynamic(light: 0x71717A, dark: 0x71717A)
}

// MARK: - Hub data

/// Everything the hub shows at a glance; each part loads only when the
/// member can see it, and a part that fails simply stays quiet.
@MainActor
@Observable
final class SettingsHubStore {
    private(set) var subscription: SubscriptionInfo?
    private(set) var trial: TrialInfo?
    private(set) var unreadNotifications = 0
    private(set) var twoFAEnabled: Bool?
    private(set) var passkeyCount: Int?
    private(set) var mailboxCount: Int?
    private(set) var warmingCount = 0
    private(set) var issueCount = 0
    private(set) var weekSent: [Double] = []
    private(set) var deals: CRMDealsSummary?
    private(set) var tasks: CRMTasksSummary?
    private(set) var meetings: CRMMeetingsSummary?
    private(set) var templateCount: Int?
    private(set) var hasLoaded = false
    private var isLoading = false

    struct Access {
        var emails: Bool
        var analytics: Bool
        var crm: Bool
        var campaigns: Bool
    }

    var weekSentTotal: Int { Int(weekSent.reduce(0, +)) }

    var isTrialing: Bool { subscription?.status == "trialing" || trial?.isInTrial == true }

    /// "13 days left", "Ends today", or nil outside a trial.
    var trialLabel: String? {
        guard isTrialing, let days = trial?.daysRemaining else { return nil }
        if trial?.isExpired == true { return "Trial ended" }
        return days <= 0 ? "Ends today" : days == 1 ? "1 day left" : "\(days) days left"
    }

    /// Factors protecting the account beyond its password.
    var securityFactors: Int { (twoFAEnabled == true ? 1 : 0) + ((passkeyCount ?? 0) > 0 ? 1 : 0) }

    func load(_ api: APIClient, access: Access) async {
        guard !isLoading else { return }
        isLoading = true
        defer { isLoading = false }

        async let sub: SubscriptionInfo? = try? api.get("subscription")
        async let tr: TrialInfo? = try? api.get("subscription/trial")
        async let feed: MoreNotificationFeed? = try? api.get("auth/me/notifications", query: ["limit": "1", "unread": "1"])
        async let twoFA: MoreTwoFAStatus? = try? api.get("auth/2fa/status")
        async let keys: [PasskeyCredential]? = try? api.get("auth/passkey/credentials")
        async let accounts: ListResponse<EmailAccount>? = access.emails ? try? api.get("emails", query: ["limit": "200"]) : nil
        async let statuses: ListResponse<AccountAnalytics>? = access.analytics ? try? api.get("analytics/accounts") : nil
        async let dash: DashboardAnalytics? = access.analytics ? try? api.get("analytics/dashboard", query: ["period": "7d"]) : nil
        async let dealSummary: CRMDealsSummary? = access.crm ? try? api.post("crm/deals/summary", body: CRMDealSearchBody()) : nil
        async let taskSummary: CRMTasksSummary? = access.crm ? try? api.post("crm/tasks/summary", body: CRMTaskSearchBody()) : nil
        async let meetingSummary: CRMMeetingsSummary? = access.crm ? try? api.get("meetings/summary") : nil
        async let templates: CRMPage<EmailTemplate>? = access.campaigns ? try? api.get("templates") : nil

        let (subValue, trialValue, feedValue, twoFAValue, keysValue) = await (sub, tr, feed, twoFA, keys)
        let (accountPage, statusPage, dashValue) = await (accounts, statuses, dash)
        let (dealValue, taskValue, meetingValue, templatePage) = await (dealSummary, taskSummary, meetingSummary, templates)

        withAnimation(.snappy) {
            subscription = subValue ?? subscription
            trial = trialValue ?? trial
            if let feedValue { unreadNotifications = feedValue.unread ?? 0 }
            if let twoFAValue { twoFAEnabled = twoFAValue.enabled ?? false }
            if let keysValue { passkeyCount = keysValue.count }
            if let accountPage {
                mailboxCount = (accountPage.pagination?.total).map(Int.init) ?? accountPage.data.count
                warmingCount = accountPage.data.filter(\.isWarmingActive).count
                if let statusPage {
                    let ids = Set(accountPage.data.map(\.id))
                    issueCount = statusPage.data.filter { ids.contains($0.id) && $0.health?.hasIssue == true }.count
                }
            }
            if let dashValue { weekSent = Self.lastWeek(dashValue.dailyTrend ?? []) }
            deals = dealValue ?? deals
            tasks = taskValue ?? tasks
            meetings = meetingValue ?? meetings
            if let templatePage {
                templateCount = (templatePage.pagination?.total).map(Int.init) ?? templatePage.items.count
            }
            hasLoaded = true
        }
    }

    /// Sends per day over the last seven, oldest first, empty days as zero.
    private static func lastWeek(_ points: [AnalyticsTrendPoint]) -> [Double] {
        var values: [String: TrendDay] = [:]
        for point in points {
            guard let raw = point.date else { continue }
            values[String(raw.prefix(10))] = TrendDay(date: .now, sent: point.sent ?? 0, opens: 0, clicks: 0, replies: 0)
        }
        return TrendDay.lastDays(7, values: values).map { Double($0.sent) }
    }
}

// MARK: - Icons

/// A symbol on a soft tint of its colour.
struct SettingsIcon: View {
    let symbol: String
    var color: Color
    var size: CGFloat = 30

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: size * 0.46, weight: .medium))
            .foregroundStyle(color)
            .frame(width: size, height: size)
            .background(color.opacity(0.13), in: RoundedRectangle(cornerRadius: size * 0.3, style: .continuous))
    }
}

// MARK: - Cards and rows

/// A grouped card with an optional caption above and note below.
struct SettingsSection<Content: View>: View {
    let title: String?
    var footer: String?
    var dividerLeading: CGFloat
    @ViewBuilder var content: Content

    init(_ title: String? = nil, footer: String? = nil, dividerLeading: CGFloat = 58, @ViewBuilder content: () -> Content) {
        self.title = title
        self.footer = footer
        self.dividerLeading = dividerLeading
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let title {
                EyebrowLabel(title)
                    .padding(.leading, 16)
            }
            MoreCard(dividerLeading: dividerLeading) { content }
            if let footer {
                Text(footer)
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.inkSecondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, 16)
                    .padding(.top, 2)
            }
        }
    }
}

/// An icon, a title (and maybe a line under it), then the value and accessory.
struct SettingsRow: View {
    let icon: String
    var color: Color = WTheme.accent
    let title: String
    var subtitle: String?
    var value: String?
    var valueColor: Color?
    var dot: Color?
    var badge = 0
    var accessory: Accessory = .chevron
    var destructive = false

    enum Accessory {
        case chevron, external, menu, none
    }

    var body: some View {
        HStack(spacing: 12) {
            SettingsIcon(symbol: icon, color: destructive ? WTheme.negative : color)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.system(size: 16))
                    .foregroundStyle(destructive ? WTheme.negative : WTheme.ink)
                    .lineLimit(1)
                if let subtitle, !subtitle.isEmpty {
                    Text(subtitle)
                        .font(.system(size: 13))
                        .foregroundStyle(WTheme.inkSecondary)
                        .lineLimit(2)
                }
            }
            Spacer(minLength: 8)
            if let value {
                HStack(spacing: 6) {
                    if let dot {
                        Circle().fill(dot).frame(width: 7, height: 7)
                    }
                    Text(value)
                        .font(.system(size: 15))
                        .monospacedDigit()
                        .foregroundStyle(valueColor ?? WTheme.inkSecondary)
                        .lineLimit(1)
                        .contentTransition(.numericText())
                }
            }
            if badge > 0 {
                Text(badge > 99 ? "99+" : "\(badge)")
                    .font(.system(size: 13, weight: .semibold))
                    .monospacedDigit()
                    .foregroundStyle(.white)
                    .padding(.horizontal, 7)
                    .frame(minWidth: 22, minHeight: 22)
                    .background(WTheme.negative, in: Capsule())
                    .contentTransition(.numericText())
            }
            switch accessory {
            case .chevron:
                Image(systemName: "chevron.right")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(WTheme.inkTertiary.opacity(0.8))
            case .external:
                Image(systemName: "arrow.up.right")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(WTheme.inkTertiary)
            case .menu:
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(WTheme.inkTertiary)
            case .none:
                EmptyView()
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, subtitle == nil ? 12 : 10)
        .frame(minHeight: 52)
        .contentShape(Rectangle())
    }
}

/// A row whose trailing control is a switch.
struct SettingsToggleRow: View {
    let icon: String
    var color: Color = WTheme.accent
    let title: String
    var subtitle: String?
    @Binding var isOn: Bool
    var disabled = false

    var body: some View {
        Toggle(isOn: $isOn) {
            HStack(spacing: 12) {
                SettingsIcon(symbol: icon, color: isOn ? color : WTheme.paused)
                    .animation(.snappy, value: isOn)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.system(size: 16))
                        .foregroundStyle(WTheme.ink)
                    if let subtitle, !subtitle.isEmpty {
                        Text(subtitle)
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkSecondary)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
        }
        .tint(WTheme.positive)
        .disabled(disabled)
        .padding(.horizontal, 16)
        .padding(.vertical, subtitle == nil ? 12 : 10)
        .frame(minHeight: 52)
        .sensoryFeedback(.selection, trigger: isOn)
    }
}

/// A full-width button row inside a card: centered or leading text, no icon.
struct SettingsButtonRow: View {
    let title: String
    var role: ButtonRole?
    var busy = false
    var done = false
    var disabled = false
    let action: () -> Void

    var body: some View {
        Button(role: role, action: action) {
            HStack(spacing: 8) {
                Text(title)
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(role == .destructive ? WTheme.negative : (disabled ? WTheme.inkTertiary : WTheme.accent))
                Spacer(minLength: 0)
                if busy {
                    ProgressView().controlSize(.small)
                } else if done {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 16))
                        .foregroundStyle(WTheme.positive)
                        .transition(.scale.combined(with: .opacity))
                }
            }
            .padding(.horizontal, 16)
            .frame(minHeight: 50)
            .contentShape(Rectangle())
        }
        .buttonStyle(MoreRowPressStyle())
        .disabled(disabled || busy)
        .animation(.snappy, value: done)
    }
}

/// A labelled text field row: the label above, the field under it.
struct SettingsFieldRow<Field: View>: View {
    let label: String
    @ViewBuilder var field: Field

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label)
                .font(.system(size: 12.5, weight: .medium))
                .foregroundStyle(WTheme.inkTertiary)
            field
                .font(.system(size: 16))
                .foregroundStyle(WTheme.ink)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Page

/// A settings page: grouped cards on the grouped canvas under an inline title.
struct SettingsPage<Content: View>: View {
    let title: String
    var spacing: CGFloat = 28
    @ViewBuilder var content: Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: spacing) {
                content
            }
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 120)
        }
        .scrollIndicators(.hidden)
        .readableMargins()
        .scrollDismissesKeyboard(.interactively)
        .defaultScrollAnchor(SettingsDebug.anchor)
        .background(WTheme.groupedPage)
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(WTheme.groupedPage, for: .navigationBar)
    }
}

/// A page hero: a glowing glyph, a headline and one line under it.
struct SettingsHero: View {
    let symbol: String
    let color: Color
    let title: String
    var subtitle: String?

    @State private var shown = false

    var body: some View {
        VStack(spacing: 12) {
            ZStack {
                Circle()
                    .fill(RadialGradient(colors: [color.opacity(0.28), color.opacity(0)], center: .center, startRadius: 4, endRadius: 64))
                    .frame(width: 128, height: 128)
                    .scaleEffect(shown ? 1 : 0.6)
                Image(systemName: symbol)
                    .font(.system(size: 30, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(width: 68, height: 68)
                    .background(
                        LinearGradient(colors: [color.opacity(0.8), color], startPoint: .topLeading, endPoint: .bottomTrailing),
                        in: RoundedRectangle(cornerRadius: 20, style: .continuous)
                    )
                    .shadow(color: color.opacity(0.35), radius: 14, y: 6)
                    .scaleEffect(shown ? 1 : 0.8)
                    .symbolEffect(.bounce, value: shown)
            }
            .frame(height: 96)
            Text(title)
                .displayFont(24, weight: .semibold)
                .foregroundStyle(WTheme.ink)
                .multilineTextAlignment(.center)
                .contentTransition(.opacity)
            if let subtitle {
                Text(subtitle)
                    .font(.system(size: 14.5))
                    .foregroundStyle(WTheme.inkSecondary)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, 12)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 4)
        .onAppear {
            withAnimation(.spring(response: 0.6, dampingFraction: 0.7).delay(0.05)) { shown = true }
        }
    }
}

// MARK: - Appearance

/// Light, dark and automatic as three small phone screens.
struct AppearanceThumbnailPicker: View {
    @Binding var selection: AppearancePreference

    var body: some View {
        HStack(spacing: 0) {
            ForEach(AppearancePreference.allCases) { option in
                Button {
                    withAnimation(.snappy) { selection = option }
                } label: {
                    VStack(spacing: 10) {
                        AppearanceThumbnail(option: option)
                            .frame(width: 62, height: 104)
                            .overlay(
                                RoundedRectangle(cornerRadius: 13, style: .continuous)
                                    .strokeBorder(selection == option ? WTheme.accent : WTheme.hairlineStrong, lineWidth: selection == option ? 2.5 : 1)
                                    .padding(-4)
                            )
                            .padding(4)
                        Text(option == .system ? "Automatic" : option.title)
                            .font(.system(size: 13.5, weight: selection == option ? .semibold : .regular))
                            .foregroundStyle(selection == option ? WTheme.ink : WTheme.inkSecondary)
                        Image(systemName: selection == option ? "checkmark.circle.fill" : "circle")
                            .font(.system(size: 20))
                            .foregroundStyle(selection == option ? WTheme.accent : WTheme.inkTertiary.opacity(0.6))
                            .contentTransition(.symbolEffect(.replace))
                    }
                    .frame(maxWidth: .infinity)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(option.title)
                .accessibilityAddTraits(selection == option ? .isSelected : [])
            }
        }
        .padding(.vertical, 18)
        .sensoryFeedback(.selection, trigger: selection)
    }
}

/// A tiny drawing of the app in one appearance.
private struct AppearanceThumbnail: View {
    let option: AppearancePreference

    var body: some View {
        switch option {
        case .light: screen(dark: false)
        case .dark: screen(dark: true)
        case .system:
            ZStack {
                screen(dark: false)
                screen(dark: true)
                    .mask(
                        GeometryReader { proxy in
                            Path { path in
                                path.move(to: CGPoint(x: proxy.size.width, y: 0))
                                path.addLine(to: CGPoint(x: proxy.size.width, y: proxy.size.height))
                                path.addLine(to: CGPoint(x: 0, y: proxy.size.height))
                                path.closeSubpath()
                            }
                        }
                    )
            }
        }
    }

    private func screen(dark: Bool) -> some View {
        let bg = dark ? Color(hex: 0x131315) : Color(hex: 0xF2F2F7)
        let card = dark ? Color(hex: 0x202024) : .white
        let line = dark ? Color(hex: 0x3A3A40) : Color(hex: 0xD9D9DE)
        return RoundedRectangle(cornerRadius: 11, style: .continuous)
            .fill(bg)
            .overlay(alignment: .top) {
                VStack(alignment: .leading, spacing: 5) {
                    Capsule().fill(line).frame(width: 26, height: 4)
                    RoundedRectangle(cornerRadius: 4, style: .continuous)
                        .fill(card)
                        .frame(height: 22)
                        .overlay(alignment: .leading) {
                            HStack(spacing: 4) {
                                Circle().fill(Color(hex: 0x0A8DE6)).frame(width: 9, height: 9)
                                Capsule().fill(line).frame(width: 22, height: 3)
                            }
                            .padding(.leading, 5)
                        }
                    RoundedRectangle(cornerRadius: 4, style: .continuous)
                        .fill(card)
                        .frame(height: 34)
                        .overlay(alignment: .topLeading) {
                            VStack(alignment: .leading, spacing: 4) {
                                Capsule().fill(line).frame(width: 30, height: 3)
                                Capsule().fill(line).frame(width: 20, height: 3)
                                Capsule().fill(line).frame(width: 26, height: 3)
                            }
                            .padding(6)
                        }
                }
                .padding(7)
                .padding(.top, 6)
            }
            .clipShape(RoundedRectangle(cornerRadius: 11, style: .continuous))
    }
}

// MARK: - Debug

enum SettingsDebug {
    /// Scripted runs can open the hub or a page scrolled (WARMBLY_DEBUG_SCROLL=bottom|center).
    static var anchor: UnitPoint? {
        #if DEBUG
        switch ProcessInfo.processInfo.environment["WARMBLY_DEBUG_SCROLL"] {
        case "bottom": return .bottom
        case "center": return .center
        default: break
        }
        #endif
        return nil
    }
}

// MARK: - Shared hub bits

/// The logo and the build, at the foot of the hub.
struct SettingsVersionFooter: View {
    var body: some View {
        let info = Bundle.main.infoDictionary
        let version = info?["CFBundleShortVersionString"] as? String ?? "1.0"
        let build = info?["CFBundleVersion"] as? String
        VStack(spacing: 6) {
            WarmblyLogo()
                .fill(WTheme.inkTertiary.opacity(0.7))
                .frame(width: 16, height: 16.4)
            Text("Warmbly \(version)\(build.map { " (\($0))" } ?? "")")
                .font(.system(size: 12))
                .monospacedDigit()
                .foregroundStyle(WTheme.inkTertiary)
            Text(AppConfig.serverOrigin.replacingOccurrences(of: "https://", with: "").replacingOccurrences(of: "http://", with: ""))
                .font(.system(size: 11.5))
                .foregroundStyle(WTheme.inkTertiary.opacity(0.8))
                .lineLimit(1)
                .truncationMode(.middle)
        }
        .frame(maxWidth: .infinity)
    }
}

/// Light, dark and automatic as one compact switch, for places too small for the thumbnails.
struct AppearanceSegments: View {
    @Binding var selection: AppearancePreference
    @Namespace private var pill

    var body: some View {
        HStack(spacing: 4) {
            ForEach([AppearancePreference.light, .dark, .system]) { option in
                let on = selection == option
                Button {
                    withAnimation(.spring(response: 0.32, dampingFraction: 0.82)) { selection = option }
                } label: {
                    HStack(spacing: 7) {
                        Image(systemName: option.symbol)
                            .font(.system(size: 14, weight: .medium))
                        Text(option == .system ? "Automatic" : option.title)
                            .font(.system(size: 14.5, weight: on ? .semibold : .regular))
                    }
                    .foregroundStyle(on ? WTheme.ink : WTheme.inkSecondary)
                    .frame(maxWidth: .infinity)
                    .frame(height: 42)
                    .background {
                        if on {
                            RoundedRectangle(cornerRadius: 13, style: .continuous)
                                .fill(Color.dynamic(light: 0xFFFFFF, dark: 0x3A3A3F))
                                .shadow(color: .black.opacity(0.08), radius: 4, y: 1)
                                .matchedGeometryEffect(id: "pill", in: pill)
                        }
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(option.title)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
        .padding(4)
        .background(Color.dynamic(light: 0xE4E4E9, dark: 0x202024), in: RoundedRectangle(cornerRadius: 17, style: .continuous))
        .sensoryFeedback(.selection, trigger: selection)
    }
}
