import SwiftUI

/// Every drawer's top: the workspace you are in, and a menu to switch it.
struct DrawerWorkspaceHeader: View {
    let topInset: CGFloat

    @Environment(AppEnvironment.self) private var env
    @State private var showWorkspaces = false

    private var orgName: String { env.session.currentOrg?.name ?? "Workspace" }

    var body: some View {
        Menu {
            ForEach(env.session.memberships) { membership in
                Button {
                    Task { try? await env.session.switchOrganization(membership.organizationID) }
                } label: {
                    if membership.organizationID == env.session.currentOrgID {
                        Label(membership.organization?.name ?? "Workspace", systemImage: "checkmark")
                    } else {
                        Text(membership.organization?.name ?? "Workspace")
                    }
                }
            }
            Divider()
            Button {
                showWorkspaces = true
            } label: {
                Label("Manage workspaces", systemImage: "square.stack")
            }
        } label: {
            HStack(spacing: 10) {
                WAvatar(
                    name: orgName,
                    imageURL: env.session.currentOrg?.avatarURL,
                    seed: env.session.currentOrgID ?? "org",
                    size: 30
                )
                Text(orgName)
                    .font(.system(size: 17, weight: .semibold))
                    .tracking(-0.2)
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                    .minimumScaleFactor(0.85)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(WTheme.inkTertiary)
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 14)
            .frame(height: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .padding(.horizontal, 12)
        .padding(.top, topInset + 8)
        .padding(.bottom, 2)
        .sheet(isPresented: $showWorkspaces) { MoreWorkspaceSwitcherSheet() }
    }
}
