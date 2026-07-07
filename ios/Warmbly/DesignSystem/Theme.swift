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

/// Warmbly brand palette, mapped from the web dashboard's tailwind tokens
/// (slate neutrals + sky accent) onto dynamic iOS colors.
enum WTheme {
    // Accent (sky)
    static let accent = Color.dynamic(light: 0x0284C7, dark: 0x38BDF8)
    static let accentDeep = Color.dynamic(light: 0x0369A1, dark: 0x0EA5E9)
    static let accentSoft = Color.dynamic(light: 0xE0F2FE, dark: 0x0C4A6E)

    // Status
    static let positive = Color.dynamic(light: 0x059669, dark: 0x34D399) // emerald
    static let warning = Color.dynamic(light: 0xD97706, dark: 0xFBBF24) // amber
    static let negative = Color.dynamic(light: 0xDC2626, dark: 0xF87171) // red
    static let info = Color.dynamic(light: 0x0284C7, dark: 0x38BDF8) // sky
    static let special = Color.dynamic(light: 0x7C3AED, dark: 0xA78BFA) // violet
    static let paused = Color.dynamic(light: 0x64748B, dark: 0x94A3B8) // slate

    // Presence conventions from the web app: viewing = emerald, editing/replying = amber.
    static let presenceViewing = positive
    static let presenceEditing = warning

    /// Deterministic avatar tint per user id, mirroring the web avatar stack.
    static func avatarColor(for id: String) -> Color {
        let palette: [Color] = [
            .dynamic(light: 0x0284C7, dark: 0x38BDF8),
            .dynamic(light: 0x7C3AED, dark: 0xA78BFA),
            .dynamic(light: 0x059669, dark: 0x34D399),
            .dynamic(light: 0xD97706, dark: 0xFBBF24),
            .dynamic(light: 0xDB2777, dark: 0xF472B6),
            .dynamic(light: 0x4F46E5, dark: 0x818CF8),
            .dynamic(light: 0xEA580C, dark: 0xFB923C),
            .dynamic(light: 0x0D9488, dark: 0x2DD4BF),
        ]
        var hash = 5381
        for byte in id.utf8 { hash = ((hash << 5) &+ hash) &+ Int(byte) }
        return palette[abs(hash) % palette.count]
    }
}
