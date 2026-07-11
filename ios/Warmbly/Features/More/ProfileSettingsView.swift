import SwiftUI

@MainActor
@Observable
final class MoreProfileStore {
    var sessions: [UserSession] = []
    var loadedOnce = false
    var isLoading = false
    var errorMessage: String?

    func load(_ api: APIClient) async {
        guard !isLoading else { return }
        isLoading = true
        errorMessage = nil
        do {
            // Bare array; current session floated first by the backend.
            sessions = try await api.get("auth/sessions")
            loadedOnce = true
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }

    func revokeSession(_ api: APIClient, id: String) async throws {
        let _: EmptyBody = try await api.delete("auth/sessions/\(id)")
        sessions.removeAll { $0.id == id }
    }

    func revokeOthers(_ api: APIClient) async throws {
        let _: EmptyBody = try await api.delete("auth/sessions")
        sessions = sessions.filter { $0.current == true }
    }
}

/// Name edits, password change, and the active-session list, laid out as one
/// flat, full-bleed sheet: identity header up top, hairline field rows, and
/// the destructive session sign-out at the bottom.
struct ProfileSettingsView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = MoreProfileStore()

    @State private var firstName = ""
    @State private var lastName = ""
    @State private var seeded = false
    @State private var savingProfile = false
    @State private var profileSaved = false

    @State private var currentPassword = ""
    @State private var newPassword = ""
    @State private var confirmPassword = ""
    @State private var changingPassword = false
    @State private var passwordChanged = false

    @State private var confirmRevokeOthers = false
    @State private var actionError: String?

    var body: some View {
        List {
            identityHeader
            profileSection
            passwordSection
            sessionsSection
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .background(Color(.systemBackground))
        .environment(\.defaultMinListRowHeight, 0)
        .navigationTitle("Profile")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            seedIfNeeded()
            await store.load(env.api)
        }
        .onChange(of: env.realtime.pulse(for: .me)) {
            Task { await store.load(env.api) }
        }
        .refreshable {
            await env.session.refreshUser()
            await store.load(env.api)
        }
        .confirmationDialog(
            "Sign out other sessions?",
            isPresented: $confirmRevokeOthers,
            titleVisibility: .visible
        ) {
            Button("Sign out everywhere else", role: .destructive) {
                Task { await revokeOthers() }
            }
            Button("Cancel", role: .cancel) {}
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

    private func seedIfNeeded() {
        guard !seeded, let user = env.session.user else { return }
        firstName = user.firstName ?? ""
        lastName = user.lastName ?? ""
        seeded = true
    }

    // MARK: Identity header

    @ViewBuilder
    private var identityHeader: some View {
        if let user = env.session.user {
            HStack(spacing: 14) {
                WAvatar(name: user.displayName, imageURL: user.avatarURL, seed: user.id, size: 60)
                VStack(alignment: .leading, spacing: 3) {
                    Text(user.displayName)
                        .font(.title3.weight(.semibold))
                        .lineLimit(1)
                    Text(user.email)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
            }
            .padding(.top, 10)
            .padding(.bottom, 6)
            .moreFlatRow(separator: .hidden)
        }
    }

    // MARK: Profile

    private var profileDirty: Bool {
        firstName.trimmingCharacters(in: .whitespaces) != (env.session.user?.firstName ?? "")
            || lastName.trimmingCharacters(in: .whitespaces) != (env.session.user?.lastName ?? "")
    }

    private var profileValid: Bool {
        let f = firstName.trimmingCharacters(in: .whitespaces)
        let l = lastName.trimmingCharacters(in: .whitespaces)
        return !f.isEmpty && !l.isEmpty && f.count <= 50 && l.count <= 50
    }

    @ViewBuilder
    private var profileSection: some View {
        MoreFlatSectionHeader("Name", top: 14)
        fieldRow("First name") {
            TextField("First name", text: $firstName)
                .textContentType(.givenName)
        }
        fieldRow("Last name") {
            TextField("Last name", text: $lastName)
                .textContentType(.familyName)
        }
        actionRow(
            "Save changes",
            busy: savingProfile,
            done: profileSaved && !profileDirty,
            disabled: !profileDirty || !profileValid || savingProfile
        ) {
            saveProfile()
        }
    }

    private func saveProfile() {
        guard !savingProfile else { return }
        savingProfile = true
        profileSaved = false
        Task {
            do {
                // 204; refetch /auth/me for the updated profile.
                let _: EmptyBody = try await env.api.patch(
                    "auth/me",
                    body: MoreProfileBody(
                        firstName: firstName.trimmingCharacters(in: .whitespaces),
                        lastName: lastName.trimmingCharacters(in: .whitespaces)
                    )
                )
                await env.session.refreshUser()
                profileSaved = true
            } catch {
                actionError = error.localizedDescription
            }
            savingProfile = false
        }
    }

    // MARK: Password

    private var passwordValid: Bool {
        !currentPassword.isEmpty && newPassword.count >= 8 && confirmPassword == newPassword
    }

    @ViewBuilder
    private var passwordSection: some View {
        MoreFlatSectionHeader("Password")
        fieldRow("Current") {
            SecureField("Current password", text: $currentPassword)
                .textContentType(.password)
        }
        fieldRow("New") {
            SecureField("New password", text: $newPassword)
                .textContentType(.newPassword)
        }
        fieldRow("Confirm") {
            SecureField("Confirm new password", text: $confirmPassword)
                .textContentType(.newPassword)
        }
        actionRow(
            "Update password",
            busy: changingPassword,
            done: passwordChanged,
            disabled: !passwordValid || changingPassword
        ) {
            changePassword()
        }
        footnoteRow("At least 8 characters. Other sessions stay signed in.")
    }

    private func changePassword() {
        guard !changingPassword else { return }
        changingPassword = true
        passwordChanged = false
        Task {
            do {
                let _: EmptyBody = try await env.api.post(
                    "auth/me/password",
                    body: MorePasswordBody(currentPassword: currentPassword, newPassword: newPassword)
                )
                currentPassword = ""
                newPassword = ""
                confirmPassword = ""
                passwordChanged = true
            } catch {
                actionError = error.localizedDescription
            }
            changingPassword = false
        }
    }

    // MARK: Sessions

    @ViewBuilder
    private var sessionsSection: some View {
        MoreFlatSectionHeader("Sessions")
        if !store.loadedOnce {
            if let message = store.errorMessage {
                HStack {
                    Text(message)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Spacer()
                    Button("Retry") {
                        Task { await store.load(env.api) }
                    }
                    .font(.subheadline.weight(.medium))
                }
                .padding(.vertical, 12)
                .moreFlatRow(separator: .hidden)
            } else {
                HStack(spacing: 10) {
                    ProgressView().controlSize(.small)
                    Text("Loading sessions")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .padding(.vertical, 12)
                .moreFlatRow(separator: .hidden)
            }
        } else {
            ForEach(store.sessions) { session in
                sessionRow(session)
                    .moreFlatRow(textLeading: MoreFlatMetrics.tileTextLeading)
                    .swipeActions(edge: .trailing) {
                        if session.current != true {
                            Button(role: .destructive) {
                                Task { await revoke(session) }
                            } label: {
                                Label("Revoke", systemImage: "xmark.circle")
                            }
                        }
                    }
            }
            if store.sessions.contains(where: { $0.current != true }) {
                Button(role: .destructive) {
                    confirmRevokeOthers = true
                } label: {
                    Text("Sign out other sessions")
                        .font(.body.weight(.medium))
                        .padding(.vertical, 12)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(Rectangle())
                }
                .moreFlatRow(separator: .hidden)
            }
            footnoteRow("Swipe a session to revoke it. The current session can't be revoked here.", bottom: 28)
        }
    }

    private func sessionRow(_ session: UserSession) -> some View {
        HStack(spacing: 12) {
            IconTile(symbol: deviceIcon(session), tone: .slate, size: 34)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(deviceTitle(session))
                        .font(.body.weight(.medium))
                        .lineLimit(1)
                    if session.current == true {
                        StatusPill(text: "This device", tone: .emerald)
                    }
                }
                Text(sessionDetail(session))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 0)
            if let active = session.lastActiveAt {
                Text(WFormat.relative(active))
                    .font(.footnote)
                    .monospacedDigit()
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.vertical, 10)
    }

    private func deviceIcon(_ session: UserSession) -> String {
        let os = (session.os ?? "").lowercased()
        if os.contains("ios") || os.contains("iphone") || os.contains("android") {
            return "iphone"
        }
        return "laptopcomputer"
    }

    private func deviceTitle(_ session: UserSession) -> String {
        let parts = [session.browser, session.os].compactMap { $0 }.filter { !$0.isEmpty }
        return parts.isEmpty ? "Unknown device" : parts.joined(separator: " · ")
    }

    private func sessionDetail(_ session: UserSession) -> String {
        var parts: [String] = []
        let location = [session.locationCity, session.locationCountry]
            .compactMap { $0 }
            .filter { !$0.isEmpty }
            .joined(separator: ", ")
        if !location.isEmpty { parts.append(location) }
        if let provider = session.authProvider, !provider.isEmpty {
            parts.append(provider.capitalized)
        }
        return parts.isEmpty ? "Location unknown" : parts.joined(separator: " · ")
    }

    private func revoke(_ session: UserSession) async {
        do {
            try await store.revokeSession(env.api, id: session.id)
        } catch {
            actionError = error.localizedDescription
        }
    }

    private func revokeOthers() async {
        do {
            try await store.revokeOthers(env.api)
        } catch {
            actionError = error.localizedDescription
        }
    }

    // MARK: Flat building blocks

    private func fieldRow<Field: View>(_ label: String, @ViewBuilder field: () -> Field) -> some View {
        HStack(spacing: 12) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 84, alignment: .leading)
            field()
        }
        .padding(.vertical, 12)
        .moreFlatRow()
    }

    private func actionRow(
        _ title: String,
        busy: Bool,
        done: Bool,
        disabled: Bool,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            HStack {
                Text(title)
                    .font(.body.weight(.medium))
                Spacer()
                if busy {
                    ProgressView().controlSize(.small)
                } else if done {
                    Image(systemName: "checkmark")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(WTheme.positive)
                }
            }
            .padding(.vertical, 12)
            .contentShape(Rectangle())
        }
        .disabled(disabled)
        .moreFlatRow()
    }

    private func footnoteRow(_ text: String, bottom: CGFloat = 4) -> some View {
        Text(text)
            .font(.footnote)
            .foregroundStyle(.secondary)
            .padding(.top, 8)
            .padding(.bottom, bottom)
            .moreFlatRow(separator: .hidden)
    }
}
