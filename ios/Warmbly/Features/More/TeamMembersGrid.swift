import SwiftUI

/// Members on a wide window: counts, search and invite up top, then cards for members and invites.
struct TeamMembersGrid: View {
    let members: [TeamMember]
    let totalMembers: Int
    let invitations: [TeamInvitation]
    let onlineIDs: Set<String>
    let onlineCount: Int
    let meID: String?
    let canManage: Bool
    @Binding var search: String
    let canRemove: (TeamMember) -> Bool
    let onInvite: () -> Void
    let onRemove: (TeamMember) -> Void
    let onRevoke: (TeamInvitation) -> Void

    private let columns = [GridItem(.adaptive(minimum: 250, maximum: 400), spacing: 14, alignment: .top)]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                header
                membersSection
                if canManage, !invitations.isEmpty {
                    invitationsSection
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 14)
            .padding(.bottom, 80)
            .frame(maxWidth: 1180)
            .frame(maxWidth: .infinity)
        }
        .scrollIndicators(.hidden)
        .scrollDismissesKeyboard(.interactively)
        .background(GroupedSurface.page)
        .toolbarBackground(GroupedSurface.page, for: .navigationBar)
    }

    // MARK: Header

    private var header: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 14) {
                statsStrip
                Spacer(minLength: 12)
                searchField.frame(width: 280)
                inviteButton
            }
            VStack(alignment: .leading, spacing: 12) {
                statsStrip
                HStack(spacing: 12) {
                    searchField
                    inviteButton
                }
            }
        }
    }

    private var statsStrip: some View {
        HStack(spacing: 0) {
            stat(value: totalMembers, label: "Members", symbol: "person.2.fill", tone: .sky)
            divider
            stat(value: onlineCount, label: "Online now", symbol: "dot.radiowaves.left.and.right", tone: .emerald, live: true)
            if canManage {
                divider
                stat(value: invitations.count, label: "Invited", symbol: "envelope.badge.fill", tone: .amber, highlight: !invitations.isEmpty)
            }
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 6)
        .surfaceCard(in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .fixedSize()
    }

    private var divider: some View {
        Rectangle().fill(WTheme.hairline).frame(width: 1, height: 30)
    }

    private func stat(value: Int, label: String, symbol: String, tone: Tone, live: Bool = false, highlight: Bool = false) -> some View {
        HStack(spacing: 10) {
            Image(systemName: symbol)
                .font(.system(size: 12.5, weight: .semibold))
                .foregroundStyle(tone.color)
                .frame(width: 30, height: 30)
                .background(tone.background, in: Circle())
            VStack(alignment: .leading, spacing: 1) {
                HStack(spacing: 6) {
                    Text("\(value)")
                        .font(.system(size: 20, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(highlight ? WTheme.warning : WTheme.ink)
                        .contentTransition(.numericText())
                    if live, value > 0 {
                        LiveDot(color: WTheme.positive, size: 6, pulsing: true)
                    }
                }
                Text(label)
                    .font(.system(size: 12.5))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
            }
        }
        .padding(.horizontal, 14)
        .animation(.snappy, value: value)
    }

    private var searchField: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(WTheme.inkTertiary)
            TextField("Search by name or email", text: $search)
                .font(.system(size: 15))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.search)
            if !search.isEmpty {
                Button {
                    search = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(WTheme.inkTertiary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
            }
        }
        .padding(.horizontal, 12)
        .frame(height: 40)
        .surfaceCard(in: Capsule())
    }

    @ViewBuilder
    private var inviteButton: some View {
        if canManage {
            Button(action: onInvite) {
                HStack(spacing: 7) {
                    Image(systemName: "person.badge.plus")
                        .font(.system(size: 14, weight: .semibold))
                    Text("Invite")
                        .font(.system(size: 15, weight: .semibold))
                }
                .foregroundStyle(WTheme.inkInverse)
                .padding(.horizontal, 16)
                .frame(height: 40)
                .background(WTheme.ink, in: Capsule())
            }
            .buttonStyle(TapScaleStyle())
            .fixedSize()
            .accessibilityLabel("Invite member")
        }
    }

    // MARK: Members

    private var membersSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            CoverSectionHeader(title: search.isEmpty ? "Everyone" : "Matches", count: members.count)
            if members.isEmpty {
                Text("Nobody matches \u{201C}\(search)\u{201D}.")
                    .font(.system(size: 15))
                    .foregroundStyle(WTheme.inkSecondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 36)
                    .surfaceCard(in: RoundedRectangle(cornerRadius: 20, style: .continuous))
            } else {
                LazyVGrid(columns: columns, alignment: .leading, spacing: 14) {
                    ForEach(members) { member in
                        memberCard(member)
                    }
                }
            }
        }
        .animation(.snappy, value: members.map(\.id))
    }

    private func memberCard(_ member: TeamMember) -> some View {
        let online = onlineIDs.contains(member.userID)
        let removable = canRemove(member)
        return VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .top) {
                WAvatar(name: member.displayName, imageURL: member.user?.avatarURL, seed: member.userID, size: 50)
                    .overlay(alignment: .bottomTrailing) {
                        if online {
                            Circle()
                                .fill(WTheme.positive)
                                .frame(width: 14, height: 14)
                                .overlay(Circle().strokeBorder(GroupedSurface.card, lineWidth: 2.5))
                                .offset(x: 1, y: 1)
                                .transition(.scale.combined(with: .opacity))
                        }
                    }
                Spacer(minLength: 8)
                if removable {
                    Menu {
                        removeButton(member)
                    } label: {
                        cardMenuLabel
                    }
                    .accessibilityLabel("Actions for \(member.displayName)")
                }
            }
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(member.displayName)
                        .font(.system(size: 16, weight: .semibold))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                    if member.userID == meID {
                        Text("You")
                            .font(.system(size: 11.5, weight: .semibold))
                            .foregroundStyle(WTheme.inkSecondary)
                            .padding(.horizontal, 6)
                            .frame(height: 18)
                            .background(WTheme.fill, in: Capsule())
                    }
                }
                Text(member.displayEmail.isEmpty ? " " : member.displayEmail)
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
            }
            HStack(spacing: 8) {
                TeamMemberRoleChips(member: member)
                Spacer(minLength: 4)
                if online {
                    Text("Online")
                        .font(.system(size: 12, weight: .medium))
                        .foregroundStyle(WTheme.positive)
                        .transition(.opacity)
                }
            }
            .frame(height: 22)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard(in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: 20, style: .continuous))
        .contextMenu {
            if removable { removeButton(member) }
        }
        .animation(.snappy, value: online)
    }

    private func removeButton(_ member: TeamMember) -> some View {
        Button(role: .destructive) {
            onRemove(member)
        } label: {
            Label("Remove from workspace", systemImage: "person.badge.minus")
        }
    }

    private var cardMenuLabel: some View {
        Image(systemName: "ellipsis")
            .font(.system(size: 14, weight: .semibold))
            .foregroundStyle(WTheme.inkSecondary)
            .frame(width: 32, height: 32)
            .background(WTheme.fill, in: Circle())
            .contentShape(Circle())
    }

    // MARK: Invitations

    private var invitationsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            CoverSectionHeader(title: "Invited", count: invitations.count, color: WTheme.warning)
            LazyVGrid(columns: columns, alignment: .leading, spacing: 14) {
                ForEach(invitations) { invitation in
                    invitationCard(invitation)
                }
            }
            Text("Use an invitation's menu to revoke it before it is accepted.")
                .font(.system(size: 13))
                .foregroundStyle(WTheme.inkSecondary)
                .padding(.horizontal, 4)
        }
    }

    private func invitationCard(_ invitation: TeamInvitation) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .top) {
                Image(systemName: "envelope.badge.fill")
                    .font(.system(size: 19))
                    .foregroundStyle(Tone.amber.color)
                    .frame(width: 50, height: 50)
                    .background(Tone.amber.background, in: Circle())
                    .overlay(Circle().strokeBorder(style: StrokeStyle(lineWidth: 1, dash: [3, 3])).foregroundStyle(Tone.amber.color.opacity(0.4)))
                Spacer(minLength: 8)
                Menu {
                    revokeButton(invitation)
                } label: {
                    cardMenuLabel
                }
                .accessibilityLabel("Actions for invitation")
            }
            VStack(alignment: .leading, spacing: 3) {
                Text(invitation.email ?? "Invitation")
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                Text(invitation.expiryText)
                    .font(.system(size: 13))
                    .foregroundStyle(invitation.isExpired ? WTheme.negative : WTheme.inkSecondary)
                    .lineLimit(1)
            }
            HStack(spacing: 4) {
                ForEach((invitation.roles ?? []).prefix(2)) { chip in
                    MoreRoleChipView(name: chip.name ?? "Role", color: MoreStyle.color(hex: chip.color) ?? WTheme.paused)
                }
                Spacer(minLength: 0)
            }
            .frame(height: 22)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard(in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .contentShape(.contextMenuPreview, RoundedRectangle(cornerRadius: 20, style: .continuous))
        .contextMenu { revokeButton(invitation) }
    }

    private func revokeButton(_ invitation: TeamInvitation) -> some View {
        Button(role: .destructive) {
            onRevoke(invitation)
        } label: {
            Label("Revoke invitation", systemImage: "xmark.circle")
        }
    }
}

/// A member's role badges: the owner crown, up to two custom roles, or the plain role.
struct TeamMemberRoleChips: View {
    let member: TeamMember

    var body: some View {
        HStack(spacing: 4) {
            if member.isOwner {
                HStack(spacing: 4) {
                    Image(systemName: "crown.fill").font(.system(size: 9))
                    Text("Owner")
                }
                .font(.system(size: 11.5, weight: .semibold))
                .foregroundStyle(Tone.amber.color)
                .padding(.horizontal, 8)
                .frame(height: 22)
                .background(Tone.amber.background, in: Capsule())
            } else if let roles = member.roles, !roles.isEmpty {
                ForEach(roles.prefix(2)) { chip in
                    MoreRoleChipView(name: chip.name ?? "Role", color: MoreStyle.color(hex: chip.color) ?? WTheme.paused)
                }
                if roles.count > 2 {
                    Text("+\(roles.count - 2)")
                        .font(.system(size: 11, weight: .medium))
                        .foregroundStyle(WTheme.inkTertiary)
                }
            } else if let role = member.role, !role.isEmpty {
                MoreRoleChipView(name: role.capitalized, color: WTheme.paused)
            }
        }
    }
}

extension TeamInvitation {
    var isExpired: Bool {
        (expiresAt ?? .distantFuture) < Date()
    }

    var expiryText: String {
        guard let expires = expiresAt else { return "Waiting to accept" }
        if expires < Date() { return "Expired" }
        return "Expires \(WFormat.relative(expires))"
    }
}
