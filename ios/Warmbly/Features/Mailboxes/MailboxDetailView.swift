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
    private enum SetupField: Hashable { case name, replyTo }

    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    @State private var store: MailboxDetailStore
    @State private var tab: MailboxDetailTab = .overview
    @State private var displayName = ""
    @State private var replyTo = ""
    @State private var fieldsSeeded = false
    @State private var confirmDisconnect = false
    @FocusState private var focusedField: SetupField?

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
        .task {
            seedFieldsIfNeeded()
            await store.loadAnalytics(env.api)
        }
        .task { await store.loadBanStatus(env.api) }
        .onChange(of: env.realtime.pulse(for: .emailAccounts)) {
            Task { await store.loadAnalytics(env.api) }
        }
        .onChange(of: env.realtime.pulse(for: .analytics)) {
            Task { await store.loadAnalytics(env.api) }
        }
        .onChange(of: focusedField) { previous, current in
            // Text rows save on focus loss, like the profile settings rows.
            if previous == .name, current != .name { saveDisplayName() }
            if previous == .replyTo, current != .replyTo { saveReplyTo() }
        }
        .confirmationDialog(
            "Disconnect \(account.email)?",
            isPresented: $confirmDisconnect,
            titleVisibility: .visible
        ) {
            Button("Disconnect", role: .destructive) {
                Task {
                    if await store.deleteAccount(env.api) { dismiss() }
                }
            }
        } message: {
            Text("Campaigns stop sending from it and history stays.")
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
        .listStyle(.plain)
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
            rampSection
            windowSection
        }
        .listStyle(.plain)
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

    // MARK: Warmup ramp (editable)

    // List responses omit warmup_reply_rate (decodes as 0), so prefer the
    // analytics snapshot until a PATCH refreshes the row.
    private var replyRateValue: Int {
        if store.rowEdited { return account.warmupReplyRate ?? 0 }
        return store.analytics?.warmupStatus?.replyRate ?? account.warmupReplyRate ?? 0
    }

    private var rampSection: some View {
        Section {
            settingStepper(
                "Starting volume", helper: "Emails per day when warmup begins.",
                value: account.warmupBase ?? 10, range: 1...100, suffix: "/day",
                write: { $0.warmupBase = $1 }, mutate: { $0.warmupBase = $1 }
            )
            settingStepper(
                "Daily increase", helper: "How many more each day as reputation builds.",
                value: account.warmupIncrease ?? 1, range: 1...50, suffix: "+/day",
                write: { $0.warmupIncrease = $1 }, mutate: { $0.warmupIncrease = $1 }
            )
            settingStepper(
                "Maximum volume", helper: "Keep conservative for new mailboxes, about 40 a day.",
                value: account.warmupMax ?? 40, range: max(1, account.warmupBase ?? 10)...500, suffix: "/day",
                write: { $0.warmupMax = $1 }, mutate: { $0.warmupMax = $1 }
            )
            settingStepper(
                "Reply rate", helper: "Share of warmup mail that gets a reply.",
                value: replyRateValue, range: 0...100, suffix: "%",
                write: { $0.warmupReplyRate = $1 }, mutate: { $0.warmupReplyRate = $1 }
            )
        } header: {
            EyebrowLabel("Ramp configuration")
        }
    }

    // MARK: Sending window (editable)

    /// HH:MM in 30-minute steps, the format the scheduler parses ("15:04").
    private static let timeSlots: [String] = (0..<48).map {
        String(format: "%02d:%02d", $0 / 2, $0 % 2 * 30)
    }

    /// Backend bit layout is Go time.Weekday: bit 0 = Sunday ... bit 6 =
    /// Saturday (scheduler findNextValidDay); mask 0 means every day.
    private static let dayBits: [(label: String, bit: Int)] = [
        ("Mon", 1), ("Tue", 2), ("Wed", 3), ("Thu", 4), ("Fri", 5), ("Sat", 6), ("Sun", 0),
    ]

    private var windowSection: some View {
        Section {
            timeRow("Start time", current: timeValue(account.warmupStartTime, fallback: "08:00")) { slot in
                Task {
                    await store.update(env.api, body: MailboxUpdateBody(warmupStartTime: slot)) {
                        $0.warmupStartTime = slot
                    }
                }
            }
            timeRow("End time", current: timeValue(account.warmupEndTime, fallback: "20:00")) { slot in
                Task {
                    await store.update(env.api, body: MailboxUpdateBody(warmupEndTime: slot)) {
                        $0.warmupEndTime = slot
                    }
                }
            }
            sendingDaysRow
        } header: {
            EyebrowLabel("Sending window")
        }
    }

    private func timeValue(_ raw: String?, fallback: String) -> String {
        guard let raw, !raw.isEmpty else { return fallback }
        return raw
    }

    private func timeRow(_ title: String, current: String, apply: @escaping (String) -> Void) -> some View {
        HStack(spacing: 12) {
            Text(title)
                .font(.body.weight(.medium))
            Spacer(minLength: 8)
            if canManage {
                Menu {
                    ForEach(Self.timeSlots, id: \.self) { slot in
                        Button {
                            apply(slot)
                        } label: {
                            if slot == current {
                                Label(slot, systemImage: "checkmark")
                            } else {
                                Text(slot)
                            }
                        }
                    }
                } label: {
                    HStack(spacing: 3) {
                        Text(current)
                            .font(.subheadline)
                            .monospacedDigit()
                        Image(systemName: "chevron.up.chevron.down")
                            .font(.system(size: 11, weight: .semibold))
                    }
                    .foregroundStyle(WTheme.accent)
                }
            } else {
                Text(current)
                    .font(.subheadline)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
    }

    private var sendingDaysRow: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Sending days")
                .font(.body.weight(.medium))
            HStack(spacing: 6) {
                ForEach(Self.dayBits, id: \.bit) { day in
                    dayChip(day.label, bit: day.bit)
                }
            }
            Text("Leave all off to send every day.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 6)
    }

    private func dayChip(_ label: String, bit: Int) -> some View {
        let mask = account.warmupDays ?? 0
        let on = mask & (1 << bit) != 0
        return Button {
            let next = mask ^ (1 << bit)
            Task {
                await store.update(env.api, body: MailboxUpdateBody(warmupDays: next)) {
                    $0.warmupDays = next
                }
            }
        } label: {
            Text(label)
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(on ? Color.white : Color.secondary)
                .frame(maxWidth: .infinity)
                .frame(height: 30)
                .background(on ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(Tone.slate.background), in: Capsule())
        }
        .buttonStyle(TapScaleStyle())
        .disabled(!canManage)
    }

    // MARK: Shared editing controls

    private func settingStepper(
        _ title: String,
        helper: String,
        value: Int,
        range: ClosedRange<Int>,
        suffix: String,
        write: @escaping (inout MailboxUpdateBody, Int) -> Void,
        mutate: @escaping (inout EmailAccount, Int) -> Void
    ) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.body.weight(.medium))
                Text(helper)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            if canManage {
                HStack(spacing: 10) {
                    stepButton("minus", enabled: value > range.lowerBound) {
                        step(to: max(range.lowerBound, value - 1), write: write, mutate: mutate)
                    }
                    Text("\(value)\(suffix)")
                        .font(.system(size: 15, weight: .semibold))
                        .monospacedDigit()
                        .frame(minWidth: 46)
                        .contentTransition(.numericText())
                    stepButton("plus", enabled: value < range.upperBound) {
                        step(to: min(range.upperBound, value + 1), write: write, mutate: mutate)
                    }
                }
            } else {
                Text("\(value)\(suffix)")
                    .font(.subheadline)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
    }

    private func step(
        to newValue: Int,
        write: @escaping (inout MailboxUpdateBody, Int) -> Void,
        mutate: @escaping (inout EmailAccount, Int) -> Void
    ) {
        Task {
            await store.updateDebounced(
                env.api,
                mutateBody: { write(&$0, newValue) },
                mutate: { mutate(&$0, newValue) }
            )
        }
    }

    private func stepButton(_ symbol: String, enabled: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 13, weight: .bold))
                .foregroundStyle(enabled ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(Color(.tertiaryLabel)))
                .frame(width: 30, height: 30)
                .background(Tone.slate.background, in: Circle())
        }
        .buttonStyle(TapScaleStyle())
        .disabled(!enabled)
    }

    // MARK: Setup tab (domain auth + identity details)

    private var setupTab: some View {
        List {
            senderProfileSection
            limitsSection
            authSection
            identitySection
            if canManage {
                dangerSection
            }
        }
        .listStyle(.plain)
    }

    // MARK: Sender profile (editable)

    private var senderProfileSection: some View {
        Section {
            fieldRow("Display name") {
                TextField("Sender name", text: $displayName)
                    .focused($focusedField, equals: .name)
                    .submitLabel(.done)
                    .onSubmit { saveDisplayName() }
                    .disabled(!canManage)
            }
            VStack(alignment: .leading, spacing: 4) {
                fieldRow("Reply-to") {
                    TextField(account.email, text: $replyTo)
                        .keyboardType(.emailAddress)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .focused($focusedField, equals: .replyTo)
                        .submitLabel(.done)
                        .onSubmit { saveReplyTo() }
                        .disabled(!canManage)
                }
                Text("Where replies land. Empty uses the mailbox address.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        } header: {
            EyebrowLabel("Sender profile")
        }
    }

    private func fieldRow<Field: View>(_ label: String, @ViewBuilder field: () -> Field) -> some View {
        HStack(spacing: 12) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 96, alignment: .leading)
            field()
        }
        .padding(.vertical, 4)
    }

    private func seedFieldsIfNeeded() {
        guard !fieldsSeeded else { return }
        displayName = account.name ?? ""
        replyTo = account.replyTo ?? ""
        fieldsSeeded = true
    }

    private func saveDisplayName() {
        let trimmed = displayName.trimmingCharacters(in: .whitespaces)
        guard trimmed != (account.name ?? "") else { return }
        Task {
            await store.update(env.api, body: MailboxUpdateBody(name: trimmed)) { $0.name = trimmed }
            displayName = store.account.name ?? trimmed
        }
    }

    private func saveReplyTo() {
        let trimmed = replyTo.trimmingCharacters(in: .whitespaces)
        guard trimmed != (account.replyTo ?? "") else { return }
        Task {
            // Empty string clears reply-to; replies go to the mailbox itself.
            await store.update(env.api, body: MailboxUpdateBody(replyTo: trimmed)) { $0.replyTo = trimmed }
            replyTo = store.account.replyTo ?? ""
        }
    }

    // MARK: Sending limits (editable)

    // min_wait_time is stored in seconds; edit it in whole minutes.
    private var minGapMinutes: Int {
        max(1, min(120, (account.minWaitTime ?? 600) / 60))
    }

    private var limitsSection: some View {
        Section {
            settingStepper(
                "Daily campaign cap", helper: "Max cold emails per day. Default 50, raise only with good reputation.",
                value: account.campaignLimit ?? 50, range: 1...100, suffix: "/day",
                write: { $0.campaignLimit = $1 }, mutate: { $0.campaignLimit = $1 }
            )
            settingStepper(
                "Minimum gap", helper: "Time between two sends from this mailbox.",
                value: minGapMinutes, range: 1...120, suffix: " min",
                write: { $0.minWaitTime = $1 * 60 }, mutate: { $0.minWaitTime = $1 * 60 }
            )
        } header: {
            EyebrowLabel("Sending limits")
        }
    }

    // MARK: Danger zone

    private var dangerSection: some View {
        Section {
            Button(role: .destructive) {
                confirmDisconnect = true
            } label: {
                HStack {
                    Spacer()
                    Text("Disconnect mailbox")
                    Spacer()
                }
            }
        } header: {
            EyebrowLabel("Danger zone")
        }
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

    // Name, reply-to, cap and gap moved into the editable sections above.
    private var identitySection: some View {
        Section("Details") {
            LabeledContent("Provider", value: account.providerLabel)
            if let synced = account.lastSyncedAt {
                LabeledContent("Last synced", value: WFormat.relative(synced))
            }
            if let created = account.createdAt {
                LabeledContent("Connected", value: WFormat.relative(created))
            }
        }
    }
}
