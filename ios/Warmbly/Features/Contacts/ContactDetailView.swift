import SwiftUI

private extension ContactDetailTab {
    var icon: String {
        switch self {
        case .emails: return "envelope.fill"
        case .timeline: return "clock.fill"
        case .notes: return "note.text"
        case .deals: return "briefcase.fill"
        }
    }
}

/// Contact 360 detail: sky hero with avatar, engagement chips, and
/// subscription state; swipeable section tabs on the white sheet below.
/// Presence claim on `contact:<id>`; reloads on the contacts (and crm,
/// for notes/deals) realtime pulse.
struct ContactDetailView: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    @State private var store: ContactDetailStore
    @State private var categoryStore = ContactCategoryStore()
    @State private var tab: ContactDetailTab = .emails
    @State private var showEdit = false

    /// Push the fresh contact back into the list when edited or deleted.
    private let onUpdated: (Contact) -> Void
    private let onDeleted: (String) -> Void

    init(
        contact: Contact,
        onUpdated: @escaping (Contact) -> Void = { _ in },
        onDeleted: @escaping (String) -> Void = { _ in }
    ) {
        _store = State(initialValue: ContactDetailStore(contact: contact))
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
        .sheet(isPresented: $showEdit) {
            ContactEditSheet(
                contact: contact,
                categoryStore: categoryStore
            ) { updated in
                store.applyLocal(updated)
                onUpdated(updated)
            } onDeleted: {
                onDeleted(contact.id)
                dismiss()
            }
        }
    }

    // MARK: Hero (white-on-sky)

    private var hero: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .center, spacing: 14) {
                WAvatar(name: contact.displayName, seed: contact.id, size: 64, onSky: true)
                    .shadow(color: Color(hex: 0x0C4A6E).opacity(0.28), radius: 10, y: 4)
                VStack(alignment: .leading, spacing: 3) {
                    Text(contact.displayName)
                        .font(.title2.bold())
                        .foregroundStyle(.white)
                        .lineLimit(1)
                        .minimumScaleFactor(0.75)
                    if let email = contact.email, !email.isEmpty {
                        Text(email)
                            .font(.subheadline)
                            .foregroundStyle(.white.opacity(0.75))
                            .lineLimit(1)
                            .textSelection(.enabled)
                    }
                    if let meta = metaLine {
                        Text(meta)
                            .font(.subheadline)
                            .foregroundStyle(.white.opacity(0.75))
                            .lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
            }
            HStack(spacing: 8) {
                heroStatusPill
                bouncedPill
                categoryDots
                Spacer(minLength: 0)
            }
            ResourceViewers(resource: presenceKey)
            heroStats
        }
        .padding(.horizontal, 20)
        .padding(.top, 2)
        .padding(.bottom, 18)
    }

    /// Suppression (red) wins over unsubscribed (slate), mirroring the web.
    private var statusInfo: (label: String, tone: Tone) {
        if contact.suppression != nil { return ("Suppressed", .rose) }
        if contact.subscribed == false { return ("Unsubscribed", .slate) }
        return ("Subscribed", .emerald)
    }

    private var heroStatusPill: some View {
        let info = statusInfo
        return HStack(spacing: 5) {
            Circle()
                .fill(info.tone.color)
                .frame(width: 7, height: 7)
            Text(info.label)
                .font(.caption.weight(.semibold))
                .foregroundStyle(.white)
        }
        .padding(.horizontal, 9)
        .padding(.vertical, 4)
        .background(.white.opacity(0.16), in: Capsule())
        .accessibilityLabel(
            contact.suppression.map { "Suppressed: \($0.source ?? "unknown")" } ?? info.label
        )
    }

    /// Bounces don't earn a stat chip; surface them as a warning pill instead.
    @ViewBuilder
    private var bouncedPill: some View {
        if let bounced = contact.engagement?.totalBounced, bounced > 0 {
            HStack(spacing: 5) {
                Circle()
                    .fill(Tone.rose.color)
                    .frame(width: 7, height: 7)
                Text("\(WFormat.compact(bounced)) bounced")
                    .font(.caption.weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(.white)
            }
            .padding(.horizontal, 9)
            .padding(.vertical, 4)
            .background(.white.opacity(0.16), in: Capsule())
        }
    }

    @ViewBuilder
    private var categoryDots: some View {
        if let categories = contact.categories, !categories.isEmpty {
            HStack(spacing: 4) {
                ForEach(categories.prefix(5)) { category in
                    Circle()
                        .fill(ContactColor.dot(category.color, seed: category.id))
                        .frame(width: 7, height: 7)
                }
            }
            .padding(.horizontal, 8)
            .padding(.vertical, 6)
            .background(.white.opacity(0.16), in: Capsule())
        }
    }

    private var metaLine: String? {
        let parts = [contact.company, contact.phone]
            .compactMap { $0 }
            .filter { !$0.isEmpty }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    @ViewBuilder
    private var heroStats: some View {
        if let e = contact.engagement {
            HStack(spacing: 10) {
                AirStatChip(
                    value: WFormat.compact(e.totalSent ?? 0),
                    label: "Sent",
                    symbol: "paperplane.fill"
                )
                AirStatChip(
                    value: WFormat.compact(e.totalOpened ?? 0),
                    label: "Opened",
                    symbol: "envelope.open.fill"
                )
                AirStatChip(
                    value: WFormat.compact(e.totalReplied ?? 0),
                    label: "Replied",
                    symbol: "arrowshape.turn.up.left.fill"
                )
            }
        }
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
                Image(systemName: "ellipsis.circle")
            }
        }
    }

    // MARK: Content

    /// Horizontal page swipe between sections, synced with the pill bar.
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
