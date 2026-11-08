import SwiftUI

/// Deals browser presented as a full-screen cover from the More hub, copying
/// the campaign leads browser: its own NavigationStack, a slide-in drawer (sky
/// hero + status scopes + the member's pipelines with a sliding capsule and
/// edge-swipe), a search pill with hamburger + presence + circular close, a
/// tracked-uppercase scope caption, and deals grouped by stage under eyebrow
/// captions in a dense hairline list. Stage-move + edit stay gated behind
/// manageContacts. Reloads on the .crm realtime pulse.

/// Drawer scopes mapping onto the store's statusFilter.
private enum CRMDealScope: String, CaseIterable, Identifiable {
    case all, open, won, lost

    var id: String { rawValue }

    /// The store's statusFilter value for this scope.
    var status: String? { self == .all ? nil : rawValue }

    init(status: String?) {
        self = status.flatMap(CRMDealScope.init(rawValue:)) ?? .all
    }

    var title: String {
        switch self {
        case .all: "All deals"
        case .open: "Open"
        case .won: "Won"
        case .lost: "Lost"
        }
    }

    var shortTitle: String { self == .all ? "All" : title }

    var icon: String {
        switch self {
        case .all: "tray.full.fill"
        case .open: "circle.lefthalf.filled"
        case .won: "checkmark.seal.fill"
        case .lost: "xmark.seal.fill"
        }
    }
}

struct CRMDealsView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    @State private var store = CRMDealsStore()
    @State private var editing: CRMDeal?
    @State private var sidebarOpen = false
    @State private var shown = false
    @State private var celebrate = 0
    @FocusState private var searchFocused: Bool

    private var canWrite: Bool { env.session.can(.manageContacts) }
    private var isSearching: Bool { !store.query.trimmingCharacters(in: .whitespaces).isEmpty }
    private var scope: CRMDealScope { CRMDealScope(status: store.statusFilter) }

    var body: some View {
        NavigationStack {
            CRMBrowserShell(sidebarOpen: $sidebarOpen) {
                mainPane
            } drawer: { topInset in
                CRMDealsSidebar(
                    store: store,
                    topInset: topInset,
                    revealed: sidebarOpen,
                    onSelectStatus: { selectStatus($0) },
                    onSelectPipeline: { selectPipeline($0) }
                )
            }
            .toolbar(.hidden, for: .navigationBar)
            .sheet(item: $editing) { deal in
                CRMDealEditSheet(store: store, deal: deal)
            }
        }
        .task(id: store.query) {
            if store.hasLoaded {
                try? await Task.sleep(for: .milliseconds(350))
            }
            guard !Task.isCancelled else { return }
            await store.load(env.api)
        }
        .onChange(of: store.statusFilter) {
            Task { await store.load(env.api) }
        }
        .onChange(of: store.pipelineID) {
            Task { await store.load(env.api) }
        }
        .onChange(of: env.realtime.pulse(for: .crm)) {
            Task { await store.load(env.api) }
        }
        .sensoryFeedback(.selection, trigger: store.statusFilter)
        .sensoryFeedback(.selection, trigger: store.pipelineID)
    }

    // MARK: Drawer plumbing

    private func openSidebar() {
        withAnimation(CRMBrowser.spring) { sidebarOpen = true }
    }

    private func selectStatus(_ status: String?, close: Bool = true) {
        withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.statusFilter = status }
        if close { closeSidebarSoon() }
    }

    private func selectPipeline(_ id: String) {
        withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.pipelineID = id }
        closeSidebarSoon()
    }

    private func closeSidebarSoon() {
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
                title: "Deals",
                subtitle: Text(store.selectedPipeline?.name ?? "Pipeline"),
                query: $store.query,
                searchPrompt: "Search deals",
                searchFocused: $searchFocused,
                menuLabel: "Open deals menu",
                onMenu: openSidebar
            ) {
                GlassCircleButton(symbol: "xmark", label: "Close deals") { dismiss() }
            }
            list
        }
        .background(WTheme.groupedPage)
        .overlay { ConfettiBurst(trigger: celebrate) }
        .sensoryFeedback(.success, trigger: celebrate)
    }

    // MARK: Pipeline card

    private var currency: String? { store.displayCurrency }

    private var pipelineCard: some View {
        let summary = store.summary
        let openValue = summary?.openValue ?? 0
        let openCount = summary?.openCount ?? 0
        let won = summary?.wonCount ?? 0
        let lost = summary?.lostCount ?? 0
        let stages = store.orderedStages
        // Open deals only; terminal stages hold closed deals and read as $0 here.
        let values: [(CRMStage, Double)] = stages.compactMap { stage in
            let open = store.deals(inStage: stage.id)
            guard !open.isEmpty else { return nil }
            return (stage, open.compactMap(\.value).reduce(0, +))
        }
        let total = max(1, values.reduce(0) { $0 + $1.1 })
        return VStack(alignment: .leading, spacing: 16) {
            HStack(alignment: .center, spacing: 14) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Open pipeline")
                        .font(.system(size: 14, weight: .medium))
                        .foregroundStyle(WTheme.inkSecondary)
                    CountUpText(value: shown ? openValue : 0, format: { CRMFormat.compactCurrency($0.rounded(), code: currency) })
                        .font(.system(size: 38, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.ink)
                        .animation(.easeOut(duration: 1.1), value: shown)
                        .animation(.snappy, value: openValue)
                    Text("\(openCount) open \(openCount == 1 ? "deal" : "deals")  ·  \(CRMFormat.compactCurrency(summary?.wonValue ?? 0, code: currency)) won")
                        .font(.system(size: 13.5))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.inkTertiary)
                }
                Spacer(minLength: 8)
                if won + lost > 0 {
                    let rate = Double(won) / Double(won + lost)
                    SweepRing(fraction: rate, color: WTheme.positive, shown: shown, lineWidth: 8) {
                        VStack(spacing: -1) {
                            Text("\(Int((rate * 100).rounded()))%")
                                .font(.system(size: 18, weight: .semibold, design: .rounded))
                                .monospacedDigit()
                                .foregroundStyle(WTheme.ink)
                            Text("win rate")
                                .font(.system(size: 10.5, weight: .medium))
                                .foregroundStyle(WTheme.inkTertiary)
                        }
                    }
                    .frame(width: 84, height: 84)
                }
            }
            if !values.isEmpty {
                GeometryReader { proxy in
                    stageBar(values, total: total, width: proxy.size.width)
                }
                .frame(height: 10)
                VStack(spacing: 9) {
                    ForEach(Array(values.enumerated()), id: \.offset) { _, item in
                        let count = store.deals(inStage: item.0.id).count
                        HStack(spacing: 8) {
                            Circle().fill(CRMHex.color(item.0.color) ?? WTheme.paused).frame(width: 8, height: 8)
                            Text(item.0.name)
                                .font(.system(size: 14))
                                .foregroundStyle(WTheme.inkSecondary)
                                .lineLimit(1)
                            Text("\(count)")
                                .font(.system(size: 13))
                                .monospacedDigit()
                                .foregroundStyle(WTheme.inkTertiary)
                            Spacer(minLength: 4)
                            Text(CRMFormat.compactCurrency(item.1, code: currency))
                                .font(.system(size: 14, weight: .semibold))
                                .monospacedDigit()
                                .foregroundStyle(WTheme.ink)
                        }
                    }
                }
            }
        }
        .coverCard()
    }

    /// One segment per stage, sized by its share of the open value.
    private func stageBar(_ values: [(CRMStage, Double)], total: Double, width: CGFloat) -> some View {
        let usable: CGFloat = width - CGFloat(values.count) * 3
        return HStack(spacing: 3) {
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

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { AnalyticsLoadingCards().padding(16) }
        } else if let error = store.errorMessage, store.deals.isEmpty {
            ErrorStateView(title: "Couldn't load deals", message: error) {
                await store.load(env.api)
            }
        } else {
            List {
                Section {
                    VStack(spacing: 14) {
                        if !isSearching {
                            pipelineCard
                        }
                        PillSwitcher(
                            options: CRMDealScope.allCases,
                            selection: Binding(get: { scope }, set: { selectStatus($0.status, close: false) }),
                            label: \.shortTitle
                        )
                    }
                    .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 0, trailing: 0))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
                }
                if store.deals.isEmpty {
                    Section {
                        emptyState
                            .listRowBackground(Color.clear)
                            .listRowSeparator(.hidden)
                    }
                } else if store.statusFilter == nil, !isSearching, !store.orderedStages.isEmpty {
                    groupedByStage
                } else {
                    Section {
                        ForEach(store.deals) { deal in
                            row(deal)
                        }
                    } header: {
                        CoverSectionHeader(title: isSearching ? "Results" : scope.title, count: store.deals.count)
                    }
                }
                Section {
                    CRMEndMarker(store.totalCount ?? store.deals.count, "deal")
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
    private var groupedByStage: some View {
        ForEach(store.orderedStages) { stage in
            let stageDeals = store.deals(inStage: stage.id)
            if !stageDeals.isEmpty {
                Section {
                    ForEach(stageDeals) { deal in
                        row(deal, showsStage: false)
                    }
                } header: {
                    CoverSectionHeader(
                        title: stage.name,
                        count: stageDeals.count,
                        color: CRMHex.color(stage.color) ?? WTheme.paused,
                        trailing: CRMFormat.compactCurrency(stageDeals.compactMap(\.value).reduce(0, +), code: currency)
                    )
                }
            }
        }
        let closed = store.closedDeals
        if !closed.isEmpty {
            Section {
                ForEach(closed) { deal in
                    row(deal)
                }
            } header: {
                CoverSectionHeader(title: "Closed", count: closed.count, color: WTheme.paused)
            }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        if isSearching {
            CoverEmptyState(symbol: "magnifyingglass", color: WTheme.inkSecondary, title: "No matching deals", message: "Try a different search.")
        } else {
            switch scope {
            case .all:
                CoverEmptyState(symbol: "briefcase.fill", color: Tone.orange.color, title: "No deals yet", message: "Deals created from replies, campaigns or the web dashboard show up here.")
            case .open:
                CoverEmptyState(symbol: "circle.lefthalf.filled", color: WTheme.accent, title: "No open deals", message: "Deals still in play show up here.")
            case .won:
                CoverEmptyState(symbol: "trophy.fill", color: WTheme.positive, title: "No wins yet", message: "Deals you mark won land here. The first one is worth celebrating.")
            case .lost:
                CoverEmptyState(symbol: "xmark.seal.fill", color: WTheme.inkSecondary, title: "Nothing lost", message: "Deals marked lost show up here.")
            }
        }
    }

    // MARK: Row

    private func row(_ deal: CRMDeal, showsStage: Bool = true) -> some View {
        let stage = deal.stage ?? store.stage(deal.stageID)
        let status = deal.status ?? "open"
        let person = deal.contact?.displayName
        var details: [String] = []
        if let person { details.append(person) }
        if status == "open", let close = deal.expectedCloseDate {
            details.append("closes \(close.formatted(.dateTime.month(.abbreviated).day()))")
        } else if status == "won", let wonAt = deal.wonAt {
            details.append("won \(WFormat.ago(wonAt)) ago")
        }
        return HStack(spacing: 12) {
            WAvatar(name: person ?? deal.name, seed: deal.contactID ?? deal.id, size: 40)
                .overlay(alignment: .bottomTrailing) {
                    if status != "open" {
                        Image(systemName: status == "won" ? "checkmark" : "xmark")
                            .font(.system(size: 8, weight: .heavy))
                            .foregroundStyle(.white)
                            .frame(width: 17, height: 17)
                            .background(status == "won" ? WTheme.positive : WTheme.paused, in: Circle())
                            .overlay(Circle().stroke(WTheme.groupedCard, lineWidth: 2))
                            .offset(x: 3, y: 3)
                            .transition(.scale.combined(with: .opacity))
                    }
                }
            VStack(alignment: .leading, spacing: 3) {
                Text(deal.name)
                    .font(.system(size: 15.5, weight: .medium))
                    .foregroundStyle(status == "lost" ? WTheme.inkSecondary : WTheme.ink)
                    .strikethrough(status == "lost", color: WTheme.inkTertiary)
                    .lineLimit(1)
                if !details.isEmpty {
                    Text(details.joined(separator: "  ·  "))
                        .font(.system(size: 13))
                        .foregroundStyle(WTheme.inkSecondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 4) {
                if let value = deal.value {
                    Text(CRMFormat.currency(value, code: deal.currency))
                        .font(.system(size: 16, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(status == "won" ? WTheme.positive : status == "lost" ? WTheme.inkTertiary : WTheme.ink)
                        .contentTransition(.numericText())
                }
                if showsStage, status == "open", let stage {
                    CRMColorChip(text: stage.name, hex: stage.color)
                }
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
        .onTapGesture {
            if canWrite { editing = deal }
        }
        .animation(.snappy, value: status)
        .listRowBackground(WTheme.groupedCard)
        .alignmentGuide(.listRowSeparatorLeading) { _ in 52 }
        .swipeActions(edge: .leading, allowsFullSwipe: true) {
            if canWrite, status == "open" {
                Button {
                    Task { await setStatus(deal, status: "won") }
                } label: {
                    Label("Won", systemImage: "trophy.fill")
                }
                .tint(WTheme.positive)
            }
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if canWrite {
                Button {
                    editing = deal
                } label: {
                    Label("Edit", systemImage: "slider.horizontal.3")
                }
                .tint(WTheme.accent)
            }
        }
        .contextMenu {
            if canWrite {
                stageMoveMenu(deal)
                if status == "open" {
                    Button {
                        Task { await setStatus(deal, status: "won") }
                    } label: {
                        Label("Mark won", systemImage: "trophy")
                    }
                    Button(role: .destructive) {
                        Task { await setStatus(deal, status: "lost") }
                    } label: {
                        Label("Mark lost", systemImage: "xmark.seal")
                    }
                } else {
                    Button {
                        Task { await setStatus(deal, status: "open") }
                    } label: {
                        Label("Reopen", systemImage: "arrow.uturn.backward")
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func stageMoveMenu(_ deal: CRMDeal) -> some View {
        let stages = store.selectedPipeline?.orderedStages ?? []
        if !stages.isEmpty {
            Menu("Move to stage") {
                ForEach(stages) { stage in
                    Button {
                        Task { await moveStage(deal, to: stage.id) }
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

    // MARK: Mutations

    private func moveStage(_ deal: CRMDeal, to stageID: String) async {
        var body = CRMDealUpdateBody()
        body.stageID = stageID
        try? await store.update(env.api, deal: deal, body: body)
    }

    private func setStatus(_ deal: CRMDeal, status: String) async {
        var body = CRMDealUpdateBody()
        body.status = status
        do {
            try await store.update(env.api, deal: deal, body: body)
            if status == "won" { celebrate += 1 }
            await store.load(env.api)
        } catch {}
    }
}

// MARK: - Drawer

/// Deals drawer: sky hero with the selected pipeline name and open/won badges,
/// the four status scopes, then the member's pipelines as a second selection
/// group (own sliding capsule) driving store.pipelineID.
private struct CRMDealsSidebar: View {
    let store: CRMDealsStore
    let topInset: CGFloat
    let revealed: Bool
    let onSelectStatus: (String?) -> Void
    let onSelectPipeline: (String) -> Void

    @Namespace private var activeNS

    var body: some View {
        CRMDrawer(title: "Deals", subtitle: store.selectedPipeline?.name, topInset: topInset) {
            if let open = store.statusCount("open") {
                CRMDrawerBadge(symbol: "briefcase.fill", text: "\(WFormat.compact(open)) open")
            }
            if let won = store.summary?.wonValue, won > 0 {
                CRMDrawerBadge(
                    symbol: "checkmark.seal.fill",
                    text: "\(CRMFormat.compactCurrency(won, code: store.displayCurrency)) won"
                )
            }
        } rows: {
            CRMDrawerSectionLabel("Status")
            ForEach(Array(CRMDealScope.allCases.enumerated()), id: \.element) { index, scope in
                CRMDrawerRow(
                    icon: scope.icon,
                    title: scope.title,
                    count: store.statusCount(scope.status),
                    selected: store.statusFilter == scope.status,
                    index: index,
                    revealed: revealed,
                    namespace: activeNS,
                    matchedID: "deals-status"
                ) {
                    onSelectStatus(scope.status)
                }
            }
            if !store.pipelines.isEmpty {
                CRMDrawerSectionLabel("Pipelines")
                ForEach(Array(store.pipelines.enumerated()), id: \.element.id) { offset, pipeline in
                    CRMDrawerRow(
                        icon: "briefcase",
                        title: pipeline.name,
                        count: store.pipelineDealCount(pipeline),
                        selected: store.pipelineID == pipeline.id,
                        index: CRMDealScope.allCases.count + offset,
                        revealed: revealed,
                        namespace: activeNS,
                        matchedID: "deals-pipeline"
                    ) {
                        onSelectPipeline(pipeline.id)
                    }
                }
            }
        }
    }
}

// MARK: - Edit sheet

/// Edit a deal's name, value, currency and stage; won/lost from a segmented
/// control. All fields optional in the PATCH body.
struct CRMDealEditSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    let store: CRMDealsStore
    let deal: CRMDeal

    @State private var name: String
    @State private var valueText: String
    @State private var currency: String
    @State private var stageID: String?
    @State private var status: String
    @State private var saving = false
    @State private var errorMessage: String?

    private let currencyOptions = ["USD", "EUR", "GBP", "AUD", "CAD"]

    init(store: CRMDealsStore, deal: CRMDeal) {
        self.store = store
        self.deal = deal
        _name = State(initialValue: deal.name)
        _valueText = State(initialValue: deal.value.map { NumberFormatter.dealValue.string(from: NSNumber(value: $0)) ?? "" } ?? "")
        _currency = State(initialValue: (deal.currency?.isEmpty == false ? deal.currency! : "USD").uppercased())
        _stageID = State(initialValue: deal.stageID)
        _status = State(initialValue: deal.status ?? "open")
    }

    private var stages: [CRMStage] {
        guard let pipelineID = deal.pipelineID else { return store.selectedPipeline?.orderedStages ?? [] }
        return store.pipelines.first { $0.id == pipelineID }?.orderedStages ?? []
    }

    var body: some View {
        NavigationStack {
            Form {
                Section("Deal") {
                    TextField("Name", text: $name)
                    HStack {
                        TextField("Value", text: $valueText)
                            .keyboardType(.decimalPad)
                            .monospacedDigit()
                        Picker("", selection: $currency) {
                            ForEach(currencyOptions, id: \.self) { code in
                                Text(code).tag(code)
                            }
                        }
                        .labelsHidden()
                        .fixedSize()
                    }
                }
                if !stages.isEmpty {
                    Section("Stage") {
                        Picker("Stage", selection: $stageID) {
                            ForEach(stages) { stage in
                                Text(stage.name).tag(stage.id as String?)
                            }
                        }
                    }
                }
                Section("Status") {
                    Picker("Status", selection: $status) {
                        Text("Open").tag("open")
                        Text("Won").tag("won")
                        Text("Lost").tag("lost")
                    }
                    .pickerStyle(.segmented)
                }
                if let errorMessage {
                    Text(errorMessage)
                        .font(.footnote)
                        .foregroundStyle(WTheme.negative)
                }
            }
            .navigationTitle("Edit deal")
            .navigationBarTitleDisplayMode(.inline)
            .presenceResource("deal:\(deal.id)", action: "editing")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if saving {
                        ProgressView().controlSize(.small)
                    } else {
                        Button("Save") { save() }
                            .disabled(name.trimmingCharacters(in: .whitespaces).isEmpty)
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    private func save() {
        guard !saving else { return }
        saving = true
        errorMessage = nil
        var body = CRMDealUpdateBody()
        let trimmedName = name.trimmingCharacters(in: .whitespaces)
        if trimmedName != deal.name { body.name = trimmedName }
        let parsedValue = Double(valueText.replacingOccurrences(of: ",", with: ""))
        if parsedValue != deal.value { body.value = parsedValue }
        if currency != (deal.currency ?? "USD").uppercased() { body.currency = currency }
        if stageID != deal.stageID { body.stageID = stageID }
        if status != (deal.status ?? "open") { body.status = status }

        Task {
            do {
                try await store.update(env.api, deal: deal, body: body)
                dismiss()
            } catch {
                errorMessage = error.localizedDescription
            }
            saving = false
        }
    }
}

private extension NumberFormatter {
    static let dealValue: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.numberStyle = .decimal
        formatter.usesGroupingSeparator = false
        formatter.maximumFractionDigits = 2
        return formatter
    }()
}
