import SwiftUI

/// Meetings list pushed from the More tab. Upcoming/past toggle, today rows get
/// a sky ping, source chip (Calendly / Cal.com / manual), join link, cancel
/// gated behind manageContacts. Reloads on the .meetings pulse.
struct CRMMeetingsView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = CRMMeetingsStore()
    @State private var pendingCancel: CRMMeeting?
    @State private var cancelError: String?

    private var canWrite: Bool { env.session.can(.manageContacts) }

    var body: some View {
        @Bindable var store = store
        VStack(spacing: 0) {
            statStrip
            Divider()
            timeframePicker
            Divider()
            list
        }
        .navigationTitle("Meetings")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                PresenceAvatars()
            }
        }
        .searchable(text: $store.query, prompt: "Search meetings")
        .task(id: store.query) {
            if store.hasLoaded {
                try? await Task.sleep(for: .milliseconds(350))
            }
            guard !Task.isCancelled else { return }
            await store.load(env.api)
        }
        .onChange(of: store.timeframe) {
            Task { await store.load(env.api) }
        }
        .onChange(of: env.realtime.pulse(for: .meetings)) {
            Task { await store.load(env.api) }
        }
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

    // MARK: Stat strip

    private var statStrip: some View {
        HStack(spacing: 0) {
            StatCell(label: "Upcoming", value: WFormat.compact(store.summary?.upcoming ?? 0), tone: .sky)
                .padding(.horizontal, 8)
            hairline
            StatCell(
                label: "Today",
                value: WFormat.compact(store.summary?.today ?? 0),
                tone: (store.summary?.today ?? 0) > 0 ? .sky : nil
            )
            .padding(.horizontal, 8)
            hairline
            StatCell(label: "Total", value: WFormat.compact(store.summary?.total ?? 0), tone: .slate)
                .padding(.horizontal, 8)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
    }

    private var hairline: some View {
        Rectangle().fill(Color(.separator)).frame(width: 0.5, height: 34)
    }

    private var timeframePicker: some View {
        @Bindable var store = store
        return Picker("Timeframe", selection: $store.timeframe) {
            Text("Upcoming").tag("upcoming")
            Text("Past").tag("past")
        }
        .pickerStyle(.segmented)
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
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
        } else if store.meetings.isEmpty {
            emptyState
        } else {
            List {
                ForEach(store.meetings) { meeting in
                    row(meeting)
                }
            }
            .listStyle(.plain)
            .refreshable { await store.load(env.api) }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        let searching = !store.query.trimmingCharacters(in: .whitespaces).isEmpty
        EmptyStateView(
            title: searching ? "No matching meetings" : (store.timeframe == "upcoming" ? "No upcoming meetings" : "No past meetings"),
            message: searching
                ? "Try a different search."
                : "Meetings booked through Calendly, Cal.com or logged manually show up here."
        )
    }

    // MARK: Row

    private func row(_ meeting: CRMMeeting) -> some View {
        HStack(spacing: 12) {
            if meeting.isToday, store.timeframe == "upcoming" {
                CRMPingDot(color: WTheme.accent)
            } else {
                Circle()
                    .fill(meeting.statusTone.color.opacity(0.5))
                    .frame(width: 6, height: 6)
            }
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
