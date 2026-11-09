import Combine
import SwiftUI

/// The signed-out experience: the website's airy-sky backdrop (painterly cloud
/// images, breathing gradient, sun glow) with a full-bleed bottom sheet that
/// docks flush to the screen edge and rides the keyboard. Steps mirror the web
/// (email -> password -> emailed 6-digit code -> optional TOTP) plus native
/// Sign in with Apple / Google. Registration confirm returns no tokens, so it
/// chains back into the login flow.
struct AuthFlowView: View {
    private enum Step: Equatable {
        case email
        case password
        case verify(session: String)
        case twoFA(pendingToken: String)
        /// The password was an issued initial one; the holder chooses their own.
        case newPassword(pendingToken: String)
        /// Apple or Google signed in to an address that already has a password.
        case link(SSOLinkChallenge)
        case resetSent
    }

    private enum Mode {
        case signIn, signUp
    }

    @Environment(AppEnvironment.self) private var env

    @State private var step = Step.email
    @State private var direction = 1.0
    @State private var mode = Mode.signIn
    @State private var email = ""
    @State private var password = ""
    @State private var newPassword = ""
    @State private var confirmPassword = ""
    @State private var code = ""
    @State private var recoveryCode = ""
    @State private var useRecovery = false
    @State private var registerFlow = false
    @State private var busy = false
    @State private var errorMessage: String?
    @State private var errorPulse = 0
    @State private var infoMessage: String?
    @State private var resendRemaining = 0
    @State private var providers: AuthProvidersInfo?
    @State private var showServerConfig = false
    /// The server a sign-in in progress is talking to, read when the settings sheet opens.
    @State private var originBeforeSettings = AppConfig.serverOrigin
    /// A chosen password whose save went unanswered, so it may be the account's password now.
    @State private var unconfirmedPassword: String?
    @State private var providersGeneration = 0
    @State private var heroAppeared = false
    @State private var modeSwitchEdge = Edge.trailing

    @FocusState private var focusedField: Field?

    private enum Field { case email, password, newPassword, confirmPassword, recovery }

    /// The email step keeps the big cloud art; deeper steps clear the sky
    /// for the step badge.
    private var heroExpanded: Bool { step == .email }

    var body: some View {
        ZStack(alignment: .bottom) {
            SkyBackdrop()
            stepScreen
        }
        .sensoryFeedback(.impact(weight: .light), trigger: step)
        .sensoryFeedback(.error, trigger: errorPulse)
        // The sheet can change the server, so the sign-in options are read again when it closes.
        .sheet(isPresented: $showServerConfig, onDismiss: settingsClosed) { ServerConfigSheet() }
        .task(id: providersGeneration) {
            providers = nil
            let loaded = await env.session.fetchSignInProviders()
            if !Task.isCancelled { providers = loaded }
        }
        #if DEBUG
        // Scripted runs can open a later step (WARMBLY_DEBUG_AUTH) to see it.
        .onAppear {
            guard let target = ProcessInfo.processInfo.environment["WARMBLY_DEBUG_AUTH"] else { return }
            email = "dev@warmbly.com"
            switch target {
            case "signup": mode = .signUp
            case "password": step = .password
            case "verify": step = .verify(session: "debug")
            case "twofa": step = .twoFA(pendingToken: "debug")
            case "newpassword": step = .newPassword(pendingToken: "debug")
            case "link": step = .link(SSOLinkChallenge(pendingToken: "debug", email: email, provider: "google"))
            case "reset": step = .resetSent
            case "server": openSettings()
            default: break
            }
        }
        #endif
        .onReceive(Timer.publish(every: 1, on: .main, in: .common).autoconnect()) { _ in
            if resendRemaining > 0 {
                withAnimation(.easeOut(duration: 0.3)) { resendRemaining -= 1 }
            }
        }
    }

    private var stepTransition: AnyTransition {
        .asymmetric(
            insertion: .move(edge: direction > 0 ? .trailing : .leading).combined(with: .opacity),
            removal: .move(edge: direction > 0 ? .leading : .trailing).combined(with: .opacity)
        )
    }

    private func go(to next: Step, direction dir: Double = 1) {
        errorMessage = nil
        direction = dir
        withAnimation(.spring(response: 0.45, dampingFraction: 0.86)) {
            step = next
        }
    }

    // MARK: - Screen: sky hero + full-bleed bottom sheet

    private var stepScreen: some View {
        VStack(spacing: 0) {
            FlowTopBar(
                onBack: step == .email ? nil : { goBackOneStep() },
                progress: stepProgress.map { ($0.index, 3) }
            ) {
                FlowSkyButton(symbol: "gearshape", label: "Settings") { openSettings() }
            }

            heroArea
                .animation(.spring(response: 0.5, dampingFraction: 0.85), value: heroExpanded)

            bottomSheet
        }
    }

    /// The sky between the top bar and the sheet. On the email step it shows
    /// the richest thing the available height can hold: the showcase at full
    /// size, the showcase gently scaled down (to ~72%) when the space is only
    /// a little tight (error line, small screens), the one-line ticker when
    /// the keyboard truly eats the room, plain sky as a last resort. Deeper
    /// steps show the step badge. Always a tap-to-dismiss surface, and the
    /// ambience never stops.
    private var heroArea: some View {
        ZStack {
            Color.clear
                .contentShape(Rectangle())
                .onTapGesture { focusedField = nil }

            if step == .email {
                HeroFlightScene()
                    .transition(.opacity)
            }

            if step == .email {
                GeometryReader { proxy in
                    let available = proxy.size.height
                    ZStack {
                        if available >= FeatureShowcase.idealHeight * 0.72 {
                            FeatureShowcase()
                                .scaleEffect(min(1, available / FeatureShowcase.idealHeight))
                        } else if available >= 64 {
                            FeatureTicker()
                        }
                    }
                    .frame(width: proxy.size.width, height: available)
                }
                .scaleEffect(heroAppeared ? 1 : 0.92)
                .opacity(heroAppeared ? 1 : 0)
                .transition(.opacity.combined(with: .scale(scale: 0.94)))
            } else if let progress = stepProgress {
                FlowSkyStage(icon: progress.icon, title: progress.label) { focusedField = nil }
                    .transition(.opacity)
            }
        }
        .frame(maxWidth: .infinity, minHeight: 40, maxHeight: .infinity)
        .onAppear {
            withAnimation(.spring(response: 0.7, dampingFraction: 0.75).delay(0.05)) {
                heroAppeared = true
            }
        }
    }

    /// Where the user is on the deeper steps: the step's position for the top
    /// bar, and its icon and name for the sky.
    private var stepProgress: (index: Int, icon: String, label: String)? {
        switch step {
        case .email, .resetSent:
            return nil
        case .password:
            return (1, "lock.fill", mode == .signIn ? "Your password" : "Choose a password")
        case .newPassword:
            return (2, "key.fill", "Choose your password")
        case .verify:
            return (2, "envelope.open.fill", "Check your email")
        case .twoFA:
            return (2, "shield.lefthalf.filled", "Two-factor check")
        case .link:
            return (1, "link", "Confirm it's you")
        }
    }

    /// The white area: full width, flush to the bottom edge (the background
    /// shape extends under the home indicator and the keyboard), rounded only
    /// at the top. Content resizes with a spring as steps swap.
    private var bottomSheet: some View {
        VStack(alignment: .leading, spacing: 0) {
            Group {
                switch step {
                case .email:
                    emailStep
                case .password:
                    passwordStep
                case .verify:
                    verifyStep
                case .twoFA:
                    twoFAStep
                case .newPassword:
                    newPasswordStep
                case let .link(challenge):
                    linkStep(challenge)
                case .resetSent:
                    resetSentStep
                }
            }
            .transition(stepTransition)

            if infoMessage != nil || errorMessage != nil {
                Group {
                    if let infoMessage {
                        Text(infoMessage)
                            .foregroundStyle(WTheme.positive)
                    }
                    if let errorMessage {
                        Text(errorMessage)
                            .foregroundStyle(WTheme.negative)
                    }
                }
                .font(.system(size: 13.5))
                .frame(maxWidth: .infinity)
                .multilineTextAlignment(.center)
                .padding(.top, 14)
                .transition(.opacity)
            }
        }
        .padding(.horizontal, 24)
        .padding(.top, 30)
        .padding(.bottom, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
        .onTapGesture { focusedField = nil }
        .background {
            // The extra tail below keeps the sheet sealed to the screen edge
            // even mid-animation, when the sheet overshoots above the rising
            // keyboard for a few frames.
            UnevenRoundedRectangle(cornerRadii: .init(topLeading: 32, topTrailing: 32), style: .continuous)
                .fill(WTheme.canvas)
                .overlay(
                    UnevenRoundedRectangle(cornerRadii: .init(topLeading: 32, topTrailing: 32), style: .continuous)
                        .stroke(WTheme.sheetEdge, lineWidth: 1)
                )
                .padding(.bottom, -600)
                .ignoresSafeArea()
        }
        .geometryGroup()
    }

    private func goBackOneStep() {
        switch step {
        case .password, .resetSent:
            go(to: .email, direction: -1)
        case .verify:
            code = ""
            infoMessage = nil
            go(to: .password, direction: -1)
        case .newPassword:
            // A save in flight finishes the sign-in it started.
            guard !busy else { return }
            // The issued password opens a fresh challenge when entered again.
            password = ""
            newPassword = ""
            confirmPassword = ""
            go(to: .password, direction: -1)
        case .twoFA, .link:
            restart()
        default:
            break
        }
    }

    // MARK: - Shared sheet pieces

    private func sheetTitle(_ title: String, _ subtitle: String) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(title)
                .displayFont(32)
                .foregroundStyle(WTheme.ink)
            Text(subtitle)
                .font(.system(size: 15.5))
                .foregroundStyle(WTheme.inkSecondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.bottom, 22)
    }

    private func primaryButton(_ title: String, disabled: Bool, action: @escaping () async -> Void) -> some View {
        Button {
            Task { await action() }
        } label: {
            HStack(spacing: 8) {
                if busy {
                    ProgressView().tint(WTheme.inkInverse)
                } else {
                    Text(title)
                    Image(systemName: "arrow.right")
                        .font(.system(size: 14, weight: .medium))
                }
            }
            .font(.system(size: 16.5, weight: .medium))
            .foregroundStyle(disabled && !busy ? WTheme.inkTertiary : WTheme.inkInverse)
            .frame(maxWidth: .infinity)
            .frame(height: 54)
            .background(disabled && !busy ? WTheme.fill : WTheme.ink, in: Capsule())
        }
        .buttonStyle(PressableButtonStyle())
        .disabled(busy || disabled)
        .animation(.easeOut(duration: 0.18), value: disabled)
    }

    private func authField(_ content: some View, focused: Bool) -> some View {
        content
            .font(.system(size: 16.5))
            .padding(.horizontal, 18)
            .frame(height: 54)
            .background(focused ? WTheme.raised : WTheme.surface, in: Capsule())
            .overlay(
                Capsule().strokeBorder(focused ? WTheme.ink.opacity(0.85) : WTheme.hairlineStrong, lineWidth: focused ? 1.4 : 1)
            )
            .animation(.easeOut(duration: 0.18), value: focused)
    }

    /// The address entered earlier, as a tappable chip that goes back a step.
    private var emailPill: some View {
        Button {
            go(to: .email, direction: -1)
        } label: {
            HStack(spacing: 7) {
                Image(systemName: "envelope")
                    .font(.system(size: 11))
                    .foregroundStyle(.tertiary)
                Text(email)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Image(systemName: "pencil")
                    .font(.system(size: 10.5))
                    .foregroundStyle(.tertiary)
            }
            .font(.system(size: 14))
            .foregroundStyle(WTheme.inkSecondary)
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .background(WTheme.surface, in: Capsule())
            .overlay(Capsule().strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
        }
        .buttonStyle(PressableButtonStyle())
    }

    // MARK: - Step: email (social-first)

    /// Sign-in and create-account are two pages of the same sheet: toggling
    /// pushes the whole step content sideways like a native page turn.
    private var emailStep: some View {
        Group {
            emailStepContent
        }
        .id(mode)
        .transition(.push(from: modeSwitchEdge))
        .sensoryFeedback(.impact(weight: .light), trigger: mode == .signIn)
    }

    /// Shown when the app signed the user out because the session expired or
    /// was revoked, so landing back here never feels like a glitch.
    private var sessionExpiredNotice: some View {
        HStack(spacing: 8) {
            Image(systemName: "clock.badge.exclamationmark")
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(Tone.amber.color)
            Text("You were signed out because your session expired. Sign in to continue.")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Tone.amber.background, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .padding(.bottom, 14)
    }

    private var emailStepContent: some View {
        VStack(alignment: .leading, spacing: 0) {
            if env.session.wasSignedOutBySession, mode == .signIn {
                sessionExpiredNotice
            }
            sheetTitle(
                mode == .signIn ? "Welcome back" : "Create your account",
                mode == .signIn ? "Sign in to pick up where you left off." : "Free to start. No credit card required."
            )

            // Social options collapse while typing so the field and button
            // stay right above the keyboard.
            if focusedField != .email {
                VStack(spacing: 12) {
                    SocialSignInRow(
                        providers: providers,
                        busy: $busy,
                        onError: { message in showError(message) },
                        onChallenge: { outcome in handleSocial(outcome) }
                    )

                    HStack(spacing: 12) {
                        Hairline()
                        Text("OR")
                            .font(WFont.meta(11, weight: .medium))
                            .tracking(0.6)
                            .foregroundStyle(WTheme.inkTertiary)
                            .fixedSize()
                        Hairline()
                    }
                    .padding(.vertical, 4)
                }
                .padding(.bottom, 12)
                .transition(.opacity.combined(with: .scale(scale: 0.96, anchor: .top)))
            }

            VStack(spacing: 12) {
                authField(
                    // .username is what AutoFill matches saved passwords against.
                    TextField("name@company.com", text: $email)
                        .textContentType(.username)
                        .keyboardType(.emailAddress)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .focused($focusedField, equals: .email)
                        .submitLabel(.continue)
                        .onSubmit { continueFromEmail() },
                    focused: focusedField == .email
                )

                primaryButton("Continue with email", disabled: !isPlausibleEmail(email)) {
                    continueFromEmail()
                }
            }

            HStack(spacing: 5) {
                Text(mode == .signIn ? "New to Warmbly?" : "Already have an account?")
                    .foregroundStyle(.secondary)
                Button(mode == .signIn ? "Create an account" : "Sign in") {
                    modeSwitchEdge = mode == .signIn ? .trailing : .leading
                    withAnimation(.spring(response: 0.45, dampingFraction: 0.88)) {
                        mode = mode == .signIn ? .signUp : .signIn
                    }
                }
                .foregroundStyle(WTheme.ink)
                .fontWeight(.medium)
                .underline(true, color: WTheme.ink.opacity(0.25))
            }
            .font(.system(size: 14.5))
            .frame(maxWidth: .infinity)
            .padding(.top, 18)
        }
        .animation(.spring(response: 0.4, dampingFraction: 0.85), value: focusedField)
    }

    private func continueFromEmail() {
        guard isPlausibleEmail(email) else { return }
        password = ""
        go(to: .password)
    }

    // MARK: - Step: password

    private var passwordStep: some View {
        VStack(alignment: .leading, spacing: 0) {
            sheetTitle(
                mode == .signIn ? "Enter your password" : "Choose a password",
                mode == .signIn ? "Use the password for this account." : "At least 8 characters. Make it strong."
            )

            VStack(spacing: 12) {
                HStack {
                    emailPill
                    Spacer()
                }
                .background(autofillUsername)

                authField(
                    SecureField("Password", text: $password)
                        .textContentType(mode == .signIn ? .password : .newPassword)
                        .focused($focusedField, equals: .password)
                        .submitLabel(.go)
                        .onSubmit { Task { await submitCredentials() } },
                    focused: focusedField == .password
                )

                if mode == .signUp, !password.isEmpty {
                    PasswordStrengthBar(password: password)
                        .transition(.opacity.combined(with: .move(edge: .top)))
                }

                primaryButton(
                    mode == .signIn ? "Sign in" : "Create account",
                    disabled: password.count < (mode == .signIn ? 1 : 8)
                ) {
                    await submitCredentials()
                }
            }
            .animation(.spring(response: 0.35, dampingFraction: 0.85), value: password.isEmpty)

            Group {
                if mode == .signIn {
                    Button("Forgot password?") {
                        Task { await submitReset() }
                    }
                    .font(.system(size: 14.5, weight: .medium))
                    .foregroundStyle(WTheme.accent)
                    .disabled(busy)
                } else {
                    termsFootnote
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.top, 18)
        }
        .onAppear { focusedField = .password }
    }

    /// The address again, invisible, beside a password field on its own page:
    /// AutoFill needs a username field there to fill and save the right account.
    private var autofillUsername: some View {
        TextField("", text: $email)
            .textContentType(.username)
            .frame(width: 1, height: 1)
            .opacity(0.01)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }

    // MARK: - Step: choose a password after an issued one

    /// Why the save button is off, once there is enough typed to say.
    private var newPasswordProblem: String? {
        if newPassword.count < 8 { return nil }
        if newPassword == password { return "Choose a password different from the one you were given." }
        if !confirmPassword.isEmpty, confirmPassword != newPassword { return "The passwords don't match." }
        return nil
    }

    private var newPasswordReady: Bool {
        newPassword.count >= 8 && newPassword != password && confirmPassword == newPassword
    }

    private var newPasswordStep: some View {
        VStack(alignment: .leading, spacing: 0) {
            sheetTitle(
                "Choose your password",
                "You signed in with a temporary password. Choose your own to finish signing in. At least 8 characters."
            )

            VStack(spacing: 12) {
                HStack {
                    emailPill
                    Spacer()
                }
                .background(autofillUsername)

                authField(
                    SecureField("New password", text: $newPassword)
                        .textContentType(.newPassword)
                        .focused($focusedField, equals: .newPassword)
                        .submitLabel(.next)
                        .onSubmit { focusedField = .confirmPassword },
                    focused: focusedField == .newPassword
                )

                if !newPassword.isEmpty {
                    PasswordStrengthBar(password: newPassword)
                        .transition(.opacity.combined(with: .move(edge: .top)))
                }

                authField(
                    SecureField("Confirm password", text: $confirmPassword)
                        .textContentType(.newPassword)
                        .focused($focusedField, equals: .confirmPassword)
                        .submitLabel(.go)
                        .onSubmit { Task { await submitNewPassword() } },
                    focused: focusedField == .confirmPassword
                )

                if let newPasswordProblem {
                    Text(newPasswordProblem)
                        .font(.system(size: 13.5))
                        .foregroundStyle(WTheme.inkSecondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .transition(.opacity)
                }

                primaryButton("Save and sign in", disabled: !newPasswordReady) {
                    await submitNewPassword()
                }
            }
            .animation(.spring(response: 0.35, dampingFraction: 0.85), value: newPassword.isEmpty)
            .animation(.easeOut(duration: 0.18), value: newPasswordProblem)
        }
        .onAppear { focusedField = .newPassword }
    }

    // MARK: - Step: link a provider to a password account

    private func providerName(_ provider: String) -> String {
        switch provider {
        case "google": "Google"
        case "apple": "Apple"
        default: "single sign-on"
        }
    }

    private func linkStep(_ challenge: SSOLinkChallenge) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            sheetTitle(
                "Confirm it's you",
                "This address already has a Warmbly account. Enter its password to add \(providerName(challenge.provider)) sign-in to it."
            )

            VStack(spacing: 12) {
                HStack {
                    Text(challenge.email)
                        .lineLimit(1)
                        .truncationMode(.middle)
                        .font(.system(size: 14))
                        .foregroundStyle(WTheme.inkSecondary)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 9)
                        .background(WTheme.surface, in: Capsule())
                        .overlay(Capsule().strokeBorder(WTheme.hairlineStrong, lineWidth: 1))
                    Spacer()
                }
                .background(autofillUsername)

                authField(
                    SecureField("Password", text: $password)
                        .textContentType(.password)
                        .focused($focusedField, equals: .password)
                        .submitLabel(.go)
                        .onSubmit { Task { await submitLink() } },
                    focused: focusedField == .password
                )

                primaryButton("Link and sign in", disabled: password.isEmpty) {
                    await submitLink()
                }
            }
        }
        .onAppear { focusedField = .password }
    }

    private var termsFootnote: some View {
        VStack(spacing: 2) {
            Text("By creating an account, you agree to the")
            HStack(spacing: 4) {
                Link("Terms of Service", destination: URL(string: "https://warmbly.com/terms")!)
                    .foregroundStyle(WTheme.accent)
                Text("and")
                Link("Privacy Policy", destination: URL(string: "https://warmbly.com/privacy")!)
                    .foregroundStyle(WTheme.accent)
            }
        }
        .font(.system(size: 12.5))
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity)
    }

    // MARK: - Step: verify (emailed code)

    private var verifyStep: some View {
        VStack(alignment: .leading, spacing: 0) {
            sheetTitle("Check your email", "We sent a 6-digit code to \(email).")

            VStack(spacing: 18) {
                OTPCodeField(code: $code) { complete in
                    Task { await submitEmailCode(complete) }
                }

                Group {
                    if busy {
                        ProgressView()
                    } else if resendRemaining > 0 {
                        Text("Resend code in \(resendRemaining)s")
                            .font(.system(size: 14.5))
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                            .contentTransition(.numericText(countsDown: true))
                    } else {
                        Button("Resend code") {
                            Task { await resendCode() }
                        }
                        .font(.system(size: 14.5, weight: .semibold))
                        .foregroundStyle(WTheme.accent)
                    }
                }
                .frame(height: 24)
            }
            .frame(maxWidth: .infinity)
        }
    }

    // MARK: - Step: 2FA

    private var twoFAStep: some View {
        VStack(alignment: .leading, spacing: 0) {
            sheetTitle(
                "Two-factor authentication",
                useRecovery ? "Enter one of your recovery codes." : "Enter the code from your authenticator app."
            )

            VStack(spacing: 16) {
                if useRecovery {
                    authField(
                        TextField("xxxxx-xxxxx", text: $recoveryCode)
                            .font(.system(size: 18, design: .monospaced))
                            .multilineTextAlignment(.center)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .focused($focusedField, equals: .recovery)
                            .submitLabel(.go)
                            .onSubmit { Task { await submitTwoFA(recoveryCode) } },
                        focused: focusedField == .recovery
                    )
                    primaryButton("Verify", disabled: recoveryCode.trimmingCharacters(in: .whitespaces).isEmpty) {
                        await submitTwoFA(recoveryCode)
                    }
                } else {
                    OTPCodeField(code: $code) { complete in
                        Task { await submitTwoFA(complete) }
                    }
                    if busy { ProgressView() }
                }

                Button(useRecovery ? "Use your authenticator app" : "Use a recovery code") {
                    withAnimation(.spring(response: 0.4, dampingFraction: 0.85)) {
                        useRecovery.toggle()
                        code = ""
                        recoveryCode = ""
                        errorMessage = nil
                    }
                }
                .font(.system(size: 14.5, weight: .semibold))
                .foregroundStyle(WTheme.accent)
            }
            .frame(maxWidth: .infinity)
        }
    }

    // MARK: - Step: reset sent

    private var resetSentStep: some View {
        VStack(alignment: .leading, spacing: 0) {
            sheetTitle("Reset link sent", "If an account exists for \(email), a reset link is on its way. Complete the reset from the email, then sign in with your new password.")

            primaryButton("Back to sign in", disabled: false) {
                restart()
            }
        }
    }

    // MARK: - Actions

    private func openSettings() {
        // A request in flight is answered by the server it was sent to.
        guard !busy else { return }
        originBeforeSettings = AppConfig.serverOrigin
        showServerConfig = true
    }

    private func settingsClosed() {
        providersGeneration += 1
        guard AppConfig.serverOrigin != originBeforeSettings else { return }
        // Codes, challenges and passwords belong to the server that issued them.
        switch step {
        case .email, .password, .resetSent:
            break
        default:
            password = ""
            newPassword = ""
            confirmPassword = ""
            restart()
        }
    }

    private func restart() {
        code = ""
        recoveryCode = ""
        useRecovery = false
        registerFlow = false
        infoMessage = nil
        go(to: .email, direction: -1)
    }

    private func showError(_ message: String) {
        withAnimation(.easeOut(duration: 0.2)) {
            errorMessage = message
            infoMessage = nil
        }
        errorPulse += 1
    }

    private func run(_ work: () async throws -> Void) async {
        busy = true
        errorMessage = nil
        do {
            try await work()
        } catch {
            showError((error as? APIError)?.errorDescription ?? error.localizedDescription)
        }
        busy = false
    }

    private func submitCredentials() async {
        guard !busy, password.count >= (mode == .signIn ? 1 : 8) else { return }
        await run {
            switch mode {
            case .signIn:
                registerFlow = false
                // A deployment can sign someone in without emailing a code, so
                // the first call may already be the whole login.
                let outcome = try await env.session.startLogin(email: email, password: password)
                switch outcome {
                case .loggedIn:
                    return
                case let .needsTwoFA(pendingToken):
                    code = ""
                    go(to: .twoFA(pendingToken: pendingToken))
                    return
                case let .needsCode(session):
                    code = ""
                    infoMessage = nil
                    resendRemaining = 60
                    go(to: .verify(session: session))
                case let .needsNewPassword(pendingToken):
                    showNewPassword(pendingToken)
                }
            case .signUp:
                registerFlow = true
                // With email verification off the account already exists, so
                // fall straight through to signing in.
                guard let session = try await env.session.startRegister(email: email, password: password) else {
                    registerFlow = false
                    let outcome = try await env.session.startLogin(email: email, password: password)
                    switch outcome {
                    case .loggedIn:
                        return
                    case let .needsTwoFA(pendingToken):
                        code = ""
                        go(to: .twoFA(pendingToken: pendingToken))
                    case let .needsCode(loginSession):
                        code = ""
                        resendRemaining = 60
                        go(to: .verify(session: loginSession))
                    case let .needsNewPassword(pendingToken):
                        showNewPassword(pendingToken)
                    }
                    return
                }
                code = ""
                infoMessage = nil
                resendRemaining = 60
                go(to: .verify(session: session))
            }
        }
    }

    private func submitEmailCode(_ submitted: String) async {
        guard case let .verify(session) = step, !busy else { return }
        await run {
            if registerFlow {
                try await env.session.confirmRegister(session: session, code: submitted)
                // Registration returns no tokens; chain into login with the same credentials.
                let outcome = try await env.session.startLogin(email: email, password: password)
                registerFlow = false
                code = ""
                resendRemaining = 60
                switch outcome {
                case .loggedIn:
                    return
                case let .needsTwoFA(pendingToken):
                    go(to: .twoFA(pendingToken: pendingToken))
                case let .needsCode(loginSession):
                    withAnimation(.easeOut(duration: 0.2)) {
                        infoMessage = "Account created. We emailed you a sign-in code."
                    }
                    go(to: .verify(session: loginSession))
                case let .needsNewPassword(pendingToken):
                    showNewPassword(pendingToken)
                }
            } else {
                let outcome = try await env.session.confirmLogin(session: session, code: submitted)
                if case let .needsTwoFA(pendingToken) = outcome {
                    code = ""
                    go(to: .twoFA(pendingToken: pendingToken))
                }
            }
        }
        if errorMessage != nil { code = "" }
    }

    private func resendCode() async {
        guard case .verify = step else { return }
        await run {
            let session: String?
            if registerFlow {
                session = try await env.session.startRegister(email: email, password: password)
            } else {
                if case let .needsCode(loginSession) = try await env.session.startLogin(email: email, password: password) {
                    session = loginSession
                } else {
                    // The retry resolved the login outright; nothing to resend.
                    return
                }
            }
            guard let session else { return }
            code = ""
            resendRemaining = 60
            step = .verify(session: session)
            withAnimation(.easeOut(duration: 0.2)) {
                infoMessage = "Code resent."
            }
        }
    }

    private func submitTwoFA(_ submitted: String) async {
        guard case let .twoFA(pendingToken) = step, !busy else { return }
        await run {
            try await env.session.verifyTwoFA(pendingToken: pendingToken, code: submitted.trimmingCharacters(in: .whitespaces))
        }
        if errorMessage != nil { code = "" }
    }

    private func showNewPassword(_ pendingToken: String) {
        unconfirmedPassword = nil
        newPassword = ""
        confirmPassword = ""
        infoMessage = nil
        go(to: .newPassword(pendingToken: pendingToken))
    }

    private func submitNewPassword() async {
        guard case let .newPassword(pendingToken) = step, !busy, newPasswordReady else { return }
        let chosen = newPassword
        var expired = false
        await run {
            do {
                let outcome = try await env.session.chooseInitialPassword(pendingToken: pendingToken, password: chosen)
                guard step == .newPassword(pendingToken: pendingToken) else { return }
                // A resent code signs in again, now with the chosen password.
                password = chosen
                unconfirmedPassword = nil
                newPassword = ""
                confirmPassword = ""
                switch outcome {
                case .loggedIn:
                    return
                case let .needsTwoFA(pending):
                    code = ""
                    go(to: .twoFA(pendingToken: pending))
                case let .needsCode(session):
                    code = ""
                    resendRemaining = 60
                    withAnimation(.easeOut(duration: 0.2)) {
                        infoMessage = "Password saved. We emailed you a sign-in code."
                    }
                    go(to: .verify(session: session))
                case let .needsNewPassword(pending):
                    showNewPassword(pending)
                }
            } catch {
                guard step == .newPassword(pendingToken: pendingToken) else { return }
                expired = (error as? APIError)?.code == "password_change_expired"
                if !expired, !Self.refusedBeforeSaving(error) { unconfirmedPassword = chosen }
                throw error
            }
        }
        // The challenge is spent, expired or voided by a reissue: only a new sign-in continues.
        if expired, step == .newPassword(pendingToken: pendingToken) {
            let message = errorMessage
            newPassword = ""
            confirmPassword = ""
            if let saved = unconfirmedPassword {
                // An earlier attempt may have saved it before its answer was lost.
                unconfirmedPassword = nil
                password = saved
                go(to: .password, direction: -1)
                withAnimation(.easeOut(duration: 0.2)) {
                    infoMessage = "Your new password may already be saved, so it is filled in. Continue with it, or enter the password you were given if it is refused."
                }
            } else {
                password = ""
                go(to: .password, direction: -1)
                if let message { showError(message) }
            }
        }
    }

    /// A refusal the server gives before it writes the password, so the issued one still stands.
    private static func refusedBeforeSaving(_ error: Error) -> Bool {
        guard let code = (error as? APIError)?.code else { return false }
        return ["password_reused", "password_breached", "sessions_not_ended"].contains(code)
    }

    private func handleSocial(_ outcome: SocialOutcome) {
        switch outcome {
        case .loggedIn:
            return
        case let .needsTwoFA(pendingToken):
            code = ""
            go(to: .twoFA(pendingToken: pendingToken))
        case let .needsLink(challenge):
            email = challenge.email
            password = ""
            go(to: .link(challenge))
        }
    }

    private func submitLink() async {
        guard case let .link(challenge) = step, !busy, !password.isEmpty else { return }
        var expired = false
        await run {
            do {
                let outcome = try await env.session.confirmSSOLink(pendingToken: challenge.pendingToken, password: password)
                // Back may have abandoned this attempt while it was in flight.
                guard step == .link(challenge) else { return }
                handleSocial(outcome)
            } catch {
                guard step == .link(challenge) else { return }
                expired = (error as? APIError)?.code == "sso_link_expired"
                throw error
            }
        }
        // The challenge is spent, so the form can only fail from here.
        if expired, step == .link(challenge) {
            let message = errorMessage
            restart()
            if let message { showError(message) }
        }
    }

    private func submitReset() async {
        guard isPlausibleEmail(email) else {
            showError("Enter your email address first.")
            return
        }
        await run {
            try await env.session.requestPasswordReset(email: email)
            go(to: .resetSent)
        }
    }

    private func isPlausibleEmail(_ value: String) -> Bool {
        let trimmed = value.trimmingCharacters(in: .whitespaces)
        guard let at = trimmed.firstIndex(of: "@"), at != trimmed.startIndex else { return false }
        let domain = trimmed[trimmed.index(after: at)...]
        return domain.contains(".") && !domain.hasPrefix(".") && !domain.hasSuffix(".")
    }
}

// MARK: - Hero ambience

/// Ambient motion around the hero mark: a dashed flight path that keeps
/// redrawing itself across the sky, and a few envelopes drifting upward at
/// different depths. Pure decoration, drawn in one Canvas pass.
struct HeroFlightScene: View {
    private struct EnvelopeSpec {
        let x: CGFloat
        let size: CGFloat
        let period: Double
        let phase: Double
        let baseOpacity: Double
    }

    private static let envelopes: [EnvelopeSpec] = [
        EnvelopeSpec(x: 0.14, size: 20, period: 13, phase: 0.0, baseOpacity: 0.4),
        EnvelopeSpec(x: 0.84, size: 15, period: 17, phase: 0.45, baseOpacity: 0.3),
        EnvelopeSpec(x: 0.32, size: 12, period: 21, phase: 0.8, baseOpacity: 0.22),
    ]

    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 40)) { context in
            let t = context.date.timeIntervalSinceReferenceDate
            Canvas { ctx, size in
                let rect = CGRect(origin: .zero, size: size)
                let trail = Self.trailPath(in: rect)

                // The faint full route, plus a brighter segment forever
                // travelling along it like a gust carrying the plane.
                ctx.stroke(trail, with: .color(WTheme.onSky.opacity(0.14)), style: StrokeStyle(lineWidth: 2, lineCap: .round, dash: [2, 9]))
                let phase = (t / 6).truncatingRemainder(dividingBy: 1)
                let window: CGFloat = 0.26
                let segment = trail.trimmedPath(from: CGFloat(phase), to: min(CGFloat(phase) + window, 1))
                ctx.stroke(segment, with: .color(WTheme.onSky.opacity(0.5)), style: StrokeStyle(lineWidth: 2.4, lineCap: .round, dash: [2, 9]))

                // Envelopes rising with a gentle sway, fading at both ends.
                for spec in Self.envelopes {
                    let progress = ((t / spec.period) + spec.phase).truncatingRemainder(dividingBy: 1)
                    let y = size.height * (1.08 - 1.3 * progress)
                    let x = size.width * spec.x + sin(t / 2.6 + spec.phase * 9) * 13
                    let fade = sin(.pi * progress)
                    var image = ctx.resolve(Image(systemName: "envelope.fill"))
                    image.shading = .color(WTheme.onSky.opacity(spec.baseOpacity * fade))
                    let box = CGRect(x: x - spec.size / 2, y: y - spec.size / 2, width: spec.size, height: spec.size * 0.72)
                    ctx.draw(image, in: box)
                }
            }
        }
        .allowsHitTesting(false)
    }

    /// A wind current arcing over the mark from off-screen left to right.
    private static func trailPath(in rect: CGRect) -> Path {
        var path = Path()
        path.move(to: CGPoint(x: rect.minX - 24, y: rect.height * 0.66))
        path.addCurve(
            to: CGPoint(x: rect.midX, y: rect.height * 0.14),
            control1: CGPoint(x: rect.width * 0.18, y: rect.height * 0.62),
            control2: CGPoint(x: rect.width * 0.3, y: rect.height * 0.14)
        )
        path.addCurve(
            to: CGPoint(x: rect.maxX + 24, y: rect.height * 0.5),
            control1: CGPoint(x: rect.width * 0.72, y: rect.height * 0.14),
            control2: CGPoint(x: rect.width * 0.86, y: rect.height * 0.42)
        )
        return path
    }
}

// MARK: - Feature showcase

/// Rotating product vignettes filling the sky while the email step is at
/// rest: glass cards with animated white-line scenes of what Warmbly does.
/// Auto-advances, swipeable, dots to jump.
struct FeatureShowcase: View {
    /// Natural height (cards + dots); the auth screen scales the whole
    /// showcase down from this when the sky gets a little tight.
    static let idealHeight: CGFloat = 256

    private struct Slide: Identifiable {
        let id: Int
        let title: String
        let subtitle: String
    }

    private static let slides: [Slide] = [
        Slide(id: 0, title: "Warm up on autopilot", subtitle: "Every inbox ramps up gradually and builds sender reputation on its own."),
        Slide(id: 1, title: "Land in the inbox", subtitle: "Watch deliverability live and catch spam placement before it hurts."),
        Slide(id: 2, title: "Every reply, one place", subtitle: "All your mailboxes and your team in a single unified inbox."),
        Slide(id: 3, title: "Know what works", subtitle: "Opens, clicks, and replies per campaign, at a glance."),
    ]

    @State private var index = 0
    @State private var float = false

    var body: some View {
        VStack(spacing: 13) {
            TabView(selection: $index) {
                ForEach(Self.slides) { slide in
                    card(slide)
                        .padding(.horizontal, 5)
                        .tag(slide.id)
                }
            }
            .tabViewStyle(.page(indexDisplayMode: .never))
            .frame(height: 226)

            HStack(spacing: 7) {
                ForEach(Self.slides) { slide in
                    Button {
                        withAnimation(.spring(response: 0.45, dampingFraction: 0.85)) {
                            index = slide.id
                        }
                    } label: {
                        Capsule()
                            .fill(WTheme.onSky.opacity(index == slide.id ? 0.95 : 0.35))
                            .frame(width: index == slide.id ? 22 : 7, height: 7)
                    }
                    .buttonStyle(.plain)
                }
            }
            .animation(.spring(response: 0.4, dampingFraction: 0.85), value: index)
        }
        .padding(.bottom, 10)
        .offset(y: float ? -3 : 3)
        .sensoryFeedback(.selection, trigger: index)
        .onAppear {
            withAnimation(.easeInOut(duration: 6).repeatForever(autoreverses: true)) {
                float = true
            }
        }
        .onReceive(Timer.publish(every: 5, on: .main, in: .common).autoconnect()) { _ in
            withAnimation(.spring(response: 0.55, dampingFraction: 0.86)) {
                index = (index + 1) % Self.slides.count
            }
        }
    }

    private func card(_ slide: Slide) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            visual(for: slide.id)
                .frame(maxWidth: .infinity)
                .frame(height: 100)
                .padding(.bottom, 14)

            Text(slide.title)
                .displayFont(20)
                .foregroundStyle(WTheme.onSky)

            Text(slide.subtitle)
                .font(.system(size: 13.5))
                .foregroundStyle(WTheme.onSky.opacity(0.6))
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, 5)
        }
        .padding(22)
        .frame(maxWidth: .infinity, alignment: .leading)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 24, style: .continuous))
    }

    @ViewBuilder
    private func visual(for id: Int) -> some View {
        switch id {
        case 0: WarmupRampVisual()
        case 1: InboxPlacementVisual()
        case 2: UnifiedInboxVisual()
        default: CampaignStatsVisual()
        }
    }
}

/// The showcase distilled to one line: a glass chip cycling through the
/// feature headlines, for the slim strip of sky left above the keyboard.
struct FeatureTicker: View {
    private static let items: [(icon: String, text: String)] = [
        ("chart.line.uptrend.xyaxis", "Warm up on autopilot"),
        ("checkmark.seal.fill", "Land in the inbox"),
        ("arrowshape.turn.up.left.fill", "Every reply, one place"),
        ("chart.bar.fill", "Know what works"),
    ]

    @State private var index = 0

    var body: some View {
        ZStack {
            HStack(spacing: 8) {
                Image(systemName: Self.items[index].icon)
                    .font(.system(size: 12, weight: .semibold))
                Text(Self.items[index].text)
                    .font(.system(size: 13.5, weight: .semibold))
                    .fixedSize()
            }
            .foregroundStyle(WTheme.onSky)
            .padding(.horizontal, 15)
            .frame(height: 36)
            .glassEffect(.regular, in: Capsule())
            .id(index)
            .transition(.blurReplace)
        }
        .onReceive(Timer.publish(every: 3.5, on: .main, in: .common).autoconnect()) { _ in
            withAnimation(.spring(response: 0.5, dampingFraction: 0.85)) {
                index = (index + 1) % Self.items.count
            }
        }
    }
}

/// A send-volume ramp drawing itself in over faint gridlines, with a glowing
/// live dot at today's volume.
struct WarmupRampVisual: View {
    @State private var progress: CGFloat = 0
    @State private var pulse = false

    var body: some View {
        GeometryReader { proxy in
            let size = proxy.size
            ZStack(alignment: .topLeading) {
                VStack(spacing: 0) {
                    ForEach(0 ..< 3, id: \.self) { _ in
                        Rectangle().fill(WTheme.onSky.opacity(0.1)).frame(height: 1)
                        Spacer()
                    }
                    Rectangle().fill(WTheme.onSky.opacity(0.22)).frame(height: 1)
                }

                Self.area(in: size)
                    .fill(
                        LinearGradient(
                            colors: [WTheme.onSky.opacity(0.22), WTheme.onSky.opacity(0.02)],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )
                    .opacity(progress)

                Self.ramp(in: size)
                    .trim(from: 0, to: progress)
                    .stroke(WTheme.onSky, style: StrokeStyle(lineWidth: 2.5, lineCap: .round))

                Circle()
                    .fill(WTheme.onSky)
                    .frame(width: 7, height: 7)
                    .background(
                        Circle()
                            .fill(WTheme.onSky.opacity(0.3))
                            .frame(width: pulse ? 24 : 10, height: pulse ? 24 : 10)
                    )
                    .position(x: size.width - 2, y: size.height * 0.08)
                    .opacity(progress > 0.96 ? 1 : 0)
                    .animation(.easeOut(duration: 0.25), value: progress > 0.96)
            }
        }
        .onAppear {
            withAnimation(.easeOut(duration: 1.4).delay(0.25)) { progress = 1 }
            withAnimation(.easeInOut(duration: 1.3).repeatForever(autoreverses: true).delay(1.7)) { pulse = true }
        }
    }

    private static func ramp(in size: CGSize) -> Path {
        var path = Path()
        path.move(to: CGPoint(x: 2, y: size.height * 0.94))
        path.addCurve(
            to: CGPoint(x: size.width * 0.55, y: size.height * 0.6),
            control1: CGPoint(x: size.width * 0.28, y: size.height * 0.94),
            control2: CGPoint(x: size.width * 0.4, y: size.height * 0.72)
        )
        path.addCurve(
            to: CGPoint(x: size.width - 2, y: size.height * 0.08),
            control1: CGPoint(x: size.width * 0.72, y: size.height * 0.47),
            control2: CGPoint(x: size.width * 0.86, y: size.height * 0.1)
        )
        return path
    }

    private static func area(in size: CGSize) -> Path {
        var path = ramp(in: size)
        path.addLine(to: CGPoint(x: size.width - 2, y: size.height))
        path.addLine(to: CGPoint(x: 2, y: size.height))
        path.closeSubpath()
        return path
    }
}

/// A deliverability gauge sweeping up to 98% inbox placement, with the
/// folder split alongside.
struct InboxPlacementVisual: View {
    @State private var progress: CGFloat = 0
    @State private var shownValue = 0

    var body: some View {
        HStack(spacing: 26) {
            ZStack {
                Circle()
                    .stroke(WTheme.onSky.opacity(0.18), lineWidth: 9)
                Circle()
                    .trim(from: 0, to: progress * 0.98)
                    .stroke(WTheme.onSky, style: StrokeStyle(lineWidth: 9, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                Text("\(shownValue)%")
                    .font(.system(size: 22, weight: .bold))
                    .foregroundStyle(WTheme.onSky)
                    .monospacedDigit()
                    .contentTransition(.numericText())
            }
            .frame(width: 96, height: 96)

            VStack(alignment: .leading, spacing: 13) {
                placementRow(icon: "tray.full.fill", label: "Inbox", fraction: 0.98)
                placementRow(icon: "xmark.bin", label: "Spam", fraction: 0.02)
            }
        }
        .padding(.horizontal, 4)
        .onAppear {
            withAnimation(.easeOut(duration: 1.3).delay(0.25)) {
                progress = 1
                shownValue = 98
            }
        }
    }

    private func placementRow(icon: String, label: String, fraction: CGFloat) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .font(.system(size: 10.5))
                Text(label)
                    .font(.system(size: 12.5, weight: .semibold))
            }
            .foregroundStyle(WTheme.onSky.opacity(0.85))

            GeometryReader { proxy in
                ZStack(alignment: .leading) {
                    Capsule().fill(WTheme.onSky.opacity(0.18))
                    Capsule()
                        .fill(WTheme.onSky.opacity(0.9))
                        .frame(width: max(5, proxy.size.width * fraction * progress))
                }
            }
            .frame(height: 5)
        }
    }
}

/// Message rows from different providers sliding into one inbox, one of them
/// already answered.
struct UnifiedInboxVisual: View {
    @State private var shown = false

    private struct Row {
        let initial: String
        let lineWidth: CGFloat
        let replied: Bool
        let unread: Bool
    }

    private static let rows: [Row] = [
        Row(initial: "G", lineWidth: 0.74, replied: false, unread: true),
        Row(initial: "M", lineWidth: 0.52, replied: true, unread: false),
        Row(initial: "O", lineWidth: 0.63, replied: false, unread: false),
    ]

    var body: some View {
        VStack(spacing: 8) {
            ForEach(Array(Self.rows.enumerated()), id: \.offset) { pair in
                let row = pair.element
                HStack(spacing: 10) {
                    Circle()
                        .fill(WTheme.onSky.opacity(0.28))
                        .frame(width: 26, height: 26)
                        .overlay(
                            Text(row.initial)
                                .font(.system(size: 11.5, weight: .bold))
                                .foregroundStyle(WTheme.onSky)
                        )

                    VStack(alignment: .leading, spacing: 4) {
                        GeometryReader { proxy in
                            Capsule()
                                .fill(WTheme.onSky.opacity(0.75))
                                .frame(width: proxy.size.width * row.lineWidth, height: 5)
                        }
                        .frame(height: 5)
                        Capsule()
                            .fill(WTheme.onSky.opacity(0.3))
                            .frame(width: 52, height: 5)
                    }

                    if row.replied {
                        Image(systemName: "arrowshape.turn.up.left.fill")
                            .font(.system(size: 11))
                            .foregroundStyle(WTheme.onSky.opacity(0.85))
                    }
                    if row.unread {
                        Circle().fill(WTheme.onSky).frame(width: 7, height: 7)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(WTheme.skyTile(0.1), in: RoundedRectangle(cornerRadius: 13))
                .offset(x: shown ? 0 : 46)
                .opacity(shown ? 1 : 0)
                .animation(
                    .spring(response: 0.55, dampingFraction: 0.8).delay(0.2 + Double(pair.offset) * 0.13),
                    value: shown
                )
            }
        }
        .onAppear { shown = true }
    }
}

/// Campaign results growing in: opens, clicks, replies as labeled bars.
struct CampaignStatsVisual: View {
    @State private var grown = false

    private static let bars: [(label: String, fraction: CGFloat)] = [
        ("Opened", 0.72),
        ("Clicked", 0.44),
        ("Replied", 0.2),
    ]

    var body: some View {
        HStack(alignment: .bottom, spacing: 22) {
            ForEach(Array(Self.bars.enumerated()), id: \.offset) { pair in
                let bar = pair.element
                VStack(spacing: 7) {
                    Text("\(Int(bar.fraction * 100))%")
                        .font(.system(size: 12, weight: .bold))
                        .foregroundStyle(WTheme.onSky.opacity(0.9))
                        .monospacedDigit()
                        .opacity(grown ? 1 : 0)

                    VStack {
                        Spacer(minLength: 0)
                        RoundedRectangle(cornerRadius: 5)
                            .fill(WTheme.onSky.opacity(0.9 - Double(pair.offset) * 0.18))
                            .frame(height: grown ? 66 * bar.fraction + 8 : 6)
                    }
                    .frame(height: 74)

                    Text(bar.label)
                        .font(.system(size: 11.5, weight: .semibold))
                        .foregroundStyle(WTheme.onSky.opacity(0.7))
                }
                .frame(width: 62)
                .animation(
                    .spring(response: 0.6, dampingFraction: 0.75).delay(0.2 + Double(pair.offset) * 0.12),
                    value: grown
                )
            }
        }
        .onAppear { grown = true }
    }
}

// MARK: - Button press feel

/// Springy scale-down on press, shared by every tappable in the auth flow.
struct PressableButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.965 : 1)
            .opacity(configuration.isPressed ? 0.88 : 1)
            .animation(.spring(response: 0.28, dampingFraction: 0.7), value: configuration.isPressed)
    }
}

// MARK: - Password strength

/// Lightweight local strength meter (the server only enforces the 8-char
/// minimum; this is guidance, not a gate).
struct PasswordStrengthBar: View {
    let password: String

    private var assessment: (score: Int, label: String, color: Color) {
        var score = 0
        if password.count >= 8 { score += 1 }
        if password.count >= 12 { score += 1 }
        var classes = 0
        if password.contains(where: \.isLowercase) { classes += 1 }
        if password.contains(where: \.isUppercase) { classes += 1 }
        if password.contains(where: \.isNumber) { classes += 1 }
        if password.contains(where: { !$0.isLetter && !$0.isNumber }) { classes += 1 }
        if classes >= 2 { score += 1 }
        if classes >= 3, password.count >= 10 { score += 1 }

        switch score {
        case 0 ... 1: return (max(score, 1), "Weak", WTheme.negative)
        case 2: return (2, "Fair", WTheme.warning)
        case 3: return (3, "Good", WTheme.info)
        default: return (4, "Strong", WTheme.positive)
        }
    }

    var body: some View {
        let result = assessment
        VStack(alignment: .leading, spacing: 6) {
            GeometryReader { proxy in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color(.secondarySystemBackground))
                    Capsule()
                        .fill(result.color)
                        .frame(width: proxy.size.width * CGFloat(result.score) / 4)
                        .animation(.spring(response: 0.35, dampingFraction: 0.8), value: result.score)
                }
            }
            .frame(height: 5)

            Text("\(result.label) password")
                .font(.system(size: 12.5))
                .foregroundStyle(.secondary)
        }
    }
}

// MARK: - Brand backdrop

/// The brand moment behind sign-in and onboarding: the site's sheer sky photo
/// (its night variant in dark mode), drifting slowly under a veil that fades
/// into the sheet. Content on it uses `WTheme.onSky`.
struct SkyBackdrop: View {
    @Environment(\.colorScheme) private var scheme
    @State private var drift = false

    var body: some View {
        GeometryReader { proxy in
            Image("SheerSky")
                .resizable()
                .scaledToFill()
                .frame(width: proxy.size.width * 1.12, height: proxy.size.height)
                .offset(x: proxy.size.width * (drift ? -0.1 : -0.02))
                .frame(width: proxy.size.width, height: proxy.size.height, alignment: .leading)
                .clipped()
                .overlay { veil }
        }
        .ignoresSafeArea()
        .onAppear {
            withAnimation(.easeInOut(duration: 14).repeatForever(autoreverses: true)) {
                drift = true
            }
        }
    }

    /// Keeps ink readable on the folds and fades into the sheet below.
    private var veil: some View {
        let edge: Color = scheme == .dark ? .black : .white
        let stops: [Double] = scheme == .dark ? [0.2, 0, 0.12] : [0.42, 0.08, 0.3]
        return LinearGradient(
            colors: [edge.opacity(stops[0]), edge.opacity(stops[1]), edge.opacity(stops[2]), WTheme.canvas],
            startPoint: .top,
            endPoint: .bottom
        )
    }
}
