import { useRef } from "react";
import { DownloadIcon, Loader2Icon } from "lucide-react";
import useExportCard from "@/hooks/useExportCard";
import StatsShareCard, { type ShareCardData } from "./StatsShareCard";

// Drops a "Share image" button next to any analytics surface. It keeps a
// branded <StatsShareCard> mounted off-viewport (real layout, so the capture
// isn't blank) and rasterizes it to a downloadable PNG on click.
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
    const { exporting, exportPng } = useExportCard();

    return (
        <>
            <button
                type="button"
                onClick={() => exportPng(ref.current, filename)}
                disabled={exporting}
                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 bg-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
            >
                {exporting ? (
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                ) : (
                    <DownloadIcon className="w-3.5 h-3.5" />
                )}
                {exporting ? "Rendering…" : label}
            </button>

            {/* Off-viewport but laid out, so html-to-image captures real pixels. */}
            <div aria-hidden className="fixed pointer-events-none -z-10" style={{ left: -99999, top: 0 }}>
                <StatsShareCard ref={ref} data={data} />
            </div>
        </>
    );
}
