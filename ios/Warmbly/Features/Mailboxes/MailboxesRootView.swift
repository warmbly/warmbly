import SwiftUI

/// Mailboxes screen, pushed from Home or More: fleet summary, server-side
/// search, cursor pagination, and realtime pulse reloads. No NavigationStack
/// of its own; it lives on the pushing tab's stack.
struct MailboxesRootView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = MailboxesStore()
    @State private var searchText = ""
    @State private var showConnect = false
    @State private var pendingRemove: EmailAccount?

    private var canManage: Bool { env.session.can(.manageEmails) }
    private var canViewAnalytics: Bool { env.session.can(.viewAnalytics) }

    var body: some View {
        content
            .navigationTitle("Mailboxes")
            .background(Color(.systemGroupedBackground))
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        showConnect = true
                    } label: {
                        Image(systemName: "plus")
                    }
                    .accessibilityLabel("Connect a mailbox")
                }
            }
            .navigationDestination(for: EmailAccount.self) { account in
                MailboxDetailView(account: account)
            }
            .searchable(text: $searchText, prompt: "Search mailboxes")
            .task(id: searchText) { await runSearch() }
            .onChange(of: env.realtime.pulse(for: .emailAccounts)) {
                Task { await store.load(env.api, includeStatuses: canViewAnalytics) }
            }
            .onChange(of: env.realtime.pulse(for: .analytics)) {
                Task { await store.loadStatuses(env.api) }
            }
            .sheet(isPresented: $showConnect) { MailboxConnectSheet() }
            .confirmationDialog(
                "Remove \(pendingRemove?.email ?? "this mailbox")?",
                isPresented: Binding(
                    get: { pendingRemove != nil },
                    set: { if !$0 { pendingRemove = nil } }
                ),
                titleVisibility: .visible,
                presenting: pendingRemove
            ) { account in
                Button("Remove account", role: .destructive) {
                    Task { await store.deleteAccount(env.api, id: account.id) }
                }
            } message: { account in
                Text("Sending stops immediately and \(account.email) leaves all warmup pools.")
            }
            .alert(
                "Something went wrong",
                isPresented: Binding(
                    get: { store.actionError != nil },
                    set: { if !$0 { store.actionError = nil } }
                )
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(store.actionError ?? "")
            }
    }

    // MARK: Content

    @ViewBuilder
    private var content: some View {
        if !store.hasLoaded {
            ScrollView { SkeletonRows(rows: 10) }
        } else if let error = store.loadError, store.accounts.isEmpty {
            ErrorStateView(title: "Couldn't load mailboxes", message: error) {
                await store.load(env.api, includeStatuses: canViewAnalytics)
            }
        } else if store.accounts.isEmpty {
            if searchText.isEmpty {
                EmptyStateView(
                    title: "No mailboxes yet",
                    message: "Connect a sender account to start warming and sending.",
                    ctaTitle: "Connect a mailbox"
                ) { showConnect = true }
            } else {
                EmptyStateView(
                    title: "No matches",
                    message: "No mailbox matches \"\(searchText)\"."
                )
            }
        } else {
            accountList
        }
    }

    private var accountList: some View {
        List {
            Section {
                fleetSummary
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets(top: 2, leading: 20, bottom: 4, trailing: 20))
            }
            Section {
                ForEach(store.accounts) { account in
                    NavigationLink(value: account) {
                        MailboxRowView(account: account, status: store.statuses[account.id])
                    }
                    .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                        swipeButtons(for: account)
                    }
                }
                if store.nextCursor != nil {
                    HStack {
                        Spacer()
                        ProgressView().controlSize(.small)
                        Spacer()
                    }
                    .listRowSeparator(.hidden)
                    .task { await store.loadMore(env.api) }
                }
            }
        }
        .listStyle(.insetGrouped)
        .refreshable { await store.load(env.api, includeStatuses: canViewAnalytics) }
    }

    private var fleetSummary: some View {
        HStack(spacing: 10) {
            fleetStat(
                value: "\(store.totalCount ?? store.accounts.count)",
                label: "Connected",
                symbol: "envelope.fill",
                tone: .sky
            )
            fleetStat(
                value: "\(store.warmingCount)",
                label: "Warming",
                symbol: "flame.fill",
                tone: store.warmingCount > 0 ? .orange : .slate
            )
            fleetStat(
                value: "\(store.issueCount)",
                label: "Issues",
                symbol: store.issueCount > 0 ? "exclamationmark.triangle.fill" : "checkmark.circle.fill",
                tone: store.issueCount > 0 ? .rose : .emerald
            )
        }
    }

    private func fleetStat(value: String, label: String, symbol: String, tone: Tone) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: symbol)
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(tone.color)
                Text(value)
                    .font(.system(size: 22, weight: .bold, design: .rounded))
                    .monospacedDigit()
                    .contentTransition(.numericText())
            }
            Text(label)
                .font(.footnote.weight(.medium))
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .background(Color(.secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
    }

    @ViewBuilder
    private func swipeButtons(for account: EmailAccount) -> some View {
        if canManage {
            Button {
                pendingRemove = account
            } label: {
                Label("Remove", systemImage: "trash")
            }
            .tint(WTheme.negative)

            switch account.warmupState {
            case .active:
                Button {
                    Task { await store.warmupAction(env.api, id: account.id, action: "pause") }
                } label: {
                    Label("Pause", systemImage: "pause.fill")
                }
                .tint(WTheme.warning)
            case .paused:
                Button {
                    Task { await store.warmupAction(env.api, id: account.id, action: "resume") }
                } label: {
                    Label("Resume", systemImage: "flame.fill")
                }
                .tint(Tone.orange.color)
            case .off:
                Button {
                    Task { await store.warmupAction(env.api, id: account.id, action: "start") }
                } label: {
                    Label("Warm up", systemImage: "flame.fill")
                }
                .tint(Tone.orange.color)
            }
        }
    }

    // MARK: Search + initial load

    private func runSearch() async {
        if store.hasLoaded {
            try? await Task.sleep(for: .milliseconds(350))
            if Task.isCancelled { return }
        }
        store.query = searchText
        await store.load(env.api, includeStatuses: canViewAnalytics)
    }
}

// MARK: - Row

struct MailboxRowView: View {
    let account: EmailAccount
    let status: AccountAnalytics?

    private var health: MailboxHealth? { status?.health }

    var body: some View {
        HStack(spacing: 12) {
            WAvatar(name: account.email, seed: account.id, size: 42)
                .overlay(alignment: .bottomTrailing) {
                    if account.isWarmingActive {
                        Image(systemName: "flame.fill")
                            .font(.system(size: 9, weight: .bold))
                            .foregroundStyle(.white)
                            .frame(width: 16, height: 16)
                            .background(Tone.orange.color, in: Circle())
                            .overlay(Circle().strokeBorder(Color(.secondarySystemGroupedBackground), lineWidth: 1.5))
                            .offset(x: 3, y: 3)
                    }
                }
            VStack(alignment: .leading, spacing: 3) {
                Text(account.email)
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                HStack(spacing: 5) {
                    Text(account.providerLabel)
                        .foregroundStyle(.secondary)
                    warmupCaption
                }
                .font(.footnote)
            }
            Spacer(minLength: 8)
            if let health {
                HealthRing(score: health.score, tone: health.tone)
                    .modifier(PingEffect(active: health.hasIssue, color: health.tone.color))
            }
        }
        .padding(.vertical, 6)
    }

    @ViewBuilder
    private var warmupCaption: some View {
        switch account.warmupState {
        case .active:
            Text("· \(volumeText)")
                .monospacedDigit()
                .foregroundStyle(Tone.orange.color)
        case .paused:
            Text("· warmup paused")
                .foregroundStyle(WTheme.warning)
        case .off:
            if let sent = status?.dailyUsage?.campaignSent {
                Text("· \(sent) sent today")
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
            }
        }
    }

    private var volumeText: String {
        if let ws = status?.warmupStatus {
            return "\(ws.currentVolume ?? 0)/\(ws.targetVolume ?? 0) warm"
        }
        return "warming"
    }
}
