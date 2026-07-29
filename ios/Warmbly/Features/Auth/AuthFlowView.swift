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
    @State private var heroAppeared = false
    @State private var modeSwitchEdge = Edge.trailing

    @FocusState private var focusedField: Field?

    private enum Field { case email, password, recovery }

    /// The email step keeps the big cloud art; deeper steps clear the sky
    /// for the step badge.
    private var heroExpanded: Bool { step == .email }

    var body: some View {
        ZStack(alignment: .bottom) {
            SkyBackdrop(largeCloud: heroExpanded)
            stepScreen
        }
        .sensoryFeedback(.impact(weight: .light), trigger: step)
        .sensoryFeedback(.error, trigger: errorPulse)
        .sheet(isPresented: $showServerConfig) { ServerConfigSheet() }
        .task { providers = await env.session.fetchAuthProviders() }
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
            HStack(spacing: 12) {
                if step != .email {
                    skyIconButton("chevron.left", label: "Back") { goBackOneStep() }
                        .transition(.opacity.combined(with: .scale(scale: 0.7)))
                }
                compactLockup
                Spacer()
                skyIconButton("gearshape", label: "Server settings") { showServerConfig = true }
            }
            .padding(.horizontal, 16)
            .padding(.top, 6)
            // The top bar reflow animates here; the sheet is left alone so it
            // tracks the keyboard's own curve with no gap.
            .animation(.spring(response: 0.45, dampingFraction: 0.86), value: step == .email)

            heroArea
                .animation(.spring(response: 0.5, dampingFraction: 0.85), value: heroExpanded)

            bottomSheet
        }
    }

    /// The brand lockup, permanently docked in the top-left: the sky's center
    /// stays free for the feature showcase.
    private var compactLockup: some View {
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

            HeroFlightScene()

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
                stepIndicator(progress)
                    .transition(.opacity.combined(with: .scale(scale: 0.9)))
            }
        }
        .frame(maxWidth: .infinity, minHeight: 40, maxHeight: .infinity)
        .onAppear {
            withAnimation(.spring(response: 0.7, dampingFraction: 0.75).delay(0.05)) {
                heroAppeared = true
            }
        }
    }

    /// Where the user is in the flow, shown in the freed-up sky on the
    /// deeper steps: the step's icon, name, position, and segment bar.
    private var stepProgress: (index: Int, icon: String, label: String)? {
        switch step {
        case .email, .resetSent:
            return nil
        case .password:
            return (1, "lock.fill", mode == .signIn ? "Your password" : "Choose a password")
        case .verify:
            return (2, "envelope.open.fill", "Check your email")
        case .twoFA:
            return (2, "shield.lefthalf.filled", "Two-factor check")
        }
    }

    /// Picks the tallest step badge that fits the sky left over above the
    /// keyboard: icon bubble + name + segments, or a single compact row.
    private func stepIndicator(_ progress: (index: Int, icon: String, label: String)) -> some View {
        ViewThatFits(in: .vertical) {
            VStack(spacing: 12) {
                ZStack {
                    Circle()
                        .fill(.white.opacity(0.16))
                    Circle()
                        .strokeBorder(.white.opacity(0.3), lineWidth: 1)
                    Image(systemName: progress.icon)
                        .font(.system(size: 20, weight: .semibold))
                        .foregroundStyle(.white)
                        .contentTransition(.symbolEffect(.replace))
                }
                .frame(width: 54, height: 54)

                VStack(spacing: 4) {
                    Text(progress.label)
                        .font(.system(size: 15.5, weight: .bold))
                        .foregroundStyle(.white)
                    Text("Step \(progress.index + 1) of 3")
                        .font(.system(size: 12))
                        .foregroundStyle(.white.opacity(0.7))
                }

                stepSegments(progress.index)
            }
            .padding(.vertical, 16)

            HStack(spacing: 10) {
                ZStack {
                    Circle().fill(.white.opacity(0.16))
                    Circle().strokeBorder(.white.opacity(0.3), lineWidth: 1)
                    Image(systemName: progress.icon)
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(.white)
                        .contentTransition(.symbolEffect(.replace))
                }
                .frame(width: 34, height: 34)

                VStack(alignment: .leading, spacing: 4) {
                    Text(progress.label)
                        .font(.system(size: 13.5, weight: .semibold))
                        .foregroundStyle(.white)
                    HStack(spacing: 7) {
                        stepSegments(progress.index)
                        Text("Step \(progress.index + 1) of 3")
                            .font(.system(size: 11))
                            .foregroundStyle(.white.opacity(0.7))
                    }
                }
            }
            .padding(.vertical, 8)
        }
        .shadow(color: Color(hex: 0x0C4A6E).opacity(0.25), radius: 6, y: 2)
        .animation(.spring(response: 0.4, dampingFraction: 0.85), value: progress.index)
    }

    private func stepSegments(_ active: Int) -> some View {
        HStack(spacing: 6) {
            ForEach(0 ..< 3, id: \.self) { index in
                Capsule()
                    .fill(.white.opacity(index <= active ? 0.95 : 0.3))
                    .frame(width: index == active ? 26 : 14, height: 4)
            }
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
            UnevenRoundedRectangle(cornerRadii: .init(topLeading: 36, topTrailing: 36))
                .fill(Color(.systemBackground)
                    .shadow(.drop(color: Color(hex: 0x0F172A).opacity(0.28), radius: 34, y: -6)))
                .padding(.bottom, -600)
                .ignoresSafeArea()
        }
        .geometryGroup()
    }

    private func skyIconButton(_ systemName: String, label: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: systemName)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(.white)
                .frame(width: 40, height: 40)
                .background(.white.opacity(0.16), in: Circle())
        }
        .buttonStyle(PressableButtonStyle())
        .accessibilityLabel(label)
    }

    private func goBackOneStep() {
        switch step {
        case .password, .resetSent:
            go(to: .email, direction: -1)
        case .verify:
            code = ""
            infoMessage = nil
            go(to: .password, direction: -1)
        case .twoFA:
            restart()
        default:
            break
        }
    }

    // MARK: - Shared sheet pieces

    private func sheetTitle(_ title: String, _ subtitle: String) -> some View {
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

    private func primaryButton(_ title: String, disabled: Bool, action: @escaping () async -> Void) -> some View {
        Button {
            Task { await action() }
        } label: {
            Group {
                if busy {
                    ProgressView().tint(.white)
                } else {
                    Text(title)
                }
            }
            .font(.system(size: 17, weight: .semibold))
            .foregroundStyle(.white)
            .frame(maxWidth: .infinity)
            .frame(height: 56)
            .background(
                LinearGradient(
                    colors: disabled && !busy
                        ? [WTheme.accent.opacity(0.35), WTheme.accent.opacity(0.35)]
                        : [Color(hex: 0x0EA5E9), Color(hex: 0x0284C7)],
                    startPoint: .top,
                    endPoint: .bottom
                ),
                in: RoundedRectangle(cornerRadius: 17)
            )
            .shadow(color: disabled ? .clear : Color(hex: 0x0284C7).opacity(0.32), radius: 12, y: 6)
        }
        .buttonStyle(PressableButtonStyle())
        .disabled(busy || disabled)
        .animation(.easeOut(duration: 0.18), value: disabled)
    }

    private func authField(_ content: some View, focused: Bool) -> some View {
        content
            .font(.system(size: 16.5))
            .padding(.horizontal, 16)
            .frame(height: 56)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 17))
            .overlay(
                RoundedRectangle(cornerRadius: 17)
                    .strokeBorder(focused ? WTheme.accent : .clear, lineWidth: 1.8)
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
            .foregroundStyle(.secondary)
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .background(Color(.secondarySystemBackground), in: Capsule())
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
                    SocialSignInRow(providers: providers, busy: $busy) { message in
                        showError(message)
                    }

                    HStack(spacing: 12) {
                        Rectangle().fill(Color(.separator).opacity(0.45)).frame(height: 1)
                        Text("or")
                            .font(.system(size: 13.5))
                            .foregroundStyle(.tertiary)
                            .fixedSize()
                        Rectangle().fill(Color(.separator).opacity(0.45)).frame(height: 1)
                    }
                    .padding(.vertical, 4)
                }
                .padding(.bottom, 12)
                .transition(.opacity.combined(with: .scale(scale: 0.96, anchor: .top)))
            }

            VStack(spacing: 12) {
                authField(
                    TextField("name@company.com", text: $email)
                        .textContentType(.emailAddress)
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
                .foregroundStyle(WTheme.accent)
                .fontWeight(.semibold)
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
            let session: String
            switch mode {
            case .signIn:
                session = try await env.session.startLogin(email: email, password: password)
                registerFlow = false
            case .signUp:
                session = try await env.session.startRegister(email: email, password: password)
                registerFlow = true
            }
            code = ""
            infoMessage = nil
            resendRemaining = 60
            go(to: .verify(session: session))
        }
    }

    private func submitEmailCode(_ submitted: String) async {
        guard case let .verify(session) = step, !busy else { return }
        await run {
            if registerFlow {
                try await env.session.confirmRegister(session: session, code: submitted)
                // Registration returns no tokens; chain into login with the same credentials.
                let loginSession = try await env.session.startLogin(email: email, password: password)
                registerFlow = false
                code = ""
                resendRemaining = 60
                withAnimation(.easeOut(duration: 0.2)) {
                    infoMessage = "Account created. We emailed you a sign-in code."
                }
                go(to: .verify(session: loginSession))
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
            let session: String
            if registerFlow {
                session = try await env.session.startRegister(email: email, password: password)
            } else {
                session = try await env.session.startLogin(email: email, password: password)
            }
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
                ctx.stroke(trail, with: .color(.white.opacity(0.14)), style: StrokeStyle(lineWidth: 2, lineCap: .round, dash: [2, 9]))
                let phase = (t / 6).truncatingRemainder(dividingBy: 1)
                let window: CGFloat = 0.26
                let segment = trail.trimmedPath(from: CGFloat(phase), to: min(CGFloat(phase) + window, 1))
                ctx.stroke(segment, with: .color(.white.opacity(0.5)), style: StrokeStyle(lineWidth: 2.4, lineCap: .round, dash: [2, 9]))

                // Envelopes rising with a gentle sway, fading at both ends.
                for spec in Self.envelopes {
                    let progress = ((t / spec.period) + spec.phase).truncatingRemainder(dividingBy: 1)
                    let y = size.height * (1.08 - 1.3 * progress)
                    let x = size.width * spec.x + sin(t / 2.6 + spec.phase * 9) * 13
                    let fade = sin(.pi * progress)
                    var image = ctx.resolve(Image(systemName: "envelope.fill"))
                    image.shading = .color(.white.opacity(spec.baseOpacity * fade))
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
                            .fill(.white.opacity(index == slide.id ? 0.95 : 0.35))
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
                .font(.system(size: 18, weight: .bold))
                .tracking(-0.3)
                .foregroundStyle(.white)

            Text(slide.subtitle)
                .font(.system(size: 13.5))
                .foregroundStyle(.white.opacity(0.78))
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, 5)
        }
        .padding(22)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            RoundedRectangle(cornerRadius: 26)
                .fill(.white.opacity(0.13))
                .overlay(
                    RoundedRectangle(cornerRadius: 26)
                        .strokeBorder(
                            LinearGradient(
                                colors: [.white.opacity(0.45), .white.opacity(0.08)],
                                startPoint: .topLeading,
                                endPoint: .bottomTrailing
                            ),
                            lineWidth: 1
                        )
                )
                .shadow(color: Color(hex: 0x0C4A6E).opacity(0.2), radius: 16, y: 9)
        }
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
            .foregroundStyle(.white)
            .padding(.horizontal, 15)
            .frame(height: 36)
            .background(.white.opacity(0.14), in: Capsule())
            .overlay(Capsule().strokeBorder(.white.opacity(0.25), lineWidth: 1))
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
                        Rectangle().fill(.white.opacity(0.1)).frame(height: 1)
                        Spacer()
                    }
                    Rectangle().fill(.white.opacity(0.22)).frame(height: 1)
                }

                Self.area(in: size)
                    .fill(
                        LinearGradient(
                            colors: [.white.opacity(0.22), .white.opacity(0.02)],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )
                    .opacity(progress)

                Self.ramp(in: size)
                    .trim(from: 0, to: progress)
                    .stroke(.white, style: StrokeStyle(lineWidth: 2.5, lineCap: .round))

                Circle()
                    .fill(.white)
                    .frame(width: 7, height: 7)
                    .background(
                        Circle()
                            .fill(.white.opacity(0.3))
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
                    .stroke(.white.opacity(0.18), lineWidth: 9)
                Circle()
                    .trim(from: 0, to: progress * 0.98)
                    .stroke(.white, style: StrokeStyle(lineWidth: 9, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                Text("\(shownValue)%")
                    .font(.system(size: 22, weight: .bold))
                    .foregroundStyle(.white)
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
            .foregroundStyle(.white.opacity(0.85))

            GeometryReader { proxy in
                ZStack(alignment: .leading) {
                    Capsule().fill(.white.opacity(0.18))
                    Capsule()
                        .fill(.white.opacity(0.9))
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
                        .fill(.white.opacity(0.28))
                        .frame(width: 26, height: 26)
                        .overlay(
                            Text(row.initial)
                                .font(.system(size: 11.5, weight: .bold))
                                .foregroundStyle(.white)
                        )

                    VStack(alignment: .leading, spacing: 4) {
                        GeometryReader { proxy in
                            Capsule()
                                .fill(.white.opacity(0.75))
                                .frame(width: proxy.size.width * row.lineWidth, height: 5)
                        }
                        .frame(height: 5)
                        Capsule()
                            .fill(.white.opacity(0.3))
                            .frame(width: 52, height: 5)
                    }

                    if row.replied {
                        Image(systemName: "arrowshape.turn.up.left.fill")
                            .font(.system(size: 11))
                            .foregroundStyle(.white.opacity(0.85))
                    }
                    if row.unread {
                        Circle().fill(.white).frame(width: 7, height: 7)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(.white.opacity(0.1), in: RoundedRectangle(cornerRadius: 13))
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
                        .foregroundStyle(.white.opacity(0.9))
                        .monospacedDigit()
                        .opacity(grown ? 1 : 0)

                    VStack {
                        Spacer(minLength: 0)
                        RoundedRectangle(cornerRadius: 5)
                            .fill(.white.opacity(0.9 - Double(pair.offset) * 0.18))
                            .frame(height: grown ? 66 * bar.fraction + 8 : 6)
                    }
                    .frame(height: 74)

                    Text(bar.label)
                        .font(.system(size: 11.5, weight: .semibold))
                        .foregroundStyle(.white.opacity(0.7))
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

// MARK: - The website's airy sky

/// The marketing site's auth backdrop, recreated from `web/src/global.css`:
/// a radial sky, a slow "breathing" light layer, a warm sun glow top-right,
/// and the site's painterly cloud renders gliding gently back and forth.
struct SkyBackdrop: View {
    var largeCloud = false

    @State private var breathe = false
    @State private var glide = false

    private static let skyStops: [Gradient.Stop] = [
        .init(color: Color(hex: 0x7DD3FC), location: 0),
        .init(color: Color(hex: 0x38BDF8), location: 0.20),
        .init(color: Color(hex: 0x0EA5E9), location: 0.40),
        .init(color: Color(hex: 0x0284C7), location: 0.62),
        .init(color: Color(hex: 0x075985), location: 0.84),
        .init(color: Color(hex: 0x0C4A6E), location: 1),
    ]

    var body: some View {
        GeometryReader { proxy in
            let size = proxy.size
            ZStack {
                // sky-base: radial from the upper area, light to deep.
                RadialGradient(
                    stops: Self.skyStops,
                    center: UnitPoint(x: 0.6, y: 0.18),
                    startRadius: 0,
                    endRadius: max(size.width, size.height) * 1.15
                )

                // sky-breathe: a soft light bloom that swells and fades.
                RadialGradient(
                    stops: [
                        .init(color: Color(hex: 0xBAE6FD).opacity(0.8), location: 0),
                        .init(color: Color(hex: 0x38BDF8).opacity(0.4), location: 0.20),
                        .init(color: Color(hex: 0x0EA5E9).opacity(0.16), location: 0.40),
                        .init(color: .clear, location: 0.62),
                    ],
                    center: UnitPoint(x: 0.6, y: 0.16),
                    startRadius: 0,
                    endRadius: max(size.width, size.height) * 1.05
                )
                .opacity(breathe ? 0.45 : 0.7)
                .scaleEffect(breathe ? 1.05 : 1)

                // sun-glow: warm haze off the top-right corner.
                RadialGradient(
                    stops: [
                        .init(color: Color(hex: 0xFDE68A).opacity(0.34), location: 0),
                        .init(color: Color(hex: 0xFDBA74).opacity(0.12), location: 0.32),
                        .init(color: Color(hex: 0xBAE6FD).opacity(0.05), location: 0.55),
                        .init(color: .clear, location: 1),
                    ],
                    center: .center,
                    startRadius: 0,
                    endRadius: 210
                )
                .frame(width: 420, height: 420)
                .blur(radius: 36)
                .position(x: size.width * 0.96, y: size.height * 0.02)

                // Painterly clouds (the site's pre-rendered webp art),
                // mirroring the web auth layout's placement and glide.
                Image("Cloud3")
                    .resizable()
                    .scaledToFit()
                    .frame(width: 280)
                    .opacity(0.6)
                    .position(x: size.width * -0.16 + 140, y: size.height * 0.04 + 73)
                    .offset(x: glide ? 40 : 0, y: glide ? -10 : 0)

                Image("Cloud4")
                    .resizable()
                    .scaledToFit()
                    .frame(width: 240)
                    .opacity(0.5)
                    .position(x: size.width * 1.14 - 120, y: size.height * 0.88 - 63)
                    .offset(x: glide ? -38 : 0, y: glide ? 8 : 0)

                if largeCloud {
                    Image("Cloud1")
                        .resizable()
                        .scaledToFit()
                        .frame(width: 360)
                        .opacity(0.35)
                        .position(x: size.width * 0.85, y: size.height * 0.42)
                        .offset(x: glide ? -24 : 0, y: glide ? 6 : 0)
                }
            }
        }
        .ignoresSafeArea()
        .onAppear {
            withAnimation(.easeInOut(duration: 15).repeatForever(autoreverses: true)) {
                breathe = true
            }
            withAnimation(.easeInOut(duration: 19).repeatForever(autoreverses: true)) {
                glide = true
            }
        }
    }
}

/// Where the app connects, for local dev or a self-hosted deployment.
/// Presets cover the common cases in one tap; the captcha token and realtime
/// origin live behind Advanced so the everyday view stays simple.
struct ServerConfigSheet: View {
    @Environment(\.dismiss) private var dismiss
    @State private var origin = AppConfig.serverOrigin
    @State private var turnstile = AppConfig.turnstileToken
    @State private var realtime = AppConfig.realtimeOriginOverride
    @State private var showAdvanced = false

    @FocusState private var focused: Field?
    private enum Field { case origin, turnstile, realtime }

    private var isDefault: Bool {
        origin == AppConfig.defaultOrigin
            && turnstile == AppConfig.defaultTurnstileToken
            && realtime.isEmpty
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .center, spacing: 14) {
                    Image(systemName: "globe")
                        .font(.system(size: 20, weight: .semibold))
                        .foregroundStyle(.white)
                        .frame(width: 46, height: 46)
                        .background(
                            LinearGradient(colors: [Color(hex: 0x38BDF8), Color(hex: 0x0284C7)], startPoint: .topLeading, endPoint: .bottomTrailing),
                            in: RoundedRectangle(cornerRadius: 14)
                        )

                    VStack(alignment: .leading, spacing: 3) {
                        Text("Connection")
                            .font(.system(size: 24, weight: .bold))
                            .tracking(-0.5)
                        Text("One app, any Warmbly server.")
                            .font(.system(size: 14))
                            .foregroundStyle(.secondary)
                    }

                    Spacer(minLength: 12)

                    Button {
                        dismiss()
                    } label: {
                        Image(systemName: "xmark")
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundStyle(.secondary)
                            .frame(width: 32, height: 32)
                            .background(Color(.secondarySystemBackground), in: Circle())
                    }
                    .buttonStyle(PressableButtonStyle())
                    .accessibilityLabel("Close")
                }
                .padding(.bottom, 24)

                // One-tap environments.
                HStack(spacing: 10) {
                    presetChip("Warmbly Cloud", icon: "cloud.fill", active: origin == "https://api.warmbly.com") {
                        origin = "https://api.warmbly.com"
                        turnstile = ""
                        realtime = ""
                    }
                    presetChip("Local dev", icon: "laptopcomputer", active: origin == "http://localhost:8080") {
                        origin = "http://localhost:8080"
                        turnstile = "warmbly-local-turnstile-bypass"
                        realtime = ""
                    }
                }
                .padding(.bottom, 24)

                fieldLabel("Server", icon: "server.rack")
                configField(
                    TextField(AppConfig.defaultOrigin, text: $origin)
                        .keyboardType(.URL)
                        .textContentType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .focused($focused, equals: .origin),
                    focused: focused == .origin
                )
                caption("Bare origin, no path. The app calls <origin>/v1. On a physical device, localhost won't reach your Mac; use your Mac's LAN IP or a tunnel URL instead.")
                    .padding(.bottom, 18)

                // Advanced: rarely needed, one quiet toggle, fields styled
                // exactly like the rest of the form.
                Button {
                    withAnimation(.spring(response: 0.4, dampingFraction: 0.85)) {
                        showAdvanced.toggle()
                    }
                } label: {
                    HStack(spacing: 5) {
                        Text(showAdvanced ? "Hide advanced options" : "Advanced options")
                            .font(.system(size: 14, weight: .medium))
                        Image(systemName: "chevron.down")
                            .font(.system(size: 10, weight: .semibold))
                            .rotationEffect(.degrees(showAdvanced ? 180 : 0))
                    }
                    .foregroundStyle(WTheme.accent)
                }
                .buttonStyle(PressableButtonStyle())
                .padding(.vertical, 2)

                if showAdvanced {
                    VStack(alignment: .leading, spacing: 0) {
                        fieldLabel("Captcha token", icon: "checkmark.shield")
                            .padding(.top, 20)
                        configField(
                            TextField("Turnstile token", text: $turnstile)
                                .textInputAutocapitalization(.never)
                                .autocorrectionDisabled()
                                .focused($focused, equals: .turnstile),
                            focused: focused == .turnstile
                        )
                        caption("Only for self-hosted setups. Dev backends accept the bypass token; the hosted service needs nothing here.")
                            .padding(.bottom, 18)

                        fieldLabel("Realtime server", icon: "bolt.horizontal")
                        configField(
                            TextField("Same as server (automatic)", text: $realtime)
                                .keyboardType(.URL)
                                .textInputAutocapitalization(.never)
                                .autocorrectionDisabled()
                                .focused($focused, equals: .realtime),
                            focused: focused == .realtime
                        )
                        caption("Optional. Where live updates connect when your realtime service runs on a different host. Leave empty to let the server decide.")
                    }
                    .transition(.opacity.combined(with: .move(edge: .top)))
                }

                if !isDefault {
                    Button("Reset to default") {
                        withAnimation(.easeOut(duration: 0.2)) {
                            origin = AppConfig.defaultOrigin
                            turnstile = AppConfig.defaultTurnstileToken
                            realtime = ""
                        }
                    }
                    .font(.system(size: 14.5, weight: .medium))
                    .foregroundStyle(WTheme.accent)
                    .frame(maxWidth: .infinity)
                    .padding(.top, 22)
                }
            }
            .padding(24)
        }
        .scrollBounceBehavior(.basedOnSize)
        .safeAreaInset(edge: .bottom) {
            Button {
                AppConfig.serverOrigin = AppConfig.normalizeOrigin(origin)
                AppConfig.turnstileToken = turnstile.trimmingCharacters(in: .whitespaces)
                AppConfig.realtimeOriginOverride = AppConfig.normalizeOrigin(realtime)
                dismiss()
            } label: {
                Text("Save")
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(maxWidth: .infinity)
                    .frame(height: 56)
                    .background(
                        LinearGradient(colors: [Color(hex: 0x0EA5E9), Color(hex: 0x0284C7)], startPoint: .top, endPoint: .bottom),
                        in: RoundedRectangle(cornerRadius: 17)
                    )
                    .shadow(color: Color(hex: 0x0284C7).opacity(0.32), radius: 12, y: 6)
            }
            .buttonStyle(PressableButtonStyle())
            .padding(.horizontal, 24)
            .padding(.top, 10)
            .padding(.bottom, 6)
            .background(Color(.systemBackground))
        }
        .presentationBackground(Color(.systemBackground))
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    private func presetChip(_ title: String, icon: String, active: Bool, action: @escaping () -> Void) -> some View {
        Button {
            withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) { action() }
        } label: {
            HStack(spacing: 7) {
                Image(systemName: icon)
                    .font(.system(size: 12.5))
                Text(title)
                    .font(.system(size: 14, weight: .semibold))
            }
            .foregroundStyle(active ? .white : .primary)
            .padding(.horizontal, 15)
            .frame(height: 38)
            .background(
                active ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(Color(.secondarySystemBackground)),
                in: Capsule()
            )
        }
        .buttonStyle(PressableButtonStyle())
    }

    private func fieldLabel(_ text: String, icon: String) -> some View {
        HStack(spacing: 6) {
            Image(systemName: icon)
                .font(.system(size: 11))
            Text(text.uppercased())
                .font(.system(size: 11, weight: .semibold))
                .tracking(1.3)
        }
        .foregroundStyle(.secondary)
        .padding(.bottom, 8)
    }

    private func configField(_ content: some View, focused: Bool) -> some View {
        content
            .font(.system(size: 15.5))
            .padding(.horizontal, 15)
            .frame(height: 52)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 15))
            .overlay(
                RoundedRectangle(cornerRadius: 15)
                    .strokeBorder(focused ? WTheme.accent : .clear, lineWidth: 1.8)
            )
            .animation(.easeOut(duration: 0.18), value: focused)
            .padding(.bottom, 8)
    }

    private func caption(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 12.5))
            .foregroundStyle(.tertiary)
            .fixedSize(horizontal: false, vertical: true)
    }
}
