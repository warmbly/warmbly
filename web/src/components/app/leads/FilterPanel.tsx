import { RotateCcwIcon } from "lucide-react";
import { NumberInput } from "@/components/ui/field";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import type { LeadFacets, LeadStatus } from "@/lib/api/models/app/leads/Lead";
import { cn } from "@/lib/utils";
import { EMPTY_REFINE, refineCount, type ReachFilter, type Refine } from "./refine";

const REACH: { id: ReachFilter; label: string }[] = [
    { id: "email", label: "Has email" },
    { id: "people", label: "Has people" },
    { id: "website", label: "Has website" },
    { id: "phone", label: "Has phone" },
    { id: "socials", label: "Has socials" },
    { id: "no_website", label: "No website" },
];

const STATUSES: { id: LeadStatus; label: string }[] = [
    { id: "new", label: "New" },
    { id: "saved", label: "Saved" },
    { id: "contacted", label: "Contacted" },
    { id: "dismissed", label: "Dismissed" },
];

const RATINGS: { v: number | null; label: string }[] = [
    { v: null, label: "Any" },
    { v: 3, label: "3+" },
    { v: 4, label: "4+" },
    { v: 4.5, label: "4.5+" },
];

export default function FilterPanel({ value, onChange, facets }: { value: Refine; onChange: (r: Refine) => void; facets?: LeadFacets }) {
    const set = <K extends keyof Refine>(k: K, v: Refine[K]) => onChange({ ...value, [k]: v });
    const toggle = <T,>(list: T[], v: T) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
    const count = refineCount(value);
    return (
        <div className="space-y-3.5">
            <div className="flex items-center">
                <span className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Refine results</span>
                {count > 0 && (
                    <button
                        type="button"
                        onClick={() => onChange(EMPTY_REFINE)}
                        className="ml-auto inline-flex items-center gap-1 text-[11px] text-slate-500 hover:text-slate-900"
                    >
                        <RotateCcwIcon className="size-3" />
                        Reset
                    </button>
                )}
            </div>
            <Group label="Reachability">
                {REACH.map((r) => (
                    <Pill key={r.id} on={value.reach.includes(r.id)} onClick={() => {
                        let next = toggle(value.reach, r.id);
                        if (r.id === "no_website") next = next.filter((x) => x !== "website");
                        if (r.id === "website") next = next.filter((x) => x !== "no_website");
                        set("reach", next);
                    }}>
                        {r.label}
                    </Pill>
                ))}
            </Group>
            <Group label="Rating">
                <div className="inline-flex rounded-md border border-slate-200 p-0.5">
                    {RATINGS.map((r) => (
                        <button
                            key={r.label}
                            type="button"
                            onClick={() => set("minRating", r.v)}
                            className={cn(
                                "h-6 px-2 rounded text-[11.5px]",
                                value.minRating === r.v ? "bg-sky-50 text-sky-700 font-medium" : "text-slate-500 hover:text-slate-900",
                            )}
                        >
                            {r.label}
                        </button>
                    ))}
                </div>
            </Group>
            <div className="grid grid-cols-3 gap-2">
                <NumField label="Min reviews" value={value.minReviews} onChange={(v) => set("minReviews", v)} />
                <NumField label="Max reviews" value={value.maxReviews} onChange={(v) => set("maxReviews", v)} />
                <NumField label="Min score" value={value.minScore} onChange={(v) => set("minScore", Math.min(100, v))} />
            </div>
            <Group label="Status">
                {STATUSES.map((s) => (
                    <Pill key={s.id} on={value.statuses.includes(s.id)} onClick={() => set("statuses", toggle(value.statuses, s.id))}>
                        {s.label}
                    </Pill>
                ))}
            </Group>
            {facets && facets.categories.length > 1 && (
                <Group label="Category">
                    {facets.categories.slice(0, 12).map((c) => (
                        <Pill key={c.value} on={value.category === c.value} onClick={() => set("category", value.category === c.value ? "" : c.value)}>
                            {c.value}
                            <span className="text-slate-400">{c.count}</span>
                        </Pill>
                    ))}
                </Group>
            )}
            {facets && facets.tags.length > 0 && (
                <Group label="Tag">
                    {facets.tags.map((t) => (
                        <Pill key={t.value} on={value.tag === t.value} onClick={() => set("tag", value.tag === t.value ? "" : t.value)}>
                            {t.value}
                            <span className="text-slate-400">{t.count}</span>
                        </Pill>
                    ))}
                </Group>
            )}
            <div className="space-y-2">
                <Switch label="Only what's in the map view" value={value.inView} onChange={(v) => set("inView", v)} />
                <Switch label="Hide leads already in contacts" value={value.hideContacts} onChange={(v) => set("hideContacts", v)} />
                <Switch label="Hide chains and franchises" value={value.excludeChains} onChange={(v) => set("excludeChains", v)} />
                <Switch
                    label="Hide dismissed leads"
                    value={value.hideDismissed && value.statuses.length === 0}
                    disabled={value.statuses.length > 0}
                    onChange={(v) => set("hideDismissed", v)}
                />
            </div>
        </div>
    );
}

function Group({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div>
            <div className="mb-1.5 text-[11.5px] text-slate-500">{label}</div>
            <div className="flex flex-wrap gap-1">{children}</div>
        </div>
    );
}

function Pill({ on, onClick, children }: { on: boolean; onClick: () => void; children: React.ReactNode }) {
    return (
        <button
            type="button"
            aria-pressed={on}
            onClick={onClick}
            className={cn(
                "h-6 px-2 rounded-md border text-[11.5px] inline-flex items-center gap-1 transition-colors",
                on ? "border-sky-300 bg-sky-50 text-sky-700" : "border-slate-200 text-slate-600 hover:border-slate-300 hover:text-slate-900",
            )}
        >
            {children}
        </button>
    );
}

function NumField({ label, value, onChange }: { label: string; value: number; onChange: (v: number) => void }) {
    return (
        <label className="block">
            <span className="mb-1 block text-[11px] text-slate-500">{label}</span>
            <NumberInput value={value} onChange={(v) => onChange(Math.max(0, Math.round(v || 0)))} min={0} placeholder="Any" />
        </label>
    );
}

function Switch({ label, value, onChange, disabled }: { label: string; value: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
    return (
        <div className={cn("flex items-center justify-between gap-2", disabled && "opacity-50")}>
            <span className="text-[12px] text-slate-700">{label}</span>
            <Toggle value={value} onChange={onChange} disabled={disabled} ariaLabel={label} />
        </div>
    );
}
