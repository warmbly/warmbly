import SwiftUI

extension Color {
    init(hex: UInt32) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255
        )
    }

    /// Dynamic color resolving per interface style.
    static func dynamic(light: UInt32, dark: UInt32) -> Color {
        Color(UIColor { traits in
            let hex = traits.userInterfaceStyle == .dark ? dark : light
            return UIColor(
                red: CGFloat((hex >> 16) & 0xFF) / 255,
                green: CGFloat((hex >> 8) & 0xFF) / 255,
                blue: CGFloat(hex & 0xFF) / 255,
                alpha: 1
            )
        })
    }
}

/// Warmbly brand palette: the marketing site's ink-on-white (slate neutrals,
/// near-black actions, hairlines) with sky kept for data and live state.
/// Dark mode uses zinc on charcoal, with cards raised a visible step.
enum WTheme {
    // Neutrals
    static let canvas = Color.dynamic(light: 0xFFFFFF, dark: 0x131315)
    static let surface = Color.dynamic(light: 0xF6F6F7, dark: 0x1A1A1D)
    static let raised = Color.dynamic(light: 0xFFFFFF, dark: 0x1F1F22)
    static let fill = Color.dynamic(light: 0xF0F0F2, dark: 0x29292D)
    static let ink = Color.dynamic(light: 0x111113, dark: 0xF7F7F8)
    static let inkInverse = Color.dynamic(light: 0xFFFFFF, dark: 0x131315)
    static let inkSecondary = Color.dynamic(light: 0x6E6E76, dark: 0xA8A8B0)
    static let inkTertiary = Color.dynamic(light: 0xA3A3AB, dark: 0x7C7C85)
    static let hairline = Color.dynamic(light: 0xEFEFF1, dark: 0x27272B)
    static let hairlineStrong = Color.dynamic(light: 0xE4E4E7, dark: 0x343439)

    // iOS grouped settings: a grey page with white cards (dark: black page, grey cards).
    static let groupedPage = Color.dynamic(light: 0xF2F2F7, dark: 0x131315)
    static let groupedCard = Color.dynamic(light: 0xFFFFFF, dark: 0x202024)
    /// A card on the plain canvas: a quiet step off white, edged with a hairline.
    static let canvasCard = Color.dynamic(light: 0xF8F8F9, dark: 0x1B1B1E)

    /// The rim of a sheet raised over the sky photo; only dark mode needs one.
    static let sheetEdge = Color(UIColor { $0.userInterfaceStyle == .dark
        ? UIColor.white.withAlphaComponent(0.1)
        : UIColor.clear })

    /// Body for floating glass chrome: in dark mode, content scrolling under it stays quiet.
    static let glassTint = Color(UIColor { $0.userInterfaceStyle == .dark
        ? UIColor(red: 0.13, green: 0.13, blue: 0.14, alpha: 0.85)
        : UIColor.clear })

    /// Ink on the sign-in sky: dark on the light photo, white on the night.
    static let onSky = Color.dynamic(light: 0x111827, dark: 0xFFFFFF)

    /// A glassy tile on the sky; the pale photo needs more white to show one.
    static func skyTile(_ opacity: Double) -> Color {
        Color(UIColor { $0.userInterfaceStyle == .dark
            ? UIColor.white.withAlphaComponent(opacity)
            : UIColor.white.withAlphaComponent(min(0.8, 0.35 + opacity * 2.5)) })
    }

    // Accent (sky)
    static let accent = Color.dynamic(light: 0x0A8DE6, dark: 0x3DB8FF)
    static let accentDeep = Color.dynamic(light: 0x0670C2, dark: 0x0EA5E9)
    static let accentSoft = Color.dynamic(light: 0xE6F4FE, dark: 0x112B3F)

    // Status
    static let positive = Color.dynamic(light: 0x12A150, dark: 0x32D583) // green
    static let warning = Color.dynamic(light: 0xE88A00, dark: 0xFFB020) // amber
    static let negative = Color.dynamic(light: 0xEB3B4E, dark: 0xFF6170) // red
    static let info = Color.dynamic(light: 0x0A8DE6, dark: 0x3DB8FF) // sky
    static let special = Color.dynamic(light: 0x7A5AF8, dark: 0xA48AFB) // violet
    static let paused = Color.dynamic(light: 0x8E8E96, dark: 0x8E8E96) // grey

    // Presence conventions from the web app: viewing = emerald, editing/replying = amber.
    static let presenceViewing = positive
    static let presenceEditing = warning

    /// Deterministic avatar tint per user id, mirroring the web avatar stack.
    static func avatarColor(for id: String) -> Color {
        let palette: [Color] = [
            .dynamic(light: 0x0A8DE6, dark: 0x2FA8F5),
            .dynamic(light: 0x7A5AF8, dark: 0x8B6CF9),
            .dynamic(light: 0x12A150, dark: 0x22B26A),
            .dynamic(light: 0xF2A100, dark: 0xF2A100),
            .dynamic(light: 0xEC4899, dark: 0xF05FA9),
            .dynamic(light: 0x5B6CF9, dark: 0x6D7CFA),
            .dynamic(light: 0xF97316, dark: 0xFB8435),
            .dynamic(light: 0x0FB5A6, dark: 0x1CC2B3),
        ]
        var hash = 5381
        for byte in id.utf8 { hash = ((hash << 5) &+ hash) &+ Int(byte) }
        return palette[abs(hash) % palette.count]
    }
}

/// Type scale: medium-weight display with tight tracking (the site's h1/h2),
/// monospaced meta for labels, ids and timestamps.
enum WFont {
    static func display(_ size: CGFloat, weight: Font.Weight = .medium) -> Font {
        .system(size: size, weight: weight)
    }

    /// Meta text (times, counts, small labels): the system face with tabular
    /// digits, a step up from the old monospaced sizes so call sites keep theirs.
    static func meta(_ size: CGFloat, weight: Font.Weight = .regular) -> Font {
        .system(size: size + 1.5, weight: weight).monospacedDigit()
    }

    /// Real code and data: tool arguments, code blocks.
    static func code(_ size: CGFloat, weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight, design: .monospaced)
    }

    /// Tracking that keeps large display type tight, as on the site.
    static func tracking(for size: CGFloat) -> CGFloat {
        size >= 20 ? -size * 0.035 : -size * 0.01
    }
}

extension View {
    /// Display text at `size`: medium weight, tight tracking.
    func displayFont(_ size: CGFloat, weight: Font.Weight = .medium) -> some View {
        font(WFont.display(size, weight: weight))
            .tracking(WFont.tracking(for: size))
    }
}
