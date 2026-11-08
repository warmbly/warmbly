// One segment: its definition up top, its live contact list below (the
// shared ContactsTable scoped by segment_ids, with include/exclude actions).

import React from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { ArrowLeftIcon, CheckIcon, ChevronDownIcon, CopyIcon, GlobeIcon, MegaphoneIcon, PencilIcon, Trash2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";

import ContactsTable from "@/components/app/contacts/ContactsTable";
import SegmentEditor from "@/components/app/segments/SegmentEditor";
import AddSegmentToCampaignDialog from "@/components/app/segments/AddSegmentToCampaignDialog";
import SplitByRegionDialog from "@/components/app/segments/SplitByRegionDialog";
import { EmptyBlock } from "@/components/layout/Page";
import { NoAccess } from "@/components/layout/NoAccess";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission, useWriteGuard } from "@/hooks/usePermission";
import { useDeleteSegment, useSegment, useSegmentFields, useSegmentOverrides, useSetSegmentMembers } from "@/lib/api/hooks/app/segments";
import type Segment from "@/lib/api/models/app/segments/Segment";
import {
    SEGMENT_OPERATORS,
    VALUELESS_OPERATORS,
    type SegmentCondition,
    type SegmentFieldSpec,
    type SegmentOverride,
} from "@/lib/api/models/app/segments/Segment";
import { cn } from "@/lib/utils";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { selectionOf } from "@/lib/api/models/app/contacts/ContactSelection";

export default function SegmentPage() {
    const canView = usePermission("VIEW_CONTACTS");
    if (!canView) return <NoAccess feature="segments" permissionLabel="View contacts" />;
    return <SegmentDetail />;
}

function SegmentDetail() {
    const { id } = useParams({ from: "/app/contacts/segments/$id" });
    const navigate = useNavigate();
    const confirm = useConfirm();
    const write = useWriteGuard("MANAGE_CONTACTS");
    // Enrolment writes campaign leads, so it takes the campaign permission.
    const campaigns = useWriteGuard("MANAGE_CAMPAIGNS");
    const segment = useSegment(id);
    const fields = useSegmentFields();
    const remove = useDeleteSegment();
    const [editorOpen, setEditorOpen] = React.useState(false);
    const [campaignOpen, setCampaignOpen] = React.useState(false);
    const [splitOpen, setSplitOpen] = React.useState(false);

    if (segment.isPending) {
        return (
            <div className="p-5 space-y-2">
                <div className="h-6 w-56 rounded bg-slate-100 animate-pulse" />
                <div className="h-4 w-80 rounded bg-slate-100 animate-pulse" />
            </div>
        );
    }
    if (segment.isError || !segment.data) {
        return (
            <div className="p-5">
                <Link to="/app/contacts/segments" className="text-[12px] text-slate-500 hover:text-slate-900 inline-flex items-center gap-1">
                    <ArrowLeftIcon className="w-3 h-3" /> Segments
                </Link>
                <EmptyBlock title="Segment not found" body="It may have been deleted by a teammate." />
            </div>
        );
    }
    const s = segment.data;

    function askDelete() {
        confirm.show(`Delete the segment "${s.name}"? Contacts themselves are kept.`, async () => {
            try {
                await remove.mutateAsync(s.id);
                toast.success("Segment deleted");
                navigate({ to: "/app/contacts/segments" });
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
    }

    return (
        <div className="flex flex-col min-h-full">
            <div className="px-5 pt-3 pb-3 border-b border-slate-200 bg-white">
                <Link to="/app/contacts/segments" className="text-[11px] text-slate-500 hover:text-slate-900 inline-flex items-center gap-1">
                    <ArrowLeftIcon className="w-3 h-3" /> Segments
                </Link>
                <div className="mt-1.5 flex flex-wrap items-start gap-3">
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2 min-w-0">
                            <span className="size-3 rounded-full shrink-0" style={{ backgroundColor: s.color }} />
                            <h1 className="text-[15px] font-semibold text-slate-900 truncate">{s.name}</h1>
                            <span className="font-mono text-[11px] text-slate-500 tabular-nums">
                                {s.contact_count.toLocaleString()} contact{s.contact_count === 1 ? "" : "s"}
                            </span>
                        </div>
                        {s.description && <p className="text-[12px] text-slate-500 mt-0.5">{s.description}</p>}
                        <ConditionSummary conditions={s.conditions} match={s.match} specs={fields.data ?? []} included={s.included_count} excluded={s.excluded_count} />
                        <SegmentIdChip id={s.id} />
                    </div>
                    <div className="flex items-center gap-1.5 shrink-0">
                        <button
                            type="button"
                            onClick={write.guard(() => setEditorOpen(true))}
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 bg-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                        >
                            <PencilIcon className="w-3 h-3" />
                            Edit
                        </button>
                        <button
                            type="button"
                            onClick={write.guard(() => setSplitOpen(true))}
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 bg-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                        >
                            <GlobeIcon className="w-3 h-3" />
                            <span className="hidden sm:inline">Split by region</span>
                            <span className="sm:hidden">Split</span>
                        </button>
                        <button
                            type="button"
                            onClick={campaigns.guard(() => setCampaignOpen(true))}
                            className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                        >
                            <MegaphoneIcon className="w-3 h-3" />
                            Add to campaign
                        </button>
                        <button
                            type="button"
                            onClick={write.guard(askDelete)}
                            aria-label="Delete segment"
                            className="size-7 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors"
                        >
                            <Trash2Icon className="w-3.5 h-3.5" />
                        </button>
                    </div>
                </div>
            </div>

            {(s.included_count > 0 || s.excluded_count > 0) && <OverridesPanel segment={s} />}

            <ContactsTable key={s.id} segment={{ id: s.id, name: s.name, color: s.color }} />

            <SegmentEditor open={editorOpen} onClose={() => setEditorOpen(false)} segment={s} />
            <AddSegmentToCampaignDialog open={campaignOpen} onClose={() => setCampaignOpen(false)} segment={s} />
            <SplitByRegionDialog open={splitOpen} onClose={() => setSplitOpen(false)} segment={s} />
        </div>
    );
}

// The segment's ID, click to copy: it is what the API takes (segments CRUD,
// members, contact search segment_ids), so integrators need it at hand.
function SegmentIdChip({ id }: { id: string }) {
    const [copied, setCopied] = React.useState(false);
    async function copy() {
        try {
            await navigator.clipboard.writeText(id);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
        } catch {
            toast.error("Could not copy");
        }
    }
    return (
        <>
            <button
                type="button"
                onClick={copy}
                aria-label={copied ? "Segment ID copied" : "Copy segment ID"}
                title="Copy segment ID"
                className="mt-1 inline-flex items-center gap-1.5 max-w-full font-mono text-[10.5px] text-slate-400 hover:text-slate-700 transition-colors"
            >
                <span className="uppercase tracking-[0.14em] font-sans font-medium text-[9.5px]">ID</span>
                <span className="truncate">{id}</span>
                {copied ? <CheckIcon className="w-3 h-3 text-emerald-600 shrink-0" /> : <CopyIcon className="w-3 h-3 shrink-0" />}
            </button>
            <span className="sr-only" role="status" aria-live="polite">
                {copied ? "Segment ID copied" : ""}
            </span>
        </>
    );
}

// Pinned contacts. Excluded ones never show in the member list, so this is
// the only place they can be seen and released.
function OverridesPanel({ segment }: { segment: Segment }) {
    const write = useWriteGuard("MANAGE_CONTACTS");
    const overrides = useSegmentOverrides(segment.id);
    const set = useSetSegmentMembers();
    const [open, setOpen] = React.useState(false);
    const [busyId, setBusyId] = React.useState<string | null>(null);

    async function clear(o: SegmentOverride) {
        setBusyId(o.contact_id);
        try {
            await set.mutateAsync({ id: segment.id, selection: selectionOf(o.contact_id), mode: "auto" });
            toast.success("Back to automatic");
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setBusyId(null);
        }
    }

    const list = overrides.data ?? [];
    // The API caps one listing, so a segment a big import pinned into shows a
    // slice of its overrides. Say so rather than implying this is all of them,
    // but only once the listing actually arrived: while it is pending `list`
    // is empty, and the notice would read "the newest 0 of 5,000".
    const pinned = segment.included_count + segment.excluded_count;
    const truncated = overrides.isSuccess && pinned > list.length;
    return (
        <div className="border-b border-slate-200 bg-slate-50/40">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                className="w-full h-9 px-5 flex items-center gap-2 text-left"
                aria-expanded={open}
            >
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Pinned contacts</span>
                <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                    {segment.included_count} in · {segment.excluded_count} out
                </span>
                <ChevronDownIcon className={cn("w-3.5 h-3.5 text-slate-400 ml-auto transition-transform", open && "rotate-180")} />
            </button>
            {open && (
                <ul className="divide-y divide-slate-100 border-t border-slate-200/60 max-h-72 overflow-y-auto bg-white">
                    {overrides.isPending && <li className="px-5 h-9 flex items-center text-[11.5px] text-slate-400">Loading…</li>}
                    {list.map((o) => {
                        const name = `${o.first_name} ${o.last_name}`.trim() || o.email;
                        return (
                            <li key={o.contact_id} className="px-5 h-9 flex items-center gap-2">
                                <span
                                    className={cn(
                                        "inline-flex items-center h-4 px-1 rounded text-[10px] font-medium shrink-0",
                                        o.mode === "include" ? "bg-emerald-50 text-emerald-700" : "bg-amber-50 text-amber-700",
                                    )}
                                >
                                    {o.mode === "include" ? "in" : "out"}
                                </span>
                                <span className="text-[12px] text-slate-900 truncate">{name}</span>
                                <span className="text-[11px] text-slate-400 truncate hidden sm:inline">{o.email}</span>
                                <button
                                    type="button"
                                    onClick={write.guard(() => clear(o))}
                                    disabled={busyId === o.contact_id}
                                    className="ml-auto h-6 px-2 rounded text-[11px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-50"
                                >
                                    {busyId === o.contact_id ? "…" : "Back to automatic"}
                                </button>
                            </li>
                        );
                    })}
                    {truncated && (
                        <li className="px-5 py-2 flex items-center text-[11.5px] text-slate-400 leading-snug">
                            Showing the newest {list.length.toLocaleString()} of {pinned.toLocaleString()}. Find any other
                            pinned contact in Contacts and use the Segments section of its drawer to release it: a
                            pinned-out contact never appears in the member list below.
                        </li>
                    )}
                </ul>
            )}
        </div>
    );
}

function describe(c: SegmentCondition, specs: SegmentFieldSpec[]): string {
    const spec = specs.find((f) => f.field === c.field);
    const label = spec?.label ?? c.field.replace(/^custom\./, "");
    const op = spec ? SEGMENT_OPERATORS[spec.kind].find((o) => o.id === c.operator)?.label : c.operator;
    if (VALUELESS_OPERATORS.has(c.operator)) return `${label} ${op ?? c.operator}`;
    if (c.values && c.values.length > 0) return `${label} ${op} ${c.values.length} value${c.values.length === 1 ? "" : "s"}`;
    if (c.operator === "within_days" || c.operator === "not_within_days") return `${label} ${op} ${c.value} days`;
    if (spec?.kind === "date" && c.value) return `${label} ${op} ${c.value.slice(0, 10)}`;
    return `${label} ${op} ${c.value ?? ""}`.trim();
}

function ConditionSummary({
    conditions,
    match,
    specs,
    included,
    excluded,
}: {
    conditions: SegmentCondition[];
    match: "all" | "any";
    specs: SegmentFieldSpec[];
    included: number;
    excluded: number;
}) {
    return (
        <div className="mt-2 flex flex-wrap items-center gap-1">
            {conditions.length === 0 ? (
                <span className="text-[11px] text-slate-400">No conditions: only contacts added by hand.</span>
            ) : (
                conditions.map((c, i) => (
                    <React.Fragment key={i}>
                        {i > 0 && <span className="text-[10px] uppercase tracking-[0.1em] text-slate-400">{match === "all" ? "and" : "or"}</span>}
                        <span className="inline-flex items-center h-5 px-1.5 rounded bg-slate-100 text-slate-700 text-[11px]">{describe(c, specs)}</span>
                    </React.Fragment>
                ))
            )}
            {included > 0 && (
                <span className="inline-flex items-center h-5 px-1.5 rounded bg-emerald-50 text-emerald-700 text-[11px]">+{included} added by hand</span>
            )}
            {excluded > 0 && (
                <span className="inline-flex items-center h-5 px-1.5 rounded bg-amber-50 text-amber-700 text-[11px]">{excluded} excluded</span>
            )}
        </div>
    );
}
