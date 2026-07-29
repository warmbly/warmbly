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

/// Workspace roster: search pill up top, dense flat member rows with role
/// pills, pending invitations, invite sheet. Visible to every member; writes
/// gated on manage_team.
struct TeamMembersView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = MoreTeamStore()
    @State private var search = ""
    @State private var showInvite = false
    @State private var memberPendingRemoval: TeamMember?
    @State private var actionError: String?
    @FocusState private var searchFocused: Bool

    private var canManage: Bool { env.session.can(.manageTeam) }

    private var filteredMembers: [TeamMember] {
        let q = search.trimmingCharacters(in: .whitespaces).lowercased()
        guard !q.isEmpty else { return store.members }
        return store.members.filter {
            $0.displayName.lowercased().contains(q) || $0.displayEmail.lowercased().contains(q)
        }
    }

    var body: some View {
        content
            .navigationTitle("Members")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if canManage {
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
            .task { await store.load(env.api, canManage: canManage) }
            .onChange(of: env.realtime.pulse(for: .team)) {
                Task { await store.load(env.api, canManage: canManage) }
            }
            .sheet(isPresented: $showInvite) {
                MoreInviteMemberSheet(store: store)
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
        } else {
            VStack(spacing: 0) {
                searchBar
                list
            }
            .background(Color(.systemBackground))
        }
    }

    // MARK: Search pill

    private var searchBar: some View {
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(.secondary)
                .padding(.leading, 14)
            TextField("Search members", text: $search)
                .font(.subheadline)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.search)
                .focused($searchFocused)
            if !search.isEmpty {
                Button {
                    search = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 16))
                        .foregroundStyle(.tertiary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
            }
            PresenceAvatars()
                .padding(.trailing, 8)
        }
        .frame(height: 44)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 22, style: .continuous))
        .padding(.horizontal, 12)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }

    // MARK: List

    private var list: some View {
        List {
            statsRow
            membersSection
            if canManage, !store.invitations.isEmpty {
                invitationsSection
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .environment(\.defaultMinListRowHeight, 0)
        .refreshable { await store.load(env.api, canManage: canManage) }
    }

    private var statsRow: some View {
        HStack(spacing: 12) {
            StatCell(label: "Members", value: "\(store.members.count)")
            Divider().frame(height: 32)
            StatCell(label: "Online", value: "\(env.realtime.presence.onlineCount)", tone: .emerald)
            if canManage {
                Divider().frame(height: 32)
                StatCell(
                    label: "Pending",
                    value: "\(store.invitations.count)",
                    tone: store.invitations.isEmpty ? nil : .amber
                )
            }
        }
        .padding(.top, 8)
        .padding(.bottom, 4)
        .moreFlatRow(separator: .hidden)
    }

    @ViewBuilder
    private var membersSection: some View {
        MoreFlatSectionHeader("Members", top: 12)
        if filteredMembers.isEmpty {
            EmptyStateView(title: "No matches", message: "No member matches that search.")
                .moreFlatRow(separator: .hidden)
        }
        ForEach(filteredMembers) { member in
            memberRow(member)
                .moreFlatRow(textLeading: MoreFlatMetrics.avatarTextLeading)
                .swipeActions(edge: .trailing) {
                    if canRemove(member) {
                        Button(role: .destructive) {
                            memberPendingRemoval = member
                        } label: {
                            Label("Remove", systemImage: "person.badge.minus")
                        }
                    }
                }
        }
    }

    private func canRemove(_ member: TeamMember) -> Bool {
        canManage && !member.isOwner && member.userID != env.session.user?.id
    }

    private func memberRow(_ member: TeamMember) -> some View {
        HStack(spacing: 12) {
            WAvatar(
                name: member.displayName,
                imageURL: member.user?.avatarURL,
                seed: member.userID,
                size: 40
            )
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 4) {
                    Text(member.displayName)
                        .font(.body.weight(.medium))
                        .lineLimit(1)
                    if member.isOwner {
                        Image(systemName: "crown.fill")
                            .font(.caption2)
                            .foregroundStyle(Tone.amber.color)
                    }
                    if member.userID == env.session.user?.id {
                        Text("you")
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                }
                Text(member.displayEmail)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            roleChips(member)
        }
        .padding(.vertical, 9)
    }

    @ViewBuilder
    private func roleChips(_ member: TeamMember) -> some View {
        HStack(spacing: 4) {
            if member.isOwner {
                MoreRoleChipView(name: "Owner", color: Tone.amber.color)
            } else if let roles = member.roles, !roles.isEmpty {
                ForEach(roles.prefix(2)) { chip in
                    MoreRoleChipView(
                        name: chip.name ?? "Role",
                        color: MoreStyle.color(hex: chip.color) ?? WTheme.paused
                    )
                }
                if roles.count > 2 {
                    Text("+\(roles.count - 2)")
                        .font(.system(size: 10, weight: .medium))
                        .foregroundStyle(.secondary)
                }
            } else if let role = member.role, !role.isEmpty {
                MoreRoleChipView(name: role, color: WTheme.paused)
            }
        }
    }

    @ViewBuilder
    private var invitationsSection: some View {
        MoreFlatSectionHeader("Pending invitations")
        ForEach(store.invitations) { invitation in
            invitationRow(invitation)
                .moreFlatRow(textLeading: MoreFlatMetrics.tileTextLeading)
                .swipeActions(edge: .trailing) {
                    Button(role: .destructive) {
                        Task { await revoke(invitation) }
                    } label: {
                        Label("Revoke", systemImage: "xmark.circle")
                    }
                }
        }
        Text("Swipe an invitation to revoke it.")
            .font(.footnote)
            .foregroundStyle(.secondary)
            .padding(.top, 8)
            .padding(.bottom, 28)
            .moreFlatRow(separator: .hidden)
    }

    private func invitationRow(_ invitation: TeamInvitation) -> some View {
        HStack(spacing: 12) {
            IconTile(symbol: "envelope.badge.fill", tone: .amber, size: 34)
            VStack(alignment: .leading, spacing: 2) {
                Text(invitation.email ?? "Invitation")
                    .font(.body.weight(.medium))
                    .lineLimit(1)
                Text(expiryText(invitation))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            HStack(spacing: 4) {
                ForEach((invitation.roles ?? []).prefix(2)) { chip in
                    MoreRoleChipView(
                        name: chip.name ?? "Role",
                        color: MoreStyle.color(hex: chip.color) ?? WTheme.paused
                    )
                }
            }
        }
        .padding(.vertical, 9)
    }

    private func expiryText(_ invitation: TeamInvitation) -> String {
        guard let expires = invitation.expiresAt else { return "Pending" }
        if expires < Date() { return "Expired" }
        return "Expires \(WFormat.relative(expires))"
    }

    private func remove(_ member: TeamMember) async {
        do {
            try await store.removeMember(env.api, userID: member.userID)
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

    private var emailValid: Bool {
        let trimmed = email.trimmingCharacters(in: .whitespaces)
        return trimmed.contains("@") && trimmed.contains(".") && trimmed.count >= 5
    }

    var body: some View {
        NavigationStack {
            List {
                MoreFlatSectionHeader("Invite by email", top: 8)
                TextField("name@company.com", text: $email)
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .padding(.vertical, 12)
                    .moreFlatRow()
                MoreFlatSectionHeader("Roles")
                if store.roles.isEmpty {
                    Text("No roles in this workspace yet.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .padding(.vertical, 8)
                        .moreFlatRow(separator: .hidden)
                }
                ForEach(store.roles) { role in
                    roleRow(role)
                        .moreFlatRow()
                }
                Text("You can only grant permissions you hold yourself.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .padding(.top, 8)
                    .padding(.bottom, 28)
                    .moreFlatRow(separator: .hidden)
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .background(Color(.systemBackground))
            .environment(\.defaultMinListRowHeight, 0)
            .navigationTitle("Invite member")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if sending {
                        ProgressView().controlSize(.small)
                    } else {
                        Button("Send") { send() }
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
    }

    private func roleRow(_ role: TeamRole) -> some View {
        let selected = selectedRoleIDs.contains(role.id)
        return Button {
            if selected {
                selectedRoleIDs.remove(role.id)
            } else {
                selectedRoleIDs.insert(role.id)
            }
        } label: {
            HStack(spacing: 10) {
                Circle()
                    .fill(MoreStyle.color(hex: role.color) ?? WTheme.paused)
                    .frame(width: 8, height: 8)
                VStack(alignment: .leading, spacing: 2) {
                    Text(role.name ?? "Role")
                        .font(.body.weight(.medium))
                        .foregroundStyle(.primary)
                    if let description = role.description, !description.isEmpty {
                        Text(description)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
                Spacer()
                if let count = role.memberCount {
                    Text("\(count)")
                        .font(.footnote)
                        .monospacedDigit()
                        .foregroundStyle(.tertiary)
                }
                Image(systemName: selected ? "checkmark.square.fill" : "square")
                    .font(.system(size: 18))
                    .foregroundStyle(selected ? WTheme.accent : Color.secondary)
            }
            .padding(.vertical, 10)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
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
