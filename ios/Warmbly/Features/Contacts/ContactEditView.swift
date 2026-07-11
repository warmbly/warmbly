import SwiftUI

/// Edit sheet: PATCH /contacts/:id (email is not updatable), category
/// assignment from the cached /auth/me categories, and delete with a
/// confirmation. Gated by .manageContacts (the caller only presents it when
/// permitted; the fields also enforce it defensively).
struct ContactEditSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let contact: Contact
    let categoryStore: ContactCategoryStore
    let onSaved: (Contact) -> Void
    let onDeleted: () -> Void

    @State private var firstName: String
    @State private var lastName: String
    @State private var company: String
    @State private var phone: String
    @State private var subscribed: Bool
    @State private var selectedCategoryIDs: Set<String>
    @State private var selectedCampaignIDs: Set<String>
    @State private var customFields: [CustomFieldRow]
    @State private var campaignOptions: [ContactMiniCampaign] = []

    @State private var saving = false
    @State private var confirmDelete = false
    @State private var deleting = false
    @State private var errorMessage: String?

    private let originalCampaignIDs: Set<String>
    private let originalFields: [String: String]

    init(
        contact: Contact,
        categoryStore: ContactCategoryStore,
        onSaved: @escaping (Contact) -> Void,
        onDeleted: @escaping () -> Void
    ) {
        self.contact = contact
        self.categoryStore = categoryStore
        self.onSaved = onSaved
        self.onDeleted = onDeleted
        _firstName = State(initialValue: contact.firstName ?? "")
        _lastName = State(initialValue: contact.lastName ?? "")
        _company = State(initialValue: contact.company ?? "")
        _phone = State(initialValue: contact.phone ?? "")
        _subscribed = State(initialValue: contact.subscribed ?? true)
        _selectedCategoryIDs = State(initialValue: Set((contact.categories ?? []).map(\.id)))
        let campIDs = Set((contact.campaigns ?? []).map(\.id))
        _selectedCampaignIDs = State(initialValue: campIDs)
        originalCampaignIDs = campIDs
        let fields = contact.customFields ?? [:]
        originalFields = fields
        _customFields = State(initialValue: fields.sorted { $0.key < $1.key }.map { CustomFieldRow(key: $0.key, value: $0.value) })
    }

    private var canManage: Bool { env.session.can(.manageContacts) }

    /// The edited custom fields as a dict, dropping blank-key rows.
    private var customFieldsDict: [String: String] {
        var out: [String: String] = [:]
        for row in customFields {
            let k = row.key.trimmingCharacters(in: .whitespaces)
            guard !k.isEmpty else { continue }
            out[k] = row.value
        }
        return out
    }

    private var isDirty: Bool {
        firstName != (contact.firstName ?? "")
            || lastName != (contact.lastName ?? "")
            || company != (contact.company ?? "")
            || phone != (contact.phone ?? "")
            || subscribed != (contact.subscribed ?? true)
            || selectedCategoryIDs != Set((contact.categories ?? []).map(\.id))
            || selectedCampaignIDs != originalCampaignIDs
            || customFieldsDict != originalFields
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Name") {
                    TextField("First name", text: $firstName)
                        .textContentType(.givenName)
                    TextField("Last name", text: $lastName)
                        .textContentType(.familyName)
                }
                Section("Details") {
                    // Email is not updatable server-side; show it read-only.
                    LabeledContent("Email", value: contact.email ?? "–")
                        .foregroundStyle(.secondary)
                    TextField("Company", text: $company)
                    TextField("Phone", text: $phone)
                        .keyboardType(.phonePad)
                    Toggle("Subscribed", isOn: $subscribed)
                        .tint(WTheme.accent)
                }
                campaignsSection
                customFieldsSection
                categorySection
            }
            .navigationTitle("Edit contact")
            .navigationBarTitleDisplayMode(.inline)
            .task { await loadCampaignOptions() }
            .presenceEditingWhileVisible(contact.id)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if saving {
                        ProgressView().controlSize(.small)
                    } else {
                        Button("Save") { Task { await save() } }
                            .disabled(!canManage || !isDirty)
                    }
                }
            }
            .task { if categoryStore.categories.isEmpty { await categoryStore.load(env.api) } }
            .confirmationDialog(
                "Delete contact?",
                isPresented: $confirmDelete,
                titleVisibility: .visible
            ) {
                Button("Delete \(contact.displayName)", role: .destructive) {
                    Task { await delete() }
                }
            } message: {
                Text("This removes the contact from your workspace. This can't be undone.")
            }
            .alert("Couldn't save", isPresented: Binding(
                get: { errorMessage != nil },
                set: { if !$0 { errorMessage = nil } }
            )) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(errorMessage ?? "")
            }
        }
        .presentationDragIndicator(.visible)
    }

    @ViewBuilder
    private var campaignsSection: some View {
        Section {
            if campaignOptions.isEmpty {
                Text("No campaigns yet.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(campaignOptions) { campaign in
                    Button {
                        toggleCampaign(campaign.id)
                    } label: {
                        HStack(spacing: 10) {
                            Image(systemName: "paperplane.fill")
                                .font(.system(size: 13))
                                .foregroundStyle(.secondary)
                                .frame(width: 16)
                            Text(campaign.name ?? "Campaign")
                                .foregroundStyle(.primary)
                                .lineLimit(1)
                            Spacer()
                            if selectedCampaignIDs.contains(campaign.id) {
                                Image(systemName: "checkmark").fontWeight(.semibold).foregroundStyle(WTheme.accent)
                            }
                        }
                    }
                    .disabled(!canManage)
                }
            }
        } header: {
            Text("Campaigns")
        } footer: {
            Text("Add this contact as a lead, or remove them from a campaign.")
        }
    }

    @ViewBuilder
    private var customFieldsSection: some View {
        Section {
            ForEach($customFields) { $row in
                HStack(spacing: 8) {
                    TextField("Field", text: $row.key)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .frame(maxWidth: 130, alignment: .leading)
                    Divider()
                    TextField("Value", text: $row.value)
                }
                .disabled(!canManage)
            }
            .onDelete { customFields.remove(atOffsets: $0) }
            if canManage {
                Button {
                    customFields.append(CustomFieldRow(key: "", value: ""))
                } label: {
                    Label("Add custom field", systemImage: "plus")
                }
            }
        } header: {
            Text("Custom fields")
        }
    }

    @ViewBuilder
    private var categorySection: some View {
        Section {
            if categoryStore.categories.isEmpty {
                Text("No categories yet. Create them on the web dashboard.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(categoryStore.categories) { category in
                    Button {
                        toggle(category.id)
                    } label: {
                        HStack(spacing: 10) {
                            Circle()
                                .fill(ContactColor.dot(category.color, seed: category.id))
                                .frame(width: 12, height: 12)
                            Text(category.title ?? "Untitled")
                                .font(.body.weight(.medium))
                                .foregroundStyle(.primary)
                            Spacer()
                            if selectedCategoryIDs.contains(category.id) {
                                Image(systemName: "checkmark")
                                    .fontWeight(.semibold)
                                    .foregroundStyle(WTheme.accent)
                            }
                        }
                    }
                    .disabled(!canManage)
                }
            }
        } header: {
            Text("Categories")
        }
        if canManage {
            Section {
                Button(role: .destructive) {
                    confirmDelete = true
                } label: {
                    if deleting {
                        ProgressView().controlSize(.small)
                    } else {
                        Label("Delete contact", systemImage: "trash")
                    }
                }
                .disabled(deleting)
            }
        }
    }

    private func toggle(_ id: String) {
        if selectedCategoryIDs.contains(id) {
            selectedCategoryIDs.remove(id)
        } else {
            selectedCategoryIDs.insert(id)
        }
    }

    private func toggleCampaign(_ id: String) {
        if selectedCampaignIDs.contains(id) {
            selectedCampaignIDs.remove(id)
        } else {
            selectedCampaignIDs.insert(id)
        }
    }

    private func loadCampaignOptions() async {
        guard campaignOptions.isEmpty else { return }
        if let page: ListResponse<ContactMiniCampaign> = try? await env.api.get("campaigns", query: ["limit": "100"]) {
            campaignOptions = page.data
        }
    }

    private func save() async {
        saving = true
        defer { saving = false }
        // Send the full category SET so removals stick.
        var body = ContactUpdateBody(
            firstName: firstName,
            lastName: lastName,
            company: company,
            phone: phone,
            subscribed: subscribed,
            categories: Array(selectedCategoryIDs)
        )
        // Campaigns/custom fields REPLACE the full set, so only send them when
        // the user actually changed them (avoids wiping membership past 100).
        if selectedCampaignIDs != originalCampaignIDs {
            body.campaigns = Array(selectedCampaignIDs)
        }
        if customFieldsDict != originalFields {
            body.customFields = customFieldsDict
        }
        do {
            let updated: Contact = try await env.api.patch("contacts/\(contact.id)", body: body)
            onSaved(updated)
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func delete() async {
        deleting = true
        defer { deleting = false }
        do {
            let _: EmptyBody = try await env.api.delete("contacts/\(contact.id)")
            onDeleted()
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}

// MARK: - Create sheet

/// Create sheet: POST /contacts (array body, email required), optional name,
/// company, phone, and category assignment.
struct ContactCreateSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let store: ContactsStore
    let categoryStore: ContactCategoryStore
    let onCreated: (Contact) -> Void

    @State private var firstName = ""
    @State private var lastName = ""
    @State private var email = ""
    @State private var company = ""
    @State private var phone = ""
    @State private var selectedCategoryIDs: Set<String> = []
    @State private var saving = false
    @State private var errorMessage: String?

    private var emailValid: Bool {
        let trimmed = email.trimmingCharacters(in: .whitespaces)
        return trimmed.contains("@") && trimmed.contains(".") && !trimmed.hasSuffix("@")
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Contact") {
                    TextField("Email (required)", text: $email)
                        .keyboardType(.emailAddress)
                        .textContentType(.emailAddress)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    TextField("First name", text: $firstName)
                        .textContentType(.givenName)
                    TextField("Last name", text: $lastName)
                        .textContentType(.familyName)
                }
                Section("Details") {
                    TextField("Company", text: $company)
                    TextField("Phone", text: $phone)
                        .keyboardType(.phonePad)
                }
                if !categoryStore.categories.isEmpty {
                    Section("Categories") {
                        ForEach(categoryStore.categories) { category in
                            Button {
                                toggle(category.id)
                            } label: {
                                HStack(spacing: 10) {
                                    Circle()
                                        .fill(ContactColor.dot(category.color, seed: category.id))
                                        .frame(width: 12, height: 12)
                                    Text(category.title ?? "Untitled")
                                        .font(.body.weight(.medium))
                                        .foregroundStyle(.primary)
                                    Spacer()
                                    if selectedCategoryIDs.contains(category.id) {
                                        Image(systemName: "checkmark")
                                            .fontWeight(.semibold)
                                            .foregroundStyle(WTheme.accent)
                                    }
                                }
                            }
                        }
                    }
                }
            }
            .navigationTitle("New contact")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if saving {
                        ProgressView().controlSize(.small)
                    } else {
                        Button("Add") { Task { await create() } }
                            .disabled(!emailValid)
                    }
                }
            }
            .task { if categoryStore.categories.isEmpty { await categoryStore.load(env.api) } }
            .alert("Couldn't add contact", isPresented: Binding(
                get: { errorMessage != nil },
                set: { if !$0 { errorMessage = nil } }
            )) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(errorMessage ?? "")
            }
        }
        .presentationDragIndicator(.visible)
    }

    private func toggle(_ id: String) {
        if selectedCategoryIDs.contains(id) {
            selectedCategoryIDs.remove(id)
        } else {
            selectedCategoryIDs.insert(id)
        }
    }

    private func create() async {
        saving = true
        defer { saving = false }
        let body = ContactCreateBody(
            firstName: firstName.trimmingCharacters(in: .whitespaces),
            lastName: lastName.trimmingCharacters(in: .whitespaces),
            email: email.trimmingCharacters(in: .whitespaces),
            company: company.trimmingCharacters(in: .whitespaces),
            phone: phone.trimmingCharacters(in: .whitespaces),
            categories: selectedCategoryIDs.isEmpty ? nil : Array(selectedCategoryIDs)
        )
        do {
            let contact = try await store.create(env.api, body: body)
            dismiss()
            onCreated(contact)
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}

/// One editable custom-field key/value row in the contact edit form.
struct CustomFieldRow: Identifiable, Hashable {
    let id = UUID()
    var key: String
    var value: String
}

// MARK: - Presence helper

/// Claims the contact as "editing" while an edit form is visible, then hands
/// presence back to "viewing" (the detail view re-claims it on reappear).
private struct PresenceEditingModifier: ViewModifier {
    @Environment(AppEnvironment.self) private var env
    let id: String

    func body(content: Content) -> some View {
        content
            .onAppear { env.realtime.setPresence(page: nil, resource: "contact:\(id)", action: "editing") }
            .onDisappear { env.realtime.setPresence(page: nil, resource: "contact:\(id)", action: "viewing") }
    }
}

extension View {
    func presenceEditingWhileVisible(_ id: String) -> some View {
        modifier(PresenceEditingModifier(id: id))
    }
}
