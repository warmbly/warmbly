import SwiftUI

// MARK: - Header

/// The top of a settings browser: the drawer button, a title with one line
/// under it, the screen's own buttons, and close. Glass over the grouped page.
struct CoverHeader<Trailing: View>: View {
    let title: String
    var subtitle: String?
    var onMenu: (() -> Void)?
    let onClose: () -> Void
    @ViewBuilder var trailing: () -> Trailing

    var body: some View {
        GlassEffectContainer(spacing: 10) {
            HStack(spacing: 10) {
                if let onMenu {
                    GlassCircleButton(symbol: "line.3.horizontal", label: "Open menu", action: onMenu)
                }
                VStack(alignment: .leading, spacing: 1) {
                    Text(title)
                        .font(.system(size: 19, weight: .semibold))
                        .tracking(-0.3)
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                        .contentTransition(.opacity)
                    if let subtitle {
                        Text(subtitle)
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkSecondary)
                            .monospacedDigit()
                            .lineLimit(1)
                            .contentTransition(.numericText())
                    }
                }
                .padding(.leading, 2)
                .animation(.snappy, value: title)
                .animation(.snappy, value: subtitle)
                Spacer(minLength: 6)
                PresenceAvatars(maxShown: 3)
                trailing()
                CoverCloseButton(label: "Close", action: onClose)
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 6)
        .padding(.bottom, 8)
    }
}

/// A cover's close button, absent where the page is an iPad sidebar destination.
struct CoverCloseButton: View {
    let label: String
    let action: () -> Void
    @Environment(\.isSidebarDestination) private var isSidebarDestination

    var body: some View {
        if !isSidebarDestination {
            GlassCircleButton(symbol: "xmark", label: label, action: action)
        }
    }
}

extension CoverHeader where Trailing == EmptyView {
    init(title: String, subtitle: String? = nil, onMenu: (() -> Void)? = nil, onClose: @escaping () -> Void) {
        self.init(title: title, subtitle: subtitle, onMenu: onMenu, onClose: onClose, trailing: { EmptyView() })
    }
}

// MARK: - Pill switcher

/// A segmented control whose selection slides between the options.
struct PillSwitcher<Value: Hashable>: View {
    let options: [Value]
    @Binding var selection: Value
    let label: (Value) -> String
    var badge: (Value) -> Int = { _ in 0 }

    @Namespace private var space

    var body: some View {
        HStack(spacing: 4) {
            ForEach(options, id: \.self) { option in
                let selected = selection == option
                Button {
                    withAnimation(.spring(response: 0.38, dampingFraction: 0.82)) { selection = option }
                } label: {
                    HStack(spacing: 6) {
                        Text(label(option))
                            .font(.system(size: 14.5, weight: selected ? .semibold : .medium))
                            .monospacedDigit()
                        let count = badge(option)
                        if count > 0 {
                            Text("\(count)")
                                .font(.system(size: 11.5, weight: .bold))
                                .monospacedDigit()
                                .foregroundStyle(selected ? WTheme.inkInverse : WTheme.inkSecondary)
                                .padding(.horizontal, 6)
                                .frame(minWidth: 18, minHeight: 18)
                                .background(selected ? WTheme.ink : WTheme.hairlineStrong, in: Capsule())
                                .contentTransition(.numericText())
                        }
                    }
                    .foregroundStyle(selected ? WTheme.ink : WTheme.inkSecondary)
                    .frame(maxWidth: .infinity)
                    .frame(height: 34)
                    .background {
                        if selected {
                            Capsule()
                                .fill(GroupedSurface.card)
                                .shadow(color: .black.opacity(0.08), radius: 4, y: 1)
                                .matchedGeometryEffect(id: "pill", in: space)
                        }
                    }
                    .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
        }
        .padding(3)
        .background(WTheme.fill, in: Capsule())
        .sensoryFeedback(.selection, trigger: selection)
    }
}

// MARK: - Cards

extension View {
    /// A white card on the grouped page.
    func coverCard(padding: CGFloat = 18) -> some View {
        self
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .surfaceCard(in: RoundedRectangle(cornerRadius: 22, style: .continuous))
    }
}

/// A card's heading: a title, an optional line under it, and a trailing slot.
struct CardTitle<Trailing: View>: View {
    let title: String
    var caption: String?
    @ViewBuilder var trailing: () -> Trailing

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(WTheme.ink)
                if let caption {
                    Text(caption)
                        .font(.system(size: 13.5))
                        .foregroundStyle(WTheme.inkSecondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer(minLength: 4)
            trailing()
        }
    }
}

extension CardTitle where Trailing == EmptyView {
    init(_ title: String, caption: String? = nil) {
        self.init(title: title, caption: caption, trailing: { EmptyView() })
    }
}

/// A big display heading between groups of cards.
struct GroupHeading<Trailing: View>: View {
    let title: String
    @ViewBuilder var trailing: () -> Trailing

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title)
                .displayFont(22, weight: .semibold)
                .foregroundStyle(WTheme.ink)
            Spacer()
            trailing()
        }
        .padding(.horizontal, 4)
        .padding(.top, 8)
    }
}

extension GroupHeading where Trailing == EmptyView {
    init(_ title: String) {
        self.init(title: title, trailing: { EmptyView() })
    }
}

// MARK: - Bars

/// A capsule that grows to `fraction` once shown.
struct GrowBar: View {
    var fraction: Double
    var color: Color
    var shown: Bool
    var delay: Double = 0
    var height: CGFloat = 6

    var body: some View {
        GeometryReader { proxy in
            ZStack(alignment: .leading) {
                Capsule().fill(WTheme.fill)
                Capsule()
                    .fill(color)
                    .frame(width: shown ? max(fraction > 0 ? height : 0, proxy.size.width * min(1, max(0, fraction))) : 0)
                    .animation(.spring(response: 0.9, dampingFraction: 0.82).delay(delay), value: shown)
                    .animation(.spring(response: 0.6, dampingFraction: 0.85), value: fraction)
            }
        }
        .frame(height: height)
        .accessibilityHidden(true)
    }
}

/// A ring that sweeps to `fraction`, with whatever sits inside it.
struct SweepRing<Center: View>: View {
    var fraction: Double
    var color: Color
    var shown: Bool
    var lineWidth: CGFloat = 10
    var delay: Double = 0.15
    @ViewBuilder var center: () -> Center

    var body: some View {
        ZStack {
            Circle().stroke(color.opacity(0.14), lineWidth: lineWidth)
            Circle()
                .trim(from: 0, to: shown ? max(0.005, min(1, fraction)) : 0)
                .stroke(
                    AngularGradient(colors: [color.opacity(0.55), color], center: .center, startAngle: .degrees(0), endAngle: .degrees(360 * max(0.05, min(1, fraction)))),
                    style: StrokeStyle(lineWidth: lineWidth, lineCap: .round)
                )
                .rotationEffect(.degrees(-90))
                .animation(.spring(response: 1.2, dampingFraction: 0.85).delay(delay), value: shown)
                .animation(.spring(response: 0.7, dampingFraction: 0.85), value: fraction)
            center()
        }
    }
}

/// A settings browser's empty state: a soft glyph, a line, and one sentence.
struct CoverEmptyState: View {
    let symbol: String
    var color: Color = WTheme.accent
    let title: String
    let message: String
    var actionTitle: String?
    var action: (() -> Void)?

    @State private var floating = false

    var body: some View {
        VStack(spacing: 14) {
            ZStack {
                Circle().fill(color.opacity(0.08)).frame(width: 112, height: 112)
                Circle().fill(color.opacity(0.12)).frame(width: 76, height: 76)
                Image(systemName: symbol)
                    .font(.system(size: 30, weight: .medium))
                    .foregroundStyle(color)
                    .offset(y: floating ? -3 : 3)
            }
            .padding(.bottom, 4)
            Text(title)
                .font(.system(size: 19, weight: .semibold))
                .foregroundStyle(WTheme.ink)
            Text(message)
                .font(.system(size: 15))
                .foregroundStyle(WTheme.inkSecondary)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: 290)
            if let actionTitle, let action {
                Button(action: action) {
                    Text(actionTitle)
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(WTheme.inkInverse)
                        .padding(.horizontal, 20)
                        .frame(height: 44)
                        .background(WTheme.ink, in: Capsule())
                }
                .buttonStyle(TapScaleStyle())
                .padding(.top, 6)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 40)
        .onAppear {
            withAnimation(.easeInOut(duration: 2.4).repeatForever(autoreverses: true)) { floating = true }
        }
    }
}

// MARK: - Celebration

/// A short burst of confetti from the top of the screen whenever `trigger` changes.
struct ConfettiBurst: View {
    var trigger: Int

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var start: Date?
    @State private var pieces: [Piece] = []

    private struct Piece {
        let x: Double
        let angle: Double
        let speed: Double
        let spin: Double
        let size: Double
        let color: Color
        let round: Bool
    }

    private static let palette: [Color] = [
        WTheme.accent, WTheme.positive, WTheme.special, WTheme.warning,
        Tone.orange.color, Color(hex: 0xEC4899),
    ]

    var body: some View {
        TimelineView(.animation(paused: start == nil)) { context in
            Canvas { canvas, size in
                guard let start else { return }
                let t = context.date.timeIntervalSince(start)
                for piece in pieces {
                    let x = size.width * piece.x + cos(piece.angle) * piece.speed * t
                    let y = -10 + sin(piece.angle) * piece.speed * t + 520 * t * t
                    var layer = canvas
                    layer.opacity = max(0, 1 - max(0, t - 1.1) / 0.8)
                    layer.translateBy(x: x, y: y)
                    layer.rotate(by: .radians(piece.spin * t))
                    let rect = CGRect(x: -piece.size / 2, y: -piece.size / 3, width: piece.size, height: piece.round ? piece.size : piece.size * 0.55)
                    let path = piece.round ? Path(ellipseIn: rect) : Path(roundedRect: rect, cornerRadius: 1.5)
                    layer.fill(path, with: .color(piece.color))
                }
            }
        }
        .allowsHitTesting(false)
        .ignoresSafeArea()
        .accessibilityHidden(true)
        .onChange(of: trigger) { fire() }
    }

    private func fire() {
        guard !reduceMotion else { return }
        pieces = (0 ..< 90).map { _ in
            Piece(
                x: .random(in: 0.1 ... 0.9),
                angle: .random(in: 0.15 * .pi ... 0.85 * .pi),
                speed: .random(in: 90 ... 360),
                spin: .random(in: -9 ... 9),
                size: .random(in: 6 ... 11),
                color: Self.palette.randomElement() ?? WTheme.accent,
                round: Bool.random() && Bool.random()
            )
        }
        start = .now
        Task {
            try? await Task.sleep(for: .seconds(2.1))
            start = nil
        }
    }
}

/// A grouped-list section header: a coloured dot, the title, a count and a trailing note.
struct CoverSectionHeader: View {
    let title: String
    var count: Int?
    var color: Color?
    var trailing: String?
    var pulsing = false

    var body: some View {
        HStack(spacing: 8) {
            if let color {
                if pulsing {
                    LiveDot(color: color, size: 7)
                } else {
                    Circle().fill(color).frame(width: 8, height: 8)
                }
            }
            Text(title)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(WTheme.ink)
            if let count {
                Text("\(count)")
                    .font(.system(size: 13, weight: .semibold))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkTertiary)
                    .contentTransition(.numericText())
            }
            Spacer(minLength: 4)
            if let trailing {
                Text(trailing)
                    .font(.system(size: 13.5, weight: .medium))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkSecondary)
            }
        }
        .textCase(nil)
        .padding(.horizontal, 4)
        .padding(.bottom, 2)
    }
}

/// A row of filter chips with counts; the selected one is ink.
struct ChipFilterRow<Value: Hashable>: View {
    let options: [Value]
    @Binding var selection: Value
    let label: (Value) -> String
    var count: (Value) -> Int? = { _ in nil }
    var alert: (Value) -> Bool = { _ in false }

    @Namespace private var space

    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 8) {
                ForEach(options, id: \.self) { option in
                    let selected = option == selection
                    Button {
                        withAnimation(.spring(response: 0.38, dampingFraction: 0.82)) { selection = option }
                    } label: {
                        HStack(spacing: 6) {
                            if alert(option) {
                                Circle().fill(WTheme.negative).frame(width: 6, height: 6)
                            }
                            Text(label(option))
                                .font(.system(size: 14, weight: selected ? .semibold : .medium))
                            if let value = count(option) {
                                Text("\(value)")
                                    .font(.system(size: 13, weight: .semibold))
                                    .monospacedDigit()
                                    .foregroundStyle(selected ? WTheme.inkInverse.opacity(0.7) : WTheme.inkTertiary)
                                    .contentTransition(.numericText())
                            }
                        }
                        .foregroundStyle(selected ? WTheme.inkInverse : WTheme.ink)
                        .padding(.horizontal, 14)
                        .frame(height: 36)
                        .background {
                            if selected {
                                Capsule().fill(WTheme.ink).matchedGeometryEffect(id: "chip", in: space)
                            } else {
                                Capsule().fill(GroupedSurface.card)
                            }
                        }
                    }
                    .buttonStyle(TapScaleStyle())
                    .accessibilityAddTraits(selected ? .isSelected : [])
                }
            }
        }
        .scrollIndicators(.hidden)
        .scrollClipDisabled()
        .sensoryFeedback(.selection, trigger: selection)
    }
}
