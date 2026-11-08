import SwiftUI

/// Meetings browser presented from the settings hub: a next-up hero with a
/// live countdown and join button, today/upcoming/canceled tiles, and a
/// day-grouped agenda. Cancel is gated behind manageContacts.
struct CRMMeetingsView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    @State private var store = CRMMeetingsStore()
    @State private var pendingCancel: CRMMeeting?
    @State private var cancelError: String?
    @State private var sidebarOpen = false
    @State private var shown = false
    @State private var lastNext: CRMMeeting?
    @FocusState private var searchFocused: Bool

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

    private func select(_ scope: CRMMeetingScope, close: Bool = true) {
        if let next = store.nextUpcoming { lastNext = next }
        withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.scope = scope }
        guard close else { return }
        Task {
            try? await Task.sleep(for: .milliseconds(280))
            withAnimation(CRMBrowser.spring) { sidebarOpen = false }
        }
    }

    // MARK: Main pane

    private var mainPane: some View {
        @Bindable var store = store
        return VStack(spacing: 0) {
            GlassHeaderBar(
                title: "Meetings",
                subtitle: store.hasLoaded ? Text("\(store.count(for: .upcoming) ?? 0) upcoming") : nil,
                query: $store.query,
                searchPrompt: "Search meetings",
                searchFocused: $searchFocused,
                menuLabel: "Open meetings menu",
                onMenu: openSidebar
            ) {
                GlassCircleButton(symbol: "xmark", label: "Close meetings") { dismiss() }
            }
            list
        }
        .background(WTheme.groupedPage)
    }

    // MARK: Next up

    @ViewBuilder
    private var heroCard: some View {
        if let next = heroMeeting {
            NextMeetingCard(meeting: next, shown: shown)
        } else {
            HStack(spacing: 14) {
                SettingsIcon(symbol: "calendar", color: WTheme.accent, size: 46)
                VStack(alignment: .leading, spacing: 3) {
                    Text("Your calendar is clear")
                        .font(.system(size: 17, weight: .semibold))
                        .foregroundStyle(WTheme.ink)
                    Text("Booked calls from Calendly and Cal.com land here.")
                        .font(.system(size: 13.5))
                        .foregroundStyle(WTheme.inkSecondary)
                }
                Spacer(minLength: 0)
            }
            .coverCard()
        }
    }

    /// The meeting the hero counts down to, kept while the past scope is open.
    private var heroMeeting: CRMMeeting? {
        store.nextUpcoming ?? lastNext
    }

    private var statsRow: some View {
        HStack(spacing: 10) {
            statTile("Today", value: store.count(for: .today) ?? 0, symbol: "sun.max.fill", tone: .amber, index: 0)
            statTile("Upcoming", value: store.count(for: .upcoming) ?? 0, symbol: "calendar.badge.clock", tone: .sky, index: 1)
            statTile("Canceled", value: store.summary?.canceled ?? 0, symbol: "calendar.badge.minus", tone: .rose, index: 2)
        }
    }

    private func statTile(_ title: String, value: Int, symbol: String, tone: Tone, index: Int) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Image(systemName: symbol)
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(tone.color)
                .frame(width: 28, height: 28)
                .background(tone.background, in: Circle())
            CountUpText(value: shown ? Double(value) : 0, format: { WFormat.compact(Int($0.rounded())) })
                .font(.system(size: 22, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .animation(.spring(response: 0.9, dampingFraction: 0.9).delay(0.1 + Double(index) * 0.06), value: shown)
                .foregroundStyle(WTheme.ink)
            Text(title)
                .font(.system(size: 12.5, weight: .medium))
                .foregroundStyle(WTheme.inkSecondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(14)
        .background(WTheme.groupedCard, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        .reveal(shown, index + 1)
    }

    // MARK: Agenda

    private struct DayGroup: Identifiable {
        let id: Date
        let title: String
        let isToday: Bool
        let meetings: [CRMMeeting]
    }

    private var dayGroups: [DayGroup] {
        let calendar = Calendar.current
        var order: [Date] = []
        var buckets: [Date: [CRMMeeting]] = [:]
        for meeting in store.visibleMeetings {
            let day = calendar.startOfDay(for: meeting.scheduledFor ?? .distantPast)
            if buckets[day] == nil { order.append(day) }
            buckets[day, default: []].append(meeting)
        }
        return order.map { day in
            DayGroup(id: day, title: dayTitle(day), isToday: calendar.isDateInToday(day), meetings: buckets[day] ?? [])
        }
    }

    private func dayTitle(_ day: Date) -> String {
        let calendar = Calendar.current
        if day == .distantPast { return "Unscheduled" }
        if calendar.isDateInToday(day) { return "Today" }
        if calendar.isDateInTomorrow(day) { return "Tomorrow" }
        if calendar.isDateInYesterday(day) { return "Yesterday" }
        let sameYear = calendar.isDate(day, equalTo: .now, toGranularity: .year)
        return sameYear
            ? day.formatted(.dateTime.weekday(.wide).month(.wide).day())
            : day.formatted(.dateTime.month(.wide).day().year())
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { AnalyticsLoadingCards().padding(16) }
        } else if let error = store.errorMessage, store.meetings.isEmpty {
            ErrorStateView(title: "Couldn't load meetings", message: error) {
                await store.load(env.api)
            }
        } else {
            List {
                Section {
                    VStack(spacing: 12) {
                        if !isSearching {
                            heroCard
                                .reveal(shown, 0)
                            statsRow
                        }
                        PillSwitcher(
                            options: CRMMeetingScope.allCases,
                            selection: Binding(get: { store.scope }, set: { select($0, close: false) }),
                            label: \.title
                        )
                        .padding(.top, 2)
                    }
                    .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 0, trailing: 0))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
                }
                if store.visibleMeetings.isEmpty {
                    Section {
                        emptyState
                            .listRowBackground(Color.clear)
                            .listRowSeparator(.hidden)
                    }
                } else {
                    ForEach(dayGroups) { group in
                        Section {
                            ForEach(group.meetings) { meeting in
                                row(meeting)
                            }
                        } header: {
                            CoverSectionHeader(
                                title: group.title,
                                count: group.meetings.count,
                                color: group.isToday ? WTheme.accent : nil,
                                pulsing: group.isToday && store.scope != .past
                            )
                        }
                    }
                    Section {
                        CRMEndMarker(endCount, "meeting")
                            .listRowBackground(Color.clear)
                    }
                }
            }
            .listStyle(.insetGrouped)
            .readableMargins()
            .listSectionSpacing(18)
            .contentMargins(.horizontal, 16, for: .scrollContent)
            .contentMargins(.top, 0, for: .scrollContent)
            .scrollContentBackground(.hidden)
            .scrollDismissesKeyboard(.immediately)
            .defaultScrollAnchor(SettingsDebug.anchor)
            .refreshable { await store.load(env.api) }
            .onAppear { shown = true }
        }
    }

    /// Today is a client-side slice, so its exact count is the visible count.
    private var endCount: Int {
        if store.scope == .today { return store.visibleMeetings.count }
        return store.totalCount ?? store.visibleMeetings.count
    }

    @ViewBuilder
    private var emptyState: some View {
        if isSearching {
            CoverEmptyState(symbol: "magnifyingglass", color: WTheme.inkSecondary, title: "No matching meetings", message: "Try a different search.")
        } else {
            switch store.scope {
            case .upcoming:
                CoverEmptyState(
                    symbol: "calendar.badge.clock",
                    title: "No upcoming meetings",
                    message: "Meetings booked through Calendly, Cal.com or logged by hand show up here."
                )
            case .today:
                CoverEmptyState(symbol: "sun.max.fill", color: WTheme.warning, title: "Nothing today", message: "A clear day. Good time to follow up on replies.")
            case .past:
                CoverEmptyState(symbol: "clock.arrow.circlepath", color: WTheme.special, title: "No past meetings", message: "Meetings that already happened show up here.")
            }
        }
    }

    // MARK: Row

    private func row(_ meeting: CRMMeeting) -> some View {
        let canceled = meeting.status == "canceled"
        let live = meeting.isLive
        return HStack(spacing: 12) {
            VStack(alignment: .trailing, spacing: 2) {
                if let start = meeting.scheduledFor {
                    Text(start.formatted(date: .omitted, time: .shortened))
                        .font(.system(size: 14.5, weight: .semibold))
                        .monospacedDigit()
                        .foregroundStyle(canceled ? WTheme.inkTertiary : WTheme.ink)
                } else {
                    Text("TBD")
                        .font(.system(size: 14.5, weight: .semibold))
                        .foregroundStyle(WTheme.inkTertiary)
                }
                if let minutes = meeting.durationMinutes {
                    Text(minutes >= 60 && minutes % 60 == 0 ? "\(minutes / 60) h" : "\(minutes) min")
                        .font(.system(size: 12))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.inkTertiary)
                }
            }
            .frame(width: 66, alignment: .trailing)
            Capsule()
                .fill(live ? WTheme.positive : meeting.statusTone.color.opacity(canceled ? 0.35 : 0.85))
                .frame(width: 3.5)
                .padding(.vertical, 2)
            VStack(alignment: .leading, spacing: 4) {
                Text(meeting.displayTitle)
                    .font(.system(size: 15.5, weight: .medium))
                    .foregroundStyle(canceled ? WTheme.inkTertiary : WTheme.ink)
                    .strikethrough(canceled, color: WTheme.inkTertiary)
                    .lineLimit(1)
                HStack(spacing: 6) {
                    if let invitee = meeting.inviteeLabel, meeting.eventName?.isEmpty == false {
                        Text(invitee)
                            .lineLimit(1)
                    }
                    if meeting.source != "manual", !meeting.sourceLabel.isEmpty {
                        Text(meeting.sourceLabel)
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                }
                .font(.system(size: 13))
                .foregroundStyle(WTheme.inkSecondary)
                if live {
                    LiveTag()
                } else if meeting.status != "booked", meeting.status != nil {
                    StatusPill(text: meeting.statusLabel.prefix(1).uppercased() + meeting.statusLabel.dropFirst(), tone: meeting.statusTone)
                }
            }
            Spacer(minLength: 6)
            if let joinURL = meeting.joinURL, let url = URL(string: joinURL),
               store.scope != .past, !canceled {
                Link(destination: url) {
                    Image(systemName: "video.fill")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(live ? .white : WTheme.accent)
                        .frame(width: 34, height: 34)
                        .background(live ? WTheme.positive : Tone.sky.background, in: Circle())
                }
                .buttonStyle(TapScaleStyle())
                .accessibilityLabel("Join meeting")
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .padding(.vertical, 4)
        .listRowBackground(WTheme.groupedCard)
        .listRowInsets(EdgeInsets(top: 8, leading: 8, bottom: 8, trailing: 14))
        .alignmentGuide(.listRowSeparatorLeading) { _ in 90 }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if canWrite, !canceled {
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
            if canWrite, !canceled {
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

// MARK: - Next meeting

/// The hero: the next meeting with a live countdown, who it is with, and a
/// join button that turns green once the call is starting.
private struct NextMeetingCard: View {
    let meeting: CRMMeeting
    let shown: Bool

    var body: some View {
        TimelineView(.periodic(from: .now, by: 15)) { context in
            let now = context.date
            let live = meeting.isLive(at: now)
            let soon = (meeting.scheduledFor?.timeIntervalSince(now) ?? .infinity) < 10 * 60
            VStack(alignment: .leading, spacing: 14) {
                HStack(spacing: 6) {
                    if live || soon {
                        PulsingDot(color: live ? WTheme.positive : WTheme.accent)
                    }
                    Text(live ? "Happening now" : "Next up")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(live ? WTheme.positive : WTheme.inkSecondary)
                    Spacer()
                    if meeting.source != "manual", !meeting.sourceLabel.isEmpty {
                        Text(meeting.sourceLabel)
                            .font(.system(size: 12, weight: .medium))
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                }
                Text(countdown(now: now, live: live))
                    .font(.system(size: 34, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(live ? WTheme.positive : WTheme.ink)
                    .contentTransition(.numericText(countsDown: true))
                    .animation(.snappy, value: countdown(now: now, live: live))
                HStack(spacing: 12) {
                    WAvatar(name: meeting.inviteeLabel ?? meeting.displayTitle, seed: meeting.inviteeEmail ?? meeting.id, size: 40)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(meeting.displayTitle)
                            .font(.system(size: 16, weight: .semibold))
                            .foregroundStyle(WTheme.ink)
                            .lineLimit(1)
                        Text(subtitle)
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkSecondary)
                            .lineLimit(1)
                    }
                    Spacer(minLength: 0)
                }
                if let joinURL = meeting.joinURL, let url = URL(string: joinURL) {
                    Link(destination: url) {
                        HStack(spacing: 8) {
                            Image(systemName: "video.fill")
                            Text(live ? "Join now" : "Join call")
                        }
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(.white)
                        .frame(maxWidth: .infinity)
                        .frame(height: 46)
                        .background(live || soon ? WTheme.positive : WTheme.ink, in: Capsule())
                    }
                    .buttonStyle(TapScaleStyle())
                    .animation(.snappy, value: live || soon)
                } else if let location = meeting.location, !location.isEmpty {
                    Label(location, systemImage: "mappin.and.ellipse")
                        .font(.system(size: 13.5))
                        .foregroundStyle(WTheme.inkSecondary)
                        .lineLimit(1)
                }
            }
        }
        .coverCard()
    }

    private var subtitle: String {
        var parts: [String] = []
        if let invitee = meeting.inviteeLabel, meeting.eventName?.isEmpty == false { parts.append(invitee) }
        if let start = meeting.scheduledFor {
            var when = CRMFormat.meetingTime(start)
            when = when.prefix(1).uppercased() + when.dropFirst()
            if let end = meeting.endTime {
                when += " to " + end.formatted(date: .omitted, time: .shortened)
            }
            parts.append(when)
        }
        return parts.joined(separator: " · ")
    }

    private func countdown(now: Date, live: Bool) -> String {
        guard let start = meeting.scheduledFor else { return "Not scheduled" }
        if live {
            guard let end = meeting.endTime else { return "Started" }
            let left = max(1, Int(end.timeIntervalSince(now) / 60))
            return "\(left) min left"
        }
        let seconds = start.timeIntervalSince(now)
        if seconds < 60 { return "Starting now" }
        let minutes = Int(seconds / 60)
        if minutes < 60 { return "In \(minutes) min" }
        let hours = minutes / 60
        if hours < 24 { return minutes % 60 == 0 ? "In \(hours) h" : "In \(hours) h \(minutes % 60) min" }
        let days = Calendar.current.dateComponents([.day], from: Calendar.current.startOfDay(for: now), to: Calendar.current.startOfDay(for: start)).day ?? hours / 24
        return days == 1 ? "Tomorrow" : "In \(days) days"
    }
}

/// A small green "Live" tag for a meeting in progress.
private struct LiveTag: View {
    var body: some View {
        HStack(spacing: 5) {
            PulsingDot(color: WTheme.positive, size: 6)
            Text("Live")
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(WTheme.positive)
        }
    }
}

/// A dot with a soft ring breathing out of it.
private struct PulsingDot: View {
    var color: Color
    var size: CGFloat = 7
    @State private var pulse = false

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: size, height: size)
            .background {
                Circle()
                    .fill(color.opacity(0.35))
                    .scaleEffect(pulse ? 2.6 : 1)
                    .opacity(pulse ? 0 : 1)
            }
            .onAppear {
                withAnimation(.easeOut(duration: 1.4).repeatForever(autoreverses: false)) { pulse = true }
            }
    }
}

extension CRMMeeting {
    var durationMinutes: Int? {
        guard let start = scheduledFor, let end = endTime, end > start else { return nil }
        return Int(end.timeIntervalSince(start) / 60)
    }

    func isLive(at now: Date = .now) -> Bool {
        guard status != "canceled", let start = scheduledFor else { return false }
        let end = endTime ?? start.addingTimeInterval(30 * 60)
        return start <= now && now < end
    }

    var isLive: Bool { isLive() }
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
