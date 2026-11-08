import SwiftUI

enum AppTab: String, Hashable {
    case home, inbox, campaigns, contacts, more
}

extension AppTab {
    /// The tab to open on launch: Home, or WARMBLY_DEBUG_TAB in scripted debug runs.
    static var launchTab: AppTab {
        #if DEBUG
        if let raw = ProcessInfo.processInfo.environment["WARMBLY_DEBUG_TAB"], let tab = AppTab(rawValue: raw) {
            return tab
        }
        #endif
        return .home
    }
}

/// Home is the workspace pulse and the front door to mailboxes/analytics;
/// Inbox carries the unread badge. Tabs the member lacks permission for are
/// hidden, like the web sidebar. The system tab bar is replaced by `WTabBar`
/// on a phone-width window and by the side `WTabRail` on a wide one.
struct MainTabView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var selection: AppTab = .launchTab
    @State private var tabBar = TabBarState()
    @State private var showAssistant = false

    private var canInbox: Bool { env.session.can(.accessUnibox) }
    private var canCampaigns: Bool { env.session.can(.viewCampaigns) }
    private var canContacts: Bool { env.session.can(.viewContacts) }
    private var canAssistant: Bool { env.session.can(.useAI) }

    /// The round button: the current screen's own action, else Remie.
    private var barAction: TabBarAction? {
        if let action = tabBar.screenAction { return action }
        guard canAssistant else { return nil }
        return TabBarAction(title: "Ask Remie", symbol: "sparkle", mark: true) { showAssistant = true }
    }

    /// Scripted debug runs open Remie on launch, optionally with a first prompt.
    private var debugPrompt: String? {
        #if DEBUG
        if let raw = ProcessInfo.processInfo.environment["WARMBLY_DEBUG_ASSISTANT"], !raw.isEmpty {
            return raw == "1" ? "" : raw
        }
        #endif
        return nil
    }

    private var items: [WTabItem] {
        var items = [WTabItem(tab: .home, title: "Home", symbol: "house")]
        if canInbox {
            items.append(WTabItem(tab: .inbox, title: "Inbox", symbol: "tray", badge: env.badges.uniboxUnread))
        }
        if canCampaigns {
            items.append(WTabItem(tab: .campaigns, title: "Campaigns", symbol: "paperplane"))
        }
        if canContacts {
            items.append(WTabItem(tab: .contacts, title: "Contacts", symbol: "person.2"))
        }
        items.append(WTabItem(tab: .more, title: "More", symbol: "square.grid.2x2"))
        return items
    }

    var body: some View {
        GeometryReader { geo in
            let shell = ShellLayout.resolve(geo.size)
            // A column, not a safe-area inset: the tab hosts don't pass a leading inset on to their content.
            HStack(spacing: 0) {
                if shell.usesRail {
                    WTabRail(items: items, selection: $selection, action: barAction)
                        .transition(.move(edge: .leading).combined(with: .opacity))
                    VerticalHairline()
                }
                tabs
            }
            .background { TabShortcuts(items: items, selection: $selection) }
            .background(WTheme.canvas.ignoresSafeArea())
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if !shell.usesRail, !tabBar.isHidden {
                    WTabBar(items: items, selection: $selection, action: barAction)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                }
            }
            .animation(.spring(response: 0.34, dampingFraction: 0.86), value: shell.usesRail)
            .environment(\.shellLayout, shell)
        }
        .animation(.spring(response: 0.34, dampingFraction: 0.86), value: tabBar.isHidden)
        .environment(tabBar)
        .fullScreenCover(isPresented: $showAssistant) {
            AssistantView(page: selection.rawValue, initialPrompt: debugPrompt.flatMap { $0.isEmpty ? nil : $0 })
        }
        .onAppear {
            if debugPrompt != nil { showAssistant = true }
        }
        .environment(\.switchTab) { tab in
            selection = tab
        }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in
            tabBar.setKeyboard(true)
        }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in
            tabBar.setKeyboard(false)
        }
    }

    private var tabs: some View {
        TabView(selection: $selection) {
            Tab("Home", systemImage: "house", value: AppTab.home) {
                HomeView().toolbarVisibility(.hidden, for: .tabBar)
            }
            if canInbox {
                Tab("Inbox", systemImage: "tray", value: AppTab.inbox) {
                    UniboxRootView().toolbarVisibility(.hidden, for: .tabBar)
                }
            }
            if canCampaigns {
                Tab("Campaigns", systemImage: "paperplane", value: AppTab.campaigns) {
                    CampaignsRootView().toolbarVisibility(.hidden, for: .tabBar)
                }
            }
            if canContacts {
                Tab("Contacts", systemImage: "person.2", value: AppTab.contacts) {
                    ContactsRootView().toolbarVisibility(.hidden, for: .tabBar)
                }
            }
            Tab("More", systemImage: "square.grid.2x2", value: AppTab.more) {
                MoreRootView().toolbarVisibility(.hidden, for: .tabBar)
            }
        }
        // The system's own iPad tab strip would sit above our bar or rail.
        .tabViewStyle(.tabBarOnly)
    }
}
