import SwiftUI

// MARK: - Board layout (shared with the tasks board)

/// Board lanes side by side: they share the width when each fits at `minWidth`, otherwise they scroll and snap.
struct CRMBoardColumns<Lane: Identifiable, Content: View>: View {
    let lanes: [Lane]
    var minWidth: CGFloat = 280
    var scrollingWidth: CGFloat = 304
    var spacing: CGFloat = 14
    var margin: CGFloat = 20
    @ViewBuilder let lane: (Lane) -> Content

    var body: some View {
        GeometryReader { geo in
            let count = CGFloat(max(lanes.count, 1))
            let available = geo.size.width - margin * 2
            let fits = count * minWidth + (count - 1) * spacing <= available
            let width = fits ? (available - (count - 1) * spacing) / count : scrollingWidth
            ScrollView(.horizontal) {
                HStack(alignment: .top, spacing: spacing) {
                    ForEach(lanes) { item in
                        lane(item)
                            .frame(width: width)
                            .frame(maxHeight: .infinity, alignment: .top)
                    }
                }
                .scrollTargetLayout()
            }
            .contentMargins(.horizontal, margin, for: .scrollContent)
            .scrollTargetBehavior(.viewAligned)
            .scrollIndicators(.hidden)
            .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
        }
    }
}

/// A board lane: a header over its own vertical scroll on a soft well, optionally a drop target for a dragged id.
struct CRMBoardLane<Header: View, Content: View>: View {
    var onDrop: ((String) -> Bool)?
    @ViewBuilder let header: () -> Header
    @ViewBuilder let content: () -> Content

    @State private var targeted = false

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 20, style: .continuous)
        VStack(spacing: 0) {
            header()
                .padding(.horizontal, 14)
                .padding(.top, 14)
                .padding(.bottom, 10)
            ScrollView {
                LazyVStack(spacing: 10) {
                    content()
                }
                .padding(.horizontal, 10)
                .padding(.top, 2)
                .padding(.bottom, 14)
            }
            .scrollIndicators(.hidden)
        }
        .frame(maxHeight: .infinity, alignment: .top)
        .background(Color.primary.opacity(targeted ? 0.06 : 0.022), in: shape)
        .overlay(shape.strokeBorder(WTheme.accent.opacity(targeted ? 0.55 : 0), lineWidth: 1.5))
        .animation(.snappy(duration: 0.2), value: targeted)
        .dropDestination(for: String.self) { items, _ in
            guard let onDrop, let id = items.first else { return false }
            return onDrop(id)
        } isTargeted: { value in
            targeted = onDrop != nil && value
        }
    }
}

/// A lane's heading: a coloured dot, the title, a count capsule and a trailing note.
struct CRMBoardLaneHeader: View {
    let title: String
    var color: Color?
    var pulsing = false
    let count: Int
    var trailing: String?

    var body: some View {
        HStack(spacing: 8) {
            if let color {
                if pulsing {
                    LiveDot(color: color, size: 7)
                } else {
                    Circle().fill(color).frame(width: 8, height: 8)
                }
            }
            Text(title)
                .font(.system(size: 14.5, weight: .semibold))
                .foregroundStyle(WTheme.ink)
                .lineLimit(1)
            Text("\(count)")
                .font(.system(size: 12, weight: .semibold))
                .monospacedDigit()
                .foregroundStyle(WTheme.inkSecondary)
                .padding(.horizontal, 7)
                .frame(minWidth: 22, minHeight: 20)
                .surfaceCard(in: Capsule())
                .contentTransition(.numericText())
            Spacer(minLength: 6)
            if let trailing {
                Text(trailing)
                    .font(.system(size: 13, weight: .medium))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// The quiet placeholder an empty lane shows.
struct CRMBoardEmptyLane: View {
    let text: String
    var symbol: String?

    var body: some View {
        VStack(spacing: 6) {
            if let symbol {
                Image(systemName: symbol)
                    .font(.system(size: 17, weight: .medium))
                    .foregroundStyle(WTheme.inkTertiary)
            }
            Text(text)
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(WTheme.inkTertiary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 26)
        .overlay {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .strokeBorder(WTheme.hairlineStrong, style: StrokeStyle(lineWidth: 1, dash: [4, 4]))
        }
    }
}

/// One lane's worth of cards laid out as a grid, for a board narrowed to a single group.
struct CRMBoardGrid<Header: View, Content: View>: View {
    @ViewBuilder let header: () -> Header
    @ViewBuilder let content: () -> Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                header()
                    .padding(.horizontal, 4)
                LazyVGrid(
                    columns: [GridItem(.adaptive(minimum: 280, maximum: 440), spacing: 14, alignment: .top)],
                    alignment: .leading,
                    spacing: 14
                ) {
                    content()
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 4)
            .padding(.bottom, 24)
        }
        .scrollDismissesKeyboard(.immediately)
    }
}

extension View {
    /// A card on a board lane, lifting under the pointer.
    func crmBoardCard() -> some View {
        let shape = RoundedRectangle(cornerRadius: 16, style: .continuous)
        return frame(maxWidth: .infinity, alignment: .leading)
            .background(WTheme.raised, in: shape)
            .overlay(shape.strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
            .shadow(color: .black.opacity(0.04), radius: 6, y: 2)
            .contentShape(shape)
            .contentShape(.contextMenuPreview, shape)
            .contentShape(.dragPreview, shape)
            .contentShape(.hoverEffect, shape)
            .hoverEffect(.lift)
    }

    @ViewBuilder
    func crmDraggable(_ id: String, enabled: Bool) -> some View {
        if enabled {
            draggable(id)
        } else {
            self
        }
    }
}

// MARK: - Pipeline values

extension CRMDealsStore {
    /// Open value per stage that holds open deals; terminal stages hold closed deals and would read as $0.
    var openStageValues: [(CRMStage, Double)] {
        orderedStages.compactMap { stage in
            let open = deals(inStage: stage.id)
            guard !open.isEmpty else { return nil }
            return (stage, open.compactMap(\.value).reduce(0, +))
        }
    }
}

/// One segment per stage, sized by its share of the open value.
struct CRMDealsStageBar: View {
    let values: [(CRMStage, Double)]
    let shown: Bool

    var body: some View {
        GeometryReader { proxy in
            let total = max(1, values.reduce(0) { $0 + $1.1 })
            let usable: CGFloat = proxy.size.width - CGFloat(values.count) * 3
            HStack(spacing: 3) {
                ForEach(Array(values.enumerated()), id: \.offset) { index, item in
                    if item.1 > 0 {
                        let share = CGFloat(item.1 / total)
                        let barWidth: CGFloat = shown ? max(8, usable * share) : 0
                        let delay: Double = 0.2 + Double(index) * 0.08
                        RoundedRectangle(cornerRadius: 4, style: .continuous)
                            .fill(CRMHex.color(item.0.color) ?? WTheme.paused)
                            .frame(width: barWidth)
                            .animation(.spring(response: 0.85, dampingFraction: 0.8).delay(delay), value: shown)
                    }
                }
                Spacer(minLength: 0)
            }
        }
        .frame(height: 10)
    }
}

// MARK: - Deals board

/// Where a dragged deal lands: a stage reopens it there, a status closes it.
enum CRMDealsBoardTarget: Hashable {
    case stage(String)
    case status(String)
}

/// The deals page on a wide pane: a pipeline strip over a kanban with one lane per stage, then Won and Lost.
struct CRMDealsBoard<Empty: View>: View {
    let store: CRMDealsStore
    let scope: CRMDealScope
    let isSearching: Bool
    let canWrite: Bool
    let shown: Bool
    let onSelectScope: (CRMDealScope) -> Void
    let onEdit: (CRMDeal) -> Void
    let onSetStatus: (CRMDeal, String) -> Void
    let onMoveStage: (CRMDeal, String) -> Void
    let onDrop: (String, CRMDealsBoardTarget) -> Void
    @ViewBuilder let empty: () -> Empty

    private struct Lane: Identifiable {
        let id: String
        let title: String
        let color: Color
        let deals: [CRMDeal]
        let target: CRMDealsBoardTarget
        let emptyText: String
    }

    private var currency: String? { store.displayCurrency }

    /// Search, a Won or Lost scope, or a pipeline without stages leaves one group, shown as a grid.
    private var isFlat: Bool {
        isSearching || scope == .won || scope == .lost || store.orderedStages.isEmpty
    }

    private var lanes: [Lane] {
        var result = store.orderedStages.map { stage in
            Lane(
                id: stage.id,
                title: stage.name,
                color: CRMHex.color(stage.color) ?? WTheme.paused,
                deals: store.deals(inStage: stage.id),
                target: .stage(stage.id),
                emptyText: canWrite ? "Drop a deal here" : "No deals"
            )
        }
        if scope == .all {
            result.append(Lane(id: "status-won", title: "Won", color: WTheme.positive, deals: store.deals.filter { $0.status == "won" }, target: .status("won"), emptyText: "No wins yet"))
            result.append(Lane(id: "status-lost", title: "Lost", color: WTheme.paused, deals: store.deals.filter { $0.status == "lost" }, target: .status("lost"), emptyText: "Nothing lost"))
        }
        return result
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if !isSearching {
                summaryStrip
                    .padding(.horizontal, 20)
            }
            filterRow
                .padding(.horizontal, 20)
            if store.deals.isEmpty {
                ScrollView { empty() }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if isFlat {
                CRMBoardGrid {
                    CRMBoardLaneHeader(
                        title: isSearching ? "Results" : scope.title,
                        color: isSearching ? nil : scope == .won ? WTheme.positive : scope == .lost ? WTheme.paused : nil,
                        count: store.deals.count,
                        trailing: total(store.deals)
                    )
                } content: {
                    ForEach(store.deals) { deal in
                        card(deal, showsStage: true)
                    }
                }
            } else {
                CRMBoardColumns(lanes: lanes) { lane in
                    CRMBoardLane(onDrop: canWrite ? { id in onDrop(id, lane.target); return true } : nil) {
                        CRMBoardLaneHeader(title: lane.title, color: lane.color, count: lane.deals.count, trailing: total(lane.deals))
                    } content: {
                        if lane.deals.isEmpty {
                            CRMBoardEmptyLane(text: lane.emptyText)
                        } else {
                            ForEach(lane.deals) { deal in
                                card(deal, showsStage: false)
                            }
                        }
                    }
                }
                .padding(.bottom, 12)
            }
        }
        .padding(.top, 4)
    }

    private func total(_ deals: [CRMDeal]) -> String? {
        let values = deals.compactMap(\.value)
        guard !values.isEmpty else { return nil }
        return CRMFormat.compactCurrency(values.reduce(0, +), code: currency)
    }

    // MARK: Summary strip

    private var summaryStrip: some View {
        let summary = store.summary
        let openValue = summary?.openValue ?? 0
        let openCount = summary?.openCount ?? 0
        let won = summary?.wonCount ?? 0
        let lost = summary?.lostCount ?? 0
        let values = store.openStageValues
        return HStack(alignment: .center, spacing: 0) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Open pipeline")
                    .font(.system(size: 13, weight: .medium))
                    .foregroundStyle(WTheme.inkSecondary)
                CountUpText(value: shown ? openValue : 0, format: { CRMFormat.compactCurrency($0.rounded(), code: currency) })
                    .font(.system(size: 30, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.ink)
                    .animation(.easeOut(duration: 1.1), value: shown)
                    .animation(.snappy, value: openValue)
                Text("\(openCount) open \(openCount == 1 ? "deal" : "deals")  ·  \(CRMFormat.compactCurrency(summary?.wonValue ?? 0, code: currency)) won")
                    .font(.system(size: 12.5))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkTertiary)
                    .lineLimit(1)
            }
            .fixedSize()
            stripDivider
            HStack(spacing: 12) {
                let rate = won + lost > 0 ? Double(won) / Double(won + lost) : 0
                SweepRing(fraction: rate, color: WTheme.positive, shown: shown, lineWidth: 6) {
                    Text(won + lost > 0 ? "\(Int((rate * 100).rounded()))%" : "–")
                        .font(.system(size: 13, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.ink)
                }
                .frame(width: 54, height: 54)
                VStack(alignment: .leading, spacing: 2) {
                    Text("Win rate")
                        .font(.system(size: 13, weight: .medium))
                        .foregroundStyle(WTheme.inkSecondary)
                    Text(won + lost > 0 ? "\(won) won  ·  \(lost) lost" : "No closed deals yet")
                        .font(.system(size: 12.5))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.inkTertiary)
                        .lineLimit(1)
                }
            }
            .fixedSize()
            stripDivider
            VStack(alignment: .leading, spacing: 10) {
                if values.isEmpty {
                    Text("No open deals in this pipeline")
                        .font(.system(size: 13))
                        .foregroundStyle(WTheme.inkTertiary)
                } else {
                    CRMDealsStageBar(values: values, shown: shown)
                    ScrollView(.horizontal) {
                        HStack(spacing: 14) {
                            ForEach(Array(values.enumerated()), id: \.offset) { _, item in
                                HStack(spacing: 6) {
                                    Circle().fill(CRMHex.color(item.0.color) ?? WTheme.paused).frame(width: 7, height: 7)
                                    Text(item.0.name)
                                        .font(.system(size: 12.5))
                                        .foregroundStyle(WTheme.inkSecondary)
                                    Text(CRMFormat.compactCurrency(item.1, code: currency))
                                        .font(.system(size: 12.5, weight: .semibold))
                                        .monospacedDigit()
                                        .foregroundStyle(WTheme.ink)
                                }
                                .lineLimit(1)
                            }
                        }
                    }
                    .scrollIndicators(.hidden)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .coverCard(padding: 18)
    }

    private var stripDivider: some View {
        Rectangle()
            .fill(WTheme.hairlineStrong)
            .frame(width: 1, height: 52)
            .padding(.horizontal, 22)
    }

    private var filterRow: some View {
        HStack(spacing: 12) {
            PillSwitcher(
                options: CRMDealScope.allCases,
                selection: Binding(get: { scope }, set: { onSelectScope($0) }),
                label: \.shortTitle
            )
            .frame(maxWidth: 380)
            Spacer(minLength: 8)
            if canWrite, !isFlat {
                Label("Drag cards between stages", systemImage: "hand.draw")
                    .font(.system(size: 12.5, weight: .medium))
                    .foregroundStyle(WTheme.inkTertiary)
                    .labelStyle(CompactLabelStyle())
            }
        }
    }

    // MARK: Card

    private func card(_ deal: CRMDeal, showsStage: Bool) -> some View {
        let stage = deal.stage ?? store.stage(deal.stageID)
        let status = deal.status ?? "open"
        let person = deal.contact?.displayName
        let when: String? = if status == "open", let close = deal.expectedCloseDate {
            "Closes \(close.formatted(.dateTime.month(.abbreviated).day()))"
        } else if status == "won", let wonAt = deal.wonAt {
            "Won \(WFormat.ago(wonAt)) ago"
        } else {
            nil
        }
        return VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(deal.name)
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(status == "lost" ? WTheme.inkSecondary : WTheme.ink)
                    .strikethrough(status == "lost", color: WTheme.inkTertiary)
                    .lineLimit(2)
                    .multilineTextAlignment(.leading)
                Spacer(minLength: 4)
                if status != "open" {
                    Image(systemName: status == "won" ? "checkmark" : "xmark")
                        .font(.system(size: 8.5, weight: .heavy))
                        .foregroundStyle(.white)
                        .frame(width: 18, height: 18)
                        .background(status == "won" ? WTheme.positive : WTheme.paused, in: Circle())
                        .accessibilityLabel(status == "won" ? "Won" : "Lost")
                }
            }
            if deal.value != nil || (showsStage && status == "open" && stage != nil) {
                HStack(spacing: 8) {
                    if let value = deal.value {
                        Text(CRMFormat.currency(value, code: deal.currency))
                            .font(.system(size: 18, weight: .semibold, design: .rounded))
                            .monospacedDigit()
                            .foregroundStyle(status == "won" ? WTheme.positive : status == "lost" ? WTheme.inkTertiary : WTheme.ink)
                            .contentTransition(.numericText())
                    }
                    Spacer(minLength: 4)
                    if showsStage, status == "open", let stage {
                        CRMColorChip(text: stage.name, hex: stage.color)
                    }
                }
            }
            if person != nil || when != nil {
                HStack(spacing: 8) {
                    if let person {
                        WAvatar(name: person, seed: deal.contactID ?? deal.id, size: 22)
                        Text(person)
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkSecondary)
                            .lineLimit(1)
                    }
                    Spacer(minLength: 4)
                    if let when {
                        Text(when)
                            .font(.system(size: 12.5))
                            .monospacedDigit()
                            .foregroundStyle(WTheme.inkTertiary)
                            .lineLimit(1)
                    }
                }
            }
        }
        .padding(14)
        .crmBoardCard()
        .onTapGesture {
            if canWrite { onEdit(deal) }
        }
        .animation(.snappy, value: status)
        .crmDraggable(deal.id, enabled: canWrite)
        .contextMenu {
            if canWrite {
                Button {
                    onEdit(deal)
                } label: {
                    Label("Edit deal", systemImage: "slider.horizontal.3")
                }
                stageMoveMenu(deal)
                if status == "open" {
                    Button {
                        onSetStatus(deal, "won")
                    } label: {
                        Label("Mark won", systemImage: "trophy")
                    }
                    Button(role: .destructive) {
                        onSetStatus(deal, "lost")
                    } label: {
                        Label("Mark lost", systemImage: "xmark.seal")
                    }
                } else {
                    Button {
                        onSetStatus(deal, "open")
                    } label: {
                        Label("Reopen", systemImage: "arrow.uturn.backward")
                    }
                }
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(canWrite ? .isButton : [])
    }

    @ViewBuilder
    private func stageMoveMenu(_ deal: CRMDeal) -> some View {
        let stages = store.selectedPipeline?.orderedStages ?? []
        if !stages.isEmpty {
            Menu("Move to stage") {
                ForEach(stages) { stage in
                    Button {
                        onMoveStage(deal, stage.id)
                    } label: {
                        if deal.stageID == stage.id {
                            Label(stage.name, systemImage: "checkmark")
                        } else {
                            Text(stage.name)
                        }
                    }
                    .disabled(deal.stageID == stage.id)
                }
            }
        }
    }
}
