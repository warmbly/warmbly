import SwiftUI

/// Floating bottom-center selection bar shown when contacts are multi-selected,
/// matching the dashboard's SelectionBar convention: a count plus bulk actions
/// (edit membership/subscription, delete).
struct ContactSelectionBar: View {
    let count: Int
    let onEdit: () -> Void
    let onDelete: () -> Void

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

            Button(role: .destructive, action: onDelete) {
                Image(systemName: "trash")
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

// MARK: - Bulk edit sheet

/// Applies membership/subscription changes to many contacts at once via
/// PATCH /contacts (BulkEditContactsData): add/remove categories, add/remove
/// campaigns, and a tri-state subscription. Owns its own submit + error.
struct ContactBulkEditSheet: View {
    @Environment(\.dismiss) private var dismiss

    let ids: [String]
    let categories: [ContactCategory]
    let campaigns: [ContactMiniCampaign]
    /// Fired once campaigns should be fetched (parent loads store.campaignOptions).
    let loadCampaigns: () async -> Void
    let perform: (ContactBulkEditBody) async throws -> Void

    private enum SubChoice: Hashable { case leave, subscribe, unsubscribe }

    @State private var subscribe: SubChoice = .leave
    @State private var addCategories: Set<String> = []
    @State private var removeCategories: Set<String> = []
    @State private var addCampaigns: Set<String> = []
    @State private var removeCampaigns: Set<String> = []
    @State private var saving = false
    @State private var errorMessage: String?

    private var hasChanges: Bool {
        subscribe != .leave
            || !addCategories.isEmpty || !removeCategories.isEmpty
            || !addCampaigns.isEmpty || !removeCampaigns.isEmpty
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Subscription", selection: $subscribe) {
                        Text("Leave as is").tag(SubChoice.leave)
                        Text("Subscribe").tag(SubChoice.subscribe)
                        Text("Unsubscribe").tag(SubChoice.unsubscribe)
                    }
                    .pickerStyle(.segmented)
                } header: {
                    Text("Subscription")
                }

                if !categories.isEmpty {
                    categoryPicker("Add categories", selection: $addCategories, counterpart: $removeCategories)
                    categoryPicker("Remove categories", selection: $removeCategories, counterpart: $addCategories)
                }

                if !campaigns.isEmpty {
                    campaignPicker("Add to campaign", selection: $addCampaigns, counterpart: $removeCampaigns)
                    campaignPicker("Remove from campaign", selection: $removeCampaigns, counterpart: $addCampaigns)
                }
            }
            .navigationTitle("Edit \(ids.count) contact\(ids.count == 1 ? "" : "s")")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if saving {
                        ProgressView().controlSize(.small)
                    } else {
                        Button("Apply") { Task { await apply() } }
                            .fontWeight(.semibold)
                            .disabled(!hasChanges)
                    }
                }
            }
            .task { await loadCampaigns() }
            .alert("Couldn't apply changes", isPresented: Binding(
                get: { errorMessage != nil }, set: { if !$0 { errorMessage = nil } }
            )) {
                Button("OK", role: .cancel) {}
            } message: { Text(errorMessage ?? "") }
        }
        .presentationDragIndicator(.visible)
    }

    private func categoryPicker(_ title: String, selection: Binding<Set<String>>, counterpart: Binding<Set<String>>) -> some View {
        Section(title) {
            ForEach(categories) { category in
                checkRow(
                    dot: ContactColor.dot(category.color, seed: category.id),
                    title: category.title ?? "Untitled",
                    on: selection.wrappedValue.contains(category.id)
                ) {
                    toggle(category.id, in: selection, counterpart: counterpart)
                }
            }
        }
    }

    private func campaignPicker(_ title: String, selection: Binding<Set<String>>, counterpart: Binding<Set<String>>) -> some View {
        Section(title) {
            ForEach(campaigns) { campaign in
                checkRow(
                    icon: "paperplane.fill",
                    title: campaign.name ?? "Campaign",
                    on: selection.wrappedValue.contains(campaign.id)
                ) {
                    toggle(campaign.id, in: selection, counterpart: counterpart)
                }
            }
        }
    }

    private func checkRow(icon: String? = nil, dot: Color? = nil, title: String, on: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 10) {
                if let dot {
                    Circle().fill(dot).frame(width: 12, height: 12)
                } else if let icon {
                    Image(systemName: icon).font(.system(size: 13)).foregroundStyle(.secondary).frame(width: 16)
                }
                Text(title).foregroundStyle(.primary).lineLimit(1)
                Spacer()
                if on {
                    Image(systemName: "checkmark").fontWeight(.semibold).foregroundStyle(WTheme.accent)
                }
            }
        }
    }

    /// Toggle membership in one set, clearing the opposite set so a value can't
    /// be both added and removed at once.
    private func toggle(_ id: String, in set: Binding<Set<String>>, counterpart: Binding<Set<String>>) {
        if set.wrappedValue.contains(id) {
            set.wrappedValue.remove(id)
        } else {
            set.wrappedValue.insert(id)
            counterpart.wrappedValue.remove(id)
        }
    }

    private func apply() async {
        saving = true
        defer { saving = false }
        var body = ContactBulkEditBody(contacts: ids)
        switch subscribe {
        case .leave: break
        case .subscribe: body.subscribe = true
        case .unsubscribe: body.subscribe = false
        }
        if !addCategories.isEmpty { body.addCategories = Array(addCategories) }
        if !removeCategories.isEmpty { body.removeCategories = Array(removeCategories) }
        if !addCampaigns.isEmpty { body.addCampaigns = Array(addCampaigns) }
        if !removeCampaigns.isEmpty { body.removeCampaigns = Array(removeCampaigns) }
        do {
            try await perform(body)
            dismiss()
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
