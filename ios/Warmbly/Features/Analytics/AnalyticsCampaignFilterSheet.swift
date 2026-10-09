import SwiftUI

/// Narrows the analytics overview to campaign folders and campaigns, the
/// same filter as the web Analytics page: the server unions every pick and
/// counts each campaign once, and an empty pick is the whole workspace.
/// Folders come from the session; campaigns page in from `GET campaigns`.
struct AnalyticsCampaignFilterSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    var initial: AnalyticsCampaignFilter
    var onApply: (AnalyticsCampaignFilter) -> Void

    @State private var query = ""
    @State private var picked = AnalyticsCampaignFilter()
    @State private var seeded = false
    @State private var campaigns: [Campaign] = []
    @State private var nextCursor: String?
    @State private var hasMore = false
    @State private var isLoading = false
    @State private var errorMessage: String?
    @State private var togglePulse = 0

    private var trimmedQuery: String { query.trimmingCharacters(in: .whitespaces) }

    /// The session's campaign folders, in position order, filtered by the search.
    private var folders: [UserGroup] {
        let all = (env.session.user?.folders ?? []).sorted { ($0.position ?? 0) < ($1.position ?? 0) }
        let q = trimmedQuery.lowercased()
        guard !q.isEmpty else { return all }
        return all.filter { ($0.name ?? "").lowercased().contains(q) }
    }

    var body: some View {
        NavigationStack {
            list
                .navigationTitle("Campaigns")
                .navigationBarTitleDisplayMode(.inline)
                .searchable(
                    text: $query,
                    placement: .navigationBarDrawer(displayMode: .always),
                    prompt: "Search campaigns and folders"
                )
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Apply") {
                            onApply(picked)
                            dismiss()
                        }
                        .fontWeight(.semibold)
                    }
                }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .sensoryFeedback(.selection, trigger: togglePulse)
        .task {
            guard !seeded else { return }
            seeded = true
            picked = initial
        }
        .task(id: trimmedQuery) {
            // Hold the search until typing pauses.
            if !trimmedQuery.isEmpty { try? await Task.sleep(for: .milliseconds(300)) }
            guard !Task.isCancelled else { return }
            await load(reset: true)
        }
    }

    private var list: some View {
        List {
            Button {
                togglePulse += 1
                withAnimation(.snappy) { picked = AnalyticsCampaignFilter() }
            } label: {
                HStack {
                    Text("All campaigns")
                        .font(.body.weight(.medium))
                        .foregroundStyle(.primary)
                    Spacer()
                    if picked.isEmpty {
                        Image(systemName: "checkmark")
                            .font(.body.weight(.semibold))
                            .foregroundStyle(WTheme.accent)
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if !folders.isEmpty {
                Section("Folders") {
                    ForEach(folders) { folder in
                        row(
                            title: folder.name ?? "Folder",
                            dot: Color(uniboxHex: folder.color) ?? WTheme.accent,
                            isPicked: picked.folders.contains(folder.id)
                        ) {
                            toggle(\.folders, folder.id)
                        }
                    }
                }
            }

            Section("Campaigns") {
                ForEach(campaigns) { campaign in
                    row(
                        title: campaign.name,
                        dot: statusColor(campaign.status),
                        isPicked: picked.campaigns.contains(campaign.id)
                    ) {
                        toggle(\.campaigns, campaign.id)
                    }
                    .onAppear {
                        if campaign.id == campaigns.last?.id { Task { await load(reset: false) } }
                    }
                }
                if isLoading {
                    ProgressView().frame(maxWidth: .infinity)
                } else if let errorMessage {
                    Text(errorMessage)
                        .font(.footnote)
                        .foregroundStyle(WTheme.negative)
                } else if campaigns.isEmpty {
                    Text("No campaigns found.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .listStyle(.plain)
        .scrollDismissesKeyboard(.immediately)
    }

    private func row(title: String, dot: Color, isPicked: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Circle()
                    .fill(dot)
                    .frame(width: 9, height: 9)
                Text(title)
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

    private func toggle(_ key: WritableKeyPath<AnalyticsCampaignFilter, Set<String>>, _ id: String) {
        togglePulse += 1
        withAnimation(.snappy) {
            if picked[keyPath: key].contains(id) {
                picked[keyPath: key].remove(id)
            } else {
                picked[keyPath: key].insert(id)
            }
        }
    }

    private func statusColor(_ status: String?) -> Color {
        switch status {
        case "active": WTheme.positive
        case "paused": Tone.amber.color
        default: Color(.systemGray3)
        }
    }

    private func load(reset: Bool) async {
        if !reset, !hasMore || isLoading { return }
        let search = trimmedQuery
        isLoading = true
        defer { isLoading = false }
        var params: [String: String?] = ["limit": "50"]
        if !search.isEmpty { params["q"] = search }
        if !reset, let nextCursor { params["cursor"] = nextCursor }
        do {
            let page: CampaignListPage = try await env.api.get("campaigns", query: params)
            guard search == trimmedQuery else { return }
            let fresh = page.data ?? []
            if reset {
                campaigns = fresh
            } else {
                campaigns.append(contentsOf: fresh.filter { new in !campaigns.contains { $0.id == new.id } })
            }
            nextCursor = page.pagination?.nextCursor
            hasMore = page.pagination?.hasMore ?? false
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}
