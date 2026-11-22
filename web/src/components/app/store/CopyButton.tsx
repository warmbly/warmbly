// A small copy button that turns into a check for a moment once the value is on the clipboard.
import React from "react";
import toast from "react-hot-toast/headless";
import { CheckIcon, CopyIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export default function CopyButton({ value, label, className, text }: { value: string; label: string; className?: string; text?: boolean }) {
    const [copied, setCopied] = React.useState(false);
    React.useEffect(() => {
        if (!copied) return;
        const t = window.setTimeout(() => setCopied(false), 1600);
        return () => window.clearTimeout(t);
    }, [copied]);
    const copy = async (e: React.MouseEvent) => {
        e.stopPropagation();
        try {
            await navigator.clipboard.writeText(value);
            setCopied(true);
        } catch {
            toast.error("Copy did not work here. Select the text and copy it.");
        }
    };
    return (
        <button
            type="button"
            onClick={(e) => void copy(e)}
            aria-label={copied ? `${label} copied` : `Copy ${label}`}
            title={copied ? "Copied" : "Copy"}
            className={cn(
                "shrink-0 inline-flex items-center justify-center gap-1 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-200",
                text ? "h-6 px-1.5 text-[11px] font-medium" : "size-6",
                className,
            )}
        >
            {copied ? <CheckIcon className="w-3 h-3 text-emerald-600" /> : <CopyIcon className="w-3 h-3" />}
            {text && (copied ? "Copied" : "Copy")}
        </button>
    );
}
