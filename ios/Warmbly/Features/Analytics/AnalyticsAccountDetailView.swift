import SwiftUI

/// Per-mailbox health detail, backed by GET analytics/accounts/:id.
/// Seeded with the list row so the push renders instantly. One flat plain
/// list: eyebrow captions as bare rows, bare stat columns, hairlines.
struct AnalyticsAccountDetailView: View {
    @Environment(AppEnvironment.self) private var env
    let account: AccountHealthRow

    @State private var detail: AccountHealthRow?
    @State private var errorMessage: String?

    private var current: AccountHealthRow { detail ?? account }
    private var presenceKey: String { "mailbox:\(account.id)" }

    var body: some View {
        List {
            headerSection
            healthSection
            usageSection
            warmupSection
            errorsSection
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
        .navigationTitle(current.email ?? "Mailbox")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .onChange(of: env.realtime.pulse(for: .emailAccounts)) {
            Task { await load() }
        }
        .presenceResource(presenceKey)
    }

    private func load() async {
        do {
            let fresh: AccountHealthRow = try await env.api.get("analytics/accounts/\(account.id)")
            detail = fresh
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    // MARK: Sections

    @ViewBuilder
    private var headerSection: some View {
        HStack(spacing: 12) {
            IconTile(symbol: "envelope", tone: AnalyticsToneMap.health(current.health?.status), size: 42)
            VStack(alignment: .leading, spacing: 3) {
                Text(current.email ?? "mailbox")
                    .font(.body.weight(.semibold))
                    .lineLimit(1)
                HStack(spacing: 4) {
                    if let provider = current.provider, !provider.isEmpty {
                        Text(provider)
                    }
                    if let synced = current.lastSyncedAt {
                        Text("· synced \(WFormat.relative(synced))")
                    }
                    if current.inCampaign == true {
                        Text("· in campaign")
                    }
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            }
            Spacer()
            ResourceViewers(resource: presenceKey)
        }
        .padding(.top, 16)
        .padding(.bottom, 6)
        .analyticsPlainRow(separator: .hidden)
        if let errorMessage, detail == nil {
            AnalyticsSectionError(text: errorMessage)
                .analyticsPlainRow(separator: .hidden)
        }
    }

    @ViewBuilder
    private var healthSection: some View {
        AnalyticsSectionCaption("Health")
        HStack(alignment: .top, spacing: 14) {
            AnalyticsMiniStat(
                label: "Status",
                value: current.health?.status ?? "unknown",
                tone: AnalyticsToneMap.health(current.health?.status)
            )
            VStack(alignment: .leading, spacing: 4) {
                EyebrowLabel("Score")
                HealthRing(
                    score: current.health?.score,
                    tone: AnalyticsToneMap.health(current.health?.status),
                    size: 38
                )
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            AnalyticsMiniStat(label: "Sync status", value: current.status ?? "–")
        }
        .padding(.vertical, 6)
        .analyticsPlainRow(separator: .hidden)
        ForEach(current.health?.issues ?? [], id: \.self) { issue in
            HStack(alignment: .top, spacing: 8) {
                Circle()
                    .fill(WTheme.warning)
                    .frame(width: 5, height: 5)
                    .padding(.top, 6)
                Text(issue)
                    .font(.subheadline)
                    .foregroundStyle(.primary)
            }
            .padding(.vertical, 4)
            .analyticsPlainRow(separator: .hidden)
        }
    }

    @ViewBuilder
    private var usageSection: some View {
        if let usage = current.dailyUsage {
            AnalyticsSectionCaption("Today's usage")
            AnalyticsUsageBarRow(
                label: "Campaign",
                sent: usage.campaignSent ?? 0,
                limit: usage.campaignLimit ?? 0,
                tint: WTheme.accent
            )
            .analyticsPlainRow(separator: .hidden)
            if let warmupLimit = usage.warmupLimit, warmupLimit > 0 {
                AnalyticsUsageBarRow(
                    label: "Warmup",
                    sent: usage.warmupSent ?? 0,
                    limit: warmupLimit,
                    tint: Tone.orange.color
                )
                .analyticsPlainRow(separator: .hidden)
            }
        }
    }

    @ViewBuilder
    private var warmupSection: some View {
        if current.warmupStatus != nil || current.warmupHealth != nil {
            AnalyticsSectionCaption("Warmup", icon: "flame", iconTone: .orange)
            if let status = current.warmupStatus {
                HStack(spacing: 14) {
                    AnalyticsMiniStat(label: "Volume", value: "\(status.currentVolume ?? 0)")
                    AnalyticsMiniStat(label: "Target", value: "\(status.targetVolume ?? 0)")
                    AnalyticsMiniStat(label: "Max", value: "\(status.maxVolume ?? 0)")
                }
                .padding(.vertical, 6)
                .analyticsPlainRow(separator: .hidden)
                HStack(spacing: 14) {
                    AnalyticsMiniStat(label: "Reply rate", value: "\(status.replyRate ?? 0)%")
                    AnalyticsMiniStat(label: "Days active", value: "\(status.daysActive ?? 0)")
                    AnalyticsMiniStat(
                        label: "State",
                        value: status.paused == true ? "paused" : (status.enabled == true ? "warming" : "off"),
                        tone: status.paused == true ? .amber : (status.enabled == true ? .orange : .slate)
                    )
                }
                .padding(.vertical, 6)
                .analyticsPlainRow(separator: .hidden)
            }
            if let health = current.warmupHealth {
                HStack(spacing: 8) {
                    StatusPill(text: health.state ?? "unknown", tone: AnalyticsToneMap.warmupState(health.state))
                    if let spam = health.spamScore {
                        Text("spam \(spam)")
                            .font(.footnote)
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                    if let blockedUntil = health.blockedUntil {
                        Text("blocked until \(blockedUntil.formatted(date: .abbreviated, time: .shortened))")
                            .font(.footnote)
                            .monospacedDigit()
                            .foregroundStyle(WTheme.negative)
                    }
                    Spacer()
                }
                .padding(.vertical, 4)
                .analyticsPlainRow(separator: .hidden)
                if let reason = health.reason, !reason.isEmpty {
                    Text(reason)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .padding(.vertical, 2)
                        .analyticsPlainRow(separator: .hidden)
                }
            }
        }
    }

    @ViewBuilder
    private var errorsSection: some View {
        let errors = current.errors ?? []
        if !errors.isEmpty {
            AnalyticsSectionCaption("Errors")
            ForEach(errors) { item in
                HStack(alignment: .top, spacing: 12) {
                    IconTile(symbol: "exclamationmark.triangle", tone: severityTone(item.severity), size: 34)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(item.title ?? item.errorCode ?? "Error")
                            .font(.body.weight(.medium))
                        if let message = item.message, !message.isEmpty {
                            Text(message)
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                        if let action = item.actionRequired, !action.isEmpty {
                            Text(action)
                                .font(.footnote.weight(.medium))
                                .foregroundStyle(WTheme.warning)
                        }
                    }
                    Spacer()
                    if let created = item.createdAt {
                        Text(WFormat.relative(created))
                            .font(.footnote)
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.vertical, 8)
                .analyticsPlainRow(separatorLeading: 62)
            }
        }
    }

    private func severityTone(_ raw: String?) -> Tone {
        switch raw {
        case "critical", "error": .rose
        case "warning": .amber
        default: .slate
        }
    }
}

/// "42 of 50 / day" with a thin progress track.
struct AnalyticsUsageBarRow: View {
    let label: String
    let sent: Int
    let limit: Int
    let tint: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(label)
                    .font(.subheadline.weight(.medium))
                Spacer()
                Text(limit > 0 ? "\(sent) of \(limit) / day" : "\(sent) sent")
                    .font(.footnote)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
            ProgressView(value: limit > 0 ? min(1, Double(sent) / Double(limit)) : 0)
                .tint(tint)
        }
        .padding(.vertical, 6)
    }
}
