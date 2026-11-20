// New Apollo import: pick a saved list, preview who comes across, then choose
// where they land. Also the edit dialog for a saved import.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertCircleIcon,
    CheckIcon,
    ChevronLeftIcon,
    ChevronRightIcon,
    DownloadCloudIcon,
    Loader2Icon,
    RefreshCwIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";

import { Label, TextInput } from "@/components/ui/field";
import CampaignPicker from "@/components/app/campaigns/CampaignPicker";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import CategoryPicker from "@/components/app/contacts/CategoryPicker";
import { SegmentMultiPicker } from "@/components/app/segments/SegmentPickers";
import { useConfirm } from "@/hooks/context/confirm";
import {
    useApolloImportPreview,
    useApolloLists,
    useCreateApolloImportSource,
    useUpdateApolloImportSource,
} from "@/lib/api/hooks/app/integrations/useApollo";
import {
    APOLLO_EMAIL_STATUS_LABELS,
    type ApolloImportSource,
    type ApolloPreview,
} from "@/lib/api/models/app/integrations/Apollo";
import { cn } from "@/lib/utils";

import { Pill, SearchSelect, primaryBtn } from "../../salesforce/_components/shared";
import { errMsg, sameIds } from "../../salesforce/_components/util";
import DialogShell from "./DialogShell";
import useEscapeToClose from "./useEscapeToClose";

const STEPS = [
    { key: "list", label: "List" },
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

    const [listId, setListId] = React.useState("");
    const [listName, setListName] = React.useState("");
    const [verifiedOnly, setVerifiedOnly] = React.useState(true);
    const [preview, setPreview] = React.useState<ApolloPreview | null>(null);
    const [previewFor, setPreviewFor] = React.useState("");
    const [name, setName] = React.useState("");
    const [campaignId, setCampaignId] = React.useState<string | null>(null);
    const [campaignName, setCampaignName] = React.useState("");
    const [categoryIds, setCategoryIds] = React.useState<string[]>([]);
    const [segmentIds, setSegmentIds] = React.useState<string[]>([]);
    const [segmentBusy, setSegmentBusy] = React.useState(false);
    const [recurring, setRecurring] = React.useState(true);

    const lists = useApolloLists(connectionId);
    const previewM = useApolloImportPreview();
    const create = useCreateApolloImportSource(connectionId);

    const selectionKey = `${listId}|${verifiedOnly}`;
    const dirty = !!listId;

    const requestClose = React.useCallback(() => {
        if (create.isPending) return;
        if (dirty) confirm.show("Discard this import? Nothing has been imported yet.", async () => onClose());
        else onClose();
    }, [dirty, confirm, onClose, create.isPending]);
    useEscapeToClose(requestClose);

    // The selection the newest preview request was for; an older answer is dropped.
    const latestPreview = React.useRef("");
    async function loadPreview() {
        const key = selectionKey;
        latestPreview.current = key;
        setPreviewFor(key);
        setPreview(null);
        try {
            const p = await previewM.mutateAsync({ connectionId, list_id: listId, verified_only: verifiedOnly });
            if (latestPreview.current === key) setPreview(p);
        } catch {
            // Rendered in the step from previewM.error.
        }
    }

    function issueFor(s: number): string | null {
        if (s === 0 && !listId) return "Pick an Apollo list first";
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
        if (target === 2 && !name.trim()) setName(listName);
    }

    async function submit() {
        try {
            await create.mutateAsync({
                connectionId,
                body: {
                    name: name.trim() || listName,
                    list_id: listId,
                    list_name: listName,
                    campaign_id: campaignId ?? undefined,
                    category_ids: categoryIds,
                    segment_ids: segmentIds,
                    verified_only: verifiedOnly,
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
        <DialogShell provider="apollo" providerName="Apollo" title="New import" onRequestClose={requestClose} wide>
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
                            <div className="space-y-5 max-w-md">
                                <div>
                                    <Label>Apollo list</Label>
                                    <SearchSelect
                                        value={listId}
                                        valueLabel={listName}
                                        onChange={(id, o) => {
                                            setListId(id);
                                            setListName(o.label);
                                            setNudge(null);
                                        }}
                                        options={(lists.data ?? []).map((l) => ({
                                            value: l.id,
                                            label: l.name || "Untitled list",
                                            hint: `${l.count.toLocaleString()} ${l.count === 1 ? "person" : "people"}`,
                                        }))}
                                        loading={lists.isFetching}
                                        placeholder="Choose a list"
                                        searchPlaceholder="Search your lists…"
                                        emptyText={lists.isError ? errMsg(lists.error, "Could not load your lists") : "No saved lists of people in Apollo."}
                                        className="w-full"
                                        minWidth={340}
                                        aria-label="Apollo list"
                                    />
                                    <p className="text-[11px] text-slate-400 mt-1.5 leading-relaxed">
                                        Saved lists of people from your Apollo account. Reading a list spends no Apollo credits.
                                    </p>
                                </div>
                                <div className="rounded-md border border-slate-200 px-3 py-2.5 flex items-start justify-between gap-4">
                                    <div>
                                        <div className="text-[12.5px] text-slate-900 font-medium">Verified addresses only</div>
                                        <p className="text-[11px] text-slate-500 mt-0.5 leading-relaxed">
                                            Leaves out people whose Apollo email status is not verified. Guessed addresses bounce more and
                                            hurt your mailboxes.
                                        </p>
                                    </div>
                                    <Toggle value={verifiedOnly} onChange={setVerifiedOnly} ariaLabel="Verified addresses only" />
                                </div>
                            </div>
                        )}
                        {step === 1 && (
                            <PreviewStep
                                loading={previewM.isPending}
                                error={previewM.isError ? errMsg(previewM.error, "Could not read the list") : null}
                                preview={previewFor === selectionKey ? preview : null}
                                listName={listName}
                                onRetry={() => void loadPreview()}
                            />
                        )}
                        {step === 2 && (
                            <div className="space-y-4 max-w-xl">
                                <div>
                                    <Label>Name</Label>
                                    <TextInput value={name} onChange={setName} placeholder={listName} maxLength={200} className="w-full" />
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
                                        suggestedName={name.trim() || listName}
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
                                            Checks the list every hour and brings in people added to it. Off imports once.
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
                            {create.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <DownloadCloudIcon className="w-3.5 h-3.5" />}
                            Create and import
                        </button>
                    )}
                </div>
            </div>
        </DialogShell>
    );
}

function PreviewStep({
    loading,
    error,
    preview,
    listName,
    onRetry,
}: {
    loading: boolean;
    error: string | null;
    preview: ApolloPreview | null;
    listName: string;
    onRetry: () => void;
}) {
    if (loading) {
        return (
            <div className="py-12 flex items-center justify-center gap-2 text-[12px] text-slate-500">
                <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400" />
                Reading {listName} from Apollo…
            </div>
        );
    }
    if (error || !preview) {
        return (
            <div className="rounded-md border border-rose-200 bg-rose-50 px-4 py-3 max-w-xl">
                <p className="text-[12.5px] font-medium text-rose-800">Could not preview {listName}</p>
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
    const left = preview.sample.filter((r) => !r.will_import).length;
    const have = preview.sample.filter((r) => r.already_in_warmbly).length;
    return (
        <div className="space-y-3">
            <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
                <div className="text-[13px] text-slate-900">
                    <span className="text-[20px] font-semibold tabular-nums">{preview.total.toLocaleString()}</span>{" "}
                    {preview.total === 1 ? "person" : "people"} in {listName}
                </div>
                {preview.sample.length > 0 && (
                    <span className="text-[11.5px] text-slate-500">
                        Showing {preview.sample.length}. {left > 0 ? `${left} of them will be left out. ` : ""}
                        {have > 0 ? `${have} are already Warmbly contacts and will be updated.` : ""}
                    </span>
                )}
            </div>
            {preview.sample.length === 0 ? (
                <p className="text-[12px] text-slate-500">This list is empty.</p>
            ) : (
                <div className="rounded-md border border-slate-200 overflow-x-auto">
                    <table className="w-full text-[12px] min-w-[640px]">
                        <thead>
                            <tr className="bg-slate-50/60 border-b border-slate-200 text-left">
                                {["Name", "Email", "Email status", "Company", "Title"].map((h) => (
                                    <th key={h} className="px-3 h-8 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                        {h}
                                    </th>
                                ))}
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-slate-100">
                            {preview.sample.map((r) => (
                                <tr key={r.apollo_id} className={cn(!r.will_import && "opacity-60")}>
                                    <td className="px-3 py-1.5 text-slate-900">
                                        <span className="inline-flex items-center gap-1.5">
                                            <span className="truncate max-w-[180px]">{r.name || "Unnamed"}</span>
                                            {r.already_in_warmbly && <Pill tone="sky">In Warmbly</Pill>}
                                        </span>
                                    </td>
                                    <td className="px-3 py-1.5">
                                        {r.email ? <span className="text-slate-700">{r.email}</span> : <span className="text-amber-700">No email</span>}
                                    </td>
                                    <td className="px-3 py-1.5">
                                        {r.email_status ? (
                                            <Pill tone={r.email_status === "verified" ? "emerald" : "amber"}>
                                                {APOLLO_EMAIL_STATUS_LABELS[r.email_status] ?? r.email_status}
                                            </Pill>
                                        ) : (
                                            <span className="text-slate-400">Unknown</span>
                                        )}
                                    </td>
                                    <td className="px-3 py-1.5 text-slate-600 truncate max-w-[160px]">{r.company}</td>
                                    <td className="px-3 py-1.5 text-slate-600 truncate max-w-[160px]">{r.title}</td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
            <p className="text-[11px] text-slate-400 leading-relaxed">
                Faded rows have no address, an unverified one, or are on your suppression list. They are not imported.
            </p>
        </div>
    );
}

// Edit a saved import: where it lands, the address filter and whether it keeps syncing.
export function EditImportDialog({
    connectionId,
    source,
    onClose,
}: {
    connectionId: string;
    source: ApolloImportSource;
    onClose: () => void;
}) {
    const confirm = useConfirm();
    const update = useUpdateApolloImportSource(connectionId);
    const [name, setName] = React.useState(source.name);
    const [campaignId, setCampaignId] = React.useState<string | null>(source.campaign_id ?? null);
    const [campaignName, setCampaignName] = React.useState("");
    const [categoryIds, setCategoryIds] = React.useState<string[]>(source.category_ids ?? []);
    const [segmentIds, setSegmentIds] = React.useState<string[]>(source.segment_ids ?? []);
    const [segmentBusy, setSegmentBusy] = React.useState(false);
    const [verifiedOnly, setVerifiedOnly] = React.useState(source.verified_only);
    const [recurring, setRecurring] = React.useState(source.recurring);

    const dirty =
        name !== source.name ||
        (campaignId ?? null) !== (source.campaign_id ?? null) ||
        recurring !== source.recurring ||
        verifiedOnly !== source.verified_only ||
        !sameIds(categoryIds, source.category_ids) ||
        !sameIds(segmentIds, source.segment_ids);

    const requestClose = React.useCallback(() => {
        if (update.isPending) return;
        if (dirty) confirm.show("Discard your changes to this import?", async () => onClose());
        else onClose();
    }, [dirty, confirm, onClose, update.isPending]);
    useEscapeToClose(requestClose);

    async function save() {
        try {
            await update.mutateAsync({
                connectionId,
                sourceId: source.id,
                body: {
                    name: name.trim() || source.list_name,
                    campaign_id: campaignId,
                    category_ids: categoryIds,
                    segment_ids: segmentIds,
                    verified_only: verifiedOnly,
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
        <DialogShell provider="apollo" providerName="Apollo" title="Edit import" onRequestClose={requestClose}>
            <div className="flex-1 min-h-0 overflow-y-auto px-5 py-5 space-y-4">
                <div className="text-[11.5px] text-slate-500">
                    Reads the list <span className="text-slate-800 font-medium">{source.list_name || source.list_id}</span>. To read
                    another list, create a new import.
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
                        suggestedName={name.trim() || source.list_name}
                        placeholder="Pick or create a segment…"
                    />
                </div>
                <div>
                    <Label>Labels</Label>
                    <CategoryPicker value={categoryIds} onChange={setCategoryIds} />
                </div>
                <div className="rounded-md border border-slate-200 px-3 py-2.5 flex items-start justify-between gap-4">
                    <div>
                        <div className="text-[12.5px] text-slate-900 font-medium">Verified addresses only</div>
                        <p className="text-[11px] text-slate-500 mt-0.5">Leaves out guessed and unavailable addresses.</p>
                    </div>
                    <Toggle value={verifiedOnly} onChange={setVerifiedOnly} ariaLabel="Verified addresses only" />
                </div>
                <div className="rounded-md border border-slate-200 px-3 py-2.5 flex items-start justify-between gap-4">
                    <div>
                        <div className="text-[12.5px] text-slate-900 font-medium">Keep in sync</div>
                        <p className="text-[11px] text-slate-500 mt-0.5">Checks the list for new people every hour.</p>
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
