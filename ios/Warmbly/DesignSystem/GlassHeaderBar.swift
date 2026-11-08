import SwiftUI

/// The tab header every list screen shares: a glass menu button, the scope as
/// a title with one line under it, and a search button that morphs into the
/// field. `trailing` holds the screen's own glass buttons (new campaign, …).
struct GlassHeaderBar<Trailing: View>: View {
    let title: String
    var subtitle: Text?
    @Binding var query: String
    var searchPrompt: String
    var searchFocused: FocusState<Bool>.Binding
    var menuLabel = "Open menu"
    let onMenu: () -> Void
    @ViewBuilder var trailing: () -> Trailing

    @State private var searchOpen = false
    @Namespace private var glass
    @Environment(\.drawerPinned) private var drawerPinned

    var body: some View {
        GlassEffectContainer(spacing: 10) {
            HStack(spacing: 10) {
                if searchOpen {
                    HStack(spacing: 8) {
                        Image(systemName: "magnifyingglass")
                            .font(.system(size: 15, weight: .medium))
                            .foregroundStyle(WTheme.inkSecondary)
                        TextField(text: $query) {
                            Text(searchPrompt).foregroundStyle(WTheme.inkTertiary)
                        }
                        .font(.system(size: 16))
                        .foregroundStyle(WTheme.ink)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .submitLabel(.search)
                        .focused(searchFocused)
                        if !query.isEmpty {
                            Button {
                                query = ""
                            } label: {
                                Image(systemName: "xmark.circle.fill")
                                    .font(.system(size: 16))
                                    .foregroundStyle(WTheme.inkTertiary)
                            }
                            .buttonStyle(.plain)
                            .accessibilityLabel("Clear search")
                        }
                    }
                    .padding(.horizontal, 15)
                    .frame(height: 44)
                    .glassEffect(.regular.interactive(), in: Capsule())
                    .glassEffectID("search", in: glass)

                    GlassCircleButton(symbol: "xmark", label: "Close search") { close() }
                        .glassEffectID("menu", in: glass)
                } else {
                    if !drawerPinned {
                        GlassCircleButton(symbol: "line.3.horizontal", label: menuLabel, action: onMenu)
                            .glassEffectID("menu", in: glass)
                    }

                    VStack(alignment: .leading, spacing: 1) {
                        Text(title)
                            .font(.system(size: 19, weight: .semibold))
                            .tracking(-0.3)
                            .foregroundStyle(WTheme.ink)
                            .lineLimit(1)
                            .contentTransition(.opacity)
                        if let subtitle {
                            subtitle
                                .font(.system(size: 13))
                                .foregroundStyle(WTheme.inkSecondary)
                                .monospacedDigit()
                                .lineLimit(1)
                                .contentTransition(.numericText())
                        }
                    }
                    .padding(.leading, 2)

                    Spacer(minLength: 6)
                    PresenceAvatars(maxShown: 3)
                    GlassCircleButton(symbol: "magnifyingglass", label: "Search") { open() }
                        .glassEffectID("search", in: glass)
                    trailing()
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 6)
        .padding(.bottom, 8)
        .animation(.spring(response: 0.42, dampingFraction: 0.86), value: searchOpen)
        .onAppear {
            // A search still in the field reopens it.
            if !query.isEmpty { searchOpen = true }
        }
    }

    private func open() {
        withAnimation(.spring(response: 0.42, dampingFraction: 0.86)) { searchOpen = true }
        Task {
            try? await Task.sleep(for: .milliseconds(120))
            searchFocused.wrappedValue = true
        }
    }

    private func close() {
        query = ""
        searchFocused.wrappedValue = false
        withAnimation(.spring(response: 0.42, dampingFraction: 0.86)) { searchOpen = false }
    }
}

extension GlassHeaderBar where Trailing == EmptyView {
    init(
        title: String,
        subtitle: Text? = nil,
        query: Binding<String>,
        searchPrompt: String,
        searchFocused: FocusState<Bool>.Binding,
        menuLabel: String = "Open menu",
        onMenu: @escaping () -> Void
    ) {
        self.init(
            title: title,
            subtitle: subtitle,
            query: query,
            searchPrompt: searchPrompt,
            searchFocused: searchFocused,
            menuLabel: menuLabel,
            onMenu: onMenu,
            trailing: { EmptyView() }
        )
    }
}

/// A 44pt Liquid Glass circle with one symbol.
struct GlassCircleButton: View {
    let symbol: String
    let label: String
    var tint: Color = WTheme.ink
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 16, weight: .medium))
                .foregroundStyle(tint)
                .frame(width: 44, height: 44)
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .glassEffect(.regular.interactive(), in: Circle())
        .accessibilityLabel(label)
    }
}

/// The screen's primary action in the header: an ink circle with one symbol.
struct HeaderPrimaryButton: View {
    let symbol: String
    let label: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(WTheme.inkInverse)
                .frame(width: 44, height: 44)
                .background(WTheme.ink, in: Circle())
                .contentShape(Circle())
        }
        .buttonStyle(TapScaleStyle())
        .accessibilityLabel(label)
    }
}

/// Under the header while searching: how many results matched.
struct SearchResultsLine: View {
    let count: Int
    var more = false

    var body: some View {
        Text("\(count)\(more ? "+" : "") \(count == 1 ? "result" : "results")")
            .font(.system(size: 13.5, weight: .medium))
            .monospacedDigit()
            .foregroundStyle(WTheme.inkSecondary)
            .contentTransition(.numericText())
            .padding(.horizontal, 20)
            .padding(.top, 4)
            .padding(.bottom, 6)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}
