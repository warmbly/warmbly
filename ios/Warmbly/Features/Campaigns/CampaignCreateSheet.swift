import SwiftUI

/// Minimal "quick create" sheet: name only (the one required field, 3-50
/// chars). Steps and senders are finished on the web dashboard. On success the
/// created campaign is handed back so the caller can push its detail.
struct CampaignCreateSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let store: CampaignsStore
    let onCreated: (Campaign) -> Void

    @State private var name = ""
    @State private var isCreating = false
    @State private var errorMessage: String?
    @FocusState private var nameFocused: Bool

    private var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var canSubmit: Bool {
        let count = trimmedName.count
        return count >= 3 && count <= 50 && !isCreating
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Campaign name", text: $name)
                        .focused($nameFocused)
                        .submitLabel(.done)
                        .onSubmit { if canSubmit { Task { await create() } } }
                } header: {
                    EyebrowLabel("Name")
                } footer: {
                    Text("3 to 50 characters. Add steps and senders on the web dashboard once it exists.")
                        .font(.footnote)
                }

                if let errorMessage {
                    Section {
                        Text(errorMessage)
                            .font(.subheadline)
                            .foregroundStyle(WTheme.negative)
                    }
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    Text("New campaign")
                        .font(.title2.bold())
                }
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                        .disabled(isCreating)
                }
            }
            .safeAreaInset(edge: .bottom) {
                Button {
                    Task { await create() }
                } label: {
                    Group {
                        if isCreating {
                            ProgressView()
                                .tint(.white)
                        } else {
                            Text("Create campaign")
                                .font(.body.weight(.semibold))
                        }
                    }
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .tint(WTheme.accent)
                .controlSize(.large)
                .disabled(!canSubmit)
                .padding(.horizontal, 16)
                .padding(.bottom, 8)
            }
            .onAppear { nameFocused = true }
        }
        .presentationDetents([.medium])
        .presentationDragIndicator(.visible)
    }

    private func create() async {
        guard canSubmit else { return }
        isCreating = true
        errorMessage = nil
        do {
            let campaign = try await store.create(env.api, name: trimmedName)
            dismiss()
            onCreated(campaign)
        } catch {
            errorMessage = error.localizedDescription
        }
        isCreating = false
    }
}
