import SwiftUI

/// Tasks browser presented as a full-screen cover from the More hub, copying
/// the campaign leads browser: its own NavigationStack, a slide-in drawer (sky
/// hero with open/overdue badges + scope pills with live counts and a sliding
/// capsule, edge-swipe to open), a search pill with hamburger + presence +
/// circular create and close buttons, a tracked-uppercase scope caption, and
/// the scoped tasks grouped under due-date eyebrow captions (overdue in rose
/// with a ping, today, tomorrow, upcoming, no due date, done). Tap the circle
/// to complete or reopen (gated behind manageContacts). Reloads on the .crm
/// realtime pulse.
struct CRMTasksView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    @State private var store = CRMTasksStore()
    @State private var showCreate = false
    @State private var sidebarOpen = false
    @State private var shown = false
    @State private var celebrate = 0
    @State private var completedPulse = 0
    /// Just-toggled tasks, held in their old place (by their old state) so the check animates before they move.
    @State private var lingering: [String: CRMTask] = [:]
    @FocusState private var searchFocused: Bool

    private var canWrite: Bool { env.session.can(.manageContacts) }
    private var isSearching: Bool { !store.query.trimmingCharacters(in: .whitespaces).isEmpty }
    private var scopedTasks: [CRMTask] {
        let ids = Set(store.tasks(in: store.scope).map(\.id))
        return store.tasks.filter { ids.contains($0.id) || lingering[$0.id] != nil }
    }

    var body: some View {
        NavigationStack {
            CRMBrowserShell(sidebarOpen: $sidebarOpen) {
                mainPane
            } drawer: { topInset in
                CRMTasksSidebar(
                    store: store,
                    topInset: topInset,
                    revealed: sidebarOpen,
                    onSelect: { select($0) }
                )
            }
            .toolbar(.hidden, for: .navigationBar)
            .sheet(isPresented: $showCreate) {
                CRMTaskCreateSheet(store: store)
            }
        }
        .task(id: store.query) {
            if store.hasLoaded {
                try? await Task.sleep(for: .milliseconds(350))
            }
            guard !Task.isCancelled else { return }
            await store.load(env.api)
        }
        .onChange(of: env.realtime.pulse(for: .crm)) {
            Task { await store.load(env.api) }
        }
        .sensoryFeedback(.selection, trigger: store.scope)
    }

    // MARK: Drawer plumbing

    private func openSidebar() {
        withAnimation(CRMBrowser.spring) { sidebarOpen = true }
    }

    /// Scope changes are a client-side refilter of the loaded page; no reload.
    private func select(_ scope: CRMTaskScope) {
        withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.scope = scope }
        Task {
            try? await Task.sleep(for: .milliseconds(280))
            withAnimation(CRMBrowser.spring) { sidebarOpen = false }
        }
    }

    // MARK: Main pane

    private var mainPane: some View {
        @Bindable var store = store
        let open = store.count(for: .open)
        return VStack(spacing: 0) {
            GlassHeaderBar(
                title: "Tasks",
                subtitle: store.hasLoaded ? Text("\(open) open") : nil,
                query: $store.query,
                searchPrompt: "Search tasks",
                searchFocused: $searchFocused,
                menuLabel: "Open tasks menu",
                onMenu: openSidebar
            ) {
                if canWrite {
                    HeaderPrimaryButton(symbol: "plus", label: "New task") { showCreate = true }
                }
                GlassCircleButton(symbol: "xmark", label: "Close tasks") { dismiss() }
            }
            list
        }
        .background(WTheme.groupedPage)
        .overlay { ConfettiBurst(trigger: celebrate) }
        .sensoryFeedback(.success, trigger: celebrate)
        .sensoryFeedback(.impact(weight: .light), trigger: completedPulse)
    }

    // MARK: Today

    private var todayCard: some View {
        let left = store.tasks(in: .dueToday).count
        let done = store.doneToday
        let total = left + done
        let fraction = total > 0 ? Double(done) / Double(total) : 1
        let overdue = store.count(for: .overdue)
        let upcoming = store.count(for: .upcoming)
        let headline = left > 0 ? "\(left) left for today" : (total > 0 ? "All done for today" : "Nothing due today")
        return HStack(spacing: 16) {
            SweepRing(fraction: fraction, color: left == 0 ? WTheme.positive : WTheme.accent, shown: shown, lineWidth: 9) {
                if left == 0 {
                    Image(systemName: "checkmark")
                        .font(.system(size: 26, weight: .bold))
                        .foregroundStyle(WTheme.positive)
                        .symbolEffect(.bounce, value: celebrate)
                        .transition(.scale.combined(with: .opacity))
                } else {
                    VStack(spacing: -1) {
                        Text("\(done)/\(total)")
                            .font(.system(size: 19, weight: .semibold, design: .rounded))
                            .monospacedDigit()
                            .foregroundStyle(WTheme.ink)
                            .contentTransition(.numericText())
                        Text("done")
                            .font(.system(size: 11, weight: .medium))
                            .foregroundStyle(WTheme.inkTertiary)
                    }
                }
            }
            .frame(width: 86, height: 86)
            .animation(.spring(response: 0.5, dampingFraction: 0.7), value: left == 0)
            VStack(alignment: .leading, spacing: 5) {
                Text(Date.now.formatted(.dateTime.weekday(.wide).month(.wide).day()))
                    .font(.system(size: 13.5, weight: .medium))
                    .foregroundStyle(WTheme.inkSecondary)
                Text(headline)
                    .font(.system(size: 21, weight: .semibold))
                    .foregroundStyle(WTheme.ink)
                    .contentTransition(.numericText())
                HStack(spacing: 10) {
                    if overdue > 0 {
                        Label("\(overdue) overdue", systemImage: "exclamationmark.circle.fill")
                            .foregroundStyle(WTheme.negative)
                    }
                    Label("\(upcoming) coming up", systemImage: "calendar")
                        .foregroundStyle(WTheme.inkSecondary)
                }
                .font(.system(size: 13, weight: .medium))
                .labelStyle(CompactLabelStyle())
            }
            Spacer(minLength: 0)
        }
        .coverCard()
        .animation(.snappy, value: left)
    }

    // MARK: Due-date groups

    private struct TaskBucket: Identifiable {
        let id: String
        let title: String
        var color: Color?
        var ping = false
        let tasks: [CRMTask]
    }

    private var groups: [TaskBucket] {
        var result: [TaskBucket] = []
        let scoped = scopedTasks
        let placed = { (task: CRMTask) in lingering[task.id] ?? task }
        let overdue = scoped.filter { placed($0).isOverdue }
        if !overdue.isEmpty {
            result.append(TaskBucket(id: "overdue", title: "Overdue", color: WTheme.negative, ping: true, tasks: overdue))
        }
        let open = scoped.filter { !placed($0).isDone && !placed($0).isOverdue }
        let calendar = Calendar.current
        let today = open.filter { $0.dueDate.map(calendar.isDateInToday) ?? false }
        let tomorrow = open.filter { $0.dueDate.map(calendar.isDateInTomorrow) ?? false }
        let later = open.filter { task in
            guard let due = task.dueDate else { return false }
            return !calendar.isDateInToday(due) && !calendar.isDateInTomorrow(due)
        }
        let undated = open.filter { $0.dueDate == nil }
        if !today.isEmpty { result.append(TaskBucket(id: "today", title: "Today", color: WTheme.accent, tasks: today)) }
        if !tomorrow.isEmpty { result.append(TaskBucket(id: "tomorrow", title: "Tomorrow", color: WTheme.special, tasks: tomorrow)) }
        if !later.isEmpty { result.append(TaskBucket(id: "later", title: "Upcoming", color: WTheme.paused, tasks: later)) }
        if !undated.isEmpty { result.append(TaskBucket(id: "undated", title: "No due date", tasks: undated)) }
        let done = scoped.filter { placed($0).isDone }
        if !done.isEmpty { result.append(TaskBucket(id: "done", title: "Done", color: WTheme.positive, tasks: done)) }
        return result
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { AnalyticsLoadingCards().padding(16) }
        } else if let error = store.errorMessage, store.tasks.isEmpty {
            ErrorStateView(title: "Couldn't load tasks", message: error) {
                await store.load(env.api)
            }
        } else {
            List {
                Section {
                    VStack(alignment: .leading, spacing: 14) {
                        if !isSearching {
                            todayCard
                        }
                        ChipFilterRow(
                            options: CRMTaskScope.allCases,
                            selection: Binding(get: { store.scope }, set: { store.scope = $0 }),
                            label: \.title,
                            count: { store.count(for: $0) },
                            alert: { $0 == .overdue && store.count(for: .overdue) > 0 }
                        )
                    }
                    .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 0, trailing: 0))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
                }
                if scopedTasks.isEmpty {
                    Section {
                        emptyState
                            .listRowBackground(Color.clear)
                            .listRowSeparator(.hidden)
                    }
                } else {
                    ForEach(groups) { group in
                        Section {
                            ForEach(group.tasks) { task in
                                row(task)
                            }
                        } header: {
                            CoverSectionHeader(title: group.title, count: group.tasks.count, color: group.color, pulsing: group.ping)
                        }
                    }
                }
                Section {
                    CRMEndMarker(scopedTasks.count, "task")
                        .listRowBackground(Color.clear)
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

    @ViewBuilder
    private var emptyState: some View {
        if isSearching {
            CoverEmptyState(symbol: "magnifyingglass", color: WTheme.inkSecondary, title: "No matching tasks", message: "Try a different search.")
        } else {
            switch store.scope {
            case .open:
                CoverEmptyState(
                    symbol: "checklist",
                    title: "No tasks yet",
                    message: "Follow-ups and reminders you create land here.",
                    actionTitle: canWrite ? "New task" : nil,
                    action: canWrite ? { showCreate = true } : nil
                )
            case .dueToday:
                CoverEmptyState(symbol: "sun.max.fill", color: WTheme.warning, title: "Nothing due today", message: "Enjoy the quiet, or get ahead on tomorrow.")
            case .overdue:
                CoverEmptyState(symbol: "checkmark.seal.fill", color: WTheme.positive, title: "Nothing overdue", message: "You're all caught up.")
            case .upcoming:
                CoverEmptyState(symbol: "calendar", color: WTheme.special, title: "Nothing upcoming", message: "Tasks due after today show up here.")
            case .completed:
                CoverEmptyState(symbol: "checkmark.circle.fill", color: WTheme.positive, title: "Nothing finished yet", message: "Tick a task off and it lands here.")
            }
        }
    }

    // MARK: Row

    private func row(_ task: CRMTask) -> some View {
        let priorityColor: Color? = switch task.priority {
        case "urgent": WTheme.negative
        case "high": Tone.orange.color
        default: nil
        }
        let calendar = Calendar.current
        let dueColor: Color = task.isOverdue ? WTheme.negative : (task.dueDate.map(calendar.isDateInToday) ?? false) ? WTheme.accent : WTheme.inkSecondary
        return HStack(spacing: 8) {
            TaskCheck(done: task.isDone) {
                Task { await toggle(task) }
            }
            .disabled(!canWrite)
            VStack(alignment: .leading, spacing: 4) {
                Text(task.title)
                    .font(.system(size: 15.5, weight: .medium))
                    .foregroundStyle(task.isDone ? WTheme.inkTertiary : WTheme.ink)
                    .strikethrough(task.isDone, color: WTheme.inkTertiary)
                    .lineLimit(2)
                HStack(spacing: 8) {
                    if let due = task.dueDate, !task.isDone {
                        Label(CRMFormat.dueLabel(due).prefix(1).uppercased() + CRMFormat.dueLabel(due).dropFirst(), systemImage: task.isOverdue ? "exclamationmark.circle" : "clock")
                            .labelStyle(CompactLabelStyle())
                            .font(.system(size: 13))
                            .monospacedDigit()
                            .foregroundStyle(dueColor)
                    }
                    if let type = task.type, !type.isEmpty {
                        CRMColorChip(text: type, hex: typeColor(type))
                    }
                }
            }
            Spacer(minLength: 6)
            if let priorityColor, !task.isDone {
                Image(systemName: "flag.fill")
                    .font(.system(size: 12))
                    .foregroundStyle(priorityColor)
                    .accessibilityLabel("\(task.priority ?? "") priority")
            }
        }
        .padding(.vertical, 2)
        .animation(.spring(response: 0.4, dampingFraction: 0.8), value: task.isDone)
        .listRowBackground(WTheme.groupedCard)
        .listRowInsets(EdgeInsets(top: 6, leading: 6, bottom: 6, trailing: 14))
        .alignmentGuide(.listRowSeparatorLeading) { _ in 50 }
        .swipeActions(edge: .leading, allowsFullSwipe: true) {
            if canWrite {
                Button {
                    Task { await toggle(task) }
                } label: {
                    Label(task.isDone ? "Reopen" : "Done", systemImage: task.isDone ? "arrow.uturn.backward" : "checkmark")
                }
                .tint(task.isDone ? WTheme.paused : WTheme.positive)
            }
        }
    }

    private func typeColor(_ name: String) -> String? {
        store.types.first { $0.name == name }?.color
    }

    private func toggle(_ task: CRMTask) async {
        guard canWrite else { return }
        let leftBefore = store.tasks(in: .dueToday).count
        if !task.isDone { completedPulse += 1 }
        lingering[task.id] = task
        Task {
            try? await Task.sleep(for: .milliseconds(900))
            _ = withAnimation(.spring(response: 0.45, dampingFraction: 0.85)) { lingering.removeValue(forKey: task.id) }
        }
        try? await store.setDone(env.api, task: task, done: !task.isDone)
        // Clearing the last task due today earns a little celebration.
        if !task.isDone, leftBefore > 0, store.tasks(in: .dueToday).isEmpty {
            celebrate += 1
        }
    }
}

// MARK: - Drawer

/// Tasks drawer: sky hero with live open/overdue badges (overdue pings when
/// non-zero) and the five scopes as pill rows. Counts prefer the server
/// summary; due today and upcoming are derived from the loaded page.
private struct CRMTasksSidebar: View {
    let store: CRMTasksStore
    let topInset: CGFloat
    let revealed: Bool
    let onSelect: (CRMTaskScope) -> Void

    @Namespace private var activeNS

    var body: some View {
        CRMDrawer(title: "Tasks", topInset: topInset) {
            CRMDrawerBadge(symbol: "checklist", text: "\(WFormat.compact(store.count(for: .open))) open")
            let overdue = store.count(for: .overdue)
            if overdue > 0 {
                CRMDrawerBadge(
                    symbol: "exclamationmark.circle.fill",
                    text: "\(WFormat.compact(overdue)) overdue",
                    live: true,
                    pingColor: Tone.rose.color
                )
            }
        } rows: {
            CRMDrawerSectionLabel("Scope")
            ForEach(Array(CRMTaskScope.allCases.enumerated()), id: \.element) { index, scope in
                let count = store.count(for: scope)
                if scope == .overdue, count > 0 {
                    CRMDrawerRow(
                        pingDot: Tone.rose.color,
                        title: scope.title,
                        count: count,
                        selected: store.scope == scope,
                        index: index,
                        revealed: revealed,
                        namespace: activeNS
                    ) {
                        onSelect(scope)
                    }
                } else {
                    CRMDrawerRow(
                        icon: scope.icon,
                        title: scope.title,
                        count: count,
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
}

// MARK: - Create sheet

/// Create a task: title, optional due date, priority, and type from the seeded
/// task-type list. Only title is required server-side.
struct CRMTaskCreateSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    let store: CRMTasksStore

    @State private var title = ""
    @State private var priority = "medium"
    @State private var typeName = ""
    @State private var hasDueDate = false
    @State private var dueDate = Calendar.current.date(byAdding: .day, value: 1, to: Date()) ?? Date()
    @State private var saving = false
    @State private var errorMessage: String?

    private let priorities = ["low", "medium", "high", "urgent"]

    var body: some View {
        NavigationStack {
            Form {
                Section("Task") {
                    TextField("Title", text: $title)
                }
                Section("Due") {
                    Toggle("Set due date", isOn: $hasDueDate.animation())
                        .tint(WTheme.accent)
                    if hasDueDate {
                        DatePicker("Due date", selection: $dueDate, displayedComponents: [.date, .hourAndMinute])
                    }
                }
                Section("Priority") {
                    Picker("Priority", selection: $priority) {
                        ForEach(priorities, id: \.self) { value in
                            Text(value.capitalized).tag(value)
                        }
                    }
                    .pickerStyle(.segmented)
                }
                if !store.types.isEmpty {
                    Section("Type") {
                        Picker("Type", selection: $typeName) {
                            Text("None").tag("")
                            ForEach(store.types) { type in
                                Text(type.name).tag(type.name)
                            }
                        }
                    }
                }
                if let errorMessage {
                    Text(errorMessage)
                        .font(.footnote)
                        .foregroundStyle(WTheme.negative)
                }
            }
            .navigationTitle("New task")
            .navigationBarTitleDisplayMode(.inline)
            .presenceResource(nil, action: "editing")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if saving {
                        ProgressView().controlSize(.small)
                    } else {
                        Button("Create") { create() }
                            .disabled(title.trimmingCharacters(in: .whitespaces).isEmpty)
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    private func create() {
        let trimmed = title.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, !saving else { return }
        saving = true
        errorMessage = nil
        var body = CRMTaskCreateBody(title: trimmed)
        body.priority = priority
        if hasDueDate { body.dueDate = dueDate }
        if !typeName.isEmpty { body.type = typeName }

        Task {
            do {
                try await store.create(env.api, body: body)
                dismiss()
            } catch {
                errorMessage = error.localizedDescription
            }
            saving = false
        }
    }
}

// MARK: - Check

/// A round check that fills and draws its tick when done, with a ring that
/// ripples out as it lands.
struct TaskCheck: View {
    let done: Bool
    var tint: Color = WTheme.inkTertiary
    let action: () -> Void

    @State private var ripple = false

    var body: some View {
        Button {
            if !done {
                ripple = false
                withAnimation(.easeOut(duration: 0.55)) { ripple = true }
            }
            action()
        } label: {
            ZStack {
                Circle()
                    .strokeBorder(done ? WTheme.positive : tint.opacity(0.9), lineWidth: 2)
                Circle()
                    .fill(WTheme.positive)
                    .scaleEffect(done ? 1 : 0.01)
                    .opacity(done ? 1 : 0)
                TickShape()
                    .trim(from: 0, to: done ? 1 : 0)
                    .stroke(.white, style: StrokeStyle(lineWidth: 2.4, lineCap: .round, lineJoin: .round))
                    .frame(width: 11, height: 9)
                    .animation(.easeOut(duration: 0.25).delay(done ? 0.12 : 0), value: done)
                Circle()
                    .stroke(WTheme.positive, lineWidth: 2)
                    .scaleEffect(ripple ? 1.9 : 1)
                    .opacity(ripple ? 0 : (done ? 0.5 : 0))
            }
            .frame(width: 24, height: 24)
            .frame(width: 40, height: 40)
            .contentShape(Rectangle())
            .animation(.spring(response: 0.32, dampingFraction: 0.58), value: done)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(done ? "Reopen task" : "Complete task")
    }
}

/// The tick inside a check.
struct TickShape: Shape {
    func path(in rect: CGRect) -> Path {
        var path = Path()
        path.move(to: CGPoint(x: rect.minX, y: rect.midY + rect.height * 0.05))
        path.addLine(to: CGPoint(x: rect.minX + rect.width * 0.36, y: rect.maxY))
        path.addLine(to: CGPoint(x: rect.maxX, y: rect.minY))
        return path
    }
}

/// An icon and a title with a tight gap, for inline metadata.
struct CompactLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 4) {
            configuration.icon.imageScale(.small)
            configuration.title
        }
    }
}
