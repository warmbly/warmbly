import SwiftUI

enum MailboxDetailTab: String, CaseIterable, Identifiable {
    case overview, warmup, setup

    var id: String { rawValue }

    var title: String {
        switch self {
        case .overview: return "Overview"
        case .warmup: return "Warmup"
        case .setup: return "Setup"
        }
    }

    var icon: String {
        switch self {
        case .overview: return "waveform.path.ecg"
        case .warmup: return "flame.fill"
        case .setup: return "checkmark.shield.fill"
        }
    }
}

/// Mailbox detail: sky hero with health/usage/warmup chips, swipeable tabs for
/// Overview / Warmup / Setup. Pushed onto the Accounts tab stack, so it does
/// NOT create its own NavigationStack. Claims presence on `mailbox:<id>` and
/// reloads on the emailAccounts/analytics pulse.
struct MailboxDetailView: View {
    @Environment(AppEnvironment.self) private var env

    @State private var store: MailboxDetailStore
    @State private var tab: MailboxDetailTab = .overview

    init(account: EmailAccount) {
        _store = State(initialValue: MailboxDetailStore(account: account))
    }

    private var account: EmailAccount { store.account }
    private var health: MailboxHealth? { store.analytics?.health }
    private var canManage: Bool { env.session.can(.manageEmails) }

    var body: some View {
        AirDetailScaffold(
            tabs: MailboxDetailTab.allCases.map { AirTabItem(id: $0.rawValue, title: $0.title, icon: $0.icon) },
            selection: Binding(
                get: { tab.rawValue },
                set: { tab = MailboxDetailTab(rawValue: $0) ?? .overview }
            )
        ) {
            hero
        } content: {
            content
        }
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .presenceResource(store.presenceKey)
        .task { await store.loadAnalytics(env.api) }
        .task { await store.loadBanStatus(env.api) }
        .onChange(of: env.realtime.pulse(for: .emailAccounts)) {
            Task { await store.loadAnalytics(env.api) }
        }
        .onChange(of: env.realtime.pulse(for: .analytics)) {
            Task { await store.loadAnalytics(env.api) }
        }
        .alert(
            "Something went wrong",
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

    // MARK: Hero

    private var hero: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .center, spacing: 12) {
                WAvatar(name: account.name ?? account.email, seed: account.id, size: 46)
                    .overlay(Circle().strokeBorder(.white.opacity(0.55), lineWidth: 1.5))
                VStack(alignment: .leading, spacing: 5) {
                    Text(account.email)
                        .font(.title2.bold())
                        .foregroundStyle(.white)
                        .lineLimit(2)
                        .minimumScaleFactor(0.7)
                        .textSelection(.enabled)
                    HStack(spacing: 8) {
                        heroStatusPill
                        Text(heroMetaLine)
                            .font(.footnote)
                            .foregroundStyle(.white.opacity(0.72))
                            .lineLimit(1)
                    }
                    ResourceViewers(resource: store.presenceKey)
                }
            }
            heroStats
        }
        .padding(.horizontal, 20)
        .padding(.top, 2)
        .padding(.bottom, 18)
    }

    /// Connection + warmup state in one glass capsule.
    private var heroStatusPill: some View {
        HStack(spacing: 5) {
            Circle()
                .fill(account.statusTone.color)
                .frame(width: 7, height: 7)
                .modifier(PingEffect(active: account.isWarmingActive, color: account.statusTone.color))
            Text(account.statusLabel)
                .font(.caption.weight(.semibold))
                .foregroundStyle(.white)
            Text("· \(account.warmupState.title.lowercased())")
                .font(.caption.weight(.medium))
                .foregroundStyle(.white.opacity(0.78))
        }
        .padding(.horizontal, 9)
        .padding(.vertical, 4)
        .background(.white.opacity(0.16), in: Capsule())
    }

    private var heroMetaLine: String {
        if let name = account.name, !name.isEmpty {
            return "\(account.providerLabel) · \(name)"
        }
        return account.providerLabel
    }

    private var heroStats: some View {
        let usage = store.analytics?.dailyUsage
        let ws = store.analytics?.warmupStatus
        return HStack(spacing: 10) {
            AirStatChip(
                value: health?.score.map { "\($0)" } ?? "–",
                label: "Health",
                symbol: "heart.fill"
            )
            AirStatChip(
                value: usage?.campaignSent.map { "\($0)/\(usage?.campaignLimit ?? account.campaignLimit ?? 50)" } ?? "–",
                label: "Sent today",
                symbol: "paperplane.fill"
            )
            AirStatChip(
                value: ws.flatMap { s in s.currentVolume.map { "\($0)/\(s.targetVolume ?? 0)" } } ?? "–",
                label: "Warmup",
                symbol: "flame.fill"
            )
        }
    }

    // MARK: Content

    /// Horizontal page swipe between tabs, synced with the pill bar.
    private var content: some View {
        TabView(selection: $tab) {
            overviewTab
                .tag(MailboxDetailTab.overview)
            warmupTab
                .tag(MailboxDetailTab.warmup)
            setupTab
                .tag(MailboxDetailTab.setup)
        }
        .tabViewStyle(.page(indexDisplayMode: .never))
        .animation(.snappy, value: tab)
    }

    // MARK: Overview tab

    private var overviewTab: some View {
        List {
            if let health {
                healthSection(health)
            } else {
                Section("Health") {
                    Text("Health data isn't available yet.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .listStyle(.insetGrouped)
        .scrollContentBackground(.hidden)
    }

    private func healthSection(_ health: MailboxHealth) -> some View {
        Section("Health") {
            HStack(spacing: 14) {
                HealthRing(score: health.score, tone: health.tone, size: 44)
                VStack(alignment: .leading, spacing: 2) {
                    Text(health.label)
                        .font(.body.weight(.medium))
                        .foregroundStyle(health.tone.color)
                    Text("Health score")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }
            .padding(.vertical, 4)
            if let issues = health.issues, !issues.isEmpty {
                ForEach(Array(issues.enumerated()), id: \.offset) { _, issue in
                    Label(issue, systemImage: "exclamationmark.triangle")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            if let errors = store.analytics?.errors, !errors.isEmpty {
                ForEach(errors) { error in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(error.title ?? "Issue")
                            .font(.body.weight(.medium))
                            .foregroundStyle(error.tone.color)
                        if let message = error.message {
                            Text(message)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                        if let action = error.actionRequired, !action.isEmpty {
                            Text(action)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .padding(.vertical, 2)
                }
            }
        }
    }

    // MARK: Warmup tab

    private var warmupTab: some View {
        List {
            warmupSection
        }
        .listStyle(.insetGrouped)
        .scrollContentBackground(.hidden)
    }

    @ViewBuilder
    private var warmupSection: some View {
        Section("Warmup") {
            if let ws = store.analytics?.warmupStatus {
                HStack(spacing: 12) {
                    warmupStat("\(ws.currentVolume ?? 0)/\(ws.targetVolume ?? 0)", label: "Today")
                    warmupStat("\(ws.maxVolume ?? account.warmupMax ?? 40)", label: "Ceiling /day")
                    warmupStat("\(ws.daysActive ?? 0)", label: "Days warming")
                }
                .padding(.vertical, 6)
            } else {
                Text(account.warmupState.title)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }

            if let wh = store.analytics?.warmupHealth, wh.isDegraded {
                StatusPill(text: wh.stateLabel, tone: wh.tone)
            }

            if let ban = store.banStatus, ban.blocked == true {
                Label(ban.reason ?? "Blocked from the warmup pool", systemImage: "hand.raised")
                    .font(.footnote)
                    .foregroundStyle(WTheme.negative)
            }

            if canManage {
                warmupControls
            }
        }
    }

    private func warmupStat(_ value: String, label: String) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(value)
                .font(.system(size: 22, weight: .bold, design: .rounded))
                .monospacedDigit()
                .contentTransition(.numericText())
            Text(label)
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private var warmupControls: some View {
        switch account.warmupState {
        case .active:
            Button {
                Task { await store.warmupAction(env.api, "pause") }
            } label: {
                Label("Pause warmup", systemImage: "pause.fill")
            }
            Button(role: .destructive) {
                Task { await store.warmupAction(env.api, "stop") }
            } label: {
                Label("Stop and reset", systemImage: "stop.fill")
            }
        case .paused:
            Button {
                Task { await store.warmupAction(env.api, "resume") }
            } label: {
                Label("Resume warmup", systemImage: "flame.fill")
            }
            .buttonStyle(.borderedProminent)
            .tint(WTheme.accent)
            Button(role: .destructive) {
                Task { await store.warmupAction(env.api, "stop") }
            } label: {
                Label("Stop and reset", systemImage: "stop.fill")
            }
        case .off:
            Button {
                Task { await store.warmupAction(env.api, "start") }
            } label: {
                Label("Start warmup", systemImage: "flame.fill")
            }
            .buttonStyle(.borderedProminent)
            .tint(WTheme.accent)
        }
    }

    // MARK: Setup tab (domain auth + identity details)

    private var setupTab: some View {
        List {
            authSection
            identitySection
        }
        .listStyle(.insetGrouped)
        .scrollContentBackground(.hidden)
    }

    @ViewBuilder
    private var authSection: some View {
        Section("Domain authentication") {
            if let auth = store.authCheck {
                authRow("SPF", ok: auth.spfFound == true)
                authRow("DKIM", ok: auth.dkimFound == true)
                authRow("DMARC", ok: auth.dmarcFound == true)
                if let summary = auth.summary, !summary.isEmpty {
                    Text(summary)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            } else {
                Text("Check \(account.senderDomain) for SPF, DKIM, and DMARC alignment.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Button {
                Task { await store.runAuthCheck(env.api) }
            } label: {
                if store.isCheckingAuth {
                    ProgressView().controlSize(.small)
                } else {
                    Text(store.authCheck == nil ? "Check now" : "Re-check")
                }
            }
            .disabled(store.isCheckingAuth)
        }
    }

    private func authRow(_ label: String, ok: Bool) -> some View {
        HStack(spacing: 12) {
            IconTile(symbol: ok ? "checkmark.seal.fill" : "xmark.circle", tone: ok ? .emerald : .slate, size: 34)
            Text(label)
                .font(.body.weight(.medium))
            Spacer()
            Text(ok ? "Found" : "Missing")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
    }

    private var identitySection: some View {
        Section("Details") {
            if let name = account.name, !name.isEmpty {
                LabeledContent("Sender name", value: name)
            }
            LabeledContent("Provider", value: account.providerLabel)
            LabeledContent("Daily cap") {
                Text("\(account.campaignLimit ?? 50)/day")
                    .monospacedDigit()
            }
            LabeledContent("Min gap") {
                Text(MailboxFormat.gap(account.minWaitTime ?? 600))
                    .monospacedDigit()
            }
            if let replyTo = account.replyTo, !replyTo.isEmpty {
                LabeledContent("Reply-to", value: replyTo)
            }
            if let synced = account.lastSyncedAt {
                LabeledContent("Last synced", value: WFormat.relative(synced))
            }
            if let created = account.createdAt {
                LabeledContent("Connected", value: WFormat.relative(created))
            }
        }
    }
}
