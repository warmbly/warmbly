// Live favicon notification badge — the red "unread" disc on the tab icon,
// like X / Gmail / Slack / Discord.
//
// Technique: draw the base favicon onto an offscreen canvas (clipped to a
// circle so the corner badge reads cleanly), overlay a red disc + white count,
// and point a dedicated <link rel="icon"> at the canvas data URL. At count 0 we
// remove the managed link so the static favicon resumes.
//
// Notes:
//   - The base PNG is same-origin (web/public), so the canvas is NOT tainted
//     and toDataURL() is safe.
//   - We render at devicePixelRatio so the small number stays crisp on retina.
//   - Safari historically ignores JS favicon changes — the document.title count
//     is the reliable cross-browser surface; the badge is progressive polish.

const BASE_ICON = "/favicon-96x96.png";
const LINK_ID = "favicon-badge";
const BADGE_COLOR = "#ef4444"; // red-500 — universal "unread" affordance

let baseReady: Promise<HTMLImageElement> | null = null;
let lastReq = 0;

function loadBase(): Promise<HTMLImageElement> {
    if (baseReady) return baseReady;
    baseReady = new Promise((resolve, reject) => {
        const img = new Image();
        img.onload = () => resolve(img);
        img.onerror = reject;
        img.src = BASE_ICON;
    });
    return baseReady;
}

function clearBadge() {
    document.getElementById(LINK_ID)?.remove();
}

/**
 * Render (or clear) the unread badge on the favicon. Count <= 0 restores the
 * plain favicon. Concurrent calls are guarded so a slower render can't clobber
 * a newer count.
 */
export async function setFaviconBadge(count: number): Promise<void> {
    if (typeof document === "undefined") return;
    const req = ++lastReq;

    if (!count || count <= 0) {
        clearBadge();
        return;
    }

    let img: HTMLImageElement;
    try {
        img = await loadBase();
    } catch {
        return; // base icon failed to load — leave the static favicon alone
    }
    if (req !== lastReq) return; // a newer count superseded this render

    const dpr = Math.max(1, Math.min(3, window.devicePixelRatio || 1));
    const size = 64;
    const canvas = document.createElement("canvas");
    canvas.width = size * dpr;
    canvas.height = size * dpr;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.scale(dpr, dpr);

    // Base, clipped to a circle so the corner badge sits cleanly (X/Slack look).
    ctx.save();
    ctx.beginPath();
    ctx.arc(size / 2, size / 2, size / 2, 0, Math.PI * 2);
    ctx.closePath();
    ctx.clip();
    ctx.drawImage(img, 0, 0, size, size);
    ctx.restore();

    // Badge geometry: top-right disc, ~30% radius, slight inset.
    const label = count > 9 ? "9+" : String(count);
    const r = size * 0.3;
    const cx = size - r - 1;
    const cy = r + 1;

    // White ring so the red disc separates from the icon.
    ctx.beginPath();
    ctx.arc(cx, cy, r + 2, 0, Math.PI * 2);
    ctx.fillStyle = "#ffffff";
    ctx.fill();

    // Red disc.
    ctx.beginPath();
    ctx.arc(cx, cy, r, 0, Math.PI * 2);
    ctx.fillStyle = BADGE_COLOR;
    ctx.fill();

    // Count.
    ctx.fillStyle = "#ffffff";
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    const fontSize = label.length > 1 ? size * 0.36 : size * 0.46;
    ctx.font = `bold ${fontSize}px -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif`;
    ctx.fillText(label, cx, cy + size * 0.02);

    // Swap the managed link (remove + re-append forces a reliable repaint).
    const url = canvas.toDataURL("image/png");
    clearBadge();
    const link = document.createElement("link");
    link.id = LINK_ID;
    link.rel = "icon";
    link.type = "image/png";
    link.href = url;
    document.head.appendChild(link);
}
