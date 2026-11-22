import SwiftUI

/// The settings tab: one of the hub layouts over a navigation stack, with the
/// workspace switcher, the browsers it opens over everything and log out. On
/// a wide window the hub becomes a list with the chosen page beside it.
struct MoreRootView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.shellLayout) private var shell
    @State private var store = SettingsHubStore()
    @State private var path: [SettingsDestination] = []
    @State private var pane: SettingsPane?
    @State private var contentWidth: CGFloat?
    @State private var cover: SettingsCover?
    @State private var showSwitcher = false
    @State private var confirmLogout = false

    var body: some View {
        // The shell's estimate covers the first frame, before the page has measured itself.
        let width = contentWidth ?? (shell.windowSize.width - shell.leadingInset)
        let wide = width >= SplitMetrics.detailMinContentWidth
        layout(wide: wide, width: width)
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { contentWidth = $0 }
            .onChange(of: wide, initial: true) { _, wide in handOff(wide) }
            .onChange(of: path) { if wide { handOff(true) } }
            .task { await load() }
            .onChange(of: env.realtime.pulse(for: .billing)) { Task { await load() } }
            .onChange(of: env.realtime.pulse(for: .notifications)) { Task { await load() } }
            .onChange(of: env.realtime.pulse(for: .emailAccounts)) { Task { await load() } }
            .onChange(of: env.realtime.pulse(for: .me)) { Task { await load() } }
            .onChange(of: env.realtime.pulse(for: .team)) { Task { await env.session.refreshCurrentOrg() } }
            .onChange(of: env.session.currentOrgID) { Task { await load() } }
            .onChange(of: path.count) { _, count in
                // Back on the hub: pick up whatever the page changed.
                if count == 0 { Task { await load() } }
            }
            .onChange(of: pane) { Task { await load() } }
            .sheet(isPresented: $showSwitcher) {
                MoreWorkspaceSwitcherSheet()
            }
            .fullScreenCover(item: $cover) { cover in
                switch cover {
                case .mailboxes: MailboxesRootView(onClose: { self.cover = nil })
                case .analytics: AnalyticsRootView(onClose: { self.cover = nil })
                case .deals: CRMDealsView()
                case .tasks: CRMTasksView()
                case .meetings: CRMMeetingsView()
                }
            }
            .confirmationDialog("Log out of Warmbly?", isPresented: $confirmLogout, titleVisibility: .visible) {
                Button("Log out", role: .destructive) {
                    Task { await env.session.logout() }
                }
                Button("Cancel", role: .cancel) {}
            }
            #if DEBUG
            .onAppear(perform: applyDebugLaunch)
            #endif
    }

    @ViewBuilder
    private func layout(wide: Bool, width: CGFloat) -> some View {
        if wide {
            SettingsSplitView(ctx: context, selection: $pane, contentWidth: width, refresh: refresh)
        } else {
            NavigationStack(path: $path) {
                hub
                    .toolbarVisibility(.hidden, for: .navigationBar)
                    .navigationDestination(for: SettingsDestination.self) { destination in
                        switch destination {
                        case .profile: ProfileSettingsView()
                        case .notifications: NotificationsView()
                        case .security: SecurityInfoView()
                        case .billing: BillingView()
                        case .team: TeamMembersView()
                        case .templates: TemplatesListView()
                        case .activity: AuditLogView()
                        }
                    }
                    .refreshable { await refresh() }
            }
        }
    }

    /// Carries the open page across a resize: pushed when narrow, selected when wide.
    private func handOff(_ wide: Bool) {
        if wide {
            if let first = path.first {
                pane = SettingsPane(first)
                path.removeAll()
            }
        } else {
            if let destination = pane?.destination { path = [destination] }
            pane = nil
        }
    }

    private func refresh() async {
        await load()
        await env.session.refreshCurrentOrg()
    }

    private var context: SettingsHubContext {
        SettingsHubContext(
            store: store,
            present: { cover = $0 },
            switchWorkspace: { showSwitcher = true },
            logout: { confirmLogout = true }
        )
    }

    private var hub: some View {
        SettingsTilesHub(ctx: context)
    }

    private func load() async {
        await store.load(env.api, access: .init(
            emails: env.session.can(.manageEmails),
            analytics: env.session.can(.viewAnalytics),
            crm: env.session.can(.viewContacts),
            campaigns: env.session.can(.viewCampaigns)
        ))
    }

    #if DEBUG
    /// WARMBLY_DEBUG_SETTINGS opens a page for scripted screenshots; wide, the hand-off selects it.
    private func applyDebugLaunch() {
        guard path.isEmpty, pane == nil, let page = ProcessInfo.processInfo.environment["WARMBLY_DEBUG_SETTINGS"] else { return }
        let destinations: [String: SettingsDestination] = [
            "profile": .profile, "notifications": .notifications, "security": .security,
            "billing": .billing, "team": .team, "templates": .templates, "activity": .activity,
        ]
        if let destination = destinations[page] { path.append(destination) }
        if page == "appearance" { pane = .appearance }
        if let target = SettingsCover(rawValue: page) { cover = target }
    }
    #endif
}

// MARK: - Workspace switcher

struct MoreWorkspaceSwitcherSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    @State private var switchingID: String?
    @State private var showCreate = false
    @State private var orgBeforeCreate: String?
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 9) {
                    EyebrowLabel("Workspaces")
                        .padding(.leading, 4)
                    MoreCard(dividerLeading: 58) {
                        ForEach(env.session.memberships) { membership in
                            if let org = membership.organization {
                                workspaceRow(membership: membership, org: org)
                            }
                        }
                        createRow
                    }
                }
                .padding(.horizontal, 20)
                .padding(.top, 8)
                .padding(.bottom, 24)
            }
            .background(WTheme.canvas)
            .navigationTitle("Workspaces")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(WTheme.canvas, for: .navigationBar)
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
        .tint(WTheme.ink)
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .presentationBackground(WTheme.canvas)
        .fullScreenCover(isPresented: $showCreate, onDismiss: {
            // Creation switches the session into the new workspace; close the
            // switcher too so the user lands straight in it.
            if env.session.currentOrgID != orgBeforeCreate { dismiss() }
        }) {
            WorkspaceCreateFlow(context: .cover, onClose: { showCreate = false })
        }
    }

    private func workspaceRow(membership: OrganizationMember, org: Organization) -> some View {
        Button {
            select(org.id)
        } label: {
            HStack(spacing: 12) {
                WAvatar(name: org.name, imageURL: org.avatarURL, seed: org.id, size: 32)
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 5) {
                        Text(org.name)
                            .font(.system(size: 15))
                            .foregroundStyle(WTheme.ink)
                            .lineLimit(1)
                        if membership.role == "owner" {
                            Image(systemName: "crown.fill")
                                .font(.system(size: 9))
                                .foregroundStyle(Tone.amber.color)
                        }
                    }
                    if let role = membership.role {
                        Text(role)
                            .font(WFont.meta(10, weight: .medium))
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                }
                Spacer(minLength: 8)
                if switchingID == org.id {
                    ProgressView().controlSize(.small)
                } else if org.id == env.session.currentOrgID {
                    Image(systemName: "checkmark")
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(WTheme.ink)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 11)
            .contentShape(Rectangle())
        }
        .buttonStyle(MoreRowPressStyle())
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
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(WTheme.ink)
                    .frame(width: 32, height: 32)
                    .overlay(
                        Circle().strokeBorder(style: StrokeStyle(lineWidth: 1, dash: [3, 3]))
                            .foregroundStyle(WTheme.hairlineStrong)
                    )
                Text("New workspace")
                    .font(.system(size: 15))
                    .foregroundStyle(WTheme.ink)
                Spacer(minLength: 8)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 11)
            .contentShape(Rectangle())
        }
        .buttonStyle(MoreRowPressStyle())
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
