import SwiftUI

/// Shown when the session has no auto-resolvable workspace. Zero orgs renders
/// the workspace-creation onboarding directly; several render as a sky screen
/// (matching sign-in and onboarding) with the workspaces on a full-bleed white
/// sheet and a path into the same creation flow.
struct OrgPickerView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var busyOrgID: String?
    @State private var errorMessage: String?
    @State private var showCreate = false
    @State private var badgeAppeared = false

    var body: some View {
        Group {
            if env.session.memberships.isEmpty {
                // First workspace: creating it IS the onboarding.
                WorkspaceCreateFlow(context: .gate)
            } else {
                picker
            }
        }
    }

    // MARK: - Picker (one or more workspaces)

    private var picker: some View {
        ZStack(alignment: .bottom) {
            SkyBackdrop()
            VStack(spacing: 0) {
                topBar
                skyArea
                sheet
            }
        }
        .fullScreenCover(isPresented: $showCreate) {
            WorkspaceCreateFlow(context: .cover, onClose: { showCreate = false })
        }
    }

    private var topBar: some View {
        HStack(spacing: 12) {
            HStack(spacing: 8) {
                WarmblyLogo()
                    .fill(.white)
                    .frame(width: 27, height: 28)
                Text("Warmbly")
                    .font(.system(size: 20, weight: .heavy))
                    .tracking(-0.4)
                    .foregroundStyle(.white)
                    .fixedSize()
            }
            .shadow(color: Color(hex: 0x0C4A6E).opacity(0.25), radius: 8, y: 3)

            Spacer()

            Button("Sign out") {
                Task { await env.session.logout() }
            }
            .font(.system(size: 13.5, weight: .semibold))
            .foregroundStyle(.white.opacity(0.9))
            .padding(.horizontal, 14)
            .frame(height: 34)
            .background(.white.opacity(0.16), in: Capsule())
            .buttonStyle(PressableButtonStyle())
        }
        .padding(.horizontal, 16)
        .padding(.top, 6)
    }

    private var skyArea: some View {
        ZStack {
            HeroFlightScene()

            VStack(spacing: 12) {
                ZStack {
                    Circle().fill(.white.opacity(0.16))
                    Circle().strokeBorder(.white.opacity(0.3), lineWidth: 1)
                    Image(systemName: "person.2.fill")
                        .font(.system(size: 20, weight: .semibold))
                        .foregroundStyle(.white)
                }
                .frame(width: 54, height: 54)

                Text("Choose a workspace")
                    .font(.system(size: 15.5, weight: .bold))
                    .foregroundStyle(.white)
            }
            .shadow(color: Color(hex: 0x0C4A6E).opacity(0.25), radius: 6, y: 2)
            .scaleEffect(badgeAppeared ? 1 : 0.9)
            .opacity(badgeAppeared ? 1 : 0)
        }
        .frame(maxWidth: .infinity, minHeight: 40, maxHeight: .infinity)
        .onAppear {
            withAnimation(.spring(response: 0.7, dampingFraction: 0.75).delay(0.05)) {
                badgeAppeared = true
            }
        }
    }

    private var sheet: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 7) {
                Text("Welcome back")
                    .font(.system(size: 30, weight: .bold))
                    .tracking(-0.6)
                Text("Pick up where your team left off, or start fresh.")
                    .font(.system(size: 15.5))
                    .foregroundStyle(.secondary)
            }
            .padding(.top, 30)
            .padding(.bottom, 18)

            ScrollView {
                VStack(spacing: 0) {
                    ForEach(Array(env.session.memberships.enumerated()), id: \.element.id) { index, membership in
                        if let org = membership.organization {
                            workspaceRow(membership: membership, org: org)
                            if index < env.session.memberships.count - 1 {
                                Divider().padding(.leading, 58)
                            }
                        }
                    }
                }
            }
            .scrollBounceBehavior(.basedOnSize)

            footer
        }
        .padding(.horizontal, 24)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            UnevenRoundedRectangle(cornerRadii: .init(topLeading: 36, topTrailing: 36))
                .fill(Color(.systemBackground)
                    .shadow(.drop(color: Color(hex: 0x0F172A).opacity(0.28), radius: 34, y: -6)))
                .padding(.bottom, -600)
                .ignoresSafeArea()
        }
        .layoutPriority(1)
    }

    private func workspaceRow(membership: OrganizationMember, org: Organization) -> some View {
        Button {
            Task { await select(org.id) }
        } label: {
            HStack(spacing: 13) {
                WAvatar(name: org.name, imageURL: org.avatarURL, seed: org.id, size: 45)
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 5) {
                        Text(org.name)
                            .font(.system(size: 16, weight: .semibold))
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                        if membership.role == "owner" {
                            Image(systemName: "crown.fill")
                                .font(.system(size: 10))
                                .foregroundStyle(Tone.amber.color)
                        }
                    }
                    if let role = membership.role {
                        Text(role.capitalized)
                            .font(.system(size: 13))
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: 8)
                if busyOrgID == org.id {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: "chevron.right")
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundStyle(.tertiary)
                }
            }
            .padding(.vertical, 13)
            .contentShape(Rectangle())
        }
        .buttonStyle(PressableButtonStyle())
        .disabled(busyOrgID != nil)
    }

    private var footer: some View {
        VStack(spacing: 12) {
            if let errorMessage {
                Text(errorMessage)
                    .font(.system(size: 13.5))
                    .foregroundStyle(WTheme.negative)
                    .frame(maxWidth: .infinity)
                    .multilineTextAlignment(.center)
                    .transition(.opacity)
            }

            Button {
                showCreate = true
            } label: {
                Label("New workspace", systemImage: "plus")
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(WTheme.accent)
                    .frame(maxWidth: .infinity)
                    .frame(height: 54)
                    .background(WTheme.accent.opacity(0.1), in: RoundedRectangle(cornerRadius: 17))
            }
            .buttonStyle(PressableButtonStyle())
            .disabled(busyOrgID != nil)
        }
        .padding(.top, 12)
        .padding(.bottom, 14)
        .animation(.easeOut(duration: 0.2), value: errorMessage != nil)
    }

    private func select(_ orgID: String) async {
        busyOrgID = orgID
        errorMessage = nil
        do {
            try await env.session.selectOrganization(orgID)
        } catch {
            errorMessage = (error as? APIError)?.errorDescription ?? error.localizedDescription
        }
        busyOrgID = nil
    }
}
