import SwiftUI

/// Read-only billing surface, flattened: plan header with pill + renewal,
/// usage as thin accent bars on hairline tracks, and the manage-on-web
/// handoff. Changing plans stays on the web dashboard.
struct BillingView: View {
    @Environment(AppEnvironment.self) private var env

    @State private var subscription: SubscriptionInfo?
    @State private var trial: TrialInfo?
    @State private var loaded = false
    @State private var errorMessage: String?

    var body: some View {
        Group {
            if !loaded, subscription == nil, errorMessage == nil {
                loadingPlaceholder
            } else if let errorMessage, subscription == nil {
                ErrorStateView(title: "Couldn't load billing", message: errorMessage) {
                    await load()
                }
            } else {
                content
            }
        }
        .navigationTitle("Billing")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            if !loaded { await load() }
        }
        .refreshable { await load() }
        .onChange(of: env.realtime.pulse(for: .billing)) { _, _ in
            Task { await load() }
        }
    }

    // MARK: - Content

    private var content: some View {
        List {
            planSection
            if let trial, isTrialing {
                trialSection(trial)
            }
            usageSection
            manageSection
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .background(Color(.systemBackground))
        .environment(\.defaultMinListRowHeight, 0)
    }

    // MARK: Plan

    @ViewBuilder
    private var planSection: some View {
        MoreFlatSectionHeader("Plan", top: 8)
        HStack(spacing: 12) {
            IconTile(symbol: "creditcard.fill", tone: .indigo, size: 34)
            VStack(alignment: .leading, spacing: 2) {
                Text(planName)
                    .font(.body.weight(.semibold))
                if let period = renewalText {
                    Text(period)
                        .font(.footnote)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 8)
            MorePlanPill(subscription: subscription)
        }
        .padding(.vertical, 10)
        .moreFlatRow(textLeading: MoreFlatMetrics.tileTextLeading)
        if let price = subscription?.plan?.price, price > 0 {
            HStack {
                Text("Price")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Spacer()
                Text(priceText(price))
                    .font(.subheadline.weight(.medium))
                    .monospacedDigit()
            }
            .padding(.vertical, 11)
            .moreFlatRow(separator: .hidden)
        }
        if subscription?.cancelAtPeriodEnd == true {
            Label("Cancels at the end of the current period", systemImage: "exclamationmark.circle")
                .font(.footnote)
                .foregroundStyle(WTheme.warning)
                .padding(.vertical, 8)
                .moreFlatRow(separator: .hidden)
        }
    }

    // MARK: Trial

    @ViewBuilder
    private func trialSection(_ trial: TrialInfo) -> some View {
        MoreFlatSectionHeader("Trial")
        HStack(spacing: 12) {
            IconTile(symbol: "clock.badge.fill", tone: trialTone(trial), size: 34)
            VStack(alignment: .leading, spacing: 2) {
                Text("Free trial")
                    .font(.body.weight(.medium))
                if let ends = trial.trialEndsAt {
                    Text("Ends \(ends.formatted(.dateTime.month().day().year()))")
                        .font(.footnote)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 8)
            if trial.isExpired == true {
                StatusPill(text: "Expired", tone: .rose)
            } else if let days = trial.daysRemaining {
                StatusPill(text: days == 1 ? "1 day left" : "\(days) days left", tone: days <= 3 ? .amber : .emerald)
            }
        }
        .padding(.vertical, 10)
        .moreFlatRow(separator: .hidden, textLeading: MoreFlatMetrics.tileTextLeading)
    }

    private func trialTone(_ trial: TrialInfo) -> Tone {
        if trial.isExpired == true { return .rose }
        if let days = trial.daysRemaining, days <= 3 { return .amber }
        return .emerald
    }

    // MARK: Usage

    @ViewBuilder
    private var usageSection: some View {
        MoreFlatSectionHeader("Usage this workspace")
        let counts = env.session.currentOrg?.counts
        let limits = env.session.currentOrg?.limits
        usageRow(MoreUsageBar(label: "Email accounts", used: counts?.emailAccounts, limit: limits?.maxEmailAccounts))
        usageRow(MoreUsageBar(label: "Active campaigns", used: counts?.activeCampaigns, limit: limits?.maxActiveCampaigns))
        usageRow(MoreUsageBar(label: "Contacts", used: counts?.totalContacts, limit: limits?.maxContacts))
        usageRow(MoreUsageBar(label: "Team members", used: counts?.totalMembers, limit: limits?.maxTeamMembers))
        footnoteRow("Usage reflects the current workspace against your plan limits.")
    }

    private func usageRow(_ bar: MoreUsageBar) -> some View {
        bar
            .padding(.vertical, 4)
            .moreFlatRow(separator: .hidden)
    }

    // MARK: Manage on the web

    @ViewBuilder
    private var manageSection: some View {
        MoreFlatSectionHeader("Manage")
        Link(destination: URL(string: "https://app.warmbly.com/app/settings/billing")!) {
            HStack(spacing: 12) {
                IconTile(symbol: "safari.fill", tone: .sky, size: 34)
                Text("Manage billing on the web")
                    .font(.body.weight(.medium))
                    .foregroundStyle(.primary)
                Spacer(minLength: 8)
                Image(systemName: "arrow.up.forward.square")
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(.tertiary)
            }
            .padding(.vertical, 11)
            .contentShape(Rectangle())
        }
        .buttonStyle(TapScaleStyle())
        .moreFlatRow(separator: .hidden, textLeading: MoreFlatMetrics.tileTextLeading)
        footnoteRow("Change plan, update payment method, and download invoices from the web dashboard.", bottom: 28)
    }

    private func footnoteRow(_ text: String, bottom: CGFloat = 4) -> some View {
        Text(text)
            .font(.footnote)
            .foregroundStyle(.secondary)
            .padding(.top, 6)
            .padding(.bottom, bottom)
            .moreFlatRow(separator: .hidden)
    }

    private var loadingPlaceholder: some View {
        VStack(spacing: 16) {
            SkeletonRows(rows: 5)
        }
        .padding(.top, 8)
    }

    // MARK: - Derived

    private var planName: String {
        let raw = subscription?.plan?.name ?? "Free"
        return raw.isEmpty ? "Free" : raw.capitalized
    }

    private var isTrialing: Bool {
        subscription?.status == "trialing" || trial?.isInTrial == true
    }

    private var renewalText: String? {
        guard let end = subscription?.currentPeriodEnd else { return nil }
        let formatted = end.formatted(.dateTime.month().day().year())
        return subscription?.cancelAtPeriodEnd == true ? "Access until \(formatted)" : "Renews \(formatted)"
    }

    private func priceText(_ price: Double) -> String {
        let unit = subscription?.plan?.duration.map { "/\($0)" } ?? ""
        return String(format: "$%.0f%@", price, unit)
    }

    // MARK: - Load

    private func load() async {
        errorMessage = nil
        do {
            async let sub: SubscriptionInfo = env.api.get("subscription")
            async let tr: TrialInfo = env.api.get("subscription/trial")
            subscription = try await sub
            trial = try? await tr
            await env.session.refreshCurrentOrg()
        } catch {
            errorMessage = (error as? APIError)?.errorDescription ?? error.localizedDescription
        }
        loaded = true
    }
}
