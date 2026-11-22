import SwiftUI

// Small shared pieces used across the More tab screens.

/// An iOS grouped card: rows stacked with inset hairlines between them.
struct MoreCard<Content: View>: View {
    /// Where the hairline starts: row inset + tile + gap.
    var dividerLeading: CGFloat = 58
    @ViewBuilder var content: Content

    var body: some View {
        VStack(spacing: 0) {
            Group(subviews: content) { subviews in
                ForEach(subviews) { subview in
                    subview
                    if subview.id != subviews.last?.id {
                        Hairline().padding(.leading, dividerLeading)
                    }
                }
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
        .surfaceCard(in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }
}

/// Row press feedback inside a card: a quiet fill, no scale.
struct MoreRowPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(configuration.isPressed ? WTheme.fill : Color.clear)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}

/// Tiny role chip tinted by the role's custom color.
struct MoreRoleChipView: View {
    let name: String
    let color: Color

    var body: some View {
        Text(name)
            .font(.system(size: 10.5, weight: .medium))
            .foregroundStyle(color)
            .padding(.horizontal, 7)
            .padding(.vertical, 2.5)
            .background(color.opacity(0.1), in: Capsule())
            .overlay(Capsule().strokeBorder(color.opacity(0.18), lineWidth: 1))
            .lineLimit(1)
    }
}
