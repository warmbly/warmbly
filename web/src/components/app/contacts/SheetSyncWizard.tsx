// SheetSyncWizard — multi-step modal for creating (or editing) an on-demand
// Google-Sheet → contacts "sync source", and optionally running the first
// "Sync now". Mirrors ImportWizard's dialog shell + house theme, and REUSES
// its column-mapper verbatim (TargetPicker + MapStep + DEDUP_OPTIONS) so the
// /lead-sync/google/preview ImportPreview is mapped with the exact same UI as
// a CSV import.
//
// Steps:
//   1. connect — if no hidden google_sheets OAuth connection exists, run the
//      EXISTING integration OAuth popup (provider "google_sheets").
//   2. sheet   — paste a Sheet ID, fetch its tabs, pick a tab.
//   3. map     — preview first rows + map columns (reused MapStep).
//   4. options — dedup strategy, optional target campaign, optional categories.
//   5. save    — POST /lead-sync/sources, then optionally Sync now → result.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    ArrowLeftIcon,
    ArrowRightIcon,
    CheckIcon,
    Loader2Icon,
    PlugZapIcon,
    RefreshCwIcon,
    SaveIcon,
    SheetIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import { MapStep, ResultStep } from "./ImportWizard";
import { DEDUP_OPTIONS, announceResult, describeError } from "./importShared";
import CategoryPicker from "./CategoryPicker";
import { Label, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import {
    useFinishIntegrationOAuth,
    useStartIntegrationOAuth,
} from "@/lib/api/hooks/app/integrations/useIntegrationOAuth";
import { openOAuthPopup } from "@/lib/integrations/oauthPopup";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useGoogleConnection from "@/lib/api/hooks/app/leadsync/useGoogleConnection";
import {
    useGetSpreadsheet,
    usePreviewSheet,
} from "@/lib/api/hooks/app/leadsync/useSheetMeta";
import useCreateLeadSyncSource from "@/lib/api/hooks/app/leadsync/useCreateLeadSyncSource";
import useSyncLeadSyncSource from "@/lib/api/hooks/app/leadsync/useSyncLeadSyncSource";
import { useQueryClient } from "@tanstack/react-query";
import type {
    ImportColumnMapping,
    ImportDedupStrategy,
    ImportPreview,
    ImportResult,
    LeadSyncSource,
    SheetMeta,
} from "@/lib/api/models/app/leadsync/LeadSync";

type Step = "connect" | "sheet" | "map" | "options" | "result";

interface Props {
    open: boolean;
    onClose: () => void;
    // When set, the source is pre-targeted to this campaign and the campaign
    // picker is hidden — used by the per-campaign "Connect a Google Sheet".
    lockedCampaign?: { id: string; name: string };
    // Notified after a source is saved so callers can refresh their list.
    onSaved?: (source: LeadSyncSource) => void;
}

const STEP_ORDER: Step[] = ["connect", "sheet", "map", "options", "result"];

export default function SheetSyncWizard({ open, onClose, lockedCampaign, onSaved }: Props) {
    const connection = useGoogleConnection();
    const connectionId = connection.data?.connection?.id ?? null;
    const connected = !!connection.data?.connected && !!connectionId;

    const [step, setStep] = React.useState<Step>("connect");
    const [sheetId, setSheetId] = React.useState("");
    const [meta, setMeta] = React.useState<SheetMeta | null>(null);
    const [tabTitle, setTabTitle] = React.useState("");
    const [preview, setPreview] = React.useState<ImportPreview | null>(null);
    const [mapping, setMapping] = React.useState<ImportColumnMapping[]>([]);
    const [hasHeader, setHasHeader] = React.useState(true);
    const [dedup, setDedup] = React.useState<ImportDedupStrategy>("update");
    const [campaignId, setCampaignId] = React.useState<string | null>(lockedCampaign?.id ?? null);
    const [campaignName, setCampaignName] = React.useState<string>(lockedCampaign?.name ?? "");
    const [categoryIds, setCategoryIds] = React.useState<string[]>([]);
    const [label, setLabel] = React.useState("");
    const [result, setResult] = React.useState<ImportResult | null>(null);
    const [busy, setBusy] = React.useState(false);

    const startOAuth = useStartIntegrationOAuth();
    const finishOAuth = useFinishIntegrationOAuth();
    const getSpreadsheet = useGetSpreadsheet();
    const previewSheet = usePreviewSheet();
    const createSource = useCreateLeadSyncSource();
    const syncSource = useSyncLeadSyncSource();
    const queryClient = useQueryClient();

    const reset = React.useCallback(() => {
        setStep("connect");
        setSheetId("");
        setMeta(null);
        setTabTitle("");
        setPreview(null);
        setMapping([]);
        setHasHeader(true);
        setDedup("update");
        setCampaignId(lockedCampaign?.id ?? null);
        setCampaignName(lockedCampaign?.name ?? "");
        setCategoryIds([]);
        setLabel("");
        setResult(null);
        setBusy(false);
    }, [lockedCampaign]);

    React.useEffect(() => {
        if (!open) reset();
    }, [open, reset]);

    // Skip straight to the sheet step once we know a connection exists.
    React.useEffect(() => {
        if (open && step === "connect" && connected) setStep("sheet");
    }, [open, step, connected]);

    async function runConnect() {
        setBusy(true);
        try {
            const { url } = await startOAuth.mutateAsync({
                provider: "google_sheets",
                label: "Google Sheets",
            });
            const { code, state } = await openOAuthPopup(url);
            await finishOAuth.mutateAsync({ code, state });
            await connection.refetch();
            await queryClient.invalidateQueries({ queryKey: ["lead-sync", "google", "connection"] });
            toast.success("Connected to Google Sheets");
            setStep("sheet");
        } catch (err) {
            toast.error(describeError(err, "Connection failed."));
        } finally {
            setBusy(false);
        }
    }

    async function loadTabs() {
        if (!connectionId) return;
        const id = sheetId.trim();
        if (!id) {
            toast.error("Paste a Sheet ID first.");
            return;
        }
        setBusy(true);
        try {
            const m = await getSpreadsheet.mutateAsync({ connection_id: connectionId, sheet_id: id });
            setMeta(m);
            // Auto-select the first tab so the picker is never empty.
            const first = m.tabs[0]?.title ?? "";
            setTabTitle(first);
            if (!label.trim()) setLabel(m.title);
        } catch (err) {
            toast.error(describeError(err, "Couldn't read that spreadsheet."));
            setMeta(null);
        } finally {
            setBusy(false);
        }
    }

    async function loadPreview() {
        if (!connectionId || !meta) return;
        if (!tabTitle) {
            toast.error("Pick a tab first.");
            return;
        }
        setBusy(true);
        try {
            const p = await previewSheet.mutateAsync({
                connection_id: connectionId,
                sheet_id: meta.sheet_id,
                tab_title: tabTitle,
            });
            setPreview(p);
            setMapping(p.suggested_mapping);
            setHasHeader(p.has_header);
            setStep("map");
        } catch (err) {
            toast.error(describeError(err, "Couldn't read that tab."));
        } finally {
            setBusy(false);
        }
    }

    const emailMapped = mapping.some((m) => m.target === "email");

    async function save(runSync: boolean) {
        if (!connectionId || !meta) return;
        setBusy(true);
        try {
            const source = await createSource.mutateAsync({
                connection_id: connectionId,
                sheet_id: meta.sheet_id,
                sheet_title: meta.title,
                tab_title: tabTitle,
                has_header: hasHeader,
                column_mapping: mapping,
                dedup,
                target_campaign_id: campaignId ?? undefined,
                category_ids: categoryIds,
                subscribed_default: true,
                label: label.trim() || meta.title,
            });
            onSaved?.(source);
            if (runSync) {
                const res = await syncSource.mutateAsync(source.id);
                setResult(res.result);
                setStep("result");
                announceResult(res.result);
            } else {
                toast.success("Sync source saved");
                onClose();
            }
        } catch (err) {
            toast.error(describeError(err, "Couldn't save the sync source."));
        } finally {
            setBusy(false);
        }
    }

    function stepIndex(): number {
        // Hide the connect dot once connected — the visible flow is 4 steps.
        const visible = connected ? STEP_ORDER.filter((s) => s !== "connect") : STEP_ORDER;
        return Math.max(0, visible.indexOf(step));
    }
    const visibleSteps = connected ? STEP_ORDER.filter((s) => s !== "connect") : STEP_ORDER;

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={onClose}
                    className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        onClick={(e) => e.stopPropagation()}
                        className="w-full max-w-[760px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[90dvh]"
                    >
                        <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-emerald-50 text-emerald-600 flex items-center justify-center">
                                <SheetIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Sync source
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">Google Sheet</span>
                            <div className="hidden sm:flex items-center gap-1 ml-2">
                                {visibleSteps.map((_, idx) => (
                                    <span
                                        key={idx}
                                        className={`h-1 w-5 rounded-full transition-colors ${
                                            idx <= stepIndex() ? "bg-slate-900" : "bg-slate-200"
                                        }`}
                                    />
                                ))}
                            </div>
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </header>

                        <div className="flex-1 min-h-0 overflow-y-auto px-5 py-4">
                            {step === "connect" && (
                                <ConnectStep busy={busy || connection.isLoading} onConnect={runConnect} />
                            )}
                            {step === "sheet" && (
                                <SheetStep
                                    sheetId={sheetId}
                                    setSheetId={setSheetId}
                                    meta={meta}
                                    tabTitle={tabTitle}
                                    setTabTitle={setTabTitle}
                                    onLoadTabs={loadTabs}
                                    busy={busy}
                                />
                            )}
                            {step === "map" && preview && (
                                <MapStep
                                    preview={preview}
                                    mapping={mapping}
                                    setMapping={setMapping}
                                    hasHeader={hasHeader}
                                    setHasHeader={setHasHeader}
                                />
                            )}
                            {step === "options" && (
                                <OptionsStep
                                    dedup={dedup}
                                    setDedup={setDedup}
                                    label={label}
                                    setLabel={setLabel}
                                    campaignId={campaignId}
                                    campaignName={campaignName}
                                    onCampaign={(id, name) => {
                                        setCampaignId(id);
                                        setCampaignName(name);
                                    }}
                                    lockedCampaign={lockedCampaign}
                                    categoryIds={categoryIds}
                                    setCategoryIds={setCategoryIds}
                                />
                            )}
                            {step === "result" && result && (
                                <ResultStep result={result} filename={meta?.title ?? "sync"} />
                            )}
                        </div>

                        <footer className="min-h-12 py-1.5 md:py-0 px-3 border-t border-slate-200 flex flex-wrap items-center gap-1.5 shrink-0 bg-slate-50/30">
                            {step === "connect" && (
                                <span className="ml-auto text-[11px] text-slate-400">
                                    We use your existing Google Sheets authorization.
                                </span>
                            )}
                            {step === "sheet" && (
                                <>
                                    {connected && !lockedCampaign && (
                                        <span className="text-[11px] text-slate-400">
                                            Connected to Google.
                                        </span>
                                    )}
                                    <button
                                        type="button"
                                        onClick={loadPreview}
                                        disabled={busy || !meta || !tabTitle}
                                        className="ml-auto h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                                    >
                                        {busy ? (
                                            <Loader2Icon className="w-3 h-3 animate-spin" />
                                        ) : (
                                            <ArrowRightIcon className="w-3 h-3" />
                                        )}
                                        Preview rows
                                    </button>
                                </>
                            )}
                            {step === "map" && (
                                <>
                                    <button
                                        type="button"
                                        onClick={() => setStep("sheet")}
                                        className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors"
                                    >
                                        <ArrowLeftIcon className="w-3 h-3" />
                                        Back
                                    </button>
                                    {!emailMapped && (
                                        <span className="text-[11px] text-amber-700 inline-flex items-center gap-1">
                                            <AlertTriangleIcon className="w-3 h-3" />
                                            Map a column to Email
                                        </span>
                                    )}
                                    <button
                                        type="button"
                                        onClick={() => setStep("options")}
                                        disabled={!emailMapped}
                                        className="ml-auto h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                                    >
                                        Continue
                                        <ArrowRightIcon className="w-3 h-3" />
                                    </button>
                                </>
                            )}
                            {step === "options" && (
                                <>
                                    <button
                                        type="button"
                                        onClick={() => setStep("map")}
                                        className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors"
                                    >
                                        <ArrowLeftIcon className="w-3 h-3" />
                                        Back
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() => save(false)}
                                        disabled={busy}
                                        className="ml-auto h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                    >
                                        <SaveIcon className="w-3 h-3" />
                                        Save only
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() => save(true)}
                                        disabled={busy}
                                        className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                    >
                                        {busy ? (
                                            <Loader2Icon className="w-3 h-3 animate-spin" />
                                        ) : (
                                            <RefreshCwIcon className="w-3 h-3" />
                                        )}
                                        Save & sync now
                                    </button>
                                </>
                            )}
                            {step === "result" && (
                                <button
                                    type="button"
                                    onClick={onClose}
                                    className="ml-auto h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors"
                                >
                                    Done
                                </button>
                            )}
                        </footer>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}

// ----- Connect step ----------------------------------------------

function ConnectStep({ busy, onConnect }: { busy: boolean; onConnect: () => void }) {
    return (
        <div className="space-y-4">
            <div className="rounded-lg border border-slate-200 p-6 text-center">
                <div className="mx-auto size-10 rounded-md bg-emerald-50 text-emerald-600 flex items-center justify-center">
                    <SheetIcon className="w-5 h-5" />
                </div>
                <p className="text-[13px] text-slate-900 font-medium mt-3">Connect Google Sheets</p>
                <p className="text-[11.5px] text-slate-500 mt-1 max-w-[42ch] mx-auto leading-relaxed">
                    Authorize Warmbly to read your spreadsheets. We only read the rows of the tab
                    you choose — nothing is written back.
                </p>
                <button
                    type="button"
                    onClick={onConnect}
                    disabled={busy}
                    className="mt-4 h-8 px-4 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    {busy ? (
                        <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                        <PlugZapIcon className="w-3.5 h-3.5" />
                    )}
                    {busy ? "Connecting…" : "Connect with Google"}
                </button>
            </div>
            <div className="rounded-md border border-slate-200 bg-slate-50/40 p-3">
                <ul className="text-[11px] text-slate-500 space-y-0.5 list-disc pl-4 leading-snug">
                    <li>One row per contact — first row should be the column headers.</li>
                    <li>At minimum a column with email addresses.</li>
                    <li>This is on-demand: nothing syncs until you press Sync now.</li>
                </ul>
            </div>
        </div>
    );
}

// ----- Sheet + tab step ------------------------------------------

function SheetStep({
    sheetId,
    setSheetId,
    meta,
    tabTitle,
    setTabTitle,
    onLoadTabs,
    busy,
}: {
    sheetId: string;
    setSheetId: (v: string) => void;
    meta: SheetMeta | null;
    tabTitle: string;
    setTabTitle: (v: string) => void;
    onLoadTabs: () => void;
    busy: boolean;
}) {
    return (
        <div className="space-y-4">
            <section>
                <Label>Spreadsheet ID</Label>
                <div className="flex items-center gap-1.5">
                    <TextInput
                        value={sheetId}
                        onChange={setSheetId}
                        placeholder="1AbC…XyZ"
                        className="font-mono flex-1"
                    />
                    <button
                        type="button"
                        onClick={onLoadTabs}
                        disabled={busy || !sheetId.trim()}
                        className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 shrink-0"
                    >
                        {busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : null}
                        Load tabs
                    </button>
                </div>
                <p className="text-[10.5px] text-slate-400 mt-1 leading-relaxed">
                    The long ID in the sheet URL between <code className="font-mono">/d/</code> and{" "}
                    <code className="font-mono">/edit</code>.
                </p>
            </section>

            {meta && (
                <section className="space-y-2">
                    <div className="flex items-center gap-2">
                        <SheetIcon className="w-3.5 h-3.5 text-emerald-600 shrink-0" />
                        <span className="text-[12.5px] text-slate-900 font-medium truncate">{meta.title}</span>
                        <span className="text-[11px] text-slate-400">{meta.tabs.length} tabs</span>
                    </div>
                    <Label>Tab to import</Label>
                    <PopoverMenu align="start">
                        <PopoverMenuTrigger asChild>
                            <SelectButton label={tabTitle || "Select a tab…"} className="w-full" />
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={260}>
                            <PopoverMenuLabel>Tabs</PopoverMenuLabel>
                            {meta.tabs.map((t) => (
                                <PopoverMenuItem
                                    key={`${t.index}-${t.title}`}
                                    selected={t.title === tabTitle}
                                    onSelect={() => setTabTitle(t.title)}
                                >
                                    {t.title}
                                </PopoverMenuItem>
                            ))}
                        </PopoverMenuContent>
                    </PopoverMenu>
                </section>
            )}
        </div>
    );
}

// ----- Options step ----------------------------------------------

function OptionsStep({
    dedup,
    setDedup,
    label,
    setLabel,
    campaignId,
    campaignName,
    onCampaign,
    lockedCampaign,
    categoryIds,
    setCategoryIds,
}: {
    dedup: ImportDedupStrategy;
    setDedup: (v: ImportDedupStrategy) => void;
    label: string;
    setLabel: (v: string) => void;
    campaignId: string | null;
    campaignName: string;
    onCampaign: (id: string | null, name: string) => void;
    lockedCampaign?: { id: string; name: string };
    categoryIds: string[];
    setCategoryIds: (v: string[]) => void;
}) {
    return (
        <div className="space-y-5">
            <section>
                <Label>Source label</Label>
                <TextInput value={label} onChange={setLabel} placeholder="My leads sheet" />
                <p className="text-[10.5px] text-slate-400 mt-1">
                    Shown in your Sync sources list. Defaults to the spreadsheet title.
                </p>
            </section>

            <section>
                <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                    Duplicate handling
                </h2>
                <p className="text-[11px] text-slate-400 leading-tight mb-3">
                    We dedupe on lowercased email each time you sync. Decide what happens when a row
                    matches a contact you already have.
                </p>
                <div className="space-y-2">
                    {DEDUP_OPTIONS.map((opt) => (
                        <label
                            key={opt.id}
                            className={`block rounded-md border p-2.5 cursor-pointer transition-colors ${
                                dedup === opt.id
                                    ? "border-slate-900 bg-slate-50"
                                    : "border-slate-200 hover:border-slate-300"
                            }`}
                        >
                            <div className="flex items-start gap-2">
                                <input
                                    type="radio"
                                    name="leadsync-dedup"
                                    className="mt-0.5 accent-slate-900"
                                    checked={dedup === opt.id}
                                    onChange={() => setDedup(opt.id)}
                                />
                                <div className="flex-1 min-w-0">
                                    <div className="text-[12px] font-medium text-slate-900 leading-tight">
                                        {opt.label}
                                    </div>
                                    <div className="text-[11px] text-slate-500 leading-snug mt-0.5">
                                        {opt.hint}
                                    </div>
                                </div>
                            </div>
                        </label>
                    ))}
                </div>
            </section>

            <section>
                <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                    Enroll in campaign
                </h2>
                {lockedCampaign ? (
                    <div className="rounded-md border border-sky-200 bg-sky-50/60 px-3 py-2 flex items-center gap-2">
                        <CheckIcon className="w-3.5 h-3.5 text-sky-600 shrink-0" />
                        <span className="text-[12px] text-slate-800">
                            New & updated leads join{" "}
                            <span className="font-medium">{lockedCampaign.name}</span>.
                        </span>
                    </div>
                ) : (
                    <>
                        <p className="text-[11px] text-slate-400 leading-tight mb-2">
                            Optionally enroll every synced contact into a campaign.
                        </p>
                        <CampaignPicker
                            campaignId={campaignId}
                            campaignName={campaignName}
                            onChange={onCampaign}
                        />
                    </>
                )}
            </section>

            <section>
                <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                    Apply categories
                </h2>
                <p className="text-[11px] text-slate-400 leading-tight mb-2">
                    Every synced contact gets these categories. Skip to leave them untagged.
                </p>
                <CategoryPicker value={categoryIds} onChange={setCategoryIds} />
            </section>
        </div>
    );
}

// CampaignPicker — house-theme PopoverMenu campaign selector backed by the
// existing campaigns list query. Single-select with an explicit "None".
function CampaignPicker({
    campaignId,
    campaignName,
    onChange,
}: {
    campaignId: string | null;
    campaignName: string;
    onChange: (id: string | null, name: string) => void;
}) {
    const [query, setQuery] = React.useState("");
    const campaigns = useCampaigns({ query, folder: "" });
    const label = campaignId ? campaignName || "Selected campaign" : "No campaign";

    return (
        <PopoverMenu align="start">
            <PopoverMenuTrigger asChild>
                <SelectButton label={label} className="w-full" />
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={280}>
                <div className="px-2 py-1.5 border-b border-slate-200">
                    <input
                        value={query}
                        onChange={(e) => setQuery(e.target.value)}
                        placeholder="Search campaigns…"
                        className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                    />
                </div>
                <PopoverMenuItem selected={!campaignId} onSelect={() => onChange(null, "")}>
                    No campaign
                </PopoverMenuItem>
                {campaigns.campaigns.map((c) => (
                    <PopoverMenuItem
                        key={c.id}
                        selected={c.id === campaignId}
                        onSelect={() => onChange(c.id, c.name)}
                    >
                        {c.name}
                    </PopoverMenuItem>
                ))}
                {campaigns.campaigns.length === 0 && (
                    <div className="px-3 py-2 text-[11.5px] text-slate-400 text-center">
                        {campaigns.isPending ? "Loading…" : "No campaigns found."}
                    </div>
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
