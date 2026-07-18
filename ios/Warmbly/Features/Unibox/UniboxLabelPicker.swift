import SwiftUI

/// House label picker (web ThreadLabelMenu / CategoryPicker parity): a
/// search-or-create header, a removable assigned-chips row, and color-dot
/// rows with trailing checkmarks. Toggles save immediately (optimistic,
/// reverting on error) — there is no Save button. Three modes: bound to an
/// open thread, standalone for a list row, and bulk for multi-select.
struct UniboxLabelPicker: View {
    enum Mode {
        /// Thread screen: saves through the thread store so the strip updates.
        case thread(UniboxThreadStore)
        /// List row: loads the thread's labels itself and saves directly.
        case standalone(threadKey: String, initial: [UniboxLabel], onChanged: ([UniboxLabel]) -> Void)
        /// Multi-select: collect a set here, the caller applies it per thread.
        case bulk(count: Int, onApply: ([String]) async -> Void)
    }

    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    let mode: Mode

    @State private var query = ""
    @State private var selected: Set<String> = []
    /// Thread labels that aren't in the session registry (deleted categories
    /// still assigned); kept so their chips can render and be removed.
    @State private var extraLabels: [UniboxLabel] = []
    @State private var loadedInitial = false
    @State private var creating = false
    @State private var applying = false
    @State private var errorMessage: String?
    @State private var togglePulse = 0
    @FocusState private var searchFocused: Bool

    private struct LabelsEnvelope: Codable, Sendable {
        var data: [UniboxLabel]?
    }

    /// The session's category registry, in position order.
    private var options: [UserGroup] {
        (env.session.user?.categories ?? []).sorted { ($0.position ?? 0) < ($1.position ?? 0) }
    }

    private var trimmedQuery: String { query.trimmingCharacters(in: .whitespaces) }

    private var filtered: [UserGroup] {
        let q = trimmedQuery.lowercased()
        guard !q.isEmpty else { return options }
        return options.filter { ($0.name ?? "").lowercased().contains(q) }
    }

    /// True when the query is empty or names an existing category exactly;
    /// otherwise the "Create" row shows.
    private var queryMatchesExisting: Bool {
        let q = trimmedQuery.lowercased()
        guard !q.isEmpty else { return true }
        return options.contains { ($0.name ?? "").lowercased() == q }
    }

    private var isBulk: Bool {
        if case .bulk = mode { return true }
        return false
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                searchHeader
                Divider()
                if !selected.isEmpty {
                    assignedChips
                    Divider()
                }
                content
            }
            .navigationTitle("Labels")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                        .fontWeight(.semibold)
                }
            }
            .safeAreaInset(edge: .bottom) {
                if isBulk { bulkFooter }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .sensoryFeedback(.selection, trigger: togglePulse)
        .task { await loadInitial() }
    }

    // MARK: Header

    private var searchHeader: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(.tertiary)
            TextField(isBulk ? "Label conversations…" : "Label conversation…", text: $query)
                .font(.subheadline)
                .focused($searchFocused)
                .autocorrectionDisabled()
                .onSubmit {
                    if !queryMatchesExisting {
                        Task { await createAndSelect() }
                    }
                }
            if !query.isEmpty {
                Button {
                    query = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 15))
                        .foregroundStyle(.tertiary)
                }
                .accessibilityLabel("Clear search")
            }
        }
        .padding(.horizontal, 16)
        .frame(height: 46)
    }

    /// Currently assigned labels as removable chips.
    private var assignedChips: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(selectedLabels) { label in
                    let tint = Color(uniboxHex: label.color) ?? WTheme.accent
                    Button {
                        toggle(label.id)
                    } label: {
                        HStack(spacing: 5) {
                            Circle().fill(tint).frame(width: 7, height: 7)
                            Text(label.title ?? "Label")
                                .font(.system(size: 11, weight: .semibold))
                                .foregroundStyle(tint)
                            Image(systemName: "xmark")
                                .font(.system(size: 8, weight: .bold))
                                .foregroundStyle(.secondary)
                        }
                        .padding(.horizontal, 9)
                        .padding(.vertical, 5)
                        .background(tint.opacity(0.12), in: Capsule())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Remove \(label.title ?? "label")")
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
        }
    }

    private var selectedLabels: [UniboxLabel] {
        var labels: [UniboxLabel] = options
            .filter { selected.contains($0.id) }
            .map { UniboxLabel(id: $0.id, title: $0.name, color: $0.color) }
        let known = Set(labels.map(\.id))
        labels += extraLabels.filter { selected.contains($0.id) && !known.contains($0.id) }
        return labels
    }

    // MARK: Rows

    @ViewBuilder
    private var content: some View {
        if options.isEmpty, trimmedQuery.isEmpty {
            EmptyStateView(
                title: "No labels yet",
                message: "Type a name above to create your first one."
            )
        } else {
            List {
                if !queryMatchesExisting {
                    createRow
                }
                ForEach(filtered) { option in
                    labelRow(option)
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

    private func labelRow(_ option: UserGroup) -> some View {
        Button {
            toggle(option.id)
        } label: {
            HStack(spacing: 10) {
                Circle()
                    .fill(Color(uniboxHex: option.color) ?? WTheme.accent)
                    .frame(width: 11, height: 11)
                Text(option.name ?? "Label")
                    .font(.body.weight(.medium))
                    .foregroundStyle(.primary)
                Spacer(minLength: 8)
                if selected.contains(option.id) {
                    Image(systemName: "checkmark")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(WTheme.accent)
                        .transition(.scale.combined(with: .opacity))
                }
            }
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

    // MARK: Bulk footer

    @ViewBuilder
    private var bulkFooter: some View {
        if case let .bulk(count, onApply) = mode {
            VStack(spacing: 0) {
                Divider()
                Button {
                    Task {
                        applying = true
                        await onApply(Array(selected))
                        applying = false
                        dismiss()
                    }
                } label: {
                    HStack(spacing: 8) {
                        if applying {
                            ProgressView().controlSize(.small).tint(.white)
                        }
                        Text(applying ? "Labeling…" : "Label \(count) conversation\(count == 1 ? "" : "s")")
                            .font(.subheadline.weight(.semibold))
                    }
                    .foregroundStyle(.white)
                    .frame(maxWidth: .infinity)
                    .frame(height: 44)
                    .background(
                        selected.isEmpty ? Color(.systemGray3) : WTheme.accent,
                        in: RoundedRectangle(cornerRadius: 12, style: .continuous)
                    )
                }
                .disabled(selected.isEmpty || applying)
                .padding(16)
            }
            .background(.bar)
        }
    }

    // MARK: State + persistence

    private func loadInitial() async {
        guard !loadedInitial else { return }
        loadedInitial = true
        switch mode {
        case let .thread(store):
            selected = Set(store.labels.map(\.id))
            extraLabels = store.labels
        case let .standalone(threadKey, initial, _):
            // Seed from the row for instant paint; the thread endpoint is the
            // source of truth.
            selected = Set(initial.map(\.id))
            extraLabels = initial
            if let envelope: LabelsEnvelope = try? await env.api.get(
                "unibox/thread/labels", query: ["thread_id": threadKey]
            ), let data = envelope.data {
                withAnimation(.snappy) {
                    selected = Set(data.map(\.id))
                    extraLabels = data
                }
            }
        case .bulk:
            break
        }
    }

    /// Flips one label and saves right away (bulk collects only); reverts the
    /// toggle if the save fails.
    private func toggle(_ id: String) {
        togglePulse += 1
        errorMessage = nil
        let previous = selected
        withAnimation(.snappy) {
            if selected.contains(id) { selected.remove(id) } else { selected.insert(id) }
        }
        guard !isBulk else { return }
        let next = Array(selected)
        Task { await save(next, revertTo: previous) }
    }

    private func save(_ categoryIDs: [String], revertTo previous: Set<String>) async {
        do {
            switch mode {
            case let .thread(store):
                try await store.setLabels(env.api, categoryIDs: categoryIDs)
            case let .standalone(threadKey, _, onChanged):
                let envelope: LabelsEnvelope = try await env.api.put(
                    "unibox/thread/labels",
                    body: UniboxSetLabelsRequest(threadID: threadKey, categoryIDs: categoryIDs)
                )
                onChanged(envelope.data ?? [])
            case .bulk:
                break
            }
        } catch {
            withAnimation(.snappy) { selected = previous }
            errorMessage = error.localizedDescription
        }
    }

    /// POST /categories {title}, refresh the session registry, then toggle the
    /// new label on immediately (mirrors the web ThreadLabelMenu).
    private func createAndSelect() async {
        let title = trimmedQuery
        guard !title.isEmpty, !creating else { return }
        creating = true
        defer { creating = false }
        do {
            let created: UserGroup = try await env.api.post("categories", body: ["title": title])
            await env.session.refreshUser()
            withAnimation(.snappy) { query = "" }
            toggle(created.id)
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
