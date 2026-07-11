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
    @State private var sidebarDrag: CGFloat = 0
    @State private var showCreate = false
    @State private var showFilters = false
    @State private var showBulkEdit = false
    @State private var confirmBulkDelete = false
    @FocusState private var searchFocused: Bool

    private static let sidebarWidth: CGFloat = 300

    private var canManage: Bool { env.session.can(.manageContacts) }
    private var pulseKey: Int { env.realtime.pulse(for: .contacts) }

    /// The member's contact categories (session), in position order.
    private var drawerCategories: [ContactCategory] { categoryStore.categories }

    var body: some View {
        NavigationStack(path: $path) {
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
            .toolbarVisibility(sidebarOpen || store.selectionMode ? .hidden : .automatic, for: .tabBar)
            .navigationDestination(for: Contact.self) { contact in
                ContactDetailView(contact: contact) { updated in
                    store.apply(updated)
                } onDeleted: { id in
                    store.remove(id: id)
                }
            }
            .fullScreenCover(isPresented: $showCreate) {
                ContactAddFlow(store: store, categoryStore: categoryStore) { created in
                    if let first = created.first { path.append(first) }
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
            .confirmationDialog(
                "Delete \(store.selectedCount) contact\(store.selectedCount == 1 ? "" : "s")?",
                isPresented: $confirmBulkDelete,
                titleVisibility: .visible
            ) {
                Button("Delete \(store.selectedCount)", role: .destructive) {
                    Task {
                        try? await store.bulkDelete(env.api, ids: Array(store.selected))
                        store.exitSelection()
                    }
                }
            } message: {
                Text("This removes them from your workspace. This can't be undone.")
            }
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
        .background(Color(.systemBackground))
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
                .accessibilityLabel("Open contacts menu")

                TextField("Search contacts", text: $store.query)
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
                    showCreate = true
                } label: {
                    Image(systemName: "plus")
                        .font(.system(size: 17, weight: .medium))
                        .foregroundStyle(.primary)
                        .frame(width: 44, height: 44)
                        .background(Color(.secondarySystemBackground), in: Circle())
                }
                .buttonStyle(TapScaleStyle())
                .accessibilityLabel("New contact")
            }
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

    // MARK: Scope caption

    private var scopeCaption: some View {
        HStack(spacing: 6) {
            Text(captionTitle)
                .font(.caption.weight(.semibold))
                .tracking(0.9)
                .foregroundStyle(.secondary)
            if !store.isSearching, !store.hasFilters, let count = scopeCount, count > 0 {
                Text(WFormat.compact(count))
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
        if store.isLoading, !store.hasLoaded {
            ScrollView { SkeletonRows(rows: 10) }
        } else if let error = store.errorMessage, store.contacts.isEmpty {
            ErrorStateView(title: "Couldn't load contacts", message: error) {
                await store.load(env.api)
            }
        } else if store.contacts.isEmpty {
            emptyState
        } else {
            List {
                ForEach(store.contacts) { contact in
                    row(contact)
                }
                if store.hasMore {
                    HStack(spacing: 8) {
                        Spacer()
                        ProgressView().controlSize(.small)
                        Text("Loading more…")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        Spacer()
                    }
                    .padding(.vertical, 6)
                    .listRowSeparator(.hidden)
                    .onAppear {
                        Task { await store.loadMore(env.api) }
                    }
                } else {
                    endMarker
                }
            }
            .listStyle(.plain)
            .scrollDismissesKeyboard(.immediately)
            .refreshable { await store.load(env.api) }
        }
    }

    /// End-of-list marker: the exact total for this scope/search.
    private var endMarker: some View {
        let count = store.totalCount ?? store.contacts.count
        return HStack {
            Spacer()
            Text("\(count) contact\(count == 1 ? "" : "s")")
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

    // MARK: Row

    private func row(_ contact: Contact) -> some View {
        Button {
            if store.selectionMode {
                store.toggleSelected(contact.id)
            } else {
                path.append(contact)
            }
        } label: {
            ContactRow(
                contact: contact,
                selecting: store.selectionMode,
                selected: store.isSelected(contact.id)
            )
        }
        .buttonStyle(.plain)
        .listRowBackground(store.isSelected(contact.id) ? Tone.sky.background : Color(.systemBackground))
        .listRowInsets(EdgeInsets(top: 4, leading: 14, bottom: 4, trailing: 16))
        .simultaneousGesture(
            LongPressGesture(minimumDuration: 0.4).onEnded { _ in
                if !store.selectionMode, canManage { store.enterSelection(with: contact.id) }
            }
        )
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if canManage, !store.selectionMode {
                Button(role: .destructive) {
                    Task { try? await store.delete(env.api, id: contact.id) }
                } label: {
                    Label("Delete", systemImage: "trash")
                }
                Button {
                    store.enterSelection(with: contact.id)
                } label: {
                    Label("Select", systemImage: "checkmark.circle")
                }
                .tint(WTheme.accent)
            }
        }
    }

    // MARK: Drawer

    private func drawer(topInset: CGFloat) -> some View {
        ContactsSidebar(
            store: store,
            categories: drawerCategories,
            selection: store.scope,
            topInset: topInset,
            revealed: sidebarOpen
        ) { scope in
            withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.scope = scope }
            // Let the highlight capsule slide to the tapped row before closing.
            Task {
                try? await Task.sleep(for: .milliseconds(280))
                closeSidebar()
            }
        }
        .frame(width: Self.sidebarWidth)
        .frame(maxHeight: .infinity)
        .background(Color(.systemBackground))
        .clipShape(UnevenRoundedRectangle(bottomTrailingRadius: 26, topTrailingRadius: 26, style: .continuous))
        .shadow(color: .black.opacity(sidebarOpen ? 0.22 : 0), radius: 30, x: 6, y: 0)
        .ignoresSafeArea()
        .offset(x: drawerOffset)
        .gesture(
            DragGesture()
                .onChanged { value in
                    sidebarDrag = min(0, value.translation.width)
                }
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

// MARK: - Row

struct ContactRow: View {
    let contact: Contact
    var selecting: Bool = false
    var selected: Bool = false

    var body: some View {
        HStack(spacing: 12) {
            if selecting {
                Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                    .font(.system(size: 22))
                    .foregroundStyle(selected ? WTheme.accent : Color(.tertiaryLabel))
                    .transition(.scale.combined(with: .opacity))
            }
            WAvatar(name: contact.displayName, seed: contact.id, size: 42)
            VStack(alignment: .leading, spacing: 3) {
                Text(contact.displayName)
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                secondaryLine
            }
            Spacer(minLength: 8)
            trailing
        }
        .padding(.vertical, 6)
        .contentShape(Rectangle())
    }

    /// One secondary fact only: the email, or the company when the row's
    /// title already is the email. Two truncated facts read worse than one.
    @ViewBuilder
    private var secondaryLine: some View {
        HStack(spacing: 6) {
            if contact.hasName, let email = contact.email, !email.isEmpty {
                Text(email)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            } else if let company = contact.company, !company.isEmpty {
                Text(company)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            categoryDots
        }
    }

    @ViewBuilder
    private var categoryDots: some View {
        if let categories = contact.categories, !categories.isEmpty {
            HStack(spacing: 3) {
                ForEach(categories.prefix(4)) { category in
                    Circle()
                        .fill(ContactColor.dot(category.color, seed: category.id))
                        .frame(width: 6, height: 6)
                }
            }
        }
    }

    @ViewBuilder
    private var trailing: some View {
        if let lead = contact.campaignLead {
            StatusPill(
                text: ContactLeadStatus.label(lead.status),
                tone: ContactLeadStatus.tone(lead.status),
                pulsing: ContactLeadStatus.isLive(lead.status)
            )
        } else if contact.subscribed == false {
            StatusPill(text: "Unsubscribed", tone: .slate)
        } else if let count = contact.campaigns?.count, count > 0 {
            Text("\(count) campaign\(count == 1 ? "" : "s")")
                .font(.caption)
                .monospacedDigit()
                .foregroundStyle(.tertiary)
        }
    }
}
