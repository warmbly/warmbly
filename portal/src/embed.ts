// Embed plumbing: a page runs inside an iframe the /embed.js loader mounts and reports its height so the
// frame never clips. Message names are the contract with portal/public/embed.js.

export function isEmbedded(): boolean {
    return new URLSearchParams(window.location.search).get("embed") === "1";
}

/** EmbeddedPage is a bearer-token page the loader frames from data-warmbly-connect or data-warmbly-report. */
export type EmbeddedPage = "connect" | "report";

/** startPageResizeReporting posts the document height to the parent on every size change; the loader matches its frame by source. */
export function startPageResizeReporting(page: EmbeddedPage): () => void {
    if (window.parent === window) return () => {};
    const post = () => {
        try {
            window.parent.postMessage({ type: "warmbly:resize", page, height: document.documentElement.scrollHeight }, "*");
        } catch {
            // sandboxed parents can refuse messages; the frame just keeps its height
        }
    };
    post();
    const ro = new ResizeObserver(post);
    ro.observe(document.body);
    return () => ro.disconnect();
}
