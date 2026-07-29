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

    private func selectStatus(_ status: String?) {
        withAnimation(.spring(response: 0.38, dampingFraction: 0.8)) { store.statusFilter = status }
        closeSidebarSoon()
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
            CRMSearchBar(
                query: $store.query,
                prompt: "Search deals",
                onMenu: { openSidebar() },
                menuLabel: "Open deals menu"
            ) {
                CRMCircleButton(symbol: "xmark", label: "Close deals", weight: .semibold, size: 15) {
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
            Text(captionTitle)
                .font(.caption.weight(.semibold))
                .tracking(0.9)
                .foregroundStyle(.secondary)
            if !isSearching, let count = store.statusCount(store.statusFilter), count > 0 {
                Text(WFormat.compact(count))
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
                    .contentTransition(.numericText())
            }
            Spacer()
            if isSearching, store.hasLoaded, let count = store.totalCount {
                Text("\(count) found")
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

    private var captionTitle: String {
        if isSearching { return "SEARCH RESULTS" }
        return scope.title.uppercased()
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { SkeletonRows(rows: 10) }
        } else if let error = store.errorMessage, store.deals.isEmpty {
            ErrorStateView(title: "Couldn't load deals", message: error) {
                await store.load(env.api)
            }
        } else if store.deals.isEmpty {
            emptyState
        } else {
            List {
                if store.statusFilter == nil, !store.orderedStages.isEmpty {
                    groupedByStage
                } else {
                    // The scope caption above already names the filter.
                    ForEach(store.deals) { deal in
                        row(deal)
                    }
                }
                CRMEndMarker(store.totalCount ?? store.deals.count, "deal")
            }
            .listStyle(.plain)
            .scrollDismissesKeyboard(.immediately)
            .refreshable { await store.load(env.api) }
        }
    }

    @ViewBuilder
    private var groupedByStage: some View {
        ForEach(store.orderedStages) { stage in
            let stageDeals = store.deals(inStage: stage.id)
            if !stageDeals.isEmpty {
                CRMListCaption(
                    title: stage.name,
                    count: stageDeals.count,
                    dotColor: CRMHex.color(stage.color) ?? WTheme.paused
                )
                ForEach(stageDeals) { deal in
                    // The eyebrow already names the stage; skip the row chip.
                    row(deal, showsStage: false)
                }
            }
        }
        let closed = store.closedDeals
        if !closed.isEmpty {
            CRMListCaption(title: "Closed", count: closed.count, tone: .slate)
            ForEach(closed) { deal in
                row(deal)
            }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        if isSearching {
            EmptyStateView(title: "No matching deals", message: "Try a different search.")
        } else {
            switch scope {
            case .all:
                EmptyStateView(
                    title: "No deals yet",
                    message: "Deals created from campaigns or the web dashboard show up here."
                )
            case .open:
                EmptyStateView(title: "No open deals", message: "Deals still in play show up here.")
            case .won:
                EmptyStateView(title: "No won deals yet", message: "Deals marked won land here.")
            case .lost:
                EmptyStateView(title: "No lost deals", message: "Deals marked lost show up here.")
            }
        }
    }

    // MARK: Row

    private func row(_ deal: CRMDeal, showsStage: Bool = true) -> some View {
        let stage = deal.stage ?? store.stage(deal.stageID)
        return HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(deal.name)
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                HStack(spacing: 6) {
                    if let contact = deal.contact {
                        Text(contact.displayName)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                    if showsStage, let stage {
                        CRMColorChip(text: stage.name, hex: stage.color)
                    }
                }
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 4) {
                if let value = deal.value {
                    Text(CRMFormat.currency(value, code: deal.currency))
                        .font(.subheadline.weight(.semibold))
                        .monospacedDigit()
                        .foregroundStyle(.primary)
                }
                if (deal.status ?? "open") != "open" {
                    StatusPill(text: deal.status ?? "open", tone: deal.statusTone)
                }
            }
        }
        .padding(.vertical, 6)
        .contentShape(Rectangle())
        .onTapGesture {
            if canWrite { editing = deal }
        }
        .listRowInsets(EdgeInsets(top: 4, leading: 20, bottom: 4, trailing: 16))
        .listRowBackground(Color(.systemBackground))
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
                if (deal.status ?? "open") == "open" {
                    Button {
                        Task { await setStatus(deal, status: "won") }
                    } label: {
                        Label("Mark won", systemImage: "checkmark.seal")
                    }
                    Button(role: .destructive) {
                        Task { await setStatus(deal, status: "lost") }
                    } label: {
                        Label("Mark lost", systemImage: "xmark.seal")
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
        try? await store.update(env.api, deal: deal, body: body)
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
