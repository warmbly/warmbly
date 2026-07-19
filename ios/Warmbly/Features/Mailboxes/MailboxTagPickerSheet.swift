import SwiftUI

/// Searchable multi-select over the member's mailbox tag definitions
/// (session `user.tags`, Go Group on the wire): colored dot + title rows
/// with trailing check circles, and a create row that POSTs /tags when the
/// search names a tag that doesn't exist yet. Apply hands the picked ids to
/// the caller: the detail screen PATCHes one row's full tag set, the list
/// screen turns them into a bulk add or remove.
struct MailboxTagPickerSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    var title: String = "Tags"
    var confirmLabel: String = "Apply"
    /// True for bulk add/remove, where applying nothing is meaningless.
    var requiresSelection: Bool = false
    var initialSelection: [String] = []
    var onApply: ([String]) async -> Void

    @State private var query = ""
    @State private var selected: Set<String> = []
    @State private var seeded = false
    @State private var creating = false
    @State private var applying = false
    @State private var errorMessage: String?
    @State private var togglePulse = 0

    /// Colors cycled onto newly created tags.
    private static let palette = [
        "#0ea5e9", "#10b981", "#8b5cf6", "#f59e0b",
        "#f43f5e", "#64748b", "#ec4899", "#14b8a6",
    ]

    /// The session's tag registry, in position order.
    private var options: [UserGroup] {
        (env.session.user?.tags ?? []).sorted { ($0.position ?? 0) < ($1.position ?? 0) }
    }

    private var trimmedQuery: String { query.trimmingCharacters(in: .whitespaces) }

    private var filtered: [UserGroup] {
        let q = trimmedQuery.lowercased()
        guard !q.isEmpty else { return options }
        return options.filter { ($0.name ?? "").lowercased().contains(q) }
    }

    /// True when the query is empty or names an existing tag exactly;
    /// otherwise the "Create" row shows.
    private var queryMatchesExisting: Bool {
        let q = trimmedQuery.lowercased()
        guard !q.isEmpty else { return true }
        return options.contains { ($0.name ?? "").lowercased() == q }
    }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(.inline)
                .searchable(
                    text: $query,
                    placement: .navigationBarDrawer(displayMode: .always),
                    prompt: "Search or create tags"
                )
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button {
                            Task {
                                applying = true
                                await onApply(Array(selected))
                                applying = false
                                dismiss()
                            }
                        } label: {
                            if applying {
                                ProgressView().controlSize(.small)
                            } else {
                                Text(selected.isEmpty ? confirmLabel : "\(confirmLabel) (\(selected.count))")
                            }
                        }
                        .fontWeight(.semibold)
                        .disabled(applying || (requiresSelection && selected.isEmpty))
                    }
                }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .sensoryFeedback(.selection, trigger: togglePulse)
        .task {
            guard !seeded else { return }
            seeded = true
            selected = Set(initialSelection)
        }
    }

    // MARK: Rows

    @ViewBuilder
    private var content: some View {
        if options.isEmpty, trimmedQuery.isEmpty {
            EmptyStateView(
                title: "No tags yet",
                message: "Search for a name above to create your first tag."
            )
        } else {
            List {
                if !queryMatchesExisting {
                    createRow
                }
                ForEach(filtered) { tag in
                    row(tag)
                }
                if let errorMessage {
                    Text(errorMessage)
                        .font(.footnote)
                        .foregroundStyle(WTheme.negative)
                }
            }
            .listStyle(.plain)
            .scrollDismissesKeyboard(.immediately)
        }
    }

    private func row(_ tag: UserGroup) -> some View {
        let isPicked = selected.contains(tag.id)
        return Button {
            togglePulse += 1
            withAnimation(.snappy) {
                if isPicked { selected.remove(tag.id) } else { selected.insert(tag.id) }
            }
        } label: {
            HStack(spacing: 12) {
                Circle()
                    .fill(Color(uniboxHex: tag.color) ?? WTheme.accent)
                    .frame(width: 11, height: 11)
                Text(tag.name ?? "Tag")
                    .font(.body.weight(.medium))
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                Spacer(minLength: 8)
                Image(systemName: isPicked ? "checkmark.circle.fill" : "circle")
                    .font(.system(size: 21))
                    .foregroundStyle(isPicked ? WTheme.accent : Color(.systemGray4))
            }
            .padding(.vertical, 3)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private var createRow: some View {
        Button {
            Task { await createAndSelect() }
        } label: {
            HStack(spacing: 10) {
                if creating {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: "plus.circle.fill")
                        .font(.system(size: 16))
                        .foregroundStyle(WTheme.accent)
                }
                Text("Create \"\(trimmedQuery)\"")
                    .font(.body.weight(.medium))
                    .foregroundStyle(WTheme.accent)
                    .lineLimit(1)
            }
        }
        .disabled(creating)
    }

    /// POST /tags {title, color} with the next palette color, refresh the
    /// session registry, then select the new tag immediately (mirrors the
    /// unibox label picker's create flow).
    private func createAndSelect() async {
        let title = trimmedQuery
        guard !title.isEmpty, !creating else { return }
        creating = true
        defer { creating = false }
        do {
            let color = Self.palette[(env.session.user?.tags?.count ?? 0) % Self.palette.count]
            let created: UserGroup = try await env.api.post("tags", body: ["title": title, "color": color])
            await env.session.refreshUser()
            togglePulse += 1
            withAnimation(.snappy) {
                query = ""
                selected.insert(created.id)
            }
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
