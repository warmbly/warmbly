import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, CopyIcon, DownloadIcon, ImageIcon, Loader2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast";
import useExportCard from "@/hooks/useExportCard";
import StatsShareCard, { type ShareAspect, type ShareCardData } from "./StatsShareCard";

// Drops a "Share image" button next to any analytics surface. It keeps a
// branded <StatsShareCard> mounted off-viewport (real layout, so the capture
// isn't blank) and, on click, opens a preview modal that rasterizes the card to
// a PNG. The user picks an aspect preset (1:1 / 3:2 / 16:9) and previews it
// before choosing to download or copy.

const ASPECTS: { value: ShareAspect; label: string; ratio: string; suffix: string }[] = [
    { value: "1:1", label: "1:1", ratio: "1 / 1", suffix: "1x1" },
    { value: "3:2", label: "3:2", ratio: "3 / 2", suffix: "3x2" },
    { value: "16:9", label: "16:9", ratio: "16 / 9", suffix: "16x9" },
];

// Apply a preset suffix before the ".png" extension (e.g. "warmbly-x.png" ->
// "warmbly-x-16x9.png"). Falls back to appending if there's no .png.
function withPresetSuffix(filename: string, suffix: string): string {
    const i = filename.toLowerCase().lastIndexOf(".png");
    if (i === -1) return `${filename}-${suffix}.png`;
    return `${filename.slice(0, i)}-${suffix}${filename.slice(i)}`;
}

export default function AnalyticsShareButton({
    data,
    filename,
    label = "Share image",
}: {
    data: ShareCardData;
    filename: string;
    label?: string;
}) {
    const ref = useRef<HTMLDivElement>(null);
    const { renderPng, downloadPng } = useExportCard();
    const [open, setOpen] = useState(false);
    const [aspect, setAspect] = useState<ShareAspect>("1:1");
    const [url, setUrl] = useState<string | null>(null);
    const [copied, setCopied] = useState(false);

    // Render the PNG when the preview opens AND whenever the aspect changes;
    // reset when it closes. The capture is heavy, so defer a frame to let the
    // off-viewport card relayout for the new aspect first. Square stays crisp at
    // pixelRatio 3; wider presets drop to 2 so the (much larger) capture is
    // snappy.
    useEffect(() => {
        if (!open) {
            setUrl(null);
            setCopied(false);
            return;
        }
        let alive = true;
        setUrl(null); // show the spinner while re-capturing for the new aspect
        // Double rAF: the off-viewport card re-lays-out for the new aspect on
        // the next frame; capturing in a second frame guarantees we rasterize
        // the committed-and-painted new size, not the previous one.
        let raf2 = 0;
        const raf1 = requestAnimationFrame(() => {
            raf2 = requestAnimationFrame(async () => {
                const out = await renderPng(ref.current, {
                    pixelRatio: aspect === "1:1" ? 3 : 2,
                    backgroundColor: "#18abed",
                });
                if (alive) setUrl(out);
            });
        });
        return () => {
            alive = false;
            cancelAnimationFrame(raf1);
            cancelAnimationFrame(raf2);
        };
    }, [open, aspect, renderPng]);

    // Esc closes the preview.
    useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") setOpen(false);
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [open]);

    async function copy() {
        if (!url) return;
        try {
            // Pass a Promise<Blob> to ClipboardItem rather than awaiting the blob
            // first: browsers (especially Safari) require clipboard.write() to run
            // within the click's user-gesture, and an `await fetch(...)` beforehand
            // forfeits it → NotAllowedError. The Promise form keeps the gesture.
            const item = new ClipboardItem({ "image/png": fetch(url).then((r) => r.blob()) });
            await navigator.clipboard.write([item]);
            setCopied(true);
            toast.success("Image copied to clipboard");
            setTimeout(() => setCopied(false), 1500);
        } catch {
            toast.error("Couldn't copy — download instead");
        }
    }

    const current = ASPECTS.find((a) => a.value === aspect) ?? ASPECTS[0];

    return (
        <>
            <button
                type="button"
                onClick={() => setOpen(true)}
                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 bg-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
            >
                <ImageIcon className="w-3.5 h-3.5" />
                {label}
            </button>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                        onMouseDown={() => setOpen(false)}
                    >
                        <motion.div
                            initial={{ opacity: 0, scale: 0.97, y: 8 }}
                            animate={{ opacity: 1, scale: 1, y: 0 }}
                            exit={{ opacity: 0, scale: 0.97, y: 8 }}
                            transition={{ duration: 0.18, ease: "easeOut" }}
                            onMouseDown={(e) => e.stopPropagation()}
                            className="w-full max-w-[min(94vw,860px)] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden"
                        >
                            <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2 shrink-0">
                                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                    Share image
                                </span>
                                <div className="h-4 w-px bg-slate-200" />
                                <span className="text-[12.5px] text-slate-600 truncate">
                                    Preview before download
                                </span>
                                <button
                                    type="button"
                                    onClick={() => setOpen(false)}
                                    aria-label="Close"
                                    className="ml-auto size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                                >
                                    <XIcon className="w-4 h-4" />
                                </button>
                            </div>

                            {/* aspect preset selector (house theme segmented control) */}
                            <div className="px-4 pt-3 flex items-center gap-2">
                                <span className="text-[11px] text-slate-500 font-medium">Aspect</span>
                                <div className="inline-flex items-center gap-1 rounded-md bg-slate-100 p-0.5">
                                    {ASPECTS.map((a) => {
                                        const active = a.value === aspect;
                                        return (
                                            <button
                                                key={a.value}
                                                type="button"
                                                onClick={() => setAspect(a.value)}
                                                aria-pressed={active}
                                                className={`h-7 px-3 rounded text-[12px] font-medium transition-colors ${
                                                    active
                                                        ? "bg-sky-600 text-white shadow-sm"
                                                        : "text-slate-600 hover:text-slate-900"
                                                }`}
                                            >
                                                {a.label}
                                            </button>
                                        );
                                    })}
                                </div>
                            </div>

                            <div className="px-4 py-4 bg-slate-50/40 flex justify-center">
                                <div
                                    className="relative max-w-full rounded-md border border-slate-200 bg-white overflow-hidden"
                                    style={{ aspectRatio: current.ratio, height: "min(460px, 58vh)" }}
                                >
                                    {url ? (
                                        <img
                                            src={url}
                                            alt="Analytics share preview"
                                            className="block w-full h-full object-fill"
                                        />
                                    ) : (
                                        <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 text-slate-400">
                                            <Loader2Icon className="w-5 h-5 animate-spin" />
                                            <span className="text-[11.5px]">Rendering…</span>
                                        </div>
                                    )}
                                </div>
                            </div>

                            <div className="px-4 py-3 border-t border-slate-200 flex items-center justify-end gap-2">
                                <button
                                    type="button"
                                    onClick={copy}
                                    disabled={!url}
                                    className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-slate-700 hover:text-slate-900 text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                                >
                                    {copied ? (
                                        <CheckIcon className="w-3.5 h-3.5 text-emerald-600" />
                                    ) : (
                                        <CopyIcon className="w-3.5 h-3.5" />
                                    )}
                                    {copied ? "Copied" : "Copy"}
                                </button>
                                <button
                                    type="button"
                                    onClick={() =>
                                        url && downloadPng(url, withPresetSuffix(filename, current.suffix))
                                    }
                                    disabled={!url}
                                    className="h-8 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                                >
                                    <DownloadIcon className="w-3.5 h-3.5" />
                                    Download PNG
                                </button>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            {/* Off-viewport but laid out, so html-to-image captures real pixels. */}
            <div aria-hidden className="fixed pointer-events-none -z-10" style={{ left: -99999, top: 0 }}>
                <StatsShareCard ref={ref} data={data} aspect={aspect} />
            </div>
        </>
    );
}
