import { useCallback, useState } from "react";
import { toPng } from "html-to-image";
import { downloadBlob } from "@/lib/api/client/app/contacts/exportContacts";

// Rasterize an offscreen DOM node to a crisp PNG and trigger a download.
//
// We use html-to-image (not html2canvas): the dashboard theme is Tailwind v4
// with oklch() colors, which html2canvas can't parse. html-to-image renders the
// node through an SVG <foreignObject> via the real browser engine, so oklch,
// CSS variables and gradients all rasterize correctly.
//
// Two passes: the first primes the embedded-font cache (a freshly-serialized
// foreignObject can render the first frame with fallback fonts); the second is
// the real capture. pixelRatio 3 keeps it sharp when shared/zoomed.
export default function useExportCard() {
    const [exporting, setExporting] = useState(false);

    const exportPng = useCallback(
        async (node: HTMLElement | null, filename: string) => {
            if (!node || exporting) return;
            setExporting(true);
            try {
                try {
                    await document.fonts?.ready;
                } catch {
                    /* fonts.ready is best-effort */
                }
                const opts = { pixelRatio: 3, backgroundColor: "#ffffff", cacheBust: true };
                await toPng(node, opts); // prime font/embed cache
                const dataUrl = await toPng(node, opts); // real capture
                const blob = await (await fetch(dataUrl)).blob();
                downloadBlob(blob, filename);
            } finally {
                setExporting(false);
            }
        },
        [exporting],
    );

    return { exporting, exportPng };
}
