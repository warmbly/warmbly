import SwiftUI

/// Navigation drawer for the analytics browser, mirroring the mailboxes /
/// contacts / campaigns sidebars: a slim sky hero with live totals, then the
/// analytics sections as pill rows on a rounded white sheet. The selected
/// pill slides between rows in the section's tone and rows cascade in when
/// the drawer opens.
struct AnalyticsSidebar: View {
    let store: AnalyticsStore
    let selection: AnalyticsScope
    let topInset: CGFloat
    let revealed: Bool
    let onSelect: (AnalyticsScope) -> Void

    @Namespace private var activeNS

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            hero
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    sectionLabel("Sections")
                    scopeRows
                }
                .padding(.horizontal, 12)
                .padding(.top, 8)
                .padding(.bottom, 40)
            }
            .background {
                UnevenRoundedRectangle(topLeadingRadius: 24, topTrailingRadius: 24, style: .continuous)
                    .fill(Color(.systemBackground))
                    .shadow(color: .black.opacity(0.1), radius: 14, y: -4)
            }
        }
        .background(alignment: .top) {
            AirSkyWash().frame(height: 340)
        }
        .background(Color(.systemBackground))
    }

    // MARK: Hero

    private var hero: some View {
        let sent = store.dashboard?.overallStats?.totalEmailsSent ?? 0
        return VStack(alignment: .leading, spacing: 13) {
            HStack(spacing: 8) {
                WarmblyLogo()
                    .fill(.white)
                    .frame(width: 21, height: 21 * (764 / 746))
                Text("Analytics")
                    .font(.system(size: 17.5, weight: .heavy))
                    .tracking(-0.4)
                    .foregroundStyle(.white)
            }
            Text("Sends, opens, replies and health")
                .font(.footnote.weight(.medium))
                .foregroundStyle(.white.opacity(0.82))
                .lineLimit(1)
            HStack(spacing: 6) {
                heroBadge(symbol: "paperplane.fill", text: "\(WFormat.compact(sent)) sent")
                if store.warmingCount > 0 {
                    heroBadge(symbol: "flame.fill", text: "\(WFormat.compact(store.warmingCount)) warming", live: true)
                } else if !store.accounts.isEmpty {
                    heroBadge(symbol: "envelope.fill", text: "\(WFormat.compact(store.accounts.count)) accounts")
                }
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, topInset + 12)
        .padding(.bottom, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func heroBadge(symbol: String, text: String, live: Bool = false) -> some View {
        HStack(spacing: 5) {
            Image(systemName: symbol)
                .font(.system(size: 10.5, weight: .semibold))
                .foregroundStyle(.white.opacity(0.9))
                .modifier(PingEffect(active: live, color: .white))
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
            .padding(.top, 14)
            .padding(.bottom, 6)
    }

    // MARK: Rows

    @ViewBuilder
    private var scopeRows: some View {
        ForEach(Array(AnalyticsScope.allCases.enumerated()), id: \.element) { index, scope in
            row(index: index, scope: scope)
        }
    }

    private func row(index: Int, scope: AnalyticsScope) -> some View {
        let selected = selection == scope
        // Only the accounts row carries a count; it turns rose when any
        // account has health issues, mirroring the sheet's counts pills.
        let count = scope == .accounts ? store.accounts.count : 0
        let countColor: Color? = scope == .accounts && store.issueCount > 0 ? Tone.rose.color : nil
        return Button {
            onSelect(scope)
        } label: {
            HStack(spacing: 13) {
                Image(systemName: scope.icon)
                    .font(.system(size: 15, weight: .medium))
                    .foregroundStyle(selected ? scope.tone.color : Color.secondary)
                    .frame(width: 24)
                Text(scope.title)
                    .font(.subheadline.weight(selected ? .semibold : .medium))
                    .foregroundStyle(selected ? scope.tone.color : Color.primary)
                    .lineLimit(1)
                Spacer(minLength: 8)
                if count > 0 {
                    Text(WFormat.compact(count))
                        .font(.footnote.weight(countColor != nil || selected ? .semibold : .medium))
                        .monospacedDigit()
                        .foregroundStyle(countColor ?? (selected ? scope.tone.color : Color.secondary))
                        .contentTransition(.numericText())
                }
            }
            .padding(.horizontal, 16)
            .frame(height: 44)
            .background {
                if selected {
                    Capsule()
                        .fill(scope.tone.background)
                        .matchedGeometryEffect(id: "analyticsdrawer-active", in: activeNS)
                }
            }
            .contentShape(Capsule())
        }
        .buttonStyle(TapScaleStyle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(count > 0 ? "\(scope.title), \(count)" : scope.title)
        .accessibilityAddTraits(selected ? .isSelected : [])
        .opacity(revealed ? 1 : 0)
        .offset(x: revealed ? 0 : -18)
        .animation(
            .spring(response: 0.42, dampingFraction: 0.82)
                .delay(revealed ? 0.03 + min(Double(index), 14) * 0.024 : 0),
            value: revealed
        )
    }
}
