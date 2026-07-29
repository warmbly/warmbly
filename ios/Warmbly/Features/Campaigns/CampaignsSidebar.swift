import SwiftUI

/// Navigation drawer for the campaigns browser, mirroring the unibox sidebar:
/// a slim sky hero with live badges, then status scopes and folders as pill
/// rows on a rounded white sheet. Counts come from `GET /campaigns-overview`;
/// folders from the session user. The selected pill slides between rows and
/// rows cascade in when the drawer opens. Presented full-bleed by
/// `CampaignsRootView`.
struct CampaignsSidebar: View {
    let store: CampaignsStore
    /// The member's campaign folders (session), in position order.
    let folders: [UserGroup]
    let selection: CampaignListScope
    let topInset: CGFloat
    let revealed: Bool
    let onSelect: (CampaignListScope) -> Void

    @Namespace private var activeNS

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            hero
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    scopeRows
                    if !folders.isEmpty {
                        sectionLabel("Folders")
                        folderRows
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
                Text("Campaigns")
                    .font(.system(size: 17.5, weight: .heavy))
                    .tracking(-0.4)
                    .foregroundStyle(.white)
            }
            HStack(spacing: 6) {
                heroBadge(symbol: "rectangle.stack.fill", text: "\(WFormat.compact(store.allCount)) total")
                heroBadge(symbol: "paperplane.fill", text: "\(WFormat.compact(store.runningCount)) sending")
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
        row(index: 0, icon: "rectangle.stack.fill", title: "All campaigns", count: store.allCount, selected: selection == .all) {
            onSelect(.all)
        }
        row(
            index: 1,
            icon: "paperplane.fill",
            title: "Sending",
            count: store.runningCount,
            selected: selection == .running,
            countColor: WTheme.positive
        ) {
            onSelect(.running)
        }
        row(
            index: 2,
            icon: "pause.circle.fill",
            title: "Paused",
            count: store.pausedCount,
            selected: selection == .paused,
            countColor: WTheme.warning
        ) {
            onSelect(.paused)
        }
        row(index: 3, icon: "doc.text.fill", title: "Drafts", count: store.draftCount, selected: selection == .draft) {
            onSelect(.draft)
        }
        row(index: 4, icon: "checkmark.circle.fill", title: "Finished", count: store.finishedCount, selected: selection == .finished) {
            onSelect(.finished)
        }
    }

    @ViewBuilder
    private var folderRows: some View {
        ForEach(Array(folders.enumerated()), id: \.element.id) { offset, folder in
            let label = folder.name ?? "Folder"
            let scope = CampaignListScope.folder(id: folder.id, label: label)
            row(
                index: 5 + offset,
                icon: "folder.fill",
                dot: Color(uniboxHex: folder.color),
                title: label,
                count: store.folderCount(folder.id),
                selected: selection == scope
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
        countColor: Color? = nil,
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
                        .font(.footnote.weight(countColor != nil || selected ? .semibold : .medium))
                        .monospacedDigit()
                        .foregroundStyle(countColor ?? (selected ? WTheme.accent : Color.secondary))
                        .contentTransition(.numericText())
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
        // Cap the stagger so a long folder list doesn't cascade for seconds.
        .animation(
            .spring(response: 0.42, dampingFraction: 0.82)
                .delay(revealed ? 0.03 + min(Double(index), 14) * 0.022 : 0),
            value: revealed
        )
    }
}
