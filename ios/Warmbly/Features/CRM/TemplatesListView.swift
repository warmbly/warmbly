import SwiftUI
import WebKit

/// Reply templates list pushed from the More tab. Rows show name + snippet;
/// tapping pushes a detail with the rendered preview. Duplicate is gated behind
/// manageCampaigns. Reloads on the .templates pulse.
struct TemplatesListView: View {
    @Environment(AppEnvironment.self) private var env
    @State private var store = TemplatesStore()

    private var canWrite: Bool { env.session.can(.manageCampaigns) }

    var body: some View {
        @Bindable var store = store
        Group {
            if store.isLoading, !store.hasLoaded {
                ScrollView { SkeletonRows(rows: 8) }
            } else if let error = store.errorMessage, store.templates.isEmpty {
                ErrorStateView(title: "Couldn't load templates", message: error) {
                    await store.load(env.api)
                }
            } else if store.templates.isEmpty {
                emptyState
            } else {
                List {
                    ForEach(store.templates) { template in
                        NavigationLink {
                            TemplateDetailView(store: store, template: template)
                        } label: {
                            row(template)
                        }
                        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                            if canWrite {
                                Button {
                                    Task { try? await store.duplicate(env.api, template: template) }
                                } label: {
                                    Label("Duplicate", systemImage: "plus.square.on.square")
                                }
                                .tint(WTheme.accent)
                            }
                        }
                    }
                }
                .listStyle(.plain)
                .refreshable { await store.load(env.api) }
            }
        }
        .navigationTitle("Templates")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                PresenceAvatars()
            }
        }
        .searchable(text: $store.query, prompt: "Search templates")
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
    }

    @ViewBuilder
    private var emptyState: some View {
        let searching = !store.query.trimmingCharacters(in: .whitespaces).isEmpty
        EmptyStateView(
            title: searching ? "No matching templates" : "No templates yet",
            message: searching
                ? "Try a different search."
                : "Reply templates you save on the web dashboard show up here."
        )
    }

    private func row(_ template: EmailTemplate) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(template.name)
                .font(.body.weight(.medium))
                .lineLimit(1)
            if let subject = template.subject, !subject.isEmpty {
                Text(subject)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Text(template.snippet)
                .font(.footnote)
                .foregroundStyle(.tertiary)
                .lineLimit(1)
        }
        .padding(.vertical, 6)
    }
}

// MARK: - Detail

/// Template detail: renders the body (POST /templates/:id/render with empty
/// variables, mirroring the send path) and falls back to the raw stored body if
/// render fails. Plain HTML shows in a WKWebView; plain text in a scroll view.
struct TemplateDetailView: View {
    @Environment(AppEnvironment.self) private var env
    let store: TemplatesStore
    let template: EmailTemplate

    @State private var rendered: TemplateRenderResult?
    @State private var isRendering = false
    @State private var duplicating = false

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
        VStack(spacing: 0) {
            header
            Divider()
            preview
        }
        .navigationTitle(template.name)
        .navigationBarTitleDisplayMode(.inline)
        .presenceResource("template:\(template.id)")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                if canWrite {
                    if duplicating {
                        ProgressView().controlSize(.small)
                    } else {
                        Button {
                            duplicate()
                        } label: {
                            Image(systemName: "plus.square.on.square")
                        }
                        .accessibilityLabel("Duplicate template")
                    }
                }
            }
        }
        .task {
            await render()
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            EyebrowLabel("Subject")
            Text(subject)
                .font(.body.weight(.semibold))
                .textSelection(.enabled)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
    }

    @ViewBuilder
    private var preview: some View {
        if isRendering, rendered == nil {
            VStack {
                Spacer()
                ProgressView()
                Spacer()
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if let html {
            HTMLBodyView(html: html)
        } else if let plain {
            ScrollView {
                Text(plain)
                    .font(.body)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(16)
            }
        } else {
            EmptyStateView(title: "Empty template", message: "This template has no body content.")
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
            try? await store.duplicate(env.api, template: template)
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
        body { font: -apple-system-body; margin: 16px; word-break: break-word; }
        img { max-width: 100%; height: auto; }
        table { max-width: 100%; }
        a { color: #0284C7; }
        @media (prefers-color-scheme: dark) { a { color: #38BDF8; } }
        </style></head><body>\(body)</body></html>
        """
    }
}
