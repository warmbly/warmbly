import SwiftUI

/// Post-signup questionnaire (mirrors the web's /onboarding gate) wearing the
/// same air as the sign-in screen: the sky with its flight ambience and a
/// step badge up top, a full-bleed white sheet with the questions below.
/// Name and referral source are required, persona and team size optional.
/// Runs before org resolution so the profile is complete on first entry.
struct OnboardingFlowView: View {
    private enum Page: Int, CaseIterable {
        case name, referral, role, teamSize

        var icon: String {
            switch self {
            case .name: "person.fill"
            case .referral: "sparkles"
            case .role: "briefcase.fill"
            case .teamSize: "person.3.fill"
            }
        }

        var skyLabel: String {
            switch self {
            case .name: "Introduce yourself"
            case .referral: "How you found us"
            case .role: "What you do"
            case .teamSize: "Your team"
            }
        }
    }

    @Environment(AppEnvironment.self) private var env

    @State private var page = Page.name
    @State private var direction = 1.0
    @State private var firstName = ""
    @State private var lastName = ""
    @State private var referralSource: String?
    @State private var role: String?
    @State private var teamSize: String?
    @State private var busy = false
    @State private var errorMessage: String?
    @State private var errorPulse = 0
    @State private var badgeAppeared = false

    @FocusState private var focusedName: NameField?
    private enum NameField { case first, last }

    private static let referralOptions: [(value: String, label: String, icon: String)] = [
        ("google", "Google search", "magnifyingglass"),
        ("x", "X (Twitter)", "at"),
        ("reddit", "Reddit", "bubble.left.and.bubble.right"),
        ("facebook", "Facebook", "person.2"),
        ("other", "Somewhere else", "sparkles"),
    ]

    private static let roleOptions: [(value: String, label: String, icon: String)] = [
        ("founder", "Founder", "flag"),
        ("sales", "Sales", "chart.line.uptrend.xyaxis"),
        ("marketing", "Marketing", "megaphone"),
        ("agency", "Agency", "building.2"),
        ("recruiter", "Recruiter", "person.badge.plus"),
        ("other", "Something else", "ellipsis.circle"),
    ]

    private static let teamSizeOptions: [(value: String, label: String)] = [
        ("just_me", "Just me"),
        ("2-10", "2 to 10"),
        ("11-50", "11 to 50"),
        ("51-200", "51 to 200"),
        ("200+", "200+"),
    ]

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
        .onAppear { prefillName() }
    }

    private var pageTransition: AnyTransition {
        .asymmetric(
            insertion: .move(edge: direction > 0 ? .trailing : .leading).combined(with: .opacity),
            removal: .move(edge: direction > 0 ? .leading : .trailing).combined(with: .opacity)
        )
    }

    // MARK: - Sky chrome

    private var topBar: some View {
        HStack(spacing: 12) {
            if page != .name {
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
        .animation(.spring(response: 0.45, dampingFraction: 0.86), value: page)
    }

    /// The animated sky window: flight ambience plus the current page's badge
    /// (icon, question name, position). Picks the tallest badge that fits so
    /// the keyboard on the name page only compacts it, never kills it.
    private var skyArea: some View {
        ZStack {
            Color.clear
                .contentShape(Rectangle())
                .onTapGesture { focusedName = nil }

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
                    Text("Step \(page.rawValue + 1) of \(Page.allCases.count)")
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
                        Text("Step \(page.rawValue + 1) of \(Page.allCases.count)")
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
            ForEach(Page.allCases, id: \.rawValue) { segment in
                Capsule()
                    .fill(.white.opacity(segment.rawValue <= page.rawValue ? 0.95 : 0.3))
                    .frame(width: segment == page ? 26 : 14, height: 4)
            }
        }
    }

    // MARK: - The sheet

    private var sheet: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Always a ScrollView: ViewThatFits swaps identities when the
            // keyboard shrinks the space, which destroys the focused field.
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
            // The tail below keeps the sheet sealed to the screen edge even
            // mid-animation while the keyboard moves.
            UnevenRoundedRectangle(cornerRadii: .init(topLeading: 36, topTrailing: 36))
                .fill(Color(.systemBackground)
                    .shadow(.drop(color: Color(hex: 0x0F172A).opacity(0.28), radius: 34, y: -6)))
                .padding(.bottom, -600)
                .ignoresSafeArea()
        }
        .geometryGroup()
        // The sheet wins the height negotiation: it takes what its page
        // needs and the sky absorbs the rest, so options never hide behind
        // the Continue button.
        .layoutPriority(1)
    }

    private var pageContent: some View {
        Group {
            switch page {
            case .name: namePage
            case .referral: referralPage
            case .role: rolePage
            case .teamSize: teamSizePage
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

            Button {
                Task { await advance() }
            } label: {
                Group {
                    if busy {
                        ProgressView().tint(.white)
                    } else {
                        HStack(spacing: 8) {
                            Text(page == .teamSize ? "Enter Warmbly" : "Continue")
                            if page == .teamSize {
                                Image(systemName: "paperplane.fill")
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
                        colors: !canAdvance && !busy
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
            .disabled(busy || !canAdvance)
            .animation(.easeOut(duration: 0.18), value: canAdvance)

            if page == .role || page == .teamSize {
                Button("Skip for now") {
                    if page == .role { role = nil } else { teamSize = nil }
                    Task { await advance(skipping: true) }
                }
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(.secondary)
                .disabled(busy)
                .transition(.opacity)
            }
        }
        .padding(.top, 10)
        .padding(.bottom, 14)
        .animation(.easeOut(duration: 0.2), value: page)
        .animation(.easeOut(duration: 0.2), value: errorMessage != nil)
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

    private var namePage: some View {
        VStack(alignment: .leading, spacing: 0) {
            pageTitle("What's your name?", "It shows up on your profile and in the emails you send.")

            VStack(spacing: 12) {
                nameField("First name", text: $firstName, contentType: .givenName, field: .first) {
                    focusedName = .last
                }
                nameField("Last name", text: $lastName, contentType: .familyName, field: .last) {
                    Task { await advance() }
                }
            }
        }
        .onAppear {
            if firstName.isEmpty { focusedName = .first }
        }
    }

    private func nameField(
        _ placeholder: String,
        text: Binding<String>,
        contentType: UITextContentType,
        field: NameField,
        onSubmit: @escaping () -> Void
    ) -> some View {
        TextField(placeholder, text: text)
            .textContentType(contentType)
            .focused($focusedName, equals: field)
            .submitLabel(field == .first ? .next : .continue)
            .onSubmit(onSubmit)
            .font(.system(size: 16.5))
            .padding(.horizontal, 16)
            .frame(height: 56)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 17))
            .overlay(
                RoundedRectangle(cornerRadius: 17)
                    .strokeBorder(focusedName == field ? WTheme.accent : .clear, lineWidth: 1.8)
            )
            .animation(.easeOut(duration: 0.18), value: focusedName)
    }

    private var referralPage: some View {
        VStack(alignment: .leading, spacing: 0) {
            pageTitle("How did you hear about us?", "One tap. It helps us know where to show up.")
            optionGrid(Self.referralOptions, selection: $referralSource)
        }
    }

    private var rolePage: some View {
        VStack(alignment: .leading, spacing: 0) {
            pageTitle("What best describes you?", "Optional. It tunes the tips you'll see.")
            optionGrid(Self.roleOptions, selection: $role)
        }
    }

    private var teamSizePage: some View {
        VStack(alignment: .leading, spacing: 0) {
            pageTitle("How big is your team?", "Optional. Helps us right-size recommendations.")

            LazyVGrid(columns: [GridItem(.flexible(), spacing: 10), GridItem(.flexible(), spacing: 10), GridItem(.flexible(), spacing: 10)], spacing: 10) {
                ForEach(Self.teamSizeOptions, id: \.value) { option in
                    sizeChip(option.label, selected: teamSize == option.value) {
                        withAnimation(.spring(response: 0.35, dampingFraction: 0.7)) {
                            teamSize = teamSize == option.value ? nil : option.value
                        }
                    }
                }
            }
        }
    }

    /// Two-column icon tiles with a springy check badge; tapping again
    /// deselects.
    private func optionGrid(_ options: [(value: String, label: String, icon: String)], selection: Binding<String?>) -> some View {
        LazyVGrid(columns: [GridItem(.flexible(), spacing: 12), GridItem(.flexible(), spacing: 12)], spacing: 12) {
            ForEach(options, id: \.value) { option in
                optionTile(option, selected: selection.wrappedValue == option.value) {
                    withAnimation(.spring(response: 0.35, dampingFraction: 0.7)) {
                        selection.wrappedValue = selection.wrappedValue == option.value ? nil : option.value
                    }
                }
            }
        }
    }

    private func optionTile(
        _ option: (value: String, label: String, icon: String),
        selected: Bool,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            VStack(spacing: 10) {
                Image(systemName: option.icon)
                    .font(.system(size: 19, weight: .semibold))
                    .foregroundStyle(selected ? .white : WTheme.accent)
                    .frame(width: 42, height: 42)
                    .background(
                        selected ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(WTheme.accent.opacity(0.12)),
                        in: Circle()
                    )
                Text(option.label)
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
            }
            .frame(maxWidth: .infinity)
            .frame(height: 104)
            .background(
                selected ? AnyShapeStyle(WTheme.accent.opacity(0.08)) : AnyShapeStyle(Color(.secondarySystemBackground)),
                in: RoundedRectangle(cornerRadius: 17)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 17)
                    .strokeBorder(selected ? WTheme.accent : .clear, lineWidth: 1.8)
            )
            .overlay(alignment: .topTrailing) {
                if selected {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 19))
                        .foregroundStyle(.white, WTheme.accent)
                        .padding(7)
                        .transition(.scale(scale: 0.4).combined(with: .opacity))
                }
            }
        }
        .buttonStyle(PressableButtonStyle())
        .sensoryFeedback(.selection, trigger: selected)
    }

    private func sizeChip(_ label: String, selected: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label)
                .font(.system(size: 14.5, weight: .semibold))
                .foregroundStyle(selected ? .white : .primary)
                .lineLimit(1)
                .minimumScaleFactor(0.75)
                .frame(maxWidth: .infinity)
                .frame(height: 46)
                .background(
                    selected ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(Color(.secondarySystemBackground)),
                    in: Capsule()
                )
        }
        .buttonStyle(PressableButtonStyle())
        .sensoryFeedback(.selection, trigger: selected)
    }

    // MARK: - Flow

    private var canAdvance: Bool {
        switch page {
        case .name:
            return !firstName.trimmingCharacters(in: .whitespaces).isEmpty
                && !lastName.trimmingCharacters(in: .whitespaces).isEmpty
        case .referral:
            return referralSource != nil
        case .role, .teamSize:
            return true
        }
    }

    private func goBack() {
        guard let previous = Page(rawValue: page.rawValue - 1) else { return }
        errorMessage = nil
        direction = -1
        withAnimation(.spring(response: 0.45, dampingFraction: 0.86)) {
            page = previous
        }
    }

    private func advance(skipping: Bool = false) async {
        guard canAdvance || skipping else { return }
        errorMessage = nil
        if page == .teamSize {
            await submit()
            return
        }
        focusedName = nil
        direction = 1
        withAnimation(.spring(response: 0.45, dampingFraction: 0.86)) {
            page = Page(rawValue: page.rawValue + 1) ?? .teamSize
        }
    }

    private func submit() async {
        busy = true
        do {
            try await env.session.completeOnboarding(
                firstName: firstName.trimmingCharacters(in: .whitespaces),
                lastName: lastName.trimmingCharacters(in: .whitespaces),
                referralSource: referralSource ?? "other",
                role: role,
                teamSize: teamSize
            )
            // The view unmounts as the session phase flips, so fire the
            // arrival haptic directly instead of via sensoryFeedback.
            UINotificationFeedbackGenerator().notificationOccurred(.success)
        } catch {
            withAnimation(.easeOut(duration: 0.2)) {
                errorMessage = (error as? APIError)?.errorDescription ?? error.localizedDescription
            }
            errorPulse += 1
        }
        busy = false
    }

    private func prefillName() {
        firstName = env.session.user?.firstName ?? ""
        lastName = env.session.user?.lastName ?? ""
        // CreateUser defaults the first name to the email local part;
        // don't present that as if the user typed it.
        if let email = env.session.user?.email, email.hasPrefix(firstName + "@") {
            firstName = ""
        }
    }
}
