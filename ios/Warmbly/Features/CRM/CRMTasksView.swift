import SwiftUI

/// Tasks checklist pushed from the More tab. Overdue / open / done sections,
/// tap the circle to complete or reopen (gated behind manageContacts), create
/// sheet with title, due date and type. Reloads on the .crm pulse.
struct CRMTasksView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = CRMTasksStore()
    @State private var showCreate = false

    private var canWrite: Bool { env.session.can(.manageContacts) }

    var body: some View {
        @Bindable var store = store
        VStack(spacing: 0) {
            statStrip
            Divider()
            list
        }
        .navigationTitle("Tasks")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItemGroup(placement: .topBarTrailing) {
                PresenceAvatars()
                if canWrite {
                    Button {
                        showCreate = true
                    } label: {
                        Image(systemName: "plus")
                    }
                    .accessibilityLabel("New task")
                }
            }
        }
        .searchable(text: $store.query, prompt: "Search tasks")
        .sheet(isPresented: $showCreate) {
            CRMTaskCreateSheet(store: store)
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
    }

    // MARK: Stat strip

    private var statStrip: some View {
        HStack(spacing: 0) {
            StatCell(label: "Open", value: WFormat.compact(store.summary?.openCount ?? store.open.count))
                .padding(.horizontal, 8)
            hairline
            StatCell(
                label: "Overdue",
                value: WFormat.compact(store.summary?.overdueCount ?? store.overdue.count),
                tone: store.overdue.isEmpty ? nil : .rose
            )
            .padding(.horizontal, 8)
            hairline
            StatCell(label: "Done", value: WFormat.compact(store.summary?.completedCount ?? store.done.count), tone: .slate)
                .padding(.horizontal, 8)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
    }

    private var hairline: some View {
        Rectangle().fill(Color(.separator)).frame(width: 0.5, height: 34)
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
        } else if store.tasks.isEmpty {
            emptyState
        } else {
            List {
                if !store.overdue.isEmpty {
                    Section {
                        ForEach(store.overdue) { task in row(task) }
                    } header: {
                        CRMSectionHeader(title: "Overdue", count: store.overdue.count, tone: .rose, ping: true)
                    }
                }
                if !store.open.isEmpty {
                    Section {
                        ForEach(store.open) { task in row(task) }
                    } header: {
                        CRMSectionHeader(title: "Open", count: store.open.count)
                    }
                }
                if !store.done.isEmpty {
                    Section {
                        ForEach(store.done) { task in row(task) }
                    } header: {
                        CRMSectionHeader(title: "Done", count: store.done.count, tone: .slate)
                    }
                }
            }
            .listStyle(.plain)
            .refreshable { await store.load(env.api) }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        let searching = !store.query.trimmingCharacters(in: .whitespaces).isEmpty
        if searching {
            EmptyStateView(title: "No matching tasks", message: "Try a different search.")
        } else if canWrite {
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
            }
            .buttonStyle(.plain)
            .disabled(!canWrite)

            VStack(alignment: .leading, spacing: 3) {
                Text(task.title)
                    .font(.body.weight(.medium))
                    .foregroundStyle(task.isDone ? .secondary : .primary)
                    .strikethrough(task.isDone, color: .secondary)
                    .lineLimit(1)
                HStack(spacing: 6) {
                    if task.isOverdue { CRMPingDot(color: WTheme.negative) }
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
