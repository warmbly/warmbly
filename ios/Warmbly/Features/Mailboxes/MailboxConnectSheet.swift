import SwiftUI

/// Modal connect flow. As a sheet it is its own root, so it wraps content in
/// a NavigationStack (unlike pushed detail views). Offers the two provider
/// OAuth launchers and the fully-native SMTP/IMAP form.
struct MailboxConnectSheet: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL

    @State private var mode: Mode = .choose
    @State private var errorText: String?
    @State private var isWorking = false

    // SMTP/IMAP form
    @State private var email = ""
    @State private var displayName = ""
    @State private var smtpHost = ""
    @State private var smtpPort = "465"
    @State private var smtpUsername = ""
    @State private var smtpPassword = ""
    @State private var imapHost = ""
    @State private var imapPort = "993"
    @State private var imapUsername = ""
    @State private var imapPassword = ""

    enum Mode: Equatable {
        case choose
        case smtp
    }

    var body: some View {
        NavigationStack {
            Group {
                switch mode {
                case .choose: chooser
                case .smtp: smtpForm
                }
            }
            .navigationTitle("Connect a mailbox")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                if mode == .smtp {
                    ToolbarItem(placement: .confirmationAction) {
                        if isWorking {
                            ProgressView().controlSize(.small)
                        } else {
                            Button("Connect") {
                                Task { await connectSMTP() }
                            }
                            .disabled(!smtpFormValid)
                        }
                    }
                }
            }
            .alert(
                "Couldn't connect",
                isPresented: Binding(
                    get: { errorText != nil },
                    set: { if !$0 { errorText = nil } }
                )
            ) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(errorText ?? "")
            }
        }
        .presentationDragIndicator(.visible)
        .sensoryFeedback(.selection, trigger: mode)
    }

    // MARK: Chooser

    private var chooser: some View {
        List {
            Section {
                providerRow("Gmail", subtitle: "Connect with Google", provider: "gmail", symbol: "envelope.fill", tone: .rose)
                providerRow("Outlook", subtitle: "Connect with Microsoft", provider: "outlook", symbol: "envelope.fill", tone: .sky)
            } header: {
                Text("OAuth")
            }
            Section {
                Button {
                    mode = .smtp
                } label: {
                    HStack(spacing: 12) {
                        IconTile(symbol: "server.rack", tone: .slate, size: 38)
                        VStack(alignment: .leading, spacing: 2) {
                            Text("SMTP / IMAP")
                                .font(.body.weight(.medium))
                                .foregroundStyle(.primary)
                            Text("Host, port, and app password")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        Image(systemName: "chevron.right")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(.tertiary)
                    }
                    .padding(.vertical, 4)
                }
                .buttonStyle(TapScaleStyle())
            } header: {
                Text("Manual")
            } footer: {
                Text("Use a host, port, and app password for providers without OAuth.")
            }
        }
    }

    private func providerRow(_ title: String, subtitle: String, provider: String, symbol: String, tone: Tone) -> some View {
        Button {
            Task { await startOAuth(provider: provider) }
        } label: {
            HStack(spacing: 12) {
                IconTile(symbol: symbol, tone: tone, size: 38)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.body.weight(.medium))
                        .foregroundStyle(.primary)
                    Text(subtitle)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if isWorking {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: "arrow.up.forward.app")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(WTheme.accent)
                }
            }
            .padding(.vertical, 4)
        }
        .buttonStyle(TapScaleStyle())
        .disabled(isWorking)
    }

    // MARK: SMTP form

    private var smtpForm: some View {
        List {
            Section("Mailbox") {
                TextField("Email address", text: $email)
                    .textContentType(.emailAddress)
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                TextField("Sender name", text: $displayName)
            }
            Section("Outgoing (SMTP)") {
                TextField("Host", text: $smtpHost)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                TextField("Port (465 or 587)", text: $smtpPort)
                    .keyboardType(.numberPad)
                TextField("Username", text: $smtpUsername)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                SecureField("Password", text: $smtpPassword)
            }
            Section("Incoming (IMAP)") {
                TextField("Host", text: $imapHost)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                TextField("Port", text: $imapPort)
                    .keyboardType(.numberPad)
                TextField("Username", text: $imapUsername)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                SecureField("Password", text: $imapPassword)
            }
            Section {
                Button {
                    Task { await connectSMTP() }
                } label: {
                    Group {
                        if isWorking {
                            ProgressView().tint(.white)
                        } else {
                            Text("Connect mailbox")
                                .font(.body.weight(.semibold))
                        }
                    }
                    .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .tint(WTheme.accent)
                .controlSize(.large)
                .disabled(!smtpFormValid || isWorking)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            }
        }
    }

    private var smtpFormValid: Bool {
        guard email.contains("@"), displayName.count >= 2 else { return false }
        guard !smtpHost.isEmpty, !imapHost.isEmpty else { return false }
        guard let sPort = Int(smtpPort), sPort == 465 || sPort == 587 else { return false }
        guard let iPort = Int(imapPort), iPort > 0 else { return false }
        return true
    }

    // MARK: Actions

    private func startOAuth(provider: String) async {
        isWorking = true
        defer { isWorking = false }
        do {
            let response: MailboxOAuthStartResponse = try await env.api.post(
                "emails/onboarding/oauth/start",
                body: MailboxOAuthStartBody(provider: provider)
            )
            if let url = URL(string: response.url) {
                openURL(url)
                dismiss()
            } else {
                errorText = "The provider returned an invalid authorization URL."
            }
        } catch {
            errorText = error.localizedDescription
        }
    }

    private func connectSMTP() async {
        isWorking = true
        defer { isWorking = false }
        let body = MailboxSMTPConnectBody(
            email: email.trimmingCharacters(in: .whitespaces),
            name: displayName.trimmingCharacters(in: .whitespaces),
            smtp: MailboxServerCredentials(
                username: smtpUsername,
                password: smtpPassword,
                host: smtpHost.trimmingCharacters(in: .whitespaces),
                port: Int(smtpPort) ?? 465
            ),
            imap: MailboxServerCredentials(
                username: imapUsername,
                password: imapPassword,
                host: imapHost.trimmingCharacters(in: .whitespaces),
                port: Int(imapPort) ?? 993
            )
        )
        do {
            let _: EmailAccount = try await env.api.post("emails/onboarding/smtp-imap", body: body)
            dismiss()
        } catch {
            errorText = error.localizedDescription
        }
    }
}
