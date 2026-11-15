import SwiftUI

/// The header a settings page draws over its list column while its open item
/// sits beside it and the navigation bar is hidden: back, title, actions and
/// an optional search field.
struct SplitListHeader<Trailing: View>: View {
    let title: String
    var subtitle: Text?
    var query: Binding<String>?
    var searchPrompt = "Search"
    @ViewBuilder var trailing: () -> Trailing

    @Environment(\.isSidebarDestination) private var isSidebarDestination
    @Environment(\.dismiss) private var dismiss
    @FocusState private var searchFocused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            GlassEffectContainer(spacing: 10) {
                HStack(spacing: 10) {
                    // A sidebar destination is a root; anywhere else the page was pushed.
                    if !isSidebarDestination {
                        GlassCircleButton(symbol: "chevron.left", label: "Back") { dismiss() }
                    }
                    VStack(alignment: .leading, spacing: 1) {
                        Text(title)
                            .font(.system(size: 19, weight: .semibold))
                            .tracking(-0.3)
                            .foregroundStyle(WTheme.ink)
                            .lineLimit(1)
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
                    trailing()
                }
            }
            if let query {
                searchField(query)
            }
        }
        .padding(.horizontal, 16)
        .padding(.top, 6)
        .padding(.bottom, 8)
    }

    private func searchField(_ query: Binding<String>) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(WTheme.inkTertiary)
            TextField(text: query) {
                Text(searchPrompt).foregroundStyle(WTheme.inkTertiary)
            }
            .font(.system(size: 15.5))
            .foregroundStyle(WTheme.ink)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .submitLabel(.search)
            .focused($searchFocused)
            if !query.wrappedValue.isEmpty {
                Button {
                    query.wrappedValue = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 15))
                        .foregroundStyle(WTheme.inkTertiary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
            }
        }
        .padding(.horizontal, 13)
        .frame(height: 38)
        .background(WTheme.fill, in: Capsule())
        .contentShape(Capsule())
        .onTapGesture { searchFocused = true }
    }
}

extension SplitListHeader where Trailing == EmptyView {
    init(title: String, subtitle: Text? = nil, query: Binding<String>? = nil, searchPrompt: String = "Search") {
        self.init(title: title, subtitle: subtitle, query: query, searchPrompt: searchPrompt, trailing: { EmptyView() })
    }
}
