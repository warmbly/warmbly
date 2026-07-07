import SwiftUI
import WebKit

/// Renders an email HTML body in a WKWebView sized to its content, with a
/// plain-text fallback. Height is reported back through a binding so the view
/// participates in normal scrolling instead of nesting a scroll region.
struct UniboxWebView: UIViewRepresentable {
    let html: String?
    let plain: String?
    @Binding var height: CGFloat

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.defaultWebpagePreferences.allowsContentJavaScript = false
        // A realistic starting frame: a .zero-sized webview created mid
        // navigation transition can lay out at width 0 and blank its first load.
        let initialFrame = CGRect(x: 0, y: 0, width: 360, height: 120)
        let webView = WKWebView(frame: initialFrame, configuration: config)
        webView.navigationDelegate = context.coordinator
        webView.scrollView.isScrollEnabled = false
        webView.scrollView.bounces = false
        webView.isOpaque = false
        webView.backgroundColor = .clear
        webView.scrollView.backgroundColor = .clear
        // Track content size continuously: one-shot didFinish measures before
        // images/relayout settle and clips the message.
        context.coordinator.sizeObservation = webView.scrollView.observe(\.contentSize) { scrollView, _ in
            let height = scrollView.contentSize.height
            Task { @MainActor [weak coordinator = context.coordinator] in
                guard let coordinator, height > 1 else { return }
                if abs(coordinator.parent.height - height) > 1 {
                    coordinator.parent.height = height
                }
            }
        }
        return webView
    }

    func updateUIView(_ webView: WKWebView, context: Context) {
        context.coordinator.parent = self
        let document = Self.document(html: html, plain: plain)
        // Reload when the content changed, or when the first load never
        // committed (url stays nil if it was dropped mid-transition) — that is
        // the "message stays blank until you tap it" failure.
        let firstLoadDied = webView.url == nil && !webView.isLoading
        guard document != context.coordinator.lastLoaded || firstLoadDied else { return }
        context.coordinator.lastLoaded = document
        webView.loadHTMLString(document, baseURL: nil)
    }

    // MARK: HTML shell

    private static func document(html: String?, plain: String?) -> String {
        let body: String
        if let html, !html.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            body = html
        } else {
            body = "<pre class=\"plain\">\(escape(plain ?? ""))</pre>"
        }
        return """
        <!doctype html><html><head>
        <meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1">
        <style>
          :root { color-scheme: light dark; }
          html, body { margin: 0; padding: 0; background: transparent; }
          body {
            font: -apple-system-body;
            font-family: -apple-system, system-ui, sans-serif;
            font-size: 15px; line-height: 1.5;
            color: #0F172A; word-break: break-word;
            -webkit-text-size-adjust: 100%;
          }
          @media (prefers-color-scheme: dark) { body { color: #E2E8F0; } }
          img, table { max-width: 100% !important; height: auto; }
          a { color: #0284C7; }
          pre.plain { white-space: pre-wrap; font-family: -apple-system, system-ui, sans-serif; }
          blockquote { margin: 0 0 0 8px; padding-left: 10px; border-left: 3px solid #CBD5E1; color: #64748B; }
        </style>
        </head><body>\(body)</body></html>
        """
    }

    private static func escape(_ raw: String) -> String {
        raw
            .replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
    }

    // MARK: Coordinator

    @MainActor
    final class Coordinator: NSObject, WKNavigationDelegate {
        var parent: UniboxWebView
        var lastLoaded: String?
        var sizeObservation: NSKeyValueObservation?

        init(_ parent: UniboxWebView) { self.parent = parent }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            measure(webView)
            // Second pass after layout settles; the first measurement can run
            // before the final width is applied.
            Task { @MainActor [weak self, weak webView] in
                try? await Task.sleep(for: .milliseconds(300))
                guard let self, let webView else { return }
                self.measure(webView)
            }
        }

        private func measure(_ webView: WKWebView) {
            webView.evaluateJavaScript("document.body.scrollHeight") { [weak self] result, _ in
                guard let self, let value = result as? CGFloat, value > 1 else { return }
                if abs(self.parent.height - value) > 1 { self.parent.height = value }
            }
        }

        // Open tapped links in Safari, not inside the message frame.
        func webView(
            _ webView: WKWebView,
            decidePolicyFor navigationAction: WKNavigationAction
        ) async -> WKNavigationActionPolicy {
            if navigationAction.navigationType == .linkActivated, let url = navigationAction.request.url {
                await UIApplication.shared.open(url)
                return .cancel
            }
            return .allow
        }
    }
}
