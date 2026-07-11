import SwiftUI

/// Add existing contacts to a campaign as leads: a searchable, multi-select
/// contact picker that PATCHes the chosen contacts with this campaign. Contacts
/// already in the campaign are shown but locked. Mirrors the contacts browser's
/// row + selection language; runs its own lightweight search over
/// POST /contacts/search.
struct CampaignAddLeadsSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let campaignID: String
    let campaignName: String
    /// Fired after a successful add so the leads list can reload.
    let onAdded: () async -> Void

    @State private var query = ""
    @State private var results: [Contact] = []
    @State private var selected: Set<String> = []
    @State private var isLoading = false
    @State private var isAdding = false
    @State private var hasLoaded = false
    @State private var errorMessage: String?
    @State private var nextCursor: String?
    @State private var hasMore = false
    @State private var generation = 0

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                searchField
                list
            }
            .background(Color(.systemBackground))
            .navigationTitle("Add leads")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .safeAreaInset(edge: .bottom) {
                if !selected.isEmpty { addBar }
            }
            .alert("Couldn't add leads", isPresented: Binding(
                get: { errorMessage != nil }, set: { if !$0 { errorMessage = nil } }
            )) {
                Button("OK", role: .cancel) {}
            } message: { Text(errorMessage ?? "") }
        }
        .task { if !hasLoaded { await load() } }
        .task(id: query) {
            guard hasLoaded else { return }
            try? await Task.sleep(for: .milliseconds(350))
            guard !Task.isCancelled else { return }
            await load()
        }
    }

    // MARK: Search field

    private var searchField: some View {
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(.secondary)
                .padding(.leading, 12)
            TextField("Search contacts", text: $query)
                .font(.subheadline)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.search)
            if !query.isEmpty {
                Button {
                    query = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 16))
                        .foregroundStyle(.tertiary)
                }
                .buttonStyle(.plain)
                .padding(.trailing, 10)
                .accessibilityLabel("Clear search")
            }
        }
        .frame(height: 44)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 22, style: .continuous))
        .padding(.horizontal, 12)
        .padding(.top, 6)
        .padding(.bottom, 4)
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if isLoading, !hasLoaded {
            ScrollView { SkeletonRows(rows: 8) }
        } else if let error = errorMessage, results.isEmpty {
            ErrorStateView(title: "Couldn't load contacts", message: error) { await load() }
        } else if results.isEmpty {
            EmptyStateView(
                title: query.isEmpty ? "No contacts yet" : "No matches",
                message: query.isEmpty ? "Add contacts in the Contacts tab first." : "Try a different search."
            )
        } else {
            List {
                ForEach(results) { contact in
                    row(contact)
                }
                if hasMore {
                    HStack {
                        Spacer()
                        ProgressView().controlSize(.small)
                        Spacer()
                    }
                    .listRowSeparator(.hidden)
                    .onAppear { Task { await loadMore() } }
                }
            }
            .listStyle(.plain)
        }
    }

    private func inCampaign(_ contact: Contact) -> Bool {
        contact.campaigns?.contains { $0.id == campaignID } ?? false
    }

    private func row(_ contact: Contact) -> some View {
        let already = inCampaign(contact)
        let isSelected = selected.contains(contact.id)
        return Button {
            guard !already else { return }
            if isSelected { selected.remove(contact.id) } else { selected.insert(contact.id) }
        } label: {
            HStack(spacing: 12) {
                Image(systemName: already ? "checkmark.circle.fill" : (isSelected ? "checkmark.circle.fill" : "circle"))
                    .font(.system(size: 22))
                    .foregroundStyle(already ? Color(.tertiaryLabel) : (isSelected ? WTheme.accent : Color(.tertiaryLabel)))
                WAvatar(name: contact.displayName, seed: contact.id, size: 40)
                VStack(alignment: .leading, spacing: 2) {
                    Text(contact.displayName)
                        .font(.body.weight(.medium))
                        .lineLimit(1)
                    if contact.hasName, let email = contact.email, !email.isEmpty {
                        Text(email).font(.footnote).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
                if already {
                    Text("In campaign")
                        .font(.caption2.weight(.medium))
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(.vertical, 6)
            .contentShape(Rectangle())
            .opacity(already ? 0.55 : 1)
        }
        .buttonStyle(.plain)
        .disabled(already)
        .listRowInsets(EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16))
    }

    // MARK: Add bar

    private var addBar: some View {
        Button {
            Task { await add() }
        } label: {
            HStack {
                if isAdding {
                    ProgressView().controlSize(.small).tint(.white)
                } else {
                    Text("Add \(selected.count) to \(campaignName)")
                        .font(.subheadline.weight(.semibold))
                        .lineLimit(1)
                }
            }
            .frame(maxWidth: .infinity)
            .frame(height: 48)
            .background(WTheme.accent, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .foregroundStyle(.white)
        }
        .buttonStyle(TapScaleStyle())
        .disabled(isAdding || selected.isEmpty)
        .padding(.horizontal, 16)
        .padding(.top, 8)
        .padding(.bottom, 8)
        .background(.regularMaterial)
    }

    // MARK: Data

    private func searchRequestBody() -> ContactSearchBody {
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        return ContactSearchBody(query: trimmed.isEmpty ? nil : trimmed)
    }

    private func load() async {
        generation += 1
        let gen = generation
        isLoading = true
        do {
            let page: ContactSearchPage = try await env.api.post(
                "contacts/search", body: searchRequestBody(), query: ["limit": "50"]
            )
            guard gen == generation else { return }
            results = page.data
            nextCursor = page.pagination?.nextCursor
            hasMore = page.pagination?.hasMore ?? false
            errorMessage = nil
            hasLoaded = true
        } catch {
            guard gen == generation else { return }
            if !hasLoaded { errorMessage = error.localizedDescription }
        }
        isLoading = false
    }

    private func loadMore() async {
        guard hasMore, let cursor = nextCursor else { return }
        let gen = generation
        do {
            let page: ContactSearchPage = try await env.api.post(
                "contacts/search", body: searchRequestBody(), query: ["limit": "50", "cursor": cursor]
            )
            guard gen == generation else { return }
            let fresh = page.data.filter { new in !results.contains(where: { $0.id == new.id }) }
            results.append(contentsOf: fresh)
            nextCursor = page.pagination?.nextCursor
            hasMore = page.pagination?.hasMore ?? false
        } catch {
            // Keep the list; the sentinel row retries on next appear.
        }
    }

    private func add() async {
        isAdding = true
        defer { isAdding = false }
        let body = ContactBulkEditBody(contacts: Array(selected), addCampaigns: [campaignID])
        do {
            let _: [Contact] = try await env.api.patch("contacts", body: body)
            await onAdded()
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
