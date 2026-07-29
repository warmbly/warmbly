import SwiftUI

// MARK: - Tab switching from embedded screens

/// Lets Home's cards jump straight to a tab (campaigns, inbox) without
/// plumbing bindings through every layer.
struct SwitchTabKey: EnvironmentKey {
    static let defaultValue: @MainActor @Sendable (AppTab) -> Void = { _ in }
}

extension EnvironmentValues {
    var switchTab: @MainActor @Sendable (AppTab) -> Void {
        get { self[SwitchTabKey.self] }
        set { self[SwitchTabKey.self] = newValue }
    }
}

// MARK: - Cards

/// The in-app surface: a soft rounded card on the grouped background.
struct AirCardModifier: ViewModifier {
    var padding: CGFloat = 16

    func body(content: Content) -> some View {
        content
            .padding(padding)
            .background(
                Color(.secondarySystemGroupedBackground),
                in: RoundedRectangle(cornerRadius: 22, style: .continuous)
            )
            .shadow(color: .black.opacity(0.045), radius: 14, y: 5)
    }
}

extension View {
    func airCard(padding: CGFloat = 16) -> some View {
        modifier(AirCardModifier(padding: padding))
    }
}

/// Section heading above a card: bold title, optional trailing action.
struct AirSectionHeader: View {
    let title: String
    var actionTitle: String?
    var action: (() -> Void)?

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title)
                .font(.title3.weight(.bold))
            Spacer()
            if let actionTitle, let action {
                Button(action: action) {
                    HStack(spacing: 3) {
                        Text(actionTitle)
                        Image(systemName: "chevron.right")
                            .font(.system(size: 11, weight: .semibold))
                    }
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(WTheme.accent)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, 4)
    }
}

// MARK: - Icon tile

/// Rounded-square tinted symbol tile that leads list rows and quick links.
struct IconTile: View {
    let symbol: String
    var tone: Tone = .sky
    var size: CGFloat = 38

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: size * 0.44, weight: .semibold))
            .foregroundStyle(tone.color)
            .frame(width: size, height: size)
            .background(tone.background, in: RoundedRectangle(cornerRadius: size * 0.32, style: .continuous))
    }
}

// MARK: - Health ring

/// Small circular score gauge (mailbox health), animated on appearance.
struct HealthRing: View {
    let score: Int?
    var tone: Tone = .emerald
    var size: CGFloat = 34

    @State private var shown = false

    private var fraction: Double {
        guard let score else { return 0 }
        return min(1, max(0.04, Double(score) / 100))
    }

    var body: some View {
        ZStack {
            Circle()
                .stroke(tone.color.opacity(0.18), lineWidth: 3.5)
            Circle()
                .trim(from: 0, to: shown ? fraction : 0)
                .stroke(tone.color, style: StrokeStyle(lineWidth: 3.5, lineCap: .round))
                .rotationEffect(.degrees(-90))
            if let score {
                Text("\(score)")
                    .font(.system(size: size * 0.34, weight: .semibold))
                    .monospacedDigit()
                    .foregroundStyle(tone.color)
            } else {
                Image(systemName: "minus")
                    .font(.system(size: size * 0.3, weight: .semibold))
                    .foregroundStyle(.tertiary)
            }
        }
        .frame(width: size, height: size)
        .onAppear {
            withAnimation(.spring(response: 0.9, dampingFraction: 0.85).delay(0.15)) {
                shown = true
            }
        }
    }
}

// MARK: - Hero stat chip (white-on-sky)

/// Glass stat chip used on sky hero headers, matching the auth showcase glass.
struct AirStatChip: View {
    let value: String
    let label: String
    var symbol: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 5) {
                if let symbol {
                    Image(systemName: symbol)
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(.white.opacity(0.85))
                }
                Text(value)
                    .font(.system(size: 21, weight: .bold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(.white)
                    .contentTransition(.numericText())
            }
            Text(label)
                .font(.system(size: 11.5, weight: .medium))
                .foregroundStyle(.white.opacity(0.72))
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .background(.white.opacity(0.14), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(
                    LinearGradient(
                        colors: [.white.opacity(0.38), .white.opacity(0.06)],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    ),
                    lineWidth: 1
                )
        )
    }
}

// MARK: - Detail scaffold (sky hero + sticky pill tabs)

/// One tab in an AirPillTabBar.
struct AirTabItem: Identifiable, Hashable {
    let id: String
    let title: String
    let icon: String
}

/// Flat full-width tab bar in the dashboard's underline language: every tab
/// gets an equal share (no horizontal scrolling or cut-off tabs), the active
/// one is bold with an accent underline that slides between tabs.
struct AirUnderlineTabBar: View {
    let tabs: [AirTabItem]
    @Binding var selection: String
    @Namespace private var underlineNS

    var body: some View {
        HStack(spacing: 0) {
            ForEach(tabs) { tab in
                tabButton(tab)
            }
        }
        .padding(.horizontal, 8)
        .overlay(alignment: .bottom) { Divider() }
        .sensoryFeedback(.selection, trigger: selection)
    }

    private func tabButton(_ tab: AirTabItem) -> some View {
        let selected = tab.id == selection
        return Button {
            withAnimation(.spring(response: 0.35, dampingFraction: 0.82)) {
                selection = tab.id
            }
        } label: {
            VStack(spacing: 0) {
                HStack(spacing: 5) {
                    Image(systemName: tab.icon)
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(selected ? WTheme.accent : Color.secondary)
                    Text(tab.title)
                        .font(.system(size: 13.5, weight: selected ? .semibold : .regular))
                        .foregroundStyle(selected ? Color.primary : .secondary)
                        .lineLimit(1)
                        .minimumScaleFactor(0.8)
                }
                .frame(maxWidth: .infinity)
                .frame(height: 46)
                ZStack {
                    if selected {
                        Capsule()
                            .fill(WTheme.accent)
                            .frame(height: 3)
                            .matchedGeometryEffect(id: "air-underline", in: underlineNS)
                    } else {
                        Color.clear.frame(height: 3)
                    }
                }
                .padding(.horizontal, 10)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// Detail-screen scaffold: sky hero fixed at the top (behind a transparent
/// nav bar), a rounded white sheet whose underline tab bar stays pinned, and
/// the tab content swiping horizontally underneath (Gmail-profile style). Tab
/// contents keep their own Lists, scrolling, and swipe actions. The sheet is
/// clipped so scrolling content never bleeds over its rounded corners.
struct AirDetailScaffold<Hero: View, Content: View>: View {
    let tabs: [AirTabItem]
    @Binding var selection: String
    @ViewBuilder var hero: Hero
    @ViewBuilder var content: Content

    private var sheetShape: UnevenRoundedRectangle {
        UnevenRoundedRectangle(topLeadingRadius: 26, topTrailingRadius: 26, style: .continuous)
    }

    var body: some View {
        VStack(spacing: 0) {
            hero
                .frame(maxWidth: .infinity, alignment: .leading)
            VStack(spacing: 0) {
                AirUnderlineTabBar(tabs: tabs, selection: $selection)
                content
            }
            .clipShape(sheetShape)
            .background(
                sheetShape
                    .fill(Color(.systemBackground))
                    .ignoresSafeArea(edges: .bottom)
                    .shadow(color: .black.opacity(0.12), radius: 18, y: -4)
            )
        }
        .background(alignment: .top) {
            AirSkyWash().ignoresSafeArea(edges: .top)
        }
        .toolbarBackground(.hidden, for: .navigationBar)
        .toolbarColorScheme(.dark, for: .navigationBar)
    }
}

/// Lightweight sky slice for detail heros: the brand radial with a soft bloom,
/// cheaper than the full animated SkyBackdrop.
struct AirSkyWash: View {
    var body: some View {
        ZStack {
            RadialGradient(
                stops: [
                    .init(color: Color(hex: 0x7DD3FC), location: 0),
                    .init(color: Color(hex: 0x38BDF8), location: 0.34),
                    .init(color: Color(hex: 0x0EA5E9), location: 0.66),
                    .init(color: Color(hex: 0x0284C7), location: 1),
                ],
                center: UnitPoint(x: 0.55, y: 0.05),
                startRadius: 0,
                endRadius: 560
            )
            RadialGradient(
                stops: [
                    .init(color: Color(hex: 0xBAE6FD).opacity(0.55), location: 0),
                    .init(color: .clear, location: 0.6),
                ],
                center: UnitPoint(x: 0.6, y: 0.1),
                startRadius: 0,
                endRadius: 420
            )
        }
    }
}

// MARK: - Web handoff

/// Points at the web dashboard for work that needs a big screen (sequence
/// editing, sender assignment, advanced tuning). Informational, not a link:
/// there is deliberately no in-app browser for editing flows.
struct WebHandoffBanner: View {
    let text: String

    var body: some View {
        HStack(alignment: .top, spacing: 11) {
            Image(systemName: "macbook")
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(WTheme.accent)
                .frame(width: 30, height: 30)
                .background(Tone.sky.background, in: RoundedRectangle(cornerRadius: 9, style: .continuous))
            VStack(alignment: .leading, spacing: 2) {
                Text("Best on a big screen")
                    .font(.footnote.weight(.semibold))
                Text(text)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
        .padding(12)
        .background(Color(.secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .strokeBorder(Tone.sky.color.opacity(0.18), lineWidth: 1)
        )
    }
}

// MARK: - Press feedback

/// Gentle press-down scale for tappable cards and rows.
struct TapScaleStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .opacity(configuration.isPressed ? 0.92 : 1)
            .animation(.spring(response: 0.28, dampingFraction: 0.8), value: configuration.isPressed)
    }
}
