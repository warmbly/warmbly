import SwiftUI

/// Browse-and-pick contacts for a recipient field: server-side search, a
/// compact category filter menu, multi-select with checkmarks, Add commits
/// the picked addresses. Complements the type-ahead in `RecipientField` for
/// people who want to pick instead of type.
struct ContactPickerSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    /// Addresses already in the field; shown as picked and not re-added.
    var existing: [String]
    var onAdd: ([String]) -> Void

    @State private var query = ""
    @State private var contacts: [Contact] = []
    @State private var categories: [ContactCategory] = []
    @State private var activeCategoryID: String?
    @State private var picked: [String] = []
    @State private var isLoading = false
    @State private var hasLoaded = false

    private var existingLowered: Set<String> { Set(existing.map { $0.lowercased() }) }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if !categories.isEmpty {
                    HStack {
                        categoryMenu
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 6)
                    Divider()
                }
                if isLoading, !hasLoaded {
                    ProgressView()
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if contacts.isEmpty {
                    EmptyStateView(
                        title: query.isEmpty ? "No contacts" : "No matches",
                        message: query.isEmpty
                            ? "Contacts you add to the workspace show up here."
                            : "No contact matches \"\(query)\"."
                    )
                } else {
                    List {
                        ForEach(contacts) { contact in
                            row(contact)
                        }
                    }
                    .listStyle(.plain)
                }
            }
            .navigationTitle("Add recipients")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(
                text: $query,
                placement: .navigationBarDrawer(displayMode: .always),
                prompt: "Search contacts"
            )
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(picked.isEmpty ? "Add" : "Add (\(picked.count))") {
                        onAdd(picked)
                        dismiss()
                    }
                    .fontWeight(.semibold)
                    .disabled(picked.isEmpty)
                }
            }
        }
        .presentationDetents([.large])
        .presentationDragIndicator(.visible)
        .task {
            if categories.isEmpty, env.session.can(.viewContacts),
               let me: ContactsMePayload = try? await env.api.get("auth/me") {
                categories = me.categories ?? []
            }
        }
        .task(id: "\(query)|\(activeCategoryID ?? "")") {
            if hasLoaded {
                try? await Task.sleep(for: .milliseconds(250))
            }
            guard !Task.isCancelled else { return }
            await search()
        }
        .sensoryFeedback(.selection, trigger: picked.count)
    }

    private var activeCategory: ContactCategory? {
        categories.first { $0.id == activeCategoryID }
    }

    /// Compact filter: "All categories" plus every category with its color
    /// dot; the trigger names the active category and takes its tint.
    private var categoryMenu: some View {
        let tint = activeCategory.flatMap { Color(uniboxHex: $0.color) } ?? WTheme.accent
        let active = activeCategory != nil
        return Menu {
            Picker("Filter by category", selection: $activeCategoryID.animation(.snappy)) {
                Text("All categories").tag(String?.none)
                ForEach(categories) { category in
                    Label {
                        Text(category.title ?? "Category")
                    } icon: {
                        Image.menuDot(Color(uniboxHex: category.color) ?? WTheme.accent)
                    }
                    .tag(String?.some(category.id))
                }
            }
        } label: {
            HStack(spacing: 5) {
                if active {
                    Circle().fill(tint).frame(width: 7, height: 7)
                } else {
                    Image(systemName: "line.3.horizontal.decrease")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(.secondary)
                }
                Text(activeCategory?.title ?? "All categories")
                    .font(.footnote.weight(active ? .semibold : .medium))
                    .foregroundStyle(active ? tint : Color.primary)
                    .lineLimit(1)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 11)
            .padding(.vertical, 6)
            .background(
                active ? AnyShapeStyle(tint.opacity(0.13)) : AnyShapeStyle(Color(.secondarySystemBackground)),
                in: Capsule()
            )
            .overlay(Capsule().strokeBorder(active ? tint.opacity(0.45) : .clear, lineWidth: 1))
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Filter by category")
    }

    private func row(_ contact: Contact) -> some View {
        let email = contact.email ?? ""
        let alreadyIn = existingLowered.contains(email.lowercased())
        let isPicked = picked.contains { $0.lowercased() == email.lowercased() }
        return Button {
            guard !alreadyIn, !email.isEmpty else { return }
            withAnimation(.snappy) {
                if isPicked {
                    picked.removeAll { $0.lowercased() == email.lowercased() }
                } else {
                    picked.append(email)
                }
            }
        } label: {
            HStack(spacing: 12) {
                WAvatar(name: contact.displayName, seed: contact.id, size: 32)
                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: 6) {
                        Text(contact.hasName ? contact.displayName : email)
                            .font(.body.weight(.medium))
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                        ForEach((contact.categories ?? []).prefix(2)) { category in
                            let tint = Color(uniboxHex: category.color) ?? WTheme.accent
                            HStack(spacing: 3) {
                                Circle().fill(tint).frame(width: 6, height: 6)
                                Text(category.title ?? "")
                                    .font(.system(size: 10.5, weight: .medium))
                                    .foregroundStyle(tint)
                                    .lineLimit(1)
                            }
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(tint.opacity(0.1), in: Capsule())
                        }
                    }
                    if let detail = detailLine(contact) {
                        Text(detail)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
                if alreadyIn {
                    Text("Added")
                        .font(.caption.weight(.medium))
                        .foregroundStyle(.tertiary)
                } else {
                    Image(systemName: isPicked ? "checkmark.circle.fill" : "circle")
                        .font(.system(size: 21))
                        .foregroundStyle(isPicked ? WTheme.accent : Color(.systemGray4))
                }
            }
            .padding(.vertical, 3)
        }
        .disabled(alreadyIn || email.isEmpty)
    }

    private func detailLine(_ contact: Contact) -> String? {
        var parts: [String] = []
        if contact.hasName, let email = contact.email, !email.isEmpty { parts.append(email) }
        if let company = contact.company, !company.isEmpty { parts.append(company) }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    private func search() async {
        guard env.session.can(.viewContacts) else { return }
        isLoading = true
        defer { isLoading = false }
        var body = ContactSearchBody()
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        if !trimmed.isEmpty { body.query = trimmed }
        if let activeCategoryID { body.categoryIDs = [activeCategoryID] }
        guard let page: ContactSearchPage = try? await env.api.post(
            "contacts/search", body: body, query: ["limit": "50"]
        ), !Task.isCancelled else { return }
        withAnimation(.snappy) { contacts = page.data.filter { !($0.email ?? "").isEmpty } }
        hasLoaded = true
    }
}
