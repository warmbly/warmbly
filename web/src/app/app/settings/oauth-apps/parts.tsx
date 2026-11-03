import { DashboardImage } from "@/components/ui/dashboard-image";

// Pieces shared by the OAuth apps page and its register dialog.

import React from "react";
import { CheckIcon, CopyIcon } from "lucide-react";

import { SearchInput } from "@/components/ui/field";
import type { WebhookEventDescriptor } from "@/lib/api/models/app/webhooks/Webhook";
import { cn } from "@/lib/utils";

export function CopyButton({ value, label, onCopied }: { value: string; label?: string; onCopied?: () => void }) {
    const [copied, setCopied] = React.useState(false);
    return (
        <button
            type="button"
            onClick={async () => {
                try {
                    await navigator.clipboard.writeText(value);
                    setCopied(true);
                    onCopied?.();
                    setTimeout(() => setCopied(false), 1500);
                } catch {
                    /* clipboard blocked */
                }
            }}
            className="inline-flex items-center gap-1 rounded-md border border-slate-200 px-2 h-7 text-[11.5px] text-slate-600 hover:bg-slate-50"
        >
            {copied ? <CheckIcon className="w-3.5 h-3.5 text-emerald-600" /> : <CopyIcon className="w-3.5 h-3.5" />}
            {copied ? "Copied" : (label ?? "Copy")}
        </button>
    );
}

// EventPicker — grouped by WebhookEventDescriptor.category with a search header
// and checkbox-square rows. Empty selection = "all events the granting org
// allows"; firehose events are flagged with an amber chip.
export function EventPicker({
    catalog,
    value,
    onChange,
}: {
    catalog: WebhookEventDescriptor[];
    value: string[];
    onChange: (next: string[]) => void;
}) {
    const [q, setQ] = React.useState("");
    const selected = new Set(value);

    const filtered = React.useMemo(() => {
        const needle = q.trim().toLowerCase();
        // A deprecated event is never delivered, so it is offered only to untick.
        const offered = catalog.filter((d) => !d.deprecated || value.includes(d.type));
        if (!needle) return offered;
        return offered.filter(
            (d) =>
                d.type.toLowerCase().includes(needle) ||
                d.category.toLowerCase().includes(needle) ||
                d.description.toLowerCase().includes(needle),
        );
    }, [catalog, q, value]);

    const grouped = React.useMemo(() => {
        const g: Record<string, WebhookEventDescriptor[]> = {};
        for (const d of filtered) (g[d.category] ??= []).push(d);
        return g;
    }, [filtered]);

    const toggle = (type: string) => {
        const next = new Set(selected);
        if (next.has(type)) next.delete(type);
        else next.add(type);
        onChange([...next]);
    };

    return (
        <div className="space-y-2.5">
            <div className="flex items-center gap-2">
                <SearchInput value={q} onChange={setQ} placeholder="Search events…" className="flex-1" />
                {value.length > 0 && (
                    <button
                        type="button"
                        onClick={() => onChange([])}
                        className="h-7 px-2.5 rounded-md border border-slate-200 text-[11.5px] text-slate-600 hover:bg-slate-50 shrink-0"
                    >
                        Subscribe to all
                    </button>
                )}
            </div>
            <div
                className={cn(
                    "rounded-md border px-2.5 py-2 text-[11.5px] leading-relaxed",
                    value.length === 0
                        ? "border-sky-200 bg-sky-50 text-sky-700"
                        : "border-slate-200 bg-slate-50 text-slate-500",
                )}
            >
                {value.length === 0 ? (
                    <>Subscribed to all events the granting org allows. New event types are included automatically (high-volume events excluded).</>
                ) : (
                    <>
                        Subscribed to <span className="font-medium">{value.length}</span>{" "}
                        {value.length === 1 ? "event" : "events"}. Leave none selected to receive all the events each org allows.
                    </>
                )}
            </div>
            <div className="max-h-[280px] overflow-y-auto rounded-md border border-slate-200 divide-y divide-slate-100">
                {Object.keys(grouped).length === 0 ? (
                    <div className="px-3 py-6 text-center text-[11.5px] text-slate-400">No events match.</div>
                ) : (
                    Object.entries(grouped).map(([cat, list]) => (
                        <div key={cat} className="p-1.5">
                            <div className="px-1.5 py-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                {cat}
                            </div>
                            <div className="space-y-0.5">
                                {list.map((d) => {
                                    const on = selected.has(d.type);
                                    return (
                                        <button
                                            key={d.type}
                                            type="button"
                                            onClick={() => toggle(d.type)}
                                            className={cn(
                                                "w-full flex items-start gap-2 rounded-md px-1.5 py-1.5 text-left transition-colors",
                                                on ? "bg-sky-50" : "hover:bg-slate-50",
                                            )}
                                        >
                                            <span
                                                className={cn(
                                                    "mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded",
                                                    on ? "bg-sky-600 text-white" : "border border-slate-300",
                                                )}
                                            >
                                                {on && <CheckIcon className="w-3 h-3" />}
                                            </span>
                                            <span className="min-w-0 flex-1">
                                                <span className="flex items-center gap-1.5">
                                                    <span className="block text-[12px] font-medium text-slate-700 font-mono truncate">
                                                        {d.type}
                                                    </span>
                                                    {d.firehose && (
                                                        <span className="shrink-0 inline-flex items-center rounded-sm bg-amber-50 border border-amber-200 px-1 text-[9.5px] uppercase tracking-[0.08em] font-semibold text-amber-700">
                                                            High volume
                                                        </span>
                                                    )}
                                                </span>
                                                {d.description && (
                                                    <span className="block text-[11px] text-slate-400 leading-tight">{d.description}</span>
                                                )}
                                            </span>
                                        </button>
                                    );
                                })}
                            </div>
                        </div>
                    ))
                )}
            </div>
            <p className="text-[10.5px] text-amber-700">
                High-volume events are only sent if you select them explicitly.
            </p>
        </div>
    );
}

const TILE_COLORS = ["bg-sky-600", "bg-indigo-600", "bg-emerald-600", "bg-rose-600", "bg-amber-600", "bg-fuchsia-600"];

// AppLogo renders an app's uploaded logo, or a colored letter tile as a fallback.
export function AppLogo({ name, url, size = "md" }: { name: string; url?: string | null; size?: "sm" | "md" | "lg" }) {
    const dim =
        size === "lg"
            ? "w-16 h-16 text-[22px] rounded-2xl"
            : size === "sm"
              ? "w-8 h-8 text-[12px] rounded-md"
              : "w-9 h-9 text-[13px] rounded-lg";
    if (url) return <DashboardImage src={url} alt={name} className={cn(dim, "object-cover border border-slate-200 shrink-0")} />;
    const letter = (name.trim()[0] ?? "?").toUpperCase();
    const color = TILE_COLORS[letter.charCodeAt(0) % TILE_COLORS.length];
    return <div className={cn(dim, color, "flex items-center justify-center text-white font-semibold shrink-0")}>{letter}</div>;
}
