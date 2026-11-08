import SwiftUI

// MARK: - Store

/// Cursor-paged audit trail. Realtime pulses merge new entries at the top
/// (the feed is append-only) so scroll position and loaded pages survive.
@MainActor
@Observable
final class AuditLogStore {
    private(set) var entries: [AuditLogEntry] = []
    private(set) var isLoading = false
    private(set) var isLoadingMore = false
    private(set) var hasLoaded = false
    private(set) var errorMessage: String?
    private(set) var nextCursor: String?
    private(set) var hasMore = false
    /// Entries that just arrived live, lit briefly in the feed.
    private(set) var fresh: Set<String> = []

    var actionFilter: String?
    var entityFilter: String?

    var hasFilters: Bool { actionFilter != nil || entityFilter != nil }

    private func query(cursor: String?) -> [String: String?] {
        [
            "limit": "50",
            "cursor": cursor,
            "action": actionFilter,
            "entity_type": entityFilter,
        ]
    }

    func load(_ api: APIClient) async {
        if isLoading { return }
        isLoading = true
        defer { isLoading = false }
        do {
            let page: AuditLogsPage = try await api.get("audit-logs", query: query(cursor: nil))
            entries = page.data ?? []
            nextCursor = page.pagination?.nextCursor
            hasMore = page.pagination?.hasMore ?? false
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
        hasLoaded = true
    }

    func loadMore(_ api: APIClient) async {
        guard !isLoading, !isLoadingMore, hasMore, let cursor = nextCursor else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page: AuditLogsPage = try await api.get("audit-logs", query: query(cursor: cursor))
            let known = Set(entries.map(\.id))
            entries.append(contentsOf: (page.data ?? []).filter { !known.contains($0.id) })
            nextCursor = page.pagination?.nextCursor
            hasMore = page.pagination?.hasMore ?? false
        } catch {
            // Keep what we have; the load-more row stays available for retry.
        }
    }

    /// Realtime pulse: prepend unseen entries without resetting pagination.
    func refreshTop(_ api: APIClient) async {
        guard hasLoaded, !isLoading else {
            await load(api)
            return
        }
        do {
            let page: AuditLogsPage = try await api.get("audit-logs", query: query(cursor: nil))
            let fresh = page.data ?? []
            if entries.isEmpty {
                entries = fresh
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            } else {
                let known = Set(entries.map(\.id))
                let new = fresh.filter { !known.contains($0.id) }
                if !new.isEmpty {
                    withAnimation(.spring(response: 0.5, dampingFraction: 0.82)) {
                        entries.insert(contentsOf: new, at: 0)
                        self.fresh.formUnion(new.map(\.id))
                    }
                    let ids = Set(new.map(\.id))
                    Task {
                        try? await Task.sleep(for: .seconds(2.4))
                        withAnimation(.easeOut(duration: 0.8)) { self.fresh.subtract(ids) }
                    }
                }
            }
            errorMessage = nil
        } catch {
            // Silent; the visible feed is still valid.
        }
    }

    func clearFilters() {
        actionFilter = nil
        entityFilter = nil
    }

    // MARK: Day grouping

    struct AuditDaySection: Identifiable {
        let id: Date
        let label: String
        let entries: [AuditLogEntry]
    }

    var sections: [AuditDaySection] {
        let calendar = Calendar.current
        var order: [Date] = []
        var buckets: [Date: [AuditLogEntry]] = [:]
        for entry in entries {
            let day = calendar.startOfDay(for: entry.when ?? Date())
            if buckets[day] == nil { order.append(day) }
            buckets[day, default: []].append(entry)
        }
        return order.map { day in
            AuditDaySection(id: day, label: AuditFmt.dayLabel(day), entries: buckets[day] ?? [])
        }
    }
}

// MARK: - Formatting and tone maps

enum AuditFmt {
    private static let sameYear: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "EEE, MMM d"
        return formatter
    }()

    private static let otherYear: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "MMM d, yyyy"
        return formatter
    }()

    static func dayLabel(_ day: Date) -> String {
        let calendar = Calendar.current
        if calendar.isDateInToday(day) { return "Today" }
        if calendar.isDateInYesterday(day) { return "Yesterday" }
        if calendar.isDate(day, equalTo: Date(), toGranularity: .year) {
            return sameYear.string(from: day)
        }
        return otherYear.string(from: day)
    }

    /// "rotate_keys" -> "Rotate keys"
    static func humanize(_ raw: String?) -> String {
        guard let raw, !raw.isEmpty else { return "unknown" }
        let spaced = raw.replacingOccurrences(of: "_", with: " ")
        return spaced.prefix(1).uppercased() + spaced.dropFirst()
    }

    static func actionTone(_ raw: String?) -> Tone {
        switch raw {
        case "create", "connect", "install", "invite", "start", "resume", "apply", "import":
            .emerald
        case "delete", "remove", "revoke", "uninstall", "disconnect", "stop":
            .rose
        case "pause", "transfer", "rotate", "rotate_keys", "reboot":
            .amber
        case "update", "send", "duplicate", "export", "test", "assign":
            .sky
        default:
            .slate
        }
    }

    static func actionIcon(_ raw: String?) -> String {
        switch raw {
        case "create": "plus"
        case "update": "pencil"
        case "delete": "trash"
        case "send": "paperplane"
        case "start", "resume": "play"
        case "stop": "stop"
        case "pause": "pause"
        case "invite": "person.badge.plus"
        case "remove": "person.badge.minus"
        case "connect": "link"
        case "disconnect": "link.badge.plus"
        case "revoke": "xmark.circle"
        case "export": "square.and.arrow.up"
        case "import": "square.and.arrow.down"
        case "duplicate": "doc.on.doc"
        case "transfer": "arrow.left.arrow.right"
        case "rotate", "rotate_keys": "key"
        case "api_call": "curlybraces"
        case "test": "checkmark.seal"
        default: "bolt"
        }
    }

    /// "delete" -> "deleted", for "Dev deleted a meeting".
    static func verb(_ raw: String?) -> String {
        switch raw {
        case "create": "created"
        case "update": "updated"
        case "delete": "deleted"
        case "send": "sent"
        case "start": "started"
        case "stop": "stopped"
        case "pause": "paused"
        case "resume": "resumed"
        case "invite": "invited"
        case "remove": "removed"
        case "connect": "connected"
        case "disconnect": "disconnected"
        case "revoke": "revoked"
        case "export": "exported"
        case "import": "imported"
        case "duplicate": "duplicated"
        case "transfer": "transferred"
        case "rotate", "rotate_keys": "rotated keys for"
        case "test": "tested"
        case "api_call": "called the API on"
        case "apply": "applied"
        case "install": "installed"
        case "uninstall": "uninstalled"
        case "assign": "assigned"
        case "reboot": "rebooted"
        default: humanize(raw).lowercased()
        }
    }

    /// The entity as a noun phrase with its article: "a meeting", "an API key".
    static func noun(_ raw: String?) -> String {
        let word: String = switch raw {
        case "email_account": "mailbox"
        case "organization_member": "member"
        case "organization": "workspace"
        case "crm_deal": "deal"
        case "crm_task": "task"
        case "crm_note": "note"
        case "crm_pipeline": "pipeline"
        case "crm_stage": "stage"
        case "api_key": "API key"
        case "lead_sync_source": "lead source"
        case "warmup_routing_rule": "warmup rule"
        case "unibox": "inbox thread"
        case "step": "campaign step"
        case nil, "": "something"
        default: humanize(raw).lowercased()
        }
        if raw == "settings" || raw == "subscription" || word == "something" {
            return raw == "subscription" ? "the subscription" : word
        }
        let article = "aeiou".contains(word.prefix(1).lowercased()) || word.hasPrefix("API") ? "an" : "a"
        return "\(article) \(word)"
    }

    /// The best human name the entry recorded for its target.
    static func subject(_ entry: AuditLogEntry) -> String? {
        let bag = entry.metadata ?? [:]
        for key in ["title", "name", "subject", "email", "invitee_email", "label"] {
            if let value = bag[key]?.trimmingCharacters(in: .whitespaces), !value.isEmpty { return value }
        }
        return nil
    }
}

// MARK: - View

/// A quick entity filter above the feed.
enum AuditScope: String, CaseIterable, Hashable {
    case all, campaigns, contacts, mailboxes, team, crm

    var title: String {
        switch self {
        case .all: "All"
        case .campaigns: "Campaigns"
        case .contacts: "Contacts"
        case .mailboxes: "Mailboxes"
        case .team: "Team"
        case .crm: "Deals"
        }
    }

    var entity: String? {
        switch self {
        case .all: nil
        case .campaigns: "campaign"
        case .contacts: "contact"
        case .mailboxes: "email_account"
        case .team: "organization_member"
        case .crm: "crm_deal"
        }
    }
}

/// The org activity trail: every audited mutation as a live timeline, with
/// new entries sliding in at the top as teammates work.
struct AuditLogView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = AuditLogStore()
    @State private var shown = false
    #if DEBUG
    @State private var debugOpen = false
    #endif

    var body: some View {
        Group {
            if !env.session.can(.viewAnalytics) {
                CoverEmptyState(
                    symbol: "lock.fill",
                    color: WTheme.inkSecondary,
                    title: "No access",
                    message: "You need the view analytics permission to see the activity trail."
                )
                .frame(maxHeight: .infinity)
            } else if let error = store.errorMessage, store.entries.isEmpty {
                ErrorStateView(title: "Couldn't load activity", message: error) {
                    await store.load(env.api)
                }
            } else if !store.hasLoaded {
                ScrollView { AnalyticsLoadingCards().padding(16) }
            } else {
                feed
            }
        }
        .background(WTheme.groupedPage)
        .navigationTitle("Activity")
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(WTheme.groupedPage, for: .navigationBar)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { filterMenu }
        }
        .task { await store.load(env.api) }
        .onChange(of: env.realtime.pulse(for: .audit)) {
            Task { await store.refreshTop(env.api) }
        }
        .onChange(of: store.actionFilter) {
            Task { await store.load(env.api) }
        }
        .onChange(of: store.entityFilter) {
            Task { await store.load(env.api) }
        }
        .sensoryFeedback(.impact(weight: .light), trigger: store.fresh.count) { old, new in new > old }
        #if DEBUG
        .navigationDestination(isPresented: $debugOpen) {
            if let first = store.entries.first { AuditEntryDetailView(entry: first) }
        }
        .onChange(of: store.hasLoaded) {
            debugOpen = ProcessInfo.processInfo.environment["WARMBLY_DEBUG_AUDIT_ENTRY"] != nil && !store.entries.isEmpty
        }
        #endif
    }

    // MARK: Hero

    /// Today's count is exact once the oldest loaded entry predates today.
    private var todayCount: (value: Int, exact: Bool) {
        let today = store.entries.count { $0.when.map(Calendar.current.isDateInToday) ?? false }
        let exact = !store.hasMore || (store.entries.last?.when.map { !Calendar.current.isDateInToday($0) } ?? true)
        return (today, exact)
    }

    private var recentActors: [AuditActor] {
        var seen = Set<String>()
        var result: [AuditActor] = []
        for entry in store.entries {
            guard let actor = entry.actor, let key = actor.id ?? actor.email, !seen.contains(key) else { continue }
            seen.insert(key)
            result.append(actor)
            if result.count == 4 { break }
        }
        return result
    }

    private var hero: some View {
        let today = todayCount
        let actors = recentActors
        return VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 7) {
                LivePulse()
                Text("Live")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(WTheme.positive)
                Spacer()
                Text("Kept for 90 days")
                    .font(.system(size: 12.5))
                    .foregroundStyle(WTheme.inkTertiary)
            }
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                CountUpText(value: shown ? Double(today.value) : 0, format: { "\(Int($0.rounded()))" })
                    .font(.system(size: 40, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.ink)
                    .contentTransition(.numericText())
                    .animation(.spring(response: 0.9, dampingFraction: 0.9).delay(0.1), value: shown)
                Text((today.exact ? "" : "+ ") + (today.value == 1 ? "change today" : "changes today"))
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(WTheme.inkSecondary)
            }
            if !actors.isEmpty {
                HStack(spacing: 10) {
                    HStack(spacing: -8) {
                        ForEach(Array(actors.enumerated()), id: \.offset) { index, actor in
                            WAvatar(name: actor.displayName ?? "?", seed: actor.id ?? actor.email ?? "", size: 28)
                                .overlay(Circle().stroke(WTheme.groupedCard, lineWidth: 2))
                                .zIndex(Double(actors.count - index))
                                .scaleEffect(shown ? 1 : 0.4)
                                .opacity(shown ? 1 : 0)
                                .animation(.spring(response: 0.5, dampingFraction: 0.65).delay(0.15 + Double(index) * 0.06), value: shown)
                        }
                    }
                    Text(actorsLine(actors))
                        .font(.system(size: 13.5))
                        .foregroundStyle(WTheme.inkSecondary)
                        .lineLimit(1)
                }
            }
        }
        .coverCard()
        .reveal(shown, 0)
    }

    private func actorsLine(_ actors: [AuditActor]) -> String {
        let first = actors.first?.firstName.flatMap { $0.isEmpty ? nil : $0 } ?? actors.first?.displayName ?? "Someone"
        switch actors.count {
        case 1: return "\(first) has been busy"
        case 2: return "\(first) and 1 teammate recently"
        default: return "\(first) and \(actors.count - 1) teammates recently"
        }
    }

    // MARK: Feed

    private var scope: Binding<AuditScope> {
        Binding(
            get: { AuditScope.allCases.first { $0.entity == store.entityFilter } ?? .all },
            set: { store.entityFilter = $0.entity }
        )
    }

    private var feed: some View {
        List {
            Section {
                VStack(spacing: 14) {
                    if !store.hasFilters || store.actionFilter == nil {
                        hero
                    }
                    ChipFilterRow(options: AuditScope.allCases, selection: scope, label: \.title)
                    if let action = store.actionFilter {
                        activeFilter(action)
                    }
                }
                .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 0, trailing: 0))
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
            }
            if store.entries.isEmpty {
                Section {
                    emptyState
                        .listRowBackground(Color.clear)
                        .listRowSeparator(.hidden)
                }
            }
            ForEach(store.sections) { section in
                Section {
                    ForEach(section.entries) { entry in
                        NavigationLink {
                            AuditEntryDetailView(entry: entry)
                        } label: {
                            AuditRowView(entry: entry)
                        }
                        .listRowBackground(
                            ZStack {
                                WTheme.groupedCard
                                if store.fresh.contains(entry.id) {
                                    WTheme.accent.opacity(0.09).transition(.opacity)
                                }
                            }
                        )
                        .listRowInsets(EdgeInsets(top: 10, leading: 14, bottom: 10, trailing: 14))
                        .alignmentGuide(.listRowSeparatorLeading) { _ in 64 }
                        .transition(.asymmetric(insertion: .move(edge: .top).combined(with: .opacity), removal: .opacity))
                    }
                } header: {
                    CoverSectionHeader(
                        title: section.label,
                        count: section.entries.count,
                        color: section.id == Calendar.current.startOfDay(for: .now) ? WTheme.positive : nil,
                        pulsing: section.id == Calendar.current.startOfDay(for: .now)
                    )
                }
            }
            if !store.entries.isEmpty {
                Section {
                    if store.hasMore {
                        ProgressView()
                            .frame(maxWidth: .infinity)
                            .frame(height: 44)
                            .onAppear {
                                Task { await store.loadMore(env.api) }
                            }
                    } else {
                        Text("That's everything from the last 90 days")
                            .font(.system(size: 12.5, weight: .medium))
                            .foregroundStyle(WTheme.inkTertiary)
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 6)
                    }
                }
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
            }
        }
        .listStyle(.insetGrouped)
        .readableMargins()
        .listSectionSpacing(18)
        .contentMargins(.horizontal, 16, for: .scrollContent)
        .contentMargins(.top, 4, for: .scrollContent)
        .contentMargins(.bottom, 100, for: .scrollContent)
        .scrollContentBackground(.hidden)
        .defaultScrollAnchor(SettingsDebug.anchor)
        .refreshable { await store.load(env.api) }
        .onAppear { shown = true }
    }

    private func activeFilter(_ action: String) -> some View {
        HStack(spacing: 8) {
            Image(systemName: AuditFmt.actionIcon(action))
                .font(.system(size: 12, weight: .semibold))
            Text("Only \(AuditFmt.verb(action))")
                .font(.system(size: 13.5, weight: .medium))
            Spacer()
            Button {
                withAnimation(.snappy) { store.actionFilter = nil }
            } label: {
                Image(systemName: "xmark.circle.fill")
                    .font(.system(size: 17))
                    .foregroundStyle(WTheme.inkTertiary)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Clear action filter")
        }
        .foregroundStyle(AuditFmt.actionTone(action).color)
        .padding(.horizontal, 14)
        .frame(height: 40)
        .background(AuditFmt.actionTone(action).background, in: Capsule())
    }

    @ViewBuilder
    private var emptyState: some View {
        if store.hasFilters {
            CoverEmptyState(
                symbol: "line.3.horizontal.decrease",
                color: WTheme.inkSecondary,
                title: "Nothing matches",
                message: "Nothing in the last 90 days matches this filter.",
                actionTitle: "Show everything",
                action: {
                    store.clearFilters()
                    Task { await store.load(env.api) }
                }
            )
        } else {
            CoverEmptyState(
                symbol: "sparkles",
                color: WTheme.positive,
                title: "Quiet so far",
                message: "Every change your team makes lands here the moment it happens."
            )
        }
    }

    private var filterMenu: some View {
        @Bindable var store = store
        return Menu {
            Picker("Action", selection: $store.actionFilter) {
                Text("All actions").tag(String?.none)
                ForEach(Self.filterActions, id: \.self) { action in
                    Label(AuditFmt.humanize(action), systemImage: AuditFmt.actionIcon(action)).tag(String?.some(action))
                }
            }
            Picker("Entity", selection: $store.entityFilter) {
                Text("All entities").tag(String?.none)
                ForEach(Self.filterEntities, id: \.self) { entity in
                    Text(AuditFmt.humanize(entity)).tag(String?.some(entity))
                }
            }
            if store.hasFilters {
                Button("Clear filters", role: .destructive) {
                    store.clearFilters()
                }
            }
        } label: {
            Image(systemName: store.hasFilters
                ? "line.3.horizontal.decrease.circle.fill"
                : "line.3.horizontal.decrease.circle")
        }
        .accessibilityLabel("Filter activity")
    }

    /// Open string enums server-side; this is the subset the web filter offers.
    private static let filterActions = [
        "create", "update", "delete", "start", "stop", "pause", "resume",
        "send", "duplicate", "connect", "disconnect", "invite", "remove",
        "revoke", "transfer", "export", "import", "rotate", "test", "api_call",
    ]

    private static let filterEntities = [
        "campaign", "contact", "email_account", "step", "template", "api_key",
        "webhook", "integration", "organization_member", "invitation", "role",
        "settings", "subscription", "automation", "unibox", "meeting",
        "tag", "folder", "category", "crm_deal", "crm_task",
    ]
}

/// A green dot breathing a ring, for a live feed.
private struct LivePulse: View {
    @State private var pulse = false

    var body: some View {
        Circle()
            .fill(WTheme.positive)
            .frame(width: 8, height: 8)
            .background {
                Circle()
                    .fill(WTheme.positive.opacity(0.35))
                    .scaleEffect(pulse ? 2.8 : 1)
                    .opacity(pulse ? 0 : 1)
            }
            .onAppear {
                withAnimation(.easeOut(duration: 1.5).repeatForever(autoreverses: false)) { pulse = true }
            }
    }
}

// MARK: - Row

/// One entry as a sentence: who did what to which thing, and when.
struct AuditRowView: View {
    let entry: AuditLogEntry

    private var actorLabel: String {
        entry.actor?.displayName ?? "A former member"
    }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            AuditActorBadge(entry: entry, size: 38)
            VStack(alignment: .leading, spacing: 3) {
                (Text(actorLabel).fontWeight(.semibold).foregroundStyle(WTheme.ink)
                    + Text(" \(AuditFmt.verb(entry.action)) \(AuditFmt.noun(entry.entityType))").foregroundStyle(WTheme.inkSecondary))
                    .font(.system(size: 14.5))
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
                if let subject = AuditFmt.subject(entry) {
                    Text(subject)
                        .font(.system(size: 13.5, weight: .medium))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                }
                if let when = entry.when {
                    Text(when.formatted(date: .omitted, time: .shortened))
                        .font(.system(size: 12.5))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.inkTertiary)
                }
            }
            Spacer(minLength: 0)
        }
    }
}

/// The actor's avatar with the action riding its corner.
struct AuditActorBadge: View {
    let entry: AuditLogEntry
    var size: CGFloat = 38

    var body: some View {
        let tone = AuditFmt.actionTone(entry.action)
        WAvatar(name: entry.actor?.displayName ?? "?", seed: entry.actor?.id ?? entry.userID ?? "", size: size)
            .overlay(alignment: .bottomTrailing) {
                Image(systemName: AuditFmt.actionIcon(entry.action))
                    .font(.system(size: size * 0.24, weight: .bold))
                    .foregroundStyle(.white)
                    .frame(width: size * 0.46, height: size * 0.46)
                    .background(tone.color, in: Circle())
                    .overlay(Circle().stroke(WTheme.groupedCard, lineWidth: 2))
                    .offset(x: 4, y: 3)
            }
    }
}

// MARK: - Detail

/// Full record for a single audited action: the sentence, when and from
/// where, and the recorded field changes and metadata.
struct AuditEntryDetailView: View {
    let entry: AuditLogEntry

    @State private var copied: String?

    private var changePairs: [(key: String, value: String)] {
        (entry.changes ?? [:]).sorted { $0.key < $1.key }.map { ($0.key, $0.value) }
    }

    private var metadataPairs: [(key: String, value: String)] {
        (entry.metadata ?? [:]).sorted { $0.key < $1.key }.map { ($0.key, $0.value) }
    }

    var body: some View {
        SettingsPage(title: "Activity", spacing: 22) {
            VStack(spacing: 12) {
                AuditActorBadge(entry: entry, size: 64)
                (Text(entry.actor?.displayName ?? "A former member").fontWeight(.semibold).foregroundStyle(WTheme.ink)
                    + Text(" \(AuditFmt.verb(entry.action)) \(AuditFmt.noun(entry.entityType))").foregroundStyle(WTheme.inkSecondary))
                    .font(.system(size: 19))
                    .multilineTextAlignment(.center)
                if let subject = AuditFmt.subject(entry) {
                    Text(subject)
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(WTheme.ink)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 6)
                        .background(WTheme.fill, in: Capsule())
                }
                if let when = entry.when {
                    Text(when.formatted(.dateTime.weekday(.wide).month(.wide).day().hour().minute()))
                        .font(.system(size: 13.5))
                        .foregroundStyle(WTheme.inkTertiary)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 20)
            .padding(.horizontal, 16)
            .background(WTheme.groupedCard, in: RoundedRectangle(cornerRadius: 22, style: .continuous))

            SettingsSection("Details", dividerLeading: 16) {
                AuditDetailRow(label: "Type", value: AuditFmt.humanize(entry.entityType))
                if let email = entry.actor?.email, !email.isEmpty {
                    AuditDetailRow(label: "Actor", value: email, copied: $copied)
                }
                if let entityID = entry.entityID, !entityID.isEmpty {
                    AuditDetailRow(label: "Record id", value: entityID, mono: true, copied: $copied)
                }
                if let ip = entry.ipAddress, !ip.isEmpty {
                    AuditDetailRow(label: "IP address", value: ip, mono: true, copied: $copied)
                }
                if let agent = entry.userAgent, !agent.isEmpty {
                    AuditDetailRow(label: "Client", value: agent)
                }
            }

            if !changePairs.isEmpty {
                SettingsSection("Changes", dividerLeading: 16) {
                    ForEach(changePairs, id: \.key) { pair in
                        AuditDetailRow(label: AuditFmt.humanize(pair.key), value: pair.value, mono: true, copied: $copied)
                    }
                }
            }

            if !metadataPairs.isEmpty {
                SettingsSection("Recorded with it", dividerLeading: 16) {
                    ForEach(metadataPairs, id: \.key) { pair in
                        AuditDetailRow(label: AuditFmt.humanize(pair.key), value: pair.value, copied: $copied)
                    }
                }
            }
        }
        .sensoryFeedback(.selection, trigger: copied)
    }
}

struct AuditDetailRow: View {
    let label: String
    let value: String
    var mono: Bool = false
    var copied: Binding<String?>?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(label)
                .font(.system(size: 14))
                .foregroundStyle(WTheme.inkSecondary)
                .frame(width: 96, alignment: .leading)
            Text(value)
                .font(mono ? .system(size: 13.5, design: .monospaced) : .system(size: 14.5))
                .foregroundStyle(WTheme.ink)
                .frame(maxWidth: .infinity, alignment: .leading)
                .textSelection(.enabled)
            if let copied {
                Button {
                    UIPasteboard.general.string = value
                    withAnimation(.snappy) { copied.wrappedValue = label }
                    Task {
                        try? await Task.sleep(for: .seconds(1.4))
                        withAnimation(.snappy) { if copied.wrappedValue == label { copied.wrappedValue = nil } }
                    }
                } label: {
                    Image(systemName: copied.wrappedValue == label ? "checkmark" : "doc.on.doc")
                        .font(.system(size: 13, weight: .medium))
                        .foregroundStyle(copied.wrappedValue == label ? WTheme.positive : WTheme.inkTertiary)
                        .contentTransition(.symbolEffect(.replace))
                        .frame(width: 28, height: 28)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Copy \(label.lowercased())")
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
    }
}
