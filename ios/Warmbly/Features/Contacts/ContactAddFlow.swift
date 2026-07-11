import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// Adding contacts as a stepped onboarding flow wearing the same sky air as the
/// campaign create flow and sign-in: pick how (one contact or paste a list),
/// enter the details, then optionally file into categories and campaigns.
/// CSV/spreadsheet import stays a web-dashboard job (surfaced as a nudge).
struct ContactAddFlow: View {
    private enum Page: Int, CaseIterable {
        case method, details, organize

        var icon: String {
            switch self {
            case .method: "person.badge.plus"
            case .details: "square.and.pencil"
            case .organize: "tag.fill"
            }
        }

        var skyLabel: String {
            switch self {
            case .method: "Add contacts"
            case .details: "Their details"
            case .organize: "Organize them"
            }
        }
    }

    private enum Mode { case single, paste, importFile }
    private enum Field: Hashable { case email, first, last, company, phone, paste }

    @Environment(AppEnvironment.self) private var env
    @Environment(\.dismiss) private var dismiss

    let store: ContactsStore
    let categoryStore: ContactCategoryStore
    let onCreated: ([Contact]) -> Void

    @State private var page = Page.method
    @State private var direction = 1.0
    @State private var mode = Mode.single

    @State private var email = ""
    @State private var firstName = ""
    @State private var lastName = ""
    @State private var company = ""
    @State private var phone = ""
    @State private var pasted = ""

    @State private var showImporter = false
    @State private var csv: ContactCSV?
    @State private var mapping: [ContactColumnTarget] = []
    @State private var importError: String?

    @State private var categoryIDs: Set<String> = []
    @State private var campaignIDs: Set<String> = []

    @State private var busy = false
    @State private var errorMessage: String?
    @State private var errorPulse = 0
    @State private var ambienceReady = false
    @State private var badgeAppeared = false

    @FocusState private var focused: Field?

    private static let pages = Page.allCases
    private var stepIndex: Int { Self.pages.firstIndex(of: page) ?? 0 }
    private var isLastPage: Bool { page == Self.pages.last }

    /// Emails parsed from the pasted blob: any @-token, deduped, lowercased.
    private var pastedEmails: [String] {
        let tokens = pasted.split { " \n\r\t,;".contains($0) }.map { String($0).trimmingCharacters(in: .whitespaces) }
        var seen = Set<String>()
        return tokens
            .filter { $0.contains("@") && $0.contains(".") && !$0.hasSuffix("@") }
            .map { $0.lowercased() }
            .filter { seen.insert($0).inserted }
    }

    private var singleEmailValid: Bool {
        let t = email.trimmingCharacters(in: .whitespaces)
        return t.contains("@") && t.contains(".") && !t.hasSuffix("@")
    }

    private var importableCount: Int {
        guard let csv else { return 0 }
        return ContactCSVBuild.importableCount(csv, mapping: mapping)
    }

    private var readyToSubmit: Bool {
        switch mode {
        case .single: singleEmailValid
        case .paste: !pastedEmails.isEmpty
        case .importFile: importableCount > 0
        }
    }

    private var addCount: Int {
        switch mode {
        case .single: singleEmailValid ? 1 : 0
        case .paste: pastedEmails.count
        case .importFile: importableCount
        }
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
        }
        .task { await categoryStore.load(env.api) }
        .task { await store.loadCampaignOptions(env.api) }
        .fileImporter(
            isPresented: $showImporter,
            allowedContentTypes: [.commaSeparatedText, .tabSeparatedText, .plainText, .text],
            allowsMultipleSelection: false
        ) { result in
            handleImport(result)
        }
        .alert("Couldn't read that file", isPresented: Binding(
            get: { importError != nil }, set: { if !$0 { importError = nil } }
        )) {
            Button("OK", role: .cancel) {}
        } message: { Text(importError ?? "") }
    }

    private var pageTransition: AnyTransition {
        .asymmetric(
            insertion: .move(edge: direction > 0 ? .trailing : .leading).combined(with: .opacity),
            removal: .move(edge: direction > 0 ? .leading : .trailing).combined(with: .opacity)
        )
    }

    // MARK: Sky chrome

    private var topBar: some View {
        HStack(spacing: 12) {
            if page != .method {
                Button { goBack() } label: {
                    Image(systemName: "chevron.left")
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(.white)
                        .frame(width: 40, height: 40)
                        .background(.white.opacity(0.16), in: Circle())
                }
                .buttonStyle(PressableButtonStyle())
                .transition(.opacity.combined(with: .scale(scale: 0.7)))
            }
            HStack(spacing: 8) {
                WarmblyLogo().fill(.white).frame(width: 27, height: 28)
                Text("New contacts")
                    .font(.system(size: 20, weight: .heavy))
                    .tracking(-0.4)
                    .foregroundStyle(.white)
                    .fixedSize()
            }
            .shadow(color: Color(hex: 0x0C4A6E).opacity(0.25), radius: 8, y: 3)
            Spacer()
            Button { dismiss() } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(.white.opacity(0.9))
                    .frame(width: 34, height: 34)
                    .background(.white.opacity(0.16), in: Circle())
            }
            .buttonStyle(PressableButtonStyle())
            .disabled(busy)
        }
        .padding(.horizontal, 16)
        .padding(.top, 6)
        .animation(.spring(response: 0.45, dampingFraction: 0.86), value: page)
    }

    private var skyArea: some View {
        ZStack {
            Color.clear.contentShape(Rectangle()).onTapGesture { focused = nil }
            if ambienceReady { HeroFlightScene().transition(.opacity) }
            pageBadge
                .scaleEffect(badgeAppeared ? 1 : 0.9)
                .opacity(badgeAppeared ? 1 : 0)
        }
        .frame(maxWidth: .infinity, minHeight: 40, maxHeight: .infinity)
        .animation(.spring(response: 0.5, dampingFraction: 0.85), value: page)
        .onAppear {
            withAnimation(.spring(response: 0.7, dampingFraction: 0.75).delay(0.05)) { badgeAppeared = true }
        }
    }

    private var pageBadge: some View {
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
                Text("Step \(stepIndex + 1) of \(Self.pages.count)")
                    .font(.system(size: 12))
                    .foregroundStyle(.white.opacity(0.7))
                    .contentTransition(.numericText())
            }
            segments
        }
        .padding(.vertical, 16)
        .shadow(color: Color(hex: 0x0C4A6E).opacity(0.25), radius: 6, y: 2)
    }

    private var segments: some View {
        HStack(spacing: 6) {
            ForEach(Array(Self.pages.enumerated()), id: \.offset) { index, segment in
                Capsule()
                    .fill(.white.opacity(index <= stepIndex ? 0.95 : 0.3))
                    .frame(width: segment == page ? 26 : 14, height: 4)
            }
        }
    }

    // MARK: Sheet

    private var sheet: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Always a ScrollView: ViewThatFits swaps identities when the
            // keyboard shrinks the space, which destroys the focused field.
            ScrollView { pageContent }
                .scrollBounceBehavior(.basedOnSize)
                .scrollDismissesKeyboard(.interactively)
            footer
        }
        .frame(maxWidth: .infinity)
        .background(
            UnevenRoundedRectangle(topLeadingRadius: 28, topTrailingRadius: 28, style: .continuous)
                .fill(Color(.systemBackground))
                .ignoresSafeArea(edges: .bottom)
        )
    }

    @ViewBuilder
    private var pageContent: some View {
        Group {
            switch page {
            case .method: methodPage
            case .details: detailsPage
            case .organize: organizePage
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, 22)
        .transition(pageTransition)
        .id(page)
    }

    private func pageTitle(_ title: String, _ subtitle: String) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(title).font(.system(size: 22, weight: .bold))
            Text(subtitle).font(.subheadline).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.bottom, 4)
    }

    // MARK: Method page

    private var methodPage: some View {
        VStack(alignment: .leading, spacing: 12) {
            pageTitle("How do you want to add?", "One at a time, paste a list, or import a file.")
            methodTile(
                mode: .single,
                icon: "person.fill",
                title: "Add one contact",
                detail: "Full name, company and phone."
            )
            methodTile(
                mode: .paste,
                icon: "list.bullet.rectangle.fill",
                title: "Paste a list",
                detail: "Drop in many emails at once."
            )
            methodTile(
                mode: .importFile,
                icon: "doc.text.fill",
                title: "Import a file",
                detail: csv == nil ? "Upload a CSV and map the columns." : "\(importableCount) contacts ready."
            )
            HStack(spacing: 8) {
                Image(systemName: "checkmark.shield")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(.secondary)
                Text("CSV or TSV, parsed right here on your phone.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .padding(.top, 2)
        }
    }

    private func methodTile(mode tileMode: Mode, icon: String, title: String, detail: String) -> some View {
        let selected = mode == tileMode
        return Button {
            withAnimation(.snappy) { mode = tileMode }
        } label: {
            HStack(spacing: 13) {
                ZStack {
                    Circle().fill(selected ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(Tone.slate.background))
                    Image(systemName: icon)
                        .font(.system(size: 17, weight: .semibold))
                        .foregroundStyle(selected ? Color.white : Color.secondary)
                }
                .frame(width: 44, height: 44)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(.body.weight(.semibold)).foregroundStyle(.primary)
                    Text(detail).font(.footnote).foregroundStyle(.secondary)
                }
                Spacer()
                Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                    .font(.system(size: 20))
                    .foregroundStyle(selected ? WTheme.accent : Color(.tertiaryLabel))
            }
            .padding(14)
            .background(Color(.secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 16, style: .continuous)
                    .strokeBorder(selected ? WTheme.accent.opacity(0.6) : Color(.separator).opacity(0.3), lineWidth: selected ? 1.5 : 1)
            )
        }
        .buttonStyle(TapScaleStyle())
    }

    // MARK: Details page

    @ViewBuilder
    private var detailsPage: some View {
        if mode == .single {
            VStack(alignment: .leading, spacing: 12) {
                pageTitle("Who is it?", "Email is required. Everything else is optional.")
                field("Email", text: $email, field: .email, keyboard: .emailAddress, lower: true)
                HStack(spacing: 10) {
                    field("First name", text: $firstName, field: .first)
                    field("Last name", text: $lastName, field: .last)
                }
                field("Company", text: $company, field: .company)
                field("Phone", text: $phone, field: .phone, keyboard: .phonePad)
            }
        } else if mode == .paste {
            VStack(alignment: .leading, spacing: 12) {
                pageTitle("Paste your list", "One email per line, or separated by commas.")
                ZStack(alignment: .topLeading) {
                    if pasted.isEmpty {
                        // `verbatim:` so SwiftUI doesn't markdown-autolink the
                        // sample emails into blue links that ignore the color;
                        // a concrete light gray at the TextEditor's text origin.
                        Text(verbatim: "jane@acme.com\njohn@globex.com")
                            .font(.body)
                            .foregroundStyle(Color(.systemGray3))
                            .padding(.horizontal, 15)
                            .padding(.top, 14)
                            .allowsHitTesting(false)
                    }
                    TextEditor(text: $pasted)
                        .focused($focused, equals: .paste)
                        .font(.body)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .scrollContentBackground(.hidden)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 6)
                        .frame(minHeight: 150)
                }
                .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                HStack(spacing: 6) {
                    Image(systemName: pastedEmails.isEmpty ? "envelope" : "checkmark.circle.fill")
                        .foregroundStyle(pastedEmails.isEmpty ? Color.secondary : WTheme.positive)
                    Text(pastedEmails.isEmpty ? "No emails detected yet" : "\(pastedEmails.count) email\(pastedEmails.count == 1 ? "" : "s") found")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                        .contentTransition(.numericText())
                }
            }
        } else {
            importMappingPage
        }
    }

    // MARK: Import mapping page

    private var importMappingPage: some View {
        VStack(alignment: .leading, spacing: 10) {
            pageTitle("Map your columns", mappingSubtitle)
            if let csv {
                Toggle("First row is a header", isOn: Binding(
                    get: { csv.hasHeader },
                    set: { newValue in
                        self.csv?.hasHeader = newValue
                        if let c = self.csv { mapping = c.guessMapping() }
                    }
                ))
                .tint(WTheme.accent)
                .font(.subheadline)

                let headers = csv.displayColumns()
                ForEach(headers.indices, id: \.self) { idx in
                    HStack(spacing: 10) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(headers[idx]).font(.subheadline.weight(.medium)).lineLimit(1)
                            if let sample = firstSample(idx) {
                                Text(sample).font(.caption).foregroundStyle(.tertiary).lineLimit(1)
                            }
                        }
                        Spacer(minLength: 8)
                        Menu {
                            ForEach(ContactColumnTarget.allCases, id: \.self) { target in
                                Button {
                                    if mapping.indices.contains(idx) { mapping[idx] = target }
                                } label: {
                                    if mapping.indices.contains(idx), mapping[idx] == target {
                                        Label(target.label, systemImage: "checkmark")
                                    } else {
                                        Text(target.label)
                                    }
                                }
                            }
                        } label: {
                            let target = mapping.indices.contains(idx) ? mapping[idx] : .ignore
                            HStack(spacing: 4) {
                                Text(target.label).font(.subheadline.weight(.medium))
                                Image(systemName: "chevron.up.chevron.down").font(.system(size: 9, weight: .semibold))
                            }
                            .foregroundStyle(target == .ignore ? Color.secondary : WTheme.accent)
                            .padding(.horizontal, 12)
                            .frame(height: 34)
                            .background((target == .ignore ? Tone.slate : Tone.sky).background, in: Capsule())
                        }
                    }
                    .padding(.vertical, 7)
                    if idx < headers.count - 1 { Divider().opacity(0.4) }
                }
            }
        }
    }

    private var mappingSubtitle: String {
        guard let csv else { return "" }
        let total = csv.dataRows.count
        return "\(importableCount) of \(total) row\(total == 1 ? "" : "s") have an email."
    }

    private func firstSample(_ idx: Int) -> String? {
        guard let csv, let row = csv.dataRows.first, idx < row.count else { return nil }
        let v = row[idx].trimmingCharacters(in: .whitespaces)
        return v.isEmpty ? nil : v
    }

    private func handleImport(_ result: Result<[URL], Error>) {
        switch result {
        case let .success(urls):
            guard let url = urls.first else { return }
            let scoped = url.startAccessingSecurityScopedResource()
            defer { if scoped { url.stopAccessingSecurityScopedResource() } }
            do {
                let data = try Data(contentsOf: url)
                let text = String(decoding: data, as: UTF8.self)
                let parsed = ContactCSV.parse(text)
                guard !parsed.columns.isEmpty, !parsed.dataRows.isEmpty else {
                    importError = "That file looks empty."
                    return
                }
                mapping = parsed.guessMapping()
                csv = parsed
                advance()
            } catch {
                importError = error.localizedDescription
            }
        case let .failure(error):
            importError = error.localizedDescription
        }
    }

    private func field(
        _ placeholder: String,
        text: Binding<String>,
        field: Field,
        keyboard: UIKeyboardType = .default,
        lower: Bool = false
    ) -> some View {
        TextField(placeholder, text: text)
            .focused($focused, equals: field)
            .keyboardType(keyboard)
            .textInputAutocapitalization(lower ? .never : .words)
            .autocorrectionDisabled(lower)
            .font(.body)
            .padding(.horizontal, 14)
            .frame(height: 48)
            .background(Color(.secondarySystemBackground), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    // MARK: Organize page

    private var organizePage: some View {
        VStack(alignment: .leading, spacing: 16) {
            pageTitle("Organize (optional)", "Tag them and drop them straight into a campaign.")
            if !categoryStore.categories.isEmpty {
                chipGroup(
                    "Categories",
                    items: categoryStore.categories.map { ($0.id, $0.title ?? "Untitled", ContactColor.dot($0.color, seed: $0.id)) },
                    selection: $categoryIDs
                )
            }
            if !store.campaignOptions.isEmpty {
                chipGroup(
                    "Add to campaign",
                    items: store.campaignOptions.map { ($0.id, $0.name ?? "Campaign", WTheme.accent) },
                    selection: $campaignIDs
                )
            }
            if categoryStore.categories.isEmpty, store.campaignOptions.isEmpty {
                Text("No categories or campaigns yet. You can add \(addCount) contact\(addCount == 1 ? "" : "s") now and organize later.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func chipGroup(_ title: String, items: [(String, String, Color)], selection: Binding<Set<String>>) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            EyebrowLabel(title)
            WrapChips(items: items, selection: selection)
        }
    }

    // MARK: Footer

    private var footer: some View {
        VStack(spacing: 0) {
            Divider().opacity(0.5)
            Button {
                if isLastPage {
                    Task { await submit() }
                } else if page == .method, mode == .importFile, csv == nil {
                    showImporter = true
                } else {
                    advance()
                }
            } label: {
                HStack(spacing: 8) {
                    if busy { ProgressView().tint(.white) }
                    Text(footerTitle)
                        .font(.system(size: 16, weight: .semibold))
                        .contentTransition(.numericText())
                }
                .frame(maxWidth: .infinity)
                .frame(height: 52)
                .background(footerEnabled ? AnyShapeStyle(WTheme.accent) : AnyShapeStyle(Color(.tertiarySystemFill)), in: Capsule())
                .foregroundStyle(footerEnabled ? Color.white : Color.secondary)
            }
            .buttonStyle(TapScaleStyle())
            .disabled(!footerEnabled || busy)
            .padding(.horizontal, 20)
            .padding(.top, 12)
            .padding(.bottom, 8)
        }
    }

    private var footerTitle: String {
        if isLastPage { return addCount > 0 ? "Add \(addCount) contact\(addCount == 1 ? "" : "s")" : "Add contacts" }
        if page == .method, mode == .importFile, csv == nil { return "Choose a file" }
        return "Continue"
    }

    private var footerEnabled: Bool {
        switch page {
        case .method: return true
        case .details: return readyToSubmit
        case .organize: return readyToSubmit
        }
    }

    // MARK: Navigation + submit

    private func goBack() {
        guard stepIndex > 0 else { return }
        direction = -1
        focused = nil
        withAnimation(.spring(response: 0.45, dampingFraction: 0.85)) { page = Self.pages[stepIndex - 1] }
    }

    private func advance() {
        guard stepIndex < Self.pages.count - 1 else { return }
        direction = 1
        focused = nil
        withAnimation(.spring(response: 0.45, dampingFraction: 0.85)) { page = Self.pages[stepIndex + 1] }
    }

    private func submit() async {
        guard readyToSubmit else { return }
        busy = true
        defer { busy = false }
        let cats = categoryIDs.isEmpty ? nil : Array(categoryIDs)
        let camps = campaignIDs.isEmpty ? nil : Array(campaignIDs)
        do {
            let created: [Contact]
            switch mode {
            case .single:
                let body = ContactCreateBody(
                    firstName: firstName.trimmingCharacters(in: .whitespaces),
                    lastName: lastName.trimmingCharacters(in: .whitespaces),
                    email: email.trimmingCharacters(in: .whitespaces),
                    company: company.trimmingCharacters(in: .whitespaces),
                    phone: phone.trimmingCharacters(in: .whitespaces),
                    categories: cats,
                    campaigns: camps
                )
                created = [try await store.create(env.api, body: body)]
            case .paste:
                let bodies = pastedEmails.map {
                    ContactCreateBody(email: $0, categories: cats, campaigns: camps)
                }
                created = try await store.createMany(env.api, bodies: bodies)
            case .importFile:
                guard let csv else { return }
                let headers = csv.hasHeader ? csv.columns : csv.displayColumns()
                let bodies = ContactCSVBuild.bodies(csv, mapping: mapping, headers: headers, categories: cats, campaigns: camps)
                // Chunk large imports so a single POST stays well under the cap.
                var acc: [Contact] = []
                var start = 0
                while start < bodies.count {
                    let end = min(start + 200, bodies.count)
                    acc += try await store.createMany(env.api, bodies: Array(bodies[start ..< end]))
                    start = end
                }
                created = acc
            }
            dismiss()
            onCreated(created)
        } catch {
            errorMessage = error.localizedDescription
            errorPulse += 1
        }
    }
}

/// Left-aligned wrapping layout for chips that flow onto new rows as needed.
private struct FlowLayout: Layout {
    var spacing: CGFloat = 8

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout Void) -> CGSize {
        let maxWidth = proposal.width ?? .infinity
        var x: CGFloat = 0, y: CGFloat = 0, rowHeight: CGFloat = 0
        for view in subviews {
            let size = view.sizeThatFits(.unspecified)
            if x + size.width > maxWidth, x > 0 {
                x = 0
                y += rowHeight + spacing
                rowHeight = 0
            }
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
        return CGSize(width: maxWidth == .infinity ? x : maxWidth, height: y + rowHeight)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout Void) {
        var x = bounds.minX, y = bounds.minY, rowHeight: CGFloat = 0
        for view in subviews {
            let size = view.sizeThatFits(.unspecified)
            if x + size.width > bounds.maxX, x > bounds.minX {
                x = bounds.minX
                y += rowHeight + spacing
                rowHeight = 0
            }
            view.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
    }
}

/// A simple wrapping row of selectable chips (categories / campaigns).
private struct WrapChips: View {
    let items: [(String, String, Color)]
    @Binding var selection: Set<String>

    var body: some View {
        FlowLayout(spacing: 8) {
            ForEach(items, id: \.0) { id, label, color in
                let on = selection.contains(id)
                Button {
                    if on { selection.remove(id) } else { selection.insert(id) }
                } label: {
                    HStack(spacing: 6) {
                        Circle().fill(color).frame(width: 8, height: 8)
                        Text(label).font(.subheadline.weight(.medium)).lineLimit(1)
                    }
                    .padding(.horizontal, 12)
                    .frame(height: 34)
                    .background((on ? Tone.sky : Tone.slate).background, in: Capsule())
                    .foregroundStyle(on ? WTheme.accent : Color.primary)
                    .overlay(Capsule().strokeBorder(on ? WTheme.accent.opacity(0.5) : Color.clear, lineWidth: 1))
                }
                .buttonStyle(TapScaleStyle())
            }
        }
    }
}
