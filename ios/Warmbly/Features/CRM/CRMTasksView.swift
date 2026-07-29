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

    private var canWrite: Bool { env.session.can(.manageContacts) }
    private var isSearching: Bool { !store.query.trimmingCharacters(in: .whitespaces).isEmpty }
    private var scopedTasks: [CRMTask] { store.tasks(in: store.scope) }

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
        return VStack(spacing: 0) {
            CRMSearchBar(
                query: $store.query,
                prompt: "Search tasks",
                onMenu: { openSidebar() },
                menuLabel: "Open tasks menu"
            ) {
                if canWrite {
                    CRMCircleButton(symbol: "plus", label: "New task") {
                        showCreate = true
                    }
                }
                CRMCircleButton(symbol: "xmark", label: "Close tasks", weight: .semibold, size: 15) {
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
            if !isSearching, store.count(for: store.scope) > 0 {
                Text(WFormat.compact(store.count(for: store.scope)))
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
                    .contentTransition(.numericText())
            }
            Spacer()
            if isSearching, store.hasLoaded {
                Text("\(scopedTasks.count) found")
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

    // MARK: Due-date groups

    private struct TaskBucket: Identifiable {
        let id: String
        let title: String
        var tone: Tone?
        var ping = false
        let tasks: [CRMTask]
    }

    /// Buckets computed over the scoped tasks; scopes narrower than Open
    /// naturally collapse to their own group(s).
    private var groups: [TaskBucket] {
        var result: [TaskBucket] = []
        let scoped = scopedTasks
        let overdue = scoped.filter(\.isOverdue)
        if !overdue.isEmpty {
            result.append(TaskBucket(id: "overdue", title: "Overdue", tone: .rose, ping: true, tasks: overdue))
        }
        let open = scoped.filter { !$0.isDone && !$0.isOverdue }
        let calendar = Calendar.current
        let today = open.filter { $0.dueDate.map(calendar.isDateInToday) ?? false }
        let tomorrow = open.filter { $0.dueDate.map(calendar.isDateInTomorrow) ?? false }
        let later = open.filter { task in
            guard let due = task.dueDate else { return false }
            return !calendar.isDateInToday(due) && !calendar.isDateInTomorrow(due)
        }
        let undated = open.filter { $0.dueDate == nil }
        if !today.isEmpty {
            result.append(TaskBucket(id: "today", title: "Due today", tone: .sky, tasks: today))
        }
        if !tomorrow.isEmpty {
            result.append(TaskBucket(id: "tomorrow", title: "Due tomorrow", tasks: tomorrow))
        }
        if !later.isEmpty {
            result.append(TaskBucket(id: "later", title: "Upcoming", tasks: later))
        }
        if !undated.isEmpty {
            result.append(TaskBucket(id: "undated", title: "No due date", tasks: undated))
        }
        let done = scoped.filter(\.isDone)
        if !done.isEmpty {
            result.append(TaskBucket(id: "done", title: "Done", tone: .slate, tasks: done))
        }
        return result
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { SkeletonRows(rows: 10) }
        } else if let error = store.errorMessage, store.tasks.isEmpty {
            ErrorStateView(title: "Couldn't load tasks", message: error) {
                await store.load(env.api)
            }
        } else if scopedTasks.isEmpty {
            emptyState
        } else {
            List {
                ForEach(groups) { group in
                    CRMListCaption(title: group.title, count: group.tasks.count, tone: group.tone, ping: group.ping)
                    ForEach(group.tasks) { task in
                        row(task)
                    }
                }
                CRMEndMarker(scopedTasks.count, "task")
            }
            .listStyle(.plain)
            .scrollDismissesKeyboard(.immediately)
            .refreshable { await store.load(env.api) }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        if isSearching {
            EmptyStateView(title: "No matching tasks", message: "Try a different search.")
        } else {
            switch store.scope {
            case .open:
                if canWrite {
                    EmptyStateView(
                        title: "No tasks yet",
                        message: "Track follow-ups and reminders here.",
                        ctaTitle: "New task"
                    ) {
                        showCreate = true
                    }
                } else {
                    EmptyStateView(title: "No tasks yet", message: "Tasks your team creates will show up here.")
                }
            case .dueToday:
                EmptyStateView(title: "Nothing due today", message: "Tasks with a due date of today show up here.")
            case .overdue:
                EmptyStateView(title: "Nothing overdue", message: "You're all caught up.")
            case .upcoming:
                EmptyStateView(title: "Nothing upcoming", message: "Tasks due after today show up here.")
            case .completed:
                EmptyStateView(title: "Nothing completed yet", message: "Finished tasks land here.")
            }
        }
    }

    // MARK: Row

    private func row(_ task: CRMTask) -> some View {
        HStack(spacing: 12) {
            Button {
                Task { await toggle(task) }
            } label: {
                Image(systemName: task.isDone ? "checkmark.circle.fill" : "circle")
                    .font(.system(size: 22))
                    .foregroundStyle(task.isDone ? WTheme.positive : (task.isOverdue ? WTheme.negative : Color.secondary))
                    .contentTransition(.symbolEffect(.replace))
                    .frame(width: 30, height: 30)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(!canWrite)
            .accessibilityLabel(task.isDone ? "Reopen task" : "Complete task")

            VStack(alignment: .leading, spacing: 3) {
                Text(task.title)
                    .font(.body.weight(.medium))
                    .foregroundStyle(task.isDone ? .secondary : .primary)
                    .strikethrough(task.isDone, color: .secondary)
                    .lineLimit(1)
                HStack(spacing: 6) {
                    if let due = task.dueDate {
                        Text(CRMFormat.dueLabel(due))
                            .font(.footnote)
                            .monospacedDigit()
                            .foregroundStyle(task.isOverdue ? WTheme.negative : .secondary)
                    }
                    if let type = task.type, !type.isEmpty {
                        CRMColorChip(text: type, hex: typeColor(type))
                    }
                }
            }
            Spacer(minLength: 8)
            if let priority = task.priority, priority != "medium", !task.isDone {
                StatusPill(text: priority, tone: task.priorityTone)
            }
        }
        .padding(.vertical, 6)
        .listRowInsets(EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16))
        .listRowBackground(Color(.systemBackground))
        .swipeActions(edge: .leading, allowsFullSwipe: true) {
            if canWrite {
                Button {
                    Task { await toggle(task) }
                } label: {
                    Label(task.isDone ? "Reopen" : "Complete", systemImage: task.isDone ? "arrow.uturn.backward" : "checkmark")
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
        try? await store.setDone(env.api, task: task, done: !task.isDone)
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
