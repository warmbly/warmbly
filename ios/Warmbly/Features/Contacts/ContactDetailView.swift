import SwiftUI

private extension ContactDetailTab {
    var icon: String {
        switch self {
        case .emails: return "envelope"
        case .timeline: return "clock"
        case .notes: return "note.text"
        case .deals: return "briefcase"
        }
    }
}

/// Contact 360 detail: a canvas header with avatar, hairline state chips and
/// engagement tiles; swipeable section tabs below.
/// Presence claim on `contact:<id>`; reloads on the contacts (and crm,
/// for notes/deals) realtime pulse.
struct ContactDetailView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    @Environment(\.closeDetail) private var closeDetail

    @State private var store: ContactDetailStore
    @State private var categoryStore = ContactCategoryStore()
    @State private var tab: ContactDetailTab = .emails
    @State private var showEdit = false
    @State private var showOrganize = false
    @State private var composing = false
    @State private var organizeCampaigns: [ContactMiniCampaign] = []
    @Environment(\.openURL) private var openURL

    /// Push the fresh contact back into the list when edited or deleted.
    private let onUpdated: (Contact) -> Void
    private let onDeleted: (String) -> Void
    /// The list row's subscription, so a change made from the list (and its rollback) shows here.
    private let listSubscribed: Bool?

    init(
        contact: Contact,
        listSubscribed: Bool? = nil,
        onUpdated: @escaping (Contact) -> Void = { _ in },
        onDeleted: @escaping (String) -> Void = { _ in }
    ) {
        _store = State(initialValue: ContactDetailStore(contact: contact))
        self.listSubscribed = listSubscribed
        self.onUpdated = onUpdated
        self.onDeleted = onDeleted
    }

    private var contact: Contact { store.contact }
    private var presenceKey: String { "contact:\(contact.id)" }

    var body: some View {
        AirDetailScaffold(
            tabs: ContactDetailTab.allCases.map { AirTabItem(id: $0.rawValue, title: $0.title, icon: $0.icon) },
            selection: Binding(
                get: { tab.rawValue },
                set: { tab = ContactDetailTab(rawValue: $0) ?? .emails }
            )
        ) {
            hero
        } content: {
            content
        }
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbarContent }
        .presenceResource(presenceKey)
        .task { await store.refreshHeader(env.api) }
        .task { await categoryStore.load(env.api) }
        .onChange(of: env.realtime.pulse(for: .contacts)) {
            Task { await store.refreshHeader(env.api) }
        }
        .onChange(of: listSubscribed) { _, subscribed in
            guard let subscribed, subscribed != (contact.subscribed != false) else { return }
            var updated = contact
            updated.subscribed = subscribed
            store.applyLocal(updated)
        }
        .sheet(isPresented: $composing) {
            ComposeView(prefillTo: [contact.email ?? ""].filter { !$0.isEmpty })
        }
        .sheet(isPresented: $showOrganize) {
            ContactBulkEditSheet(
                ids: [contact.id],
                categories: categoryStore.categories,
                campaigns: organizeCampaigns,
                loadCampaigns: {
                    guard organizeCampaigns.isEmpty else { return }
                    if let page: ListResponse<ContactMiniCampaign> = try? await env.api.get("campaigns", query: ["limit": "100"]) {
                        organizeCampaigns = page.data
                    }
                },
                perform: { body in
                    let _: [Contact] = try await env.api.patch("contacts", body: body)
                    await store.refreshHeader(env.api)
                    onUpdated(store.contact)
                }
            )
        }
        .sheet(isPresented: $showEdit) {
            ContactEditSheet(
                contact: contact,
                categoryStore: categoryStore
            ) { updated in
                store.applyLocal(updated)
                onUpdated(updated)
            } onDeleted: {
                onDeleted(contact.id)
                leave()
            }
        }
    }

    /// Back to the list: pops the stack, or empties the detail column beside it.
    private func leave() {
        if let closeDetail { closeDetail() } else { dismiss() }
    }

    // MARK: Hero

    private var hero: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .center, spacing: 14) {
                avatar
                VStack(alignment: .leading, spacing: 3) {
                    Text(contact.displayName)
                        .displayFont(26)
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                        .minimumScaleFactor(0.75)
                    if let role = roleLine {
                        Text(role)
                            .font(.system(size: 14.5, weight: .medium))
                            .foregroundStyle(WTheme.inkSecondary)
                            .lineLimit(1)
                    }
                    if contact.hasName, let email = contact.email, !email.isEmpty {
                        Text(email)
                            .font(.system(size: 13.5))
                            .foregroundStyle(WTheme.inkTertiary)
                            .lineLimit(1)
                            .textSelection(.enabled)
                    }
                }
                Spacer(minLength: 8)
            }
            RecipientFlow(spacing: 6) {
                heroStatusPill
                bouncedPill
                ForEach(contact.categories ?? []) { category in
                    heroChip {
                        Circle()
                            .fill(ContactColor.dot(category.color, seed: category.id))
                            .frame(width: 7, height: 7)
                        Text(category.title ?? "Label")
                            .font(.system(size: 12.5, weight: .medium))
                            .foregroundStyle(WTheme.inkSecondary)
                    }
                }
            }
            ResourceViewers(resource: presenceKey)
            quickActions
            if let engagement = contact.engagement {
                ContactJourney(engagement: engagement)
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, 6)
        .padding(.bottom, 16)
        .animation(.snappy, value: contact.engagement)
    }

    /// The avatar, ringed like its list row for a reply or click in the last week.
    private var avatar: some View {
        let latest = contact.engagement.flatMap(ContactJourney.latest)
        let warm = latest.map { ($0.kind == .replied || $0.kind == .clicked) && Date.now.timeIntervalSince($0.at) < 7 * 86_400 } ?? false
        return WAvatar(name: contact.displayName, seed: contact.id, size: 64)
            .padding(4)
            .overlay {
                Circle()
                    .strokeBorder(warm ? (latest?.kind.color ?? WTheme.hairlineStrong) : WTheme.hairlineStrong, lineWidth: warm ? 2.5 : 1)
            }
    }

    /// "IT Director at Fieldstone", from the contact's title field and company.
    private var roleLine: String? {
        let fields = contact.customFields ?? [:]
        let title = (fields["title"] ?? fields["job_title"] ?? fields["jobTitle"] ?? "").trimmingCharacters(in: .whitespaces)
        let company = (contact.company ?? "").trimmingCharacters(in: .whitespaces)
        switch (title.isEmpty, company.isEmpty) {
        case (false, false): return "\(title) at \(company)"
        case (false, true): return title
        case (true, false): return company
        default: return nil
        }
    }

    /// Hairline chip used across the hero: raised fill, strong hairline edge.
    private func heroChip<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        HStack(spacing: 6) { content() }
            .padding(.horizontal, 9)
            .frame(height: 26)
            .background(WTheme.raised, in: Capsule())
            .overlay(Capsule().strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
    }

    /// Suppression (red) wins over unsubscribed (slate), mirroring the web.
    private var statusInfo: (label: String, tone: Tone) {
        if contact.suppression != nil { return ("Suppressed", .rose) }
        if contact.subscribed == false { return ("Unsubscribed", .slate) }
        return ("Subscribed", .emerald)
    }

    private var heroStatusPill: some View {
        let info = statusInfo
        return heroChip {
            Circle()
                .fill(info.tone.color)
                .frame(width: 6, height: 6)
            Text(info.label)
                .font(.system(size: 12.5, weight: .medium))
                .foregroundStyle(WTheme.inkSecondary)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(
            contact.suppression.map { "Suppressed: \($0.source ?? "unknown")" } ?? info.label
        )
    }

    /// Bounces don't earn a journey step; surface them as a warning pill instead.
    @ViewBuilder
    private var bouncedPill: some View {
        if let bounced = contact.engagement?.totalBounced, bounced > 0 {
            heroChip {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.system(size: 10, weight: .semibold))
                    .foregroundStyle(Tone.rose.color)
                Text("\(WFormat.compact(bounced)) bounced")
                    .font(.system(size: 12.5, weight: .medium))
                    .monospacedDigit()
                    .foregroundStyle(Tone.rose.color)
            }
        }
    }

    // MARK: Quick actions

    private var quickActions: some View {
        HStack(spacing: 8) {
            if env.session.can(.accessUnibox), let email = contact.email, !email.isEmpty, contact.subscribed != false {
                quickAction("Email", symbol: "envelope.fill") { composing = true }
            }
            if let phone = contact.phone?.filter({ $0.isNumber || $0 == "+" }), !phone.isEmpty, let url = URL(string: "tel:\(phone)") {
                quickAction("Call", symbol: "phone.fill") { openURL(url) }
            }
            if env.session.can(.manageContacts) {
                quickAction("Campaign", symbol: "paperplane.fill") { showOrganize = true }
            }
            quickAction("Note", symbol: "square.and.pencil") {
                withAnimation(.snappy) { tab = .notes }
            }
        }
    }

    private func quickAction(_ title: String, symbol: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            VStack(spacing: 5) {
                Image(systemName: symbol)
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(WTheme.accent)
                Text(title)
                    .font(.system(size: 12.5, weight: .medium))
                    .foregroundStyle(WTheme.ink)
            }
            .frame(maxWidth: .infinity)
            .frame(height: 58)
            .contentShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        }
        .buttonStyle(TapScaleStyle())
        .glassEffect(.regular.interactive(), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .sensoryFeedback(.impact(weight: .light), trigger: composing)
    }

    // MARK: Toolbar

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
                Button {
                    Task { await store.refreshHeader(env.api) }
                } label: {
                    Label("Refresh", systemImage: "arrow.clockwise")
                }
                if env.session.can(.manageContacts) {
                    Button {
                        showEdit = true
                    } label: {
                        Label("Edit contact", systemImage: "pencil")
                    }
                }
            } label: {
                Image(systemName: "ellipsis")
                    .foregroundStyle(WTheme.ink)
            }
        }
    }

    // MARK: Content

    /// Horizontal page swipe between sections, synced with the tab bar.
    private var content: some View {
        TabView(selection: $tab) {
            ContactEmailsSection(store: store)
                .tag(ContactDetailTab.emails)
            ContactTimelineSection(store: store)
                .tag(ContactDetailTab.timeline)
            ContactNotesSection(store: store)
                .tag(ContactDetailTab.notes)
            ContactDealsSection(store: store)
                .tag(ContactDetailTab.deals)
        }
        .tabViewStyle(.page(indexDisplayMode: .never))
        .animation(.snappy, value: tab)
    }
}
