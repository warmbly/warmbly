import SwiftUI

/// Navigation drawer for the campaign Leads browser, mirroring the contacts /
/// campaigns / unibox sidebars: a slim sky hero with the campaign name and live
/// lead totals, an "Add leads" action, then the status scopes as pill rows on a
/// rounded white sheet. Counts come from the search `lead_counts` block. The
/// selected pill slides between rows and rows cascade in when the drawer opens.
struct CampaignLeadsSidebar: View {
    let store: CampaignLeadsStore
    let campaignName: String
    let selection: CampaignLeadScope
    let topInset: CGFloat
    let revealed: Bool
    let onSelect: (CampaignLeadScope) -> Void

    @Namespace private var activeNS

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            hero
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    sectionLabel("Status")
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
            AirSkyWash().frame(height: 360)
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
                Text("Leads")
                    .font(.system(size: 17.5, weight: .heavy))
                    .tracking(-0.4)
                    .foregroundStyle(.white)
            }
            Text(campaignName)
                .font(.footnote.weight(.medium))
                .foregroundStyle(.white.opacity(0.82))
                .lineLimit(1)
            HStack(spacing: 6) {
                heroBadge(symbol: "person.2.fill", text: "\(WFormat.compact(store.scopeCount(.all))) leads")
                let processing = store.scopeCount(.processing)
                if processing > 0 {
                    heroBadge(symbol: "paperplane.fill", text: "\(WFormat.compact(processing)) sending", live: true)
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
        ForEach(Array(CampaignLeadScope.allCases.enumerated()), id: \.element) { index, scope in
            row(index: index, scope: scope)
        }
    }

    private func row(index: Int, scope: CampaignLeadScope) -> some View {
        let selected = selection == scope
        let count = store.scopeCount(scope)
        let live = scope == .processing && count > 0
        return Button {
            onSelect(scope)
        } label: {
            HStack(spacing: 13) {
                Group {
                    if live {
                        Circle()
                            .fill(scope.tone.color)
                            .frame(width: 11, height: 11)
                            .modifier(PingEffect(active: true, color: scope.tone.color))
                    } else {
                        Image(systemName: scope.icon)
                            .font(.system(size: 15, weight: .medium))
                            .foregroundStyle(selected ? WTheme.accent : Color.secondary)
                    }
                }
                .frame(width: 24)
                Text(scope.title)
                    .font(.subheadline.weight(selected ? .semibold : .medium))
                    .foregroundStyle(selected ? WTheme.accent : Color.primary)
                    .lineLimit(1)
                Spacer(minLength: 8)
                if count > 0 {
                    Text(WFormat.compact(count))
                        .font(.footnote.weight(selected ? .semibold : .medium))
                        .monospacedDigit()
                        .foregroundStyle(selected ? WTheme.accent : Color.secondary)
                        .contentTransition(.numericText())
                }
            }
            .padding(.horizontal, 16)
            .frame(height: 44)
            .background {
                if selected {
                    Capsule()
                        .fill(Tone.sky.background)
                        .matchedGeometryEffect(id: "leaddrawer-active", in: activeNS)
                }
            }
            .contentShape(Capsule())
        }
        .buttonStyle(TapScaleStyle())
        .opacity(revealed ? 1 : 0)
        .offset(x: revealed ? 0 : -18)
        .animation(
            .spring(response: 0.42, dampingFraction: 0.82)
                .delay(revealed ? 0.03 + min(Double(index), 14) * 0.024 : 0),
            value: revealed
        )
    }
}
