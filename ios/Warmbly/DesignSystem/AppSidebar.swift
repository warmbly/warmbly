import SwiftUI

struct AppSidebarItem: Identifiable {
    let tab: AppTab
    let title: String
    let symbol: String
    var badge: Int = 0

    var id: AppTab { tab }
}

struct AppSidebarSection: Identifiable {
    let id: String
    var title: String?
    let items: [AppSidebarItem]
}

/// The iPad's navigation, built from the drawers' parts so it reads as the
/// same app: the workspace header, the pages as rows under one sliding tile,
/// and Remie and the account at the foot.
struct AppSidebar: View {
    @Binding var selection: AppTab
    let sections: [AppSidebarSection]
    let showsAssistant: Bool
    let topInset: CGFloat
    let onCollapse: () -> Void

    @Environment(AppEnvironment.self) private var env
    @Namespace private var activeNS

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            DrawerWorkspaceHeader(topInset: topInset)
                .padding(.trailing, 26)
                .overlay(alignment: .bottomTrailing) {
                    Button(action: onCollapse) {
                        Image(systemName: "sidebar.leading")
                            .font(.system(size: 16, weight: .medium))
                            .foregroundStyle(WTheme.inkSecondary)
                            .frame(width: 40, height: 44)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(TapScaleStyle())
                    .hoverEffect(.highlight)
                    .padding(.trailing, 12)
                    .padding(.bottom, 2)
                    .accessibilityLabel("Hide sidebar")
                }
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(sections) { section in
                        if let title = section.title {
                            Text(title)
                                .font(.system(size: 14, weight: .semibold))
                                .foregroundStyle(WTheme.inkSecondary)
                                .padding(.horizontal, 14)
                                .padding(.top, 18)
                                .padding(.bottom, 4)
                        }
                        ForEach(section.items) { item in
                            row(item)
                        }
                    }
                }
                .padding(.horizontal, 12)
                .padding(.top, 8)
                .padding(.bottom, 16)
            }
            .scrollIndicators(.hidden)
            footer
        }
        .background(WTheme.canvas.ignoresSafeArea())
        .animation(.spring(response: 0.32, dampingFraction: 0.82), value: selection)
        .sensoryFeedback(.selection, trigger: selection)
    }

    private func row(_ item: AppSidebarItem) -> some View {
        let selected = item.tab == selection
        return Button {
            selection = item.tab
        } label: {
            HStack(spacing: 13) {
                Image(systemName: item.symbol)
                    .font(.system(size: 16))
                    .symbolVariant(selected ? .fill : .none)
                    .foregroundStyle(selected ? WTheme.ink : WTheme.inkSecondary)
                    .frame(width: 24)
                Text(item.title)
                    .font(.system(size: 16, weight: selected ? .medium : .regular))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                Spacer(minLength: 8)
                if item.badge > 0 {
                    Text(WFormat.compact(item.badge))
                        .font(.system(size: 14, weight: .semibold))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.accent)
                        .contentTransition(.numericText(value: Double(item.badge)))
                }
            }
            .padding(.horizontal, 14)
            .frame(height: 42)
            .background {
                if selected {
                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                        .fill(WTheme.fill)
                        .matchedGeometryEffect(id: "sidebar-active", in: activeNS)
                }
            }
            .contentShape(.hoverEffect, RoundedRectangle(cornerRadius: 12, style: .continuous))
            .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
        }
        .buttonStyle(TapScaleStyle())
        .hoverEffect(.highlight)
        .accessibilityLabel(item.title)
        .accessibilityValue(item.badge > 0 ? "\(item.badge) unread" : "")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var footer: some View {
        VStack(spacing: 10) {
            if showsAssistant {
                Button {
                    selection = .assistant
                } label: {
                    HStack(spacing: 10) {
                        RemieMark(size: 22)
                        Text("Ask Remie")
                            .font(.system(size: 15, weight: .medium))
                            .foregroundStyle(selection == .assistant ? WTheme.ink : WTheme.inkSecondary)
                        Spacer(minLength: 4)
                        Text("⌘J")
                            .font(WFont.meta(12, weight: .medium))
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                    .padding(.horizontal, 14)
                    .frame(height: 44)
                    .glassEffect(.regular.tint(WTheme.glassTint).interactive(), in: Capsule())
                    .contentShape(Capsule())
                }
                .buttonStyle(TapScaleStyle())
                .accessibilityLabel("Ask Remie")
                .accessibilityAddTraits(selection == .assistant ? .isSelected : [])
            }
            account
        }
        .padding(.horizontal, 12)
        .padding(.top, 10)
        .padding(.bottom, 12)
        .overlay(alignment: .top) {
            Rectangle().fill(WTheme.hairline).frame(height: 1)
        }
    }

    private var account: some View {
        let selected = selection == .more
        let user = env.session.user
        return Button {
            selection = .more
        } label: {
            HStack(spacing: 11) {
                WAvatar(name: user?.displayName ?? "You", imageURL: user?.avatarURL, seed: user?.id ?? "me", size: 32)
                VStack(alignment: .leading, spacing: 1) {
                    Text(user?.displayName ?? "Account")
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                    Text(user?.email ?? "")
                        .font(.system(size: 12.5))
                        .foregroundStyle(WTheme.inkTertiary)
                        .lineLimit(1)
                }
                Spacer(minLength: 6)
                Image(systemName: "gearshape")
                    .font(.system(size: 15))
                    .symbolVariant(selected ? .fill : .none)
                    .foregroundStyle(selected ? WTheme.ink : WTheme.inkTertiary)
            }
            .padding(.horizontal, 10)
            .frame(height: 52)
            .background {
                if selected {
                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                        .fill(WTheme.fill)
                        .matchedGeometryEffect(id: "sidebar-active", in: activeNS)
                }
            }
            .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
        }
        .buttonStyle(TapScaleStyle())
        .hoverEffect(.highlight)
        .accessibilityLabel("Settings")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}
