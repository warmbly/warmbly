import SwiftUI

enum AppTab: String, Hashable {
    case home, inbox, campaigns, contacts, more
}

/// Home is the workspace pulse and the front door to mailboxes/analytics;
/// Inbox carries the unread badge. Tabs the member lacks permission for are
/// hidden, like the web sidebar.
struct MainTabView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var selection: AppTab = .home

    private var canInbox: Bool { env.session.can(.accessUnibox) }
    private var canCampaigns: Bool { env.session.can(.viewCampaigns) }
    private var canContacts: Bool { env.session.can(.viewContacts) }

    var body: some View {
        TabView(selection: $selection) {
            Tab("Home", systemImage: "house.fill", value: AppTab.home) {
                HomeView()
            }
            if canInbox {
                Tab("Inbox", systemImage: "tray.fill", value: AppTab.inbox) {
                    UniboxRootView()
                }
                .badge(env.badges.uniboxUnread)
            }
            if canCampaigns {
                Tab("Campaigns", systemImage: "paperplane.fill", value: AppTab.campaigns) {
                    CampaignsRootView()
                }
            }
            if canContacts {
                Tab("Contacts", systemImage: "person.2.fill", value: AppTab.contacts) {
                    ContactsRootView()
                }
            }
            Tab("More", systemImage: "circle.grid.2x2.fill", value: AppTab.more) {
                MoreRootView()
            }
        }
        .tabBarMinimizeBehavior(.onScrollDown)
        .sensoryFeedback(.selection, trigger: selection)
        .environment(\.switchTab) { tab in
            selection = tab
        }
    }
}
