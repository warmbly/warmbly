import SwiftUI

// MARK: - Due-date buckets

enum CRMTasksBucketKind: String {
    case overdue, today, tomorrow, later, undated, done

    var emptyText: String {
        switch self {
        case .overdue: "Nothing overdue"
        case .today: "All clear for today"
        case .tomorrow: "Nothing due tomorrow"
        case .later: "Nothing coming up"
        case .undated: "Every task has a date"
        case .done: "Nothing finished yet"
        }
    }

    var emptySymbol: String {
        switch self {
        case .overdue, .today: "checkmark.circle"
        case .tomorrow, .later: "calendar"
        case .undated: "clock"
        case .done: "checkmark.seal"
        }
    }
}

/// One due-date group of tasks, a section on the phone and a lane on the board.
struct CRMTasksBucket: Identifiable {
    let kind: CRMTasksBucketKind
    let title: String
    var color: Color?
    var ping = false
    let tasks: [CRMTask]

    var id: String { kind.rawValue }
}

extension CRMTaskScope {
    /// The lanes a scope keeps on the board even when empty, so ticking off a lane's last task does not reflow the board.
    var boardKinds: [CRMTasksBucketKind] {
        switch self {
        case .open: [.overdue, .today, .tomorrow, .later, .undated]
        case .dueToday: [.today]
        case .overdue: [.overdue]
        case .upcoming: [.tomorrow, .later]
        case .completed: [.done]
        }
    }
}

// MARK: - Tasks board

/// The tasks page on a wide pane: a compact today strip over one lane per due-date group.
struct CRMTasksBoard<Empty: View>: View {
    let store: CRMTasksStore
    let buckets: [CRMTasksBucket]
    let isEmpty: Bool
    let isSearching: Bool
    let canWrite: Bool
    let shown: Bool
    let celebrate: Int
    let onToggle: (CRMTask) -> Void
    @ViewBuilder let empty: () -> Empty

    private var lanes: [CRMTasksBucket] {
        let kept = isSearching ? [] : store.scope.boardKinds
        return buckets.filter { kept.contains($0.kind) || !$0.tasks.isEmpty }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if !isSearching {
                todayStrip
                    .padding(.horizontal, 20)
            }
            ChipFilterRow(
                options: CRMTaskScope.allCases,
                selection: Binding(get: { store.scope }, set: { store.scope = $0 }),
                label: \.title,
                count: { store.count(for: $0) },
                alert: { $0 == .overdue && store.count(for: .overdue) > 0 }
            )
            .padding(.horizontal, 20)
            let lanes = lanes
            if isEmpty || lanes.isEmpty {
                ScrollView { empty() }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if lanes.count == 1, let only = lanes.first {
                CRMBoardGrid {
                    header(only)
                } content: {
                    ForEach(only.tasks) { task in
                        card(task)
                    }
                }
            } else {
                CRMBoardColumns(lanes: lanes, minWidth: 240, scrollingWidth: 284) { lane in
                    CRMBoardLane {
                        header(lane)
                    } content: {
                        if lane.tasks.isEmpty {
                            CRMBoardEmptyLane(text: lane.kind.emptyText, symbol: lane.kind.emptySymbol)
                        } else {
                            ForEach(lane.tasks) { task in
                                card(task)
                            }
                        }
                    }
                }
                .padding(.bottom, 12)
            }
        }
        .padding(.top, 4)
    }

    private func header(_ bucket: CRMTasksBucket) -> some View {
        CRMBoardLaneHeader(
            title: bucket.title,
            color: bucket.color ?? WTheme.inkTertiary,
            pulsing: bucket.ping,
            count: bucket.tasks.count
        )
    }

    // MARK: Today strip

    private var todayStrip: some View {
        let left = store.tasks(in: .dueToday).count
        let done = store.doneToday
        let total = left + done
        let fraction = total > 0 ? Double(done) / Double(total) : 1
        let overdue = store.count(for: .overdue)
        let upcoming = store.count(for: .upcoming)
        let headline = left > 0 ? "\(left) left for today" : (total > 0 ? "All done for today" : "Nothing due today")
        return HStack(spacing: 16) {
            SweepRing(fraction: fraction, color: left == 0 ? WTheme.positive : WTheme.accent, shown: shown, lineWidth: 7) {
                if left == 0 {
                    Image(systemName: "checkmark")
                        .font(.system(size: 20, weight: .bold))
                        .foregroundStyle(WTheme.positive)
                        .symbolEffect(.bounce, value: celebrate)
                        .transition(.scale.combined(with: .opacity))
                } else {
                    Text("\(done)/\(total)")
                        .font(.system(size: 15, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.ink)
                        .contentTransition(.numericText())
                }
            }
            .frame(width: 62, height: 62)
            .animation(.spring(response: 0.5, dampingFraction: 0.7), value: left == 0)
            VStack(alignment: .leading, spacing: 3) {
                Text(Date.now.formatted(.dateTime.weekday(.wide).month(.wide).day()))
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(WTheme.inkSecondary)
                Text(headline)
                    .font(.system(size: 20, weight: .semibold))
                    .foregroundStyle(WTheme.ink)
                    .contentTransition(.numericText())
                    .lineLimit(1)
            }
            .layoutPriority(1)
            Spacer(minLength: 12)
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 10) {
                    stat(overdue, "Overdue", overdue > 0 ? WTheme.negative : WTheme.ink)
                    stat(left, "Due today", WTheme.accent)
                    stat(upcoming, "Coming up", WTheme.special)
                    stat(done, "Done today", WTheme.positive)
                }
                HStack(spacing: 10) {
                    stat(overdue, "Overdue", overdue > 0 ? WTheme.negative : WTheme.ink)
                    stat(upcoming, "Coming up", WTheme.special)
                }
                EmptyView()
            }
        }
        .coverCard(padding: 16)
        .animation(.snappy, value: left)
    }

    private func stat(_ value: Int, _ label: String, _ color: Color) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text("\(value)")
                .font(.system(size: 19, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(value > 0 ? color : WTheme.inkTertiary)
                .contentTransition(.numericText())
            Text(label)
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(WTheme.inkSecondary)
                .lineLimit(1)
        }
        .fixedSize()
        .padding(.horizontal, 14)
        .padding(.vertical, 9)
        .frame(minWidth: 92, alignment: .leading)
        .background(WTheme.fill, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .accessibilityElement(children: .combine)
    }

    // MARK: Card

    private func card(_ task: CRMTask) -> some View {
        let priorityColor: Color? = switch task.priority {
        case "urgent": WTheme.negative
        case "high": Tone.orange.color
        default: nil
        }
        let calendar = Calendar.current
        let dueColor: Color = task.isOverdue ? WTheme.negative : (task.dueDate.map(calendar.isDateInToday) ?? false) ? WTheme.accent : WTheme.inkSecondary
        let showsDue = task.dueDate != nil && !task.isDone
        let type = task.type.flatMap { $0.isEmpty ? nil : $0 }
        return HStack(alignment: .top, spacing: 4) {
            TaskCheck(done: task.isDone) {
                onToggle(task)
            }
            .disabled(!canWrite)
            VStack(alignment: .leading, spacing: 6) {
                Text(task.title)
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(task.isDone ? WTheme.inkTertiary : WTheme.ink)
                    .strikethrough(task.isDone, color: WTheme.inkTertiary)
                    .lineLimit(3)
                    .multilineTextAlignment(.leading)
                if showsDue || type != nil {
                    HStack(spacing: 8) {
                        if let due = task.dueDate, !task.isDone {
                            let label = CRMFormat.dueLabel(due)
                            Label(label.prefix(1).uppercased() + label.dropFirst(), systemImage: task.isOverdue ? "exclamationmark.circle" : "clock")
                                .labelStyle(CompactLabelStyle())
                                .font(.system(size: 12.5))
                                .monospacedDigit()
                                .foregroundStyle(dueColor)
                                .lineLimit(1)
                        }
                        if let type {
                            CRMColorChip(text: type, hex: store.types.first { $0.name == type }?.color)
                        }
                    }
                }
            }
            .padding(.top, 10)
            Spacer(minLength: 4)
            if let priorityColor, !task.isDone {
                Image(systemName: "flag.fill")
                    .font(.system(size: 12))
                    .foregroundStyle(priorityColor)
                    .padding(.top, 13)
                    .accessibilityLabel("\(task.priority ?? "") priority")
            }
        }
        .padding(.leading, 2)
        .padding(.trailing, 14)
        .padding(.bottom, 10)
        .animation(.spring(response: 0.4, dampingFraction: 0.8), value: task.isDone)
        .crmBoardCard()
        .contextMenu {
            if canWrite {
                Button {
                    onToggle(task)
                } label: {
                    Label(task.isDone ? "Reopen" : "Mark done", systemImage: task.isDone ? "arrow.uturn.backward" : "checkmark.circle")
                }
            }
        }
    }
}
