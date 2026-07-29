import SwiftUI

/// Campaign settings, editable on mobile: name/description, delivery toggles,
/// and the single-value tuning knobs (sender strategy, rotation, ESP matching,
/// ramp-up, lead flow) as menus/steppers that PATCH inline. Genuinely
/// web-shaped things (CC/BCC lists, tracking-domain DNS) stay read-only with a
/// handoff. Delete lives here too.
struct CampaignSettingsPage: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let store: CampaignDetailStore
    let onDeleted: () -> Void

    @State private var name = ""
    @State private var description = ""
    @State private var isSaving = false
    @State private var confirmDelete = false
    @State private var deleteError: String?

    private var campaign: Campaign { store.campaign }
    private var canManage: Bool { env.session.can(.manageCampaigns) }

    private var trimmedName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var nameDirty: Bool {
        trimmedName != campaign.name || description != (campaign.description ?? "")
    }
    private var nameValid: Bool { (3...50).contains(trimmedName.count) }

    var body: some View {
        List {
            identitySection
            deliverySection
            strategySection
            rampSection
            leadFlowSection
            handoffSection
            if canManage {
                dangerSection
            }
        }
        .listStyle(.insetGrouped)
        .navigationTitle("Settings")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if nameDirty, canManage {
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        Task { await saveIdentity() }
                    } label: {
                        if isSaving {
                            ProgressView().controlSize(.small)
                        } else {
                            Text("Save").fontWeight(.semibold)
                        }
                    }
                    .disabled(!nameValid || isSaving)
                }
            }
        }
        .onAppear {
            name = campaign.name
            description = campaign.description ?? ""
        }
        .confirmationDialog("Delete campaign?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Delete \(campaign.name)", role: .destructive) {
                Task { await deleteCampaign() }
            }
        } message: {
            Text("Leads stay in your contacts. This can't be undone.")
        }
        .alert("Couldn't update campaign", isPresented: Binding(
            get: { store.actionError != nil },
            set: { if !$0 { store.actionError = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(store.actionError ?? "")
        }
        .alert("Couldn't delete campaign", isPresented: Binding(
            get: { deleteError != nil },
            set: { if !$0 { deleteError = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(deleteError ?? "")
        }
    }

    // MARK: Name + description

    private var identitySection: some View {
        Section {
            TextField("Campaign name", text: $name)
                .disabled(!canManage)
            TextField("Description (optional)", text: $description, axis: .vertical)
                .lineLimit(2...5)
                .disabled(!canManage)
        } header: {
            Text("Campaign")
        } footer: {
            if !nameValid, nameDirty {
                Text("Name must be 3–50 characters.")
                    .foregroundStyle(WTheme.negative)
            }
        }
    }

    // MARK: Delivery toggles

    private var deliverySection: some View {
        Section {
            toggleRow(
                "Plain-text only", subtitle: "Send without HTML formatting", icon: "textformat", tone: .slate,
                value: campaign.textOnly ?? false,
                body: { CampaignUpdateBody(textOnly: $0) }, mutate: { $0.textOnly = $1 }
            )
            toggleRow(
                "Unsubscribe header", subtitle: "One-click list-unsubscribe (recommended)", icon: "hand.wave.fill", tone: .emerald,
                value: campaign.unsubscribeHeader ?? false,
                body: { CampaignUpdateBody(unsubscribeHeader: $0) }, mutate: { $0.unsubscribeHeader = $1 }
            )
            toggleRow(
                "Risky addresses", subtitle: "Send to leads with risky verification", icon: "exclamationmark.shield.fill", tone: .amber,
                value: campaign.riskyEmails ?? false,
                body: { CampaignUpdateBody(riskyEmails: $0) }, mutate: { $0.riskyEmails = $1 }
            )
        } header: {
            Text("Delivery")
        } footer: {
            Text("Risky addresses raise bounce risk; keep this off unless you know the list.")
        }
    }

    // MARK: Sending strategy (editable menus)

    private var strategySection: some View {
        Section {
            menuRow(
                "Sender strategy", icon: "person.crop.rectangle.stack.fill", tone: .sky,
                current: campaign.senderStrategy ?? "tags",
                options: [("tags", "By tags"), ("explicit", "Explicit list")]
            ) { value in
                await store.update(env.api, body: CampaignUpdateBody(senderStrategy: value)) { $0.senderStrategy = value }
            }
            menuRow(
                "Rotation", icon: "arrow.triangle.2.circlepath", tone: .indigo,
                current: campaign.rotationMode ?? "weighted",
                options: [("weighted", "Weighted"), ("round_robin", "Round robin"), ("least_recently_used", "Least recently used")]
            ) { value in
                await store.update(env.api, body: CampaignUpdateBody(rotationMode: value)) { $0.rotationMode = value }
            }
            menuRow(
                "ESP matching", icon: "arrow.left.arrow.right", tone: .emerald,
                current: campaign.espMatchMode ?? "off",
                options: [("off", "Off"), ("prefer", "Prefer same ESP"), ("strict", "Strict same ESP")]
            ) { value in
                await store.update(env.api, body: CampaignUpdateBody(espMatchMode: value)) { $0.espMatchMode = value }
            }
        } header: {
            Text("Sending strategy")
        } footer: {
            Text("ESP matching sends from a mailbox on the same provider as the lead when possible.")
        }
    }

    // MARK: Ramp-up

    private var rampSection: some View {
        Section {
            toggleRow(
                "Gradual ramp-up", subtitle: "Raise the daily send slowly over time", icon: "chart.line.uptrend.xyaxis", tone: .orange,
                value: campaign.rampEnabled ?? false,
                body: { CampaignUpdateBody(rampEnabled: $0) }, mutate: { $0.rampEnabled = $1 }
            )
            if campaign.rampEnabled == true {
                stepperRow(
                    "Start", subtitle: "emails/day on day one", icon: "1.circle.fill", tone: .orange,
                    value: campaign.rampStart ?? 10, range: 1...100,
                    body: { CampaignUpdateBody(rampStart: $0) }, mutate: { $0.rampStart = $1 }
                )
                stepperRow(
                    "Ceiling", subtitle: "the most it ramps to", icon: "arrow.up.to.line", tone: .orange,
                    value: campaign.rampCeiling ?? 50, range: max(campaign.rampStart ?? 1, 1)...100,
                    body: { CampaignUpdateBody(rampCeiling: $0) }, mutate: { $0.rampCeiling = $1 }
                )
                stepperRow(
                    "Step", subtitle: "added per day", icon: "plus.forwardslash.minus", tone: .orange,
                    value: campaign.rampIncrement ?? 1, range: 0...100,
                    body: { CampaignUpdateBody(rampIncrement: $0) }, mutate: { $0.rampIncrement = $1 }
                )
            }
        } header: {
            Text("Ramp-up")
        } footer: {
            Text("Ramp only ever lowers volume against the daily limit, never raises it above your cap.")
        }
    }

    // MARK: Lead flow

    private var leadFlowSection: some View {
        Section {
            stepperRow(
                "New leads per day", subtitle: (campaign.maxNewLeadsPerDay ?? 0) == 0 ? "Unlimited" : nil,
                icon: "person.badge.plus", tone: .sky,
                value: campaign.maxNewLeadsPerDay ?? 0, range: 0...1000, step: 5, zeroLabel: "Off",
                body: { CampaignUpdateBody(maxNewLeadsPerDay: $0) }, mutate: { $0.maxNewLeadsPerDay = $1 }
            )
            toggleRow(
                "Prioritize new leads", subtitle: "Send to fresh leads before follow-ups", icon: "arrow.up.circle.fill", tone: .sky,
                value: campaign.prioritizeNewLeads ?? false,
                body: { CampaignUpdateBody(prioritizeNewLeads: $0) }, mutate: { $0.prioritizeNewLeads = $1 }
            )
        } header: {
            Text("Lead flow")
        } footer: {
            Text("Cap how many brand-new leads enter the sequence each day. Off means no limit.")
        }
    }

    // MARK: Web-only (read-only + handoff)

    @ViewBuilder
    private var handoffSection: some View {
        let cc = campaign.cc ?? []
        let bcc = campaign.bcc ?? []
        let domain = campaign.trackingDomain ?? ""
        if !cc.isEmpty || !bcc.isEmpty || !domain.isEmpty {
            Section {
                if !cc.isEmpty { readOnlyRow("CC", cc.joined(separator: ", ")) }
                if !bcc.isEmpty { readOnlyRow("BCC", bcc.joined(separator: ", ")) }
                if !domain.isEmpty {
                    readOnlyRow(
                        "Tracking domain",
                        campaign.trackingDomainVerified == true ? domain : "\(domain) (unverified)"
                    )
                }
            } header: {
                Text("On the web")
            } footer: {
                Text("CC/BCC lists and tracking-domain DNS are managed in Warmbly on the web.")
            }
        }
    }

    private func readOnlyRow(_ title: String, _ value: String) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title)
            Spacer()
            Text(value)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.trailing)
        }
    }

    // MARK: Danger

    private var dangerSection: some View {
        Section {
            Button(role: .destructive) {
                confirmDelete = true
            } label: {
                HStack {
                    Spacer()
                    Text("Delete campaign")
                    Spacer()
                }
            }
        } footer: {
            Text("Only the campaign creator can delete a campaign.")
        }
    }

    // MARK: Reusable rows

    private func toggleRow(
        _ title: String, subtitle: String, icon: String, tone: Tone,
        value: Bool,
        body: @escaping (Bool) -> CampaignUpdateBody,
        mutate: @escaping (inout Campaign, Bool) -> Void
    ) -> some View {
        Toggle(isOn: Binding(
            get: { value },
            set: { newValue in
                Task { await store.update(env.api, body: body(newValue)) { mutate(&$0, newValue) } }
            }
        )) {
            rowLabel(title, subtitle: subtitle, icon: icon, tone: tone)
        }
        .tint(WTheme.accent)
        .disabled(!canManage)
    }

    private func menuRow(
        _ title: String, icon: String, tone: Tone,
        current: String,
        options: [(value: String, label: String)],
        apply: @escaping (String) async -> Void
    ) -> some View {
        HStack(spacing: 12) {
            rowLabel(title, subtitle: nil, icon: icon, tone: tone)
            Spacer(minLength: 8)
            Menu {
                ForEach(options, id: \.value) { option in
                    Button {
                        Task { await apply(option.value) }
                    } label: {
                        if option.value == current {
                            Label(option.label, systemImage: "checkmark")
                        } else {
                            Text(option.label)
                        }
                    }
                }
            } label: {
                HStack(spacing: 3) {
                    Text(options.first { $0.value == current }?.label ?? current)
                        .font(.subheadline)
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.system(size: 11, weight: .semibold))
                }
                .foregroundStyle(WTheme.accent)
            }
            .disabled(!canManage)
        }
    }

    private func stepperRow(
        _ title: String, subtitle: String?, icon: String, tone: Tone,
        value: Int, range: ClosedRange<Int>, step: Int = 1, zeroLabel: String? = nil,
        body: @escaping (Int) -> CampaignUpdateBody,
        mutate: @escaping (inout Campaign, Int) -> Void
    ) -> some View {
        let display = (value == 0 && zeroLabel != nil) ? zeroLabel! : "\(value)"
        return HStack(spacing: 12) {
            rowLabel(title, subtitle: subtitle, icon: icon, tone: tone)
            Spacer(minLength: 8)
            HStack(spacing: 12) {
                stepButton("minus", enabled: canManage && value > range.lowerBound) {
                    let next = max(range.lowerBound, value - step)
                    Task { await store.update(env.api, body: body(next)) { mutate(&$0, next) } }
                }
                Text(display)
                    .font(.system(size: 17, weight: .semibold))
                    .monospacedDigit()
                    .frame(minWidth: 40)
                    .contentTransition(.numericText())
                stepButton("plus", enabled: canManage && value < range.upperBound) {
                    let next = min(range.upperBound, value + step)
                    Task { await store.update(env.api, body: body(next)) { mutate(&$0, next) } }
                }
            }
        }
    }

    private func rowLabel(_ title: String, subtitle: String?, icon: String, tone: Tone) -> some View {
        HStack(spacing: 12) {
            IconTile(symbol: icon, tone: tone, size: 30)
            VStack(alignment: .leading, spacing: 1) {
                Text(title)
                if let subtitle {
                    Text(subtitle)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private func stepButton(_ symbol: String, enabled: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 13, weight: .bold))
                .foregroundStyle(enabled ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(Color(.tertiaryLabel)))
                .frame(width: 30, height: 30)
                .background(Tone.slate.background, in: Circle())
        }
        .buttonStyle(TapScaleStyle())
        .disabled(!enabled)
    }

    // MARK: Actions

    private func saveIdentity() async {
        isSaving = true
        let newName = trimmedName
        let newDescription = description
        await store.update(
            env.api,
            body: CampaignUpdateBody(name: newName, description: newDescription)
        ) {
            $0.name = newName
            $0.description = newDescription
        }
        name = store.campaign.name
        description = store.campaign.description ?? ""
        isSaving = false
    }

    private func deleteCampaign() async {
        do {
            try await store.delete(env.api)
            onDeleted()
        } catch {
            deleteError = error.localizedDescription
        }
    }
}
