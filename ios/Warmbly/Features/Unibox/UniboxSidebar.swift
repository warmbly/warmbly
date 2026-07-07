import SwiftUI

/// Navigation drawer for the unibox: a slim sky hero (white brand mark + tiny
/// glass badges for sent today / unread), then scopes, mailboxes, and labels
/// as clean pill rows on a rounded white sheet. The selected pill slides
/// between rows (matched geometry) and rows cascade in when the drawer opens.
/// Counts come from `GET /v1/unibox/overview`; sent-today from analytics.
/// Presented full-bleed by `UniboxRootView`.
struct UniboxSidebar: View {
    let store: UniboxStore
    /// All of the member's categories (session), merged with overview unread counts.
    let labels: [UniboxGroupCount]
    let selection: UniboxScope
    let topInset: CGFloat
    let revealed: Bool
    let onSelect: (UniboxScope) -> Void
    let onScheduled: () -> Void

    @Namespace private var activeNS

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            hero
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    scopeRows
                    if !store.mailboxes.isEmpty {
                        sectionLabel("Mailboxes")
                        mailboxRows
                    }
                    if !labels.isEmpty {
                        sectionLabel("Labels")
                        labelRows
                        row(
                            index: 7 + store.mailboxes.count + labels.count,
                            icon: "tag.slash",
                            title: "Uncategorized",
                            count: 0,
                            selected: selection == .uncategorized
                        ) {
                            onSelect(.uncategorized)
                        }
                    }
                }
                .padding(.horizontal, 12)
                .padding(.top, 16)
                .padding(.bottom, 40)
            }
            .background {
                UnevenRoundedRectangle(topLeadingRadius: 24, topTrailingRadius: 24, style: .continuous)
                    .fill(Color(.systemBackground))
                    .shadow(color: .black.opacity(0.1), radius: 14, y: -4)
            }
        }
        .background(alignment: .top) {
            AirSkyWash().frame(height: 320)
        }
        .background(Color(.systemBackground))
    }

    // MARK: Hero

    private var hero: some View {
        VStack(alignment: .leading, spacing: 13) {
            HStack(spacing: 8) {
                WarmblyLogo()
                    .fill(.white)
                    .frame(width: 21, height: 21 * (764 / 746))
                Text("Warmbly")
                    .font(.system(size: 17.5, weight: .heavy))
                    .tracking(-0.4)
                    .foregroundStyle(.white)
            }
            HStack(spacing: 6) {
                if let sent = store.sentToday {
                    heroBadge(symbol: "paperplane.fill", text: "\(WFormat.compact(sent)) sent today")
                }
                heroBadge(symbol: "envelope.badge.fill", text: "\(WFormat.compact(store.unreadCount)) unread")
                if store.sentToday == nil {
                    heroBadge(symbol: "sun.max.fill", text: "\(WFormat.compact(store.todayCount)) today")
                }
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, topInset + 12)
        .padding(.bottom, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func heroBadge(symbol: String, text: String) -> some View {
        HStack(spacing: 5) {
            Image(systemName: symbol)
                .font(.system(size: 10.5, weight: .semibold))
                .foregroundStyle(.white.opacity(0.9))
            Text(text)
                .font(.footnote.weight(.medium))
                .monospacedDigit()
                .foregroundStyle(.white)
                .contentTransition(.numericText())
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 5.5)
        .background(.white.opacity(0.16), in: Capsule())
    }

    private func sectionLabel(_ text: String) -> some View {
        EyebrowLabel(text)
            .padding(.horizontal, 14)
            .padding(.top, 18)
            .padding(.bottom, 6)
    }

    // MARK: Rows

    @ViewBuilder
    private var scopeRows: some View {
        row(index: 0, icon: "tray.2.fill", title: "All inboxes", count: store.allCount, selected: selection == .all) {
            onSelect(.all)
        }
        row(
            index: 1,
            icon: "envelope.badge.fill",
            title: "Unread",
            count: store.unreadCount,
            selected: selection == .unread,
            unreadCount: true
        ) {
            onSelect(.unread)
        }
        row(index: 2, icon: "sun.max.fill", title: "Today", count: store.todayCount, selected: selection == .today) {
            onSelect(.today)
        }
        row(index: 3, icon: "calendar", title: "Last 7 days", count: store.weekCount, selected: selection == .week) {
            onSelect(.week)
        }
        row(
            index: 4,
            icon: "arrowshape.turn.up.left.fill",
            title: "Awaiting reply",
            count: store.awaitingCount,
            selected: selection == .awaiting
        ) {
            onSelect(.awaiting)
        }
        row(index: 5, icon: "moon.zzz.fill", title: "Snoozed", count: store.snoozedCount, selected: selection == .snoozed) {
            onSelect(.snoozed)
        }
        row(index: 6, icon: "clock.badge", title: "Scheduled", count: store.scheduledCount, selected: false, chevron: true) {
            onScheduled()
        }
    }

    @ViewBuilder
    private var mailboxRows: some View {
        ForEach(Array(store.mailboxes.enumerated()), id: \.element.id) { offset, mailbox in
            let label = mailbox.name?.isEmpty == false ? (mailbox.name ?? "") : (mailbox.email ?? "Mailbox")
            let scope = UniboxScope.mailbox(id: mailbox.id, label: label)
            row(
                index: 7 + offset,
                icon: "envelope.fill",
                title: label,
                count: mailbox.unread ?? 0,
                selected: selection == scope,
                unreadCount: true
            ) {
                onSelect(scope)
            }
        }
    }

    @ViewBuilder
    private var labelRows: some View {
        ForEach(Array(labels.enumerated()), id: \.element.id) { offset, category in
            let label = category.title ?? "Label"
            let scope = UniboxScope.category(id: category.id, label: label, colorHex: category.color)
            row(
                index: 7 + store.mailboxes.count + offset,
                dot: Color(uniboxHex: category.color) ?? WTheme.accent,
                title: label,
                count: category.unread ?? 0,
                selected: selection == scope,
                unreadCount: true
            ) {
                onSelect(scope)
            }
        }
    }

    private func row(
        index: Int,
        icon: String? = nil,
        dot: Color? = nil,
        title: String,
        count: Int,
        selected: Bool,
        unreadCount: Bool = false,
        chevron: Bool = false,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            HStack(spacing: 13) {
                Group {
                    if let dot {
                        Circle()
                            .fill(dot)
                            .frame(width: 11, height: 11)
                    } else if let icon {
                        Image(systemName: icon)
                            .font(.system(size: 15, weight: .medium))
                            .foregroundStyle(selected ? WTheme.accent : Color.secondary)
                    }
                }
                .frame(width: 24)
                Text(title)
                    .font(.subheadline.weight(selected ? .semibold : .medium))
                    .foregroundStyle(selected ? WTheme.accent : Color.primary)
                    .lineLimit(1)
                Spacer(minLength: 8)
                if count > 0 {
                    Text(WFormat.compact(count))
                        .font(.footnote.weight(unreadCount || selected ? .semibold : .medium))
                        .monospacedDigit()
                        .foregroundStyle(
                            unreadCount ? WTheme.negative : (selected ? WTheme.accent : Color.secondary)
                        )
                        .contentTransition(.numericText())
                }
                if chevron {
                    Image(systemName: "chevron.right")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(.horizontal, 16)
            .frame(height: 44)
            .background {
                if selected {
                    Capsule()
                        .fill(Tone.sky.background)
                        .matchedGeometryEffect(id: "drawer-active", in: activeNS)
                }
            }
            .contentShape(Capsule())
        }
        .buttonStyle(TapScaleStyle())
        .opacity(revealed ? 1 : 0)
        .offset(x: revealed ? 0 : -18)
        // Cap the stagger so long mailbox/label lists don't cascade for seconds.
        .animation(
            .spring(response: 0.42, dampingFraction: 0.82)
                .delay(revealed ? 0.03 + min(Double(index), 14) * 0.022 : 0),
            value: revealed
        )
    }
}
