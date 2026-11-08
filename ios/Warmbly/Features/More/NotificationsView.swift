import SwiftUI
import UIKit

@MainActor
@Observable
final class MoreNotificationsStore {
    var items: [UserNotification] = []
    var unread = 0
    var preferences: [String: MoreCategoryPref] = [:]
    var emailDigestMinutes = 30
    var loadedOnce = false
    var isLoading = false
    var errorMessage: String?
    var actionError: String?

    func load(_ api: APIClient) async {
        guard !isLoading else { return }
        isLoading = true
        errorMessage = nil
        do {
            let feed: MoreNotificationFeed = try await api.get(
                "auth/me/notifications",
                query: ["limit": "50"]
            )
            items = feed.notifications ?? []
            unread = feed.unread ?? 0
            PushManager.shared.setBadge(unread)
            let envelope: MoreNotificationPreferencesEnvelope = try await api.get("auth/me/notification-preferences")
            preferences = envelope.preferences?.categories ?? [:]
            emailDigestMinutes = envelope.preferences?.emailDigestMinutes ?? 30
            loadedOnce = true
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    func markRead(_ id: String, _ api: APIClient) async {
        guard let index = items.firstIndex(where: { $0.id == id }), items[index].isUnread else { return }
        items[index].readAt = Date()
        unread = max(0, unread - 1)
        PushManager.shared.setBadge(unread)
        let _: MoreOkResponse? = try? await api.post("auth/me/notifications/\(id)/read")
    }

    func markAllRead(_ api: APIClient) async {
        let now = Date()
        for index in items.indices where items[index].readAt == nil {
            items[index].readAt = now
        }
        unread = 0
        PushManager.shared.setBadge(0)
        // PUT on the collection marks everything read; the handler ignores the body.
        let _: MoreOkResponse? = try? await api.put("auth/me/notifications", body: EmptyBody())
    }

    /// Autosaves the full preferences object, mirroring the web settings page.
    func setEnabled(_ key: String, _ enabled: Bool, _ api: APIClient) async {
        let previous = preferences
        var pref = preferences[key] ?? MoreCategoryPref(
            enabled: enabled,
            channels: MoreChannelPrefs(inApp: true, email: false, slack: false, push: true)
        )
        pref.enabled = enabled
        preferences[key] = pref
        await persist(previous: previous, api)
    }

    /// Email bundling window, saved through the same PUT as the toggles.
    func setEmailWindow(_ minutes: Int, _ api: APIClient) async {
        guard minutes != emailDigestMinutes else { return }
        let previous = preferences
        let previousWindow = emailDigestMinutes
        emailDigestMinutes = minutes
        await persist(previous: previous, previousWindow: previousWindow, api)
    }

    /// Global channel state, like the web settings page: a channel reads "on"
    /// only when every category delivers to it.
    func channelOn(_ channel: WritableKeyPath<MoreChannelPrefs, Bool?>) -> Bool {
        !preferences.isEmpty && preferences.values.allSatisfy { $0.channels?[keyPath: channel] == true }
    }

    /// Flips a delivery channel across every category at once (the web has the
    /// same all-or-nothing toggle; per-category channel splits stay web/API-only).
    func setChannel(_ channel: WritableKeyPath<MoreChannelPrefs, Bool?>, _ on: Bool, _ api: APIClient) async {
        let previous = preferences
        for key in preferences.keys {
            var pref = preferences[key] ?? MoreCategoryPref(enabled: false, channels: nil)
            var channels = pref.channels ?? MoreChannelPrefs(inApp: true, email: false, slack: false, push: true)
            channels[keyPath: channel] = on
            pref.channels = channels
            preferences[key] = pref
        }
        await persist(previous: previous, api)
    }

    private func persist(previous: [String: MoreCategoryPref], previousWindow: Int? = nil, _ api: APIClient) async {
        do {
            let body = MorePreferencesBody(
                preferences: NotificationPreferences(categories: preferences, emailDigestMinutes: emailDigestMinutes)
            )
            let echoed: MoreNotificationPreferencesEnvelope = try await api.put(
                "auth/me/notification-preferences",
                body: body
            )
            if let echoedPrefs = echoed.preferences {
                preferences = echoedPrefs.categories
                emailDigestMinutes = echoedPrefs.emailDigestMinutes
            }
        } catch {
            preferences = previous
            if let previousWindow { emailDigestMinutes = previousWindow }
            actionError = error.localizedDescription
        }
    }
}

private enum MoreNotificationMeta {
    static func icon(_ category: String?) -> String {
        switch category {
        case "inbound_reply": return "arrowshape.turn.up.left.fill"
        case "inbound_out_of_office": return "moon.zzz.fill"
        case "health_bounce": return "arrow.uturn.left.circle.fill"
        case "health_complaint": return "flag.fill"
        case "health_worker_downtime": return "bolt.slash.fill"
        case "security_new_signin": return "lock.shield.fill"
        case "billing_alert": return "creditcard.fill"
        case "team_activity": return "person.2.fill"
        default: return "bell.fill"
        }
    }

    static func tone(_ category: String?) -> Tone {
        switch category {
        case "inbound_reply": return .emerald
        case "inbound_out_of_office": return .slate
        case "health_bounce": return .rose
        case "health_complaint": return .rose
        case "health_worker_downtime": return .amber
        case "security_new_signin": return .indigo
        case "billing_alert": return .orange
        case "team_activity": return .sky
        default: return .sky
        }
    }

    static func label(_ key: String) -> String {
        switch key {
        case "inbound_reply": return "Replies"
        case "inbound_out_of_office": return "Out of office"
        case "health_bounce": return "Bounces"
        case "health_complaint": return "Spam complaints"
        case "health_worker_downtime": return "Worker downtime"
        case "security_new_signin": return "New sign-ins"
        case "billing_alert": return "Billing"
        case "team_activity": return "Team"
        default:
            let pretty = key.replacingOccurrences(of: "_", with: " ")
            return pretty.prefix(1).uppercased() + pretty.dropFirst()
        }
    }

    static func caption(_ key: String) -> String? {
        switch key {
        case "inbound_reply": return "A contact replies to your outreach"
        case "inbound_out_of_office": return "Auto-replies detected on your sends"
        case "health_bounce": return "A campaign email hard bounces"
        case "health_complaint": return "A recipient marks your mail as spam"
        case "health_worker_downtime": return "A sending worker goes offline"
        case "security_new_signin": return "Your account signs in from a new device"
        case "billing_alert": return "Your trial is ending or a payment needs attention"
        case "team_activity": return "A teammate joined your workspace"
        default: return nil
        }
    }

    static let knownGroups: [(title: String, keys: [String])] = [
        ("Inbound", ["inbound_reply", "inbound_out_of_office"]),
        ("Mailbox health", ["health_bounce", "health_complaint", "health_worker_downtime"]),
        ("Security", ["security_new_signin"]),
        ("Account", ["billing_alert", "team_activity"]),
    ]

    // Bundling-window presets in minutes. The 30 minute floor mirrors the
    // backend: there is no per-event email mode.
    static let windowOptions: [(minutes: Int, label: String, caption: String)] = [
        (30, "Every 30 minutes", "The fastest option. Skips anything you already read."),
        (60, "Every hour", "At most one bundled email per hour."),
        (180, "Every 3 hours", "A few bundles across a working day."),
        (360, "Every 6 hours", "Morning, afternoon, evening."),
        (720, "Every 12 hours", "Twice a day at most."),
        (1440, "Once a day", "One daily summary of everything unread."),
    ]

    static func windowLabel(_ minutes: Int) -> String {
        if let match = windowOptions.first(where: { $0.minutes == minutes }) {
            return match.label
        }
        if minutes % 60 == 0 {
            return "Every \(minutes / 60) hours"
        }
        return "Every \(minutes) minutes"
    }

    static func windowCaption(_ minutes: Int) -> String {
        windowOptions.first(where: { $0.minutes == minutes })?.caption
            ?? "Bundles everything unread into one email per window."
    }
}

/// Everything Warmbly told you, grouped by day with the unread ones marked, and
/// what it may tell you and where, as switches.
struct NotificationsView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = MoreNotificationsStore()
    @State private var tab = Tab.feed
    @Namespace private var tabSpace

    private enum Tab: String, CaseIterable {
        case feed = "Inbox"
        case preferences = "Preferences"
    }

    var body: some View {
        content
            .safeAreaInset(edge: .top, spacing: 0) { tabSwitcher }
            .background(WTheme.groupedPage)
            .navigationTitle("Notifications")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(WTheme.groupedPage, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    if tab == .feed, store.unread > 0 {
                        Button("Read all") {
                            Task { await store.markAllRead(env.api) }
                        }
                    }
                }
            }
            .task {
                #if DEBUG
                if ProcessInfo.processInfo.environment["WARMBLY_DEBUG_NOTIFICATIONS"] == "preferences" { tab = .preferences }
                #endif
                await store.load(env.api)
                await PushManager.shared.refreshAuthorization()
            }
            .onChange(of: env.realtime.pulse(for: .notifications)) {
                Task { await store.load(env.api) }
            }
            .alert(
                "Couldn't save preferences",
                isPresented: Binding(
                    get: { store.actionError != nil },
                    set: { if !$0 { store.actionError = nil } }
                )
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(store.actionError ?? "")
            }
    }

    private var tabSwitcher: some View {
        HStack(spacing: 4) {
            ForEach(Tab.allCases, id: \.self) { option in
                Button {
                    withAnimation(.snappy) { tab = option }
                } label: {
                    HStack(spacing: 6) {
                        Text(option.rawValue)
                            .font(.system(size: 14.5, weight: tab == option ? .semibold : .medium))
                        if option == .feed, store.unread > 0 {
                            Text("\(store.unread)")
                                .font(.system(size: 11.5, weight: .bold))
                                .monospacedDigit()
                                .foregroundStyle(.white)
                                .padding(.horizontal, 6)
                                .frame(minWidth: 18, minHeight: 18)
                                .background(WTheme.negative, in: Capsule())
                                .contentTransition(.numericText())
                        }
                    }
                    .foregroundStyle(tab == option ? WTheme.ink : WTheme.inkSecondary)
                    .frame(maxWidth: .infinity)
                    .frame(height: 34)
                    .background {
                        if tab == option {
                            Capsule()
                                .fill(WTheme.groupedCard)
                                .shadow(color: .black.opacity(0.08), radius: 4, y: 1)
                                .matchedGeometryEffect(id: "tab", in: tabSpace)
                        }
                    }
                    .contentShape(Capsule())
                }
                .buttonStyle(.plain)
            }
        }
        .padding(3)
        .background(WTheme.fill, in: Capsule())
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .background(WTheme.groupedPage)
        .sensoryFeedback(.selection, trigger: tab)
    }

    @ViewBuilder
    private var content: some View {
        if !store.loadedOnce {
            if let message = store.errorMessage {
                ErrorStateView(title: "Couldn't load notifications", message: message) {
                    await store.load(env.api)
                }
            } else {
                SkeletonRows()
            }
        } else {
            ScrollView {
                Group {
                    switch tab {
                    case .feed: feed
                    case .preferences: preferences
                    }
                }
                .padding(.horizontal, 16)
                .padding(.top, 8)
                .padding(.bottom, 120)
                .transition(.opacity)
            }
            .scrollIndicators(.hidden)
            .refreshable { await store.load(env.api) }
            .readableMargins()
        }
    }

    // MARK: Feed

    private var dayGroups: [(title: String, items: [UserNotification])] {
        let calendar = Calendar.current
        var groups: [(title: String, items: [UserNotification])] = []
        for item in store.items {
            let date = item.createdAt ?? .distantPast
            let title: String
            if calendar.isDateInToday(date) {
                title = "Today"
            } else if calendar.isDateInYesterday(date) {
                title = "Yesterday"
            } else if let week = calendar.date(byAdding: .day, value: -7, to: .now), date > week {
                title = "This week"
            } else {
                title = "Earlier"
            }
            if groups.last?.title == title {
                groups[groups.count - 1].items.append(item)
            } else {
                groups.append((title, [item]))
            }
        }
        return groups
    }

    @ViewBuilder
    private var feed: some View {
        if store.items.isEmpty {
            VStack(spacing: 0) {
                SettingsHero(
                    symbol: "bell.badge.fill",
                    color: SettingsPalette.notifications,
                    title: "All caught up",
                    subtitle: "Replies, deliverability alerts and sign-in notices land here."
                )
            }
            .padding(.top, 60)
        } else {
            VStack(alignment: .leading, spacing: 26) {
                ForEach(dayGroups, id: \.title) { group in
                    SettingsSection(group.title, dividerLeading: 62) {
                        ForEach(group.items) { item in
                            feedRow(item)
                        }
                    }
                }
            }
        }
    }

    private func feedRow(_ item: UserNotification) -> some View {
        let tone = MoreNotificationMeta.tone(item.category)
        return Button {
            Task { await store.markRead(item.id, env.api) }
        } label: {
            HStack(alignment: .top, spacing: 12) {
                SettingsIcon(symbol: MoreNotificationMeta.icon(item.category), color: tone.color, size: 34)
                VStack(alignment: .leading, spacing: 3) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(item.title ?? "Notification")
                            .font(.system(size: 15.5, weight: item.isUnread ? .semibold : .regular))
                            .foregroundStyle(WTheme.ink)
                            .lineLimit(2)
                        Spacer(minLength: 4)
                        if let created = item.createdAt {
                            HStack(spacing: 5) {
                                Text(WFormat.ago(created))
                                    .font(.system(size: 13))
                                    .monospacedDigit()
                                    .foregroundStyle(item.isUnread ? WTheme.accent : WTheme.inkTertiary)
                                if item.isUnread {
                                    Circle().fill(WTheme.accent).frame(width: 8, height: 8)
                                        .transition(.scale.combined(with: .opacity))
                                }
                            }
                        }
                    }
                    if let body = item.body, !body.isEmpty {
                        Text(body)
                            .font(.system(size: 14))
                            .foregroundStyle(WTheme.inkSecondary)
                            .lineLimit(3)
                            .multilineTextAlignment(.leading)
                    }
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 12)
            .contentShape(Rectangle())
        }
        .buttonStyle(MoreRowPressStyle())
        .animation(.snappy, value: item.isUnread)
    }

    // MARK: Preferences

    private var extraKeys: [String] {
        let known = Set(MoreNotificationMeta.knownGroups.flatMap(\.keys))
        return store.preferences.keys.filter { !known.contains($0) }.sorted()
    }

    private var preferences: some View {
        VStack(alignment: .leading, spacing: 28) {
            SettingsSection("Delivered to", footer: "Each channel applies to every kind of alert you keep on below.") {
                SettingsRow(
                    icon: "app.badge.fill", color: WTheme.accent, title: "In the app",
                    value: "Always", accessory: .none
                )
                SettingsToggleRow(
                    icon: "iphone.radiowaves.left.and.right", color: SettingsPalette.notifications, title: "Push",
                    subtitle: PushManager.shared.authorization == .denied ? "Off for Warmbly in iOS Settings" : "The first alert right away, a burst as one summary",
                    isOn: channelBinding(\.push),
                    disabled: PushManager.shared.authorization == .denied
                )
                if PushManager.shared.authorization == .denied {
                    Button {
                        if let url = URL(string: UIApplication.openNotificationSettingsURLString) {
                            UIApplication.shared.open(url)
                        }
                    } label: {
                        SettingsRow(icon: "gearshape.fill", color: SettingsPalette.profile, title: "Turn on in iOS Settings", accessory: .external)
                    }
                    .buttonStyle(MoreRowPressStyle())
                }
                SettingsToggleRow(
                    icon: "envelope.fill", color: SettingsPalette.templates, title: "Email",
                    subtitle: "Bundled, and never for alerts you already read",
                    isOn: channelBinding(\.email)
                )
                SettingsToggleRow(
                    icon: "number", color: SettingsPalette.members, title: "Slack",
                    subtitle: "To the channel set up in Integrations on the web",
                    isOn: channelBinding(\.slack)
                )
            }

            ForEach(MoreNotificationMeta.knownGroups, id: \.title) { group in
                let present = group.keys.filter { store.preferences[$0] != nil }
                if !present.isEmpty {
                    SettingsSection(group.title) {
                        ForEach(present, id: \.self) { key in
                            preferenceRow(key)
                        }
                    }
                }
            }
            if !extraKeys.isEmpty {
                SettingsSection("Other") {
                    ForEach(extraKeys, id: \.self) { key in
                        preferenceRow(key)
                    }
                }
            }

            SettingsSection(
                "Email bundling",
                footer: "\(MoreNotificationMeta.windowCaption(store.emailDigestMinutes)) Sign-in alerts always send right away."
            ) {
                Menu {
                    Picker("Bundling window", selection: windowBinding) {
                        ForEach(windowChoices, id: \.self) { minutes in
                            Text(MoreNotificationMeta.windowLabel(minutes)).tag(minutes)
                        }
                    }
                } label: {
                    SettingsRow(
                        icon: "tray.full.fill", color: SettingsPalette.templates, title: "Send at most",
                        value: MoreNotificationMeta.windowLabel(store.emailDigestMinutes).replacingOccurrences(of: "Every ", with: "every "),
                        accessory: .menu
                    )
                }
                .buttonStyle(MoreRowPressStyle())
            }
        }
    }

    private func preferenceRow(_ key: String) -> some View {
        SettingsToggleRow(
            icon: MoreNotificationMeta.icon(key),
            color: MoreNotificationMeta.tone(key).color,
            title: MoreNotificationMeta.label(key),
            subtitle: MoreNotificationMeta.caption(key),
            isOn: preferenceBinding(key)
        )
    }

    private func preferenceBinding(_ key: String) -> Binding<Bool> {
        Binding(
            get: { store.preferences[key]?.enabled ?? false },
            set: { newValue in
                Task { await store.setEnabled(key, newValue, env.api) }
            }
        )
    }

    // Preset windows, plus the server value when it matches none (set to a
    // custom window on the web) so the picker never lies about the state.
    private var windowChoices: [Int] {
        var choices = MoreNotificationMeta.windowOptions.map(\.minutes)
        if !choices.contains(store.emailDigestMinutes) {
            choices.append(store.emailDigestMinutes)
            choices.sort()
        }
        return choices
    }

    private var windowBinding: Binding<Int> {
        Binding(
            get: { store.emailDigestMinutes },
            set: { newValue in
                Task { await store.setEmailWindow(newValue, env.api) }
            }
        )
    }

    private func channelBinding(_ channel: WritableKeyPath<MoreChannelPrefs, Bool?>) -> Binding<Bool> {
        Binding(
            get: { store.channelOn(channel) },
            set: { newValue in
                Task { await store.setChannel(channel, newValue, env.api) }
            }
        )
    }
}
