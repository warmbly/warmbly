import SwiftUI

// The campaign Leads browser: the full contacts-style browse experience scoped
// to one campaign. A slide-in drawer (sky hero + status scopes with live counts
// + sliding capsule + edge-swipe), a search pill with advanced filters, dense
// rows that surface who is being processed right now, a pushed per-lead detail
// with the engagement funnel, multi-select with rich bulk edit + remove, and an
// add-leads picker. Leads search runs through POST /contacts/search (campaign_ids
// + lead_status + the shared advanced filters), so counts stay exact at scale,
// and it reloads on the contacts/campaigns realtime pulses so processing state
// and replies land live. Presented as a full-screen cover from the campaign hub.

// MARK: - Store

@MainActor
@Observable
final class CampaignLeadsStore {
    private(set) var leads: [CampaignLead] = []
    /// Exact count for the current scope + search (`pagination.total`).
    private(set) var totalCount: Int?
    /// Per-status totals for the scope rows (from the first page's lead_counts).
    private(set) var counts: CampaignLeadCounts?
    private(set) var nextCursor: String?
    private(set) var hasMore = false
    private(set) var isLoading = false
    private(set) var isLoadingMore = false
    private(set) var errorMessage: String?
    private(set) var hasLoaded = false

    var scope: CampaignLeadScope = .all
    var query = ""
    /// Advanced criteria from the shared filter sheet, layered on scope + search.
    var filters = ContactAdvancedFilters()

    // Multi-select
    var selectionMode = false
    private(set) var selected: Set<String> = []

    /// Campaigns for the bulk add/remove pickers (fetched lazily).
    private(set) var campaignOptions: [ContactMiniCampaign] = []

    private var generation = 0
    private static let isoDate = Date.ISO8601FormatStyle()

    var isSearching: Bool { !query.trimmingCharacters(in: .whitespaces).isEmpty }
    var hasFilters: Bool { filters.isActive }

    func scopeCount(_ scope: CampaignLeadScope) -> Int { scope.count(counts) }

    // MARK: Selection

    var selectedCount: Int { selected.count }
    var allLoadedSelected: Bool { !leads.isEmpty && selected.count >= leads.count }

    func isSelected(_ id: String) -> Bool { selected.contains(id) }

    func toggleSelected(_ id: String) {
        if selected.contains(id) { selected.remove(id) } else { selected.insert(id) }
    }

    func enterSelection(with id: String? = nil) {
        withAnimation(.snappy) {
            selectionMode = true
            if let id { selected.insert(id) }
        }
    }

    func exitSelection() {
        withAnimation(.snappy) {
            selectionMode = false
            selected.removeAll()
        }
    }

    func selectAllLoaded() {
        withAnimation(.snappy) {
            if allLoadedSelected { selected.removeAll() } else { selected = Set(leads.map(\.id)) }
        }
    }

    // MARK: Loading

    private func searchBody(_ campaignID: String) -> ContactSearchBody {
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        var body = ContactSearchBody(query: trimmed.isEmpty ? nil : trimmed)
        body.campaignIDs = [campaignID]
        body.leadStatus = scope.leadStatus

        let f = filters
        let completeFields = f.customFields.filter(\.isComplete)
        if !completeFields.isEmpty {
            body.customFieldFilters = completeFields.map {
                ContactFieldFilterPayload(
                    name: $0.name.trimmingCharacters(in: .whitespaces),
                    value: $0.value.trimmingCharacters(in: .whitespaces),
                    type: $0.type.rawValue
                )
            }
        }
        if !f.categoryIDs.isEmpty { body.categoryIDs = Array(f.categoryIDs) }
        if let s = f.subscribed { body.subscribed = s }
        if let mn = f.minCampaigns { body.minCampaigns = mn }
        if let mx = f.maxCampaigns { body.maxCampaigns = mx }
        body.createdAfter = f.createdAfter.map { Self.isoDate.format($0) }
        body.createdBefore = f.createdBefore.map { Self.isoDate.format($0) }
        body.updatedAfter = f.updatedAfter.map { Self.isoDate.format($0) }
        body.updatedBefore = f.updatedBefore.map { Self.isoDate.format($0) }
        body.sortBy = f.sortBy.rawValue
        body.reverse = f.reverse
        return body
    }

    func load(_ api: APIClient, campaignID: String) async {
        generation += 1
        let gen = generation
        isLoading = true
        if !hasLoaded { errorMessage = nil }
        do {
            let page: CampaignLeadsPage = try await api.post(
                "contacts/search",
                body: searchBody(campaignID),
                query: ["limit": "50"]
            )
            guard gen == generation else { return }
            withAnimation(.snappy) {
                leads = page.data ?? []
                totalCount = page.pagination?.total.map(Int.init)
                if let fresh = page.leadCounts { counts = fresh }
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
            errorMessage = nil
            hasLoaded = true
            isLoading = false
        } catch {
            guard gen == generation else { return }
            if !hasLoaded { errorMessage = error.localizedDescription }
            isLoading = false
        }
    }

    func loadMore(_ api: APIClient, campaignID: String) async {
        guard hasMore, !isLoadingMore, let cursor = nextCursor else { return }
        let gen = generation
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page: CampaignLeadsPage = try await api.post(
                "contacts/search",
                body: searchBody(campaignID),
                query: ["limit": "50", "cursor": cursor]
            )
            guard gen == generation else { return }
            let fresh = (page.data ?? []).filter { new in !leads.contains(where: { $0.id == new.id }) }
            withAnimation(.snappy) {
                leads.append(contentsOf: fresh)
                nextCursor = page.pagination?.nextCursor
                hasMore = page.pagination?.hasMore ?? false
            }
        } catch {
            // Keep the list; the sentinel row retries on next appear.
        }
    }

    // MARK: Bulk

    /// Campaigns for the bulk add/remove pickers (id + name), fetched once.
    func loadCampaignOptions(_ api: APIClient) async {
        guard campaignOptions.isEmpty else { return }
        if let page: ListResponse<ContactMiniCampaign> = try? await api.get("campaigns", query: ["limit": "100"]) {
            campaignOptions = page.data
        }
    }

    /// PATCH /contacts (bulk): apply membership/subscription changes to the ids.
    func bulkEdit(_ api: APIClient, body: ContactBulkEditBody) async throws {
        let _: [Contact] = try await api.patch("contacts", body: body)
    }

    /// PATCH /contacts (bulk) removing this campaign from the given leads, then
    /// drop them from the visible list and nudge the counts down so the UI reacts
    /// immediately; the next realtime reload reconciles exactly.
    func removeFromCampaign(_ api: APIClient, campaignID: String, ids: [String]) async throws {
        guard !ids.isEmpty else { return }
        let body = ContactBulkEditBody(contacts: ids, removeCampaigns: [campaignID])
        let _: [Contact] = try await api.patch("contacts", body: body)
        let removed = Set(ids)
        withAnimation(.snappy) {
            let gone = leads.filter { removed.contains($0.id) }
            leads.removeAll { removed.contains($0.id) }
            if let total = totalCount { totalCount = max(0, total - gone.count) }
            bumpCounts(removing: gone)
        }
    }

    /// Optimistically decrement the chip totals for removed leads by their
    /// current derived bucket, so both "All" and the active scope move at once.
    private func bumpCounts(removing gone: [CampaignLead]) {
        guard var c = counts else { return }
        for lead in gone {
            c.total = max(0, (c.total ?? 0) - 1)
            switch lead.progress?.status {
            case "active": c.processing = max(0, (c.processing ?? 0) - 1)
            case "replied": c.replied = max(0, (c.replied ?? 0) - 1)
            case "bounced": c.bounced = max(0, (c.bounced ?? 0) - 1)
            case "unsubscribed": c.unsubscribed = max(0, (c.unsubscribed ?? 0) - 1)
            default: c.queued = max(0, (c.queued ?? 0) - 1)
            }
        }
        counts = c
    }
}

// MARK: - Browser

struct CampaignLeadsPageView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let campaignID: String
    let campaignName: String
    let store: CampaignLeadsStore

    @State private var categoryStore = ContactCategoryStore()
    @State private var openLead: CampaignLead?
    @State private var sidebarOpen = false
    @State private var sidebarDrag: CGFloat = 0
    @State private var showFilters = false
    @State private var showBulkEdit = false
    @State private var showAddLeads = false
    @State private var confirmBulkRemove = false
    @FocusState private var searchFocused: Bool

    private static let sidebarWidth: CGFloat = 300
    private var canManage: Bool { env.session.can(.manageContacts) }

    /// Sends bump .campaigns/.analytics together and lead membership bumps
    /// .contacts; summing them means one reload per event, never a double.
    private var reloadPulse: Int {
        env.realtime.pulse(for: .contacts) &+ env.realtime.pulse(for: .campaigns)
    }

    var body: some View {
        NavigationStack {
            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    mainPane
                        .scaleEffect(sidebarOpen ? 0.97 : 1, anchor: .trailing)
                    if sidebarOpen {
                        Color.black.opacity(0.32)
                            .ignoresSafeArea()
                            .transition(.opacity)
                            .onTapGesture { closeSidebar() }
                    }
                    drawer(topInset: geo.safeAreaInsets.top)
                }
            }
            .toolbar(.hidden, for: .navigationBar)
            .navigationDestination(item: $openLead) { lead in
                CampaignLeadDetailView(campaignID: campaignID, lead: lead, store: store)
            }
            .sheet(isPresented: $showFilters) {
                ContactFiltersSheet(
                    categories: categoryStore.categories,
                    initial: store.filters
                ) { store.filters = $0 }
                .presentationDetents([.large])
            }
            .sheet(isPresented: $showBulkEdit) {
                ContactBulkEditSheet(
                    ids: Array(store.selected),
                    categories: categoryStore.categories,
                    campaigns: store.campaignOptions,
                    loadCampaigns: { await store.loadCampaignOptions(env.api) },
                    perform: { body in
                        try await store.bulkEdit(env.api, body: body)
                        await store.load(env.api, campaignID: campaignID)
                        store.exitSelection()
                    }
                )
            }
            .sheet(isPresented: $showAddLeads) {
                CampaignAddLeadsSheet(campaignID: campaignID, campaignName: campaignName) {
                    await store.load(env.api, campaignID: campaignID)
                }
            }
            .confirmationDialog(
                "Remove \(store.selectedCount) lead\(store.selectedCount == 1 ? "" : "s")?",
                isPresented: $confirmBulkRemove,
                titleVisibility: .visible
            ) {
                Button("Remove from campaign", role: .destructive) {
                    Task {
                        try? await store.removeFromCampaign(env.api, campaignID: campaignID, ids: Array(store.selected))
                        store.exitSelection()
                    }
                }
            } message: {
                Text("They stay in your contacts and keep their history. Sending to them stops.")
            }
        }
        .task { if !store.hasLoaded { await store.load(env.api, campaignID: campaignID) } }
        .task { await categoryStore.load(env.api) }
        .task(id: store.query) {
            guard store.hasLoaded else { return }
            try? await Task.sleep(for: .milliseconds(350))
            guard !Task.isCancelled else { return }
            await store.load(env.api, campaignID: campaignID)
        }
        .onChange(of: store.scope) {
            store.exitSelection()
            Task { await store.load(env.api, campaignID: campaignID) }
        }
        .onChange(of: store.filters) {
            store.exitSelection()
            Task { await store.load(env.api, campaignID: campaignID) }
        }
        .onChange(of: reloadPulse) {
            Task { await store.load(env.api, campaignID: campaignID) }
        }
        .sensoryFeedback(.impact(weight: .light), trigger: sidebarOpen)
        .sensoryFeedback(.selection, trigger: store.scope)
        .sensoryFeedback(.impact(weight: .medium), trigger: store.selectionMode)
    }

    // MARK: Main pane

    private var mainPane: some View {
        VStack(spacing: 0) {
            if store.selectionMode { selectionHeader } else { searchBar }
            scopeCaption
            list
        }
        .background(Color(.systemBackground))
        .overlay(alignment: .bottom) {
            if store.selectionMode {
                CampaignLeadSelectionBar(
                    count: store.selectedCount,
                    onEdit: { showBulkEdit = true },
                    onRemove: { confirmBulkRemove = true }
                )
                .padding(.bottom, 10)
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .simultaneousGesture(
            DragGesture(minimumDistance: 25)
                .onEnded { value in
                    if !sidebarOpen, !store.selectionMode, value.startLocation.x < 44, value.translation.width > 70 {
                        openSidebar()
                    }
                }
        )
    }

    // MARK: Search pill

    private var searchBar: some View {
        @Bindable var store = store
        return HStack(spacing: 10) {
            HStack(spacing: 6) {
                Button {
                    openSidebar()
                } label: {
                    Image(systemName: "line.3.horizontal")
                        .font(.system(size: 17, weight: .medium))
                        .foregroundStyle(.primary)
                        .frame(width: 38, height: 38)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Open leads menu")

                TextField("Search leads", text: $store.query)
                    .font(.subheadline)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.search)
                    .focused($searchFocused)

                if !store.query.isEmpty {
                    Button {
                        store.query = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .font(.system(size: 16))
                            .foregroundStyle(.tertiary)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Clear search")
                }

                filterButton
                PresenceAvatars()
                    .padding(.trailing, 8)
            }
            .frame(height: 44)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 22, style: .continuous))

            if canManage {
                Button {
                    showAddLeads = true
                } label: {
                    Image(systemName: "plus")
                        .font(.system(size: 17, weight: .medium))
                        .foregroundStyle(.primary)
                        .frame(width: 44, height: 44)
                        .background(Color(.secondarySystemBackground), in: Circle())
                }
                .buttonStyle(TapScaleStyle())
                .accessibilityLabel("Add leads")
            }

            Button {
                dismiss()
            } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(.primary)
                    .frame(width: 44, height: 44)
                    .background(Color(.secondarySystemBackground), in: Circle())
            }
            .buttonStyle(TapScaleStyle())
            .accessibilityLabel("Close leads")
        }
        .padding(.horizontal, 12)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }

    private var filterButton: some View {
        Button {
            showFilters = true
        } label: {
            ZStack(alignment: .topTrailing) {
                Image(systemName: "slider.horizontal.3")
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(store.hasFilters ? WTheme.accent : .secondary)
                    .frame(width: 32, height: 38)
                    .contentShape(Rectangle())
                if store.filters.activeCount > 0 {
                    Text("\(store.filters.activeCount)")
                        .font(.system(size: 9, weight: .bold))
                        .foregroundStyle(.white)
                        .frame(minWidth: 14, minHeight: 14)
                        .background(WTheme.accent, in: Circle())
                        .offset(x: 4, y: 1)
                } else if store.hasFilters {
                    Circle().fill(WTheme.accent).frame(width: 7, height: 7).offset(x: 2, y: 3)
                }
            }
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Filters")
    }

    // MARK: Selection header

    private var selectionHeader: some View {
        HStack(spacing: 12) {
            Button("Done") { store.exitSelection() }
                .fontWeight(.semibold)
            Spacer()
            Text("\(store.selectedCount) selected")
                .font(.subheadline.weight(.semibold))
                .monospacedDigit()
                .contentTransition(.numericText())
            Spacer()
            Button(store.allLoadedSelected ? "Clear" : "Select all") { store.selectAllLoaded() }
        }
        .padding(.horizontal, 16)
        .frame(height: 44)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }

    // MARK: Caption

    private var scopeCaption: some View {
        HStack(spacing: 6) {
            Text(captionTitle)
                .font(.caption.weight(.semibold))
                .tracking(0.9)
                .foregroundStyle(.secondary)
            if !store.isSearching, !store.hasFilters, store.scopeCount(store.scope) > 0 {
                Text(WFormat.compact(store.scopeCount(store.scope)))
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
                    .contentTransition(.numericText())
            }
            Spacer()
            if (store.isSearching || store.hasFilters), store.hasLoaded, let count = store.totalCount {
                Text("\(count) found")
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                    .contentTransition(.numericText())
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, 8)
        .padding(.bottom, 2)
    }

    private var captionTitle: String {
        if store.isSearching { return "SEARCH RESULTS" }
        if store.hasFilters { return "FILTERED" }
        return store.scope.title.uppercased()
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { SkeletonRows(rows: 10) }
        } else if let error = store.errorMessage, store.leads.isEmpty {
            ErrorStateView(title: "Couldn't load leads", message: error) {
                await store.load(env.api, campaignID: campaignID)
            }
        } else if store.leads.isEmpty {
            emptyState
        } else {
            List {
                ForEach(store.leads) { lead in
                    row(lead)
                }
                if store.hasMore {
                    HStack(spacing: 8) {
                        Spacer()
                        ProgressView().controlSize(.small)
                        Text("Loading more…").font(.footnote).foregroundStyle(.secondary)
                        Spacer()
                    }
                    .padding(.vertical, 6)
                    .listRowSeparator(.hidden)
                    .onAppear { Task { await store.loadMore(env.api, campaignID: campaignID) } }
                } else {
                    endMarker
                }
            }
            .listStyle(.plain)
            .scrollDismissesKeyboard(.immediately)
            .refreshable { await store.load(env.api, campaignID: campaignID) }
        }
    }

    private var endMarker: some View {
        let count = store.totalCount ?? store.leads.count
        return HStack {
            Spacer()
            Text("\(count) lead\(count == 1 ? "" : "s")")
                .font(.footnote)
                .monospacedDigit()
                .foregroundStyle(.tertiary)
            Spacer()
        }
        .padding(.vertical, 10)
        .listRowSeparator(.hidden)
    }

    @ViewBuilder
    private var emptyState: some View {
        if store.isSearching || store.hasFilters {
            EmptyStateView(title: "No matching leads", message: "Try a different search or loosen your filters.")
        } else {
            switch store.scope {
            case .all:
                if canManage {
                    EmptyStateView(
                        title: "No leads yet",
                        message: "Add contacts to this campaign to start sending.",
                        ctaTitle: "Add leads"
                    ) {
                        showAddLeads = true
                    }
                } else {
                    EmptyStateView(title: "No leads yet", message: "Leads added to this campaign show up here.")
                }
            case .processing:
                EmptyStateView(title: "Nothing sending right now", message: "Leads mid-sequence show up here while the campaign is active.")
            case .replied:
                EmptyStateView(title: "No replies yet", message: "Leads who reply land here.")
            case .bounced:
                EmptyStateView(title: "No bounces", message: "Hard-bounced leads show up here.")
            case .queued:
                EmptyStateView(title: "Nothing queued", message: "Leads waiting for their first send appear here.")
            case .unsubscribed:
                EmptyStateView(title: "Nobody unsubscribed", message: "Unsubscribed leads show up here.")
            }
        }
    }

    // MARK: Row

    private func row(_ lead: CampaignLead) -> some View {
        Button {
            if store.selectionMode {
                store.toggleSelected(lead.id)
            } else {
                openLead = lead
            }
        } label: {
            CampaignLeadRow(
                lead: lead,
                selecting: store.selectionMode,
                selected: store.isSelected(lead.id)
            )
        }
        .buttonStyle(.plain)
        .listRowBackground(store.isSelected(lead.id) ? Tone.sky.background : Color(.systemBackground))
        .listRowInsets(EdgeInsets(top: 4, leading: 14, bottom: 4, trailing: 16))
        .simultaneousGesture(
            LongPressGesture(minimumDuration: 0.4).onEnded { _ in
                if !store.selectionMode, canManage { store.enterSelection(with: lead.id) }
            }
        )
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if canManage, !store.selectionMode {
                Button(role: .destructive) {
                    Task { try? await store.removeFromCampaign(env.api, campaignID: campaignID, ids: [lead.id]) }
                } label: {
                    Label("Remove", systemImage: "person.badge.minus")
                }
                Button {
                    store.enterSelection(with: lead.id)
                } label: {
                    Label("Select", systemImage: "checkmark.circle")
                }
                .tint(WTheme.accent)
            }
        }
    }

    // MARK: Drawer

    private func drawer(topInset: CGFloat) -> some View {
        CampaignLeadsSidebar(
            store: store,
            campaignName: campaignName,
            selection: store.scope,
            topInset: topInset,
            revealed: sidebarOpen,
            onSelect: { scope in
                withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.scope = scope }
                Task {
                    try? await Task.sleep(for: .milliseconds(280))
                    closeSidebar()
                }
            }
        )
        .frame(width: Self.sidebarWidth)
        .frame(maxHeight: .infinity)
        .background(Color(.systemBackground))
        .clipShape(UnevenRoundedRectangle(bottomTrailingRadius: 26, topTrailingRadius: 26, style: .continuous))
        .shadow(color: .black.opacity(sidebarOpen ? 0.22 : 0), radius: 30, x: 6, y: 0)
        .ignoresSafeArea()
        .offset(x: drawerOffset)
        .gesture(
            DragGesture()
                .onChanged { value in sidebarDrag = min(0, value.translation.width) }
                .onEnded { value in
                    if value.translation.width < -80 || value.predictedEndTranslation.width < -160 {
                        closeSidebar()
                    } else {
                        withAnimation(.spring(response: 0.32, dampingFraction: 0.86)) { sidebarDrag = 0 }
                    }
                }
        )
    }

    private var drawerOffset: CGFloat {
        (sidebarOpen ? 0 : -Self.sidebarWidth - 40) + sidebarDrag
    }

    private func openSidebar() {
        searchFocused = false
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) { sidebarOpen = true }
    }

    private func closeSidebar() {
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) {
            sidebarOpen = false
            sidebarDrag = 0
        }
    }
}

// MARK: - Row view

struct CampaignLeadRow: View {
    let lead: CampaignLead
    var selecting: Bool = false
    var selected: Bool = false

    private var progress: CampaignLeadProgress? { lead.progress }

    var body: some View {
        HStack(spacing: 12) {
            if selecting {
                Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                    .font(.system(size: 22))
                    .foregroundStyle(selected ? WTheme.accent : Color(.tertiaryLabel))
                    .transition(.scale.combined(with: .opacity))
            }
            WAvatar(name: lead.displayName, seed: lead.id, size: 42)
            VStack(alignment: .leading, spacing: 3) {
                Text(lead.displayName)
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                secondaryLine
                engagementLine
            }
            Spacer(minLength: 8)
            trailing
        }
        .padding(.vertical, 6)
        .contentShape(Rectangle())
    }

    /// One secondary fact: the email (when the title is a name), else the step.
    @ViewBuilder
    private var secondaryLine: some View {
        HStack(spacing: 5) {
            if lead.hasName, let email = lead.email, !email.isEmpty {
                Text(email).lineLimit(1)
            } else if let step = progress?.currentStep, !step.isEmpty {
                Text(step).lineLimit(1)
            } else if let company = lead.company, !company.isEmpty {
                Text(company).lineLimit(1)
            }
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
    }

    /// Current step + compact engagement counters, only what's non-zero.
    @ViewBuilder
    private var engagementLine: some View {
        if let progress {
            HStack(spacing: 8) {
                if lead.hasName, lead.email?.isEmpty == false, let step = progress.currentStep, !step.isEmpty {
                    Label(step, systemImage: "arrow.turn.down.right")
                        .labelStyle(.titleAndIcon)
                        .lineLimit(1)
                }
                counter("envelope.open.fill", progress.opened, .indigo)
                counter("cursorarrow.click", progress.clicked, .amber)
                counter("arrowshape.turn.up.left.fill", progress.replied, .emerald)
            }
            .font(.caption2)
            .foregroundStyle(.tertiary)
        }
    }

    @ViewBuilder
    private func counter(_ symbol: String, _ value: Int?, _ tone: Tone) -> some View {
        if let value, value > 0 {
            HStack(spacing: 2) {
                Image(systemName: symbol).font(.system(size: 9, weight: .semibold))
                Text("\(value)").monospacedDigit()
            }
            .foregroundStyle(tone.color)
        }
    }

    @ViewBuilder
    private var trailing: some View {
        if let progress {
            VStack(alignment: .trailing, spacing: 4) {
                StatusPill(
                    text: progress.statusLabel,
                    tone: progress.statusTone,
                    pulsing: progress.status == "active"
                )
                if let last = progress.lastActivityAt {
                    Text(WFormat.relative(last))
                        .font(.caption2)
                        .monospacedDigit()
                        .foregroundStyle(.tertiary)
                }
            }
        }
    }
}

// MARK: - Selection bar

/// Floating bottom-center bar for multi-selected leads: a count plus bulk edit
/// (membership/subscription) and remove-from-campaign.
struct CampaignLeadSelectionBar: View {
    let count: Int
    let onEdit: () -> Void
    let onRemove: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Text("\(count)")
                .font(.subheadline.weight(.bold))
                .monospacedDigit()
                .foregroundStyle(.white)
                .frame(minWidth: 26, minHeight: 26)
                .background(WTheme.accent, in: Circle())
                .contentTransition(.numericText())
            Text("selected")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Spacer(minLength: 8)
            Button(action: onEdit) {
                Label("Edit", systemImage: "slider.horizontal.3")
                    .font(.subheadline.weight(.semibold))
            }
            .buttonStyle(.borderedProminent)
            .tint(WTheme.accent)
            .controlSize(.small)
            .disabled(count == 0)
            Button(role: .destructive, action: onRemove) {
                Image(systemName: "person.badge.minus")
                    .font(.system(size: 15, weight: .semibold))
                    .frame(width: 30, height: 30)
            }
            .buttonStyle(.bordered)
            .tint(WTheme.negative)
            .controlSize(.small)
            .disabled(count == 0)
        }
        .padding(.leading, 12)
        .padding(.trailing, 10)
        .padding(.vertical, 8)
        .background(.regularMaterial, in: Capsule())
        .overlay(Capsule().strokeBorder(Color(.separator).opacity(0.35), lineWidth: 1))
        .shadow(color: .black.opacity(0.14), radius: 14, y: 4)
        .padding(.horizontal, 20)
    }
}

// MARK: - Lead detail

/// Pushed per-lead detail scoped to this campaign: a sky hero with the lead's
/// status, the engagement funnel, current step + last activity, and actions to
/// open the full contact profile or remove the lead from the campaign.
struct CampaignLeadDetailView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let campaignID: String
    let lead: CampaignLead
    let store: CampaignLeadsStore

    @State private var confirmRemove = false

    /// Track the live store copy so the status pill / funnel update on the
    /// realtime reload, falling back to the pushed snapshot if the lead scrolls
    /// out of the loaded page window.
    private var live: CampaignLead { store.leads.first { $0.id == lead.id } ?? lead }
    private var progress: CampaignLeadProgress? { live.progress }
    private var canManage: Bool { env.session.can(.manageContacts) }
    private var presenceKey: String { "contact:\(lead.id)" }

    var body: some View {
        ScrollView {
            VStack(spacing: 0) {
                hero
                    .frame(maxWidth: .infinity, alignment: .leading)
                sheet
            }
        }
        .background(alignment: .top) { AirSkyWash().ignoresSafeArea(edges: .top) }
        .scrollContentBackground(.hidden)
        .toolbarBackground(.hidden, for: .navigationBar)
        .toolbarColorScheme(.dark, for: .navigationBar)
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .presenceResource(presenceKey)
        .confirmationDialog("Remove from campaign?", isPresented: $confirmRemove, titleVisibility: .visible) {
            Button("Remove \(live.displayName)", role: .destructive) {
                Task {
                    try? await store.removeFromCampaign(env.api, campaignID: campaignID, ids: [lead.id])
                    dismiss()
                }
            }
        } message: {
            Text("They stay in your contacts and keep their history. Sending to them stops.")
        }
    }

    // MARK: Hero

    private var hero: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .center, spacing: 14) {
                WAvatar(name: live.displayName, seed: lead.id, size: 60, onSky: true)
                    .shadow(color: Color(hex: 0x0C4A6E).opacity(0.28), radius: 10, y: 4)
                VStack(alignment: .leading, spacing: 3) {
                    Text(live.displayName)
                        .font(.title2.bold())
                        .foregroundStyle(.white)
                        .lineLimit(1)
                        .minimumScaleFactor(0.75)
                    if live.hasName, let email = live.email, !email.isEmpty {
                        Text(email)
                            .font(.subheadline)
                            .foregroundStyle(.white.opacity(0.75))
                            .lineLimit(1)
                            .textSelection(.enabled)
                    }
                    if let company = live.company, !company.isEmpty {
                        Text(company)
                            .font(.subheadline)
                            .foregroundStyle(.white.opacity(0.72))
                            .lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
            }
            if let progress {
                StatusPill(
                    text: progress.statusLabel,
                    tone: progress.statusTone,
                    pulsing: progress.status == "active"
                )
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, 4)
        .padding(.bottom, 18)
    }

    // MARK: Sheet

    private var sheet: some View {
        VStack(alignment: .leading, spacing: 0) {
            funnelSection
            Divider().padding(.horizontal, 20)
            detailRows
            Divider().padding(.horizontal, 20)
            actions
        }
        .padding(.top, 18)
        .padding(.bottom, 40)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            UnevenRoundedRectangle(topLeadingRadius: 26, topTrailingRadius: 26, style: .continuous)
                .fill(Color(.systemBackground))
                .ignoresSafeArea(edges: .bottom)
                .shadow(color: .black.opacity(0.12), radius: 18, y: -4)
        )
    }

    @ViewBuilder
    private var funnelSection: some View {
        if let progress {
            VStack(alignment: .leading, spacing: 10) {
                EyebrowLabel("Engagement")
                    .padding(.horizontal, 20)
                HStack(spacing: 10) {
                    ForEach(Array(progress.funnel.enumerated()), id: \.offset) { _, item in
                        funnelStat(item.label, item.value, item.tone)
                    }
                }
                .padding(.horizontal, 16)
            }
        }
    }

    private func funnelStat(_ label: String, _ value: Int, _ tone: Tone) -> some View {
        VStack(spacing: 4) {
            Text(WFormat.compact(value))
                .font(.system(size: 20, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(value > 0 ? tone.color : Color.secondary)
            Text(label)
                .font(.caption2.weight(.medium))
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 12)
        .background(tone.background.opacity(value > 0 ? 1 : 0.5), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    @ViewBuilder
    private var detailRows: some View {
        VStack(spacing: 0) {
            if let step = progress?.currentStep, !step.isEmpty {
                infoRow(icon: "arrow.turn.down.right", tone: .indigo, title: "Current step", value: step)
            }
            if let last = progress?.lastActivityAt {
                infoRow(icon: "clock.fill", tone: .sky, title: "Last activity", value: last.formatted(.relative(presentation: .named)))
            }
            infoRow(
                icon: live.subscribed == false ? "bell.slash.fill" : "checkmark.seal.fill",
                tone: live.subscribed == false ? .slate : .emerald,
                title: "Subscription",
                value: live.subscribed == false ? "Unsubscribed" : "Subscribed"
            )
            if let added = live.createdAt {
                infoRow(icon: "calendar", tone: .slate, title: "Added", value: added.formatted(date: .abbreviated, time: .omitted))
            }
        }
        .padding(.vertical, 6)
    }

    private func infoRow(icon: String, tone: Tone, title: String, value: String) -> some View {
        HStack(spacing: 12) {
            IconTile(symbol: icon, tone: tone, size: 34)
            Text(title)
                .font(.subheadline.weight(.medium))
            Spacer(minLength: 8)
            Text(value)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 10)
    }

    @ViewBuilder
    private var actions: some View {
        VStack(spacing: 0) {
            NavigationLink {
                ContactDetailView(contact: Contact(seed: lead))
            } label: {
                actionRow(icon: "person.crop.circle", tone: .sky, title: "Open full contact", showsChevron: true)
            }
            .buttonStyle(.plain)
            if canManage {
                Button(role: .destructive) {
                    confirmRemove = true
                } label: {
                    actionRow(icon: "person.badge.minus", tone: .rose, title: "Remove from campaign", destructive: true)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.top, 4)
    }

    private func actionRow(icon: String, tone: Tone, title: String, destructive: Bool = false, showsChevron: Bool = false) -> some View {
        HStack(spacing: 12) {
            IconTile(symbol: icon, tone: tone, size: 34)
            Text(title)
                .font(.body.weight(.medium))
                .foregroundStyle(destructive ? WTheme.negative : Color.primary)
            Spacer(minLength: 8)
            if showsChevron {
                Image(systemName: "chevron.right")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 11)
        .contentShape(Rectangle())
    }
}

// MARK: - Contact seed

extension Contact {
    /// A partial contact seeded from a lead row, so the full contact detail can
    /// push instantly and refresh itself from GET /contacts/:id.
    init(seed lead: CampaignLead) {
        self.init(
            id: lead.id,
            firstName: lead.firstName,
            lastName: lead.lastName,
            email: lead.email,
            company: lead.company,
            phone: nil,
            customFields: nil,
            subscribed: lead.subscribed,
            campaigns: nil,
            categories: nil,
            campaignLead: nil,
            updatedAt: nil,
            createdAt: lead.createdAt,
            engagement: nil,
            suppression: nil
        )
    }
}
