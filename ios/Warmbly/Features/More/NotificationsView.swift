import SwiftUI
import UIKit

@MainActor
@Observable
final class MoreNotificationsStore {
    var items: [UserNotification] = []
    var unread = 0
    var preferences: [String: MoreCategoryPref] = [:]
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

    private func persist(previous: [String: MoreCategoryPref], _ api: APIClient) async {
        do {
            let body = MorePreferencesBody(preferences: NotificationPreferences(categories: preferences))
            let echoed: MoreNotificationPreferencesEnvelope = try await api.put(
                "auth/me/notification-preferences",
                body: body
            )
            if let categories = echoed.preferences?.categories {
                preferences = categories
            }
        } catch {
            preferences = previous
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
        default: return nil
        }
    }

    static let knownGroups: [(title: String, keys: [String])] = [
        ("Inbound", ["inbound_reply", "inbound_out_of_office"]),
        ("Mailbox health", ["health_bounce", "health_complaint", "health_worker_downtime"]),
        ("Security", ["security_new_signin"]),
    ]
}

/// In-app notification feed plus per-category preferences, rendered as one
/// flat surface: dense tile-led feed rows with hairline separators, eyebrow
/// captions instead of grouped bands.
struct NotificationsView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = MoreNotificationsStore()
    @State private var tab = 0

    var body: some View {
        VStack(spacing: 0) {
            Picker("View", selection: $tab) {
                Text("Feed").tag(0)
                Text("Preferences").tag(1)
            }
            .pickerStyle(.segmented)
            .padding(.horizontal, 16)
            .padding(.bottom, 8)
            content
        }
        .background(Color(.systemBackground))
        .navigationTitle("Notifications")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                if tab == 0, store.unread > 0 {
                    Button("Mark all read") {
                        Task { await store.markAllRead(env.api) }
                    }
                }
            }
        }
        .task {
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
        } else if tab == 0 {
            feedList
        } else {
            preferencesList
        }
    }

    // MARK: Feed

    @ViewBuilder
    private var feedList: some View {
        if store.items.isEmpty {
            List {
                EmptyStateView(
                    title: "All caught up",
                    message: "Replies, deliverability alerts, and sign-in notices land here."
                )
                .listRowSeparator(.hidden)
                .listRowBackground(Color(.systemBackground))
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .refreshable { await store.load(env.api) }
        } else {
            List {
                ForEach(store.items) { item in
                    feedRow(item)
                        .moreFlatRow(textLeading: MoreFlatMetrics.tileTextLeading)
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .environment(\.defaultMinListRowHeight, 0)
            .refreshable { await store.load(env.api) }
        }
    }

    private func feedRow(_ item: UserNotification) -> some View {
        let tone = MoreNotificationMeta.tone(item.category)
        return Button {
            Task { await store.markRead(item.id, env.api) }
        } label: {
            HStack(alignment: .top, spacing: 12) {
                IconTile(symbol: MoreNotificationMeta.icon(item.category), tone: tone, size: 34)
                VStack(alignment: .leading, spacing: 2) {
                    Text(item.title ?? "Notification")
                        .font(.subheadline.weight(item.isUnread ? .semibold : .regular))
                        .foregroundStyle(.primary)
                        .lineLimit(2)
                    if let body = item.body, !body.isEmpty {
                        Text(body)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                    }
                }
                Spacer(minLength: 8)
                VStack(alignment: .trailing, spacing: 6) {
                    if let created = item.createdAt {
                        Text(WFormat.relative(created))
                            .font(.footnote)
                            .monospacedDigit()
                            .foregroundStyle(item.isUnread ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(Color(.tertiaryLabel)))
                    }
                    if item.isUnread {
                        Circle()
                            .fill(WTheme.accent)
                            .frame(width: 7, height: 7)
                    }
                }
                .padding(.top, 2)
            }
            .padding(.vertical, 10)
            .contentShape(Rectangle())
        }
        .buttonStyle(TapScaleStyle())
    }

    // MARK: Preferences

    private var extraKeys: [String] {
        let known = Set(MoreNotificationMeta.knownGroups.flatMap(\.keys))
        return store.preferences.keys.filter { !known.contains($0) }.sorted()
    }

    private var preferencesList: some View {
        List {
            ForEach(MoreNotificationMeta.knownGroups, id: \.title) { group in
                let present = group.keys.filter { store.preferences[$0] != nil }
                if !present.isEmpty {
                    MoreFlatSectionHeader(group.title, top: group.title == MoreNotificationMeta.knownGroups.first?.title ? 8 : 20)
                    ForEach(present, id: \.self) { key in
                        preferenceRow(key)
                    }
                }
            }
            if !extraKeys.isEmpty {
                MoreFlatSectionHeader("Other")
                ForEach(extraKeys, id: \.self) { key in
                    preferenceRow(key)
                }
            }
            MoreFlatSectionHeader("Channels")
            channelRow(
                symbol: "app.badge.fill",
                tone: .sky,
                label: "In-app",
                caption: "The feed above; always on for enabled categories."
            ) {
                Text("On")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(Tone.emerald.color)
            }
            channelRow(
                symbol: "iphone.gen3.radiowaves.left.and.right",
                tone: .rose,
                label: "Push",
                caption: "Alerts on this device. The first event pushes right away; bursts arrive as one summary instead of a ping per event."
            ) {
                Toggle("", isOn: channelBinding(\.push))
                    .labelsHidden()
                    .tint(WTheme.accent)
                    .disabled(PushManager.shared.authorization == .denied)
            }
            if PushManager.shared.authorization == .denied {
                HStack(spacing: 6) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .font(.caption)
                        .foregroundStyle(Tone.amber.color)
                    Text("Notifications are off for Warmbly in iOS Settings.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    Spacer(minLength: 8)
                    Button("Open Settings") {
                        if let url = URL(string: UIApplication.openNotificationSettingsURLString) {
                            UIApplication.shared.open(url)
                        }
                    }
                    .font(.footnote.weight(.medium))
                }
                .padding(.vertical, 6)
                .moreFlatRow(textLeading: MoreFlatMetrics.tileTextLeading)
            }
            channelRow(
                symbol: "envelope.fill",
                tone: .indigo,
                label: "Email",
                caption: "Delivery to your account email."
            ) {
                Toggle("", isOn: channelBinding(\.email))
                    .labelsHidden()
                    .tint(WTheme.accent)
            }
            channelRow(
                symbol: "bubble.left.and.bubble.right.fill",
                tone: .emerald,
                label: "Slack",
                caption: "Posts to the Slack channel set up in Integrations on the web."
            ) {
                Toggle("", isOn: channelBinding(\.slack))
                    .labelsHidden()
                    .tint(WTheme.accent)
            }
            Text("Channels apply across every enabled category above.")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.top, 16)
                .padding(.bottom, 28)
                .moreFlatRow(separator: .hidden)
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .environment(\.defaultMinListRowHeight, 0)
        .refreshable { await store.load(env.api) }
    }

    private func preferenceRow(_ key: String) -> some View {
        Toggle(isOn: preferenceBinding(key)) {
            HStack(spacing: 12) {
                IconTile(
                    symbol: MoreNotificationMeta.icon(key),
                    tone: store.preferences[key]?.enabled == true ? MoreNotificationMeta.tone(key) : .slate,
                    size: 34
                )
                VStack(alignment: .leading, spacing: 2) {
                    Text(MoreNotificationMeta.label(key))
                        .font(.body.weight(.medium))
                    if let caption = MoreNotificationMeta.caption(key) {
                        Text(caption)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .tint(WTheme.accent)
        .padding(.vertical, 9)
        .moreFlatRow(textLeading: MoreFlatMetrics.tileTextLeading)
    }

    private func preferenceBinding(_ key: String) -> Binding<Bool> {
        Binding(
            get: { store.preferences[key]?.enabled ?? false },
            set: { newValue in
                Task { await store.setEnabled(key, newValue, env.api) }
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

    private func channelRow(
        symbol: String,
        tone: Tone,
        label: String,
        caption: String,
        @ViewBuilder trailing: () -> some View
    ) -> some View {
        HStack(spacing: 12) {
            IconTile(symbol: symbol, tone: tone, size: 34)
            VStack(alignment: .leading, spacing: 2) {
                Text(label)
                    .font(.body.weight(.medium))
                Text(caption)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            trailing()
        }
        .padding(.vertical, 9)
        .moreFlatRow(textLeading: MoreFlatMetrics.tileTextLeading)
    }
}
