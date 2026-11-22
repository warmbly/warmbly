import SwiftUI

@MainActor
@Observable
final class MoreTeamStore {
    var members: [TeamMember] = []
    var invitations: [TeamInvitation] = []
    var roles: [TeamRole] = []
    var loadedOnce = false
    var isLoading = false
    var errorMessage: String?

    func load(_ api: APIClient, canManage: Bool) async {
        guard !isLoading else { return }
        isLoading = true
        errorMessage = nil
        do {
            let page: ListResponse<TeamMember> = try await api.get("organization/members")
            members = page.data
            if canManage {
                // Invitations require manage_team; roles power the invite sheet.
                let invites: ListResponse<TeamInvitation>? = try? await api.get("organization/invitations")
                invitations = invites?.data ?? []
                let rolePage: ListResponse<TeamRole>? = try? await api.get("organization/roles")
                roles = rolePage?.data ?? []
            }
            loadedOnce = true
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    func invite(_ api: APIClient, email: String, roleIDs: [String]) async throws {
        let _: MoreInviteResponse = try await api.post(
            "organization/members/invite",
            body: MoreInviteBody(email: email, roleIDs: roleIDs)
        )
        let invites: ListResponse<TeamInvitation>? = try? await api.get("organization/invitations")
        if let invites { invitations = invites.data }
    }

    func removeMember(_ api: APIClient, userID: String) async throws {
        let _: MessageResponse = try await api.delete("organization/members/\(userID)")
        members.removeAll { $0.userID == userID }
    }

    func revokeInvitation(_ api: APIClient, id: String) async throws {
        let _: MessageResponse = try await api.delete("organization/invitations/\(id)")
        invitations.removeAll { $0.id == id }
    }
}

/// Who is in the workspace: how many, who is here right now and who is still
/// invited, searchable, with the invite one tap away for those who can.
struct TeamMembersView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = MoreTeamStore()
    @State private var search = ""
    @State private var showInvite = false
    @State private var memberPendingRemoval: TeamMember?
    @State private var memberAwaitingReauth: TeamMember?
    @State private var actionError: String?
    @State private var wide = false

    private var canManage: Bool { env.session.can(.manageTeam) }

    private var onlineIDs: Set<String> { Set(env.realtime.presence.online.map(\.id)) }

    private var filteredMembers: [TeamMember] {
        let q = search.trimmingCharacters(in: .whitespaces).lowercased()
        let online = onlineIDs
        let me = env.session.user?.id
        let sorted = store.members.sorted { a, b in
            // You first, then whoever is online, then by name.
            if (a.userID == me) != (b.userID == me) { return a.userID == me }
            let ao = online.contains(a.userID), bo = online.contains(b.userID)
            if ao != bo { return ao }
            return a.displayName.localizedCaseInsensitiveCompare(b.displayName) == .orderedAscending
        }
        guard !q.isEmpty else { return sorted }
        return sorted.filter {
            $0.displayName.lowercased().contains(q) || $0.displayEmail.lowercased().contains(q)
        }
    }

    var body: some View {
        content
            .navigationTitle("Members")
            .surfaceTitleDisplayMode()
            .toolbar {
                // The wide grid carries its own invite button.
                if canManage, !wide {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            showInvite = true
                        } label: {
                            Image(systemName: "person.badge.plus")
                        }
                        .accessibilityLabel("Invite member")
                    }
                }
            }
            .onGeometryChange(for: Bool.self) { $0.size.width >= SplitMetrics.detailMinContentWidth } action: { wide = $0 }
            .task { await store.load(env.api, canManage: canManage) }
            .onChange(of: env.realtime.pulse(for: .team)) {
                Task { await store.load(env.api, canManage: canManage) }
            }
            .sheet(isPresented: $showInvite) {
                MoreInviteMemberSheet(store: store)
            }
            .sheet(item: $memberAwaitingReauth) { member in
                ReauthSheet(
                    reason: "Removing a member needs a recent sign-in.",
                    actionTitle: "Confirm and remove"
                ) {
                    Task { await remove(member) }
                }
            }
            .confirmationDialog(
                "Remove member",
                isPresented: Binding(
                    get: { memberPendingRemoval != nil },
                    set: { if !$0 { memberPendingRemoval = nil } }
                ),
                titleVisibility: .visible,
                presenting: memberPendingRemoval
            ) { member in
                Button("Remove \(member.displayName)", role: .destructive) {
                    Task { await remove(member) }
                }
                Button("Cancel", role: .cancel) {}
            } message: { member in
                Text("\(member.displayEmail) loses access to this workspace immediately.")
            }
            .alert(
                "Something went wrong",
                isPresented: Binding(
                    get: { actionError != nil },
                    set: { if !$0 { actionError = nil } }
                )
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actionError ?? "")
            }
    }

    @ViewBuilder
    private var content: some View {
        if !store.loadedOnce {
            if let message = store.errorMessage {
                ErrorStateView(title: "Couldn't load members", message: message) {
                    await store.load(env.api, canManage: canManage)
                }
            } else {
                SkeletonRows()
            }
        } else if wide {
            TeamMembersGrid(
                members: filteredMembers,
                totalMembers: store.members.count,
                invitations: store.invitations,
                onlineIDs: onlineIDs,
                onlineCount: env.realtime.presence.onlineCount,
                meID: env.session.user?.id,
                canManage: canManage,
                search: $search,
                canRemove: canRemove,
                onInvite: { showInvite = true },
                onRemove: { memberPendingRemoval = $0 },
                onRevoke: { invitation in Task { await revoke(invitation) } }
            )
            .refreshable { await store.load(env.api, canManage: canManage) }
        } else {
            SettingsPage(title: "Members", spacing: 22) {
                stats
                searchField
                membersSection
                if canManage, !store.invitations.isEmpty {
                    invitationsSection
                }
            }
            .refreshable { await store.load(env.api, canManage: canManage) }
        }
    }

    // MARK: Stats

    private var stats: some View {
        HStack(spacing: 0) {
            stat(value: store.members.count, label: "Members", color: WTheme.ink)
            Rectangle().fill(WTheme.hairline).frame(width: 1, height: 36)
            stat(value: env.realtime.presence.onlineCount, label: "Online now", color: WTheme.positive, live: true)
            if canManage {
                Rectangle().fill(WTheme.hairline).frame(width: 1, height: 36)
                stat(value: store.invitations.count, label: "Invited", color: store.invitations.isEmpty ? WTheme.ink : WTheme.warning)
            }
        }
        .padding(.vertical, 16)
        .surfaceCard(in: RoundedRectangle(cornerRadius: 22, style: .continuous))
    }

    private func stat(value: Int, label: String, color: Color, live: Bool = false) -> some View {
        VStack(spacing: 3) {
            HStack(spacing: 6) {
                if live, value > 0 {
                    LiveDot(color: WTheme.positive, size: 7, pulsing: true)
                }
                Text("\(value)")
                    .font(.system(size: 26, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(color)
                    .contentTransition(.numericText())
            }
            Text(label)
                .font(.system(size: 12.5))
                .foregroundStyle(WTheme.inkSecondary)
        }
        .frame(maxWidth: .infinity)
        .animation(.snappy, value: value)
    }

    private var searchField: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(WTheme.inkTertiary)
            TextField("Search by name or email", text: $search)
                .font(.system(size: 16))
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
        .surfaceCard(in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    // MARK: Members

    private var membersSection: some View {
        SettingsSection(search.isEmpty ? "Everyone" : "\(filteredMembers.count) found", dividerLeading: 70) {
            if canManage, search.isEmpty {
                Button {
                    showInvite = true
                } label: {
                    HStack(spacing: 12) {
                        Image(systemName: "plus")
                            .font(.system(size: 16, weight: .semibold))
                            .foregroundStyle(WTheme.accent)
                            .frame(width: 42, height: 42)
                            .background(WTheme.accentSoft, in: Circle())
                        Text("Invite a teammate")
                            .font(.system(size: 16, weight: .medium))
                            .foregroundStyle(WTheme.accent)
                        Spacer()
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 10)
                    .contentShape(Rectangle())
                }
                .buttonStyle(MoreRowPressStyle())
            }
            if filteredMembers.isEmpty {
                Text("Nobody matches \u{201C}\(search)\u{201D}.")
                    .font(.system(size: 15))
                    .foregroundStyle(WTheme.inkSecondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 22)
            }
            ForEach(filteredMembers) { member in
                if canRemove(member) {
                    Menu {
                        Button(role: .destructive) {
                            memberPendingRemoval = member
                        } label: {
                            Label("Remove from workspace", systemImage: "person.badge.minus")
                        }
                    } label: {
                        memberRow(member)
                    }
                    .buttonStyle(MoreRowPressStyle())
                } else {
                    memberRow(member)
                }
            }
        }
        .animation(.snappy, value: filteredMembers.map(\.id))
    }

    private func canRemove(_ member: TeamMember) -> Bool {
        canManage && !member.isOwner && member.userID != env.session.user?.id
    }

    private func memberRow(_ member: TeamMember) -> some View {
        let online = onlineIDs.contains(member.userID)
        return HStack(spacing: 12) {
            WAvatar(name: member.displayName, imageURL: member.user?.avatarURL, seed: member.userID, size: 42)
                .overlay(alignment: .bottomTrailing) {
                    if online {
                        Circle()
                            .fill(WTheme.positive)
                            .frame(width: 12, height: 12)
                            .overlay(Circle().strokeBorder(GroupedSurface.card, lineWidth: 2.5))
                            .offset(x: 1, y: 1)
                            .transition(.scale.combined(with: .opacity))
                    }
                }
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 5) {
                    Text(member.displayName)
                        .font(.system(size: 16))
                        .foregroundStyle(WTheme.ink)
                        .lineLimit(1)
                    if member.userID == env.session.user?.id {
                        Text("You")
                            .font(.system(size: 11.5, weight: .semibold))
                            .foregroundStyle(WTheme.inkSecondary)
                            .padding(.horizontal, 6)
                            .frame(height: 18)
                            .background(WTheme.fill, in: Capsule())
                    }
                }
                Text(member.displayEmail)
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            TeamMemberRoleChips(member: member)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .contentShape(Rectangle())
        .animation(.snappy, value: online)
    }

    // MARK: Invitations

    private var invitationsSection: some View {
        SettingsSection("Invited", footer: "Tap an invitation to revoke it before it is accepted.", dividerLeading: 70) {
            ForEach(store.invitations) { invitation in
                Menu {
                    Button(role: .destructive) {
                        Task { await revoke(invitation) }
                    } label: {
                        Label("Revoke invitation", systemImage: "xmark.circle")
                    }
                } label: {
                    invitationRow(invitation)
                }
                .buttonStyle(MoreRowPressStyle())
            }
        }
    }

    private func invitationRow(_ invitation: TeamInvitation) -> some View {
        HStack(spacing: 12) {
            Image(systemName: "envelope.badge.fill")
                .font(.system(size: 17))
                .foregroundStyle(Tone.amber.color)
                .frame(width: 42, height: 42)
                .background(Tone.amber.background, in: Circle())
                .overlay(Circle().strokeBorder(style: StrokeStyle(lineWidth: 1, dash: [3, 3])).foregroundStyle(Tone.amber.color.opacity(0.4)))
            VStack(alignment: .leading, spacing: 2) {
                Text(invitation.email ?? "Invitation")
                    .font(.system(size: 16))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                Text(invitation.expiryText)
                    .font(.system(size: 13))
                    .foregroundStyle(invitation.isExpired ? WTheme.negative : WTheme.inkSecondary)
            }
            Spacer(minLength: 8)
            HStack(spacing: 4) {
                ForEach((invitation.roles ?? []).prefix(2)) { chip in
                    MoreRoleChipView(name: chip.name ?? "Role", color: MoreStyle.color(hex: chip.color) ?? WTheme.paused)
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .contentShape(Rectangle())
    }

    private func remove(_ member: TeamMember) async {
        do {
            try await store.removeMember(env.api, userID: member.userID)
        } catch let error as APIError where error.code == "reauth_required" {
            memberAwaitingReauth = member
        } catch {
            actionError = error.localizedDescription
        }
    }

    private func revoke(_ invitation: TeamInvitation) async {
        do {
            try await store.revokeInvitation(env.api, id: invitation.id)
        } catch {
            actionError = error.localizedDescription
        }
    }
}

// MARK: - Invite sheet

struct MoreInviteMemberSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    let store: MoreTeamStore

    @State private var email = ""
    @State private var selectedRoleIDs: Set<String> = []
    @State private var sending = false
    @State private var errorMessage: String?
    @FocusState private var emailFocused: Bool

    private var emailValid: Bool {
        let trimmed = email.trimmingCharacters(in: .whitespaces)
        return trimmed.contains("@") && trimmed.contains(".") && trimmed.count >= 5
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 26) {
                    SettingsSection("Email", footer: "They get a link to join this workspace.", dividerLeading: 16) {
                        HStack(spacing: 10) {
                            Image(systemName: "at")
                                .font(.system(size: 15, weight: .medium))
                                .foregroundStyle(WTheme.inkTertiary)
                            TextField("name@company.com", text: $email)
                                .font(.system(size: 16))
                                .keyboardType(.emailAddress)
                                .textContentType(.emailAddress)
                                .textInputAutocapitalization(.never)
                                .autocorrectionDisabled()
                                .focused($emailFocused)
                                .submitLabel(.send)
                                .onSubmit { if emailValid { send() } }
                            if emailValid {
                                Image(systemName: "checkmark.circle.fill")
                                    .foregroundStyle(WTheme.positive)
                                    .transition(.scale.combined(with: .opacity))
                            }
                        }
                        .padding(.horizontal, 16)
                        .frame(height: 52)
                        .animation(.snappy, value: emailValid)
                    }
                    SettingsSection(
                        "Roles",
                        footer: "You can only grant permissions you hold yourself.",
                        dividerLeading: 40
                    ) {
                        if store.roles.isEmpty {
                            Text("No roles in this workspace yet.")
                                .font(.system(size: 15))
                                .foregroundStyle(WTheme.inkSecondary)
                                .padding(16)
                        }
                        ForEach(store.roles) { role in
                            roleRow(role)
                        }
                    }
                }
                .padding(.horizontal, 16)
                .padding(.top, 8)
                .padding(.bottom, 32)
            }
            .background(GroupedSurface.page)
            .navigationTitle("Invite a teammate")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(GroupedSurface.page, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if sending {
                        ProgressView().controlSize(.small)
                    } else {
                        Button("Send") { send() }
                            .fontWeight(.semibold)
                            .disabled(!emailValid)
                    }
                }
            }
            .alert(
                "Couldn't send invitation",
                isPresented: Binding(
                    get: { errorMessage != nil },
                    set: { if !$0 { errorMessage = nil } }
                )
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(errorMessage ?? "")
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .presentationBackground(GroupedSurface.page)
        .onAppear { emailFocused = true }
    }

    private func roleRow(_ role: TeamRole) -> some View {
        let selected = selectedRoleIDs.contains(role.id)
        return Button {
            withAnimation(.snappy) {
                if selected {
                    selectedRoleIDs.remove(role.id)
                } else {
                    selectedRoleIDs.insert(role.id)
                }
            }
        } label: {
            HStack(spacing: 12) {
                Circle()
                    .fill(MoreStyle.color(hex: role.color) ?? WTheme.paused)
                    .frame(width: 12, height: 12)
                VStack(alignment: .leading, spacing: 2) {
                    Text(role.name ?? "Role")
                        .font(.system(size: 16))
                        .foregroundStyle(WTheme.ink)
                    if let description = role.description, !description.isEmpty {
                        Text(description)
                            .font(.system(size: 13))
                            .foregroundStyle(WTheme.inkSecondary)
                            .lineLimit(2)
                    }
                }
                Spacer(minLength: 8)
                if let count = role.memberCount {
                    Text("\(count)")
                        .font(.system(size: 14))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.inkTertiary)
                }
                Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                    .font(.system(size: 22))
                    .foregroundStyle(selected ? WTheme.accent : WTheme.inkTertiary.opacity(0.6))
                    .contentTransition(.symbolEffect(.replace))
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 11)
            .contentShape(Rectangle())
        }
        .buttonStyle(MoreRowPressStyle())
        .sensoryFeedback(.selection, trigger: selected)
    }

    private func send() {
        guard !sending else { return }
        sending = true
        Task {
            do {
                try await store.invite(
                    env.api,
                    email: email.trimmingCharacters(in: .whitespaces),
                    roleIDs: Array(selectedRoleIDs)
                )
                dismiss()
            } catch {
                errorMessage = error.localizedDescription
            }
            sending = false
        }
    }
}
