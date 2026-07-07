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

    @State private var saving = false
    @State private var confirmDelete = false
    @State private var deleting = false
    @State private var errorMessage: String?

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
    }

    private var canManage: Bool { env.session.can(.manageContacts) }

    private var isDirty: Bool {
        firstName != (contact.firstName ?? "")
            || lastName != (contact.lastName ?? "")
            || company != (contact.company ?? "")
            || phone != (contact.phone ?? "")
            || subscribed != (contact.subscribed ?? true)
            || selectedCategoryIDs != Set((contact.categories ?? []).map(\.id))
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
                categorySection
            }
            .navigationTitle("Edit contact")
            .navigationBarTitleDisplayMode(.inline)
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

    private func save() async {
        saving = true
        defer { saving = false }
        // Send the full category SET so removals stick.
        let body = ContactUpdateBody(
            firstName: firstName,
            lastName: lastName,
            company: company,
            phone: phone,
            subscribed: subscribed,
            categories: Array(selectedCategoryIDs)
        )
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
