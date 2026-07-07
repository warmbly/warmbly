import SwiftUI

/// Deals list pushed from the More tab. Pipeline picker, tappable status stat
/// strip, deals grouped by stage, stage-move + edit gated behind manageContacts.
/// No NavigationStack of its own; the More tab owns the stack.
struct CRMDealsView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = CRMDealsStore()
    @State private var editing: CRMDeal?

    private var canWrite: Bool { env.session.can(.manageContacts) }

    var body: some View {
        @Bindable var store = store
        VStack(spacing: 0) {
            if !store.pipelines.isEmpty {
                pipelinePicker
                Divider()
            }
            statStrip
            Divider()
            list
        }
        .navigationTitle("Deals")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                PresenceAvatars()
            }
        }
        .searchable(text: $store.query, prompt: "Search deals")
        .sheet(item: $editing) { deal in
            CRMDealEditSheet(store: store, deal: deal)
        }
        .task(id: store.query) {
            if store.hasLoaded {
                try? await Task.sleep(for: .milliseconds(350))
            }
            guard !Task.isCancelled else { return }
            await store.load(env.api)
        }
        .onChange(of: store.pipelineID) {
            Task { await store.load(env.api) }
        }
        .onChange(of: env.realtime.pulse(for: .crm)) {
            Task { await store.load(env.api) }
        }
    }

    // MARK: Pipeline picker

    private var pipelinePicker: some View {
        @Bindable var store = store
        return Menu {
            Picker("Pipeline", selection: $store.pipelineID) {
                ForEach(store.pipelines) { pipeline in
                    Text(pipeline.name).tag(pipeline.id as String?)
                }
            }
        } label: {
            HStack(spacing: 6) {
                Text(store.selectedPipeline?.name ?? "Pipeline")
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(.primary)
                Image(systemName: "chevron.down")
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(.secondary)
                Spacer()
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
        }
    }

    // MARK: Stat strip

    private var statStrip: some View {
        HStack(spacing: 0) {
            statCell(
                "Open",
                count: store.summary?.openCount,
                value: store.summary?.openValue,
                tone: .sky,
                status: "open"
            )
            hairline
            statCell(
                "Won",
                count: store.summary?.wonCount,
                value: store.summary?.wonValue,
                tone: .emerald,
                status: "won"
            )
            hairline
            statCell(
                "Lost",
                count: store.summary?.lostCount,
                value: store.summary?.lostValue,
                tone: .rose,
                status: "lost"
            )
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
    }

    private var hairline: some View {
        Rectangle().fill(Color(.separator)).frame(width: 0.5, height: 40)
    }

    private func statCell(_ label: String, count: Int?, value: Double?, tone: Tone, status: String) -> some View {
        Button {
            withAnimation(.snappy) {
                store.statusFilter = store.statusFilter == status ? nil : status
            }
            Task { await store.load(env.api) }
        } label: {
            CRMDealStatCell(
                label: label,
                count: count,
                value: value,
                currency: store.displayCurrency,
                tone: tone,
                selected: store.statusFilter == status
            )
            .padding(.horizontal, 8)
        }
        .buttonStyle(TapScaleStyle())
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
                    flatList
                }
            }
            .listStyle(.plain)
            .refreshable { await store.load(env.api) }
        }
    }

    @ViewBuilder
    private var groupedByStage: some View {
        ForEach(store.orderedStages) { stage in
            let stageDeals = store.deals(inStage: stage.id)
            if !stageDeals.isEmpty {
                Section {
                    ForEach(stageDeals) { deal in
                        row(deal)
                    }
                } header: {
                    stageHeader(stage, count: stageDeals.count)
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
                CRMSectionHeader(title: "Closed", count: closed.count, tone: .slate)
            }
        }
    }

    @ViewBuilder
    private var flatList: some View {
        ForEach(store.deals) { deal in
            row(deal)
        }
    }

    private func stageHeader(_ stage: CRMStage, count: Int) -> some View {
        HStack(spacing: 6) {
            Circle()
                .fill(CRMHex.color(stage.color) ?? WTheme.paused)
                .frame(width: 7, height: 7)
            Text(stage.name.uppercased())
                .font(.system(size: 10, weight: .medium))
                .tracking(1.4)
                .foregroundStyle(.secondary)
            Text("\(count)")
                .font(.system(size: 10, weight: .semibold))
                .monospacedDigit()
                .foregroundStyle(.tertiary)
            Spacer(minLength: 0)
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        let filtering = store.statusFilter != nil || !store.query.trimmingCharacters(in: .whitespaces).isEmpty
        EmptyStateView(
            title: filtering ? "No matching deals" : "No deals yet",
            message: filtering
                ? "Try a different search or status."
                : "Deals created from campaigns or the web dashboard show up here."
        )
    }

    // MARK: Row

    private func row(_ deal: CRMDeal) -> some View {
        let stage = deal.stage ?? store.stage(deal.stageID)
        return HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(deal.name)
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                HStack(spacing: 6) {
                    if let contact = deal.contact {
                        Text(contact.displayName)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                    if let stage {
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
