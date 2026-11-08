import SwiftUI

// MARK: - Visibility

/// Screens that need the whole height (an open drawer, selection mode) hide
/// the tab bar through this, keyed per screen so one can't unhide another's.
@MainActor
@Observable
final class TabBarState {
    private(set) var hiddenBy: Set<String> = []
    private(set) var keyboardVisible = false

    var isHidden: Bool { !hiddenBy.isEmpty || keyboardVisible }

    func set(_ hidden: Bool, by key: String) {
        if hidden { hiddenBy.insert(key) } else { hiddenBy.remove(key) }
    }

    func setKeyboard(_ visible: Bool) {
        keyboardVisible = visible
    }

    /// A screen's own round action, replacing the default while it's on screen.
    private(set) var actions: [String: TabBarAction] = [:]
    private var actionOrder: [String] = []

    var screenAction: TabBarAction? {
        actionOrder.last.flatMap { actions[$0] }
    }

    func setAction(_ action: TabBarAction?, by key: String) {
        actionOrder.removeAll { $0 == key }
        if let action {
            actions[key] = action
            actionOrder.append(key)
        } else {
            actions[key] = nil
        }
    }
}

private struct TabBarHiddenModifier: ViewModifier {
    @Environment(TabBarState.self) private var state: TabBarState?
    let hidden: Bool
    @State private var key = UUID().uuidString

    func body(content: Content) -> some View {
        content
            .onAppear { apply(hidden) }
            .onChange(of: hidden) { _, value in apply(value) }
            .onDisappear { apply(false) }
    }

    private func apply(_ value: Bool) {
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) {
            state?.set(value, by: key)
        }
    }
}

private struct TabBarActionModifier: ViewModifier {
    @Environment(TabBarState.self) private var state: TabBarState?
    let action: TabBarAction
    let enabled: Bool
    @State private var key = UUID().uuidString

    func body(content: Content) -> some View {
        content
            .onAppear { apply(enabled) }
            .onChange(of: enabled) { _, value in apply(value) }
            .onDisappear { apply(false) }
    }

    private func apply(_ value: Bool) {
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) {
            state?.setAction(value ? action : nil, by: key)
        }
    }
}

extension View {
    /// Puts this screen's primary action in the tab bar's round button while on screen.
    func tabBarAction(_ title: String, symbol: String, enabled: Bool = true, perform: @escaping () -> Void) -> some View {
        modifier(TabBarActionModifier(action: TabBarAction(title: title, symbol: symbol, perform: perform), enabled: enabled))
    }

    /// Hides the app's tab bar while `hidden` is true and this view is on screen.
    func tabBarHidden(_ hidden: Bool) -> some View {
        modifier(TabBarHiddenModifier(hidden: hidden))
    }
}

// MARK: - Bar

struct WTabItem: Identifiable {
    let tab: AppTab
    let title: String
    let symbol: String
    var badge: Int = 0

    var id: AppTab { tab }
}

/// The app's own tab bar, Linear-style on Liquid Glass: a compact glass
/// capsule floating off the bottom edge, icon-only and monochrome, the active
/// tab on a soft tile that slides between items, and one round glass action
/// beside it that the bar merges with as they meet.
struct WTabBar: View {
    let items: [WTabItem]
    @Binding var selection: AppTab
    var action: TabBarAction?
    var onReselect: (AppTab) -> Void = { _ in }

    @Namespace private var activeNS
    @Namespace private var glassNS

    var body: some View {
        GlassEffectContainer(spacing: 14) {
            HStack(spacing: 10) {
                HStack(spacing: 2) {
                    ForEach(items) { item in
                        button(item)
                    }
                }
                .padding(5)
                .background(WTheme.glassTint, in: Capsule())
                .glassEffect(.regular.tint(WTheme.glassTint).interactive(), in: Capsule())
                .glassEffectID("bar", in: glassNS)

                if let action {
                    Button(action: action.perform) {
                        Group {
                            if action.mark {
                                RemieMark(size: 26)
                                    .transition(.scale(scale: 0.5).combined(with: .opacity))
                            } else {
                                Image(systemName: action.symbol)
                                    .font(.system(size: 18, weight: .medium))
                                    .foregroundStyle(WTheme.ink)
                                    .transition(.scale(scale: 0.5).combined(with: .opacity))
                            }
                        }
                        .frame(width: 56, height: 56)
                        .contentShape(Circle())
                    }
                    .buttonStyle(TabPressStyle())
                    .background(WTheme.glassTint, in: Circle())
                    .glassEffect(.regular.tint(WTheme.glassTint).interactive(), in: Circle())
                    .glassEffectID("action", in: glassNS)
                    .keyboardShortcut("n", modifiers: .command)
                    .accessibilityLabel(action.title)
                }
            }
        }
        .padding(.horizontal, 18)
        .padding(.bottom, 4)
        .sensoryFeedback(.selection, trigger: selection)
    }

    private func button(_ item: WTabItem) -> some View {
        let selected = item.tab == selection
        return Button {
            if selected {
                onReselect(item.tab)
            } else {
                withAnimation(.spring(response: 0.32, dampingFraction: 0.82)) {
                    selection = item.tab
                }
            }
        } label: {
            Image(systemName: selected ? "\(item.symbol).fill" : item.symbol)
                .font(.system(size: 18, weight: selected ? .medium : .regular))
                .foregroundStyle(selected ? WTheme.ink : WTheme.inkTertiary)
                .contentTransition(.symbolEffect(.replace))
                .frame(maxWidth: .infinity)
                .frame(height: 46)
                .background {
                    if selected {
                        Capsule()
                            .fill(WTheme.ink.opacity(0.08))
                            .matchedGeometryEffect(id: "active", in: activeNS)
                    }
                }
                .overlay(alignment: .top) { TabBadge(count: item.badge).offset(x: 12, y: 4) }
                .contentShape(Capsule())
        }
        .buttonStyle(TabPressStyle())
        .accessibilityLabel(item.title)
        .accessibilityValue(item.badge > 0 ? "\(item.badge) unread" : "")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// The tab bar stood on its side for a wide window: the same glass capsule
/// and round action, stacked along the leading edge and centered on it, where
/// a thumb holding an unfolded phone or a tablet already rests.
struct WTabRail: View {
    let items: [WTabItem]
    @Binding var selection: AppTab
    var action: TabBarAction?
    var onReselect: (AppTab) -> Void = { _ in }

    @Namespace private var activeNS
    @Namespace private var glassNS

    var body: some View {
        GlassEffectContainer(spacing: 14) {
            VStack(spacing: 10) {
                VStack(spacing: 2) {
                    ForEach(items) { item in
                        button(item)
                    }
                }
                .padding(5)
                .background(WTheme.glassTint, in: Capsule())
                .glassEffect(.regular.tint(WTheme.glassTint).interactive(), in: Capsule())
                .glassEffectID("rail", in: glassNS)

                if let action {
                    Button(action: action.perform) {
                        Group {
                            if action.mark {
                                RemieMark(size: 24)
                            } else {
                                Image(systemName: action.symbol)
                                    .font(.system(size: 17, weight: .medium))
                                    .foregroundStyle(WTheme.ink)
                            }
                        }
                        .transition(.scale(scale: 0.5).combined(with: .opacity))
                        .frame(width: 56, height: 56)
                        .contentShape(Circle())
                    }
                    .buttonStyle(TabPressStyle())
                    .background(WTheme.glassTint, in: Circle())
                    .glassEffect(.regular.tint(WTheme.glassTint).interactive(), in: Circle())
                    .glassEffectID("action", in: glassNS)
                    .hoverEffect(.highlight)
                    .keyboardShortcut("n", modifiers: .command)
                    .accessibilityLabel(action.title)
                }
            }
        }
        .frame(width: ShellLayout.railWidth)
        .frame(maxHeight: .infinity)
        .sensoryFeedback(.selection, trigger: selection)
    }

    private func button(_ item: WTabItem) -> some View {
        let selected = item.tab == selection
        return Button {
            if selected {
                onReselect(item.tab)
            } else {
                withAnimation(.spring(response: 0.32, dampingFraction: 0.82)) {
                    selection = item.tab
                }
            }
        } label: {
            Image(systemName: selected ? "\(item.symbol).fill" : item.symbol)
                .font(.system(size: 18, weight: selected ? .medium : .regular))
                .foregroundStyle(selected ? WTheme.ink : WTheme.inkTertiary)
                .contentTransition(.symbolEffect(.replace))
                .frame(width: 46, height: 46)
                .background {
                    if selected {
                        Capsule()
                            .fill(WTheme.ink.opacity(0.08))
                            .matchedGeometryEffect(id: "active", in: activeNS)
                    }
                }
                .overlay(alignment: .topTrailing) { TabBadge(count: item.badge).offset(x: 3, y: 1) }
                .contentShape(Capsule())
        }
        .buttonStyle(TabPressStyle())
        .hoverEffect(.highlight)
        .help(item.title)
        .accessibilityLabel(item.title)
        .accessibilityValue(item.badge > 0 ? "\(item.badge) unread" : "")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// Command-number switches tabs on a hardware keyboard, like Mail and Safari.
/// Mounted on the shell, so it keeps working while a screen hides the bar.
struct TabShortcuts: View {
    let items: [WTabItem]
    @Binding var selection: AppTab

    var body: some View {
        ForEach(Array(items.prefix(9).enumerated()), id: \.element.id) { index, item in
            Button(item.title) { selection = item.tab }
                .keyboardShortcut(KeyEquivalent(Character("\(index + 1)")), modifiers: .command)
        }
        .opacity(0)
        .accessibilityHidden(true)
    }
}

struct TabBadge: View {
    let count: Int

    var body: some View {
        if count > 0 {
            Text(count > 99 ? "99+" : "\(count)")
                .font(WFont.meta(9.5, weight: .semibold))
                .foregroundStyle(.white)
                .padding(.horizontal, 4.5)
                .frame(minWidth: 16, minHeight: 16)
                .background(WTheme.accent, in: Capsule())
                .overlay(Capsule().strokeBorder(.white.opacity(0.9), lineWidth: 1.5))
                .contentTransition(.numericText())
                .transition(.scale.combined(with: .opacity))
        }
    }
}

/// The round control floating beside the tab bar.
struct TabBarAction {
    let title: String
    let symbol: String
    /// Shows Remie's mark instead of the symbol.
    var mark = false
    let perform: () -> Void
}

private struct TabPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.92 : 1)
            .animation(.spring(response: 0.25, dampingFraction: 0.7), value: configuration.isPressed)
    }
}
