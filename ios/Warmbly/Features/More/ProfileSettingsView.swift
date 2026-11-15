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

/// Who you are, how you sign in and where you're signed in: an identity
/// header, the name card that saves in place, a password section that opens
/// when asked, and every device with a session.
struct ProfileSettingsView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = MoreProfileStore()

    @State private var firstName = ""
    @State private var lastName = ""
    @State private var seeded = false
    @State private var savingProfile = false
    @State private var profileSaved = false

    @State private var editingPassword = false
    @State private var currentPassword = ""
    @State private var newPassword = ""
    @State private var confirmPassword = ""
    @State private var changingPassword = false
    @State private var passwordChanged = false

    @State private var confirmRevokeOthers = false
    @State private var actionError: String?
    @FocusState private var focus: Field?

    private enum Field { case first, last, current, new, confirm }

    var body: some View {
        SettingsPage(title: "Profile") {
            identityHeader
            nameSection
            passwordSection
            sessionsSection
            DeleteAccountSection()
        }
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
            "Sign out every other device?",
            isPresented: $confirmRevokeOthers,
            titleVisibility: .visible
        ) {
            Button("Sign out other devices", role: .destructive) {
                Task { await revokeOthers() }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This device stays signed in.")
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

    // MARK: Identity

    @ViewBuilder
    private var identityHeader: some View {
        if let user = env.session.user {
            VStack(spacing: 10) {
                WAvatar(name: user.displayName, imageURL: user.avatarURL, seed: user.id, size: 92)
                    .overlay(Circle().strokeBorder(GroupedSurface.card, lineWidth: 4))
                    .shadow(color: .black.opacity(0.08), radius: 14, y: 6)
                Text(user.displayName)
                    .displayFont(26, weight: .semibold)
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                    .contentTransition(.opacity)
                    .padding(.top, 4)
                Text(user.email)
                    .font(.system(size: 15))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                if env.session.memberships.count > 1 {
                    Text("Member of \(env.session.memberships.count) workspaces")
                        .font(.system(size: 12.5, weight: .medium))
                        .foregroundStyle(WTheme.inkSecondary)
                        .padding(.horizontal, 10)
                        .frame(height: 24)
                        .background(WTheme.fill, in: Capsule())
                        .padding(.top, 2)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.top, 8)
            .animation(.snappy, value: user.displayName)
        }
    }

    // MARK: Name

    private var profileDirty: Bool {
        firstName.trimmingCharacters(in: .whitespaces) != (env.session.user?.firstName ?? "")
            || lastName.trimmingCharacters(in: .whitespaces) != (env.session.user?.lastName ?? "")
    }

    private var profileValid: Bool {
        let f = firstName.trimmingCharacters(in: .whitespaces)
        let l = lastName.trimmingCharacters(in: .whitespaces)
        return !f.isEmpty && !l.isEmpty && f.count <= 50 && l.count <= 50
    }

    private var nameSection: some View {
        SettingsSection("Name", footer: "Teammates see this name on everything you touch.", dividerLeading: 16) {
            SettingsFieldRow(label: "First name") {
                TextField("First name", text: $firstName)
                    .textContentType(.givenName)
                    .submitLabel(.next)
                    .focused($focus, equals: .first)
                    .onSubmit { focus = .last }
            }
            SettingsFieldRow(label: "Last name") {
                TextField("Last name", text: $lastName)
                    .textContentType(.familyName)
                    .submitLabel(.done)
                    .focused($focus, equals: .last)
                    .onSubmit { if profileDirty, profileValid { saveProfile() } }
            }
            if profileDirty || savingProfile || profileSaved {
                SettingsButtonRow(
                    title: profileSaved && !profileDirty ? "Saved" : "Save changes",
                    busy: savingProfile,
                    done: profileSaved && !profileDirty,
                    disabled: !profileDirty || !profileValid
                ) {
                    saveProfile()
                }
                .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .animation(.snappy, value: profileDirty)
        .animation(.snappy, value: profileSaved)
    }

    private func saveProfile() {
        guard !savingProfile else { return }
        focus = nil
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
                try? await Task.sleep(for: .seconds(2))
                profileSaved = false
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

    /// 0 to 4: length and the mix of character kinds. The server has the final say.
    private var strength: Int {
        guard !newPassword.isEmpty else { return 0 }
        var score = newPassword.count >= 8 ? 1 : 0
        if newPassword.count >= 12 { score += 1 }
        let kinds = [
            newPassword.contains(where: \.isLowercase),
            newPassword.contains(where: \.isUppercase),
            newPassword.contains(where: \.isNumber),
            newPassword.contains(where: { !$0.isLetter && !$0.isNumber }),
        ].filter { $0 }.count
        if kinds >= 3 { score += 1 }
        if kinds == 4, newPassword.count >= 14 { score += 1 }
        return max(1, score)
    }

    private var strengthLabel: (String, Color) {
        switch strength {
        case 0, 1: (newPassword.count < 8 ? "Too short" : "Weak", WTheme.negative)
        case 2: ("Fair", WTheme.warning)
        case 3: ("Good", WTheme.positive)
        default: ("Strong", WTheme.positive)
        }
    }

    private var passwordSection: some View {
        SettingsSection(
            "Password",
            footer: editingPassword ? "At least 8 characters, and not one that has shown up in a known breach. Your other devices stay signed in." : nil,
            dividerLeading: editingPassword ? 16 : 58
        ) {
            if !editingPassword {
                Button {
                    withAnimation(.snappy) { editingPassword = true }
                    focus = .current
                } label: {
                    SettingsRow(
                        icon: "key.fill", color: SettingsPalette.security, title: "Change password",
                        value: passwordChanged ? "Updated" : nil, valueColor: WTheme.positive,
                        dot: passwordChanged ? WTheme.positive : nil
                    )
                }
                .buttonStyle(MoreRowPressStyle())
            } else {
                SettingsFieldRow(label: "Current password") {
                    SecureField("Required", text: $currentPassword)
                        .textContentType(.password)
                        .focused($focus, equals: .current)
                        .submitLabel(.next)
                        .onSubmit { focus = .new }
                }
                SettingsFieldRow(label: "New password") {
                    VStack(alignment: .leading, spacing: 8) {
                        SecureField("At least 8 characters", text: $newPassword)
                            .textContentType(.newPassword)
                            .focused($focus, equals: .new)
                            .submitLabel(.next)
                            .onSubmit { focus = .confirm }
                        if !newPassword.isEmpty {
                            strengthMeter
                                .transition(.opacity)
                        }
                    }
                }
                SettingsFieldRow(label: "Confirm new password") {
                    HStack {
                        SecureField("Repeat it", text: $confirmPassword)
                            .textContentType(.newPassword)
                            .focused($focus, equals: .confirm)
                            .submitLabel(.done)
                            .onSubmit { if passwordValid { changePassword() } }
                        if !confirmPassword.isEmpty {
                            Image(systemName: confirmPassword == newPassword ? "checkmark.circle.fill" : "xmark.circle.fill")
                                .foregroundStyle(confirmPassword == newPassword ? WTheme.positive : WTheme.negative)
                                .contentTransition(.symbolEffect(.replace))
                        }
                    }
                }
                HStack(spacing: 0) {
                    SettingsButtonRow(title: "Cancel", disabled: changingPassword) {
                        withAnimation(.snappy) { resetPassword() }
                    }
                    Rectangle().fill(WTheme.hairline).frame(width: 1, height: 28)
                    SettingsButtonRow(title: "Update password", busy: changingPassword, disabled: !passwordValid) {
                        changePassword()
                    }
                }
            }
        }
        .animation(.snappy, value: newPassword.isEmpty)
    }

    private var strengthMeter: some View {
        HStack(spacing: 8) {
            HStack(spacing: 4) {
                ForEach(0 ..< 4, id: \.self) { index in
                    Capsule()
                        .fill(index < strength ? strengthLabel.1 : WTheme.fill)
                        .frame(height: 4)
                }
            }
            Text(strengthLabel.0)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(strengthLabel.1)
                .frame(width: 66, alignment: .trailing)
        }
        .animation(.snappy, value: strength)
    }

    private func resetPassword() {
        editingPassword = false
        currentPassword = ""
        newPassword = ""
        confirmPassword = ""
        focus = nil
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
                withAnimation(.snappy) { resetPassword() }
                passwordChanged = true
            } catch {
                actionError = error.localizedDescription
            }
            changingPassword = false
        }
    }

    // MARK: Devices

    private var otherSessions: Int { store.sessions.filter { $0.current != true }.count }

    @ViewBuilder
    private var sessionsSection: some View {
        SettingsSection(
            "Signed-in devices",
            footer: store.loadedOnce && otherSessions > 0 ? "Tap a device to sign it out. Signing out ends its session right away." : nil
        ) {
            if !store.loadedOnce {
                if let message = store.errorMessage {
                    HStack {
                        Text(message)
                            .font(.system(size: 14))
                            .foregroundStyle(WTheme.inkSecondary)
                        Spacer()
                        Button("Retry") {
                            Task { await store.load(env.api) }
                        }
                        .font(.system(size: 15, weight: .medium))
                    }
                    .padding(16)
                } else {
                    HStack(spacing: 10) {
                        ProgressView().controlSize(.small)
                        Text("Loading devices")
                            .font(.system(size: 15))
                            .foregroundStyle(WTheme.inkSecondary)
                    }
                    .padding(16)
                }
            } else {
                ForEach(store.sessions) { session in
                    if session.current == true {
                        sessionRow(session)
                    } else {
                        Menu {
                            Button(role: .destructive) {
                                Task { await revoke(session) }
                            } label: {
                                Label("Sign out this device", systemImage: "rectangle.portrait.and.arrow.right")
                            }
                        } label: {
                            sessionRow(session)
                        }
                        .buttonStyle(MoreRowPressStyle())
                    }
                }
                if otherSessions > 0 {
                    SettingsButtonRow(title: otherSessions == 1 ? "Sign out the other device" : "Sign out \(otherSessions) other devices", role: .destructive) {
                        confirmRevokeOthers = true
                    }
                }
            }
        }
        .animation(.snappy, value: store.sessions.count)
    }

    private func sessionRow(_ session: UserSession) -> some View {
        HStack(spacing: 12) {
            SettingsIcon(symbol: deviceIcon(session), color: session.current == true ? WTheme.positive : SettingsPalette.profile, size: 36)
            VStack(alignment: .leading, spacing: 2) {
                Text(deviceTitle(session))
                    .font(.system(size: 16))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                Text(sessionDetail(session))
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            if session.current == true {
                StatusPill(text: "This device", tone: .emerald)
            } else if let active = session.lastActiveAt {
                Text(WFormat.relative(active))
                    .font(.system(size: 13.5))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkTertiary)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 11)
        .contentShape(Rectangle())
    }

    private func deviceIcon(_ session: UserSession) -> String {
        let os = (session.os ?? "").lowercased()
        let browser = (session.browser ?? "").lowercased()
        if os.contains("ios") || os.contains("iphone") || os.contains("android") { return "iphone" }
        if os.contains("ipad") { return "ipad" }
        if browser.contains("curl") || browser.contains("api") { return "terminal.fill" }
        return "laptopcomputer"
    }

    private func deviceTitle(_ session: UserSession) -> String {
        let parts = [session.browser, session.os].compactMap { $0 }.filter { !$0.isEmpty }
        return parts.isEmpty ? "Unknown device" : parts.joined(separator: " on ")
    }

    private func sessionDetail(_ session: UserSession) -> String {
        var parts: [String] = []
        let location = [session.locationCity, session.locationCountry]
            .compactMap { $0 }
            .filter { !$0.isEmpty }
            .joined(separator: ", ")
        if !location.isEmpty { parts.append(location) }
        if let provider = session.authProvider, !provider.isEmpty {
            parts.append("via \(provider.capitalized)")
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
}
