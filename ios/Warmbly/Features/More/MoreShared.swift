import SwiftUI

// Small shared pieces used across the More tab screens.

/// Flat hub row: 34pt tinted icon tile + medium title (+ optional footnote
/// subtitle), with an optional trailing value and a live unread count chip.
struct MoreHubRow: View {
    let icon: String
    let title: String
    var subtitle: String? = nil
    let tone: Tone
    var titleTone: Tone? = nil
    var value: String? = nil
    var badge: Int = 0

    private var hasSubtitle: Bool {
        if let subtitle, !subtitle.isEmpty { return true }
        return false
    }

    var body: some View {
        HStack(spacing: 12) {
            IconTile(symbol: icon, tone: tone, size: 34)
            VStack(alignment: .leading, spacing: 1.5) {
                Text(title)
                    .font(.body.weight(.medium))
                    .foregroundStyle(titleTone?.color ?? Color.primary)
                if let subtitle, !subtitle.isEmpty {
                    Text(subtitle)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 8)
            if let value {
                Text(value)
                    .font(.footnote)
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
                    .contentTransition(.numericText())
            }
            if badge > 0 {
                Text(badge > 99 ? "99+" : "\(badge)")
                    .font(.system(size: 10, weight: .bold))
                    .monospacedDigit()
                    .foregroundStyle(.white)
                    .padding(.horizontal, 5)
                    .frame(minWidth: 18, minHeight: 18)
                    .background(WTheme.accent, in: Capsule())
                    .contentTransition(.numericText())
                    .animation(.default, value: badge)
            }
        }
        .padding(.vertical, hasSubtitle ? 9 : 11)
    }
}

/// Plan capsule: name colored by tier, "{Plan} · Trial" while trialing,
/// rose when past due.
struct MorePlanPill: View {
    let subscription: SubscriptionInfo?

    private var planName: String {
        let raw = subscription?.plan?.name ?? "Free"
        return raw.isEmpty ? "Free" : raw.capitalized
    }

    private var text: String {
        switch subscription?.status {
        case "past_due": return "\(planName) · Past due"
        case "trialing": return "\(planName) · Trial"
        default: return planName
        }
    }

    private var colors: (fg: Color, bg: Color) {
        if subscription?.status == "past_due" {
            return (Tone.rose.color, Tone.rose.background)
        }
        return MoreStyle.planColors(planName)
    }

    var body: some View {
        Text(text)
            .font(.system(size: 10, weight: .semibold))
            .foregroundStyle(colors.fg)
            .padding(.horizontal, 7)
            .padding(.vertical, 2.5)
            .background(colors.bg, in: Capsule())
    }
}

/// Tiny role chip tinted by the role's custom color.
struct MoreRoleChipView: View {
    let name: String
    let color: Color

    var body: some View {
        Text(name)
            .font(.system(size: 10, weight: .medium))
            .foregroundStyle(color)
            .padding(.horizontal, 6)
            .padding(.vertical, 2.5)
            .background(color.opacity(0.13), in: Capsule())
            .lineLimit(1)
    }
}

/// Thin usage bar: "used of limit" with a tone that heats up near the cap.
struct MoreUsageBar: View {
    let label: String
    let used: Int?
    let limit: Int?

    private var fraction: Double {
        guard let used, let limit, limit > 0 else { return 0 }
        return min(1, Double(used) / Double(limit))
    }

    private var barColor: Color {
        if fraction >= 1 { return WTheme.negative }
        if fraction >= 0.85 { return WTheme.warning }
        return WTheme.accent
    }

    private var valueText: String {
        let usedText = WFormat.compact(used ?? 0)
        if let limit { return "\(usedText) of \(WFormat.compact(limit))" }
        return "\(usedText) · unlimited"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(label)
                    .font(.system(size: 12.5, weight: .medium))
                Spacer()
                Text(valueText)
                    .font(.system(size: 11))
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                    .contentTransition(.numericText())
            }
            GeometryReader { proxy in
                ZStack(alignment: .leading) {
                    Capsule().fill(Tone.slate.background)
                    if limit != nil {
                        Capsule()
                            .fill(barColor)
                            .frame(width: max(fraction > 0 ? 4 : 0, proxy.size.width * fraction))
                    }
                }
            }
            .frame(height: 4)
        }
        .padding(.vertical, 4)
        .animation(.default, value: used)
    }
}
