import SwiftUI

@MainActor
@Observable
final class MoreSecurityStore {
    var twoFAEnabled: Bool?
    var passkeys: [PasskeyCredential] = []
    var loadedOnce = false
    var isLoading = false
    var errorMessage: String?

    func load(_ api: APIClient) async {
        guard !isLoading else { return }
        isLoading = true
        errorMessage = nil
        do {
            let status: MoreTwoFAStatus = try await api.get("auth/2fa/status")
            twoFAEnabled = status.enabled ?? false
            // Bare array.
            passkeys = try await api.get("auth/passkey/credentials")
            loadedOnce = true
        } catch {
            errorMessage = error.localizedDescription
        }
        isLoading = false
    }
}

/// How well the account is protected: a hero that says it plainly, the two
/// second steps and their state, every passkey, and the way to set one up.
struct SecurityInfoView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = MoreSecurityStore()

    private var factors: Int {
        (store.twoFAEnabled == true ? 1 : 0) + (store.passkeys.isEmpty ? 0 : 1)
    }

    var body: some View {
        content
            .navigationTitle("Security")
            .surfaceTitleDisplayMode()
            .task { await store.load(env.api) }
            .onChange(of: env.realtime.pulse(for: .me)) {
                Task { await store.load(env.api) }
            }
    }

    @ViewBuilder
    private var content: some View {
        if !store.loadedOnce {
            if let message = store.errorMessage {
                ErrorStateView(title: "Couldn't load security settings", message: message) {
                    await store.load(env.api)
                }
            } else {
                SkeletonRows(rows: 4)
            }
        } else {
            SettingsPage(title: "Security") {
                SettingsHero(
                    symbol: factors > 0 ? "checkmark.shield.fill" : "exclamationmark.shield.fill",
                    color: factors > 0 ? WTheme.positive : WTheme.warning,
                    title: factors > 0 ? "Your account is protected" : "Add a second step",
                    subtitle: factors > 0
                        ? "Signing in takes more than your password."
                        : "With only a password, anyone who learns it can sign in. A code or a passkey stops them."
                )
                protectionSection
                if !store.passkeys.isEmpty { passkeysSection }
                setupSection
            }
            .refreshable { await store.load(env.api) }
        }
    }

    private var protectionSection: some View {
        SettingsSection("Sign-in protection", footer: "\(factors) of 2 turned on.") {
            SettingsRow(
                icon: "lock.rotation", color: SettingsPalette.security, title: "Authenticator app",
                subtitle: "A 6-digit code from an app like 1Password",
                value: store.twoFAEnabled == true ? "On" : "Off",
                valueColor: store.twoFAEnabled == true ? WTheme.positive : WTheme.inkTertiary,
                dot: store.twoFAEnabled == true ? WTheme.positive : nil,
                accessory: .none
            )
            SettingsRow(
                icon: "person.badge.key.fill", color: SettingsPalette.templates, title: "Passkeys",
                subtitle: "Face ID, Touch ID or a security key",
                value: store.passkeys.isEmpty ? "None" : "\(store.passkeys.count) saved",
                valueColor: store.passkeys.isEmpty ? WTheme.inkTertiary : WTheme.positive,
                dot: store.passkeys.isEmpty ? nil : WTheme.positive,
                accessory: .none
            )
        }
    }

    private var passkeysSection: some View {
        SettingsSection("Passkeys") {
            ForEach(store.passkeys) { credential in
                passkeyRow(credential)
            }
        }
    }

    private func passkeyRow(_ credential: PasskeyCredential) -> some View {
        HStack(spacing: 12) {
            SettingsIcon(symbol: credential.backupState == true ? "key.icloud.fill" : "key.fill", color: SettingsPalette.templates)
            VStack(alignment: .leading, spacing: 2) {
                Text(credential.name?.isEmpty == false ? credential.name! : "Passkey")
                    .font(.system(size: 16))
                    .foregroundStyle(WTheme.ink)
                    .lineLimit(1)
                Text(passkeyDetail(credential))
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.inkSecondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            if let created = credential.createdAt {
                Text(created.formatted(.dateTime.month(.abbreviated).day().year()))
                    .font(.system(size: 13.5))
                    .monospacedDigit()
                    .foregroundStyle(WTheme.inkTertiary)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 11)
    }

    private func passkeyDetail(_ credential: PasskeyCredential) -> String {
        var parts: [String] = []
        if let provider = credential.provider, !provider.isEmpty {
            parts.append(provider)
        } else if credential.backupState == true {
            parts.append("Synced")
        }
        if let lastUsed = credential.lastUsedAt {
            parts.append("Used \(WFormat.relative(lastUsed))")
        } else {
            parts.append("Never used")
        }
        return parts.joined(separator: " · ")
    }

    @ViewBuilder
    private var setupSection: some View {
        if let url = AppConfig.dashboardURL("/app/settings/security") {
            SettingsSection(footer: "Setting up a second step needs a code scanned or a key registered, which happens on the web dashboard.") {
                Link(destination: url) {
                    SettingsRow(
                        icon: "safari.fill", color: WTheme.accent,
                        title: factors > 0 ? "Manage on the web" : "Set up on the web",
                        accessory: .external
                    )
                }
                .buttonStyle(MoreRowPressStyle())
            }
        }
    }
}
