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

    // Rasterize an offscreen node to a PNG *data URL* (two-pass: prime the
    // font/embed cache, then capture). Returns null on failure. Does NOT
    // download — callers can preview the result first, then downloadPng it.
    //
    // pixelRatio defaults to 3 (crisp for the square card). Wide presets (16:9
    // at 1920px) get huge at 3x, so callers can pass a lower ratio (e.g. 2) to
    // keep the capture snappy.
    const renderPng = useCallback(
        async (
            node: HTMLElement | null,
            options?: { pixelRatio?: number; backgroundColor?: string },
        ): Promise<string | null> => {
            if (!node) return null;
            setExporting(true);
            try {
                try {
                    await document.fonts?.ready;
                } catch {
                    /* fonts.ready is best-effort */
                }
                const rect = node.getBoundingClientRect();
                const width = Math.round(rect.width || node.offsetWidth);
                const height = Math.round(rect.height || node.offsetHeight);
                const opts = {
                    pixelRatio: options?.pixelRatio ?? 3,
                    width,
                    height,
                    canvasWidth: width,
                    canvasHeight: height,
                    backgroundColor: options?.backgroundColor,
                    cacheBust: true,
                };
                await toPng(node, opts); // prime font/embed cache
                return await toPng(node, opts); // real capture
            } catch {
                return null;
            } finally {
                setExporting(false);
            }
        },
        [],
    );

    // Trigger a browser download of a PNG data URL (produced by renderPng).
    const downloadPng = useCallback(async (dataUrl: string, filename: string) => {
        const blob = await (await fetch(dataUrl)).blob();
        downloadBlob(blob, filename);
    }, []);

    // Convenience: render + download in one shot (no preview step).
    const exportPng = useCallback(
        async (node: HTMLElement | null, filename: string, options?: { pixelRatio?: number }) => {
            const url = await renderPng(node, options);
            if (url) await downloadPng(url, filename);
        },
        [renderPng, downloadPng],
    );

    return { exporting, renderPng, downloadPng, exportPng };
}
