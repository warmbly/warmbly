import SwiftUI

/// Campaign creation as a stepped onboarding flow wearing the same air as
/// sign-in: the sky with its flight ambience and a step badge up top, a
/// full-bleed white sheet with the questions below. Steps: name it, pick the
/// sending pace and rules, then optionally file it into a folder (the folder
/// page only exists when the member has folders). Steps and senders are
/// finished on the web dashboard; on success the created campaign is handed
/// back so the caller can push its detail.
struct CampaignCreateFlow: View {
    private enum Page: Int, CaseIterable {
        case name, pace, folder

        var icon: String {
            switch self {
            case .name: "pencil.line"
            case .pace: "speedometer"
            case .folder: "folder.fill"
            }
        }

        var skyLabel: String {
            switch self {
            case .name: "Name your campaign"
            case .pace: "Sending pace"
            case .folder: "Keep it organized"
            }
        }
    }

    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let store: CampaignsStore
    let onCreated: (Campaign) -> Void

    @State private var page = Page.name
    @State private var direction = 1.0
    @State private var name = ""
    @State private var descriptionText = ""
    @State private var dailyLimit = 50
    @State private var stopOnReply = true
    @State private var openTracking = true
    @State private var linkTracking = true
    @State private var folderID: String?
    @State private var busy = false
    @State private var errorMessage: String?
    @State private var errorPulse = 0
    @State private var badgeAppeared = false
    /// The 40fps flight-scene canvas mounts only after the cover's slide-up
    /// finishes, so presenting the flow never competes with it for frames.
    @State private var ambienceReady = false

    @State private var folderQuery = ""

    @FocusState private var focusedField: Field?
    private enum Field { case name, description, folderSearch }

    /// Per-mailbox daily budgets, mirroring the product's conservative
    /// defaults (50 is the built-in cap; fresh mailboxes should start low).
    private static let paceOptions: [(value: Int, label: String, detail: String, icon: String)] = [
        (10, "Gentle", "Fresh mailboxes", "leaf.fill"),
        (25, "Steady", "Warming up", "thermometer.sun.fill"),
        (50, "Standard", "Proven mailboxes", "gauge.with.dots.needle.50percent"),
    ]

    /// The member's campaign folders (session), in position order.
    private var folders: [UserGroup] {
        (env.session.user?.folders ?? []).sorted { ($0.position ?? 0) < ($1.position ?? 0) }
    }

    /// The folder page only exists when there are folders to file into.
    private var pages: [Page] {
        folders.isEmpty ? [.name, .pace] : [.name, .pace, .folder]
    }

    private var stepIndex: Int { pages.firstIndex(of: page) ?? 0 }
    private var isLastPage: Bool { page == pages.last }

    private var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

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
        .task {
            try? await Task.sleep(for: .milliseconds(480))
            withAnimation(.easeIn(duration: 0.7)) { ambienceReady = true }
            if page == .name, name.isEmpty { focusedField = .name }
        }
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
                Text("New campaign")
                    .font(.system(size: 20, weight: .heavy))
                    .tracking(-0.4)
                    .foregroundStyle(.white)
                    .fixedSize()
            }
            .shadow(color: Color(hex: 0x0C4A6E).opacity(0.25), radius: 8, y: 3)

            Spacer()

            Button {
                dismiss()
            } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(.white.opacity(0.9))
                    .frame(width: 34, height: 34)
                    .background(.white.opacity(0.16), in: Circle())
            }
            .buttonStyle(PressableButtonStyle())
            .disabled(busy)
            .accessibilityLabel("Cancel")
        }
        .padding(.horizontal, 16)
        .padding(.top, 6)
        .animation(.spring(response: 0.45, dampingFraction: 0.86), value: page)
    }

    /// The animated sky window: flight ambience plus the current page's badge.
    /// Picks the tallest badge that fits so the keyboard on the name page
    /// only compacts it, never kills it.
    private var skyArea: some View {
        ZStack {
            Color.clear
                .contentShape(Rectangle())
                .onTapGesture { focusedField = nil }

            if ambienceReady {
                HeroFlightScene()
                    .transition(.opacity)
            }

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
                    Text("Step \(stepIndex + 1) of \(pages.count)")
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
                        Text("Step \(stepIndex + 1) of \(pages.count)")
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
            ForEach(Array(pages.enumerated()), id: \.offset) { index, segment in
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
            case .pace: pacePage
            case .folder: folderPage
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
                            Text(isLastPage ? "Create campaign" : "Continue")
                            if isLastPage {
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

            if page == .folder {
                Button("No folder") {
                    folderID = nil
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
            pageTitle("Name your campaign", "You'll add steps and senders on the web dashboard once it exists.")

            VStack(spacing: 12) {
                textInput("Campaign name", text: $name, field: .name, submitLabel: .next) {
                    focusedField = .description
                }
                .overlay(alignment: .trailing) {
                    if !trimmedName.isEmpty {
                        Text("\(trimmedName.count)/50")
                            .font(.caption)
                            .monospacedDigit()
                            .foregroundStyle(trimmedName.count > 50 ? AnyShapeStyle(WTheme.negative) : AnyShapeStyle(.tertiary))
                            .contentTransition(.numericText())
                            .padding(.trailing, 16)
                    }
                }
                textInput("Description (optional)", text: $descriptionText, field: .description, submitLabel: .continue) {
                    Task { await advance() }
                }
            }
        }
    }

    private func textInput(
        _ placeholder: String,
        text: Binding<String>,
        field: Field,
        submitLabel: SubmitLabel,
        onSubmit: @escaping () -> Void
    ) -> some View {
        TextField(placeholder, text: text)
            .focused($focusedField, equals: field)
            .submitLabel(submitLabel)
            .onSubmit(onSubmit)
            .font(.system(size: 16.5))
            .padding(.horizontal, 16)
            .padding(.trailing, field == .name ? 44 : 0)
            .frame(height: 56)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 17))
            .overlay(
                RoundedRectangle(cornerRadius: 17)
                    .strokeBorder(focusedField == field ? WTheme.accent : .clear, lineWidth: 1.8)
            )
            .animation(.easeOut(duration: 0.18), value: focusedField)
    }

    private var pacePage: some View {
        VStack(alignment: .leading, spacing: 0) {
            pageTitle("How fast should it send?", "Per mailbox, per day, spread out through the day. You can change this anytime.")

            HStack(spacing: 12) {
                ForEach(Self.paceOptions, id: \.value) { option in
                    paceTile(option, selected: dailyLimit == option.value) {
                        withAnimation(.spring(response: 0.35, dampingFraction: 0.7)) {
                            dailyLimit = option.value
                        }
                    }
                }
            }
            .padding(.bottom, 18)

            VStack(spacing: 10) {
                ruleRow(
                    "Stop on reply",
                    detail: "Pause the sequence when they answer.",
                    icon: "arrowshape.turn.up.left.fill",
                    isOn: $stopOnReply
                )
                ruleRow(
                    "Open tracking",
                    detail: "Know when your emails get opened.",
                    icon: "envelope.open.fill",
                    isOn: $openTracking
                )
                ruleRow(
                    "Link tracking",
                    detail: "Know when links get clicked.",
                    icon: "link",
                    isOn: $linkTracking
                )
            }
        }
    }

    private func paceTile(
        _ option: (value: Int, label: String, detail: String, icon: String),
        selected: Bool,
        action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            VStack(spacing: 7) {
                Image(systemName: option.icon)
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(selected ? .white : WTheme.accent)
                    .frame(width: 38, height: 38)
                    .background(
                        selected ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(WTheme.accent.opacity(0.12)),
                        in: Circle()
                    )
                Text("\(option.value)/day")
                    .font(.system(size: 15, weight: .bold))
                    .monospacedDigit()
                    .foregroundStyle(.primary)
                Text(option.detail)
                    .font(.system(size: 11.5, weight: .medium))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.75)
            }
            .frame(maxWidth: .infinity)
            .frame(height: 118)
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
                        .font(.system(size: 17))
                        .foregroundStyle(.white, WTheme.accent)
                        .padding(6)
                        .transition(.scale(scale: 0.4).combined(with: .opacity))
                }
            }
        }
        .buttonStyle(PressableButtonStyle())
        .sensoryFeedback(.selection, trigger: selected)
    }

    private func ruleRow(_ title: String, detail: String, icon: String, isOn: Binding<Bool>) -> some View {
        HStack(spacing: 12) {
            Image(systemName: icon)
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(WTheme.accent)
                .frame(width: 34, height: 34)
                .background(WTheme.accent.opacity(0.12), in: Circle())
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.system(size: 14.5, weight: .semibold))
                Text(detail)
                    .font(.system(size: 12))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
            }
            Spacer(minLength: 8)
            Toggle("", isOn: isOn)
                .labelsHidden()
                .tint(WTheme.accent)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 17))
    }

    /// Folders shown on the folder page, filtered by the search box once the
    /// list is big enough to need one.
    private var visibleFolders: [UserGroup] {
        let trimmed = folderQuery.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return folders }
        return folders.filter { ($0.name ?? "").localizedCaseInsensitiveContains(trimmed) }
    }

    private var folderPage: some View {
        VStack(alignment: .leading, spacing: 0) {
            pageTitle("File it in a folder?", "Optional. Folders keep the campaign list browsable as it grows.")

            if folders.count > 8 {
                folderSearchField
                    .padding(.bottom, 12)
            }

            // Two-up chips so big folder libraries stay a short scroll, not
            // a wall of full-width rows.
            LazyVGrid(columns: [GridItem(.flexible(), spacing: 10), GridItem(.flexible(), spacing: 10)], spacing: 10) {
                ForEach(visibleFolders) { folder in
                    folderChip(folder, selected: folderID == folder.id) {
                        withAnimation(.spring(response: 0.35, dampingFraction: 0.7)) {
                            folderID = folderID == folder.id ? nil : folder.id
                        }
                    }
                }
            }

            if visibleFolders.isEmpty {
                Text("No folders match \"\(folderQuery.trimmingCharacters(in: .whitespaces))\"")
                    .font(.system(size: 13.5))
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 24)
            }
        }
    }

    private var folderSearchField: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(.secondary)
            TextField("Search folders", text: $folderQuery)
                .focused($focusedField, equals: .folderSearch)
                .font(.system(size: 15))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            if !folderQuery.isEmpty {
                Button {
                    folderQuery = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 15))
                        .foregroundStyle(.tertiary)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear folder search")
            }
        }
        .padding(.horizontal, 14)
        .frame(height: 44)
        .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 14))
        .overlay(
            RoundedRectangle(cornerRadius: 14)
                .strokeBorder(focusedField == .folderSearch ? WTheme.accent : .clear, lineWidth: 1.8)
        )
        .animation(.easeOut(duration: 0.18), value: focusedField)
    }

    private func folderChip(_ folder: UserGroup, selected: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 9) {
                Circle()
                    .fill(Color(uniboxHex: folder.color) ?? WTheme.accent)
                    .frame(width: 10, height: 10)
                Text(folder.name ?? "Folder")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(.primary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
                Spacer(minLength: 4)
                if selected {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 17))
                        .foregroundStyle(.white, WTheme.accent)
                        .transition(.scale(scale: 0.4).combined(with: .opacity))
                }
            }
            .padding(.horizontal, 13)
            .frame(height: 46)
            .background(
                selected ? AnyShapeStyle(WTheme.accent.opacity(0.08)) : AnyShapeStyle(Color(.secondarySystemBackground)),
                in: RoundedRectangle(cornerRadius: 14)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 14)
                    .strokeBorder(selected ? WTheme.accent : .clear, lineWidth: 1.8)
            )
        }
        .buttonStyle(PressableButtonStyle())
        .sensoryFeedback(.selection, trigger: selected)
    }

    // MARK: - Flow

    private var canAdvance: Bool {
        switch page {
        case .name:
            let count = trimmedName.count
            return count >= 3 && count <= 50
        case .pace, .folder:
            return true
        }
    }

    private func goBack() {
        guard stepIndex > 0 else { return }
        errorMessage = nil
        direction = -1
        withAnimation(.spring(response: 0.45, dampingFraction: 0.86)) {
            page = pages[stepIndex - 1]
        }
    }

    private func advance(skipping: Bool = false) async {
        guard canAdvance || skipping else { return }
        errorMessage = nil
        if isLastPage || skipping {
            await submit()
            return
        }
        focusedField = nil
        direction = 1
        withAnimation(.spring(response: 0.45, dampingFraction: 0.86)) {
            page = pages[stepIndex + 1]
        }
    }

    private func submit() async {
        busy = true
        do {
            let description = descriptionText.trimmingCharacters(in: .whitespacesAndNewlines)
            let campaign = try await store.create(env.api, body: CampaignCreateBody(
                name: trimmedName,
                description: description.isEmpty ? nil : description,
                dailyLimit: dailyLimit,
                stopOnReply: stopOnReply,
                openTracking: openTracking,
                linkTracking: linkTracking,
                folderIDs: folderID.map { [$0] }
            ))
            // The cover dismisses right away, so fire the arrival haptic
            // directly instead of via sensoryFeedback.
            UINotificationFeedbackGenerator().notificationOccurred(.success)
            dismiss()
            onCreated(campaign)
        } catch {
            withAnimation(.easeOut(duration: 0.2)) {
                errorMessage = (error as? APIError)?.errorDescription ?? error.localizedDescription
            }
            errorPulse += 1
        }
        busy = false
    }
}
