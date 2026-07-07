import SwiftUI

/// Contacts tab root: stat strip, category filter, dense hairline list backed
/// by POST /contacts/search, realtime reloads, search, create sheet.
struct ContactsRootView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = ContactsStore()
    @State private var categoryStore = ContactCategoryStore()
    @State private var path: [Contact] = []
    @State private var showCreate = false
    @State private var showCategoryFilter = false

    private var pulseKey: Int { env.realtime.pulse(for: .contacts) }

    var body: some View {
        @Bindable var store = store
        NavigationStack(path: $path) {
            VStack(spacing: 0) {
                filterBar
                list
            }
            .background(Color(.systemGroupedBackground))
            .navigationTitle("Contacts")
            .navigationSubtitle(store.displayTotal > 0 ? "\(WFormat.compact(store.displayTotal)) people" : "")
            .toolbar {
                ToolbarItemGroup(placement: .topBarTrailing) {
                    PresenceAvatars()
                    if env.session.can(.manageContacts) {
                        Button {
                            showCreate = true
                        } label: {
                            Image(systemName: "plus")
                        }
                        .accessibilityLabel("New contact")
                    }
                }
            }
            .navigationDestination(for: Contact.self) { contact in
                ContactDetailView(contact: contact) { updated in
                    store.apply(updated)
                } onDeleted: { id in
                    store.remove(id: id)
                }
            }
            .searchable(text: $store.query, prompt: "Search contacts")
            .sheet(isPresented: $showCreate) {
                ContactCreateSheet(store: store, categoryStore: categoryStore) { contact in
                    path.append(contact)
                }
            }
            .sheet(isPresented: $showCategoryFilter) {
                ContactCategoryFilterSheet(
                    categories: categoryStore.categories,
                    selected: $store.categoryFilter
                )
                .presentationDetents([.medium, .large])
            }
        }
        .task(id: filterSignature) {
            // First run loads immediately; subsequent runs debounce typing.
            if store.hasLoaded {
                try? await Task.sleep(for: .milliseconds(350))
            }
            guard !Task.isCancelled else { return }
            await store.load(env.api)
        }
        .task { await categoryStore.load(env.api) }
        .onChange(of: pulseKey) {
            Task { await store.load(env.api) }
        }
        .onChange(of: env.realtime.pulse(for: .me)) {
            Task { await categoryStore.load(env.api) }
        }
    }

    /// Reload whenever the query or category filter changes.
    private var filterSignature: String {
        "\(store.query)|\(store.categoryFilter?.id ?? "")"
    }

    // MARK: Filter bar

    private var filterBar: some View {
        HStack(spacing: 8) {
            Button {
                showCategoryFilter = true
            } label: {
                HStack(spacing: 6) {
                    if let filter = store.categoryFilter {
                        Circle()
                            .fill(ContactColor.dot(filter.color, seed: filter.id))
                            .frame(width: 8, height: 8)
                        Text(filter.title ?? "Category")
                            .lineLimit(1)
                    } else {
                        Image(systemName: "line.3.horizontal.decrease.circle")
                        Text("All categories")
                    }
                }
                .font(.subheadline.weight(.medium))
                .foregroundStyle(store.categoryFilter == nil ? Color.primary : WTheme.accent)
                .padding(.horizontal, 13)
                .padding(.vertical, 7.5)
                .background(
                    (store.categoryFilter == nil ? Tone.slate : Tone.sky).background,
                    in: Capsule()
                )
            }
            .buttonStyle(TapScaleStyle())

            if store.categoryFilter != nil {
                Button {
                    withAnimation(.snappy) { store.categoryFilter = nil }
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 17))
                        .foregroundStyle(.tertiary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear category filter")
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 9)
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
                    HStack {
                        Spacer()
                        ProgressView().controlSize(.small)
                        Spacer()
                    }
                    .listRowSeparator(.hidden)
                    .onAppear {
                        Task { await store.loadMore(env.api) }
                    }
                }
            }
            .listStyle(.insetGrouped)
            .scrollContentBackground(.hidden)
            .refreshable { await store.load(env.api) }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        let searching = !store.query.trimmingCharacters(in: .whitespaces).isEmpty || store.categoryFilter != nil
        if searching {
            EmptyStateView(
                title: "No matching contacts",
                message: "Try a different search or clear the category filter."
            )
        } else if env.session.can(.manageContacts) {
            EmptyStateView(
                title: "No contacts yet",
                message: "Add a contact here, or import a list on the web dashboard.",
                ctaTitle: "New contact"
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

    private func row(_ contact: Contact) -> some View {
        NavigationLink(value: contact) {
            ContactRow(contact: contact)
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if env.session.can(.manageContacts) {
                Button(role: .destructive) {
                    Task { try? await store.delete(env.api, id: contact.id) }
                } label: {
                    Label("Delete", systemImage: "trash")
                }
            }
        }
    }
}

// MARK: - Row

struct ContactRow: View {
    let contact: Contact

    var body: some View {
        HStack(spacing: 12) {
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

// MARK: - Category filter sheet

/// Single-select category filter: bordered rows with a color dot and a check.
struct ContactCategoryFilterSheet: View {
    @Environment(\.dismiss) private var dismiss
    let categories: [ContactCategory]
    @Binding var selected: ContactCategory?
    @State private var search = ""

    private var filtered: [ContactCategory] {
        let trimmed = search.trimmingCharacters(in: .whitespaces).lowercased()
        guard !trimmed.isEmpty else { return categories }
        return categories.filter { ($0.title ?? "").lowercased().contains(trimmed) }
    }

    var body: some View {
        NavigationStack {
            Group {
                if categories.isEmpty {
                    EmptyStateView(
                        title: "No categories",
                        message: "Create categories on the web dashboard to filter contacts."
                    )
                } else {
                    List {
                        Button {
                            selected = nil
                            dismiss()
                        } label: {
                            HStack {
                                Image(systemName: "circle.grid.2x2")
                                    .foregroundStyle(.secondary)
                                Text("All categories")
                                Spacer()
                                if selected == nil {
                                    Image(systemName: "checkmark").foregroundStyle(WTheme.accent)
                                }
                            }
                        }
                        .foregroundStyle(.primary)

                        ForEach(filtered) { category in
                            Button {
                                selected = category
                                dismiss()
                            } label: {
                                HStack {
                                    Circle()
                                        .fill(ContactColor.dot(category.color, seed: category.id))
                                        .frame(width: 10, height: 10)
                                    Text(category.title ?? "Untitled")
                                        .lineLimit(1)
                                    Spacer()
                                    if selected?.id == category.id {
                                        Image(systemName: "checkmark").foregroundStyle(WTheme.accent)
                                    }
                                }
                            }
                            .foregroundStyle(.primary)
                        }
                    }
                    .listStyle(.plain)
                    .searchable(text: $search, prompt: "Filter categories")
                }
            }
            .navigationTitle("Category")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}
