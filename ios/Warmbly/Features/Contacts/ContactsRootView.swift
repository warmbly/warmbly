import SwiftUI

/// Contacts tab root, matching the campaigns/unibox browser: a compact
/// search-pill header with a hamburger that slides in the navigation drawer
/// (browse scopes + categories with counts), a dense full-width hairline list,
/// and server-side filtering so every scope has correct totals and infinite
/// scroll. Full control: advanced criteria filters, multi-select bulk edit and
/// delete, a stepped onboarding add flow, and rich individual editing.
struct ContactsRootView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = ContactsStore()
    @State private var categoryStore = ContactCategoryStore()
    @State private var path: [Contact] = []
    @State private var sidebarOpen = false
    /// What the detail column shows when the list and detail sit side by side.
    @State private var openContact: Contact?
    @State private var layout = SplitMetrics.stacked
    @State private var showCreate = false
    @State private var showFilters = false
    @State private var showBulkEdit = false
    @State private var confirmBulkDelete = false
    @State private var pendingDelete: Contact?
    @State private var editing: Contact?
    @State private var actionError: String?
    @State private var rowsShown = false
    @FocusState private var searchFocused: Bool

    private static let sidebarWidth: CGFloat = 300

    private var canManage: Bool { env.session.can(.manageContacts) }
    private var pulseKey: Int { env.realtime.pulse(for: .contacts) }

    /// The member's contact categories (session), in position order.
    private var drawerCategories: [ContactCategory] { categoryStore.categories }

    var body: some View {
        ListDetailShell(
            path: $path,
            openItem: $openContact,
            sidebarOpen: $sidebarOpen,
            layout: $layout,
            sidebarWidth: Self.sidebarWidth,
            drawerRadius: 22,
            dimOpacity: 0.22,
            placeholder: DetailPlaceholder(
                title: "No contact open",
                message: "Pick a contact to see their journey, notes and campaigns here.",
                symbol: "person.2"
            )
        ) {
            mainPane
                .toolbar(.hidden, for: .navigationBar)
                .tabBarHidden(sidebarOpen || store.selectionMode)
                .navigationDestination(for: Contact.self) { contact in
                    detail(contact)
                }
        } drawer: { topInset, revealed in
            sidebar(topInset: topInset, revealed: revealed)
        } detail: { contact in
            detail(contact)
        }
        .fullScreenCover(isPresented: $showCreate) {
            ContactAddFlow(store: store, categoryStore: categoryStore) { created in
                if let first = created.first { open(first) }
            }
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
                    await store.load(env.api)
                    store.exitSelection()
                }
            )
        }
        .sheet(item: $editing) { contact in
            ContactBulkEditSheet(
                ids: [contact.id],
                categories: categoryStore.categories,
                campaigns: store.campaignOptions,
                loadCampaigns: { await store.loadCampaignOptions(env.api) },
                perform: { body in
                    try await store.bulkEdit(env.api, body: body)
                    await store.load(env.api)
                }
            )
        }
        .confirmationDialog(
            "Delete \(pendingDelete?.displayName ?? "contact")?",
            isPresented: Binding(get: { pendingDelete != nil }, set: { if !$0 { pendingDelete = nil } }),
            titleVisibility: .visible,
            presenting: pendingDelete
        ) { contact in
            Button("Delete contact", role: .destructive) {
                Task {
                    do {
                        try await store.delete(env.api, id: contact.id)
                        closeDeleted([contact.id])
                    } catch {
                        actionError = error.localizedDescription
                    }
                }
            }
        } message: { _ in
            Text("This removes them from your workspace and every campaign. This can't be undone.")
        }
        .alert("Couldn't update contact", isPresented: Binding(get: { actionError != nil }, set: { if !$0 { actionError = nil } })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(actionError ?? "")
        }
        .confirmationDialog(
            "Delete \(store.selectedCount) contact\(store.selectedCount == 1 ? "" : "s")?",
            isPresented: $confirmBulkDelete,
            titleVisibility: .visible
        ) {
            Button("Delete \(store.selectedCount)", role: .destructive) {
                Task {
                    let ids = store.selected
                    do {
                        try await store.bulkDelete(env.api, ids: Array(ids))
                        closeDeleted(ids)
                    } catch {
                        actionError = error.localizedDescription
                    }
                    store.exitSelection()
                }
            }
        } message: {
            Text("This removes them from your workspace. This can't be undone.")
        }
        .task(id: store.query) {
            // First run loads immediately; subsequent runs debounce typing.
            if store.hasLoaded {
                try? await Task.sleep(for: .milliseconds(350))
            }
            guard !Task.isCancelled else { return }
            await store.load(env.api)
        }
        .task { await categoryStore.load(env.api) }
        .onChange(of: store.scope) {
            store.exitSelection()
            Task { await store.load(env.api) }
        }
        .onChange(of: store.filters) {
            store.exitSelection()
            Task { await store.load(env.api) }
        }
        .onChange(of: pulseKey) {
            Task { await store.load(env.api) }
        }
        .onChange(of: env.realtime.pulse(for: .me)) {
            Task { await categoryStore.load(env.api) }
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
        #if DEBUG
        // Scripted debug runs can open the drawer (WARMBLY_DEBUG_DRAWER=1).
        .onAppear {
            let debug = ProcessInfo.processInfo.environment
            if debug["WARMBLY_DEBUG_DRAWER"] != nil { openSidebar() }
            if let raw = debug["WARMBLY_DEBUG_SORT"], let sort = ContactSort(rawValue: raw) { store.filters.sortBy = sort }
        }
        // WARMBLY_DEBUG_CONTACT opens the contact whose name or address contains it.
        .onChange(of: store.hasLoaded) { _, loaded in
            guard loaded, path.isEmpty, openContact == nil, let match = ProcessInfo.processInfo.environment["WARMBLY_DEBUG_CONTACT"],
                  let contact = store.contacts.first(where: { $0.displayName.localizedCaseInsensitiveContains(match) || ($0.email ?? "").localizedCaseInsensitiveContains(match) })
            else { return }
            open(contact)
        }
        #endif
        .background(WTheme.canvas)
        .overlay(alignment: .bottom) {
            if store.selectionMode {
                ContactSelectionBar(
                    count: store.selectedCount,
                    onEdit: { showBulkEdit = true },
                    onDelete: { confirmBulkDelete = true }
                )
                .padding(.bottom, 10)
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .simultaneousGesture(
            DragGesture(minimumDistance: 25)
                .onEnded { value in
                    // Gmail's edge swipe: open the drawer from the left edge.
                    if !sidebarOpen, !store.selectionMode, value.startLocation.x < 44, value.translation.width > 70 {
                        openSidebar()
                    }
                }
        )
    }

    // MARK: Header

    private var searchBar: some View {
        @Bindable var store = store
        return GlassHeaderBar(
            title: store.isNarrowed ? "Filtered" : store.scope.title,
            subtitle: subtitle,
            query: $store.query,
            searchPrompt: "Search contacts",
            searchFocused: $searchFocused,
            menuLabel: "Open contacts menu",
            onMenu: openSidebar
        ) {
            filterButton
            if canManage {
                HeaderPrimaryButton(symbol: "plus", label: "New contact") { showCreate = true }
            }
        }
    }

    private var subtitle: Text? {
        if store.isNarrowed, store.hasLoaded, let count = store.totalCount {
            return Text("\(WFormat.compact(count)) found")
        }
        guard let count = scopeCount else { return nil }
        return Text("\(WFormat.compact(count)) contact\(count == 1 ? "" : "s")")
    }

    private var filterButton: some View {
        GlassCircleButton(symbol: "line.3.horizontal.decrease", label: "Filters", tint: store.hasFilters ? WTheme.accent : WTheme.ink) {
            showFilters = true
        }
        .overlay(alignment: .topTrailing) {
            if store.filters.activeCount > 0 {
                Text("\(store.filters.activeCount)")
                    .font(.system(size: 10.5, weight: .bold))
                    .monospacedDigit()
                    .foregroundStyle(.white)
                    .frame(minWidth: 17, minHeight: 17)
                    .background(WTheme.accent, in: Circle())
                    .offset(x: 3, y: -3)
                    .transition(.scale.combined(with: .opacity))
            }
        }
    }

    // MARK: Selection header

    private var selectionHeader: some View {
        HStack(spacing: 12) {
            Button("Done") { store.exitSelection() }
                .buttonStyle(InkButtonStyle(size: .small))
            Spacer()
            Text("\(store.selectedCount) selected")
                .font(.system(size: 15, weight: .semibold))
                .monospacedDigit()
                .foregroundStyle(WTheme.ink)
                .contentTransition(.numericText())
            Spacer()
            Button(store.allLoadedSelected ? "Clear" : "Select all") { store.selectAllLoaded() }
                .buttonStyle(OutlineButtonStyle(size: .small))
        }
        .padding(.horizontal, 12)
        .frame(height: 44)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }

    // MARK: Scope caption

    @ViewBuilder
    private var scopeCaption: some View {
        if store.isSearching, store.hasLoaded {
            SearchResultsLine(count: store.totalCount ?? 0)
        }
    }

    /// The drawer count matching the current scope, for the caption.
    private var scopeCount: Int? {
        switch store.scope {
        case .all: store.allCount
        case .subscribed: store.subscribedCount
        case .unsubscribed: store.unsubscribedCount
        case .inCampaign: store.inCampaignCount
        case .notContacted: store.notContactedCount
        case let .category(id, _): store.categoryCount(id)
        }
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if !store.hasLoaded, store.errorMessage == nil {
            ScrollView { SkeletonRows(rows: 10) }
        } else if let error = store.errorMessage, store.contacts.isEmpty {
            ErrorStateView(title: "Couldn't load contacts", message: error) {
                await store.load(env.api)
            }
        } else if store.contacts.isEmpty {
            emptyState
        } else {
            List {
                ForEach(ContactSection.build(store.contacts, sort: store.filters.sortBy)) { section in
                    Section {
                        ForEach(Array(section.contacts.enumerated()), id: \.element.id) { offset, contact in
                            row(contact)
                                .reveal(rowsShown, min(section.start + offset, 8))
                        }
                    } header: {
                        if let title = section.title { sectionHeader(title) }
                    }
                }
                if store.hasMore {
                    HStack(spacing: 8) {
                        Spacer()
                        ProgressView().controlSize(.small)
                        Text("Loading more…")
                            .font(WFont.meta(11, weight: .medium))
                            .foregroundStyle(WTheme.inkTertiary)
                        Spacer()
                    }
                    .padding(.vertical, 6)
                    .listRowSeparator(.hidden)
                    .listRowBackground(WTheme.canvas)
                    .onAppear {
                        Task { await store.loadMore(env.api) }
                    }
                } else {
                    endMarker
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .background(WTheme.canvas)
            .scrollDismissesKeyboard(.immediately)
            .refreshable { await store.load(env.api) }
            .listSectionSpacing(0)
            .environment(\.defaultMinListHeaderHeight, 0)
            .onAppear { rowsShown = true }
        }
    }

    /// End-of-list marker: the exact total for this scope/search.
    private var endMarker: some View {
        let count = store.totalCount ?? store.contacts.count
        return HStack {
            Spacer()
            Text("\(count) contact\(count == 1 ? "" : "s")")
                .font(.system(size: 13))
                .monospacedDigit()
                .foregroundStyle(WTheme.inkTertiary)
            Spacer()
        }
        .padding(.top, 14)
        .padding(.bottom, 90)
        .listRowSeparator(.hidden)
        .listRowBackground(WTheme.canvas)
    }

    @ViewBuilder
    private var emptyState: some View {
        if store.isSearching || store.hasFilters {
            EmptyStateView(title: "No matching contacts", message: "Try a different search or loosen your filters.")
        } else {
            switch store.scope {
            case .subscribed:
                EmptyStateView(title: "No subscribed contacts", message: "Subscribed contacts show up here.")
            case .unsubscribed:
                EmptyStateView(title: "Nobody unsubscribed", message: "Unsubscribed contacts show up here.")
            case .inCampaign:
                EmptyStateView(title: "No one in a campaign", message: "Contacts added to a campaign show up here.")
            case .notContacted:
                EmptyStateView(title: "Everyone's in a campaign", message: "Contacts not yet in any campaign show up here.")
            case .category:
                EmptyStateView(title: "Category is empty", message: "Tag contacts with this category to see them here.")
            case .all:
                if canManage {
                    EmptyStateView(
                        title: "No contacts yet",
                        message: "Add contacts here, or import a list on the web dashboard.",
                        ctaTitle: "Add contacts"
                    ) {
                        showCreate = true
                    }
                } else {
                    EmptyStateView(
                        title: "No contacts yet",
                        message: "Contacts your team adds will show up here."
                    )
                }
            }
        }
    }

    /// A sort heading that stays pinned while its rows scroll under it.
    private func sectionHeader(_ title: String) -> some View {
        Text(title)
            .font(.system(size: 13.5, weight: .semibold))
            .foregroundStyle(WTheme.inkSecondary)
            .textCase(nil)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 20)
            .padding(.top, 10)
            .padding(.bottom, 6)
            .background(WTheme.canvas)
            .listRowInsets(EdgeInsets())
    }

    // MARK: Row

    private func row(_ contact: Contact) -> some View {
        Button {
            if store.selectionMode {
                store.toggleSelected(contact.id)
            } else {
                open(contact)
            }
        } label: {
            ContactRow(
                contact: contact,
                selecting: store.selectionMode,
                selected: store.isSelected(contact.id)
            )
        }
        .buttonStyle(TapScaleStyle())
        .listRowBackground(store.isSelected(contact.id) ? WTheme.fill : isOpen(contact) ? WTheme.accentSoft : WTheme.canvas)
        .listRowSeparatorTint(WTheme.hairline)
        .listRowInsets(EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16))
        .swipeActions(edge: .leading, allowsFullSwipe: true) {
            if canManage, !store.selectionMode {
                Button {
                    editing = contact
                } label: {
                    Label("Labels and campaigns", systemImage: "tag")
                }
                .tint(WTheme.accent)
            }
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if canManage, !store.selectionMode {
                Button {
                    pendingDelete = contact
                } label: {
                    Label("Delete", systemImage: "trash")
                }
                .tint(WTheme.negative)
                let subscribed = contact.subscribed != false
                Button {
                    Task {
                        do { try await store.setSubscribed(env.api, id: contact.id, !subscribed) } catch { actionError = error.localizedDescription }
                    }
                } label: {
                    Label(subscribed ? "Unsubscribe" : "Resubscribe", systemImage: subscribed ? "bell.slash" : "bell")
                }
                .tint(WTheme.warning)
                Button {
                    store.enterSelection(with: contact.id)
                } label: {
                    Label("Select", systemImage: "checkmark.circle")
                }
                .tint(WTheme.paused)
            }
        }
        .contextMenu {
            if canManage, !store.selectionMode {
                Button { editing = contact } label: { Label("Labels and campaigns", systemImage: "tag") }
                let subscribed = contact.subscribed != false
                Button {
                    Task {
                        do { try await store.setSubscribed(env.api, id: contact.id, !subscribed) } catch { actionError = error.localizedDescription }
                    }
                } label: {
                    Label(subscribed ? "Unsubscribe" : "Resubscribe", systemImage: subscribed ? "bell.slash" : "bell")
                }
                Button { store.enterSelection(with: contact.id) } label: { Label("Select", systemImage: "checkmark.circle") }
                Button(role: .destructive) { pendingDelete = contact } label: { Label("Delete", systemImage: "trash") }
            }
        }
    }

    // MARK: Drawer

    private func sidebar(topInset: CGFloat, revealed: Bool) -> some View {
        ContactsSidebar(
            store: store,
            categories: drawerCategories,
            selection: store.scope,
            topInset: topInset,
            revealed: revealed
        ) { scope in
            withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.scope = scope }
            // Let the highlight capsule slide to the tapped row before closing.
            Task {
                try? await Task.sleep(for: .milliseconds(280))
                closeSidebar()
            }
        }
    }

    private func detail(_ contact: Contact) -> some View {
        ContactDetailView(
            contact: contact,
            listSubscribed: store.contacts.first(where: { $0.id == contact.id }).map { $0.subscribed != false }
        ) { updated in
            store.apply(updated)
        } onDeleted: { id in
            store.remove(id: id)
        }
    }

    /// Deleted from the list: out of the detail column and the stack too, wherever a resize moved it.
    private func closeDeleted(_ ids: Set<String>) {
        if let open = openContact, ids.contains(open.id) { openContact = nil }
        path.removeAll { ids.contains($0.id) }
    }

    /// Pushes on a narrow window, fills the detail column on a wide one.
    private func open(_ contact: Contact) {
        if layout.showsDetail {
            openContact = contact
        } else {
            path.append(contact)
        }
    }

    private func isOpen(_ contact: Contact) -> Bool {
        layout.showsDetail && openContact?.id == contact.id
    }

    private func openSidebar() {
        guard !layout.pinsSidebar else { return }
        searchFocused = false
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) { sidebarOpen = true }
    }

    private func closeSidebar() {
        withAnimation(.spring(response: 0.34, dampingFraction: 0.86)) {
            sidebarOpen = false
        }
    }
}

/// Round hairline check that fills ink when on (row multi-select).
struct ContactCheckCircle: View {
    let on: Bool
    var size: CGFloat = 22

    var body: some View {
        ZStack {
            Circle()
                .strokeBorder(on ? WTheme.ink : WTheme.hairlineStrong, lineWidth: 1.5)
            if on {
                Circle().fill(WTheme.ink)
                Image(systemName: "checkmark")
                    .font(.system(size: size * 0.45, weight: .semibold))
                    .foregroundStyle(WTheme.inkInverse)
                    .transition(.scale.combined(with: .opacity))
            }
        }
        .frame(width: size, height: size)
        .animation(.spring(response: 0.28, dampingFraction: 0.7), value: on)
    }
}
