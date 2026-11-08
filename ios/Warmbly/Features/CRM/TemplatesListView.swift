import SwiftUI
import WebKit

/// Reply templates pushed from settings: a hero with the count, native search,
/// and grouped cards showing each template's subject, snippet and the merge
/// fields it uses. Duplicate is gated behind manageCampaigns.
struct TemplatesListView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = TemplatesStore()
    @State private var openTemplate: EmailTemplate?
    @State private var shown = false
    @State private var duplicated = 0

    private var canWrite: Bool { env.session.can(.manageCampaigns) }
    private var isSearching: Bool { !store.query.trimmingCharacters(in: .whitespaces).isEmpty }

    var body: some View {
        @Bindable var store = store
        list
            .background(WTheme.groupedPage)
            .navigationTitle("Templates")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(WTheme.groupedPage, for: .navigationBar)
            .searchable(text: $store.query, placement: .navigationBarDrawer(displayMode: .automatic), prompt: "Search templates")
            .navigationDestination(item: $openTemplate) { template in
                TemplateDetailView(store: store, template: template)
            }
            .sensoryFeedback(.success, trigger: duplicated)
            .task(id: store.query) {
                if store.hasLoaded {
                    try? await Task.sleep(for: .milliseconds(350))
                }
                guard !Task.isCancelled else { return }
                await store.load(env.api)
            }
            .onChange(of: env.realtime.pulse(for: .templates)) {
                Task { await store.load(env.api) }
            }
            #if DEBUG
            .onChange(of: store.hasLoaded) {
                if ProcessInfo.processInfo.environment["WARMBLY_DEBUG_TEMPLATE"] != nil { openTemplate = store.templates.first }
            }
            #endif
    }

    // MARK: Hero

    private var hero: some View {
        let latest = store.templates.compactMap(\.updatedAt).max()
        return HStack(spacing: 14) {
            SettingsIcon(symbol: "text.bubble.fill", color: Tone.indigo.color, size: 50)
                .symbolEffect(.bounce, value: shown)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    CountUpText(value: shown ? Double(store.templates.count) : 0, format: { "\(Int($0.rounded()))" })
                        .font(.system(size: 28, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(WTheme.ink)
                        .animation(.spring(response: 0.9, dampingFraction: 0.9).delay(0.1), value: shown)
                    Text(store.templates.count == 1 ? "reply template" : "reply templates")
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(WTheme.inkSecondary)
                }
                Text(latest.map { "Last edited \(WFormat.relative($0))" } ?? "Insert one from any reply in the inbox.")
                    .font(.system(size: 13))
                    .foregroundStyle(WTheme.inkTertiary)
            }
            Spacer(minLength: 0)
        }
        .coverCard()
        .reveal(shown, 0)
    }

    // MARK: List

    @ViewBuilder
    private var list: some View {
        if store.isLoading, !store.hasLoaded {
            ScrollView { AnalyticsLoadingCards().padding(16) }
        } else if let error = store.errorMessage, store.templates.isEmpty {
            ErrorStateView(title: "Couldn't load templates", message: error) {
                await store.load(env.api)
            }
        } else {
            List {
                if !isSearching {
                    Section {
                        hero
                            .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 0, trailing: 0))
                            .listRowBackground(Color.clear)
                            .listRowSeparator(.hidden)
                    }
                }
                if store.templates.isEmpty {
                    Section {
                        emptyState
                            .listRowBackground(Color.clear)
                            .listRowSeparator(.hidden)
                    }
                } else {
                    Section {
                        ForEach(Array(store.templates.enumerated()), id: \.element.id) { index, template in
                            row(template, index: index)
                        }
                    } header: {
                        CoverSectionHeader(title: isSearching ? "Results" : "All templates", count: store.templates.count)
                    }
                    Section {
                        CRMEndMarker(store.templates.count, "template")
                    }
                }
            }
            .listStyle(.insetGrouped)
            .readableMargins()
            .listSectionSpacing(18)
            .contentMargins(.horizontal, 16, for: .scrollContent)
            .contentMargins(.top, 4, for: .scrollContent)
            .scrollContentBackground(.hidden)
            .scrollDismissesKeyboard(.immediately)
            .defaultScrollAnchor(SettingsDebug.anchor)
            .refreshable { await store.load(env.api) }
            .onAppear { shown = true }
        }
    }

    @ViewBuilder
    private var emptyState: some View {
        if isSearching {
            CoverEmptyState(symbol: "magnifyingglass", color: WTheme.inkSecondary, title: "No matching templates", message: "Try a different search.")
        } else {
            CoverEmptyState(
                symbol: "text.bubble",
                color: Tone.indigo.color,
                title: "No templates yet",
                message: "Save a reply you send often on the web dashboard and it shows up here."
            )
        }
    }

    // MARK: Row

    private func row(_ template: EmailTemplate, index: Int) -> some View {
        let tone = TemplateTone.of(template.name)
        return Button {
            openTemplate = template
        } label: {
            HStack(alignment: .top, spacing: 12) {
                Text(String(template.name.prefix(1)).uppercased())
                    .font(.system(size: 17, weight: .semibold, design: .rounded))
                    .foregroundStyle(tone.color)
                    .frame(width: 40, height: 40)
                    .background(tone.background, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                VStack(alignment: .leading, spacing: 4) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(template.name)
                            .font(.system(size: 15.5, weight: .semibold))
                            .foregroundStyle(WTheme.ink)
                            .lineLimit(1)
                        Spacer(minLength: 8)
                        if let updated = template.updatedAt {
                            Text(WFormat.relative(updated))
                                .font(.system(size: 12))
                                .monospacedDigit()
                                .foregroundStyle(WTheme.inkTertiary)
                        }
                    }
                    if let subject = template.subject, !subject.isEmpty {
                        Text(EmailTemplate.tinted(subject))
                            .font(.system(size: 13.5, weight: .medium))
                            .foregroundStyle(WTheme.inkSecondary)
                            .lineLimit(1)
                    }
                    Text(EmailTemplate.tinted(template.snippet))
                        .font(.system(size: 13.5))
                        .foregroundStyle(WTheme.inkTertiary)
                        .lineLimit(2)
                }
            }
            .padding(.vertical, 6)
            .contentShape(Rectangle())
        }
        .buttonStyle(MoreRowPressStyle())
        .listRowBackground(WTheme.groupedCard)
        .listRowInsets(EdgeInsets(top: 8, leading: 14, bottom: 8, trailing: 14))
        .alignmentGuide(.listRowSeparatorLeading) { _ in 66 }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if canWrite {
                Button {
                    Task { await duplicate(template) }
                } label: {
                    Label("Duplicate", systemImage: "plus.square.on.square")
                }
                .tint(WTheme.accent)
            }
        }
        .contextMenu {
            Button {
                UIPasteboard.general.string = template.bodyPlain ?? template.snippet
            } label: {
                Label("Copy text", systemImage: "doc.on.doc")
            }
            if canWrite {
                Button {
                    Task { await duplicate(template) }
                } label: {
                    Label("Duplicate", systemImage: "plus.square.on.square")
                }
            }
        }
    }

    private func duplicate(_ template: EmailTemplate) async {
        if (try? await store.duplicate(env.api, template: template)) != nil {
            duplicated += 1
        }
    }
}

// MARK: - Merge fields

extension EmailTemplate {
    /// The `{{field}}` names a template fills in, in first-use order.
    var mergeFields: [String] {
        let text = [subject, bodyPlain ?? bodyHTML].compactMap { $0 }.joined(separator: " ")
        guard let regex = try? Regex(#"\{\{\s*([A-Za-z0-9_.]+)[^}]*\}\}"#) else { return [] }
        var seen: [String] = []
        for match in text.matches(of: regex) {
            if let name = match.output[1].substring.map(String.init), !seen.contains(name) {
                seen.append(name)
            }
        }
        return seen
    }
}

extension EmailTemplate {
    /// Text with its `{{field}}` tokens tinted, so they read as fill-ins.
    static func tinted(_ text: String) -> AttributedString {
        var result = AttributedString(text)
        guard let regex = try? Regex(#"\{\{[^}]*\}\}"#) else { return result }
        for match in text.matches(of: regex) {
            if let range = Range(match.range, in: result) {
                result[range].foregroundColor = Tone.indigo.color
            }
        }
        return result
    }

    /// A merge field as words: `originalSubject` and `first_name` become
    /// "Original subject" and "First name".
    static func fieldLabel(_ field: String) -> String {
        var words = ""
        for char in field {
            if char == "_" || char == "." {
                words.append(" ")
            } else if char.isUppercase, !words.isEmpty, words.last != " " {
                words.append(" ")
                words.append(contentsOf: char.lowercased())
            } else {
                words.append(char)
            }
        }
        let lower = words.lowercased()
        return lower.prefix(1).uppercased() + lower.dropFirst()
    }
}

/// A short row of merge-field chips, with a "+N" for the rest.
struct MergeFieldChips: View {
    let fields: [String]
    var limit = 99

    var body: some View {
        HStack(spacing: 5) {
            ForEach(fields.prefix(limit), id: \.self) { field in
                Text(EmailTemplate.fieldLabel(field))
                    .font(.system(size: 11.5, weight: .medium))
                    .foregroundStyle(Tone.indigo.color)
                    .lineLimit(1)
                    .padding(.horizontal, 7)
                    .padding(.vertical, 3)
                    .background(Tone.indigo.background, in: Capsule())
            }
            if fields.count > limit {
                Text("+\(fields.count - limit)")
                    .font(.system(size: 11.5, weight: .medium))
                    .foregroundStyle(WTheme.inkTertiary)
            }
        }
    }
}

/// A stable tone per template name, so its monogram keeps its color.
private enum TemplateTone {
    static func of(_ name: String) -> Tone {
        let tones: [Tone] = [.sky, .indigo, .emerald, .orange, .amber, .rose]
        let sum = name.unicodeScalars.reduce(0) { $0 &+ Int($1.value) }
        return tones[abs(sum) % tones.count]
    }
}

// MARK: - Detail

/// Template detail: the subject and merge fields over an email-style preview
/// of the rendered body (POST /templates/:id/render with empty variables,
/// falling back to the stored body).
struct TemplateDetailView: View {
    @Environment(AppEnvironment.self) private var env
    let store: TemplatesStore
    let template: EmailTemplate

    @State private var rendered: TemplateRenderResult?
    @State private var isRendering = false
    @State private var duplicating = false
    @State private var copied = false
    @State private var duplicated = 0

    private var canWrite: Bool { env.session.can(.manageCampaigns) }

    private var subject: String {
        let value = rendered?.subject ?? template.subject
        return (value?.isEmpty == false) ? value! : "No subject"
    }

    private var html: String? {
        let value = rendered?.bodyHTML ?? template.bodyHTML
        return (value?.isEmpty == false) ? value : nil
    }

    private var plain: String? {
        let value = rendered?.bodyPlain ?? template.bodyPlain
        return (value?.isEmpty == false) ? value : nil
    }

    var body: some View {
        VStack(spacing: 12) {
            header
            preview
            actions
        }
        .padding(.horizontal, 16)
        .padding(.top, 8)
        // Clears the floating tab bar.
        .padding(.bottom, 78)
        .background(WTheme.groupedPage)
        .navigationTitle(template.name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(WTheme.groupedPage, for: .navigationBar)
        .presenceResource("template:\(template.id)")
        .sensoryFeedback(.success, trigger: duplicated)
        .sensoryFeedback(.selection, trigger: copied)
        .task {
            await render()
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Subject")
                .font(.system(size: 12.5, weight: .medium))
                .foregroundStyle(WTheme.inkTertiary)
            Text(EmailTemplate.tinted(subject))
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(WTheme.ink)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
            let fields = template.mergeFields
            if !fields.isEmpty {
                HStack(spacing: 8) {
                    Text("Fills in")
                        .font(.system(size: 12.5, weight: .medium))
                        .foregroundStyle(WTheme.inkTertiary)
                    ScrollView(.horizontal) {
                        MergeFieldChips(fields: fields)
                    }
                    .scrollIndicators(.hidden)
                }
                .padding(.top, 2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .coverCard(padding: 16)
    }

    @ViewBuilder
    private var preview: some View {
        Group {
            if isRendering, rendered == nil {
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(0..<5, id: \.self) { line in
                        RoundedRectangle(cornerRadius: 4)
                            .fill(WTheme.fill)
                            .frame(height: 12)
                            .frame(maxWidth: line == 4 ? 160 : .infinity)
                    }
                    Spacer()
                }
                .padding(18)
                .skeletonShimmer()
            } else if let html {
                HTMLBodyView(html: html)
            } else if let plain {
                ScrollView {
                    Text(plain)
                        .font(.system(size: 15.5))
                        .lineSpacing(3)
                        .foregroundStyle(WTheme.ink)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(18)
                }
            } else {
                CoverEmptyState(symbol: "doc", color: WTheme.inkSecondary, title: "Empty template", message: "This template has no body yet.")
                    .frame(maxHeight: .infinity)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(WTheme.groupedCard, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
        .clipShape(RoundedRectangle(cornerRadius: 22, style: .continuous))
        .transition(.opacity)
        .animation(.easeOut(duration: 0.25), value: rendered == nil)
    }

    private var actions: some View {
        HStack(spacing: 10) {
            Button {
                UIPasteboard.general.string = plain ?? template.snippet
                withAnimation(.spring(response: 0.35, dampingFraction: 0.7)) { copied = true }
                Task {
                    try? await Task.sleep(for: .seconds(1.6))
                    withAnimation(.snappy) { copied = false }
                }
            } label: {
                Label(copied ? "Copied" : "Copy text", systemImage: copied ? "checkmark" : "doc.on.doc")
                    .contentTransition(.symbolEffect(.replace))
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(copied ? WTheme.positive : WTheme.ink)
                    .frame(maxWidth: .infinity)
                    .frame(height: 48)
                    .background(WTheme.groupedCard, in: Capsule())
            }
            .buttonStyle(TapScaleStyle())
            if canWrite {
                Button {
                    duplicate()
                } label: {
                    HStack(spacing: 8) {
                        if duplicating {
                            ProgressView().controlSize(.small).tint(.white)
                        } else {
                            Image(systemName: "plus.square.on.square")
                        }
                        Text("Duplicate")
                    }
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(WTheme.inkInverse)
                    .frame(maxWidth: .infinity)
                    .frame(height: 48)
                    .background(WTheme.ink, in: Capsule())
                }
                .buttonStyle(TapScaleStyle())
                .disabled(duplicating)
            }
        }
    }

    private func render() async {
        guard rendered == nil, !isRendering else { return }
        isRendering = true
        // Empty variables render as the send path would; failure falls back to raw body.
        rendered = try? await env.api.post(
            "templates/\(template.id)/render",
            body: TemplateRenderBody(variables: [:])
        )
        isRendering = false
    }

    private func duplicate() {
        guard !duplicating else { return }
        duplicating = true
        Task {
            if (try? await store.duplicate(env.api, template: template)) != nil {
                duplicated += 1
            }
            duplicating = false
        }
    }
}

// MARK: - HTML body

/// Renders sanitized template HTML in a non-scrolling-locked WKWebView, wrapped
/// with a minimal responsive style block so it reads on a phone.
struct HTMLBodyView: UIViewRepresentable {
    let html: String

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.defaultWebpagePreferences.allowsContentJavaScript = false
        let webView = WKWebView(frame: .zero, configuration: config)
        webView.isOpaque = false
        webView.backgroundColor = .clear
        webView.scrollView.backgroundColor = .clear
        return webView
    }

    func updateUIView(_ webView: WKWebView, context: Context) {
        webView.loadHTMLString(wrapped(html), baseURL: nil)
    }

    private func wrapped(_ body: String) -> String {
        """
        <!doctype html><html><head>
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <style>
        :root { color-scheme: light dark; }
        body { font: -apple-system-body; margin: 18px; word-break: break-word; }
        img { max-width: 100%; height: auto; }
        table { max-width: 100%; }
        a { color: #0284C7; }
        @media (prefers-color-scheme: dark) { a { color: #38BDF8; } }
        </style></head><body>\(body)</body></html>
        """
    }
}
