import SwiftUI

// Settings on a wide window: the hub as a list on the left and the chosen
// page beside it, as in the iPad Settings app.

/// A page the wide layout shows beside its list.
enum SettingsPane: Hashable {
    case profile, notifications, security, billing, appearance, team, templates, activity

    init(_ destination: SettingsDestination) {
        switch destination {
        case .profile: self = .profile
        case .notifications: self = .notifications
        case .security: self = .security
        case .billing: self = .billing
        case .team: self = .team
        case .templates: self = .templates
        case .activity: self = .activity
        }
    }

    /// The page pushed for it on a narrow window; appearance is inline there.
    var destination: SettingsDestination? {
        switch self {
        case .profile: .profile
        case .notifications: .notifications
        case .security: .security
        case .billing: .billing
        case .appearance: nil
        case .team: .team
        case .templates: .templates
        case .activity: .activity
        }
    }
}

struct SettingsSplitView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.shellLayout) private var shell
    let ctx: SettingsHubContext
    @Binding var selection: SettingsPane?
    let contentWidth: CGFloat
    let refresh: () async -> Void

    var body: some View {
        let access = HubAccess(env)
        let shown = resolved(selection ?? .profile, access)
        HStack(spacing: 0) {
            SettingsPaneList(
                ctx: ctx,
                access: access,
                showsWorkspace: shell.bar != .sidebar,
                selection: shown,
                select: { selection = $0 },
                refresh: refresh
            )
            .frame(width: min(max(contentWidth * 0.34, 340), 400))
            VerticalHairline()
            NavigationStack {
                page(shown)
            }
            .id(shown)
            .transition(.opacity)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(GroupedSurface.page)
        .animation(.snappy(duration: 0.22), value: shown)
    }

    /// Falls back to Profile when the member lost access to the chosen page.
    private func resolved(_ pane: SettingsPane, _ access: HubAccess) -> SettingsPane {
        switch pane {
        case .billing where !access.billing: .profile
        case .templates where !access.campaigns: .profile
        case .activity where !access.analytics: .profile
        default: pane
        }
    }

    @ViewBuilder
    private func page(_ pane: SettingsPane) -> some View {
        switch pane {
        case .profile: ProfileSettingsView()
        case .notifications: NotificationsView()
        case .security: SecurityInfoView()
        case .billing: BillingView()
        case .appearance: AppearanceSettingsPage()
        case .team: TeamMembersView()
        case .templates: TemplatesListView()
        case .activity: AuditLogView()
        }
    }
}

// MARK: - List

/// The left column: who you are, your settings and, without the iPad
/// sidebar, the workspace pages it would otherwise hold.
private struct SettingsPaneList: View {
    @Environment(AppEnvironment.self) private var env
    @AppStorage(AppearancePreference.storageKey) private var appearance = AppearancePreference.standard
    let ctx: SettingsHubContext
    let access: HubAccess
    let showsWorkspace: Bool
    let selection: SettingsPane
    let select: (SettingsPane) -> Void
    let refresh: () async -> Void
    @Namespace private var selectionNS

    private var store: SettingsHubStore { ctx.store }

    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 24) {
                VStack(alignment: .leading, spacing: 14) {
                    header
                    identityCard
                }
                group("Preferences") {
                    row(.profile, icon: "person.crop.circle.fill", color: SettingsPalette.profile, title: "Profile")
                    row(.notifications, icon: "bell.badge.fill", color: SettingsPalette.notifications, title: "Notifications", badge: store.unreadNotifications)
                    row(
                        .security, icon: "lock.shield.fill", color: SettingsPalette.security, title: "Security",
                        value: store.securityValue.text.isEmpty ? nil : store.securityValue.text,
                        dot: store.securityValue.text.isEmpty ? nil : store.securityValue.color
                    )
                    if access.billing {
                        row(
                            .billing, icon: "creditcard.fill", color: SettingsPalette.billing, title: "Billing",
                            value: store.trialLabel ?? (store.hasLoaded ? store.planName() : nil)
                        )
                    }
                    row(
                        .appearance, icon: appearance.symbol, color: SettingsPalette.appearance, title: "Appearance",
                        value: appearance == .system ? "Automatic" : appearance.title
                    )
                }
                if showsWorkspace { workspace }
                group(nil) {
                    Button(role: .destructive, action: ctx.logout) {
                        PaneRowFace(icon: "rectangle.portrait.and.arrow.right", color: WTheme.negative, title: "Log out", destructive: true)
                    }
                    .buttonStyle(PaneRowPressStyle())
                    .hoverEffect(.highlight)
                }
                SettingsVersionFooter()
                    .padding(.top, 4)
            }
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 120)
        }
        .defaultScrollAnchor(SettingsDebug.anchor)
        .refreshable { await refresh() }
        .background(GroupedSurface.page)
        .animation(.spring(response: 0.32, dampingFraction: 0.82), value: selection)
        .sensoryFeedback(.selection, trigger: selection)
    }

    private var header: some View {
        let unread = store.unreadNotifications
        return HStack(alignment: .center) {
            Text("Settings")
                .font(.system(size: 19, weight: .semibold))
                .tracking(-0.3)
                .foregroundStyle(WTheme.ink)
            Spacer()
            Button { select(.notifications) } label: {
                SettingsBellFace(unread: unread)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(unread > 0 ? "Notifications, \(unread) unread" : "Notifications")
        }
        .padding(.horizontal, 4)
    }

    // MARK: Identity

    /// You, and the workspace you are in, which opens the switcher.
    private var identityCard: some View {
        VStack(spacing: 0) {
            if let user = env.session.user {
                Button { select(.profile) } label: {
                    HStack(spacing: 12) {
                        WAvatar(name: user.displayName, imageURL: user.avatarURL, seed: user.id, size: 46)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(user.displayName)
                                .font(.system(size: 17, weight: .semibold))
                                .foregroundStyle(WTheme.ink)
                                .lineLimit(1)
                            Text(user.email)
                                .font(.system(size: 13.5))
                                .foregroundStyle(WTheme.inkSecondary)
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }
                        Spacer(minLength: 0)
                    }
                    .padding(14)
                    .contentShape(Rectangle())
                }
                .buttonStyle(MoreRowPressStyle())
                .accessibilityLabel("Profile, \(user.displayName)")
                Hairline().padding(.leading, 14)
            }
            Button(action: ctx.switchWorkspace) {
                HStack(spacing: 10) {
                    WAvatar(
                        name: env.session.currentOrg?.name ?? "W",
                        imageURL: env.session.currentOrg?.avatarURL,
                        seed: env.session.currentOrgID ?? "org",
                        size: 22
                    )
                    VStack(alignment: .leading, spacing: 1) {
                        Text("Workspace")
                            .font(.system(size: 11.5))
                            .foregroundStyle(WTheme.inkSecondary)
                        Text(env.session.currentOrg?.name ?? "Select")
                            .font(.system(size: 14.5, weight: .medium))
                            .foregroundStyle(WTheme.ink)
                            .lineLimit(1)
                    }
                    Spacer(minLength: 8)
                    if !access.billing, let role = env.session.role {
                        Text(role.capitalized)
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkSecondary)
                            .lineLimit(1)
                    }
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.system(size: 11.5, weight: .semibold))
                        .foregroundStyle(WTheme.inkTertiary)
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .contentShape(Rectangle())
            }
            .buttonStyle(MoreRowPressStyle())
            .accessibilityLabel("Switch workspace")
        }
        .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
        .surfaceCard(in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }

    // MARK: Workspace

    private var workspace: some View {
        group("Workspace") {
            if access.emails {
                coverRow(
                    .mailboxes, icon: "envelope.fill", color: SettingsPalette.mailboxes, title: "Mailboxes",
                    value: store.mailboxCount.map(String.init), dot: store.mailboxCaption?.color
                )
            }
            if access.analytics {
                coverRow(.analytics, icon: "chart.bar.fill", color: SettingsPalette.analytics, title: "Analytics")
            }
            if access.crm {
                coverRow(.deals, icon: "briefcase.fill", color: SettingsPalette.deals, title: "Deals", value: store.deals.map { "\($0.openCount ?? 0)" })
                coverRow(
                    .tasks, icon: "checklist", color: SettingsPalette.tasks, title: "Tasks",
                    value: store.tasks.map { "\($0.openCount)" }, dot: store.tasksCaption?.color
                )
                coverRow(.meetings, icon: "calendar", color: SettingsPalette.meetings, title: "Meetings", value: store.meetings.map { "\($0.upcoming ?? 0)" })
            }
            if access.campaigns {
                row(.templates, icon: "doc.on.doc.fill", color: SettingsPalette.templates, title: "Templates", value: store.templateCount.map(String.init))
            }
            row(
                .team, icon: "person.2.fill", color: SettingsPalette.members, title: "Members",
                value: env.session.currentOrg?.counts?.totalMembers.map(String.init)
            )
            if access.analytics {
                row(.activity, icon: "clock.arrow.circlepath", color: SettingsPalette.activity, title: "Activity")
            }
        }
    }

    // MARK: Parts

    /// A card of rows under an optional caption, inset so the selection reads as a tile.
    private func group(_ title: String?, @ViewBuilder content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            if let title {
                EyebrowLabel(title)
                    .padding(.leading, 14)
            }
            VStack(spacing: 2) { content() }
                .padding(6)
                .surfaceCard(in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        }
    }

    private func row(_ pane: SettingsPane, icon: String, color: Color, title: String, value: String? = nil, dot: Color? = nil, badge: Int = 0) -> some View {
        let selected = selection == pane
        return Button { select(pane) } label: {
            PaneRowFace(icon: icon, color: color, title: title, value: value, dot: dot, badge: badge, selected: selected)
                .background {
                    if selected {
                        RoundedRectangle(cornerRadius: 14, style: .continuous)
                            .fill(WTheme.accentSoft)
                            .matchedGeometryEffect(id: "settings-pane", in: selectionNS)
                    }
                }
        }
        .buttonStyle(PaneRowPressStyle())
        .hoverEffect(.highlight)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private func coverRow(_ cover: SettingsCover, icon: String, color: Color, title: String, value: String? = nil, dot: Color? = nil) -> some View {
        Button { ctx.present(cover) } label: {
            PaneRowFace(icon: icon, color: color, title: title, value: value, dot: dot)
        }
        .buttonStyle(PaneRowPressStyle())
        .hoverEffect(.highlight)
    }
}

/// A compact list row: tinted icon, title, then a value, a dot or a badge.
private struct PaneRowFace: View {
    let icon: String
    let color: Color
    let title: String
    var value: String?
    var dot: Color?
    var badge = 0
    var selected = false
    var destructive = false

    var body: some View {
        HStack(spacing: 12) {
            SettingsIcon(symbol: icon, color: color, size: 30)
            Text(title)
                .font(.system(size: 15.5, weight: selected ? .semibold : .regular))
                .foregroundStyle(destructive ? WTheme.negative : WTheme.ink)
                .lineLimit(1)
            Spacer(minLength: 8)
            if let value {
                HStack(spacing: 6) {
                    if let dot {
                        Circle().fill(dot).frame(width: 7, height: 7)
                    }
                    Text(value)
                        .font(.system(size: 14))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.inkSecondary)
                        .lineLimit(1)
                        .contentTransition(.numericText())
                }
            }
            if badge > 0 {
                Text(badge > 99 ? "99+" : "\(badge)")
                    .font(.system(size: 12.5, weight: .semibold))
                    .monospacedDigit()
                    .foregroundStyle(.white)
                    .padding(.horizontal, 6)
                    .frame(minWidth: 21, minHeight: 21)
                    .background(WTheme.negative, in: Capsule())
                    .contentTransition(.numericText())
            }
        }
        .padding(.horizontal, 10)
        .frame(height: 46)
        .contentShape(.hoverEffect, RoundedRectangle(cornerRadius: 14, style: .continuous))
        .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
    }
}

/// Press feedback for an inset row: a rounded fill under it.
private struct PaneRowPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .fill(configuration.isPressed ? WTheme.fill : Color.clear)
            )
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}

// MARK: - Appearance page

/// The appearance picker as a page of its own, for the wide layout.
struct AppearanceSettingsPage: View {
    @AppStorage(AppearancePreference.storageKey) private var appearance = AppearancePreference.standard

    var body: some View {
        SettingsPage(title: "Appearance") {
            SettingsSection("Theme", footer: "Automatic follows your device's light and dark setting.") {
                AppearanceThumbnailPicker(selection: $appearance)
            }
        }
    }
}
