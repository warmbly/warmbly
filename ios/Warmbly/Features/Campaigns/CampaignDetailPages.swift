import SwiftUI
import Charts

// Pushed sub-pages of the campaign hub (Leads / Sequence / Senders) plus the
// analytics store the hub's performance section reads. Each page: SkeletonRows
// on first load, stale data during reloads, ErrorStateView on failure,
// EmptyStateView when empty, and a reload on the relevant realtime pulse.
// Heavy editing (writing steps, assigning senders) is a web-dashboard job;
// those pages carry a WebHandoffBanner instead of half an editor.

// MARK: - Analytics store (hub performance section)

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

/// 30-day sent/replies chart cell for the hub's performance card. Hidden
/// entirely until there are at least two days with any volume.
struct CampaignDailyChartRow: View {
    let daily: [CampaignDailyStat]

    private var chartDays: [(id: Int, day: Date, sent: Int, replies: Int)] {
        daily.enumerated().compactMap { index, day in
            guard let date = CampaignDates.day(from: day.date) else { return nil }
            return (id: index, day: date, sent: day.sent ?? 0, replies: day.replies ?? 0)
        }
    }

    var body: some View {
        if chartDays.count > 1, chartDays.contains(where: { $0.sent > 0 }) {
            VStack(alignment: .leading, spacing: 8) {
                Text("Last 30 days")
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
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
            }
            .padding(.vertical, 6)
        }
    }
}

// MARK: - Sequence

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

struct CampaignSequencePage: View {
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
                EmptyStateView(title: "No steps yet", message: "Write the sequence in Warmbly on the web; every step shows up here.")
            } else {
                List {
                    ForEach(store.steps) { step in
                        NavigationLink {
                            CampaignStepReader(step: step)
                        } label: {
                            CampaignStepRow(step: step)
                        }
                    }
                    WebHandoffBanner(text: "Writing and rearranging steps needs the full sequence editor. Open this campaign in Warmbly on the web to edit it.")
                        .listRowSeparator(.hidden)
                        .listRowInsets(EdgeInsets(top: 14, leading: 16, bottom: 20, trailing: 16))
                }
                .listStyle(.plain)
                .refreshable { await store.load(env.api, campaignID: campaignID) }
            }
        }
        .navigationTitle("Sequence")
        .navigationBarTitleDisplayMode(.inline)
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

/// Read-only step viewer: subject line and the full body exactly as it sends
/// (HTML through the shared webview, plain text as a fallback).
struct CampaignStepReader: View {
    let step: CampaignStep

    @State private var bodyHeight: CGFloat = 120

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                VStack(alignment: .leading, spacing: 6) {
                    HStack(spacing: 8) {
                        Text("Step \(step.position ?? 0)")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(Tone.sky.color)
                            .padding(.horizontal, 8)
                            .padding(.vertical, 3)
                            .background(Tone.sky.background, in: Capsule())
                        if let wait = step.waitAfter, wait > 0 {
                            Text("sends \(wait) day\(wait == 1 ? "" : "s") after the previous step")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                    if let subject = step.subject, !subject.isEmpty {
                        Text(subject)
                            .font(.title3.weight(.semibold))
                    }
                }
                Divider()
                if step.bodyHTML?.isEmpty == false || step.bodyPlain?.isEmpty == false {
                    UniboxWebView(html: step.bodyHTML, plain: step.bodyPlain, height: $bodyHeight)
                        .frame(height: bodyHeight)
                } else {
                    Text("This step has no body yet.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                WebHandoffBanner(text: "Edit this step in the sequence editor in Warmbly on the web.")
                    .padding(.top, 6)
            }
            .padding(16)
        }
        .background(Color(.systemBackground))
        .navigationTitle(step.name ?? "Step")
        .navigationBarTitleDisplayMode(.inline)
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

struct CampaignSendersPageView: View {
    @Environment(AppEnvironment.self) private var env
    let campaign: Campaign
    let store: CampaignSendersStore

    private var strategyLine: String {
        campaign.senderStrategy == "tags"
            ? "Sends from mailboxes matching the campaign's tags."
            : "Sends from the mailboxes listed here."
    }

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
                    Section {
                        ForEach(store.senders) { sender in
                            CampaignSenderRow(sender: sender, account: store.addresses[sender.emailAccountID])
                        }
                    } footer: {
                        Text(strategyLine)
                    }
                    WebHandoffBanner(text: "Assigning mailboxes, tags and rotation weights happens in the campaign preferences in Warmbly on the web.")
                        .listRowSeparator(.hidden)
                        .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 20, trailing: 16))
                }
                .listStyle(.plain)
                .refreshable { await store.load(env.api, campaignID: campaign.id) }
            }
        }
        .navigationTitle("Senders")
        .navigationBarTitleDisplayMode(.inline)
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
