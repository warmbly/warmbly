import SwiftUI

// MARK: - Brand logo

/// The Warmbly "flick" mark, transcribed from the web SVG
/// (viewBox 0 0 746 764, single polygon, tinted via foregroundStyle).
struct WarmblyLogo: Shape {
    func path(in rect: CGRect) -> Path {
        let sx = rect.width / 746
        let sy = rect.height / 764
        func point(_ x: CGFloat, _ y: CGFloat) -> CGPoint {
            CGPoint(x: rect.minX + x * sx, y: rect.minY + y * sy)
        }
        var path = Path()
        path.move(to: point(222.805, 644.772))
        path.addLine(to: point(186.274, 108.881))
        path.addLine(to: point(704.5, 451.158))
        path.addLine(to: point(484.5, 451.158))
        path.addLine(to: point(245.5, 196.158))
        path.addLine(to: point(444, 463.5))
        path.closeSubpath()
        return path
    }
}

struct WarmblyWordmark: View {
    var markSize: CGFloat = 22

    var body: some View {
        HStack(spacing: 8) {
            WarmblyLogo()
                .fill(Color.primary)
                .frame(width: markSize, height: markSize * (764 / 746))
            Text("Warmbly")
                .font(.system(size: markSize * 0.82, weight: .heavy, design: .default))
                .tracking(-0.4)
        }
    }
}

// MARK: - Tones

/// The product's semantic tint pairs (bg-*-50 + text-*-700 chips on the web).
enum Tone {
    case emerald, amber, rose, sky, slate, indigo, orange

    var color: Color {
        switch self {
        case .emerald: WTheme.positive
        case .amber: WTheme.warning
        case .rose: WTheme.negative
        case .sky: WTheme.accent
        case .slate: WTheme.paused
        case .indigo: Color.dynamic(light: 0x4338CA, dark: 0x818CF8)
        case .orange: Color.dynamic(light: 0xEA580C, dark: 0xFB923C)
        }
    }

    var background: Color {
        switch self {
        case .emerald: Color.dynamic(light: 0xECFDF5, dark: 0x064E3B)
        case .amber: Color.dynamic(light: 0xFFFBEB, dark: 0x78350F)
        case .rose: Color.dynamic(light: 0xFFF1F2, dark: 0x881337)
        case .sky: Color.dynamic(light: 0xF0F9FF, dark: 0x0C4A6E)
        case .slate: Color.dynamic(light: 0xF1F5F9, dark: 0x1E293B)
        case .indigo: Color.dynamic(light: 0xEEF2FF, dark: 0x312E81)
        case .orange: Color.dynamic(light: 0xFFF7ED, dark: 0x7C2D12)
        }
    }
}

// MARK: - Small primitives

/// 10pt tracked-uppercase section label — the signature eyebrow style.
struct EyebrowLabel: View {
    let text: String

    init(_ text: String) { self.text = text }

    var body: some View {
        Text(text.uppercased())
            .font(.system(size: 10, weight: .medium))
            .tracking(1.4)
            .foregroundStyle(.tertiary)
    }
}

struct StatusPill: View {
    let text: String
    let tone: Tone
    var pulsing: Bool = false

    var body: some View {
        HStack(spacing: 5) {
            Circle()
                .fill(tone.color)
                .frame(width: 6, height: 6)
                .modifier(PingEffect(active: pulsing, color: tone.color))
            Text(text)
                .font(.system(size: 11, weight: .medium))
                .foregroundStyle(tone.color)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 3.5)
        .background(tone.background, in: Capsule())
    }
}

/// The web's `animate-ping` halo behind live dots.
struct PingEffect: ViewModifier {
    let active: Bool
    let color: Color
    @State private var animating = false

    func body(content: Content) -> some View {
        content.background {
            if active {
                Circle()
                    .fill(color.opacity(0.45))
                    .scaleEffect(animating ? 2.4 : 1)
                    .opacity(animating ? 0 : 0.6)
                    .animation(.easeOut(duration: 1.1).repeatForever(autoreverses: false), value: animating)
                    .onAppear { animating = true }
            }
        }
    }
}

/// Thin, tabular stat number over an eyebrow label; stats are thin, never bold.
struct StatCell: View {
    let label: String
    let value: String
    var tone: Tone? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            EyebrowLabel(label)
            Text(value)
                .font(.system(size: 24, weight: .light, design: .default))
                .monospacedDigit()
                .foregroundStyle(tone?.color ?? Color.primary)
                .contentTransition(.numericText())
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Avatars

struct WAvatar: View {
    let name: String
    var imageURL: String?
    var seed: String = ""
    var size: CGFloat = 28
    /// Light treatment for placement on the sky/Air hero: a clean white disc
    /// with tinted initials, instead of a saturated fill that muddies on blue.
    var onSky: Bool = false

    private var initials: String {
        let parts = name.split(separator: " ")
        let letters = parts.prefix(2).compactMap(\.first)
        if letters.isEmpty { return "?" }
        return String(letters).uppercased()
    }

    var body: some View {
        let tint = WTheme.avatarColor(for: seed.isEmpty ? name : seed)
        ZStack {
            if onSky {
                Circle().fill(.white)
                Circle().fill(tint.opacity(0.12))
                Text(initials)
                    .font(.system(size: size * 0.4, weight: .bold))
                    .foregroundStyle(tint)
            } else {
                // Solid, softly-graded fill with white initials — the clean
                // email-client look, not a pale rainbow wash.
                Circle().fill(
                    LinearGradient(
                        colors: [tint.opacity(0.92), tint],
                        startPoint: .top, endPoint: .bottom
                    )
                )
                Text(initials)
                    .font(.system(size: size * 0.4, weight: .semibold))
                    .foregroundStyle(.white)
                    .shadow(color: tint.opacity(0.35), radius: 0.5, y: 0.5)
            }
            if let imageURL, let url = URL(string: imageURL) {
                AsyncImage(url: url) { phase in
                    if case let .success(image) = phase {
                        image.resizable().scaledToFill()
                    }
                }
                .clipShape(Circle())
            }
        }
        .frame(width: size, height: size)
    }
}

// MARK: - Presence UI

/// Header avatar stack: amber ring = editing/replying, emerald ring = viewing.
struct PresenceAvatars: View {
    @Environment(AppEnvironment.self) private var env
    var maxShown = 4

    var body: some View {
        let others = env.realtime.presence.online.filter { $0.id != env.session.user?.id }
        if !others.isEmpty {
            HStack(spacing: -7) {
                ForEach(others.prefix(maxShown)) { member in
                    WAvatar(
                        name: member.primary?.name ?? "?",
                        imageURL: member.primary?.avatar,
                        seed: member.id,
                        size: 26
                    )
                    .overlay(Circle().stroke(ringColor(member), lineWidth: 1.5))
                }
                if others.count > maxShown {
                    Text("+\(others.count - maxShown)")
                        .font(.system(size: 10, weight: .semibold))
                        .frame(width: 26, height: 26)
                        .background(Tone.slate.background, in: Circle())
                }
            }
        }
    }

    private func ringColor(_ member: PresenceMember) -> Color {
        let action = member.primary?.action
        return action == "editing" || action == "replying" ? WTheme.presenceEditing : WTheme.presenceViewing
    }
}

/// "Who else is on this record" pill for detail screens.
struct ResourceViewers: View {
    @Environment(AppEnvironment.self) private var env
    let resource: String

    var body: some View {
        let viewers = env.realtime.presence.viewers(of: resource, excluding: env.session.user?.id)
        if !viewers.isEmpty {
            HStack(spacing: 4) {
                ForEach(viewers.prefix(3)) { member in
                    WAvatar(name: member.primary?.name ?? "?", imageURL: member.primary?.avatar, seed: member.id, size: 20)
                }
                Text(label(for: viewers))
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(hot(viewers) ? WTheme.presenceEditing : WTheme.presenceViewing)
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background((hot(viewers) ? Tone.amber : Tone.emerald).background, in: Capsule())
        }
    }

    private func hot(_ viewers: [PresenceMember]) -> Bool {
        viewers.contains { $0.primary?.action == "editing" || $0.primary?.action == "replying" }
    }

    private func label(for viewers: [PresenceMember]) -> String {
        if viewers.count == 1 {
            let name = viewers[0].primary?.name ?? "Someone"
            switch viewers[0].primary?.action {
            case "editing": return "\(name) is editing"
            case "replying": return "\(name) is replying"
            default: return "\(name) is viewing"
            }
        }
        return "\(viewers.count) viewing"
    }
}

/// Claims a record for presence while the view is on screen.
struct PresenceResourceModifier: ViewModifier {
    @Environment(AppEnvironment.self) private var env
    let resource: String?
    let action: String

    func body(content: Content) -> some View {
        content
            .onAppear {
                env.realtime.setPresence(page: nil, resource: resource, action: action)
            }
            .onDisappear {
                env.realtime.setPresence(page: nil, resource: nil, action: "viewing")
            }
    }
}

extension View {
    func presenceResource(_ resource: String?, action: String = "viewing") -> some View {
        modifier(PresenceResourceModifier(resource: resource, action: action))
    }
}

// MARK: - States

struct EmptyStateView: View {
    let title: String
    let message: String
    var ctaTitle: String? = nil
    var cta: (() -> Void)? = nil

    var body: some View {
        VStack(spacing: 8) {
            Text(title)
                .font(.system(size: 14, weight: .medium))
            Text(message)
                .font(.system(size: 12))
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 260)
            if let ctaTitle, let cta {
                Button(ctaTitle, action: cta)
                    .buttonStyle(.borderedProminent)
                    .controlSize(.small)
                    .padding(.top, 6)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(.vertical, 48)
    }
}

struct ErrorStateView: View {
    let title: String
    var message: String? = nil
    let retry: () async -> Void

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: "exclamationmark.triangle")
                .font(.system(size: 18))
                .foregroundStyle(WTheme.negative)
                .frame(width: 36, height: 36)
                .background(Tone.rose.background, in: RoundedRectangle(cornerRadius: 8))
            Text(title)
                .font(.system(size: 14, weight: .medium))
            if let message {
                Text(message)
                    .font(.system(size: 12))
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 280)
            }
            Button("Try again") {
                Task { await retry() }
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(.vertical, 48)
    }
}

/// Diagonal shimmer sweep for skeleton placeholders: a bright band moves
/// across a dimmed mask, so loading reads as alive without spinners.
private struct SkeletonShimmer: ViewModifier {
    @State private var on = false

    func body(content: Content) -> some View {
        content
            .mask {
                LinearGradient(
                    stops: [
                        .init(color: .black.opacity(0.35), location: 0),
                        .init(color: .black, location: 0.5),
                        .init(color: .black.opacity(0.35), location: 1),
                    ],
                    startPoint: on ? UnitPoint(x: 1, y: 0.8) : UnitPoint(x: -1.5, y: -0.3),
                    endPoint: on ? UnitPoint(x: 2.5, y: 1.1) : UnitPoint(x: 0, y: 0)
                )
            }
            .animation(.linear(duration: 1.3).repeatForever(autoreverses: false), value: on)
            .onAppear { on = true }
    }
}

extension View {
    func skeletonShimmer() -> some View { modifier(SkeletonShimmer()) }
}

/// List placeholder shaped like real rows (44pt avatar + two text lines with
/// varied widths), shimmering and fading toward the bottom.
struct SkeletonRows: View {
    var rows = 8

    private static let widths: [(title: CGFloat, subtitle: CGFloat)] = [
        (150, 230), (185, 200), (120, 250), (200, 175),
        (140, 220), (170, 240), (125, 195), (190, 210),
    ]

    var body: some View {
        VStack(spacing: 0) {
            ForEach(0 ..< rows, id: \.self) { index in
                let width = Self.widths[index % Self.widths.count]
                HStack(spacing: 12) {
                    Circle()
                        .frame(width: 44, height: 44)
                    VStack(alignment: .leading, spacing: 7) {
                        RoundedRectangle(cornerRadius: 4)
                            .frame(width: width.title, height: 11)
                        RoundedRectangle(cornerRadius: 4)
                            .frame(width: width.subtitle, height: 9)
                    }
                    Spacer(minLength: 0)
                }
                .foregroundStyle(Color(.systemGray5))
                .padding(.horizontal, 16)
                .padding(.vertical, 10)
                .opacity(1.0 - Double(index) * (0.7 / Double(max(rows - 1, 1))))
            }
        }
        .skeletonShimmer()
        .accessibilityLabel("Loading")
    }
}

// MARK: - Formatting

enum WFormat {
    /// 12.3k / 1.2M compact numbers, mirroring the web's AnimatedNumber output.
    static func compact(_ value: Int) -> String {
        let n = Double(value)
        switch abs(n) {
        case 1_000_000...:
            return String(format: "%.1fM", n / 1_000_000).replacingOccurrences(of: ".0M", with: "M")
        case 1_000...:
            return String(format: "%.1fk", n / 1_000).replacingOccurrences(of: ".0k", with: "k")
        default:
            return String(value)
        }
    }

    static func relative(_ date: Date) -> String {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter.localizedString(for: date, relativeTo: Date())
    }

    static func percent(_ numerator: Int, of denominator: Int) -> String {
        guard denominator > 0 else { return "0%" }
        return String(format: "%.0f%%", Double(numerator) / Double(denominator) * 100)
    }
}
