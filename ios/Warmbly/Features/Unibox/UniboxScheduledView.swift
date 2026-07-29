import SwiftUI

/// All pending scheduled sends across the org, with cancel. Reloads on the
/// unibox pulse (the web polls this; we lean on the realtime spine + refresh).
struct UniboxScheduledView: View {
    @Environment(AppEnvironment.self) private var env

    @State private var store = UniboxScheduledStore()
    @State private var cancelTarget: ScheduledSend?
    @State private var cancelError: String?

    private var canCancel: Bool { env.session.can(.accessUnibox) }

    var body: some View {
        content
            .navigationTitle("Scheduled")
            .navigationBarTitleDisplayMode(.inline)
            .task { await store.load(env.api) }
            .onChange(of: env.realtime.pulse(for: .unibox)) {
                Task { await store.load(env.api) }
            }
            .confirmationDialog(
                "Cancel scheduled send?",
                isPresented: Binding(
                    get: { cancelTarget != nil },
                    set: { if !$0 { cancelTarget = nil } }
                ),
                titleVisibility: .visible,
                presenting: cancelTarget
            ) { item in
                Button("Cancel send", role: .destructive) {
                    Task { await cancel(item) }
                }
            } message: { _ in
                Text("This send won't go out.")
            }
            .alert("Couldn't cancel", isPresented: Binding(
                get: { cancelError != nil },
                set: { if !$0 { cancelError = nil } }
            )) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(cancelError ?? "")
            }
    }

    @ViewBuilder
    private var content: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { SkeletonRows(rows: 6) }
        } else if let error = store.errorMessage, store.items.isEmpty {
            ErrorStateView(title: "Couldn't load scheduled sends", message: error) {
                await store.load(env.api)
            }
        } else if store.items.isEmpty {
            EmptyStateView(
                title: "Nothing scheduled",
                message: "Scheduled replies show up here until they send."
            )
        } else {
            List {
                ForEach(store.items) { item in
                    ScheduledRow(item: item, showThread: true) {
                        if canCancel { cancelTarget = item }
                    }
                    .listRowInsets(EdgeInsets(top: 10, leading: 16, bottom: 10, trailing: 16))
                    .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                        if canCancel {
                            Button(role: .destructive) {
                                cancelTarget = item
                            } label: {
                                Label("Cancel", systemImage: "xmark.circle")
                            }
                        }
                    }
                }
            }
            .listStyle(.plain)
            .refreshable { await store.load(env.api) }
        }
    }

    private func cancel(_ item: ScheduledSend) async {
        do {
            try await store.cancel(env.api, taskID: item.taskID)
        } catch {
            cancelError = error.localizedDescription
        }
    }
}

// MARK: - Shared scheduled row

/// One pending scheduled send. `onCancel` is invoked by the trailing button;
/// list callers additionally wire `.swipeActions`.
struct ScheduledRow: View {
    let item: ScheduledSend
    var showThread: Bool = true
    var onCancel: () -> Void

    private var recipients: String {
        (item.to ?? []).map(UniboxAddress.display).joined(separator: ", ")
    }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            IconTile(symbol: "clock.badge", tone: .amber, size: 40)
            VStack(alignment: .leading, spacing: 3) {
                Text(item.subject?.isEmpty == false ? (item.subject ?? "") : "(no subject)")
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                if !recipients.isEmpty {
                    Text("to \(recipients)")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                if let snippet = item.snippet, !snippet.isEmpty {
                    Text(snippet)
                        .font(.subheadline)
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                }
                HStack(spacing: 6) {
                    Text(UniboxFormat.absoluteTime(item.scheduledAt))
                        .font(.footnote)
                        .monospacedDigit()
                    if let account = item.accountEmail, !account.isEmpty {
                        Text("· \(account)")
                            .font(.footnote)
                            .lineLimit(1)
                    }
                }
                .foregroundStyle(WTheme.warning)
                .padding(.top, 2)
            }
            Spacer(minLength: 6)
            Button(action: onCancel) {
                Image(systemName: "xmark.circle.fill")
                    .font(.system(size: 22))
                    .foregroundStyle(.tertiary)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Cancel scheduled send")
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }
}
