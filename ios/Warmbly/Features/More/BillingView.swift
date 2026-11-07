import SwiftUI

/// The plan you are on and what it includes, usage against what the server
/// enforces and the AI balance. Read only: the app sells nothing, so adding
/// money and automatic reload link out to the web.
struct BillingView: View {
    @Environment(AppEnvironment.self) private var env

    @State private var subscription: SubscriptionInfo?
    @State private var trial: TrialInfo?
    @State private var usage: OrgUsageSnapshot?
    @State private var credits: CreditBalance?
    @State private var creditUsage: CreditUsage?
    @State private var balanceInfo: BalanceInfo?
    @State private var loaded = false
    @State private var errorMessage: String?
    @State private var shown = false

    var body: some View {
        Group {
            if !loaded, subscription == nil, errorMessage == nil {
                SkeletonRows(rows: 5)
                    .padding(.top, 8)
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
        .onChange(of: env.realtime.pulse(for: .billing)) { _, _ in
            Task { await load() }
        }
    }

    // MARK: Content

    private var content: some View {
        SettingsPage(title: "Billing") {
            planCard
            if let notice {
                noticeView(notice)
            }
            usageSection
            if credits != nil || creditUsage != nil {
                creditsSection
            }
        }
        .refreshable { await load() }
        .onAppear { shown = true }
    }

    // MARK: Plan card

    private var planCard: some View {
        let style = PlanStyle(subscription?.plan?.name, trialing: isTrialing)
        return VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .center, spacing: 14) {
                    SettingsIcon(symbol: style.symbol, color: style.color, size: 48)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Your plan")
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkSecondary)
                        Text(planName)
                            .displayFont(26, weight: .semibold)
                            .foregroundStyle(WTheme.ink)
                            .lineLimit(1)
                            .minimumScaleFactor(0.75)
                    }
                    Spacer(minLength: 8)
                    if let status = statusChip {
                        chip(status.label, tone: status.tone)
                    }
                }
                Text(style.tagline)
                    .font(.system(size: 14.5))
                    .foregroundStyle(WTheme.inkSecondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.top, 14)
                HStack(alignment: .firstTextBaseline, spacing: 5) {
                    Text(priceAmount)
                        .font(.system(size: 34, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.ink)
                        .contentTransition(.numericText())
                    if let unit = priceUnit {
                        Text(unit)
                            .font(.system(size: 15))
                            .foregroundStyle(WTheme.inkSecondary)
                    }
                    if let savings = priceSavings {
                        Text(savings)
                            .font(.system(size: 12.5, weight: .semibold))
                            .foregroundStyle(WTheme.positive)
                            .padding(.leading, 4)
                    }
                }
                .padding(.top, 12)
            }
            .padding(18)
            Hairline()
            HStack(spacing: 0) {
                ForEach(Array(facts.enumerated()), id: \.offset) { index, fact in
                    if index > 0 {
                        Rectangle().fill(WTheme.hairline).frame(width: 1).padding(.vertical, 12)
                    }
                    factCell(fact.caption, fact.value)
                }
            }
            .fixedSize(horizontal: false, vertical: true)
        }
        .background(WTheme.groupedCard)
        .clipShape(RoundedRectangle(cornerRadius: 22, style: .continuous))
        .reveal(shown, 0)
        .animation(.snappy, value: usage != nil)
    }

    private func factCell(_ caption: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(caption)
                .font(.system(size: 12))
                .foregroundStyle(WTheme.inkSecondary)
                .lineLimit(1)
            Text(value)
                .font(.system(size: 14.5, weight: .medium))
                .monospacedDigit()
                .foregroundStyle(WTheme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .contentTransition(.opacity)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private func chip(_ label: String, tone: Tone) -> some View {
        HStack(spacing: 5) {
            Circle().fill(tone.color).frame(width: 6, height: 6)
            Text(label)
                .font(.system(size: 12.5, weight: .semibold))
                .foregroundStyle(tone.color)
        }
        .padding(.horizontal, 9)
        .frame(height: 24)
        .background(tone.background, in: Capsule())
    }

    // MARK: Notice

    private struct Notice {
        let tone: Tone
        let symbol: String
        let title: String
        let body: String
    }

    private var notice: Notice? {
        if subscription?.status == "past_due" {
            return Notice(
                tone: .rose, symbol: "exclamationmark.triangle.fill", title: "Payment failed",
                body: "The card on file couldn't be charged."
            )
        }
        if subscription?.cancelAtPeriodEnd == true, subscription?.status != "canceled" {
            let end = subscription?.currentPeriodEnd.map { $0.formatted(.dateTime.month(.wide).day()) }
            return Notice(
                tone: .amber, symbol: "calendar.badge.exclamationmark",
                title: end.map { "Ends on \($0)" } ?? "Ends with this period",
                body: "The plan stays fully active until then."
            )
        }
        return nil
    }

    private func noticeView(_ notice: Notice) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: notice.symbol)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(notice.tone.color)
                .frame(width: 20)
                .padding(.top, 1)
            VStack(alignment: .leading, spacing: 3) {
                Text(notice.title)
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(WTheme.ink)
                Text(notice.body)
                    .font(.system(size: 14))
                    .foregroundStyle(WTheme.inkSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(notice.tone.background, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }

    // MARK: Usage

    private struct Meter: Identifiable {
        let id: String
        let icon: String
        let color: Color
        let label: String
        let used: Double
        let limit: Double?
        var format: (Double) -> String = { WFormat.compact(Int($0)) }
    }

    private var meters: [Meter] {
        let counts = usage?.counts ?? env.session.currentOrg?.counts
        let limits = usage?.limits ?? env.session.currentOrg?.limits
        var rows: [Meter] = [
            Meter(
                id: "mailboxes", icon: "envelope.fill", color: SettingsPalette.mailboxes, label: "Mailboxes",
                used: Double(usage?.mailboxes?.used ?? counts?.emailAccounts ?? 0),
                limit: (usage?.mailboxes?.allowance ?? limits?.maxEmailAccounts).map(Double.init)
            ),
            Meter(
                id: "sent", icon: "paperplane.fill", color: SettingsPalette.analytics, label: "Sent today",
                used: Double(counts?.emailsSentToday ?? 0), limit: limits?.dailyCampaignLimit.map(Double.init)
            ),
            Meter(
                id: "campaigns", icon: "megaphone.fill", color: SettingsPalette.deals, label: "Active campaigns",
                used: Double(counts?.activeCampaigns ?? 0), limit: limits?.maxActiveCampaigns.map(Double.init)
            ),
            Meter(
                id: "contacts", icon: "person.crop.rectangle.stack.fill", color: SettingsPalette.tasks, label: "Contacts",
                used: Double(counts?.totalContacts ?? 0), limit: limits?.maxContacts.map(Double.init)
            ),
            Meter(
                id: "members", icon: "person.2.fill", color: SettingsPalette.members, label: "Members",
                used: Double(counts?.totalMembers ?? 0), limit: limits?.maxTeamMembers.map(Double.init)
            ),
        ]
        if let storage = usage?.storage, let limit = storage.limitBytes, limit > 0 {
            rows.append(Meter(
                id: "storage", icon: "externaldrive.fill", color: SettingsPalette.activity, label: "Storage",
                used: Double(storage.usedBytes ?? 0), limit: Double(limit),
                format: { Self.bytes(Int64($0)) }
            ))
        }
        return rows
    }

    private static func bytes(_ count: Int64) -> String {
        let formatter = ByteCountFormatter()
        formatter.countStyle = .file
        formatter.allowsNonnumericFormatting = false
        return formatter.string(fromByteCount: count)
    }

    private var usageSection: some View {
        SettingsSection("Usage", footer: "Live counts against the limits this workspace is held to.", dividerLeading: 58) {
            ForEach(Array(meters.enumerated()), id: \.element.id) { index, meter in
                meterRow(meter, index: index)
            }
        }
        .reveal(shown, 1)
    }

    private func meterRow(_ meter: Meter, index: Int) -> some View {
        let fraction = meter.limit.map { $0 > 0 ? min(1, meter.used / $0) : 0 }
        let tint = (fraction ?? 0) >= 1 ? WTheme.negative : (fraction ?? 0) >= 0.85 ? WTheme.warning : meter.color
        return HStack(alignment: .center, spacing: 12) {
            SettingsIcon(symbol: meter.icon, color: meter.color)
            VStack(alignment: .leading, spacing: 7) {
                HStack(alignment: .firstTextBaseline) {
                    Text(meter.label)
                        .font(.system(size: 16))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    HStack(alignment: .firstTextBaseline, spacing: 4) {
                        Text(meter.format(meter.used))
                            .font(.system(size: 15, weight: .medium))
                            .foregroundStyle(WTheme.ink)
                            .contentTransition(.numericText())
                        Text(meter.limit.map { "of \(meter.format($0))" } ?? "· Unlimited")
                            .font(.system(size: 13.5))
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                    .monospacedDigit()
                    .lineLimit(1)
                }
                if let fraction {
                    GeometryReader { proxy in
                        ZStack(alignment: .leading) {
                            Capsule().fill(WTheme.fill)
                            Capsule()
                                .fill(tint)
                                .frame(width: shown ? max(5, proxy.size.width * fraction) : 0)
                                .animation(.spring(response: 0.9, dampingFraction: 0.85).delay(0.15 + Double(index) * 0.06), value: shown)
                        }
                    }
                    .frame(height: 5)
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .accessibilityElement(children: .combine)
    }

    // MARK: AI balance

    /// Dollars from a backend with the balance, credits from one before it.
    private var inDollars: Bool {
        credits?.balanceUSD != nil || creditUsage?.spentMonthUSD != nil
    }

    private func money(_ value: Double) -> String {
        inDollars ? WFormat.usd(value) : Int(value.rounded()).formatted()
    }

    private func spent(_ usd: Double?, _ legacy: Int?) -> Double {
        inDollars ? usd ?? 0 : Double(legacy ?? 0)
    }

    private func amount(_ bucket: CreditUsage.Bucket) -> Double {
        spent(bucket.amountUSD, bucket.credits)
    }

    private var creditsSection: some View {
        let days = creditDays
        let peak = max(inDollars ? 0.01 : 1, days.map(\.amount).max() ?? 0)
        return VStack(alignment: .leading, spacing: 8) {
            EyebrowLabel(inDollars ? "AI balance" : "AI credits")
                .padding(.leading, 16)
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Balance")
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkSecondary)
                        Text(balanceText)
                            .font(.system(size: 28, weight: .semibold, design: .rounded))
                            .monospacedDigit()
                            .foregroundStyle(WTheme.ink)
                            .contentTransition(.numericText())
                    }
                    Spacer()
                    VStack(alignment: .trailing, spacing: 2) {
                        Text("This month")
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkSecondary)
                        Text("\(money(spent(creditUsage?.spentMonthUSD, creditUsage?.spentMonth))) spent")
                            .font(.system(size: 15, weight: .medium))
                            .monospacedDigit()
                            .foregroundStyle(WTheme.ink)
                        Text("\(money(spent(creditUsage?.spentWeekUSD, creditUsage?.spentWeek))) this week")
                            .font(.system(size: 13))
                            .monospacedDigit()
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                }
                HStack(alignment: .bottom, spacing: 4) {
                    ForEach(Array(days.enumerated()), id: \.offset) { index, day in
                        let isToday = index == days.count - 1
                        UnevenRoundedRectangle(topLeadingRadius: 3, topTrailingRadius: 3, style: .continuous)
                            .fill(day.amount > 0 ? (isToday ? SettingsPalette.analytics : SettingsPalette.analytics.opacity(0.55)) : WTheme.fill)
                            .frame(height: shown ? max(4, 64 * CGFloat(day.amount / peak)) : 4)
                            .frame(maxWidth: .infinity)
                            .animation(.spring(response: 0.8, dampingFraction: 0.8).delay(0.2 + Double(index) * 0.02), value: shown)
                    }
                }
                .frame(height: 64, alignment: .bottom)
                .padding(.top, 18)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(inDollars ? "AI spend over the last two weeks" : "Credits spent over the last two weeks")
                HStack {
                    Text(days.first.map { $0.date.formatted(.dateTime.month(.abbreviated).day()) } ?? "")
                    Spacer()
                    Text("Today")
                }
                .font(.system(size: 11.5))
                .foregroundStyle(WTheme.inkTertiary)
                .padding(.top, 6)
            }
            .padding(16)
            .background(WTheme.groupedCard)
            .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))

            if !topUses.isEmpty {
                MoreCard(dividerLeading: 16) {
                    ForEach(topUses, id: \.key) { bucket in
                        useRow(bucket)
                    }
                }
                .padding(.top, 4)
            }
            if let reload = balanceInfo?.autoReload, balanceInfo?.unlimited != true {
                autoReloadCard(reload)
                    .padding(.top, 4)
            }
            if let footer = creditsFooter {
                Text(footer)
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.inkSecondary)
                    .padding(.horizontal, 16)
                    .padding(.top, 2)
            }
        }
        .reveal(shown, 2)
    }

    /// Automatic reload, read only: it charges a card, which happens on the web.
    private func autoReloadCard(_ reload: BalanceInfo.AutoReload) -> some View {
        let on = reload.enabled == true
        let detail: String? = if on, let added = reload.amountUSD, let threshold = reload.thresholdUSD {
            "Adds \(WFormat.usd(added)) when the balance falls below \(WFormat.usd(threshold))"
        } else if on {
            nil
        } else {
            "Add money or turn it on from the web dashboard"
        }
        return MoreCard {
            SettingsRow(
                icon: "arrow.clockwise.circle.fill", color: SettingsPalette.billing,
                title: "Automatic reload", subtitle: detail,
                value: on ? "On" : "Off", accessory: .none
            )
            if let url = AppConfig.dashboardURL("/app/settings/billing/balance") {
                Link(destination: url) {
                    SettingsRow(
                        icon: "safari.fill", color: WTheme.accent,
                        title: "Manage on the web", accessory: .external
                    )
                }
                .buttonStyle(MoreRowPressStyle())
            }
        }
    }

    private func useRow(_ bucket: CreditUsage.Bucket) -> some View {
        let total = topUses.map { amount($0) }.reduce(0, +)
        let share = total > 0 ? amount(bucket) / total : 0
        return VStack(alignment: .leading, spacing: 7) {
            HStack(alignment: .firstTextBaseline) {
                Text(Self.reasonLabel(bucket.key))
                    .font(.system(size: 15))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                Spacer(minLength: 8)
                Text(money(amount(bucket)))
                    .font(.system(size: 15, weight: .medium))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.ink)
                Text("\(Int((share * 100).rounded()))%")
                    .font(.system(size: 13))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkTertiary)
                    .frame(width: 38, alignment: .trailing)
            }
            GeometryReader { proxy in
                ZStack(alignment: .leading) {
                    Capsule().fill(WTheme.fill)
                    Capsule()
                        .fill(SettingsPalette.analytics.opacity(0.7))
                        .frame(width: shown ? max(5, proxy.size.width * share) : 0)
                        .animation(.spring(response: 0.9, dampingFraction: 0.85).delay(0.3), value: shown)
                }
            }
            .frame(height: 4)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .accessibilityElement(children: .combine)
    }

    private static let reasonLabels: [String: String] = [
        "writing_assistant": "Writing assistant",
        "writing_edit": "Selection edits",
        "reply_draft": "Reply drafts",
        "agent_iteration": "Remie",
        "inbox_agent_draft": "Inbox agent",
        "research_run": "Contact research",
        "automation_ai": "Automation AI",
        "campaign_ai": "Campaign switches",
        "spam_analysis": "Spam analysis",
        "placement_test": "Placement tests",
    ]

    private static func reasonLabel(_ key: String) -> String {
        if let label = reasonLabels[key] { return label }
        let words = key.replacingOccurrences(of: "_", with: " ")
        return words.prefix(1).uppercased() + words.dropFirst()
    }

    private var topUses: [CreditUsage.Bucket] {
        Array((creditUsage?.byReason ?? []).filter { amount($0) > 0 }.sorted { amount($0) > amount($1) }.prefix(4))
    }

    /// The last fourteen days, oldest first, with the empty days filled in.
    private var creditDays: [(date: Date, amount: Double)] {
        let calendar = Calendar.current
        let parser = DateFormatter()
        parser.calendar = Calendar(identifier: .gregorian)
        parser.locale = Locale(identifier: "en_US_POSIX")
        parser.dateFormat = "yyyy-MM-dd"
        var byDay: [Date: Double] = [:]
        for point in creditUsage?.series ?? [] {
            if let date = parser.date(from: point.date) {
                byDay[calendar.startOfDay(for: date), default: 0] += spent(point.amountUSD, point.credits)
            }
        }
        let today = calendar.startOfDay(for: .now)
        return (0..<14).reversed().compactMap { offset in
            calendar.date(byAdding: .day, value: -offset, to: today).map { ($0, byDay[$0] ?? 0) }
        }
    }

    private var balanceText: String {
        if credits?.unlimited == true { return "Unlimited" }
        return money(spent(credits?.balanceUSD, credits?.balance))
    }

    private var creditsFooter: String? {
        guard credits?.unlimited != true else {
            return inDollars ? "AI drafting, research and analysis are not metered here." : "Credits pay for AI drafting, research and analysis."
        }
        var parts: [String] = []
        let allowance = spent(credits?.monthlyAllowanceUSD, credits?.monthlyAllowance)
        if allowance > 0 {
            var line = "\(money(allowance)) included each month"
            if let reset = credits?.nextResetAt {
                line += ", next on \(reset.formatted(.dateTime.month(.abbreviated).day()))"
            }
            parts.append(line)
        }
        let purchased = spent(credits?.purchasedUSD, credits?.purchasedBalance)
        if purchased > 0 {
            parts.append(inDollars ? "\(money(purchased)) added that never expires" : "\(Int(purchased).formatted()) bought credits that never expire")
        }
        return parts.isEmpty ? nil : parts.joined(separator: ". ") + "."
    }

    // MARK: Derived

    private var planName: String {
        let raw = subscription?.plan?.name ?? "Free"
        guard let first = raw.first else { return "Free" }
        return first.uppercased() + raw.dropFirst().lowercased()
    }

    private var isTrialing: Bool {
        subscription?.status == "trialing" || trial?.isInTrial == true
    }

    private var statusChip: (label: String, tone: Tone)? {
        if isTrialing { return ("Trial", .sky) }
        switch subscription?.status {
        case "past_due": return ("Payment due", .rose)
        case "canceled": return ("Canceled", .slate)
        case "active":
            return subscription?.cancelAtPeriodEnd == true ? ("Ending", .amber) : ("Active", .emerald)
        default: return nil
        }
    }

    private var priceAmount: String {
        guard let plan = subscription?.plan, let price = plan.price, price > 0 else { return "Free" }
        let charged = plan.discountedPrice.flatMap { $0 > 0 && $0 < price ? $0 : nil } ?? price
        return "$" + Int(charged.rounded()).formatted()
    }

    private var priceUnit: String? {
        guard (subscription?.plan?.price ?? 0) > 0 else { return nil }
        return subscription?.plan?.duration == "year" ? "/ year" : "/ month"
    }

    private var priceSavings: String? {
        guard let savings = subscription?.plan?.savings, savings > 0 else { return nil }
        return "Save \(Int(savings))%"
    }

    private var facts: [(caption: String, value: String)] {
        let date: (Date) -> String = { $0.formatted(.dateTime.month(.abbreviated).day()) }
        var first: (String, String)
        if isTrialing, let ends = trial?.trialEndsAt ?? subscription?.trialEnd ?? subscription?.freeTrialEndsAt {
            first = ("Trial ends", date(ends))
        } else if let end = subscription?.currentPeriodEnd {
            first = (subscription?.cancelAtPeriodEnd == true ? "Active until" : "Renews", date(end))
        } else {
            first = ("Mailboxes", usage?.mailboxes?.allowance.map { "Up to \($0)" } ?? "Unlimited")
        }
        let sends: String = if let limit = usage?.limits?.dailyCampaignLimit {
            limit.formatted()
        } else {
            usage == nil ? "–" : "Unlimited"
        }
        let allowanceUSD = subscription?.plan?.monthlyAllowanceUSD ?? credits?.monthlyAllowanceUSD
        let aiCredits: String = if credits?.unlimited == true {
            "Unlimited"
        } else if let allowanceUSD, allowanceUSD > 0 {
            "\(WFormat.usd(allowanceUSD).replacingOccurrences(of: ".00", with: "")) a month"
        } else if !inDollars, let monthly = subscription?.plan?.monthlyCredits, monthly > 0 {
            "\(WFormat.compact(monthly)) a month"
        } else {
            "Pay as you go"
        }
        return [first, ("Daily sends", sends), (inDollars || allowanceUSD != nil ? "AI included" : "AI credits", aiCredits)]
    }

    // MARK: Load

    private func load() async {
        errorMessage = nil
        do {
            async let sub: SubscriptionInfo = env.api.get("subscription")
            async let tr: TrialInfo = env.api.get("subscription/trial")
            async let lim: OrgUsageSnapshot = env.api.get("organization/current/limits")
            async let bal: CreditBalance = env.api.get("subscription/credits")
            async let use: CreditUsage = env.api.get("subscription/credits/usage")
            async let rel: BalanceInfo = env.api.get("subscription/balance")
            let subscription = try await sub
            let fresh = (try? await tr, try? await lim, try? await bal, try? await use, try? await rel)
            withAnimation(.snappy) {
                self.subscription = subscription
                trial = fresh.0
                usage = fresh.1 ?? usage
                credits = fresh.2
                creditUsage = fresh.3
                balanceInfo = fresh.4
            }
        } catch {
            errorMessage = (error as? APIError)?.errorDescription ?? error.localizedDescription
        }
        loaded = true
    }
}

/// How a plan looks: its glyph, colour and one-line pitch.
private struct PlanStyle {
    let symbol: String
    let color: Color
    let tagline: String

    init(_ name: String?, trialing: Bool) {
        let key = (name ?? "free").lowercased()
        if trialing || key.contains("trial") {
            symbol = "sparkles"; color = WTheme.accent
            tagline = "Every feature unlocked while you try Warmbly."
        } else if key.hasPrefix("starter") {
            symbol = "paperplane.fill"; color = WTheme.positive
            tagline = "Great for small businesses with a small budget."
        } else if key.hasPrefix("grow") {
            symbol = "chart.line.uptrend.xyaxis"; color = Tone.orange.color
            tagline = "Ideal for growing businesses scaling their outreach."
        } else if key.hasPrefix("business") {
            symbol = "building.2.fill"; color = WTheme.accent
            tagline = "For established teams that need higher limits and advanced features."
        } else if key.hasPrefix("enterprise") {
            symbol = "crown.fill"; color = WTheme.ink
            tagline = "Custom volume and dedicated support for large organizations."
        } else if key.contains("warmup") || key.contains("pool") {
            symbol = "flame.fill"; color = Tone.orange.color
            tagline = "The premium warmup pool and no mailbox cap."
        } else {
            symbol = "leaf.fill"; color = WTheme.paused
            tagline = "Warm up to 10 mailboxes in the Warmbly pool."
        }
    }
}
