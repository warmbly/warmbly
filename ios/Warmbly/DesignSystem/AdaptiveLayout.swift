import SwiftUI

// MARK: - Shell

/// Where the tab bar sits.
enum TabBarPlacement: Equatable {
    /// Floating off the bottom edge: iPhone, iPhone Duo unfolded in portrait,
    /// and an iPad window too narrow for the sidebar or with it hidden.
    case bottom
    /// No bar: the iPad's `AppSidebar` holds the navigation as a first column.
    case sidebar
    /// At the bottom of the strip iPhone Duo keeps down one side for the status
    /// bar and the system's own bars (the cover screen, and unfolded in
    /// landscape), where a system tab bar would be.
    case strip(HorizontalEdge)
}

/// How the window is laid out, following where the system puts its own bars.
struct ShellLayout: Equatable {
    var windowSize: CGSize = .zero
    var bar: TabBarPlacement = .bottom
    /// The width the shell takes from the content's leading side.
    var leadingInset: CGFloat = 0

    static let railWidth: CGFloat = 76
    static let railMinWindowWidth: CGFloat = 600
    static let sidebarWidth: CGFloat = 272
    /// Below this an iPad window keeps the list and its open item side by side
    /// only without the sidebar, so it takes the bottom bar instead.
    static let sidebarMinWindowWidth: CGFloat = 900

    @MainActor
    static func resolve(_ size: CGSize, safeArea: EdgeInsets, systemBarEdge: HorizontalEdge?, sidebarHidden: Bool = false) -> ShellLayout {
        var bar: TabBarPlacement = .bottom
        if let systemBarEdge {
            bar = .strip(systemBarEdge)
        } else if isPad, !sidebarHidden, size.width >= sidebarMinWindowWidth {
            bar = .sidebar
        }
        #if DEBUG
        bar = debugBarPlacement ?? bar
        #endif
        let leading: CGFloat = switch bar {
        case .sidebar: sidebarWidth + 1
        case .strip(.leading): max(safeArea.leading, railWidth)
        default: 0
        }
        return ShellLayout(windowSize: size, bar: bar, leadingInset: leading)
    }

    @MainActor
    static var isPad: Bool { UIDevice.current.userInterfaceIdiom == .pad }

}

/// Reports the edge where the system places its vertical bar on iPhone Duo,
/// and nil wherever its bars are horizontal.
struct VerticalBarEdgeReader: UIViewRepresentable {
    @Binding var edge: HorizontalEdge?

    /// The active scene's edge, so the first frame already has the bar in place.
    @MainActor
    static func current() -> HorizontalEdge? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let scene = scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
        return scene.flatMap { edge(in: $0.traitCollection) }
    }

    static func edge(in traits: UITraitCollection) -> HorizontalEdge? {
        // The trait ships in the iOS 27.1 SDK; older toolchains build without the strip.
        #if compiler(>=6.4.1)
        guard #available(iOS 27.1, *) else { return nil }
        switch traits.verticalBarEdge {
        case .leading: return .leading
        case .trailing: return .trailing
        default: return nil
        }
        #else
        return nil
        #endif
    }

    func makeUIView(context: Context) -> ReaderView {
        let view = ReaderView()
        view.isUserInteractionEnabled = false
        // Reported outside SwiftUI's update, which may be what moved this view into its window.
        view.onChange = { value in Task { @MainActor in edge = value } }
        return view
    }

    func updateUIView(_ view: ReaderView, context: Context) {}

    final class ReaderView: UIView {
        var onChange: ((HorizontalEdge?) -> Void)?
        private var reported: HorizontalEdge??

        override init(frame: CGRect) {
            super.init(frame: frame)
            #if compiler(>=6.4.1)
            if #available(iOS 27.1, *) {
                registerForTraitChanges(UITraitCollection.systemTraitsAffectingVerticalBarEdge) { (view: ReaderView, _) in
                    view.report()
                }
            }
            #endif
        }

        required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

        override func didMoveToWindow() {
            super.didMoveToWindow()
            report()
        }

        private func report() {
            guard window != nil else { return }
            let edge = VerticalBarEdgeReader.edge(in: traitCollection)
            guard reported != .some(edge) else { return }
            reported = .some(edge)
            onChange?(edge)
        }
    }
}

extension EnvironmentValues {
    @Entry var shellLayout = ShellLayout()
    /// Set on a detail shown beside its list: leaving it clears the selection
    /// instead of popping a stack it is not in.
    @Entry var closeDetail: (() -> Void)?
    /// The list tab's drawer is open as its own column, so headers drop the menu button.
    @Entry var drawerPinned = false
    /// Shown as a sidebar destination rather than a cover, so it has no close button.
    @Entry var isSidebarDestination = false
}

// MARK: - List and detail

/// Whether a list tab shows its detail beside the list, and whether its
/// drawer stays pinned open as a third column.
struct SplitMetrics: Equatable {
    let showsDetail: Bool
    let pinsSidebar: Bool
    let listWidth: CGFloat

    static let detailMinContentWidth: CGFloat = 720
    static let pinnedMinContentWidth: CGFloat = 1180

    init(contentWidth: CGFloat, shell: ShellLayout) {
        showsDetail = contentWidth >= Self.detailMinContentWidth
        pinsSidebar = contentWidth >= Self.pinnedMinContentWidth
        if pinsSidebar {
            listWidth = min(max(contentWidth * 0.3, 340), 400)
        } else {
            // Ends the list at the middle of the window, so on a book-style
            // foldable the list and the detail each sit on their own half.
            let toFold = shell.windowSize.width / 2 - shell.leadingInset
            listWidth = min(max(toFold, 320), 480)
        }
    }
}

/// The right-hand column of a list tab: the open item in its own stack, or a
/// quiet prompt to pick one.
struct DetailColumn<Item: Hashable, Detail: View>: View {
    let item: Item?
    let placeholderTitle: String
    let placeholderMessage: String
    let placeholderSymbol: String
    let onClose: (Item) -> Void
    @ViewBuilder let detail: (Item) -> Detail

    var body: some View {
        Group {
            if let item {
                NavigationStack {
                    detail(item)
                }
                .id(item)
                .environment(\.closeDetail, { onClose(item) })
                .transition(.opacity)
            } else {
                DetailPlaceholder(title: placeholderTitle, message: placeholderMessage, symbol: placeholderSymbol)
                    .transition(.opacity)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(WTheme.canvas)
        .animation(.snappy(duration: 0.22), value: item)
    }
}

struct DetailPlaceholder: View {
    let title: String
    let message: String
    let symbol: String

    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: symbol)
                .font(.system(size: 30, weight: .light))
                .foregroundStyle(WTheme.inkTertiary)
                .frame(width: 64, height: 64)
                .background(WTheme.fill, in: Circle())
            Text(title)
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(WTheme.ink)
            Text(message)
                .font(.system(size: 14))
                .foregroundStyle(WTheme.inkSecondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 280)
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(WTheme.canvas)
        .accessibilityElement(children: .combine)
    }
}

/// The frame every list tab shares: its list in a stack, the slide-in drawer,
/// and on a wide window the open item beside the list and, wider still, the
/// drawer pinned open as a first column.
struct ListDetailShell<Item: Hashable, ListContent: View, Drawer: View, Detail: View>: View {
    @Binding var path: [Item]
    @Binding var openItem: Item?
    @Binding var sidebarOpen: Bool
    @Binding var layout: SplitMetrics
    let sidebarWidth: CGFloat
    var drawerRadius: CGFloat = 20
    var dimOpacity: Double = 0.24
    let placeholder: DetailPlaceholder
    @ViewBuilder let list: () -> ListContent
    @ViewBuilder let drawer: (_ topInset: CGFloat, _ revealed: Bool) -> Drawer
    @ViewBuilder let detail: (Item) -> Detail

    @Environment(\.shellLayout) private var shell
    @State private var drag: CGFloat = 0

    var body: some View {
        GeometryReader { geo in
            let split = SplitMetrics(contentWidth: geo.size.width, shell: shell)
            ZStack(alignment: .leading) {
                HStack(spacing: 0) {
                    if split.pinsSidebar {
                        drawer(geo.safeAreaInsets.top, true)
                            .frame(width: sidebarWidth)
                            .frame(maxHeight: .infinity)
                            .background(WTheme.canvas)
                            .ignoresSafeArea(edges: .vertical)
                        VerticalHairline()
                    }
                    NavigationStack(path: $path) {
                        list()
                    }
                    .frame(width: split.showsDetail ? split.listWidth : nil)
                    .environment(\.drawerPinned, split.pinsSidebar)
                    if split.showsDetail {
                        VerticalHairline()
                        DetailColumn(
                            item: openItem,
                            placeholderTitle: placeholder.title,
                            placeholderMessage: placeholder.message,
                            placeholderSymbol: placeholder.symbol,
                            // An action finishing on an item the user already left must not close the newer one.
                            onClose: { item in if openItem == item { openItem = nil } },
                            detail: detail
                        )
                    }
                }
                .scaleEffect(sidebarOpen ? 0.97 : 1, anchor: .trailing)
                if !split.pinsSidebar {
                    if sidebarOpen {
                        Color.black.opacity(dimOpacity)
                            .ignoresSafeArea()
                            .transition(.opacity)
                            .onTapGesture { close() }
                    }
                    slideInDrawer(topInset: geo.safeAreaInsets.top)
                }
            }
            .splitHandoff(showsDetail: split.showsDetail, path: $path, selection: $openItem)
            .onAppear { layout = split }
            .onChange(of: split) { _, value in
                layout = value
                if value.pinsSidebar { close() }
            }
        }
    }

    private func slideInDrawer(topInset: CGFloat) -> some View {
        let shape = UnevenRoundedRectangle(bottomTrailingRadius: drawerRadius, topTrailingRadius: drawerRadius, style: .continuous)
        return drawer(topInset, sidebarOpen)
            .frame(width: sidebarWidth)
            .frame(maxHeight: .infinity)
            .background(WTheme.canvas)
            .clipShape(shape)
            .overlay(shape.strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
            // Vertical only: on a wide window the side rail holds the leading edge.
            .ignoresSafeArea(edges: .vertical)
            .offset(x: (sidebarOpen ? 0 : -sidebarWidth - 40 - shell.leadingInset) + drag)
            .gesture(
                DragGesture()
                    .onChanged { value in
                        drag = min(0, value.translation.width)
                    }
                    .onEnded { value in
                        if value.translation.width < -80 || value.predictedEndTranslation.width < -160 {
                            close()
                        } else {
                            withAnimation(.spring(response: 0.32, dampingFraction: 0.86)) { drag = 0 }
                        }
                    }
            )
    }

    private func close() {
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) {
            sidebarOpen = false
            drag = 0
        }
    }
}

extension SplitMetrics {
    /// Before the first layout pass: a phone-width stack.
    static let stacked = SplitMetrics(contentWidth: 0, shell: ShellLayout())
}

struct VerticalHairline: View {
    var body: some View {
        Rectangle()
            .fill(WTheme.hairlineStrong)
            .frame(width: 1)
            .ignoresSafeArea()
    }
}

/// Moves a list tab's open item between the stack (narrow) and the detail
/// column (wide) when the window changes size, so a rotation, a fold or a
/// Stage Manager resize keeps what was open.
struct SplitHandoff<Item: Hashable>: ViewModifier {
    let showsDetail: Bool
    @Binding var path: [Item]
    @Binding var selection: Item?

    func body(content: Content) -> some View {
        content
            .onAppear { sync(showsDetail) }
            .onChange(of: showsDetail) { _, wide in sync(wide) }
    }

    private func sync(_ wide: Bool) {
        if wide {
            if let first = path.first {
                selection = first
                path.removeAll()
            }
        } else if let selection {
            path = [selection]
            self.selection = nil
        }
    }
}

extension View {
    func splitHandoff<Item: Hashable>(showsDetail: Bool, path: Binding<[Item]>, selection: Binding<Item?>) -> some View {
        modifier(SplitHandoff(showsDetail: showsDetail, path: path, selection: selection))
    }

    /// Keeps a scroll view's or list's content at a comfortable reading width
    /// on a wide window, centered, while its background runs edge to edge.
    func readableMargins(_ maxWidth: CGFloat = 760) -> some View {
        modifier(ReadableMargins(maxWidth: maxWidth))
    }
}

struct ReadableMargins: ViewModifier {
    let maxWidth: CGFloat
    @State private var width: CGFloat = 0

    func body(content: Content) -> some View {
        // nil keeps the list's default margins; one branch keeps its scroll position across a resize.
        content
            .contentMargins(.horizontal, width > maxWidth ? (width - maxWidth) / 2 : nil, for: .scrollContent)
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
    }
}

// MARK: - Debug window

#if DEBUG
extension ShellLayout {
    /// WARMBLY_DEBUG_BARS=bottom|sidebar|leading|trailing places the tab bar as on
    /// another device, for a WARMBLY_DEBUG_FRAME run that cannot fold the host.
    static let debugBarPlacement: TabBarPlacement? = {
        switch ProcessInfo.processInfo.environment["WARMBLY_DEBUG_BARS"] {
        case "bottom": .bottom
        case "sidebar": .sidebar
        case "leading": .strip(.leading)
        case "trailing": .strip(.trailing)
        default: nil
        }
    }()
}

/// WARMBLY_DEBUG_FRAME=<width>x<height> renders the app in a window of that
/// size, so a scripted run on an iPad simulator can show another device's
/// layout (951x669 is an unfolded iPhone Duo). It sets the size only: safe-area
/// insets and full-screen covers still come from the host, so check those on
/// the target simulator.
struct DebugWindowFrame: ViewModifier {
    private let size: CGSize? = {
        guard let raw = ProcessInfo.processInfo.environment["WARMBLY_DEBUG_FRAME"] else { return nil }
        let parts = raw.lowercased().split(separator: "x").compactMap { Double($0) }
        guard parts.count == 2, parts.allSatisfy({ $0.isFinite && $0 > 0 }) else { return nil }
        return CGSize(width: parts[0], height: parts[1])
    }()

    func body(content: Content) -> some View {
        if let size {
            GeometryReader { geo in
                let scale = min(geo.size.width / size.width, geo.size.height / size.height, 1)
                content
                    .frame(width: size.width, height: size.height)
                    .clipped()
                    .scaleEffect(scale)
                    .frame(width: geo.size.width, height: geo.size.height)
            }
            .background(Color.black.ignoresSafeArea())
        } else {
            content
        }
    }
}
#endif
