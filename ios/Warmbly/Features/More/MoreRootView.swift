import SwiftUI

/// Lightweight state for the hub itself: the plan pill and the unread badge.
@MainActor
@Observable
final class MoreHubStore {
    var subscription: SubscriptionInfo?
    var unreadNotifications = 0
    private var isLoading = false

    func load(_ api: APIClient) async {
        guard !isLoading else { return }
        isLoading = true
        subscription = try? await api.get("subscription")
        if let feed: MoreNotificationFeed = try? await api.get(
            "auth/me/notifications",
            query: ["limit": "10", "unread": "1"]
        ) {
            unreadNotifications = feed.unread ?? 0
        }
        isLoading = false
    }
}

/// The More tab hub: one flat, full-bleed list (no grouped cards) — a rich
/// workspace header up top, then eyebrow-captioned, hairline-separated rows
/// that push every screen in this module.
struct MoreRootView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = MoreHubStore()
    @State private var showSwitcher = false
    @State private var confirmLogout = false
    @State private var showDeals = false
    @State private var showTasks = false
    @State private var showMeetings = false
    @State private var showMailboxes = false
    @State private var showAnalytics = false

    /// Content leading where row text starts (inset + 34 tile + 12 gap), so
    /// hairlines tuck under the text column, Gmail-style.
    private static let rowTextLeading: CGFloat = 62

    var body: some View {
        NavigationStack {
            List {
                workspaceHeader
                if env.session.can(.manageEmails) { sendingSection }
                if env.session.can(.viewAnalytics) { insightsSection }
                if env.session.can(.viewContacts) { crmSection }
                if env.session.can(.viewCampaigns) { resourcesSection }
                teamSection
                accountSection
                systemSection
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .environment(\.defaultMinListRowHeight, 0)
            .background(Color(.systemBackground))
            .contentMargins(.bottom, 28, for: .scrollContent)
            .navigationTitle("More")
            .task { await store.load(env.api) }
            .onChange(of: env.realtime.pulse(for: .billing)) {
                Task { await store.load(env.api) }
            }
            .onChange(of: env.realtime.pulse(for: .notifications)) {
                Task { await store.load(env.api) }
            }
            .onChange(of: env.realtime.pulse(for: .team)) {
                Task { await env.session.refreshCurrentOrg() }
            }
            .onChange(of: env.session.currentOrgID) {
                Task { await store.load(env.api) }
            }
            .refreshable {
                await store.load(env.api)
                await env.session.refreshCurrentOrg()
            }
            .sheet(isPresented: $showSwitcher) {
                MoreWorkspaceSwitcherSheet()
            }
            .confirmationDialog("Log out of Warmbly?", isPresented: $confirmLogout, titleVisibility: .visible) {
                Button("Log out", role: .destructive) {
                    Task { await env.session.logout() }
                }
                Button("Cancel", role: .cancel) {}
            }
        }
    }

    // MARK: Flat-list helpers

    /// Eyebrow caption as a bare row — never a grouped `Section` header (that
    /// reintroduces the sticky gray band). Whitespace above is the separator.
    private func sectionHeader(_ title: String, top: CGFloat = 22) -> some View {
        EyebrowLabel(title)
            .padding(.horizontal, 20)
            .padding(.top, top)
            .padding(.bottom, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .listRowInsets(EdgeInsets())
            .listRowSeparator(.hidden)
            .listRowBackground(Color(.systemBackground))
    }

    private func plainRow<Content: View>(
        separator: Visibility = .automatic,
        @ViewBuilder _ content: () -> Content
    ) -> some View {
        content()
            .listRowInsets(EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20))
            .listRowSeparator(separator)
            .alignmentGuide(.listRowSeparatorLeading) { _ in Self.rowTextLeading }
            .listRowBackground(Color(.systemBackground))
    }

    private func navRow<Destination: View>(
        icon: String,
        tone: Tone,
        title: String,
        subtitle: String? = nil,
        value: String? = nil,
        badge: Int = 0,
        @ViewBuilder destination: @escaping () -> Destination
    ) -> some View {
        plainRow {
            NavigationLink {
                destination()
            } label: {
                MoreHubRow(icon: icon, title: title, subtitle: subtitle, tone: tone, value: value, badge: badge)
            }
        }
    }

    // MARK: Workspace header

    private var connectionLabel: String {
        switch env.realtime.connectionState {
        case .connected: return "Live"
        case .connecting: return "Connecting"
        case .disconnected: return "Offline"
        }
    }

    private var connectionTone: Tone {
        switch env.realtime.connectionState {
        case .connected: return .emerald
        case .connecting: return .amber
        case .disconnected: return .rose
        }
    }

    /// One premium header block: the workspace identity (tap to switch) with
    /// the live connection + presence strip tucked underneath.
    @ViewBuilder
    private var workspaceHeader: some View {
        plainRow(separator: .hidden) {
            Button {
                showSwitcher = true
            } label: {
                HStack(spacing: 13) {
                    if let org = env.session.currentOrg {
                        WAvatar(name: org.name, imageURL: org.avatarURL, seed: org.id, size: 46)
                        VStack(alignment: .leading, spacing: 5) {
                            Text(org.name)
                                .font(.system(size: 19, weight: .semibold))
                                .foregroundStyle(.primary)
                                .lineLimit(1)
                            HStack(spacing: 7) {
                                MorePlanPill(subscription: store.subscription)
                                if let role = env.session.role {
                                    Text(role.capitalized)
                                        .font(.footnote)
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }
                    } else {
                        Text("Select workspace")
                            .font(.body.weight(.medium))
                            .foregroundStyle(.primary)
                    }
                    Spacer(minLength: 8)
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(.tertiary)
                }
                .padding(.top, 6)
                .padding(.bottom, 12)
                .contentShape(Rectangle())
            }
            .buttonStyle(TapScaleStyle())
            .accessibilityLabel("Switch workspace")
        }
        plainRow(separator: .hidden) {
            HStack(spacing: 10) {
                StatusPill(
                    text: connectionLabel,
                    tone: connectionTone,
                    pulsing: env.realtime.connectionState == .connected
                )
                Spacer(minLength: 8)
                PresenceAvatars()
                Text("\(env.realtime.presence.onlineCount) online")
                    .font(.footnote.weight(.medium))
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                    .contentTransition(.numericText())
                    .animation(.default, value: env.realtime.presence.onlineCount)
            }
            .padding(.bottom, 4)
        }
    }

    // MARK: Sections

    @ViewBuilder
    private var sendingSection: some View {
        sectionHeader("Sending")
        coverRow(icon: "envelope.fill", tone: .orange, title: "Mailboxes", subtitle: "Sender accounts and warmup", isPresented: $showMailboxes) {
            MailboxesRootView(onClose: { showMailboxes = false })
        }
    }

    @ViewBuilder
    private var insightsSection: some View {
        sectionHeader("Insights")
        coverRow(icon: "chart.bar.fill", tone: .sky, title: "Analytics", subtitle: "Sends, opens, replies", isPresented: $showAnalytics) {
            AnalyticsRootView(onClose: { showAnalytics = false })
        }
        navRow(icon: "clock.arrow.circlepath", tone: .indigo, title: "Activity", subtitle: "Audit log, last 90 days") {
            AuditLogView()
        }
    }

    @ViewBuilder
    private var crmSection: some View {
        sectionHeader("CRM")
        coverRow(icon: "briefcase.fill", tone: .sky, title: "Deals", isPresented: $showDeals) {
            CRMDealsView()
        }
        coverRow(icon: "checklist", tone: .emerald, title: "Tasks", isPresented: $showTasks) {
            CRMTasksView()
        }
        coverRow(icon: "calendar", tone: .orange, title: "Meetings", isPresented: $showMeetings) {
            CRMMeetingsView()
        }
    }

    /// CRM rows open full-screen drawer browsers, not pushes; a chevron keeps
    /// them reading as navigable.
    private func coverRow<Content: View>(
        icon: String,
        tone: Tone,
        title: String,
        subtitle: String? = nil,
        isPresented: Binding<Bool>,
        @ViewBuilder content: @escaping () -> Content
    ) -> some View {
        plainRow {
            Button {
                isPresented.wrappedValue = true
            } label: {
                HStack(spacing: 8) {
                    MoreHubRow(icon: icon, title: title, subtitle: subtitle, tone: tone)
                    Image(systemName: "chevron.right")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(.tertiary)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .fullScreenCover(isPresented: isPresented) {
                content()
            }
        }
    }

    @ViewBuilder
    private var resourcesSection: some View {
        sectionHeader("Resources")
        navRow(icon: "doc.on.doc.fill", tone: .indigo, title: "Templates") {
            TemplatesListView()
        }
    }

    @ViewBuilder
    private var teamSection: some View {
        sectionHeader("Team")
        navRow(icon: "person.2.fill", tone: .sky, title: "Members", value: memberCountValue) {
            TeamMembersView()
        }
    }

    private var memberCountValue: String? {
        guard let count = env.session.currentOrg?.counts?.totalMembers else { return nil }
        return count == 1 ? "1 member" : "\(count) members"
    }

    @ViewBuilder
    private var accountSection: some View {
        sectionHeader("Account")
        navRow(icon: "person.crop.circle", tone: .slate, title: "Profile", subtitle: env.session.user?.email) {
            ProfileSettingsView()
        }
        navRow(icon: "bell", tone: .amber, title: "Notifications", badge: store.unreadNotifications) {
            NotificationsView()
        }
        navRow(icon: "lock.shield", tone: .emerald, title: "Security", subtitle: "2FA, passkeys, sessions") {
            SecurityInfoView()
        }
        if env.session.can(.manageBilling) {
            navRow(icon: "creditcard", tone: .indigo, title: "Billing", subtitle: "Plan and usage") {
                BillingView()
            }
        }
    }

    @ViewBuilder
    private var systemSection: some View {
        sectionHeader("System")
        plainRow {
            HStack(spacing: 12) {
                IconTile(symbol: "server.rack", tone: .slate, size: 34)
                VStack(alignment: .leading, spacing: 1.5) {
                    Text("Server")
                        .font(.body.weight(.medium))
                    Text(AppConfig.serverOrigin)
                        .font(.footnote.monospaced())
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
            }
            .padding(.vertical, 9)
        }
        plainRow {
            Button(role: .destructive) {
                confirmLogout = true
            } label: {
                MoreHubRow(icon: "rectangle.portrait.and.arrow.right", title: "Log out", tone: .rose, titleTone: .rose)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        }
    }
}

// MARK: - Workspace switcher

struct MoreWorkspaceSwitcherSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    @State private var switchingID: String?
    @State private var showCreate = false
    @State private var orgBeforeCreate: String?
    @State private var errorMessage: String?

    /// Avatar 32 + 12 gap: hairlines start under the workspace names.
    private static let rowTextLeading: CGFloat = 44

    var body: some View {
        NavigationStack {
            List {
                sheetHeader("Workspaces", top: 10)
                ForEach(env.session.memberships) { membership in
                    if let org = membership.organization {
                        sheetRow { workspaceRow(membership: membership, org: org) }
                    }
                }
                sheetRow(separator: .hidden) { createRow }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .environment(\.defaultMinListRowHeight, 0)
            .background(Color(.systemBackground))
            .navigationTitle("Workspace")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .alert(
                "Couldn't switch workspace",
                isPresented: Binding(
                    get: { errorMessage != nil },
                    set: { if !$0 { errorMessage = nil } }
                )
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(errorMessage ?? "")
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .fullScreenCover(isPresented: $showCreate, onDismiss: {
            // Creation switches the session into the new workspace; close the
            // switcher too so the user lands straight in it.
            if env.session.currentOrgID != orgBeforeCreate { dismiss() }
        }) {
            WorkspaceCreateFlow(context: .cover, onClose: { showCreate = false })
        }
    }

    private func sheetHeader(_ title: String, top: CGFloat = 22) -> some View {
        EyebrowLabel(title)
            .padding(.horizontal, 20)
            .padding(.top, top)
            .padding(.bottom, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .listRowInsets(EdgeInsets())
            .listRowSeparator(.hidden)
            .listRowBackground(Color(.systemBackground))
    }

    private func sheetRow<Content: View>(
        separator: Visibility = .automatic,
        @ViewBuilder _ content: () -> Content
    ) -> some View {
        content()
            .listRowInsets(EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20))
            .listRowSeparator(separator)
            .alignmentGuide(.listRowSeparatorLeading) { _ in Self.rowTextLeading }
            .listRowBackground(Color(.systemBackground))
    }

    private func workspaceRow(membership: OrganizationMember, org: Organization) -> some View {
        Button {
            select(org.id)
        } label: {
            HStack(spacing: 12) {
                WAvatar(name: org.name, imageURL: org.avatarURL, seed: org.id, size: 32)
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 4) {
                        Text(org.name)
                            .font(.system(size: 14, weight: .medium))
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                        if membership.role == "owner" {
                            Image(systemName: "crown.fill")
                                .font(.system(size: 9))
                                .foregroundStyle(Tone.amber.color)
                        }
                    }
                    if let role = membership.role {
                        Text(role.capitalized)
                            .font(.system(size: 11.5))
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: 8)
                if switchingID == org.id {
                    ProgressView().controlSize(.small)
                } else if org.id == env.session.currentOrgID {
                    Image(systemName: "checkmark")
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(WTheme.accent)
                }
            }
            .padding(.vertical, 9)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(switchingID != nil)
    }

    /// Opens the full workspace-creation onboarding rather than an inline form.
    private var createRow: some View {
        Button {
            orgBeforeCreate = env.session.currentOrgID
            showCreate = true
        } label: {
            HStack(spacing: 12) {
                Image(systemName: "plus")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(WTheme.accent)
                    .frame(width: 32, height: 32)
                    .background(Tone.sky.background, in: Circle())
                Text("New workspace")
                    .font(.system(size: 14, weight: .medium))
                    .foregroundStyle(WTheme.accent)
                Spacer(minLength: 8)
            }
            .padding(.top, 10)
            .padding(.bottom, 16)
            .contentShape(Rectangle())
        }
        .buttonStyle(TapScaleStyle())
        .disabled(switchingID != nil)
    }

    private func select(_ orgID: String) {
        guard switchingID == nil else { return }
        if orgID == env.session.currentOrgID {
            dismiss()
            return
        }
        switchingID = orgID
        Task {
            do {
                try await env.session.switchOrganization(orgID)
                dismiss()
            } catch {
                errorMessage = error.localizedDescription
            }
            switchingID = nil
        }
    }

}
