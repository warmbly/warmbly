// Multi-select pickers used by the segment condition builder and the contact
// import: campaigns, segments and enum options. Same chip box + dropdown
// language as CategoryPicker; segments can be created inline where offered.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, Loader2Icon, PlusIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import { useCreateSegment, useSegments } from "@/lib/api/hooks/app/segments";
import { cn } from "@/lib/utils";

export interface PickOption {
    id: string;
    label: string;
    color?: string;
}

export function MultiPicker({
    value,
    onChange,
    options,
    placeholder = "Pick…",
    searchable = true,
    className,
    onCreate,
    suggestedName,
    createNoun = "item",
}: {
    value: string[];
    onChange: (next: string[]) => void;
    options: PickOption[];
    placeholder?: string;
    searchable?: boolean;
    className?: string;
    /** Creates an option from a name and resolves to its id; enables "Create". */
    onCreate?: (name: string) => Promise<string>;
    /** Offered as a one-click create when nothing is typed. */
    suggestedName?: string;
    createNoun?: string;
}) {
    const [creating, setCreating] = React.useState(false);
    // Names of options created here, shown until the list refetches them.
    const [created, setCreated] = React.useState<Record<string, string>>({});
    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLDivElement>(null);
    useClickOutside(open, () => setOpen(false), ref);
    const placement = useFlipPlacement(triggerRef, open, 270);

    const byId = React.useMemo(() => new Map(options.map((o) => [o.id, o])), [options]);
    const chips = value.map((id) => byId.get(id) ?? { id, label: created[id] ?? "Unknown" });
    const filtered = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return options;
        return options.filter((o) => o.label.toLowerCase().includes(q));
    }, [options, query]);

    function toggle(id: string) {
        onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id]);
    }

    const typed = query.trim();
    const createName = typed || suggestedName?.trim() || "";
    const nameTaken = options.some((o) => o.label.toLowerCase() === createName.toLowerCase());
    const canCreate = !!onCreate && createName !== "" && !nameTaken;

    async function create() {
        if (!onCreate || !canCreate || creating) return;
        setCreating(true);
        try {
            const id = await onCreate(createName);
            setCreated((c) => ({ ...c, [id]: createName }));
            onChange([...value, id]);
            setQuery("");
            setOpen(false);
        } catch (err) {
            const msg = (err as { message?: unknown } | null)?.message;
            toast.error(typeof msg === "string" && msg ? msg : `The ${createNoun} could not be created.`);
        } finally {
            setCreating(false);
        }
    }

    return (
        <div ref={ref} className={cn("relative", className)}>
            <div ref={triggerRef} className="rounded-md border border-slate-200 bg-white min-h-[28px]">
                {chips.length === 0 ? (
                    <button
                        type="button"
                        onClick={() => setOpen((o) => !o)}
                        className="w-full text-left px-2.5 h-7 text-[12px] text-slate-400 hover:text-slate-600"
                    >
                        {placeholder}
                    </button>
                ) : (
                    <div className="px-1.5 py-1 flex flex-wrap gap-1">
                        {chips.map((c) => (
                            <span
                                key={c.id}
                                className="inline-flex items-center gap-1 h-5 pl-1.5 pr-1 rounded text-[11px] font-medium bg-slate-100 text-slate-700"
                            >
                                {c.color && <span className="size-2 rounded-full shrink-0" style={{ backgroundColor: c.color }} />}
                                <span className="truncate max-w-[140px]">{c.label}</span>
                                <button
                                    type="button"
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        toggle(c.id);
                                    }}
                                    className="opacity-70 hover:opacity-100"
                                    aria-label={`Remove ${c.label}`}
                                >
                                    <XIcon className="w-2.5 h-2.5" />
                                </button>
                            </span>
                        ))}
                        <button
                            type="button"
                            onClick={() => setOpen((o) => !o)}
                            className="inline-flex items-center gap-1 h-5 px-1.5 rounded text-[11px] font-medium border border-dashed border-slate-300 text-slate-500 hover:border-slate-400 hover:text-slate-700"
                        >
                            <PlusIcon className="w-2.5 h-2.5" />
                            Add
                        </button>
                    </div>
                )}
            </div>
            <AnimatePresence>
                {open && (
                    <motion.div
                        data-floating
                        initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        className={cn(
                            "absolute left-0 right-0 z-30 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden",
                            placement === "top" ? "bottom-full mb-1" : "top-full mt-1",
                        )}
                    >
                        {searchable && (
                            <div className="px-2 py-1.5 border-b border-slate-200">
                                <input
                                    value={query}
                                    onChange={(e) => setQuery(e.target.value)}
                                    onKeyDown={(e) => {
                                        if (e.key === "Enter" && typed && filtered.length === 0) {
                                            e.preventDefault();
                                            void create();
                                        }
                                    }}
                                    placeholder={onCreate ? `Search or name a new ${createNoun}…` : "Search…"}
                                    autoFocus
                                    className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                                />
                            </div>
                        )}
                        <div className="max-h-56 overflow-y-auto py-1">
                            {canCreate && (
                                <button
                                    type="button"
                                    onClick={() => void create()}
                                    disabled={creating}
                                    className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-sky-700 hover:bg-sky-50 transition-colors disabled:opacity-60"
                                >
                                    {creating ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <PlusIcon className="w-3.5 h-3.5" />}
                                    <span className="truncate">
                                        New {createNoun}: <span className="font-medium">{createName}</span>
                                    </span>
                                </button>
                            )}
                            {filtered.length === 0 && !canCreate && (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">Nothing to pick.</div>
                            )}
                            {filtered.map((o) => {
                                const checked = value.includes(o.id);
                                return (
                                    <button
                                        key={o.id}
                                        type="button"
                                        onClick={() => toggle(o.id)}
                                        className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                                    >
                                        <span
                                            className={cn(
                                                "size-3.5 rounded border flex items-center justify-center transition-colors shrink-0",
                                                checked ? "border-slate-900 bg-slate-900" : "border-slate-300 bg-white",
                                            )}
                                        >
                                            {checked && <CheckIcon className="w-2 h-2 text-white" />}
                                        </span>
                                        {o.color && <span className="size-2.5 rounded-full shrink-0" style={{ backgroundColor: o.color }} />}
                                        <span className="truncate">{o.label}</span>
                                    </button>
                                );
                            })}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

export function CampaignMultiPicker({ value, onChange }: { value: string[]; onChange: (next: string[]) => void }) {
    const campaigns = useCampaigns({ query: "", folder: "", limit: 100 });
    const options = React.useMemo<PickOption[]>(
        () => campaigns.campaigns.map((c) => ({ id: c.id, label: c.name })),
        [campaigns.campaigns],
    );
    // Walk every page once so the picker holds the whole workspace.
    const { hasNextPage, isFetchingNextPage, fetchNextPage } = campaigns;
    React.useEffect(() => {
        if (hasNextPage && !isFetchingNextPage) void fetchNextPage();
    }, [hasNextPage, isFetchingNextPage, fetchNextPage]);
    return <MultiPicker value={value} onChange={onChange} options={options} placeholder="Pick campaigns…" />;
}

export function SegmentMultiPicker({
    value,
    onChange,
    exclude,
    allowCreate = false,
    suggestedName,
    placeholder = "Pick segments…",
}: {
    value: string[];
    onChange: (next: string[]) => void;
    exclude?: string;
    /** Offer to create a segment with no conditions: a list of exactly who is pinned into it. */
    allowCreate?: boolean;
    suggestedName?: string;
    placeholder?: string;
}) {
    const segments = useSegments();
    const createSegment = useCreateSegment();
    const options = React.useMemo<PickOption[]>(
        () =>
            (segments.data ?? [])
                .filter((s) => s.id !== exclude)
                .map((s) => ({ id: s.id, label: s.name, color: s.color })),
        [segments.data, exclude],
    );
    return (
        <MultiPicker
            value={value}
            onChange={onChange}
            options={options}
            placeholder={placeholder}
            onCreate={allowCreate ? async (name) => (await createSegment.mutateAsync({ name, match: "all", conditions: [] })).id : undefined}
            suggestedName={suggestedName}
            createNoun="segment"
        />
    );
}

const ENUM_LABELS: Record<string, string> = {
    unknown: "Unknown",
    manual: "Added manually",
    campaign: "Added from a campaign",
    import: "Imported",
    sheet_sync: "Google Sheets sync",
    crm_sync: "CRM sync",
    api: "API",
    ai_assistant: "Remie",
    form: "Form submission",
    automation: "Automation",
    apollo: "Apollo",
    clay: "Clay",
    lead_finder: "Lead finder",
    valid: "Valid",
    risky: "Risky",
    invalid: "Invalid",
    gmail: "Gmail",
    outlook: "Outlook",
    other: "Other",
};

export function EnumMultiPicker({
    value,
    onChange,
    options,
    labels,
}: {
    value: string[];
    onChange: (next: string[]) => void;
    options: string[];
    labels?: Record<string, string>;
}) {
    const opts = React.useMemo<PickOption[]>(() => options.map((o) => ({ id: o, label: labels?.[o] ?? ENUM_LABELS[o] ?? o })), [options, labels]);
    return <MultiPicker value={value} onChange={onChange} options={opts} placeholder="Pick values…" searchable={options.length > 8} />;
}
