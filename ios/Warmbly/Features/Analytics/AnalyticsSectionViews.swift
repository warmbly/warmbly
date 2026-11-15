import SwiftUI

// MARK: - Tone maps

enum AnalyticsToneMap {
    /// Deliverability band -> chip tone.
    static func band(_ raw: String?) -> Tone {
        switch raw {
        case "healthy": .emerald
        case "warning": .amber
        case "quarantine": .orange
        case "blocked": .rose
        default: .slate
        }
    }

    static func bandLabel(_ raw: String?) -> String {
        switch raw {
        case "healthy": "Healthy"
        case "warning": "Watch"
        case "quarantine": "At risk"
        case "blocked": "Blocked"
        default: "No data yet"
        }
    }

    /// Account health status -> chip tone.
    static func health(_ raw: String?) -> Tone {
        switch raw {
        case "healthy": .emerald
        case "warning": .amber
        case "error": .rose
        default: .slate
        }
    }

    /// Warmup pool state -> chip tone.
    static func warmupState(_ raw: String?) -> Tone {
        switch raw {
        case "healthy": .emerald
        case "watch": .amber
        case "throttled": .amber
        case "quarantined": .orange
        case "blocked": .rose
        default: .slate
        }
    }
}

/// Compact labeled number used inside stat grids.
struct AnalyticsMiniStat: View {
    let label: String
    let value: String
    var tone: Tone? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(label)
                .font(.system(size: 13))
                .foregroundStyle(WTheme.inkSecondary)
                .lineLimit(1)
            Text(value)
                .font(.system(size: 20, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(tone?.color ?? WTheme.ink)
                .contentTransition(.numericText())
                .lineLimit(1)
                .minimumScaleFactor(0.75)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Inline section failure; the rest of the screen keeps working.
struct AnalyticsSectionError: View {
    let text: String

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 13))
                .foregroundStyle(WTheme.negative)
            Text(text)
                .font(.system(size: 14))
                .foregroundStyle(WTheme.inkSecondary)
        }
        .padding(.vertical, 6)
    }
}

// MARK: - Deliverability

/// The workspace's sending reputation: a score, the three rates that move
/// it against their safe ranges, how replies read, and who is suppressed.
struct AnalyticsDeliverabilityPage: View {
    let store: AnalyticsStore
    var shown: Bool

    var body: some View {
        if let summary = store.deliverability {
            VStack(alignment: .leading, spacing: 16) {
                scoreCard(summary).reveal(shown, 0)
                rangesCard(summary).reveal(shown, 1)
                if intentTotal(summary) > 0 {
                    intentCard(summary).reveal(shown, 2)
                }
                suppressionCard(summary).reveal(shown, 3)
                if let mailboxes = summary.byMailbox, !mailboxes.isEmpty {
                    mailboxCard(mailboxes).reveal(shown, 4)
                }
            }
        } else if let error = store.deliverabilityError {
            AnalyticsSectionError(text: error).coverCard()
        } else {
            AnalyticsLoadingCards()
        }
    }

    // MARK: Score

    private func scoreCard(_ summary: DeliverabilitySummary) -> some View {
        let tone = AnalyticsToneMap.band(summary.band)
        let score = summary.score ?? (summary.band == "healthy" ? 100 : 0)
        return HStack(spacing: 18) {
            SweepRing(fraction: Double(score) / 100, color: tone.color, shown: shown, lineWidth: 11) {
                VStack(spacing: -2) {
                    CountUpText(value: shown ? Double(score) : 0, format: { "\(Int($0.rounded()))" })
                        .font(.system(size: 34, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.ink)
                        .animation(.easeOut(duration: 1.1).delay(0.15), value: shown)
                    Text("of 100")
                        .font(.system(size: 12, weight: .medium))
                        .foregroundStyle(WTheme.inkTertiary)
                }
            }
            .frame(width: 116, height: 116)
            VStack(alignment: .leading, spacing: 6) {
                StatusPill(text: AnalyticsToneMap.bandLabel(summary.band), tone: tone)
                Text(headline(summary.band))
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(WTheme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                Text((summary.emailsSent ?? 0) > 0 ? "Read from \(WFormat.compact(summary.emailsSent ?? 0)) emails sent" : "Nothing sent in this period to judge")
                    .font(.system(size: 13.5))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkSecondary)
            }
            Spacer(minLength: 0)
        }
        .coverCard()
        .accessibilityElement(children: .combine)
    }

    private func headline(_ band: String?) -> String {
        switch band {
        case "healthy": "Your mail is landing well"
        case "warning": "Worth keeping an eye on"
        case "quarantine": "Sending is at risk"
        case "blocked": "Sending is paused"
        default: "Send a few emails to see this"
        }
    }

    // MARK: Ranges

    private func rangesCard(_ summary: DeliverabilitySummary) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            CardTitle("Inside the safe range", caption: "The rates mailbox providers judge you on")
            RateGauge(
                label: "Bounce rate", value: summary.bounceRate ?? 0,
                watch: 2, risk: 5, note: "Keep under 2%", shown: shown, delay: 0.2
            )
            RateGauge(
                label: "Complaint rate", value: summary.complaintRate ?? 0,
                watch: 0.03, risk: 0.10, note: "Keep under 0.03%", shown: shown, delay: 0.3
            )
            if let spam = summary.spamPlacementRate {
                RateGauge(
                    label: "Landed in spam", value: spam,
                    watch: 10, risk: 20, note: "Keep under 10%", shown: shown, delay: 0.4
                )
            } else {
                HStack(spacing: 8) {
                    Image(systemName: "tray.full")
                        .font(.system(size: 13))
                        .foregroundStyle(WTheme.inkTertiary)
                    Text("Spam placement shows once warmup has enough deliveries to read.")
                        .font(.system(size: 13.5))
                        .foregroundStyle(WTheme.inkSecondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .coverCard()
    }

    // MARK: Intent

    private func intentTotal(_ summary: DeliverabilitySummary) -> Int {
        intents(summary).reduce(0) { $0 + $1.count }
    }

    private func intents(_ summary: DeliverabilitySummary) -> [(label: String, count: Int, color: Color)] {
        [
            ("Interested", summary.intentPositive ?? 0, WTheme.positive),
            ("Question", summary.intentQuestion ?? 0, WTheme.accent),
            ("Neutral", summary.intentNeutral ?? 0, WTheme.special),
            ("Out of office", summary.intentOutOfOffice ?? 0, WTheme.paused),
            ("Not interested", summary.intentNegative ?? 0, WTheme.negative),
            ("Automated", summary.intentAutomated ?? 0, WTheme.hairlineStrong),
        ]
    }

    private func intentCard(_ summary: DeliverabilitySummary) -> some View {
        let rows = intents(summary).filter { $0.count > 0 }
        let total = max(1, rows.reduce(0) { $0 + $1.count })
        return VStack(alignment: .leading, spacing: 16) {
            CardTitle("How people replied", caption: "\(total) \(total == 1 ? "reply" : "replies"), read for intent")
            GeometryReader { proxy in
                HStack(spacing: 3) {
                    ForEach(Array(rows.enumerated()), id: \.offset) { index, row in
                        let width = max(6, (proxy.size.width - CGFloat(rows.count - 1) * 3) * CGFloat(row.count) / CGFloat(total))
                        RoundedRectangle(cornerRadius: 4, style: .continuous)
                            .fill(row.color)
                            .frame(width: shown ? width : 0)
                            .animation(.spring(response: 0.8, dampingFraction: 0.82).delay(0.2 + Double(index) * 0.07), value: shown)
                    }
                    Spacer(minLength: 0)
                }
            }
            .frame(height: 14)
            LazyVGrid(columns: [GridItem(.flexible(), spacing: 12), GridItem(.flexible(), spacing: 12)], alignment: .leading, spacing: 10) {
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    HStack(spacing: 7) {
                        RoundedRectangle(cornerRadius: 2.5).fill(row.color).frame(width: 9, height: 9)
                        Text(row.label)
                            .font(.system(size: 14))
                            .foregroundStyle(WTheme.inkSecondary)
                            .lineLimit(1)
                        Spacer(minLength: 4)
                        Text("\(row.count)")
                            .font(.system(size: 14, weight: .semibold))
                            .monospacedDigit()
                            .foregroundStyle(WTheme.ink)
                    }
                }
            }
        }
        .coverCard()
    }

    // MARK: Suppression

    private func suppressionCard(_ summary: DeliverabilitySummary) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            CardTitle("Kept off your lists", caption: "Addresses Warmbly will not email again")
            HStack(spacing: 0) {
                AnalyticsMiniStat(label: "Suppressed", value: AnalyticsFmt.count(summary.suppressedRecipients))
                AnalyticsMiniStat(label: "Unsubscribed", value: AnalyticsFmt.count(summary.unsubscribeCount))
                AnalyticsMiniStat(
                    label: "Complaints",
                    value: AnalyticsFmt.count(summary.complaintCount),
                    tone: (summary.complaintCount ?? 0) > 0 ? .rose : nil
                )
            }
            if let pending = summary.dlqPending, pending > 0 {
                Label("\(pending) events waiting to retry", systemImage: "clock.arrow.circlepath")
                    .font(.system(size: 13.5))
                    .foregroundStyle(WTheme.warning)
            }
        }
        .coverCard()
    }

    // MARK: Mailboxes

    private func mailboxCard(_ rows: [AnalyticsDeliverabilityMailbox]) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            CardTitle("By mailbox").padding(.bottom, 8)
            ForEach(Array(rows.prefix(8).enumerated()), id: \.element.id) { index, row in
                if index > 0 { Hairline().padding(.leading, 50) }
                HStack(spacing: 12) {
                    store.mailboxAvatar(id: row.id, email: row.email, size: 36)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(row.email ?? "Mailbox")
                            .font(.system(size: 15, weight: .medium))
                            .foregroundStyle(WTheme.ink)
                            .lineLimit(1)
                        Text("\(WFormat.compact(row.sent ?? 0)) sent  ·  \(AnalyticsFmt.rate(row.bounceRate)) bounced")
                            .font(.system(size: 13))
                            .monospacedDigit()
                            .foregroundStyle(WTheme.inkSecondary)
                    }
                    Spacer()
                    StatusPill(text: AnalyticsToneMap.bandLabel(row.band), tone: AnalyticsToneMap.band(row.band))
                }
                .padding(.vertical, 9)
            }
        }
        .coverCard()
    }
}

/// A rate on a track split into fine, watch and risk, with a knob that slides to it.
struct RateGauge: View {
    let label: String
    let value: Double
    let watch: Double
    let risk: Double
    let note: String
    var shown: Bool
    var delay: Double = 0

    private var tone: Tone { value >= risk ? .rose : value >= watch ? .amber : .emerald }
    private var scale: Double { risk * 1.6 }

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack(alignment: .firstTextBaseline) {
                Text(label)
                    .font(.system(size: 15))
                    .foregroundStyle(WTheme.ink)
                Spacer()
                Text(AnalyticsFmt.rate(value))
                    .font(.system(size: 17, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(tone == .emerald ? WTheme.ink : tone.color)
            }
            GeometryReader { proxy in
                let width = proxy.size.width
                let watchX = width * watch / scale
                let riskX = width * risk / scale
                let knob = width * min(1, value / scale)
                ZStack(alignment: .leading) {
                    HStack(spacing: 2) {
                        Capsule().fill(WTheme.positive.opacity(0.28)).frame(width: max(0, watchX - 1))
                        Rectangle().fill(WTheme.warning.opacity(0.3)).frame(width: max(0, riskX - watchX - 2))
                        Capsule().fill(WTheme.negative.opacity(0.26))
                    }
                    .frame(height: 6)
                    Circle()
                        .fill(GroupedSurface.card)
                        .frame(width: 16, height: 16)
                        .overlay(Circle().stroke(tone.color, lineWidth: 4))
                        .shadow(color: tone.color.opacity(0.35), radius: 4, y: 1)
                        .offset(x: (shown ? knob : 0) - 8)
                        .animation(.spring(response: 1, dampingFraction: 0.7).delay(delay), value: shown)
                }
                .frame(height: 16)
            }
            .frame(height: 16)
            Text(note)
                .font(.system(size: 12.5))
                .foregroundStyle(WTheme.inkTertiary)
        }
        .accessibilityElement(children: .combine)
    }
}

// MARK: - Warmup

/// Warmup across every mailbox: what went out, how the days tracked the
/// ramp, where it landed, and each mailbox's climb.
struct AnalyticsWarmupPage: View {
    let store: AnalyticsStore
    var shown: Bool

    @State private var selectedDay: Int?

    var body: some View {
        if let summary = store.warmup?.summary {
            VStack(alignment: .leading, spacing: 16) {
                heroCard(summary).reveal(shown, 0)
                if (summary.totalSent ?? 0) > 0 {
                    dailyCard.reveal(shown, 1)
                }
                if !placements.isEmpty {
                    placementCard.reveal(shown, 2)
                }
                if !warming.isEmpty {
                    rampCard.reveal(shown, 3)
                }
            }
        } else if let error = store.warmupError {
            AnalyticsSectionError(text: error).coverCard()
        } else {
            AnalyticsLoadingCards()
        }
    }

    // MARK: Hero

    private func heroCard(_ summary: AnalyticsWarmupSummary) -> some View {
        let progress = (summary.targetProgress ?? 0) / 100
        return VStack(alignment: .leading, spacing: 18) {
            HStack(alignment: .center, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 6) {
                        Image(systemName: "flame.fill")
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundStyle(Tone.orange.color)
                            .symbolEffect(.bounce, value: shown)
                        Text("Warmup emails sent")
                            .font(.system(size: 14, weight: .medium))
                            .foregroundStyle(WTheme.inkSecondary)
                    }
                    CountUpText(value: shown ? Double(summary.totalSent ?? 0) : 0, format: { Int($0.rounded()).formatted() })
                        .font(.system(size: 40, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.ink)
                        .animation(.easeOut(duration: 1.2), value: shown)
                    Text("over \(summary.daysActive ?? 0) active \((summary.daysActive ?? 0) == 1 ? "day" : "days")")
                        .font(.system(size: 13.5))
                        .foregroundStyle(WTheme.inkTertiary)
                }
                Spacer(minLength: 8)
                SweepRing(fraction: progress, color: Tone.orange.color, shown: shown, lineWidth: 9) {
                    VStack(spacing: -1) {
                        Text("\(Int((progress * 100).rounded()))%")
                            .font(.system(size: 20, weight: .semibold, design: .rounded))
                            .monospacedDigit()
                            .foregroundStyle(WTheme.ink)
                        Text("of target")
                            .font(.system(size: 11, weight: .medium))
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                }
                .frame(width: 96, height: 96)
            }
            Hairline()
            HStack(spacing: 0) {
                AnalyticsMiniStat(label: "Replies", value: AnalyticsFmt.count(summary.totalReplied))
                AnalyticsMiniStat(label: "Reply rate", value: AnalyticsFmt.rate(summary.replyRate))
                AnalyticsMiniStat(label: "A day", value: String(format: "%.0f", summary.averageDaily ?? 0))
            }
        }
        .coverCard()
    }

    // MARK: Daily bars

    private struct Day {
        let date: Date
        let sent: Int
        let target: Int
        let replied: Int
    }

    private var days: [Day] {
        (store.warmup?.dailyStats ?? []).compactMap { day in
            guard let date = AnalyticsDay.parse(day.date.map { String($0.prefix(10)) }) else { return nil }
            return Day(date: date, sent: day.emailsSent ?? 0, target: day.targetVolume ?? 0, replied: day.emailsReplied ?? 0)
        }
    }

    private var dailyCard: some View {
        let days = days
        let peak = max(1, days.map { max($0.sent, $0.target) }.max() ?? 1)
        let picked = selectedDay.flatMap { days.indices.contains($0) ? days[$0] : nil }
        let utcDay: Date.FormatStyle = {
            var style = Date.FormatStyle.dateTime.weekday(.abbreviated).month(.abbreviated).day()
            style.timeZone = TimeZone(identifier: "UTC")!
            return style
        }()
        return VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 3) {
                Text(picked.map { utcDay.format($0.date) } ?? "Each day against its target")
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(WTheme.ink)
                    .contentTransition(.opacity)
                Group {
                    if let picked {
                        Text("\(picked.sent) sent of \(picked.target)  ·  \(picked.replied) replied")
                    } else {
                        Text("Drag across the bars to read a day")
                    }
                }
                .font(.system(size: 13.5))
                .monospacedDigit()
                .foregroundStyle(WTheme.inkSecondary)
                .contentTransition(.numericText())
            }
            .animation(.snappy(duration: 0.2), value: selectedDay)
            GeometryReader { proxy in
                let count = max(1, days.count)
                let gap: CGFloat = count > 40 ? 1.5 : 4
                let barWidth = max(2, (proxy.size.width - gap * CGFloat(count - 1)) / CGFloat(count))
                HStack(alignment: .bottom, spacing: gap) {
                    ForEach(Array(days.enumerated()), id: \.offset) { index, day in
                        let height = proxy.size.height * CGFloat(day.sent) / CGFloat(peak)
                        let targetY = proxy.size.height * CGFloat(day.target) / CGFloat(peak)
                        let dim = selectedDay != nil && selectedDay != index
                        ZStack(alignment: .bottom) {
                            RoundedRectangle(cornerRadius: min(4, barWidth / 2), style: .continuous)
                                .fill(WTheme.fill)
                                .frame(height: max(2, targetY))
                            RoundedRectangle(cornerRadius: min(4, barWidth / 2), style: .continuous)
                                .fill(LinearGradient(colors: [Color(hex: 0xFDBA74), Tone.orange.color], startPoint: .top, endPoint: .bottom))
                                .frame(height: shown ? max(2, height) : 2)
                                .opacity(dim ? 0.35 : 1)
                                .animation(.spring(response: 0.8, dampingFraction: 0.78).delay(0.15 + Double(index) * min(0.04, 1.2 / Double(count))), value: shown)
                        }
                        .frame(width: barWidth)
                    }
                }
                .frame(maxHeight: .infinity, alignment: .bottom)
                .contentShape(Rectangle())
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { value in
                            let index = Int(value.location.x / (barWidth + gap))
                            selectedDay = min(max(0, index), count - 1)
                        }
                        .onEnded { _ in
                            withAnimation(.snappy) { selectedDay = nil }
                        }
                )
            }
            .frame(height: 130)
            .sensoryFeedback(.selection, trigger: selectedDay)
            HStack(spacing: 14) {
                legend(color: Tone.orange.color, text: "Sent")
                legend(color: WTheme.hairlineStrong, text: "Target")
                Spacer()
            }
        }
        .coverCard()
    }

    private func legend(color: Color, text: String) -> some View {
        HStack(spacing: 6) {
            RoundedRectangle(cornerRadius: 2.5).fill(color).frame(width: 9, height: 9)
            Text(text)
                .font(.system(size: 12.5))
                .foregroundStyle(WTheme.inkSecondary)
        }
    }

    // MARK: Placement

    private var placements: [AccountHealthRow] {
        store.accounts.filter { ($0.warmupPlacement?.delivered ?? 0) > 0 }
    }

    private var placementCard: some View {
        VStack(alignment: .leading, spacing: 4) {
            CardTitle("Where warmup landed", caption: "Verified deliveries at Google, Microsoft and Yahoo, last 7 days")
                .padding(.bottom, 10)
            ForEach(Array(placements.enumerated()), id: \.element.id) { index, account in
                if index > 0 { Hairline() }
                placementRow(account, index: index)
            }
            HStack(spacing: 14) {
                legend(color: WTheme.positive, text: "Inbox")
                legend(color: WTheme.accent, text: "Tabs")
                legend(color: WTheme.negative, text: "Spam")
                Spacer()
            }
            .padding(.top, 10)
        }
        .coverCard()
    }

    private func placementRow(_ account: AccountHealthRow, index: Int) -> some View {
        let placement = account.warmupPlacement
        let delivered = max(1, placement?.delivered ?? 1)
        let parts: [(Int, Color)] = [
            (placement?.inbox ?? 0, WTheme.positive),
            (placement?.tabs ?? 0, WTheme.accent),
            (placement?.spam ?? 0, WTheme.negative),
        ]
        let rate = placement?.inboxRate ?? 0
        return VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(account.email ?? "Mailbox")
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                Spacer(minLength: 8)
                Text(String(format: "%.0f%%", rate))
                    .font(.system(size: 15, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(rate >= 90 ? WTheme.positive : rate >= 80 ? WTheme.warning : WTheme.negative)
                Text("inbox")
                    .font(.system(size: 12.5))
                    .foregroundStyle(WTheme.inkTertiary)
            }
            GeometryReader { proxy in
                HStack(spacing: 2) {
                    ForEach(Array(parts.enumerated()), id: \.offset) { part, item in
                        if item.0 > 0 {
                            Capsule()
                                .fill(item.1)
                                .frame(width: shown ? max(4, (proxy.size.width - 4) * CGFloat(item.0) / CGFloat(delivered)) : 0)
                                .animation(.spring(response: 0.9, dampingFraction: 0.8).delay(0.2 + Double(index) * 0.08 + Double(part) * 0.1), value: shown)
                        }
                    }
                    Spacer(minLength: 0)
                }
            }
            .frame(height: 8)
            Text("\(placement?.delivered ?? 0) delivered  ·  \(placement?.spam ?? 0) in spam")
                .font(.system(size: 12.5))
                .monospacedDigit()
                .foregroundStyle(WTheme.inkTertiary)
        }
        .padding(.vertical, 10)
        .accessibilityElement(children: .combine)
    }

    // MARK: Ramp

    private var warming: [AccountHealthRow] {
        store.accounts.filter { $0.warmupStatus?.enabled == true }
    }

    private var rampCard: some View {
        VStack(alignment: .leading, spacing: 4) {
            CardTitle("Each mailbox's ramp", caption: "Today's daily target on its way to the ceiling")
                .padding(.bottom, 8)
            ForEach(Array(warming.enumerated()), id: \.element.id) { index, account in
                if index > 0 { Hairline().padding(.leading, 48) }
                rampRow(account, index: index)
            }
        }
        .coverCard()
    }

    private func rampRow(_ account: AccountHealthRow, index: Int) -> some View {
        let status = account.warmupStatus
        let ceiling = max(1, status?.maxVolume ?? 40)
        let today = min(ceiling, status?.targetVolume ?? 0)
        let paused = status?.paused == true
        return HStack(alignment: .top, spacing: 12) {
            MailboxAvatar(email: account.email ?? "", avatarURL: account.avatarURL, brand: account.resolvedBrand, seed: account.id, size: 36)
            VStack(alignment: .leading, spacing: 8) {
                HStack(alignment: .firstTextBaseline) {
                    Text(account.email ?? "Mailbox")
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    Text(paused ? "Paused" : "Day \(status?.daysActive ?? 0)")
                        .font(.system(size: 13, weight: .semibold))
                        .monospacedDigit()
                        .foregroundStyle(paused ? WTheme.inkTertiary : Tone.orange.color)
                }
                WarmupBar(progress: shown ? Double(today) / Double(ceiling) : 0, paused: paused, delay: Double(index) * 0.08)
                    .frame(height: 8)
                Text("\(Text("\(today)").foregroundStyle(WTheme.ink).fontWeight(.semibold)) of \(ceiling) a day  ·  \(status?.currentVolume ?? 0) sent today")
                    .font(.system(size: 12.5))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkSecondary)
            }
        }
        .padding(.vertical, 10)
    }
}

// MARK: - Mailboxes

/// Every mailbox's health: a summary of the fleet, then each mailbox with
/// today's sending and its score, opening its detail.
struct AnalyticsAccountsPage: View {
    let store: AnalyticsStore
    var shown: Bool

    #if DEBUG
    @State private var debugOpen = false
    #endif

    var body: some View {
        if store.accounts.isEmpty, let error = store.accountsError {
            AnalyticsSectionError(text: error).coverCard()
        } else if store.accounts.isEmpty, store.isLoading {
            AnalyticsLoadingCards()
        } else if store.accounts.isEmpty {
            CoverEmptyState(
                symbol: "envelope.fill",
                title: "No mailboxes yet",
                message: "Connect a mailbox and its health shows up here."
            )
        } else {
            VStack(alignment: .leading, spacing: 16) {
                summaryCard.reveal(shown, 0)
                listCard.reveal(shown, 1)
            }
            #if DEBUG
            .navigationDestination(isPresented: $debugOpen) {
                if let first = store.accounts.last { AnalyticsAccountDetailView(account: first) }
            }
            .task { debugOpen = ProcessInfo.processInfo.environment["WARMBLY_DEBUG_ANALYTICS_ACCOUNT"] != nil }
            #endif
        }
    }

    private var counts: (healthy: Int, warning: Int, error: Int) {
        let accounts = store.accounts
        return (
            accounts.count { $0.health?.status == "healthy" },
            accounts.count { $0.health?.status == "warning" },
            accounts.count { $0.health?.status == "error" }
        )
    }

    private var averageScore: Int {
        let scores = store.accounts.compactMap { $0.health?.score }
        guard !scores.isEmpty else { return 0 }
        return scores.reduce(0, +) / scores.count
    }

    private var summaryCard: some View {
        let counts = counts
        let total = max(1, store.accounts.count)
        let segments: [(String, Int, Color)] = [
            ("Healthy", counts.healthy, WTheme.positive),
            ("Warning", counts.warning, WTheme.warning),
            ("Error", counts.error, WTheme.negative),
        ]
        let average = averageScore
        let tone: Tone = average >= 80 ? .emerald : average >= 60 ? .amber : .rose
        return VStack(alignment: .leading, spacing: 16) {
            HStack(alignment: .center, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Average health")
                        .font(.system(size: 14, weight: .medium))
                        .foregroundStyle(WTheme.inkSecondary)
                    HStack(alignment: .firstTextBaseline, spacing: 4) {
                        CountUpText(value: shown ? Double(average) : 0, format: { "\(Int($0.rounded()))" })
                            .font(.system(size: 40, weight: .semibold, design: .rounded))
                            .monospacedDigit()
                            .foregroundStyle(WTheme.ink)
                            .animation(.easeOut(duration: 1.1), value: shown)
                        Text("/ 100")
                            .font(.system(size: 15))
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                    Text("across \(store.accounts.count) \(store.accounts.count == 1 ? "mailbox" : "mailboxes")")
                        .font(.system(size: 13.5))
                        .foregroundStyle(WTheme.inkTertiary)
                }
                Spacer()
                SweepRing(fraction: Double(average) / 100, color: tone.color, shown: shown, lineWidth: 9) {
                    Image(systemName: tone == .emerald ? "checkmark" : "exclamationmark")
                        .font(.system(size: 24, weight: .bold))
                        .foregroundStyle(tone.color)
                        .symbolEffect(.bounce, value: shown)
                }
                .frame(width: 88, height: 88)
            }
            GeometryReader { proxy in
                HStack(spacing: 3) {
                    ForEach(Array(segments.enumerated()), id: \.offset) { index, segment in
                        if segment.1 > 0 {
                            RoundedRectangle(cornerRadius: 4, style: .continuous)
                                .fill(segment.2)
                                .frame(width: shown ? max(6, (proxy.size.width - 6) * CGFloat(segment.1) / CGFloat(total)) : 0)
                                .animation(.spring(response: 0.8, dampingFraction: 0.82).delay(0.25 + Double(index) * 0.1), value: shown)
                        }
                    }
                    Spacer(minLength: 0)
                }
            }
            .frame(height: 10)
            HStack(spacing: 16) {
                ForEach(Array(segments.enumerated()), id: \.offset) { _, segment in
                    HStack(spacing: 6) {
                        Circle().fill(segment.2).frame(width: 7, height: 7)
                        Text("\(segment.1) \(segment.0.lowercased())")
                            .font(.system(size: 13.5))
                            .monospacedDigit()
                            .foregroundStyle(segment.1 > 0 ? WTheme.ink : WTheme.inkTertiary)
                    }
                }
                Spacer()
            }
        }
        .coverCard()
    }

    private var listCard: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(store.accounts.enumerated()), id: \.element.id) { index, account in
                if index > 0 { Hairline().padding(.leading, 66) }
                NavigationLink {
                    AnalyticsAccountDetailView(account: account)
                } label: {
                    AnalyticsAccountRowView(account: account, shown: shown, index: index)
                }
                .buttonStyle(MoreRowPressStyle())
            }
        }
        .padding(.vertical, 4)
        .frame(maxWidth: .infinity, alignment: .leading)
        .clipShape(RoundedRectangle(cornerRadius: 22, style: .continuous))
        .surfaceCard(in: RoundedRectangle(cornerRadius: 22, style: .continuous))
    }
}

struct AnalyticsAccountRowView: View {
    let account: AccountHealthRow
    var shown = true
    var index = 0

    var body: some View {
        let tone = AnalyticsToneMap.health(account.health?.status)
        let usage = account.dailyUsage
        let limit = max(0, usage?.campaignLimit ?? 0)
        let sent = usage?.campaignSent ?? 0
        let issues = account.errors?.count ?? 0
        return HStack(spacing: 12) {
            MailboxAvatar(email: account.email ?? "", avatarURL: account.avatarURL, brand: account.resolvedBrand, seed: account.id, size: 40)
            VStack(alignment: .leading, spacing: 6) {
                Text(account.email ?? "Mailbox")
                    .font(.system(size: 15.5, weight: .medium))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                HStack(spacing: 6) {
                    Text(account.providerLabel)
                    if issues > 0 {
                        Text("·")
                        Text("\(issues) \(issues == 1 ? "issue" : "issues")")
                            .foregroundStyle(WTheme.negative)
                    } else if limit > 0 {
                        Text("·")
                        Text("\(sent)/\(limit) today").monospacedDigit()
                    }
                }
                .font(.system(size: 13))
                .foregroundStyle(WTheme.inkSecondary)
                .lineLimit(1)
                if limit > 0, sent > 0 {
                    GrowBar(fraction: Double(sent) / Double(limit), color: WTheme.accent, shown: shown, delay: 0.2 + Double(index) * 0.05, height: 4)
                        .frame(maxWidth: 160)
                }
            }
            Spacer(minLength: 6)
            HealthRing(score: account.health?.score, tone: tone, size: 38)
            Image(systemName: "chevron.right")
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(WTheme.inkTertiary)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .contentShape(Rectangle())
    }
}
