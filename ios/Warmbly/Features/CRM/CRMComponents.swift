import SwiftUI

// Small CRM-scoped presentation helpers shared across the module's screens.

// MARK: - Colors and formatting

enum CRMHex {
    /// Parses a `#rrggbb` string (stage/task-type colors) into a Color.
    static func color(_ hex: String?) -> Color? {
        guard var raw = hex?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty else { return nil }
        if raw.hasPrefix("#") { raw.removeFirst() }
        guard raw.count == 6, let value = UInt32(raw, radix: 16) else { return nil }
        return Color(hex: value)
    }
}

enum CRMFormat {
    private static let currencySymbols: [String: String] = [
        "USD": "$", "EUR": "€", "GBP": "£", "JPY": "¥", "INR": "₹", "BRL": "R$",
        "AUD": "A$", "CAD": "C$", "NZD": "NZ$", "HUF": "Ft ", "CHF": "CHF ",
        "SEK": "kr ", "NOK": "kr ", "DKK": "kr ", "PLN": "zł ", "CZK": "Kč ",
    ]

    static func symbol(for code: String) -> String {
        currencySymbols[code.uppercased()] ?? "\(code.uppercased()) "
    }

    /// Full currency amount, e.g. "$1,250" / "$1,250.50".
    static func currency(_ value: Double, code: String?) -> String {
        let resolved = (code?.isEmpty == false) ? code!.uppercased() : "USD"
        let wholeNumber = value.truncatingRemainder(dividingBy: 1) == 0
        return value.formatted(
            .currency(code: resolved).precision(.fractionLength(wholeNumber ? 0 : 2))
        )
    }

    /// Compact currency for stat strips, e.g. "$12.3k".
    static func compactCurrency(_ value: Double, code: String?) -> String {
        let resolved = (code?.isEmpty == false) ? code!.uppercased() : "USD"
        if abs(value) >= 1000 {
            return symbol(for: resolved) + WFormat.compact(Int(value))
        }
        return currency(value, code: resolved)
    }

    /// Short due-date label: "today 14:30", "tomorrow", "Jun 12".
    static func dueLabel(_ date: Date) -> String {
        let calendar = Calendar.current
        if calendar.isDateInToday(date) {
            return "today " + date.formatted(date: .omitted, time: .shortened)
        }
        if calendar.isDateInTomorrow(date) { return "tomorrow" }
        if calendar.isDateInYesterday(date) { return "yesterday" }
        return date.formatted(.dateTime.day().month(.abbreviated))
    }

    /// Meeting time label with the time always included.
    static func meetingTime(_ date: Date) -> String {
        let calendar = Calendar.current
        if calendar.isDateInToday(date) {
            return "today " + date.formatted(date: .omitted, time: .shortened)
        }
        if calendar.isDateInTomorrow(date) {
            return "tomorrow " + date.formatted(date: .omitted, time: .shortened)
        }
        return date.formatted(.dateTime.day().month(.abbreviated).hour().minute())
    }
}

// MARK: - Tone mappings

extension CRMDeal {
    var statusTone: Tone {
        switch status {
        case "won": .emerald
        case "lost": .rose
        default: .sky
        }
    }
}

extension CRMTask {
    var priorityTone: Tone {
        switch priority {
        case "urgent": .rose
        case "high": .orange
        case "low": .slate
        default: .sky
        }
    }
}

extension CRMMeeting {
    var statusTone: Tone {
        switch status {
        case "booked": .emerald
        case "rescheduled": .amber
        case "canceled": .rose
        case "no_show": .rose
        case "completed": .slate
        default: .slate
        }
    }

    var statusLabel: String {
        switch status {
        case "no_show": "no show"
        default: status ?? "booked"
        }
    }
}

// MARK: - Chips and accents

/// Colored capsule chip driven by a server hex color (stages, task types).
struct CRMColorChip: View {
    let text: String
    var hex: String?

    var body: some View {
        let color = CRMHex.color(hex) ?? WTheme.paused
        Text(text)
            .font(.system(size: 11, weight: .medium))
            .foregroundStyle(color)
            .padding(.horizontal, 8)
            .padding(.vertical, 3.5)
            .background(color.opacity(0.12), in: Capsule())
            .lineLimit(1)
    }
}

/// Small always-pulsing dot (overdue/red, today/sky accents).
struct CRMPingDot: View {
    var color: Color

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: 6, height: 6)
            .modifier(PingEffect(active: true, color: color))
    }
}

/// Tracked-uppercase section header with an optional pulsing accent + count.
struct CRMSectionHeader: View {
    let title: String
    var count: Int? = nil
    var tone: Tone? = nil
    var ping = false

    var body: some View {
        HStack(spacing: 6) {
            if ping, let tone {
                CRMPingDot(color: tone.color).padding(.trailing, 2)
            }
            Text(title.uppercased())
                .font(.system(size: 10, weight: .medium))
                .tracking(1.4)
                .foregroundStyle(tone?.color ?? Color.secondary)
            if let count {
                Text("\(count)")
                    .font(.system(size: 10, weight: .semibold))
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
            }
            Spacer(minLength: 0)
        }
    }
}

/// Deal stat cell: count over eyebrow label plus a compact value line;
/// doubles as a status filter with a bottom accent bar when selected.
struct CRMDealStatCell: View {
    let label: String
    let count: Int?
    let value: Double?
    let currency: String?
    let tone: Tone
    let selected: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            EyebrowLabel(label)
            Text(count.map { "\($0)" } ?? "–")
                .font(.system(size: 22, weight: .bold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(selected ? tone.color : Color.primary)
                .contentTransition(.numericText())
            Text(value.map { CRMFormat.compactCurrency($0, code: currency) } ?? " ")
                .font(.footnote)
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .contentTransition(.numericText())
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.bottom, 6)
        .overlay(alignment: .bottom) {
            Rectangle()
                .fill(selected ? tone.color : Color.clear)
                .frame(height: 2)
        }
    }
}
