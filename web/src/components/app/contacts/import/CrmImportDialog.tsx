// Import from the connected CRM: pick a HubSpot list or a Pipedrive filter, see
// who comes in and who the workspace's CRM rules skip, then continue in the
// regular import review.

import React from "react";
import { Link } from "@tanstack/react-router";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    ArrowRightIcon,
    CheckIcon,
    ChevronLeftIcon,
    ListIcon,
    Loader2Icon,
    RefreshCwIcon,
    UsersIcon,
    XIcon,
} from "lucide-react";
import { SearchInput } from "@/components/ui/field";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { type CrmInfo, CrmMark } from "@/components/app/crm/crmProviders";
import { crmErrorMessage } from "@/components/app/crm/crmModeUtils";
import useCrmProvider from "@/hooks/useCrmProvider";
import useBrowseState from "@/hooks/useBrowseState";
import { browseText } from "@/lib/browse-contacts-campaigns";
import useBrowseDebouncedValue from "../filters/useBrowseDebouncedValue";
import { useCrmLists, useImportCrmList, usePreviewCrmImport } from "@/lib/api/hooks/app/crm/provider/useCrmLists";
import useCrmMetadata from "@/lib/api/hooks/app/crm/provider/useCrmMetadata";
import { getContactImport } from "@/lib/api/client/app/contacts/contactImports";
import type { CRMImportPreview, CRMList } from "@/lib/api/models/app/crm/CRMProvider";
import type { AppError } from "@/lib/api/client/normalizeError";
import { cn } from "@/lib/utils";
import type { ImportStep } from "../ImportWizard";
import { mappingProblem } from "../importShared";

type Step = "list" | "review";

function steps(crm: CrmInfo): { key: Step; label: string }[] {
    return [
        { key: "list", label: `Choose a ${crm.words.list}` },
        { key: "review", label: "Review" },
    ];
}

const paneVariants = {
    enter: (dir: 1 | -1) => ({ x: dir * 28, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: 1 | -1) => ({ x: dir * -28, opacity: 0 }),
};

const PAGE_SIZE = 25;
const MAX_LIST_IMPORT = 25000;

export default function CrmImportDialog({
    open,
    onClose,
    onContinue,
    target,
}: {
    open: boolean;
    onClose: () => void;
    // Hands the created draft to the regular import wizard, on the step it should open.
    onContinue: (importId: string, step: ImportStep) => void;
    // "Adding to <campaign>" when the import lands somewhere specific.
    target?: string;
}) {
    const { crm } = useCrmProvider();
    const [step, setStep] = React.useState<Step>("list");
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [list, setList] = React.useState<CRMList | null>(null);
    const [applyGuards, setApplyGuards] = React.useState(true);
    const [nudge, setNudge] = React.useState<string | null>(null);
    const importList = useImportCrmList();

    React.useEffect(() => {
        if (open) return;
        setStep("list");
        setDirection(1);
        setList(null);
        setApplyGuards(true);
        setNudge(null);
        importList.reset();
        // importList.reset is stable per mutation; resetting on close only.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [open]);

    function go(next: Step) {
        if (next === "review" && !list) {
            setNudge(`Pick a ${crm.words.list} to continue.`);
            return;
        }
        setNudge(null);
        setDirection(next === "review" ? 1 : -1);
        setStep(next);
    }

    React.useEffect(() => {
        if (!open) return;
        const onKey = (ev: KeyboardEvent) => {
            if (ev.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            ev.preventDefault();
            if (!importList.isPending) onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, onClose, importList.isPending]);

    async function runImport() {
        if (!list || importList.isPending) return;
        try {
            const res = await importList.mutateAsync({ list_id: list.external_id, apply_guards: applyGuards });
            // Open on the review when the generated columns map cleanly, else on mapping.
            let at: ImportStep = "review";
            try {
                const imp = await getContactImport(res.import_id);
                const mapping = imp.options?.mapping?.length ? imp.options.mapping : imp.preview?.suggested_mapping;
                if (!mapping || mappingProblem(mapping)) at = "map";
            } catch {
                at = "map";
            }
            onContinue(res.import_id, at);
        } catch {
            /* shown in the footer */
        }
    }

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={() => {
                        if (!importList.isPending) onClose();
                    }}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-2 sm:px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label={`Import from ${crm.name}`}
                        initial={{ y: 8, opacity: 0, scale: 0.985 }}
                        animate={{ y: 0, opacity: 1, scale: 1 }}
                        exit={{ y: 8, opacity: 0, scale: 0.985 }}
                        transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                        onMouseDown={(ev) => ev.stopPropagation()}
                        className="w-full max-w-[600px] h-[min(88dvh,640px)] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2 shrink-0">
                            <CrmMark provider={crm.id} className="w-4 h-4" />
                            <h2 className="text-[13px] font-semibold text-slate-900">Import from {crm.name}</h2>
                            {target && <span className="text-[11.5px] text-slate-400 truncate">{target}</span>}
                            <button
                                type="button"
                                onClick={onClose}
                                disabled={importList.isPending}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-50"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </header>

                        <Stepper step={step} canReview={!!list} onGo={go} />

                        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
                            <AnimatePresence mode="wait" initial={false} custom={direction}>
                                <motion.div
                                    key={step}
                                    custom={direction}
                                    variants={paneVariants}
                                    initial="enter"
                                    animate="center"
                                    exit="exit"
                                    transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                                    className="px-4 sm:px-5 py-4"
                                >
                                    {step === "list" ? (
                                        <ListStep
                                            selected={list}
                                            onSelect={(l) => {
                                                setList(l);
                                                setNudge(null);
                                            }}
                                            onPick={(l) => {
                                                setList(l);
                                                setNudge(null);
                                                setDirection(1);
                                                setStep("review");
                                            }}
                                        />
                                    ) : list ? (
                                        <ReviewStep list={list} applyGuards={applyGuards} setApplyGuards={setApplyGuards} />
                                    ) : null}
                                </motion.div>
                            </AnimatePresence>
                        </div>

                        <footer className="min-h-12 py-1.5 px-3 border-t border-slate-200 flex items-center gap-2 shrink-0 bg-slate-50/30">
                            {step === "review" ? (
                                <button
                                    type="button"
                                    onClick={() => go("list")}
                                    disabled={importList.isPending}
                                    className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                                >
                                    <ChevronLeftIcon className="w-3 h-3" />
                                    Back
                                </button>
                            ) : (
                                <span className="text-[11px] text-slate-400 pl-1 hidden sm:inline">
                                    Nothing is imported until you confirm on the import review.
                                </span>
                            )}
                            <div className="ml-auto flex items-center gap-2 min-w-0">
                                {(nudge || importList.isError) && (
                                    <span role="status" className="text-[11.5px] text-amber-700 inline-flex items-center gap-1 min-w-0">
                                        <AlertTriangleIcon className="w-3 h-3 shrink-0" />
                                        <span className="truncate" title={nudge ?? crmErrorMessage(importList.error)}>
                                            {nudge ?? crmErrorMessage(importList.error)}
                                        </span>
                                    </span>
                                )}
                                {step === "list" ? (
                                    <button
                                        type="button"
                                        onClick={() => go("review")}
                                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors shrink-0"
                                    >
                                        Continue
                                        <ArrowRightIcon className="w-3 h-3" />
                                    </button>
                                ) : (
                                    <button
                                        type="button"
                                        onClick={runImport}
                                        disabled={importList.isPending}
                                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:bg-slate-200 disabled:text-slate-500 shrink-0"
                                    >
                                        {importList.isPending ? (
                                            <Loader2Icon className="w-3 h-3 animate-spin" />
                                        ) : (
                                            <ArrowRightIcon className="w-3 h-3" />
                                        )}
                                        {importList.isPending ? `Reading the ${crm.words.list}…` : "Import"}
                                    </button>
                                )}
                            </div>
                        </footer>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

function Stepper({ step, canReview, onGo }: { step: Step; canReview: boolean; onGo: (s: Step) => void }) {
    const { crm } = useCrmProvider();
    const STEPS = steps(crm);
    const at = STEPS.findIndex((s) => s.key === step);
    return (
        <div className="px-4 sm:px-5 h-10 border-b border-slate-100 flex items-center shrink-0 bg-slate-50/40">
            {STEPS.map((s, i) => {
                const active = i === at;
                const done = i < at;
                const reachable = i <= at || canReview;
                return (
                    <React.Fragment key={s.key}>
                        <button
                            type="button"
                            onClick={() => onGo(s.key)}
                            disabled={!reachable}
                            aria-current={active ? "step" : undefined}
                            className={cn(
                                "inline-flex items-center gap-2 h-7 pl-1 pr-2 rounded-md shrink-0 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-100",
                                reachable && !active ? "hover:bg-slate-100" : "",
                                !reachable ? "cursor-default" : "",
                            )}
                        >
                            <span
                                className={cn(
                                    "size-5 rounded-full inline-flex items-center justify-center text-[10.5px] font-semibold tabular-nums transition-colors",
                                    done
                                        ? "bg-sky-600 text-white"
                                        : active
                                          ? "bg-white text-sky-700 ring-1 ring-inset ring-sky-600"
                                          : "bg-white text-slate-400 ring-1 ring-inset ring-slate-200",
                                )}
                            >
                                {done ? <CheckIcon className="w-3 h-3" strokeWidth={3} /> : i + 1}
                            </span>
                            <span
                                className={cn(
                                    "text-[11.5px] font-medium whitespace-nowrap",
                                    active ? "text-slate-900" : done ? "text-slate-600" : "text-slate-400",
                                )}
                            >
                                {s.label}
                            </span>
                        </button>
                        {i < STEPS.length - 1 && (
                            <span className="relative flex-1 h-px mx-2 bg-slate-200 min-w-3 overflow-hidden">
                                <motion.span
                                    initial={false}
                                    animate={{ scaleX: i < at ? 1 : 0 }}
                                    transition={{ duration: 0.25, ease: [0.22, 1, 0.36, 1] }}
                                    style={{ originX: 0 }}
                                    className="absolute inset-0 bg-sky-600"
                                />
                            </span>
                        )}
                    </React.Fragment>
                );
            })}
        </div>
    );
}

function ListStep({
    selected,
    onSelect,
    onPick,
}: {
    selected: CRMList | null;
    onSelect: (l: CRMList) => void;
    // Double click: choose and go straight to the review.
    onPick: (l: CRMList) => void;
}) {
    const { crm } = useCrmProvider();
    const browseName = `contacts:crm-import:${crm.id}:lists`;
    const [q, setQ] = useBrowseState(`${browseName}:query`, "", browseText);
    const query = useBrowseDebouncedValue(q.trim(), browseName, 300);
    // Each "Load more" adds the next page's cursor; every page is its own query.
    const [cursors, setCursors] = React.useState<(string | undefined)[]>([undefined]);
    React.useEffect(() => setCursors([undefined]), [query]);

    return (
        <div className="space-y-3">
            <div>
                <p className="text-[12.5px] text-slate-700">
                    Pick the {crm.name} {crm.words.list} to bring in.
                </p>
                <p className="text-[11.5px] text-slate-500 mt-0.5">
                    {crm.id === "pipedrive"
                        ? "A saved people filter brings in whoever matches it right now."
                        : "Active lists bring in whoever is on them right now. Static lists bring in their saved members."}
                </p>
            </div>
            <SearchInput value={q} onChange={setQ} placeholder={`Search ${crm.name} ${crm.words.lists}…`} autoFocus className="w-full" />
            <div className="space-y-1">
                {cursors.map((cursor, i) => (
                    <ListPage
                        key={`${query}:${cursor ?? "first"}`}
                        query={query}
                        cursor={cursor}
                        first={i === 0}
                        last={i === cursors.length - 1}
                        selected={selected}
                        onSelect={onSelect}
                        onPick={onPick}
                        onMore={(next) => setCursors((c) => [...c, next])}
                    />
                ))}
            </div>
        </div>
    );
}

function ListPage({
    query,
    cursor,
    first,
    last,
    selected,
    onSelect,
    onPick,
    onMore,
}: {
    query: string;
    cursor?: string;
    first: boolean;
    last: boolean;
    selected: CRMList | null;
    onSelect: (l: CRMList) => void;
    onPick: (l: CRMList) => void;
    onMore: (cursor: string) => void;
}) {
    const lists = useCrmLists({ q: query || undefined, cursor, limit: PAGE_SIZE });
    const { crm } = useCrmProvider();

    if (lists.isPending) {
        return (
            <div className="space-y-1">
                {[0, 1, 2].map((i) => (
                    <div key={i} className="h-12 rounded-md bg-slate-100 animate-pulse" />
                ))}
            </div>
        );
    }
    if (lists.isError) {
        const err = lists.error as unknown as AppError;
        const reauth = err?.code === "crm_reauth_required";
        return (
            <div className="rounded-md border border-amber-200 bg-amber-50/60 px-3 py-2.5 flex items-start gap-2">
                <AlertTriangleIcon className="w-3.5 h-3.5 text-amber-600 mt-px shrink-0" />
                <div className="min-w-0 flex-1 text-[11.5px] text-amber-900 leading-snug">
                    {crmErrorMessage(lists.error)}
                    {reauth && (
                        <Link to={crm.settingsPath} className="ml-1 font-medium underline hover:text-amber-950">
                            Open {crm.name} settings
                        </Link>
                    )}
                </div>
                {!reauth && (
                    <button
                        type="button"
                        onClick={() => void lists.refetch()}
                        className="shrink-0 h-6 px-2 rounded-md border border-amber-200 bg-white text-[11px] text-amber-800 inline-flex items-center gap-1 hover:bg-amber-50 transition-colors"
                    >
                        <RefreshCwIcon className="w-3 h-3" />
                        Retry
                    </button>
                )}
            </div>
        );
    }

    const data = lists.data.data;
    const next = lists.data.pagination.next_cursor;
    return (
        <>
            {first && data.length === 0 && (
                <div className="rounded-md border border-dashed border-slate-200 px-3 py-8 text-center">
                    <ListIcon className="w-4 h-4 text-slate-300 mx-auto mb-1.5" />
                    <p className="text-[11.5px] text-slate-500">
                        {query
                            ? `No ${crm.name} ${crm.words.list} matches that search.`
                            : crm.id === "pipedrive"
                              ? "This Pipedrive account has no saved people filters yet."
                              : "This HubSpot account has no contact lists yet."}
                    </p>
                </div>
            )}
            {data.map((l) => {
                const active = selected?.external_id === l.external_id;
                return (
                    <button
                        key={l.external_id}
                        type="button"
                        onClick={() => onSelect(l)}
                        onDoubleClick={() => onPick(l)}
                        aria-pressed={active}
                        className={cn(
                            "w-full rounded-md border px-3 py-2 flex items-center gap-2.5 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-100",
                            active ? "border-sky-400 bg-sky-50/60" : "border-slate-200 bg-white hover:border-slate-300",
                        )}
                    >
                        <span
                            className={cn(
                                "size-4 rounded-full border shrink-0 inline-flex items-center justify-center transition-colors",
                                active ? "border-sky-600 bg-sky-600 text-white" : "border-slate-300 bg-white",
                            )}
                        >
                            {active && <CheckIcon className="w-2.5 h-2.5" strokeWidth={3} />}
                        </span>
                        <span className="min-w-0 flex-1">
                            <span className="block text-[12.5px] font-medium text-slate-900 truncate">{l.name}</span>
                            <span className="mt-0.5 flex items-center gap-1.5 text-[11px] text-slate-500">
                                <UsersIcon className="w-3 h-3 text-slate-400" />
                                {listSize(l, crm)}
                            </span>
                        </span>
                        {crm.id !== "pipedrive" && (
                            <span
                                className={cn(
                                    "shrink-0 h-5 px-1.5 rounded text-[10.5px] font-medium inline-flex items-center",
                                    l.dynamic ? "bg-violet-50 text-violet-700" : "bg-slate-100 text-slate-600",
                                )}
                                title={l.dynamic ? `Membership updates on its own in ${crm.name}` : "Members are added by hand"}
                            >
                                {l.dynamic ? "Active" : "Static"}
                            </span>
                        )}
                    </button>
                );
            })}
            {last && next && (
                <button
                    type="button"
                    onClick={() => onMore(next)}
                    className="w-full h-8 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-50 transition-colors"
                >
                    Load more {crm.words.lists}
                </button>
            )}
        </>
    );
}

function ReviewStep({
    list,
    applyGuards,
    setApplyGuards,
}: {
    list: CRMList;
    applyGuards: boolean;
    setApplyGuards: (v: boolean) => void;
}) {
    const { settings, crm } = useCrmProvider();
    const metadata = useCrmMetadata();
    const preview = usePreviewCrmImport();
    // Both answers are kept, so flipping the rules back and forth asks the CRM once each.
    const [results, setResults] = React.useState<Record<string, CRMImportPreview>>({});
    const key = `${list.external_id}:${applyGuards ? 1 : 0}`;
    const result = results[key];
    const { mutate, reset } = preview;

    React.useEffect(() => {
        if (results[key]) return;
        reset();
        mutate(
            { list_id: list.external_id, apply_guards: applyGuards },
            { onSuccess: (p) => setResults((r) => ({ ...r, [key]: p })) },
        );
        // results is read only to skip a repeat; key covers list and rules.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [key, mutate, reset]);

    const rules = guardRules(settings?.config?.guards, metadata.data?.lifecycle_stages, crm);
    const skippedTotal = result?.skipped.reduce((n, s) => n + s.count, 0) ?? 0;
    const loading = !result && !preview.isError;

    return (
        <div className="space-y-4">
            <div className="flex items-start gap-2.5">
                <span className={cn("size-8 rounded-md inline-flex items-center justify-center shrink-0", crm.tint)}>
                    <CrmMark provider={crm.id} className="w-4 h-4" />
                </span>
                <div className="min-w-0">
                    <p className="text-[13px] font-semibold text-slate-900 truncate">{result?.list_name || list.name}</p>
                    <p className="text-[11.5px] text-slate-500">
                        {crm.id === "pipedrive"
                            ? `Saved people filter${result ? ` · ${result.total.toLocaleString()} ${result.total === 1 ? "person" : "people"}` : ""}`
                            : `${listSize(list, crm)} · ${list.dynamic ? "Active list" : "Static list"}`}
                    </p>
                </div>
            </div>

            <div className="rounded-md border border-slate-200 bg-white px-3 py-2.5 flex items-start gap-3">
                <div className="min-w-0 flex-1">
                    <div className="text-[12.5px] font-medium text-slate-900">Apply {crm.name} rules</div>
                    <div className="text-[11px] text-slate-500 leading-snug mt-0.5">
                        {rules.length > 0
                            ? `Skips ${joinList(rules)}, as set in ${crm.name} settings.`
                            : `No skip rules are set in ${crm.name} settings, so only ${crm.words.contacts} without an email are left out.`}
                    </div>
                </div>
                <Toggle value={applyGuards} onChange={setApplyGuards} ariaLabel={`Apply ${crm.name} rules`} />
            </div>

            {preview.isError && !result ? (
                <div className="rounded-md border border-amber-200 bg-amber-50/60 px-3 py-2.5 flex items-start gap-2">
                    <AlertTriangleIcon className="w-3.5 h-3.5 text-amber-600 mt-px shrink-0" />
                    <div className="min-w-0 flex-1 text-[11.5px] text-amber-900 leading-snug">
                        {crmErrorMessage(preview.error)}
                        {(preview.error as unknown as AppError)?.code === "crm_reauth_required" && (
                            <Link to={crm.settingsPath} className="ml-1 font-medium underline hover:text-amber-950">
                                Open {crm.name} settings
                            </Link>
                        )}
                    </div>
                    <button
                        type="button"
                        onClick={() =>
                            mutate(
                                { list_id: list.external_id, apply_guards: applyGuards },
                                { onSuccess: (p) => setResults((r) => ({ ...r, [key]: p })) },
                            )
                        }
                        className="shrink-0 h-6 px-2 rounded-md border border-amber-200 bg-white text-[11px] text-amber-800 inline-flex items-center gap-1 hover:bg-amber-50 transition-colors"
                    >
                        <RefreshCwIcon className="w-3 h-3" />
                        Retry
                    </button>
                </div>
            ) : loading ? (
                <div className="rounded-md border border-slate-200 px-3 py-6 flex items-center justify-center gap-2 text-[12px] text-slate-500">
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400" />
                    Checking the {crm.words.list} against {crm.name}…
                </div>
            ) : result ? (
                <div className="space-y-3">
                    <div className="grid grid-cols-2 gap-1.5">
                        <Tile label="Will be imported" value={result.included} tone="sky" />
                        <Tile label="Skipped" value={skippedTotal} tone={skippedTotal ? "amber" : "slate"} />
                    </div>

                    {result.skipped.length > 0 && (
                        <div>
                            <p className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-1.5">
                                Why {skippedTotal.toLocaleString()} {skippedTotal === 1 ? "is" : "are"} skipped
                            </p>
                            <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
                                {result.skipped.map((s) => (
                                    <div
                                        key={s.reason}
                                        className="flex items-center gap-2 px-3 py-1.5 border-b last:border-b-0 border-slate-100"
                                    >
                                        <span className="text-[12px] text-slate-700 flex-1 min-w-0 truncate">{s.label}</span>
                                        <span className="relative w-16 h-1 rounded-full bg-slate-100 overflow-hidden shrink-0">
                                            <span
                                                className="absolute inset-y-0 left-0 bg-amber-400/80"
                                                style={{ width: `${Math.max(4, Math.round((s.count / Math.max(1, result.total)) * 100))}%` }}
                                            />
                                        </span>
                                        <span className="w-12 text-right text-[12px] tabular-nums text-slate-900 shrink-0">
                                            {s.count.toLocaleString()}
                                        </span>
                                    </div>
                                ))}
                            </div>
                        </div>
                    )}

                    {result.sample.length > 0 && (
                        <div>
                            <p className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-1.5">
                                First in
                            </p>
                            <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
                                {result.sample.map((p) => {
                                    const name = `${p.first_name ?? ""} ${p.last_name ?? ""}`.trim();
                                    return (
                                        <div
                                            key={p.email}
                                            className="flex items-center gap-2 px-3 py-1.5 border-b last:border-b-0 border-slate-100 min-w-0"
                                        >
                                            <span className="text-[12px] text-slate-900 truncate min-w-0">{name || p.email}</span>
                                            {name && <span className="text-[11px] text-slate-400 truncate min-w-0">{p.email}</span>}
                                            {p.company && (
                                                <span className="ml-auto text-[11px] text-slate-500 truncate shrink-0 max-w-[40%]">
                                                    {p.company}
                                                </span>
                                            )}
                                        </div>
                                    );
                                })}
                            </div>
                        </div>
                    )}

                    {result.truncated && (
                        <p className="flex items-start gap-1.5 text-[11px] text-slate-500 leading-snug">
                            <AlertTriangleIcon className="w-3 h-3 text-amber-500 mt-px shrink-0" />
                            This {crm.words.list} is larger than one import takes, so the first {MAX_LIST_IMPORT.toLocaleString()} members are
                            read. Import again later for the rest.
                        </p>
                    )}

                    {result.included === 0 ? (
                        <p className="text-[11.5px] text-amber-700">
                            Nobody in this {crm.words.list} can be imported
                            {applyGuards && skippedTotal > 0 ? ` with the ${crm.name} rules on` : ""}.
                        </p>
                    ) : (
                        <p className="text-[11.5px] text-slate-500 leading-snug">
                            Next you choose campaigns, labels and what happens to contacts you already have, in the regular
                            import review.
                        </p>
                    )}
                </div>
            ) : null}
        </div>
    );
}

function Tile({ label, value, tone }: { label: string; value: number; tone: "sky" | "amber" | "slate" }) {
    const cls = { sky: "text-sky-700", amber: "text-amber-700", slate: "text-slate-900" }[tone];
    return (
        <div className="rounded-md border border-slate-200 bg-white px-3 py-2">
            <div className="text-[10px] uppercase tracking-[0.12em] text-slate-500 font-medium">{label}</div>
            <div className={cn("mt-1 text-[18px] font-semibold tabular-nums leading-none", cls)}>{value.toLocaleString()}</div>
        </div>
    );
}

function guardRules(
    g: { skip_lifecycle_stages: string[]; skip_open_deals: boolean; skip_other_owners: boolean; skip_opted_out: boolean } | undefined,
    stages: { value: string; label: string }[] | undefined,
    crm: CrmInfo,
): string[] {
    if (!g) return [];
    const out: string[] = [];
    const who = crm.words.contacts;
    if (g.skip_lifecycle_stages?.length) {
        const names = g.skip_lifecycle_stages.map((v) => (stages?.find((s) => s.value === v)?.label ?? v).toLowerCase());
        out.push(crm.id === "pipedrive" ? `${who} labeled ${joinList(names)}` : `${joinList(names)} ${who}`);
    }
    if (g.skip_open_deals) out.push(`${who} with an open deal`);
    if (g.skip_other_owners) out.push(`${who} owned by someone outside this workspace`);
    if (g.skip_opted_out) out.push(`${who} who opted out of email`);
    return out;
}

// "1,204 contacts", or nothing to count for a Pipedrive filter.
function listSize(l: CRMList, crm: CrmInfo): string {
    if (l.size < 0) return "Saved people filter";
    const noun = crm.id === "pipedrive" ? (l.size === 1 ? "person" : "people") : l.size === 1 ? "contact" : "contacts";
    return `${l.size.toLocaleString()} ${noun}`;
}

function joinList(items: string[]): string {
    if (items.length <= 1) return items[0] ?? "";
    if (items.length === 2) return `${items[0]} and ${items[1]}`;
    return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}
