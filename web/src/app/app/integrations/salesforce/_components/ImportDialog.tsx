// New Salesforce import: pick a list view or a Salesforce Campaign, preview what
// comes across, then choose where it lands. Also the edit dialog for a saved
// import source.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertCircleIcon,
    CheckIcon,
    ChevronLeftIcon,
    ChevronRightIcon,
    ContactIcon,
    DownloadCloudIcon,
    Loader2Icon,
    MegaphoneIcon,
    RefreshCwIcon,
    UserIcon,
    XIcon,
    type LucideIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";

import { Label, TextInput } from "@/components/ui/field";
import CampaignPicker from "@/components/app/campaigns/CampaignPicker";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import CategoryPicker from "@/components/app/contacts/CategoryPicker";
import { SegmentMultiPicker } from "@/components/app/segments/SegmentPickers";
import { useConfirm } from "@/hooks/context/confirm";
import {
    useCreateSalesforceImportSource,
    useSalesforceCampaigns,
    useSalesforceImportPreview,
    useSalesforceListViews,
    useUpdateSalesforceImportSource,
} from "@/lib/api/hooks/app/integrations/useSalesforce";
import type {
    SalesforceImportObject,
    SalesforceImportPreview,
    SalesforceImportSource,
    SalesforceSourceKind,
} from "@/lib/api/models/app/integrations/Salesforce";
import { cn } from "@/lib/utils";

import ProviderGlyph from "../../_components/ProviderGlyph";
import { Pill, SearchSelect, primaryBtn } from "./shared";
import { errMsg, sameIds } from "./util";

type Kind = "lead_view" | "contact_view" | "campaign";

const KINDS: { id: Kind; label: string; hint: string; icon: LucideIcon }[] = [
    { id: "lead_view", label: "Lead list view", hint: "Any Lead list view your user can see", icon: UserIcon },
    { id: "contact_view", label: "Contact list view", hint: "Any Contact list view your user can see", icon: ContactIcon },
    { id: "campaign", label: "Salesforce Campaign", hint: "Its Leads and Contacts, as campaign members", icon: MegaphoneIcon },
];

function kindParts(k: Kind): { source_kind: SalesforceSourceKind; object: SalesforceImportObject } {
    if (k === "campaign") return { source_kind: "campaign", object: "CampaignMember" };
    return { source_kind: "list_view", object: k === "lead_view" ? "Lead" : "Contact" };
}

const STEPS = [
    { key: "source", label: "Source" },
    { key: "preview", label: "Preview" },
    { key: "options", label: "Options" },
] as const;

const paneVariants = {
    enter: (dir: 1 | -1) => ({ x: dir * 28, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: 1 | -1) => ({ x: dir * -28, opacity: 0 }),
};

export default function ImportDialog({ connectionId, onClose }: { connectionId: string; onClose: () => void }) {
    const confirm = useConfirm();
    const [step, setStep] = React.useState(0);
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [nudge, setNudge] = React.useState<string | null>(null);

    const [kind, setKind] = React.useState<Kind>("lead_view");
    const [sourceId, setSourceId] = React.useState("");
    const [sourceLabel, setSourceLabel] = React.useState("");
    const [preview, setPreview] = React.useState<SalesforceImportPreview | null>(null);
    const [previewFor, setPreviewFor] = React.useState("");
    const [name, setName] = React.useState("");
    const [campaignId, setCampaignId] = React.useState<string | null>(null);
    const [campaignName, setCampaignName] = React.useState("");
    const [categoryIds, setCategoryIds] = React.useState<string[]>([]);
    const [segmentIds, setSegmentIds] = React.useState<string[]>([]);
    const [segmentBusy, setSegmentBusy] = React.useState(false);
    const [recurring, setRecurring] = React.useState(true);

    const previewM = useSalesforceImportPreview();
    const create = useCreateSalesforceImportSource(connectionId);

    const selectionKey = `${kind}|${sourceId}`;
    const dirty = !!sourceId;

    const requestClose = React.useCallback(() => {
        if (create.isPending) return;
        if (dirty) confirm.show("Discard this import? Nothing has been imported yet.", async () => onClose());
        else onClose();
    }, [dirty, confirm, onClose, create.isPending]);

    React.useEffect(() => {
        const onKey = (ev: KeyboardEvent) => {
            if (ev.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            ev.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [requestClose]);

    // The selection the newest preview request was for; an older answer is dropped.
    const latestPreview = React.useRef("");
    async function loadPreview() {
        const parts = kindParts(kind);
        const key = selectionKey;
        latestPreview.current = key;
        setPreviewFor(key);
        setPreview(null);
        try {
            const p = await previewM.mutateAsync({ connectionId, ...parts, source_id: sourceId });
            if (latestPreview.current === key) setPreview(p);
        } catch {
            // Rendered in the step from previewM.error.
        }
    }

    function issueFor(s: number): string | null {
        if (s === 0 && !sourceId) return kind === "campaign" ? "Pick a Salesforce Campaign first" : "Pick a list view first";
        if (s === 1) {
            if (previewM.isPending) return "Wait for the preview to load";
            if (!preview || previewFor !== selectionKey) return "The preview did not load. Retry it before continuing";
        }
        return null;
    }

    function goTo(target: number) {
        if (target > step) {
            for (let s = step; s < target; s++) {
                const issue = issueFor(s);
                if (issue) {
                    setNudge(issue);
                    return;
                }
            }
        }
        setNudge(null);
        setDirection(target > step ? 1 : -1);
        setStep(target);
        if (target === 1 && previewFor !== selectionKey) void loadPreview();
        if (target === 2 && !name.trim()) setName(sourceLabel);
    }

    async function submit() {
        const parts = kindParts(kind);
        try {
            await create.mutateAsync({
                connectionId,
                body: {
                    name: name.trim() || sourceLabel,
                    ...parts,
                    source_id: sourceId,
                    source_label: sourceLabel,
                    campaign_id: campaignId ?? undefined,
                    category_ids: categoryIds,
                    segment_ids: segmentIds,
                    recurring,
                },
            });
            toast.success("Import started");
            onClose();
        } catch (err) {
            toast.error(errMsg(err, "Could not create the import"));
        }
    }

    const last = STEPS.length - 1;

    return (
        <DialogShell title="New import" onRequestClose={requestClose} wide>
            <div className="px-4 h-10 border-b border-slate-200 flex items-center gap-1 shrink-0">
                {STEPS.map((s, i) => (
                    <React.Fragment key={s.key}>
                        {i > 0 && <ChevronRightIcon className="w-3 h-3 text-slate-300" />}
                        <button
                            type="button"
                            onClick={() => goTo(i)}
                            className={cn(
                                "h-7 px-2 rounded-md text-[12px] inline-flex items-center gap-1.5 transition-colors",
                                i === step ? "text-slate-900 font-medium bg-slate-100" : "text-slate-500 hover:text-slate-800",
                            )}
                        >
                            <span
                                className={cn(
                                    "size-4 rounded-full text-[10px] inline-flex items-center justify-center",
                                    i < step ? "bg-sky-600 text-white" : i === step ? "bg-slate-900 text-white" : "bg-slate-200 text-slate-500",
                                )}
                            >
                                {i < step ? <CheckIcon className="w-2.5 h-2.5" /> : i + 1}
                            </span>
                            {s.label}
                        </button>
                    </React.Fragment>
                ))}
            </div>

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
                        className="px-5 py-5"
                    >
                        {step === 0 && (
                            <SourceStep
                                connectionId={connectionId}
                                kind={kind}
                                setKind={(k) => {
                                    setKind(k);
                                    setSourceId("");
                                    setSourceLabel("");
                                    setNudge(null);
                                }}
                                sourceId={sourceId}
                                sourceLabel={sourceLabel}
                                onPick={(id, label) => {
                                    setSourceId(id);
                                    setSourceLabel(label);
                                    setNudge(null);
                                }}
                            />
                        )}
                        {step === 1 && (
                            <PreviewStep
                                loading={previewM.isPending}
                                error={previewM.isError ? errMsg(previewM.error, "Could not read the records") : null}
                                preview={previewFor === selectionKey ? preview : null}
                                sourceLabel={sourceLabel}
                                onRetry={() => void loadPreview()}
                            />
                        )}
                        {step === 2 && (
                            <div className="space-y-4 max-w-xl">
                                <div>
                                    <Label>Name</Label>
                                    <TextInput value={name} onChange={setName} placeholder={sourceLabel} maxLength={200} className="w-full" />
                                </div>
                                <div>
                                    <Label>Add to a Warmbly campaign (optional)</Label>
                                    <CampaignPicker
                                        campaignId={campaignId}
                                        campaignName={campaignName}
                                        onChange={(id, n) => {
                                            setCampaignId(id);
                                            setCampaignName(n);
                                        }}
                                        noneLabel="Only add to contacts"
                                    />
                                </div>
                                <div>
                                    <Label>Add to segments (optional)</Label>
                                    <SegmentMultiPicker
                                        onBusyChange={setSegmentBusy}
                                        value={segmentIds}
                                        onChange={setSegmentIds}
                                        allowCreate
                                        suggestedName={name.trim() || sourceLabel}
                                        placeholder="Pick or create a segment…"
                                    />
                                </div>
                                <div>
                                    <Label>Labels (optional)</Label>
                                    <CategoryPicker value={categoryIds} onChange={setCategoryIds} />
                                </div>
                                <div className="rounded-md border border-slate-200 px-3 py-2.5 flex items-start justify-between gap-4">
                                    <div>
                                        <div className="text-[12.5px] text-slate-900 font-medium">Keep in sync</div>
                                        <p className="text-[11px] text-slate-500 mt-0.5 leading-relaxed">
                                            Checks the {kind === "campaign" ? "campaign" : "list view"} every 30 minutes and
                                            brings in new people. Off imports once.
                                        </p>
                                    </div>
                                    <Toggle value={recurring} onChange={setRecurring} ariaLabel="Keep in sync" />
                                </div>
                            </div>
                        )}
                    </motion.div>
                </AnimatePresence>
            </div>

            <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5 shrink-0 bg-slate-50/30">
                {step > 0 && (
                    <button
                        type="button"
                        onClick={() => goTo(step - 1)}
                        disabled={create.isPending}
                        className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                    >
                        <ChevronLeftIcon className="w-3 h-3" />
                        Back
                    </button>
                )}
                <div className="ml-auto flex items-center gap-2 min-w-0">
                    <AnimatePresence initial={false}>
                        {nudge && (
                            <motion.span
                                key={nudge}
                                initial={{ opacity: 0, x: 6 }}
                                animate={{ opacity: 1, x: 0 }}
                                exit={{ opacity: 0, x: 6 }}
                                transition={{ duration: 0.14 }}
                                role="status"
                                className="text-[11.5px] text-amber-700 inline-flex items-center gap-1 min-w-0"
                            >
                                <AlertCircleIcon className="w-3 h-3 shrink-0" />
                                <span className="truncate">{nudge}</span>
                            </motion.span>
                        )}
                    </AnimatePresence>
                    {step < last ? (
                        <button type="button" onClick={() => goTo(step + 1)} className={cn(primaryBtn, "shrink-0")}>
                            Continue
                            <ChevronRightIcon className="w-3 h-3" />
                        </button>
                    ) : (
                        <button type="button" onClick={() => void submit()} disabled={create.isPending || segmentBusy} className={cn(primaryBtn, "shrink-0")}>
                            {create.isPending ? (
                                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                            ) : (
                                <DownloadCloudIcon className="w-3.5 h-3.5" />
                            )}
                            Create and import
                        </button>
                    )}
                </div>
            </div>
        </DialogShell>
    );
}

function SourceStep({
    connectionId,
    kind,
    setKind,
    sourceId,
    sourceLabel,
    onPick,
}: {
    connectionId: string;
    kind: Kind;
    setKind: (k: Kind) => void;
    sourceId: string;
    sourceLabel: string;
    onPick: (id: string, label: string) => void;
}) {
    const isCampaign = kind === "campaign";
    const views = useSalesforceListViews(connectionId, kind === "contact_view" ? "Contact" : "Lead", !isCampaign);
    const [q, setQ] = React.useState("");
    const [debounced, setDebounced] = React.useState("");
    React.useEffect(() => {
        const t = window.setTimeout(() => setDebounced(q.trim()), 250);
        return () => window.clearTimeout(t);
    }, [q]);
    const campaigns = useSalesforceCampaigns(connectionId, debounced, isCampaign);

    return (
        <div className="space-y-5">
            <div className="grid gap-2 sm:grid-cols-3">
                {KINDS.map((k) => {
                    const active = k.id === kind;
                    return (
                        <button
                            key={k.id}
                            type="button"
                            onClick={() => setKind(k.id)}
                            aria-pressed={active}
                            className={cn(
                                "text-left rounded-md border px-3 py-2.5 transition-colors",
                                active ? "border-sky-300 bg-sky-50" : "border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50",
                            )}
                        >
                            <k.icon className={cn("w-4 h-4", active ? "text-sky-600" : "text-slate-400")} />
                            <div className={cn("mt-1.5 text-[12.5px] font-medium", active ? "text-sky-800" : "text-slate-800")}>
                                {k.label}
                            </div>
                            <div className="text-[11px] text-slate-500 mt-0.5 leading-snug">{k.hint}</div>
                        </button>
                    );
                })}
            </div>

            <div className="max-w-md">
                <Label>{isCampaign ? "Salesforce Campaign" : "List view"}</Label>
                {isCampaign ? (
                    <SearchSelect
                        value={sourceId}
                        valueLabel={sourceLabel}
                        onChange={(id, o) => onPick(id, o.label)}
                        options={(campaigns.data ?? []).map((c) => ({
                            value: c.id,
                            label: c.name,
                            hint: [c.status, c.type, `${c.member_count.toLocaleString()} members`].filter(Boolean).join(" · "),
                        }))}
                        onQueryChange={setQ}
                        loading={campaigns.isFetching}
                        placeholder="Choose a campaign"
                        searchPlaceholder="Search Salesforce campaigns…"
                        emptyText={campaigns.isError ? errMsg(campaigns.error, "Could not load campaigns") : "No campaigns match."}
                        className="w-full"
                        minWidth={360}
                        aria-label="Salesforce Campaign"
                    />
                ) : (
                    <SearchSelect
                        value={sourceId}
                        valueLabel={sourceLabel}
                        onChange={(id, o) => onPick(id, o.label)}
                        options={(views.data ?? []).map((v) => ({ value: v.id, label: v.label }))}
                        loading={views.isFetching}
                        placeholder="Choose a list view"
                        searchPlaceholder="Search list views…"
                        emptyText={views.isError ? errMsg(views.error, "Could not load list views") : "No list views."}
                        className="w-full"
                        minWidth={320}
                        aria-label="List view"
                    />
                )}
                <p className="text-[11px] text-slate-400 mt-1.5 leading-relaxed">
                    People without an email address are skipped. Anyone already in Warmbly is updated and linked, never
                    duplicated.
                </p>
            </div>
        </div>
    );
}

function PreviewStep({
    loading,
    error,
    preview,
    sourceLabel,
    onRetry,
}: {
    loading: boolean;
    error: string | null;
    preview: SalesforceImportPreview | null;
    sourceLabel: string;
    onRetry: () => void;
}) {
    if (loading) {
        return (
            <div className="py-12 flex items-center justify-center gap-2 text-[12px] text-slate-500">
                <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400" />
                Reading {sourceLabel} from Salesforce…
            </div>
        );
    }
    if (error || !preview) {
        return (
            <div className="rounded-md border border-rose-200 bg-rose-50 px-4 py-3 max-w-xl">
                <p className="text-[12.5px] font-medium text-rose-800">Could not preview {sourceLabel}</p>
                {error && <p className="text-[11.5px] text-rose-700 mt-0.5 break-words">{error}</p>}
                <button
                    type="button"
                    onClick={onRetry}
                    className="mt-2.5 h-7 px-2.5 rounded-md border border-rose-200 bg-white text-[12px] text-rose-700 hover:bg-rose-50 inline-flex items-center gap-1.5"
                >
                    <RefreshCwIcon className="w-3 h-3" />
                    Retry
                </button>
            </div>
        );
    }
    const noEmail = preview.sample.filter((r) => !r.email).length;
    const linked = preview.sample.filter((r) => r.already_linked).length;
    return (
        <div className="space-y-3">
            <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
                <div className="text-[13px] text-slate-900">
                    <span className="text-[20px] font-semibold tabular-nums">{preview.total.toLocaleString()}</span>{" "}
                    {preview.total === 1 ? "record" : "records"} in {sourceLabel}
                </div>
                {preview.sample.length > 0 && (
                    <span className="text-[11.5px] text-slate-500">
                        Showing {preview.sample.length}. {noEmail > 0 ? `${noEmail} of them have no email and will be skipped. ` : ""}
                        {linked > 0 ? `${linked} are already linked to Warmbly contacts.` : ""}
                    </span>
                )}
            </div>
            {preview.sample.length === 0 ? (
                <p className="text-[12px] text-slate-500">Nothing to import right now.</p>
            ) : (
                <div className="rounded-md border border-slate-200 overflow-x-auto">
                    <table className="w-full text-[12px] min-w-[640px]">
                        <thead>
                            <tr className="bg-slate-50/60 border-b border-slate-200 text-left">
                                {["Name", "Email", "Company", "Title", "Owner", "Status"].map((h) => (
                                    <th key={h} className="px-3 h-8 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                        {h}
                                    </th>
                                ))}
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-100">
                            {preview.sample.map((r) => (
                                <tr key={r.record_id}>
                                    <td className="px-3 py-1.5 text-slate-900">
                                        <span className="inline-flex items-center gap-1.5">
                                            <span className="truncate max-w-[180px]">{r.name || "Unnamed"}</span>
                                            {r.already_linked && <Pill tone="sky">Already linked</Pill>}
                                        </span>
                                    </td>
                                    <td className="px-3 py-1.5">
                                        {r.email ? (
                                            <span className="text-slate-700">{r.email}</span>
                                        ) : (
                                            <span className="text-amber-700">No email</span>
                                        )}
                                    </td>
                                    <td className="px-3 py-1.5 text-slate-600 truncate max-w-[160px]">{r.company}</td>
                                    <td className="px-3 py-1.5 text-slate-600 truncate max-w-[160px]">{r.title}</td>
                                    <td className="px-3 py-1.5 text-slate-600">{r.owner_name}</td>
                                    <td className="px-3 py-1.5 text-slate-600">{r.status}</td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}

// Edit a saved import source: where it lands and whether it keeps syncing.
export function EditImportDialog({
    connectionId,
    source,
    onClose,
}: {
    connectionId: string;
    source: SalesforceImportSource;
    onClose: () => void;
}) {
    const confirm = useConfirm();
    const update = useUpdateSalesforceImportSource(connectionId);
    const [name, setName] = React.useState(source.name);
    const [campaignId, setCampaignId] = React.useState<string | null>(source.campaign_id ?? null);
    const [campaignName, setCampaignName] = React.useState("");
    const [categoryIds, setCategoryIds] = React.useState<string[]>(source.category_ids ?? []);
    const [segmentIds, setSegmentIds] = React.useState<string[]>(source.segment_ids ?? []);
    const [segmentBusy, setSegmentBusy] = React.useState(false);
    const [recurring, setRecurring] = React.useState(source.recurring);

    const dirty =
        name !== source.name ||
        (campaignId ?? null) !== (source.campaign_id ?? null) ||
        recurring !== source.recurring ||
        !sameIds(categoryIds, source.category_ids) ||
        !sameIds(segmentIds, source.segment_ids);

    const requestClose = React.useCallback(() => {
        if (update.isPending) return;
        if (dirty) confirm.show("Discard your changes to this import?", async () => onClose());
        else onClose();
    }, [dirty, confirm, onClose, update.isPending]);

    React.useEffect(() => {
        const onKey = (ev: KeyboardEvent) => {
            if (ev.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            ev.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [requestClose]);

    async function save() {
        try {
            await update.mutateAsync({
                connectionId,
                sourceId: source.id,
                body: {
                    name: name.trim() || source.source_label,
                    campaign_id: campaignId,
                    category_ids: categoryIds,
                    segment_ids: segmentIds,
                    recurring,
                },
            });
            toast.success("Import updated");
            onClose();
        } catch (err) {
            toast.error(errMsg(err, "Could not update the import"));
        }
    }

    return (
        <DialogShell title="Edit import" onRequestClose={requestClose}>
            <div className="flex-1 min-h-0 overflow-y-auto px-5 py-5 space-y-4">
                <div className="text-[11.5px] text-slate-500">
                    Reads <span className="text-slate-800 font-medium">{source.source_label}</span>. To read something else,
                    create a new import.
                </div>
                <div>
                    <Label>Name</Label>
                    <TextInput value={name} onChange={setName} maxLength={200} className="w-full" />
                </div>
                <div>
                    <Label>Add to a Warmbly campaign</Label>
                    <CampaignPicker
                        campaignId={campaignId}
                        campaignName={campaignName}
                        onChange={(id, n) => {
                            setCampaignId(id);
                            setCampaignName(n);
                        }}
                        noneLabel="Only add to contacts"
                    />
                </div>
                <div>
                    <Label>Add to segments</Label>
                    <SegmentMultiPicker
                        onBusyChange={setSegmentBusy}
                        value={segmentIds}
                        onChange={setSegmentIds}
                        allowCreate
                        suggestedName={name.trim() || source.source_label}
                        placeholder="Pick or create a segment…"
                    />
                </div>
                <div>
                    <Label>Labels</Label>
                    <CategoryPicker value={categoryIds} onChange={setCategoryIds} />
                </div>
                <div className="rounded-md border border-slate-200 px-3 py-2.5 flex items-start justify-between gap-4">
                    <div>
                        <div className="text-[12.5px] text-slate-900 font-medium">Keep in sync</div>
                        <p className="text-[11px] text-slate-500 mt-0.5">Checks for new people every 30 minutes.</p>
                    </div>
                    <Toggle value={recurring} onChange={setRecurring} ariaLabel="Keep in sync" />
                </div>
            </div>
            <div className="px-3 h-12 border-t border-slate-200 flex items-center justify-end gap-1.5 shrink-0 bg-slate-50/30">
                <button
                    type="button"
                    onClick={requestClose}
                    className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                >
                    Cancel
                </button>
                <button type="button" onClick={() => void save()} disabled={!dirty || update.isPending || segmentBusy} className={primaryBtn}>
                    {update.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                    Save
                </button>
            </div>
        </DialogShell>
    );
}

function DialogShell({
    title,
    onRequestClose,
    wide,
    children,
}: {
    title: string;
    onRequestClose: () => void;
    wide?: boolean;
    children: React.ReactNode;
}) {
    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ duration: 0.15 }}
            onMouseDown={onRequestClose}
            className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-2 sm:px-4"
        >
            <motion.div
                role="dialog"
                aria-modal="true"
                aria-label={title}
                initial={{ y: 8, opacity: 0, scale: 0.985 }}
                animate={{ y: 0, opacity: 1, scale: 1 }}
                transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                onMouseDown={(ev) => ev.stopPropagation()}
                className={cn(
                    "w-full rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[90dvh]",
                    wide ? "max-w-[860px] h-[min(90dvh,640px)]" : "max-w-[520px]",
                )}
            >
                <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                    <ProviderGlyph provider="salesforce" name="Salesforce" size={7} />
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Salesforce</span>
                    <div className="h-4 w-px bg-slate-200" />
                    <span className="text-[12.5px] text-slate-900 font-medium">{title}</span>
                    <button
                        type="button"
                        onClick={onRequestClose}
                        aria-label="Close"
                        className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </header>
                {children}
            </motion.div>
        </motion.div>
    );
}
