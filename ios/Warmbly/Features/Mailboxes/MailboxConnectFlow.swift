import AuthenticationServices
import SwiftUI

/// Full-screen "connect a mailbox" onboarding wearing the same air as
/// sign-up: the sky with its flight ambience and a step badge, a full-bleed
/// white sheet below. Gmail/Outlook connect through an in-app
/// ASWebAuthenticationSession ceremony; everything else goes through the
/// native SMTP/IMAP form. Dismissal always goes through `onClose()`; the
/// environment DismissAction can no-op in this app's presentation contexts.
struct MailboxConnectFlow: View {
    var onClose: () -> Void = {}
    var onConnected: ((EmailAccount) -> Void)? = nil

    private enum Page: Int {
        case provider, connect, account, imap, smtp, done

        var icon: String {
            switch self {
            case .provider: "envelope.badge.person.crop"
            case .connect: "lock.shield.fill"
            case .account: "person.crop.circle"
            case .imap: "tray.and.arrow.down.fill"
            case .smtp: "paperplane.fill"
            case .done: "checkmark.seal.fill"
            }
        }

        var skyLabel: String {
            switch self {
            case .provider: "Add a mailbox"
            case .connect: "Authorize access"
            case .account: "Your address"
            case .imap: "Incoming mail"
            case .smtp: "Outgoing mail"
            case .done: "Connected"
            }
        }
    }

    /// Which branch the step badge counts: OAuth is two steps, SMTP is five.
    private enum Route {
        case undecided, oauth, smtp
    }

    private enum WarmupOffer: Equatable {
        case offer, starting, active, failed(String)
    }

    private enum Field: Hashable {
        case name, email
        case imapHost, imapPort, imapUser, imapPass
        case smtpHost, smtpPort, smtpUser, smtpPass
    }

    @Environment(AppEnvironment.self) private var env

    @State private var page = Page.provider
    @State private var route = Route.undecided
    @State private var direction = 1.0
    @State private var badgeAppeared = false

    // OAuth path
    @State private var pendingProvider = "gmail"
    @State private var oauthProvider: String?
    @State private var oauthFlow = MailboxOAuthFlow()

    // SMTP path form
    @State private var name = ""
    @State private var email = ""
    @State private var imapHost = ""
    @State private var imapPort = "993"
    @State private var imapUsername = ""
    @State private var imapPassword = ""
    @State private var smtpHost = ""
    @State private var smtpPort = "587"
    @State private var sameLogin = true
    @State private var smtpUsername = ""
    @State private var smtpPassword = ""

    @State private var busy = false
    @State private var errorMessage: String?
    @State private var errorPulse = 0

    // Done page
    @State private var connectedAccount: EmailAccount?
    @State private var warmupOffer = WarmupOffer.offer

    @FocusState private var focused: Field?

    var body: some View {
        ZStack(alignment: .bottom) {
            SkyBackdrop()
            VStack(spacing: 0) {
                topBar
                skyArea
                sheet
            }
        }
        .sensoryFeedback(.impact(weight: .light), trigger: page)
        .sensoryFeedback(.error, trigger: errorPulse)
    }

    private var pagePath: [Page] {
        route == .smtp
            ? [.provider, .account, .imap, .smtp, .done]
            : [.provider, .connect, .done]
    }

    private var stepIndex: Int { pagePath.firstIndex(of: page) ?? 0 }

    private var isBusy: Bool { busy || warmupOffer == .starting || oauthProvider != nil }

    private var pageTransition: AnyTransition {
        .asymmetric(
            insertion: .move(edge: direction > 0 ? .trailing : .leading).combined(with: .opacity),
            removal: .move(edge: direction > 0 ? .leading : .trailing).combined(with: .opacity)
        )
    }

    // MARK: - Sky chrome

    private var topBar: some View {
        HStack(spacing: 12) {
            if stepIndex > 0, page != .done {
                Button {
                    goBack()
                } label: {
                    Image(systemName: "chevron.left")
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(.white)
                        .frame(width: 40, height: 40)
                        .background(.white.opacity(0.16), in: Circle())
                }
                .buttonStyle(PressableButtonStyle())
                .accessibilityLabel("Back")
                .disabled(isBusy)
                .transition(.opacity.combined(with: .scale(scale: 0.7)))
            }

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

            Button {
                onClose()
            } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(width: 40, height: 40)
                    .background(.white.opacity(0.16), in: Circle())
            }
            .buttonStyle(PressableButtonStyle())
            .accessibilityLabel("Cancel")
            .disabled(isBusy)
        }
        .padding(.horizontal, 16)
        .padding(.top, 6)
        .animation(.spring(response: 0.45, dampingFraction: 0.86), value: page)
    }

    private var skyArea: some View {
        ZStack {
            Color.clear
                .contentShape(Rectangle())
                .onTapGesture { focused = nil }

            HeroFlightScene()

            pageBadge
                .scaleEffect(badgeAppeared ? 1 : 0.9)
                .opacity(badgeAppeared ? 1 : 0)
        }
        .frame(maxWidth: .infinity, minHeight: 40, maxHeight: .infinity)
        .animation(.spring(response: 0.5, dampingFraction: 0.85), value: page)
        .onAppear {
            withAnimation(.spring(response: 0.7, dampingFraction: 0.75).delay(0.05)) {
                badgeAppeared = true
            }
        }
    }

    private var pageBadge: some View {
        ViewThatFits(in: .vertical) {
            VStack(spacing: 12) {
                ZStack {
                    Circle().fill(.white.opacity(0.16))
                    Circle().strokeBorder(.white.opacity(0.3), lineWidth: 1)
                    Image(systemName: page.icon)
                        .font(.system(size: 20, weight: .semibold))
                        .foregroundStyle(.white)
                        .contentTransition(.symbolEffect(.replace))
                }
                .frame(width: 54, height: 54)

                VStack(spacing: 4) {
                    Text(page.skyLabel)
                        .font(.system(size: 15.5, weight: .bold))
                        .foregroundStyle(.white)
                        .contentTransition(.opacity)
                    Text("Step \(stepIndex + 1) of \(pagePath.count)")
                        .font(.system(size: 12))
                        .foregroundStyle(.white.opacity(0.7))
                        .contentTransition(.numericText())
                }

                pageSegments
            }
            .padding(.vertical, 16)

            HStack(spacing: 10) {
                ZStack {
                    Circle().fill(.white.opacity(0.16))
                    Circle().strokeBorder(.white.opacity(0.3), lineWidth: 1)
                    Image(systemName: page.icon)
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(.white)
                        .contentTransition(.symbolEffect(.replace))
                }
                .frame(width: 34, height: 34)

                VStack(alignment: .leading, spacing: 4) {
                    Text(page.skyLabel)
                        .font(.system(size: 13.5, weight: .semibold))
                        .foregroundStyle(.white)
                        .contentTransition(.opacity)
                    HStack(spacing: 7) {
                        pageSegments
                        Text("Step \(stepIndex + 1) of \(pagePath.count)")
                            .font(.system(size: 11))
                            .foregroundStyle(.white.opacity(0.7))
                            .contentTransition(.numericText())
                    }
                }
            }
            .padding(.vertical, 8)
        }
        .shadow(color: Color(hex: 0x0C4A6E).opacity(0.25), radius: 6, y: 2)
    }

    private var pageSegments: some View {
        HStack(spacing: 6) {
            ForEach(Array(pagePath.enumerated()), id: \.element.rawValue) { index, segment in
                Capsule()
                    .fill(.white.opacity(index <= stepIndex ? 0.95 : 0.3))
                    .frame(width: segment == page ? 26 : 14, height: 4)
            }
        }
    }

    // MARK: - The sheet

    private var sheet: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Always a ScrollView: ViewThatFits swaps identities when the
            // keyboard shrinks the space, which destroys the focused field
            // mid-tap on the taller form pages.
            ScrollView {
                pageContent
            }
            .scrollBounceBehavior(.basedOnSize)
            .scrollDismissesKeyboard(.interactively)

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
        .geometryGroup()
        .layoutPriority(1)
    }

    private var pageContent: some View {
        Group {
            switch page {
            case .provider: providerPage
            case .connect: connectPage
            case .account: accountPage
            case .imap: imapPage
            case .smtp: smtpPage
            case .done: donePage
            }
        }
        .padding(.top, 30)
        .padding(.bottom, 8)
        .transition(pageTransition)
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

            if page == .smtp, busy {
                Text("Verified against your server before saving.")
                    .font(.system(size: 13))
                    .foregroundStyle(.tertiary)
                    .frame(maxWidth: .infinity)
                    .multilineTextAlignment(.center)
                    .transition(.opacity)
            }

            if page == .connect, oauthProvider != nil {
                Text("Waiting for authorization in the \(providerDisplayName) window.")
                    .font(.system(size: 13))
                    .foregroundStyle(.tertiary)
                    .frame(maxWidth: .infinity)
                    .multilineTextAlignment(.center)
                    .transition(.opacity)
            }

            if page != .provider {
                Button {
                    Task { await primaryAction() }
                } label: {
                    Group {
                        if primaryBusy {
                            ProgressView().tint(.white)
                        } else {
                            HStack(spacing: 8) {
                                Text(primaryLabel)
                                if let icon = primaryIcon {
                                    Image(systemName: icon)
                                        .font(.system(size: 14, weight: .semibold))
                                }
                            }
                        }
                    }
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(maxWidth: .infinity)
                    .frame(height: 56)
                    .background(
                        LinearGradient(
                            colors: !canAdvance && !primaryBusy
                                ? [WTheme.accent.opacity(0.35), WTheme.accent.opacity(0.35)]
                                : [Color(hex: 0x0EA5E9), Color(hex: 0x0284C7)],
                            startPoint: .top,
                            endPoint: .bottom
                        ),
                        in: RoundedRectangle(cornerRadius: 17)
                    )
                    .shadow(color: !canAdvance ? .clear : Color(hex: 0x0284C7).opacity(0.32), radius: 12, y: 6)
                }
                .buttonStyle(PressableButtonStyle())
                .disabled(primaryBusy || !canAdvance)
                .animation(.easeOut(duration: 0.18), value: canAdvance)
            }

            if page == .done, warmupOffer == .offer {
                Button("Skip for now") {
                    onClose()
                }
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(.secondary)
                .disabled(primaryBusy)
                .transition(.opacity)
            }
        }
        .padding(.top, 10)
        .padding(.bottom, 14)
        .animation(.easeOut(duration: 0.2), value: page)
        .animation(.easeOut(duration: 0.2), value: errorMessage != nil)
        .animation(.easeOut(duration: 0.2), value: warmupOffer)
    }

    private var primaryBusy: Bool { busy || warmupOffer == .starting || oauthProvider != nil }

    private var primaryLabel: String {
        switch page {
        case .provider, .account, .imap: "Continue"
        case .connect: "Continue with \(providerDisplayName)"
        case .smtp: "Connect mailbox"
        case .done:
            switch warmupOffer {
            case .offer, .starting: "Start warmup"
            case .active, .failed: "Done"
            }
        }
    }

    private var primaryIcon: String? {
        switch page {
        case .connect: "arrow.up.right.square"
        case .smtp: "paperplane.fill"
        case .done: warmupOffer == .offer ? "flame.fill" : nil
        default: nil
        }
    }

    // MARK: - Pages

    private func pageTitle(_ title: String, _ subtitle: String) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(title)
                .font(.system(size: 30, weight: .bold))
                .tracking(-0.6)
                .foregroundStyle(.primary)
            Text(subtitle)
                .font(.system(size: 15.5))
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.bottom, 22)
    }

    private var providerPage: some View {
        VStack(alignment: .leading, spacing: 0) {
            pageTitle(
                "Connect a sending account",
                "Warm it up and send campaigns from your own address."
            )

            VStack(spacing: 12) {
                providerRow(
                    "Gmail / Google Workspace",
                    sub: "OAuth via Google. Best deliverability for Gmail.",
                    provider: "gmail"
                ) {
                    brandLogoTile("GoogleLogo", size: 42)
                }
                providerRow(
                    "Outlook / Microsoft 365",
                    sub: "OAuth via Microsoft. Native sync for Outlook accounts.",
                    provider: "outlook"
                ) {
                    brandLogoTile("OutlookLogo", size: 42)
                }
                providerRow(
                    "Other (SMTP / IMAP)",
                    sub: "Any provider with a host, port and app password.",
                    provider: nil
                ) {
                    IconTile(symbol: "server.rack", tone: .slate, size: 42)
                }
            }

            Text("Credentials are stored encrypted. You can disconnect anytime.")
                .font(.system(size: 13))
                .foregroundStyle(.tertiary)
                .padding(.top, 12)
        }
    }

    /// Brand mark on a white tile with a hairline border, like the web's
    /// provider rows.
    private func brandLogoTile(_ asset: String, size: CGFloat) -> some View {
        Image(asset)
            .resizable()
            .scaledToFit()
            .frame(width: size * 0.55, height: size * 0.55)
            .frame(width: size, height: size)
            .background(.white, in: RoundedRectangle(cornerRadius: size * 0.32))
            .overlay(
                RoundedRectangle(cornerRadius: size * 0.32)
                    .strokeBorder(Color(.separator).opacity(0.5), lineWidth: 1)
            )
    }

    private func providerRow<Tile: View>(
        _ title: String,
        sub: String,
        provider: String?,
        @ViewBuilder tile: () -> Tile
    ) -> some View {
        Button {
            errorMessage = nil
            if let provider {
                pendingProvider = provider
                route = .oauth
                goTo(.connect)
            } else {
                route = .smtp
                goTo(.account)
            }
        } label: {
            HStack(spacing: 12) {
                tile()
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.system(size: 15.5, weight: .semibold))
                        .foregroundStyle(.primary)
                    Text(sub)
                        .font(.system(size: 13))
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.leading)
                        .lineLimit(2)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 8)
                Image(systemName: "chevron.right")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(.tertiary)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 13)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 17))
        }
        .buttonStyle(PressableButtonStyle())
    }

    // MARK: - Connect page (OAuth ceremony)

    private var providerDisplayName: String {
        pendingProvider == "outlook" ? "Microsoft" : "Google"
    }

    private var providerLogoAsset: String {
        pendingProvider == "outlook" ? "OutlookLogo" : "GoogleLogo"
    }

    private var connectPage: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 14) {
                brandLogoTile(providerLogoAsset, size: 56)
                VStack(alignment: .leading, spacing: 3) {
                    Text("Connect with \(providerDisplayName)")
                        .font(.system(size: 22, weight: .bold))
                        .tracking(-0.4)
                        .foregroundStyle(.primary)
                    Text(pendingProvider == "outlook" ? "Outlook or Microsoft 365" : "Gmail or Google Workspace")
                        .font(.system(size: 14))
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.bottom, 16)

            Text("We'll open a \(providerDisplayName) sign-in window. Approve the access and you're done.")
                .font(.system(size: 15.5))
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.bottom, 20)

            VStack(alignment: .leading, spacing: 12) {
                connectScopeRow("paperplane.fill", "Send and read mail on your behalf")
                connectScopeRow("arrowshape.turn.up.left.fill", "Track replies and deliveries")
                connectScopeRow("lock.fill", "Tokens are stored encrypted. Revoke anytime.")
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 17))
        }
    }

    private func connectScopeRow(_ symbol: String, _ text: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: symbol)
                .font(.system(size: 12.5, weight: .semibold))
                .foregroundStyle(WTheme.accent)
                .frame(width: 18)
            Text(text)
                .font(.system(size: 14))
                .foregroundStyle(.primary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var accountPage: some View {
        VStack(alignment: .leading, spacing: 0) {
            pageTitle(
                "Your sending address",
                "The name and email recipients will see."
            )

            VStack(alignment: .leading, spacing: 14) {
                fieldBlock("Name", helper: "The sender name recipients see.") {
                    styledField("Alex Rivera", text: $name, field: .name) {
                        focused = .email
                    }
                    .textContentType(.name)
                }

                fieldBlock("Email") {
                    styledField("alex@company.com", text: $email, field: .email, submit: .continue) {
                        Task { await advanceIfValid() }
                    }
                    .textContentType(.emailAddress)
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                }
            }
        }
        .onAppear {
            if name.isEmpty { focused = .name }
        }
    }

    private var imapPage: some View {
        VStack(alignment: .leading, spacing: 0) {
            pageTitle(
                "Incoming mail",
                "IMAP keeps replies and inbox activity in sync."
            )

            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .top, spacing: 12) {
                    fieldBlock("Host") {
                        styledField("imap.example.com", text: $imapHost, field: .imapHost) {
                            focused = .imapUser
                        }
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    }

                    fieldBlock("Port") {
                        styledField("993", text: $imapPort, field: .imapPort)
                            .keyboardType(.numberPad)
                            .frame(width: 96)
                    }
                }

                fieldBlock("Username") {
                    styledField("alex@company.com", text: $imapUsername, field: .imapUser) {
                        focused = .imapPass
                    }
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                }

                fieldBlock("Password", helper: "Usually an app-specific password, not your main login.") {
                    styledField("App password", text: $imapPassword, field: .imapPass, secure: true, submit: .continue) {
                        Task { await advanceIfValid() }
                    }
                }
            }
        }
        .onAppear {
            if imapUsername.isEmpty { imapUsername = trimmedEmail }
            if imapHost.isEmpty { focused = .imapHost }
        }
    }

    private var smtpPage: some View {
        VStack(alignment: .leading, spacing: 0) {
            pageTitle(
                "Outgoing mail",
                "The SMTP server your campaigns send through."
            )

            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .top, spacing: 12) {
                    fieldBlock("Host") {
                        styledField("smtp.example.com", text: $smtpHost, field: .smtpHost) {
                            focused = .smtpPort
                        }
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    }

                    fieldBlock("Port", helper: "465 or 587.") {
                        styledField("587", text: $smtpPort, field: .smtpPort)
                            .keyboardType(.numberPad)
                            .frame(width: 96)
                    }
                }

                Toggle(isOn: $sameLogin.animation(.spring(response: 0.4, dampingFraction: 0.85))) {
                    Text("Use the same login as IMAP")
                        .font(.system(size: 15.5, weight: .medium))
                        .foregroundStyle(.primary)
                }
                .tint(WTheme.accent)
                .padding(.horizontal, 16)
                .frame(height: 56)
                .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 17))

                if !sameLogin {
                    fieldBlock("Username") {
                        styledField("alex@company.com", text: $smtpUsername, field: .smtpUser) {
                            focused = .smtpPass
                        }
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    }
                    .transition(.opacity.combined(with: .move(edge: .top)))

                    fieldBlock("Password") {
                        styledField("App password", text: $smtpPassword, field: .smtpPass, secure: true, submit: .continue) {
                            Task { await advanceIfValid() }
                        }
                    }
                    .transition(.opacity.combined(with: .move(edge: .top)))
                }
            }
        }
        .onAppear {
            if smtpHost.isEmpty { focused = .smtpHost }
        }
    }

    private var donePage: some View {
        VStack(alignment: .leading, spacing: 0) {
            pageTitle(
                "Mailbox connected",
                "Syncing has started. It will appear in your accounts list right away."
            )

            if let account = connectedAccount {
                HStack(spacing: 12) {
                    IconTile(symbol: "envelope.fill", tone: .sky, size: 42)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(account.email)
                            .font(.system(size: 16.5, weight: .semibold))
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        Text(account.providerLabel)
                            .font(.system(size: 13))
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 8)
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 20))
                        .foregroundStyle(.white, WTheme.positive)
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 13)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 17))
                .padding(.bottom, 14)
            }

            warmupBlock
        }
    }

    private var warmupBlock: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                IconTile(symbol: "flame.fill", tone: .orange, size: 42)
                VStack(alignment: .leading, spacing: 4) {
                    Text(warmupOffer == .active ? "Warming up" : "Start warming up")
                        .font(.system(size: 15.5, weight: .semibold))
                        .foregroundStyle(.primary)
                        .contentTransition(.opacity)
                    if warmupOffer == .active {
                        StatusPill(text: "warming", tone: .orange, pulsing: true)
                            .transition(.scale(scale: 0.6).combined(with: .opacity))
                    }
                }
                Spacer(minLength: 8)
                if warmupOffer == .active {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 20))
                        .foregroundStyle(.white, Tone.orange.color)
                        .transition(.scale(scale: 0.4).combined(with: .opacity))
                }
            }

            Text(warmupOffer == .active
                ? "This mailbox is building sender reputation. Track its progress from the accounts list."
                : "Gradually builds sender reputation before campaigns. Starts at 10 a day and ramps automatically.")
                .font(.system(size: 13.5))
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)

            if case let .failed(message) = warmupOffer {
                Text(message)
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.negative)
                    .fixedSize(horizontal: false, vertical: true)
                    .transition(.opacity)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Tone.orange.background.opacity(0.55), in: RoundedRectangle(cornerRadius: 17))
        .overlay(
            RoundedRectangle(cornerRadius: 17)
                .strokeBorder(Tone.orange.color.opacity(0.25), lineWidth: 1)
        )
        .animation(.spring(response: 0.4, dampingFraction: 0.8), value: warmupOffer)
    }

    // MARK: - Field primitives

    private func fieldBlock(_ label: String, helper: String? = nil, @ViewBuilder content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(label)
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(.secondary)
            content()
            if let helper {
                Text(helper)
                    .font(.system(size: 12.5))
                    .foregroundStyle(.tertiary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private func styledField(
        _ placeholder: String,
        text: Binding<String>,
        field: Field,
        secure: Bool = false,
        submit: SubmitLabel = .next,
        onSubmit: @escaping () -> Void = {}
    ) -> some View {
        Group {
            if secure {
                SecureField(placeholder, text: text)
            } else {
                TextField(placeholder, text: text)
            }
        }
        .focused($focused, equals: field)
        .submitLabel(submit)
        .onSubmit(onSubmit)
        .font(.system(size: 16.5))
        .padding(.horizontal, 16)
        .frame(height: 56)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 17))
        .overlay(
            RoundedRectangle(cornerRadius: 17)
                .strokeBorder(focused == field ? WTheme.accent : .clear, lineWidth: 1.8)
        )
        .animation(.easeOut(duration: 0.18), value: focused)
    }

    // MARK: - Flow

    private var trimmedName: String { name.trimmingCharacters(in: .whitespaces) }
    private var trimmedEmail: String { email.trimmingCharacters(in: .whitespaces) }

    private var canAdvance: Bool {
        switch page {
        case .provider:
            return false
        case .connect:
            return true
        case .account:
            return trimmedName.count >= 2 && trimmedEmail.contains("@") && trimmedEmail.contains(".")
        case .imap:
            return !imapHost.trimmingCharacters(in: .whitespaces).isEmpty
                && (Int(imapPort) ?? 0) > 0
        case .smtp:
            let port = Int(smtpPort) ?? 0
            return !smtpHost.trimmingCharacters(in: .whitespaces).isEmpty
                && (port == 465 || port == 587)
        case .done:
            return true
        }
    }

    private func goTo(_ target: Page) {
        focused = nil
        let path = pagePath
        let from = path.firstIndex(of: page) ?? 0
        let to = path.firstIndex(of: target) ?? 0
        direction = to >= from ? 1 : -1
        withAnimation(.spring(response: 0.45, dampingFraction: 0.86)) {
            page = target
        }
    }

    private func goBack() {
        errorMessage = nil
        let path = pagePath
        guard let index = path.firstIndex(of: page), index > 0 else { return }
        let previous = path[index - 1]
        focused = nil
        direction = -1
        withAnimation(.spring(response: 0.45, dampingFraction: 0.86)) {
            page = previous
        }
        if previous == .provider { route = .undecided }
    }

    private func primaryAction() async {
        switch page {
        case .provider:
            break
        case .connect:
            await startOAuth(pendingProvider)
        case .account, .imap, .smtp:
            await advanceIfValid()
        case .done:
            switch warmupOffer {
            case .offer:
                await startWarmup()
            case .active, .failed:
                onClose()
            case .starting:
                break
            }
        }
    }

    private func advanceIfValid() async {
        guard canAdvance else { return }
        errorMessage = nil
        switch page {
        case .account:
            goTo(.imap)
        case .imap:
            goTo(.smtp)
        case .smtp:
            await submitSMTP()
        case .provider, .connect, .done:
            break
        }
    }

    private func showError(_ message: String) {
        withAnimation(.easeOut(duration: 0.2)) {
            errorMessage = message
        }
        errorPulse += 1
    }

    private func finishConnect(_ account: EmailAccount) {
        connectedAccount = account
        onConnected?(account)
        UINotificationFeedbackGenerator().notificationOccurred(.success)
        if route != .smtp { route = .oauth }
        goTo(.done)
    }

    // MARK: - OAuth

    private func startOAuth(_ provider: String) async {
        guard oauthProvider == nil, !busy else { return }
        withAnimation(.easeOut(duration: 0.2)) { errorMessage = nil }
        oauthProvider = provider
        defer { oauthProvider = nil }
        do {
            let start: MailboxOAuthStartResponse = try await env.api.post(
                "emails/onboarding/oauth/start",
                body: MailboxOAuthStartBody(provider: provider)
            )
            guard let url = URL(string: start.url) else {
                showError("The provider returned an invalid authorization URL.")
                return
            }

            let callback = try await oauthFlow.authorize(url: url)
            let items = URLComponents(url: callback, resolvingAgainstBaseURL: false)?.queryItems ?? []
            func query(_ name: String) -> String? {
                items.first { $0.name == name }?.value
            }

            if let providerError = query("error"), !providerError.isEmpty {
                // Declining consent is a cancel, not a failure worth a banner.
                if providerError != "access_denied" {
                    showError("The provider couldn't authorize the connection (\(providerError)).")
                }
                return
            }
            guard query("state") == start.state else {
                showError("Sign-in session mismatch. Please try again.")
                return
            }
            guard let code = query("code"), !code.isEmpty else {
                showError("The provider didn't return an authorization code. Please try again.")
                return
            }

            let account: EmailAccount = try await env.api.post(
                "emails/onboarding/oauth/finish",
                body: MailboxOAuthFinishBody(code: code, state: start.state)
            )
            finishConnect(account)
        } catch MailboxOAuthFlow.Failure.cancelled {
            // User closed the sheet; stay quiet.
        } catch {
            showError((error as? APIError)?.errorDescription ?? error.localizedDescription)
        }
    }

    // MARK: - SMTP submit

    private func submitSMTP() async {
        focused = nil
        errorMessage = nil
        busy = true
        let login = (
            username: imapUsername,
            password: imapPassword
        )
        let body = MailboxSMTPConnectBody(
            email: trimmedEmail,
            name: trimmedName,
            smtp: MailboxServerCredentials(
                username: sameLogin ? login.username : smtpUsername,
                password: sameLogin ? login.password : smtpPassword,
                host: smtpHost.trimmingCharacters(in: .whitespaces),
                port: Int(smtpPort) ?? 587
            ),
            imap: MailboxServerCredentials(
                username: login.username,
                password: login.password,
                host: imapHost.trimmingCharacters(in: .whitespaces),
                port: Int(imapPort) ?? 993
            )
        )
        do {
            let account: EmailAccount = try await env.api.post("emails/onboarding/smtp-imap", body: body)
            busy = false
            finishConnect(account)
        } catch {
            busy = false
            showError((error as? APIError)?.errorDescription ?? error.localizedDescription)
        }
    }

    // MARK: - Warmup offer

    private func startWarmup() async {
        guard let account = connectedAccount, warmupOffer == .offer else { return }
        withAnimation(.easeOut(duration: 0.2)) { warmupOffer = .starting }
        do {
            let updated: EmailAccount = try await env.api.post("emails/\(account.id)/warmup/start")
            connectedAccount = updated
            UINotificationFeedbackGenerator().notificationOccurred(.success)
            withAnimation(.spring(response: 0.4, dampingFraction: 0.8)) {
                warmupOffer = .active
            }
        } catch {
            let message = (error as? APIError)?.errorDescription ?? error.localizedDescription
            withAnimation(.easeOut(duration: 0.2)) {
                warmupOffer = .failed(message)
            }
            errorPulse += 1
        }
    }
}

/// `POST /emails/onboarding/oauth/finish` body (not in MailboxModels).
private struct MailboxOAuthFinishBody: Encodable {
    var code: String
    var state: String
}

/// The mailbox OAuth ceremony: opens the backend-minted authorization URL in
/// an ASWebAuthenticationSession and hands back the `warmbly://email-oauth`
/// callback. Mirrors GoogleSignInFlow's presentation-context pattern.
@MainActor
final class MailboxOAuthFlow: NSObject, ASWebAuthenticationPresentationContextProviding {
    enum Failure: Error {
        case cancelled
        case malformedResponse
    }

    private var activeSession: ASWebAuthenticationSession?
    private var presentationWindow: UIWindow?

    func authorize(url: URL) async throws -> URL {
        // Captured up front (we're on the main actor) so the presentation
        // callback never has to conjure a window of its own.
        guard let window = Self.keyWindow else { throw Failure.malformedResponse }
        presentationWindow = window
        defer { presentationWindow = nil }

        return try await withCheckedThrowingContinuation { continuation in
            let session = ASWebAuthenticationSession(url: url, callbackURLScheme: "warmbly") { [weak self] callbackURL, error in
                Task { @MainActor in self?.activeSession = nil }
                if let error {
                    if let webError = error as? ASWebAuthenticationSessionError, webError.code == .canceledLogin {
                        continuation.resume(throwing: Failure.cancelled)
                    } else {
                        continuation.resume(throwing: error)
                    }
                    return
                }
                guard let callbackURL else {
                    continuation.resume(throwing: Failure.malformedResponse)
                    return
                }
                continuation.resume(returning: callbackURL)
            }
            session.presentationContextProvider = self
            // Keep the user's provider session so re-connects skip the login.
            session.prefersEphemeralWebBrowserSession = false
            activeSession = session
            if !session.start() {
                activeSession = nil
                continuation.resume(throwing: Failure.malformedResponse)
            }
        }
    }

    nonisolated func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        MainActor.assumeIsolated {
            // authorize guards that a window exists before the session starts.
            presentationWindow ?? Self.keyWindow!
        }
    }

    private static var keyWindow: UIWindow? {
        let windows = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
        return windows.first(where: \.isKeyWindow) ?? windows.first
    }
}
