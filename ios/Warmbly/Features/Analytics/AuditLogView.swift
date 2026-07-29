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
                    withAnimation(.snappy) {
                        entries.insert(contentsOf: new, at: 0)
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

    static func entityTone(_ raw: String?) -> Tone {
        switch raw {
        case "campaign", "step", "template", "unibox":
            .sky
        case "contact", "crm_pipeline", "crm_stage", "crm_deal", "crm_task", "crm_note":
            .indigo
        case "email_account", "warmup_routing_rule":
            .orange
        case "organization_member", "invitation", "team", "role", "user":
            .emerald
        case "api_key", "webhook", "integration", "lead_sync_source", "automation":
            .amber
        default:
            .slate
        }
    }
}

// MARK: - View

/// The org activity trail: every audited mutation, live. This feed is the
/// realtime spine made visible.
struct AuditLogView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = AuditLogStore()

    var body: some View {
        Group {
            if !env.session.can(.viewAnalytics) {
                EmptyStateView(
                    title: "No access",
                    message: "You need the view analytics permission to see the activity trail."
                )
            } else if !store.entries.isEmpty {
                feed
            } else if let error = store.errorMessage {
                ErrorStateView(title: "Couldn't load activity", message: error) {
                    await store.load(env.api)
                }
            } else if store.hasLoaded {
                if store.hasFilters {
                    EmptyStateView(
                        title: "No matching entries",
                        message: "Nothing in the last 90 days matches these filters.",
                        ctaTitle: "Clear filters"
                    ) {
                        store.clearFilters()
                        Task { await store.load(env.api) }
                    }
                } else {
                    EmptyStateView(
                        title: "No activity yet",
                        message: "Team actions appear here as they happen. Entries are kept for 90 days."
                    )
                }
            } else {
                SkeletonRows(rows: 12)
            }
        }
        .navigationTitle("Activity")
        .navigationBarTitleDisplayMode(.inline)
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
    }

    private var feed: some View {
        List {
            ForEach(store.sections) { section in
                // Day captions are bare rows, not sticky grouped headers.
                AnalyticsSectionCaption(
                    section.label,
                    top: section.id == store.sections.first?.id ? 14 : 20
                )
                ForEach(section.entries) { entry in
                    NavigationLink {
                        AuditEntryDetailView(entry: entry)
                    } label: {
                        AuditRowView(entry: entry)
                    }
                    .analyticsPlainRow(separatorLeading: 64)
                }
            }
            if store.hasMore {
                HStack {
                    Spacer()
                    ProgressView()
                    Spacer()
                }
                .frame(height: 44)
                .listRowSeparator(.hidden)
                .listRowBackground(Color(.systemBackground))
                .onAppear {
                    Task { await store.loadMore(env.api) }
                }
            } else {
                Text("End of trail · 90 day retention")
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(.tertiary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 14)
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color(.systemBackground))
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .environment(\.defaultMinListRowHeight, 0)
        .background(Color(.systemBackground))
        .refreshable { await store.load(env.api) }
    }

    private var filterMenu: some View {
        @Bindable var store = store
        return Menu {
            Picker("Action", selection: $store.actionFilter) {
                Text("All actions").tag(String?.none)
                ForEach(Self.filterActions, id: \.self) { action in
                    Text(AuditFmt.humanize(action)).tag(String?.some(action))
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

// MARK: - Row

struct AuditRowView: View {
    let entry: AuditLogEntry

    private var actorLabel: String {
        entry.actor?.displayName ?? "former member"
    }

    var body: some View {
        HStack(spacing: 12) {
            IconTile(symbol: AuditFmt.actionIcon(entry.action), tone: AuditFmt.actionTone(entry.action), size: 36)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(AuditFmt.humanize(entry.action))
                        .font(.body.weight(.medium))
                        .lineLimit(1)
                    AuditEntityChip(entityType: entry.entityType)
                }
                HStack(spacing: 4) {
                    Text(actorLabel)
                        .lineLimit(1)
                    if let entityID = entry.entityID, !entityID.isEmpty {
                        Text("·")
                        Text(String(entityID.prefix(8)))
                            .font(.footnote.monospaced())
                    }
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
            }
            Spacer()
            if let when = entry.when {
                Text(WFormat.relative(when))
                    .font(.footnote)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 8)
    }
}

struct AuditEntityChip: View {
    let entityType: String?

    var body: some View {
        Text(AuditFmt.humanize(entityType).lowercased())
            .font(.system(size: 10, weight: .semibold))
            .foregroundStyle(AuditFmt.entityTone(entityType).color)
            .padding(.horizontal, 6)
            .padding(.vertical, 2.5)
            .background(AuditFmt.entityTone(entityType).background, in: Capsule())
            .lineLimit(1)
    }
}

// MARK: - Detail

/// Full record for a single audited action: actor, target, request context,
/// and the recorded field changes / metadata.
struct AuditEntryDetailView: View {
    let entry: AuditLogEntry

    private var changePairs: [(key: String, value: String)] {
        (entry.changes ?? [:]).sorted { $0.key < $1.key }.map { ($0.key, $0.value) }
    }

    private var metadataPairs: [(key: String, value: String)] {
        (entry.metadata ?? [:]).sorted { $0.key < $1.key }.map { ($0.key, $0.value) }
    }

    var body: some View {
        List {
            HStack(spacing: 12) {
                IconTile(symbol: AuditFmt.actionIcon(entry.action), tone: AuditFmt.actionTone(entry.action), size: 42)
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 6) {
                        Text(AuditFmt.humanize(entry.action))
                            .font(.body.weight(.semibold))
                        AuditEntityChip(entityType: entry.entityType)
                    }
                    Text(entry.actor?.displayName ?? "former member")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }
            .padding(.top, 16)
            .padding(.bottom, 6)
            .analyticsPlainRow(separator: .hidden)

            AnalyticsSectionCaption("Details")
            if let when = entry.when {
                AuditDetailRow(label: "When", value: when.formatted(date: .abbreviated, time: .standard))
                    .analyticsPlainRow()
            }
            if let email = entry.actor?.email, !email.isEmpty {
                AuditDetailRow(label: "Actor", value: email)
                    .analyticsPlainRow()
            }
            if let entityID = entry.entityID, !entityID.isEmpty {
                AuditDetailRow(label: "Entity id", value: entityID, mono: true)
                    .analyticsPlainRow()
            }
            if let ip = entry.ipAddress, !ip.isEmpty {
                AuditDetailRow(label: "IP address", value: ip, mono: true)
                    .analyticsPlainRow()
            }
            if let agent = entry.userAgent, !agent.isEmpty {
                AuditDetailRow(label: "User agent", value: agent)
                    .analyticsPlainRow()
            }

            if !changePairs.isEmpty {
                AnalyticsSectionCaption("Changes")
                ForEach(changePairs, id: \.key) { pair in
                    AuditDetailRow(label: AuditFmt.humanize(pair.key), value: pair.value, mono: true)
                        .analyticsPlainRow()
                }
            }

            if !metadataPairs.isEmpty {
                AnalyticsSectionCaption("Metadata")
                ForEach(metadataPairs, id: \.key) { pair in
                    AuditDetailRow(label: AuditFmt.humanize(pair.key), value: pair.value, mono: true)
                        .analyticsPlainRow()
                }
            }

            Color.clear
                .frame(height: 24)
                .listRowInsets(EdgeInsets())
                .listRowSeparator(.hidden)
                .listRowBackground(Color(.systemBackground))
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .environment(\.defaultMinListRowHeight, 0)
        .background(Color(.systemBackground))
        .navigationTitle("Activity")
        .navigationBarTitleDisplayMode(.inline)
    }
}

struct AuditDetailRow: View {
    let label: String
    let value: String
    var mono: Bool = false

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 96, alignment: .leading)
            Text(value)
                .font(mono ? .subheadline.monospaced() : .subheadline)
                .foregroundStyle(.primary)
                .frame(maxWidth: .infinity, alignment: .leading)
                .textSelection(.enabled)
        }
        .padding(.vertical, 6)
    }
}
