import SwiftUI

enum AppTab: String, Hashable {
    case home, inbox, campaigns, contacts, more
    /// Sidebar destinations on iPad; on iPhone these open from the settings hub.
    case deals, tasks, meetings, mailboxes, analytics, templates, team, activity, assistant

    /// Reached from the iPad sidebar only.
    var isSidebarOnly: Bool {
        switch self {
        case .home, .inbox, .campaigns, .contacts, .more: false
        default: true
        }
    }
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
/// hidden, like the web sidebar. The system tab bar is always hidden: the app
/// draws `AppSidebar` as the first column of a wide iPad window (holding the
/// CRM and workspace pages too), `WTabRail` in iPhone Duo's side strip, and
/// `WTabBar` along the bottom everywhere else.
struct MainTabView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var selection: AppTab = .launchTab
    @State private var tabBar = TabBarState()
    @State private var showAssistant = false
    @State private var systemBarEdge: HorizontalEdge? = VerticalBarEdgeReader.current()
    @AppStorage("ipad_sidebar_hidden") private var sidebarHidden = false

    private var canInbox: Bool { env.session.can(.accessUnibox) }
    private var canCampaigns: Bool { env.session.can(.viewCampaigns) }
    private var canContacts: Bool { env.session.can(.viewContacts) }
    private var canAssistant: Bool { env.session.can(.useAI) }
    private var canMailboxes: Bool { env.session.can(.manageEmails) }
    private var canAnalytics: Bool { env.session.can(.viewAnalytics) }

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

    /// Settings and Remie sit at the sidebar's foot, so they are not rows here.
    private var sidebarSections: [AppSidebarSection] {
        var main = [AppSidebarItem(tab: .home, title: "Home", symbol: "house")]
        if canInbox {
            main.append(AppSidebarItem(tab: .inbox, title: "Inbox", symbol: "tray", badge: env.badges.uniboxUnread))
        }
        if canCampaigns {
            main.append(AppSidebarItem(tab: .campaigns, title: "Campaigns", symbol: "paperplane"))
        }
        if canContacts {
            main.append(AppSidebarItem(tab: .contacts, title: "Contacts", symbol: "person.2"))
        }
        var sections = [AppSidebarSection(id: "main", items: main)]
        if canContacts {
            sections.append(AppSidebarSection(id: "sales", title: "Sales", items: [
                AppSidebarItem(tab: .deals, title: "Deals", symbol: "briefcase"),
                AppSidebarItem(tab: .tasks, title: "Tasks", symbol: "checklist"),
                AppSidebarItem(tab: .meetings, title: "Meetings", symbol: "calendar"),
            ]))
        }
        var workspace: [AppSidebarItem] = []
        if canMailboxes {
            workspace.append(AppSidebarItem(tab: .mailboxes, title: "Mailboxes", symbol: "envelope"))
        }
        if canAnalytics {
            workspace.append(AppSidebarItem(tab: .analytics, title: "Analytics", symbol: "chart.bar"))
        }
        if canCampaigns {
            workspace.append(AppSidebarItem(tab: .templates, title: "Templates", symbol: "doc.on.doc"))
        }
        workspace.append(AppSidebarItem(tab: .team, title: "Members", symbol: "person.3"))
        if canAnalytics {
            workspace.append(AppSidebarItem(tab: .activity, title: "Activity", symbol: "clock.arrow.circlepath"))
        }
        sections.append(AppSidebarSection(id: "workspace", title: "Workspace", items: workspace))
        return sections
    }

    var body: some View {
        GeometryReader { geo in
            let shell = ShellLayout.resolve(geo.size, safeArea: geo.safeAreaInsets, systemBarEdge: systemBarEdge, sidebarHidden: sidebarHidden)
            HStack(spacing: 0) {
                if shell.bar == .sidebar {
                    AppSidebar(
                        selection: $selection,
                        sections: sidebarSections,
                        showsAssistant: canAssistant,
                        topInset: geo.safeAreaInsets.top,
                        onCollapse: { setSidebarHidden(true) }
                    )
                    .frame(width: ShellLayout.sidebarWidth)
                    .ignoresSafeArea(edges: .vertical)
                    .transition(.move(edge: .leading).combined(with: .opacity))
                    Rectangle()
                        .fill(WTheme.hairline)
                        .frame(width: 1)
                        .ignoresSafeArea(edges: .vertical)
                }
                tabs
            }
            .padding(stripShortfall(shell, safeArea: geo.safeAreaInsets))
            .overlay { strip(shell, safeArea: geo.safeAreaInsets) }
            .background { TabShortcuts(items: items, selection: $selection) }
            .background { shortcuts(sidebar: shell.bar == .sidebar) }
            .background(WTheme.canvas.ignoresSafeArea())
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if shell.bar == .bottom, !tabBar.isHidden {
                    WTabBar(
                        items: items,
                        selection: $selection,
                        action: barAction,
                        onShowSidebar: canShowSidebar(geo.size) ? { setSidebarHidden(false) } : nil
                    )
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }
            }
            .animation(.spring(response: 0.34, dampingFraction: 0.86), value: shell.bar)
            .environment(\.shellLayout, shell)
            .environment(\.surfaceStyle, ShellLayout.isPad ? .canvas : .grouped)
        }
        .background(VerticalBarEdgeReader(edge: $systemBarEdge))
        .animation(.spring(response: 0.34, dampingFraction: 0.86), value: tabBar.isHidden)
        .environment(tabBar)
        .fullScreenCover(isPresented: $showAssistant) {
            AssistantView(page: selection.rawValue, initialPrompt: debugPrompt.flatMap { $0.isEmpty ? nil : $0 })
        }
        .onAppear {
            // The sidebar pages are not tabs on iPhone.
            if !ShellLayout.isPad, selection.isSidebarOnly { selection = .more }
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

    /// A hidden sidebar comes back from the bar, in an iPad window wide enough to hold it.
    private func canShowSidebar(_ size: CGSize) -> Bool {
        sidebarHidden && ShellLayout.isPad && size.width >= ShellLayout.sidebarMinWindowWidth
    }

    private func setSidebarHidden(_ hidden: Bool) {
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) { sidebarHidden = hidden }
    }

    /// Command-J asks Remie (its own page beside the sidebar, else over
    /// everything); Control-Command-S shows or hides the sidebar, as in Apple's apps.
    private func shortcuts(sidebar: Bool) -> some View {
        Group {
            if canAssistant {
                Button("Ask Remie") {
                    if sidebar { selection = .assistant } else { showAssistant = true }
                }
                .keyboardShortcut("j", modifiers: .command)
            }
            if ShellLayout.isPad {
                Button(sidebarHidden ? "Show Sidebar" : "Hide Sidebar") { setSidebarHidden(!sidebarHidden) }
                    .keyboardShortcut("s", modifiers: [.control, .command])
            }
        }
        .opacity(0)
        .accessibilityHidden(true)
    }

    /// The tab bar in the system's side strip, under the status bar, where a
    /// system tab bar sits on iPhone Duo. On the cover screen it steps aside
    /// like the bottom bar; unfolded, a screen's request is for the phone
    /// layout, so only the keyboard hides it.
    @ViewBuilder
    private func strip(_ shell: ShellLayout, safeArea: EdgeInsets) -> some View {
        let hidden = tabBar.keyboardVisible || (tabBar.isHidden && shell.windowSize.width < ShellLayout.railMinWindowWidth)
        if case .strip(let edge) = shell.bar, !hidden {
            let inset = edge == .leading ? safeArea.leading : safeArea.trailing
            WTabRail(items: items, selection: $selection, action: barAction)
                .frame(width: max(inset, ShellLayout.railWidth))
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: edge == .leading ? .bottomLeading : .bottomTrailing)
                .ignoresSafeArea(.container, edges: .horizontal)
                .ignoresSafeArea(.keyboard)
                .transition(.move(edge: edge == .leading ? .leading : .trailing).combined(with: .opacity))
        }
    }

    /// Room the content gives up when the system's strip is narrower than the rail.
    private func stripShortfall(_ shell: ShellLayout, safeArea: EdgeInsets) -> EdgeInsets {
        guard case .strip(let edge) = shell.bar else { return EdgeInsets() }
        let inset = edge == .leading ? safeArea.leading : safeArea.trailing
        let shortfall = max(ShellLayout.railWidth - inset, 0)
        return edge == .leading ? EdgeInsets(top: 0, leading: shortfall, bottom: 0, trailing: 0)
            : EdgeInsets(top: 0, leading: 0, bottom: 0, trailing: shortfall)
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
            if ShellLayout.isPad {
                sidebarPages
            }
        }
        // Keeps the system's own iPad tab strip from appearing above our chrome.
        .tabViewStyle(.tabBarOnly)
    }

    /// The pages the settings hub opens on iPhone, each a destination of its
    /// own in the iPad sidebar.
    @TabContentBuilder<AppTab>
    private var sidebarPages: some TabContent<AppTab> {
        if canContacts {
            Tab("Deals", systemImage: "briefcase", value: AppTab.deals) { CRMDealsView().sidebarDestination() }
            Tab("Tasks", systemImage: "checklist", value: AppTab.tasks) { CRMTasksView().sidebarDestination() }
            Tab("Meetings", systemImage: "calendar", value: AppTab.meetings) { CRMMeetingsView().sidebarDestination() }
        }
        if canMailboxes {
            Tab("Mailboxes", systemImage: "envelope", value: AppTab.mailboxes) { MailboxesRootView().sidebarDestination() }
        }
        if canAnalytics {
            Tab("Analytics", systemImage: "chart.bar", value: AppTab.analytics) { AnalyticsRootView().sidebarDestination() }
            Tab("Activity", systemImage: "clock.arrow.circlepath", value: AppTab.activity) {
                NavigationStack { AuditLogView() }.sidebarDestination()
            }
        }
        if canCampaigns {
            Tab("Templates", systemImage: "doc.on.doc", value: AppTab.templates) {
                NavigationStack { TemplatesListView() }.sidebarDestination()
            }
        }
        Tab("Members", systemImage: "person.3", value: AppTab.team) {
            NavigationStack { TeamMembersView() }.sidebarDestination()
        }
        if canAssistant {
            Tab("Remie", systemImage: "sparkle", value: AppTab.assistant) {
                AssistantView(page: AppTab.assistant.rawValue, initialPrompt: nil).sidebarDestination()
            }
        }
    }
}

private extension View {
    /// A page the iPhone opens as a cover, here a tab of its own beside the sidebar.
    func sidebarDestination() -> some View {
        environment(\.isSidebarDestination, true).toolbarVisibility(.hidden, for: .tabBar)
    }
}
