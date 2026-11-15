import SwiftUI

/// How settings-style pages draw their page and cards. iPhone keeps the grouped
/// look (a grey page under white cards, as its covers and sheets are); iPad
/// draws them on the same white canvas as Inbox and Home, so a page beside the
/// sidebar reads as part of the app rather than a sheet over it.
enum SurfaceStyle {
    case grouped, canvas
}

extension EnvironmentValues {
    @Entry var surfaceStyle: SurfaceStyle = .grouped
}

/// The page or card fill for the current `SurfaceStyle`.
struct GroupedSurface: ShapeStyle {
    let isCard: Bool

    static let page = GroupedSurface(isCard: false)
    static let card = GroupedSurface(isCard: true)

    func resolve(in environment: EnvironmentValues) -> Color.Resolved {
        let color: Color = switch (environment.surfaceStyle, isCard) {
        case (.grouped, false): WTheme.groupedPage
        case (.grouped, true): WTheme.groupedCard
        case (.canvas, false): WTheme.canvas
        case (.canvas, true): WTheme.canvasCard
        }
        return color.resolve(in: environment)
    }
}

extension View {
    /// A card's fill, with a hairline edge on the canvas where fill alone barely separates it.
    func surfaceCard<S: InsettableShape>(in shape: S) -> some View {
        modifier(SurfaceCardModifier(shape: shape))
    }

    /// Inline titles; on the canvas they lead, like the app's own headers.
    func surfaceTitleDisplayMode() -> some View {
        modifier(SurfaceTitleModifier())
    }
}

private struct SurfaceCardModifier<S: InsettableShape>: ViewModifier {
    @Environment(\.surfaceStyle) private var style
    let shape: S

    func body(content: Content) -> some View {
        content
            .background(GroupedSurface.card, in: shape)
            .overlay {
                if style == .canvas {
                    shape.strokeBorder(WTheme.hairline, lineWidth: 1)
                }
            }
    }
}

private struct SurfaceTitleModifier: ViewModifier {
    @Environment(\.surfaceStyle) private var style

    func body(content: Content) -> some View {
        if style == .canvas {
            content
                .navigationBarTitleDisplayMode(.inline)
                .toolbarRole(.editor)
        } else {
            content.navigationBarTitleDisplayMode(.inline)
        }
    }
}
