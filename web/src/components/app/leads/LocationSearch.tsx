import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2Icon, MapPinIcon, NavigationIcon, XIcon } from "lucide-react";
import useClickOutside from "@/hooks/useClickOutside";
import { geocodeLocation } from "@/lib/api/client/app/leads/leads";
import type { GeocodeResult, LatLng } from "@/lib/api/models/app/leads/Lead";
import { cn } from "@/lib/utils";

// A typed place that moves the map; suggestions come from the instance's geocoder.
export default function LocationSearch({
    value,
    onChange,
    near,
    onPick,
}: {
    value: string;
    onChange: (v: string) => void;
    near: LatLng | null;
    onPick: (r: GeocodeResult) => void;
}) {
    const [open, setOpen] = useState(false);
    const [results, setResults] = useState<GeocodeResult[]>([]);
    const [loading, setLoading] = useState(false);
    const [active, setActive] = useState(0);
    const box = useRef<HTMLDivElement>(null);
    const typed = useRef(false);
    useClickOutside(open, () => setOpen(false), box);

    useEffect(() => {
        if (!typed.current) return;
        const q = value.trim();
        if (q.length < 2) {
            setResults([]);
            return;
        }
        const ctrl = new AbortController();
        setLoading(true);
        const t = window.setTimeout(() => {
            geocodeLocation(q, near ?? undefined, ctrl.signal)
                .then((r) => {
                    setResults(r);
                    setActive(0);
                    setOpen(true);
                })
                .catch(() => setResults([]))
                .finally(() => setLoading(false));
        }, 280);
        return () => {
            ctrl.abort();
            window.clearTimeout(t);
        };
        // near only biases the ranking; a pan must not refire the lookup.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [value]);

    const pick = (r: GeocodeResult) => {
        typed.current = false;
        onChange(r.detail ? `${r.label}, ${r.detail.split(", ")[0]}` : r.label);
        onPick(r);
        setOpen(false);
    };

    return (
        <div ref={box} className="relative">
            <div className="relative">
                <MapPinIcon className="absolute left-2 top-1/2 -translate-y-1/2 size-3.5 text-slate-400" />
                <input
                    value={value}
                    onChange={(e) => {
                        typed.current = true;
                        onChange(e.target.value);
                    }}
                    onFocus={() => results.length && setOpen(true)}
                    onKeyDown={(e) => {
                        if (!open || !results.length) return;
                        if (e.key === "ArrowDown") {
                            e.preventDefault();
                            setActive((a) => Math.min(a + 1, results.length - 1));
                        } else if (e.key === "ArrowUp") {
                            e.preventDefault();
                            setActive((a) => Math.max(a - 1, 0));
                        } else if (e.key === "Enter") {
                            e.preventDefault();
                            pick(results[active]);
                        }
                    }}
                    placeholder="City, neighborhood or address"
                    aria-label="Location"
                    className="h-8 w-full rounded-md border border-slate-200 bg-white pl-7 pr-7 text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                />
                {loading ? (
                    <Loader2Icon className="absolute right-2 top-1/2 -translate-y-1/2 size-3.5 animate-spin text-slate-400" />
                ) : value ? (
                    <button
                        type="button"
                        aria-label="Clear location"
                        onClick={() => {
                            onChange("");
                            setResults([]);
                        }}
                        className="absolute right-1.5 top-1/2 -translate-y-1/2 size-5 rounded text-slate-400 hover:text-slate-700 inline-flex items-center justify-center"
                    >
                        <XIcon className="size-3" />
                    </button>
                ) : null}
            </div>
            <AnimatePresence>
                {open && results.length > 0 && (
                    <motion.ul
                        data-floating
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.12 }}
                        className="absolute z-30 mt-1 w-full overflow-hidden rounded-md border border-slate-200 bg-white py-1 shadow-[0_12px_32px_-12px_rgba(15,23,42,0.3)]"
                        role="listbox"
                    >
                        {results.map((r, i) => (
                            <li key={`${r.label}-${r.center.lat}-${r.center.lng}`}>
                                <button
                                    type="button"
                                    role="option"
                                    aria-selected={i === active}
                                    onMouseEnter={() => setActive(i)}
                                    onClick={() => pick(r)}
                                    className={cn("w-full px-2.5 py-1.5 text-left flex items-start gap-2", i === active ? "bg-sky-50" : "hover:bg-slate-50")}
                                >
                                    <NavigationIcon className="mt-0.5 size-3 shrink-0 text-slate-400" />
                                    <span className="min-w-0">
                                        <span className="block truncate text-[12.5px] text-slate-900">{r.label}</span>
                                        {r.detail && <span className="block truncate text-[11px] text-slate-500">{r.detail}</span>}
                                    </span>
                                    {r.kind && <span className="ml-auto shrink-0 text-[10px] uppercase tracking-wide text-slate-400">{r.kind}</span>}
                                </button>
                            </li>
                        ))}
                    </motion.ul>
                )}
            </AnimatePresence>
        </div>
    );
}
