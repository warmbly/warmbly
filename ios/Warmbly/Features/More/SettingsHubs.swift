import SwiftUI

// The settings hub. It draws every destination from one store; the tab
// around it owns navigation, sheets and covers.

// MARK: - Facts

extension SettingsHubStore {
    /// The mailbox line: trouble first, then warming, then all clear.
    var mailboxCaption: (text: String, color: Color?)? {
        guard mailboxCount != nil else { return nil }
        if issueCount > 0 { return ("\(issueCount) need attention", WTheme.negative) }
        if warmingCount > 0 { return ("\(warmingCount) warming", Tone.orange.color) }
        return ("All healthy", WTheme.positive)
    }

    var dealsCaption: String? {
        guard let deals else { return nil }
        guard let value = deals.openValue, value > 0 else { return "No open value" }
        return CRMFormat.compactCurrency(value, code: deals.currency) + " open"
    }

    var tasksCaption: (text: String, color: Color?)? {
        guard let tasks else { return nil }
        let overdue = tasks.overdueCount ?? 0
        return overdue > 0 ? ("\(overdue) overdue", WTheme.negative) : ("Nothing overdue", nil)
    }

    var securityValue: (text: String, color: Color) {
        guard twoFAEnabled != nil else { return ("", WTheme.inkTertiary) }
        return securityFactors > 0 ? ("On", WTheme.positive) : ("Off", WTheme.warning)
    }

    func planName() -> String {
        let raw = subscription?.plan?.name ?? "Free"
        guard let first = raw.first else { return "Free" }
        return first.uppercased() + raw.dropFirst().lowercased()
    }
}

private struct HubAccess {
    let emails: Bool
    let analytics: Bool
    let crm: Bool
    let campaigns: Bool
    let billing: Bool

    @MainActor
    init(_ env: AppEnvironment) {
        emails = env.session.can(.manageEmails)
        analytics = env.session.can(.viewAnalytics)
        crm = env.session.can(.viewContacts)
        campaigns = env.session.can(.viewCampaigns)
        billing = env.session.can(.manageBilling)
    }
}

// MARK: - Tiles

/// You and your standing in one card, your workspace as live tiles, then
/// your preferences as one card below.
struct SettingsTilesHub: View {
    @Environment(AppEnvironment.self) private var env
    let ctx: SettingsHubContext
    @AppStorage(AppearancePreference.storageKey) private var appearance = AppearancePreference.standard
    @State private var appeared = false

    private var store: SettingsHubStore { ctx.store }

    var body: some View {
        let access = HubAccess(env)
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 30) {
                VStack(alignment: .leading, spacing: 16) {
                    HStack(alignment: .center) {
                        Text("Settings")
                            .displayFont(34, weight: .bold)
                            .foregroundStyle(WTheme.ink)
                        Spacer()
                        notificationsButton
                    }
                    .padding(.horizontal, 4)
                    profileCard(access)
                }
                workspace(access)
                preferences(access)
                SettingsSection("Appearance") {
                    AppearanceThumbnailPicker(selection: $appearance)
                }
                .reveal(appeared, 8)
                SettingsSection {
                    Button(role: .destructive, action: ctx.logout) {
                        SettingsRow(icon: "rectangle.portrait.and.arrow.right", title: "Log out", accessory: .none, destructive: true)
                    }
                    .buttonStyle(MoreRowPressStyle())
                }
                .reveal(appeared, 9)
                SettingsVersionFooter()
            }
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 120)
        }
        .defaultScrollAnchor(SettingsDebug.anchor)
        .readableMargins()
        .background(WTheme.groupedPage)
        .onAppear { appeared = true }
    }

    // Glass bell into Notifications, badged with the unread count.
    private var notificationsButton: some View {
        let unread = store.unreadNotifications
        return NavigationLink(value: SettingsDestination.notifications) {
            Image(systemName: "bell")
                .font(.system(size: 17, weight: .medium))
                .foregroundStyle(WTheme.ink)
                .symbolEffect(.bounce, value: unread)
                .frame(width: 44, height: 44)
                .contentShape(Circle())
                .glassEffect(.regular.interactive(), in: Circle())
                .overlay(alignment: .topTrailing) {
                    if unread > 0 {
                        Text(unread > 99 ? "99+" : "\(unread)")
                            .font(.system(size: 11.5, weight: .bold))
                            .monospacedDigit()
                            .foregroundStyle(.white)
                            .padding(.horizontal, 5)
                            .frame(minWidth: 20, minHeight: 20)
                            .background(WTheme.negative, in: Capsule())
                            .overlay(Capsule().strokeBorder(WTheme.groupedPage, lineWidth: 2))
                            .offset(x: 5, y: -3)
                            .contentTransition(.numericText())
                            .transition(.scale.combined(with: .opacity))
                    }
                }
                .animation(.spring(response: 0.4, dampingFraction: 0.7), value: unread)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(unread > 0 ? "Notifications, \(unread) unread" : "Notifications")
    }

    // MARK: Profile card

    private func profileCard(_ access: HubAccess) -> some View {
        VStack(spacing: 0) {
            if let user = env.session.user {
                NavigationLink(value: SettingsDestination.profile) {
                    HStack(spacing: 14) {
                        WAvatar(name: user.displayName, imageURL: user.avatarURL, seed: user.id, size: 60)
                        VStack(alignment: .leading, spacing: 3) {
                            Text(user.displayName)
                                .font(.system(size: 21, weight: .semibold))
                                .foregroundStyle(WTheme.ink)
                                .lineLimit(1)
                            Text(user.email)
                                .font(.system(size: 14.5))
                                .foregroundStyle(WTheme.inkSecondary)
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }
                        Spacer(minLength: 8)
                        Image(systemName: "chevron.right")
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundStyle(WTheme.inkTertiary.opacity(0.8))
                    }
                    .padding(16)
                    .contentShape(Rectangle())
                }
                .buttonStyle(MoreRowPressStyle())
                .accessibilityLabel("Profile, \(user.displayName)")
            }
            Hairline()
            HStack(spacing: 0) {
                Button(action: ctx.switchWorkspace) {
                    statusCell("Workspace", env.session.currentOrg?.name ?? "Select", grows: true) {
                        WAvatar(
                            name: env.session.currentOrg?.name ?? "W",
                            imageURL: env.session.currentOrg?.avatarURL,
                            seed: env.session.currentOrgID ?? "org",
                            size: 16
                        )
                    }
                }
                .buttonStyle(MoreRowPressStyle())
                .accessibilityLabel("Switch workspace")
                cellDivider
                if access.billing {
                    NavigationLink(value: SettingsDestination.billing) {
                        statusCell("Plan", store.trialLabel ?? (store.hasLoaded ? store.planName() : "–")) { EmptyView() }
                    }
                    .buttonStyle(MoreRowPressStyle())
                } else {
                    statusCell("Role", env.session.role?.capitalized ?? "Member") { EmptyView() }
                }
                cellDivider
                NavigationLink(value: SettingsDestination.security) {
                    let protected = store.securityFactors > 0
                    statusCell("Sign-in", store.twoFAEnabled == nil ? "–" : (protected ? "Protected" : "2FA off")) {
                        if store.twoFAEnabled != nil {
                            Circle()
                                .fill(protected ? WTheme.positive : WTheme.warning)
                                .frame(width: 7, height: 7)
                        }
                    }
                }
                .buttonStyle(MoreRowPressStyle())
            }
            .fixedSize(horizontal: false, vertical: true)
        }
        .background(WTheme.groupedCard)
        .clipShape(RoundedRectangle(cornerRadius: 22, style: .continuous))
        .animation(.snappy, value: store.hasLoaded)
    }

    private var cellDivider: some View {
        Rectangle().fill(WTheme.hairline).frame(width: 1).padding(.vertical, 12)
    }

    /// The workspace cell takes the spare width; the others fit their text.
    private func statusCell(_ caption: String, _ value: String, grows: Bool = false, @ViewBuilder leading: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(caption)
                .font(.system(size: 12))
                .foregroundStyle(WTheme.inkSecondary)
            HStack(spacing: 6) {
                leading()
                Text(value)
                    .font(.system(size: 14.5, weight: .medium))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                    .contentTransition(.opacity)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: grows ? .infinity : nil, alignment: .leading)
        .fixedSize(horizontal: !grows, vertical: false)
        .contentShape(Rectangle())
    }

    // MARK: Workspace tiles

    @ViewBuilder
    private func workspace(_ access: HubAccess) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline) {
                Text("Your workspace")
                    .displayFont(22, weight: .semibold)
                    .foregroundStyle(WTheme.ink)
                Spacer()
                PresenceAvatars()
            }
            .padding(.horizontal, 4)
            LazyVGrid(columns: [GridItem(.flexible(), spacing: 12), GridItem(.flexible(), spacing: 12)], spacing: 12) {
                ForEach(Array(tiles(access).enumerated()), id: \.element.id) { index, tile in
                    tileButton(tile)
                        .reveal(appeared, index)
                }
            }
        }
    }

    private struct Tile: Identifiable {
        let id: String
        let icon: String
        let color: Color
        let title: String
        var metric: String?
        var caption: String?
        var captionColor: Color?
        var spark: [Double]?
        var target: Target

        enum Target {
            case push(SettingsDestination)
            case cover(SettingsCover)
        }
    }

    private func tiles(_ access: HubAccess) -> [Tile] {
        var tiles: [Tile] = []
        if access.emails {
            tiles.append(Tile(
                id: "mailboxes", icon: "envelope.fill", color: SettingsPalette.mailboxes, title: "Mailboxes",
                metric: store.mailboxCount.map(String.init), caption: store.mailboxCaption?.text,
                captionColor: store.mailboxCaption?.color, target: .cover(.mailboxes)
            ))
        }
        if access.analytics {
            tiles.append(Tile(
                id: "analytics", icon: "chart.bar.fill", color: SettingsPalette.analytics, title: "Analytics",
                metric: store.hasLoaded ? WFormat.compact(store.weekSentTotal) : nil, caption: "sent this week",
                spark: store.weekSent, target: .cover(.analytics)
            ))
        }
        if access.crm {
            tiles.append(Tile(
                id: "deals", icon: "briefcase.fill", color: SettingsPalette.deals, title: "Deals",
                metric: store.deals.map { "\($0.openCount ?? 0)" }, caption: store.dealsCaption, target: .cover(.deals)
            ))
            tiles.append(Tile(
                id: "tasks", icon: "checklist", color: SettingsPalette.tasks, title: "Tasks",
                metric: store.tasks.map { "\($0.openCount)" }, caption: store.tasksCaption?.text,
                captionColor: store.tasksCaption?.color, target: .cover(.tasks)
            ))
            tiles.append(Tile(
                id: "meetings", icon: "calendar", color: SettingsPalette.meetings, title: "Meetings",
                metric: store.meetings.map { "\($0.upcoming ?? 0)" },
                caption: store.meetings.map { ($0.today ?? 0) > 0 ? "\($0.today ?? 0) today" : "None today" },
                target: .cover(.meetings)
            ))
        }
        if access.campaigns {
            tiles.append(Tile(
                id: "templates", icon: "doc.on.doc.fill", color: SettingsPalette.templates, title: "Templates",
                metric: store.templateCount.map(String.init), caption: "reply templates", target: .push(.templates)
            ))
        }
        let members = env.session.currentOrg?.counts?.totalMembers
        tiles.append(Tile(
            id: "members", icon: "person.2.fill", color: SettingsPalette.members, title: "Members",
            metric: members.map(String.init), caption: "\(env.realtime.presence.onlineCount) online now",
            captionColor: WTheme.positive, target: .push(.team)
        ))
        if access.analytics {
            tiles.append(Tile(
                id: "activity", icon: "clock.arrow.circlepath", color: SettingsPalette.activity, title: "Activity",
                metric: nil, caption: "Audit log, last 90 days", target: .push(.activity)
            ))
        }
        return tiles
    }

    @ViewBuilder
    private func tileButton(_ tile: Tile) -> some View {
        switch tile.target {
        case let .push(destination):
            NavigationLink(value: destination) { tileFace(tile) }
                .buttonStyle(TapScaleStyle())
        case let .cover(cover):
            Button { ctx.present(cover) } label: { tileFace(tile) }
                .buttonStyle(TapScaleStyle())
        }
    }

    private func tileFace(_ tile: Tile) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .top) {
                SettingsIcon(symbol: tile.icon, color: tile.color, size: 34)
                Spacer(minLength: 4)
                if let spark = tile.spark, spark.contains(where: { $0 > 0 }) {
                    Sparkline(values: spark, color: tile.color, lineWidth: 1.6, delay: 0.3)
                        .frame(width: 62, height: 28)
                }
            }
            Spacer(minLength: 14)
            if let metric = tile.metric {
                Text(metric)
                    .font(.system(size: 28, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.ink)
                    .contentTransition(.numericText())
                    .lineLimit(1)
            }
            Text(tile.title)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(WTheme.ink)
                .padding(.top, tile.metric == nil ? 0 : 1)
            Text(tile.caption ?? " ")
                .font(.system(size: 12.5, weight: tile.captionColor == nil ? .regular : .medium))
                .foregroundStyle(tile.captionColor ?? WTheme.inkSecondary)
                .lineLimit(1)
                .padding(.top, 2)
        }
        .padding(14)
        .frame(maxWidth: .infinity, minHeight: 148, alignment: .topLeading)
        .background(WTheme.groupedCard, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
        .animation(.snappy, value: tile.metric)
    }

    // MARK: Preferences

    private func preferences(_ access: HubAccess) -> some View {
        SettingsSection("Preferences") {
            NavigationLink(value: SettingsDestination.profile) {
                SettingsRow(icon: "person.crop.circle.fill", color: SettingsPalette.profile, title: "Profile", subtitle: "Name, password, devices")
            }
            .buttonStyle(MoreRowPressStyle())
            NavigationLink(value: SettingsDestination.notifications) {
                SettingsRow(icon: "bell.badge.fill", color: SettingsPalette.notifications, title: "Notifications", badge: store.unreadNotifications)
            }
            .buttonStyle(MoreRowPressStyle())
            NavigationLink(value: SettingsDestination.security) {
                SettingsRow(
                    icon: "lock.shield.fill", color: SettingsPalette.security, title: "Security",
                    value: store.securityValue.text.isEmpty ? nil : store.securityValue.text,
                    valueColor: store.securityValue.color, dot: store.securityValue.text.isEmpty ? nil : store.securityValue.color
                )
            }
            .buttonStyle(MoreRowPressStyle())
            if access.billing {
                NavigationLink(value: SettingsDestination.billing) {
                    SettingsRow(icon: "creditcard.fill", color: SettingsPalette.billing, title: "Billing", value: store.trialLabel ?? store.planName())
                }
                .buttonStyle(MoreRowPressStyle())
            }
        }
        .reveal(appeared, 7)
    }
}
