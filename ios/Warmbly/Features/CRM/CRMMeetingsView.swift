import SwiftUI

/// Meetings browser presented as a full-screen cover from the More hub,
/// copying the campaign leads browser: its own NavigationStack, a slide-in
/// drawer (sky hero with the next meeting badge + upcoming/today/past scopes
/// with a sliding capsule, edge-swipe to open), a search pill with hamburger +
/// presence + circular close, a tracked-uppercase scope caption, and a dense
/// hairline list grouped under eyebrow captions (today gets a sky ping). Rows
/// show time + attendee + source + state pill with an inline join affordance
/// when a call link exists; cancel is gated behind manageContacts. Reloads on
/// the .meetings realtime pulse.
struct CRMMeetingsView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    @State private var store = CRMMeetingsStore()
    @State private var pendingCancel: CRMMeeting?
    @State private var cancelError: String?
    @State private var sidebarOpen = false

    private var canWrite: Bool { env.session.can(.manageContacts) }
    private var isSearching: Bool { !store.query.trimmingCharacters(in: .whitespaces).isEmpty }

    var body: some View {
        NavigationStack {
            CRMBrowserShell(sidebarOpen: $sidebarOpen) {
                mainPane
            } drawer: { topInset in
                CRMMeetingsSidebar(
                    store: store,
                    topInset: topInset,
                    revealed: sidebarOpen,
                    onSelect: { select($0) }
                )
            }
            .toolbar(.hidden, for: .navigationBar)
        }
        .task(id: store.query) {
            if store.hasLoaded {
                try? await Task.sleep(for: .milliseconds(350))
            }
            guard !Task.isCancelled else { return }
            await store.load(env.api)
        }
        .onChange(of: store.scope) {
            Task { await store.load(env.api) }
        }
        .onChange(of: env.realtime.pulse(for: .meetings)) {
            Task { await store.load(env.api) }
        }
        .sensoryFeedback(.selection, trigger: store.scope)
        .confirmationDialog(
            "Cancel meeting?",
            isPresented: Binding(
                get: { pendingCancel != nil },
                set: { if !$0 { pendingCancel = nil } }
            ),
            titleVisibility: .visible,
            presenting: pendingCancel
        ) { meeting in
            Button("Cancel \(meeting.displayTitle)", role: .destructive) {
                Task { await cancel(meeting) }
            }
        } message: { _ in
            Text("This removes it from your meetings.")
        }
        .alert("Couldn't cancel meeting", isPresented: Binding(
            get: { cancelError != nil },
            set: { if !$0 { cancelError = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(cancelError ?? "")
        }
    }

    // MARK: Drawer plumbing

    private func openSidebar() {
        withAnimation(CRMBrowser.spring) { sidebarOpen = true }
    }

    private func select(_ scope: CRMMeetingScope) {
        withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.scope = scope }
        Task {
            try? await Task.sleep(for: .milliseconds(280))
            withAnimation(CRMBrowser.spring) { sidebarOpen = false }
        }
    }

    // MARK: Main pane

    private var mainPane: some View {
        @Bindable var store = store
        return VStack(spacing: 0) {
            CRMSearchBar(
                query: $store.query,
                prompt: "Search meetings",
                onMenu: { openSidebar() },
                menuLabel: "Open meetings menu"
            ) {
                CRMCircleButton(symbol: "xmark", label: "Close meetings", weight: .semibold, size: 15) {
                    dismiss()
                }
            }
            scopeCaption
            list
        }
        .background(Color(.systemBackground))
    }

    // MARK: Caption

    private var scopeCaption: some View {
        HStack(spacing: 6) {
            Text(isSearching ? "SEARCH RESULTS" : store.scope.title.uppercased())
                .font(.caption.weight(.semibold))
                .tracking(0.9)
                .foregroundStyle(.secondary)
            if !isSearching, let count = captionCount, count > 0 {
                Text(WFormat.compact(count))
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
                    .contentTransition(.numericText())
            }
            Spacer()
            if isSearching, store.hasLoaded {
                Text("\(store.visibleMeetings.count) found")
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                    .contentTransition(.numericText())
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, 8)
        .padding(.bottom, 2)
    }

    private var captionCount: Int? {
        store.count(for: store.scope) ?? store.totalCount
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { SkeletonRows(rows: 8) }
        } else if let error = store.errorMessage, store.meetings.isEmpty {
            ErrorStateView(title: "Couldn't load meetings", message: error) {
                await store.load(env.api)
            }
        } else if store.visibleMeetings.isEmpty {
            emptyState
        } else {
            List {
                if store.scope == .upcoming {
                    upcomingGroups
                } else {
                    ForEach(store.visibleMeetings) { meeting in
                        row(meeting)
                    }
                }
                CRMEndMarker(endCount, "meeting")
            }
            .listStyle(.plain)
            .scrollDismissesKeyboard(.immediately)
            .refreshable { await store.load(env.api) }
        }
    }

    /// Today is a client-side slice, so its exact count is the visible count.
    private var endCount: Int {
        if store.scope == .today { return store.visibleMeetings.count }
        return store.totalCount ?? store.visibleMeetings.count
    }

    @ViewBuilder
    private var upcomingGroups: some View {
        let today = store.meetings.filter(\.isToday)
        let later = store.meetings.filter { !$0.isToday }
        if !today.isEmpty {
            CRMListCaption(title: "Today", count: today.count, tone: .sky, ping: true)
            ForEach(today) { meeting in
                row(meeting)
            }
        }
        if !later.isEmpty {
            CRMListCaption(title: "Later", count: later.count)
            ForEach(later) { meeting in
                row(meeting)
            }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        if isSearching {
            EmptyStateView(title: "No matching meetings", message: "Try a different search.")
        } else {
            switch store.scope {
            case .upcoming:
                EmptyStateView(
                    title: "No upcoming meetings",
                    message: "Meetings booked through Calendly, Cal.com or logged manually show up here."
                )
            case .today:
                EmptyStateView(title: "Nothing today", message: "Meetings scheduled for today show up here.")
            case .past:
                EmptyStateView(title: "No past meetings", message: "Meetings that already happened show up here.")
            }
        }
    }

    // MARK: Row

    private func row(_ meeting: CRMMeeting) -> some View {
        HStack(spacing: 12) {
            Circle()
                .fill(meeting.statusTone.color.opacity(meeting.isToday && store.scope != .past ? 1 : 0.5))
                .frame(width: 6, height: 6)
            VStack(alignment: .leading, spacing: 3) {
                Text(meeting.displayTitle)
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                HStack(spacing: 6) {
                    if let scheduled = meeting.scheduledFor {
                        Text(CRMFormat.meetingTime(scheduled))
                            .font(.footnote)
                            .monospacedDigit()
                            .foregroundStyle(meeting.isToday ? WTheme.accent : .secondary)
                    }
                    if let invitee = meeting.inviteeLabel, meeting.eventName?.isEmpty == false {
                        Text(invitee)
                            .font(.footnote)
                            .foregroundStyle(.tertiary)
                            .lineLimit(1)
                    }
                }
            }
            Spacer(minLength: 8)
            if let joinURL = meeting.joinURL, let url = URL(string: joinURL),
               store.scope != .past, meeting.status != "canceled" {
                Link(destination: url) {
                    Image(systemName: "video.fill")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(WTheme.accent)
                        .frame(width: 32, height: 32)
                        .background(Tone.sky.background, in: Circle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Join meeting")
            }
            VStack(alignment: .trailing, spacing: 4) {
                if !meeting.sourceLabel.isEmpty {
                    Text(meeting.sourceLabel)
                        .font(.caption.weight(.medium))
                        .foregroundStyle(.tertiary)
                }
                StatusPill(text: meeting.statusLabel, tone: meeting.statusTone)
            }
        }
        .padding(.vertical, 6)
        .listRowInsets(EdgeInsets(top: 4, leading: 20, bottom: 4, trailing: 16))
        .listRowBackground(Color(.systemBackground))
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if canWrite, meeting.status != "canceled" {
                Button(role: .destructive) {
                    pendingCancel = meeting
                } label: {
                    Label("Cancel", systemImage: "calendar.badge.minus")
                }
            }
        }
        .contextMenu {
            if let joinURL = meeting.joinURL, let url = URL(string: joinURL) {
                Link(destination: url) {
                    Label("Join", systemImage: "video")
                }
            }
            if let rescheduleURL = meeting.rescheduleURL, let url = URL(string: rescheduleURL) {
                Link(destination: url) {
                    Label("Reschedule", systemImage: "calendar")
                }
            }
            if canWrite, meeting.status != "canceled" {
                Button(role: .destructive) {
                    pendingCancel = meeting
                } label: {
                    Label("Cancel", systemImage: "calendar.badge.minus")
                }
            }
        }
    }

    private func cancel(_ meeting: CRMMeeting) async {
        do {
            try await store.cancel(env.api, meeting: meeting)
        } catch {
            cancelError = error.localizedDescription
        }
    }
}

// MARK: - Drawer

/// Meetings drawer: sky hero with a next-meeting badge (falling back to the
/// upcoming count) plus a live today badge, and the three timeframe scopes.
private struct CRMMeetingsSidebar: View {
    let store: CRMMeetingsStore
    let topInset: CGFloat
    let revealed: Bool
    let onSelect: (CRMMeetingScope) -> Void

    @Namespace private var activeNS

    var body: some View {
        CRMDrawer(title: "Meetings", topInset: topInset) {
            if let next = store.nextUpcoming, let scheduled = next.scheduledFor {
                CRMDrawerBadge(symbol: "calendar.badge.clock", text: "Next \(CRMFormat.meetingTime(scheduled))")
            } else {
                CRMDrawerBadge(
                    symbol: "calendar.badge.clock",
                    text: "\(WFormat.compact(store.count(for: .upcoming) ?? 0)) upcoming"
                )
            }
            if let today = store.count(for: .today), today > 0 {
                CRMDrawerBadge(symbol: "sun.max.fill", text: "\(WFormat.compact(today)) today", live: true)
            }
        } rows: {
            CRMDrawerSectionLabel("Timeframe")
            ForEach(Array(CRMMeetingScope.allCases.enumerated()), id: \.element) { index, scope in
                CRMDrawerRow(
                    icon: scope.icon,
                    title: scope.title,
                    count: store.count(for: scope),
                    selected: store.scope == scope,
                    index: index,
                    revealed: revealed,
                    namespace: activeNS
                ) {
                    onSelect(scope)
                }
            }
        }
    }
}
