import SwiftUI
import Charts

// The four campaign-detail tabs. Each: SkeletonRows on first load, stale data
// during reloads, ErrorStateView on failure, EmptyStateView when empty, and a
// reload on the relevant realtime pulse. These are pushed inside the Campaigns
// tab's NavigationStack, so none of them creates its own stack.

// MARK: - Overview

@MainActor
@Observable
final class CampaignOverviewStore {
    private(set) var analytics: CampaignAnalytics?
    private(set) var daily: [CampaignDailyStat] = []
    private(set) var isLoading = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    func load(_ api: APIClient, campaignID: String) async {
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            let result: CampaignAnalytics = try await api.get("analytics/campaigns/\(campaignID)")
            let to = Date()
            let from = to.addingTimeInterval(-30 * 86_400)
            let dailyPage: CampaignDailyPage? = try? await api.get(
                "analytics/campaigns/\(campaignID)/daily",
                query: ["from": CampaignDates.param(from), "to": CampaignDates.param(to)]
            )
            withAnimation(.snappy) {
                analytics = result
                daily = dailyPage?.data ?? []
            }
            errorMessage = nil
            hasLoaded = true
        } catch {
            if !hasLoaded { errorMessage = error.localizedDescription }
        }
        isLoading = false
    }
}

struct CampaignOverviewTab: View {
    @Environment(AppEnvironment.self) private var env
    let campaign: Campaign
    let store: CampaignOverviewStore

    private let columns = [
        GridItem(.flexible(), alignment: .leading),
        GridItem(.flexible(), alignment: .leading),
        GridItem(.flexible(), alignment: .leading),
    ]

    private var chartDays: [(id: Int, day: Date, sent: Int, opens: Int, replies: Int)] {
        store.daily.enumerated().compactMap { index, day in
            guard let date = CampaignDates.day(from: day.date) else { return nil }
            return (id: index, day: date, sent: day.sent ?? 0, opens: day.opens ?? 0, replies: day.replies ?? 0)
        }
    }

    var body: some View {
        Group {
            if store.isLoading, !store.hasLoaded {
                ScrollView { SkeletonRows(rows: 6) }
            } else if let error = store.errorMessage, store.analytics == nil {
                ErrorStateView(title: "Couldn't load analytics", message: error) {
                    await store.load(env.api, campaignID: campaign.id)
                }
            } else {
                List {
                    summarySection
                    dailySection
                    stepsSection
                }
                .listStyle(.insetGrouped)
                .refreshable { await store.load(env.api, campaignID: campaign.id) }
            }
        }
        .task { if !store.hasLoaded { await store.load(env.api, campaignID: campaign.id) } }
        .onChange(of: env.realtime.pulse(for: .analytics)) {
            Task { await store.load(env.api, campaignID: campaign.id) }
        }
    }

    @ViewBuilder
    private var summarySection: some View {
        let summary = store.analytics?.summary
        Section {
            LazyVGrid(columns: columns, alignment: .leading, spacing: 14) {
                CampaignStatCell(label: "Sent", value: WFormat.compact(summary?.emailsSent ?? 0))
                CampaignStatCell(label: "Opens", value: WFormat.compact(summary?.uniqueOpens ?? 0), tone: .sky)
                CampaignStatCell(label: "Replies", value: WFormat.compact(summary?.replies ?? 0), tone: .emerald)
                CampaignStatCell(label: "Open rate", value: rate(summary?.openRate))
                CampaignStatCell(label: "Reply rate", value: rate(summary?.replyRate))
                CampaignStatCell(label: "Bounce rate", value: rate(summary?.bounceRate),
                                 tone: (summary?.bounceRate ?? 0) >= 5 ? .rose : nil)
            }
            .padding(.vertical, 8)
            .listRowSeparator(.hidden)
            if let machine = summary?.machineOpens, machine > 0 {
                Text("\(WFormat.compact(machine)) automated opens (Apple Mail Privacy et al.)")
                    .font(.footnote)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                    .listRowSeparator(.hidden)
            }
        } header: {
            EyebrowLabel("Performance")
        }
    }

    @ViewBuilder
    private var dailySection: some View {
        if chartDays.count > 1, chartDays.contains(where: { $0.sent > 0 }) {
            Section {
                Chart(chartDays, id: \.id) { day in
                    BarMark(x: .value("Day", day.day), y: .value("Sent", day.sent))
                        .foregroundStyle(Tone.sky.color.opacity(0.75))
                    LineMark(x: .value("Day", day.day), y: .value("Replies", day.replies))
                        .foregroundStyle(Tone.emerald.color)
                }
                .chartXAxis {
                    AxisMarks(values: .automatic(desiredCount: 4)) { _ in
                        AxisGridLine()
                        AxisValueLabel(format: .dateTime.month(.abbreviated).day())
                    }
                }
                .chartYAxis {
                    AxisMarks(position: .trailing, values: .automatic(desiredCount: 3))
                }
                .frame(height: 130)
                .padding(.vertical, 6)
                .listRowSeparator(.hidden)
            } header: {
                EyebrowLabel("Last 30 days")
            }
        }
    }

    @ViewBuilder
    private var stepsSection: some View {
        let steps = store.analytics?.steps ?? []
        if !steps.isEmpty {
            Section {
                ForEach(steps) { step in
                    HStack(spacing: 12) {
                        Text("\(step.position ?? 0)")
                            .font(.footnote.weight(.semibold))
                            .monospacedDigit()
                            .foregroundStyle(Tone.sky.color)
                            .frame(width: 28, height: 28)
                            .background(Tone.sky.background, in: RoundedRectangle(cornerRadius: 8))
                        VStack(alignment: .leading, spacing: 3) {
                            Text(step.name ?? "Step")
                                .font(.body.weight(.medium))
                                .lineLimit(1)
                            Text("\(WFormat.compact(step.emailsSent ?? 0)) sent · \(WFormat.compact(step.opens ?? 0)) opens · \(WFormat.compact(step.replies ?? 0)) replies")
                                .font(.footnote)
                                .monospacedDigit()
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                        }
                        Spacer()
                    }
                    .padding(.vertical, 6)
                }
            } header: {
                EyebrowLabel("Per step")
            }
        }
    }

    private func rate(_ value: Double?) -> String {
        String(format: "%.1f%%", value ?? 0)
    }
}

/// Bold rounded stat number over an eyebrow label (detail-screen style).
private struct CampaignStatCell: View {
    let label: String
    let value: String
    var tone: Tone? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            EyebrowLabel(label)
            Text(value)
                .font(.system(size: 22, weight: .bold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(tone?.color ?? Color.primary)
                .contentTransition(.numericText())
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Leads

@MainActor
@Observable
final class CampaignLeadsStore {
    private(set) var leads: [CampaignLead] = []
    private(set) var nextCursor: String?
    private(set) var hasMore = false
    private(set) var isLoading = false
    private(set) var isLoadingMore = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    private var generation = 0

    func load(_ api: APIClient, campaignID: String) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            let page: CampaignLeadsPage = try await api.post(
                "contacts/search",
                body: CampaignLeadsSearchBody(query: "", campaignIDs: [campaignID]),
                query: ["limit": "50"]
            )
            guard gen == generation else { return }
            withAnimation(.snappy) {
                leads = page.data ?? []
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
            errorMessage = nil
            hasLoaded = true
            isLoading = false
        } catch {
            guard gen == generation else { return }
            if !hasLoaded { errorMessage = error.localizedDescription }
            isLoading = false
        }
    }

    func loadMore(_ api: APIClient, campaignID: String) async {
        guard hasMore, !isLoadingMore, let cursor = nextCursor else { return }
        let gen = generation
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page: CampaignLeadsPage = try await api.post(
                "contacts/search",
                body: CampaignLeadsSearchBody(query: "", campaignIDs: [campaignID]),
                query: ["limit": "50", "cursor": cursor]
            )
            guard gen == generation else { return }
            let fresh = (page.data ?? []).filter { new in !leads.contains(where: { $0.id == new.id }) }
            withAnimation(.snappy) {
                leads.append(contentsOf: fresh)
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
        } catch {
            // Keep the list; the sentinel row retries on next appear.
        }
    }
}

struct CampaignLeadsTab: View {
    @Environment(AppEnvironment.self) private var env
    let campaignID: String
    let store: CampaignLeadsStore

    var body: some View {
        Group {
            if store.isLoading, !store.hasLoaded {
                ScrollView { SkeletonRows(rows: 8) }
            } else if let error = store.errorMessage, store.leads.isEmpty {
                ErrorStateView(title: "Couldn't load leads", message: error) {
                    await store.load(env.api, campaignID: campaignID)
                }
            } else if store.leads.isEmpty {
                EmptyStateView(title: "No leads yet", message: "Add contacts to this campaign to start sending.")
            } else {
                List {
                    ForEach(store.leads) { lead in
                        CampaignLeadRow(lead: lead)
                    }
                    if store.hasMore {
                        HStack {
                            Spacer()
                            ProgressView().controlSize(.small)
                            Spacer()
                        }
                        .listRowSeparator(.hidden)
                        .onAppear {
                            Task { await store.loadMore(env.api, campaignID: campaignID) }
                        }
                    }
                }
                .listStyle(.plain)
                .refreshable { await store.load(env.api, campaignID: campaignID) }
            }
        }
        .task { if !store.hasLoaded { await store.load(env.api, campaignID: campaignID) } }
        .onChange(of: env.realtime.pulse(for: .contacts)) {
            Task { await store.load(env.api, campaignID: campaignID) }
        }
    }
}

struct CampaignLeadRow: View {
    let lead: CampaignLead

    var body: some View {
        HStack(spacing: 12) {
            WAvatar(name: lead.displayName, seed: lead.id, size: 38)
            VStack(alignment: .leading, spacing: 3) {
                Text(lead.displayName)
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                HStack(spacing: 4) {
                    if lead.hasName, let email = lead.email, !email.isEmpty {
                        Text(email).lineLimit(1)
                    }
                    if let step = lead.progress?.currentStep, !step.isEmpty {
                        if lead.hasName, lead.email?.isEmpty == false { Text("·") }
                        Text(step).lineLimit(1)
                    }
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            if let progress = lead.progress {
                StatusPill(
                    text: progress.statusLabel,
                    tone: progress.statusTone,
                    pulsing: progress.status == "active"
                )
            }
        }
        .padding(.vertical, 6)
    }
}

// MARK: - Steps

@MainActor
@Observable
final class CampaignStepsStore {
    private(set) var steps: [CampaignStep] = []
    private(set) var isLoading = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    func load(_ api: APIClient, campaignID: String) async {
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            // GET /campaigns/:id/steps returns a raw JSON array (no envelope).
            let result: [CampaignStep] = try await api.get("campaigns/\(campaignID)/steps")
            withAnimation(.snappy) {
                steps = result.sorted { ($0.position ?? 0) < ($1.position ?? 0) }
            }
            errorMessage = nil
            hasLoaded = true
        } catch {
            if !hasLoaded { errorMessage = error.localizedDescription }
        }
        isLoading = false
    }
}

struct CampaignStepsTab: View {
    @Environment(AppEnvironment.self) private var env
    let campaignID: String
    let store: CampaignStepsStore

    var body: some View {
        Group {
            if store.isLoading, !store.hasLoaded {
                ScrollView { SkeletonRows(rows: 5) }
            } else if let error = store.errorMessage, store.steps.isEmpty {
                ErrorStateView(title: "Couldn't load steps", message: error) {
                    await store.load(env.api, campaignID: campaignID)
                }
            } else if store.steps.isEmpty {
                EmptyStateView(title: "No steps yet", message: "Build the sequence on the web dashboard.")
            } else {
                List {
                    ForEach(store.steps) { step in
                        CampaignStepRow(step: step)
                    }
                }
                .listStyle(.plain)
                .refreshable { await store.load(env.api, campaignID: campaignID) }
            }
        }
        .task { if !store.hasLoaded { await store.load(env.api, campaignID: campaignID) } }
        .onChange(of: env.realtime.pulse(for: .campaigns)) {
            Task { await store.load(env.api, campaignID: campaignID) }
        }
    }
}

struct CampaignStepRow: View {
    let step: CampaignStep

    private var waitLine: String? {
        guard let wait = step.waitAfter, wait > 0 else { return nil }
        return "wait \(wait)d after"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 10) {
                Text("\(step.position ?? 0)")
                    .font(.footnote.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(Tone.sky.color)
                    .frame(width: 28, height: 28)
                    .background(Tone.sky.background, in: RoundedRectangle(cornerRadius: 8))
                Text(step.name ?? "Step")
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                Spacer(minLength: 8)
                if let wait = waitLine {
                    Text(wait)
                        .font(.caption)
                        .monospacedDigit()
                        .foregroundStyle(.tertiary)
                }
            }
            if let subject = step.subject, !subject.isEmpty {
                Text(subject)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            let preview = step.bodyPreview
            if !preview.isEmpty {
                Text(preview)
                    .font(.subheadline)
                    .foregroundStyle(.tertiary)
                    .lineLimit(2)
            }
        }
        .padding(.vertical, 6)
    }
}

// MARK: - Senders

@MainActor
@Observable
final class CampaignSendersStore {
    private(set) var senders: [CampaignSender] = []
    /// email_account_id -> address, resolved from GET /emails.
    private(set) var addresses: [String: CampaignSenderAccount] = [:]
    private(set) var isLoading = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    func load(_ api: APIClient, campaignID: String) async {
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            let page: CampaignSendersPage = try await api.get("campaigns/\(campaignID)/senders")
            let list = page.data ?? []
            // Resolve account ids to addresses; best-effort, never blocks the list.
            var lookup: [String: CampaignSenderAccount] = [:]
            if !list.isEmpty,
               let accounts: CampaignSenderAccountsPage = try? await api.get("emails", query: ["limit": "200"]) {
                for account in accounts.data ?? [] { lookup[account.id] = account }
            }
            withAnimation(.snappy) {
                senders = list
                addresses = lookup
            }
            errorMessage = nil
            hasLoaded = true
        } catch {
            if !hasLoaded { errorMessage = error.localizedDescription }
        }
        isLoading = false
    }
}

struct CampaignSendersTab: View {
    @Environment(AppEnvironment.self) private var env
    let campaign: Campaign
    let store: CampaignSendersStore

    var body: some View {
        Group {
            if store.isLoading, !store.hasLoaded {
                ScrollView { SkeletonRows(rows: 5) }
            } else if let error = store.errorMessage, store.senders.isEmpty {
                ErrorStateView(title: "Couldn't load senders", message: error) {
                    await store.load(env.api, campaignID: campaign.id)
                }
            } else if store.senders.isEmpty {
                EmptyStateView(
                    title: "No senders assigned",
                    message: campaign.senderStrategy == "tags"
                        ? "This campaign sends from tagged mailboxes. Assign tags on the web dashboard."
                        : "Assign sender mailboxes on the web dashboard."
                )
            } else {
                List {
                    ForEach(store.senders) { sender in
                        CampaignSenderRow(sender: sender, account: store.addresses[sender.emailAccountID])
                    }
                }
                .listStyle(.plain)
                .refreshable { await store.load(env.api, campaignID: campaign.id) }
            }
        }
        .task { if !store.hasLoaded { await store.load(env.api, campaignID: campaign.id) } }
        .onChange(of: env.realtime.pulse(for: .emailAccounts)) {
            Task { await store.load(env.api, campaignID: campaign.id) }
        }
    }
}

struct CampaignSenderRow: View {
    let sender: CampaignSender
    let account: CampaignSenderAccount?

    private var title: String {
        account?.email ?? sender.emailAccountID
    }

    var body: some View {
        HStack(spacing: 12) {
            IconTile(symbol: "envelope", tone: sender.enabled == false ? .slate : .sky, size: 36)
            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                HStack(spacing: 4) {
                    if let provider = account?.provider, !provider.isEmpty {
                        Text(provider)
                    }
                    if let sent = sender.lastSentAt {
                        if account?.provider?.isEmpty == false { Text("·") }
                        Text("last sent \(WFormat.relative(sent))")
                    }
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            }
            Spacer(minLength: 8)
            if sender.enabled == false {
                StatusPill(text: "paused", tone: .slate)
            } else if let weight = sender.weight, weight != 1 {
                Text("×\(weight)")
                    .font(.footnote.weight(.medium))
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 6)
    }
}
