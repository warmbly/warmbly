// Starts a placement batch: the same placement test from many sending
// mailboxes, a few at a time, so the results can be read by domain and
// provider. Three steps (Senders, Email, Review); every count comes from the
// server's preview of the same request, so nothing here guesses.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { Link, useNavigate } from "@tanstack/react-router";
import {
    AlertCircleIcon,
    AlertTriangleIcon,
    CheckIcon,
    ChevronLeftIcon,
    ChevronRightIcon,
    Layers3Icon,
    Loader2Icon,
    PlayIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";
import { Label, NumberInput, SearchInput } from "@/components/ui/field";
import { Checkbox } from "@/components/ui/checkbox";
import { OptionSelect, Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import TagSelector from "@/components/app/popup/select/TagSelector";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import {
    useCreatePlacementBatch,
    usePlacementBatchPreview,
    usePlacementOverview,
    usePlacementSeeds,
} from "@/lib/api/hooks/app/placement/usePlacement";
import {
    PANEL_LABEL,
    type PlacementBatchPreview,
    type PlacementBatchRequest,
    type PlacementPanel,
    type PlacementSample,
    type PlacementSampleMode,
    type PlacementSenderScope,
    type PlacementTracking,
    type PlacementUnavailable,
    type PlacementWorkspaceSeed,
} from "@/lib/api/models/app/placement/Placement";
import { domainOf } from "@/lib/api/models/app/emails/MailboxSources";
import type { AppError } from "@/lib/api/client/normalizeError";
import { cn } from "@/lib/utils";
import { fmtUsd, fmtUsdCost } from "@/lib/money";
import {
    CampaignPicker,
    CopySourceFields,
    FamilyChips,
    InlineError,
    PanelChoice,
    SectionLabel,
    TrackingChoice,
} from "../tests/PlacementFormParts";
import {
    copyBody,
    copyIssue,
    newIdempotencyKey,
    useCampaignEmailSteps,
    type CopyDraft,
    type CopySource,
} from "../tests/placementCopy";
import SeedChooser from "../tests/SeedChooser";
import { BATCH_RETRY_DAYS, batchErrorMessage, fmtDuration, type BatchErrorField } from "./placementBatches";

type Scope = "mailboxes" | "campaign" | "workspace";
type StepKey = "senders" | "email" | "review";

const STEPS: { key: StepKey; label: string }[] = [
    { key: "senders", label: "Senders" },
    { key: "email", label: "Email" },
    { key: "review", label: "Review" },
];

// Rows drawn in the mailbox list at once; search narrows the rest.
const MAILBOX_ROWS_SHOWN = 300;

interface Draft extends CopyDraft {
    scope: Scope;
    senderIds: string[];
    scopeCampaignId: string;
    providers: string[];
    domains: string[];
    tagIds: string[];
    untested: boolean;
    untestedDays: number;
    includeInactive: boolean;
    sampleMode: PlacementSampleMode;
    sampleCount: number;
    samplePercent: number;
    spread: boolean;
    tracking: PlacementTracking;
    panel: PlacementPanel;
    seedIds: string[];
    families: string[];
    onUnavailable: PlacementUnavailable;
    // The dollar total the user agreed to; a different total needs asking again.
    payConsent: number | null;
}

export interface NewPlacementBatchPrefill {
    campaignId?: string;
    scope?: Scope;
    untestedDays?: number;
    senderIds?: string[];
}

function emptyDraft(prefill?: NewPlacementBatchPrefill): Draft {
    const scope: Scope =
        prefill?.scope ?? (prefill?.senderIds?.length ? "mailboxes" : prefill?.campaignId ? "campaign" : "workspace");
    const fromStep = !!prefill?.campaignId;
    return {
        source: fromStep ? "step" : "custom",
        campaignId: prefill?.campaignId ?? "",
        stepId: "",
        subject: "",
        bodyHtml: "",
        bodyPlain: "",
        bodyCode: false,
        contact: null,
        scope,
        senderIds: prefill?.senderIds ?? [],
        scopeCampaignId: prefill?.campaignId ?? "",
        providers: [],
        domains: [],
        tagIds: [],
        untested: !!prefill?.untestedDays,
        untestedDays: prefill?.untestedDays ?? 30,
        includeInactive: false,
        sampleMode: "all",
        sampleCount: 50,
        samplePercent: 10,
        spread: true,
        tracking: fromStep ? "campaign" : "off",
        panel: "instance",
        seedIds: [],
        families: [],
        onUnavailable: "defer",
        payConsent: null,
    };
}

// What the user typed or picked, for the discard prompt. Defaults the dialog
// fills in itself (the panel, the first step) do not count.
function draftKey(d: Draft): string {
    return JSON.stringify({ ...d, panel: "", stepId: "", payConsent: null, contact: d.contact?.id ?? "" });
}

function sampleOf(d: Draft): PlacementSample {
    const stratify = d.spread ? { stratify: "provider" as const } : {};
    switch (d.sampleMode) {
        case "random":
            return { mode: "random", count: d.sampleCount, ...stratify };
        case "percent":
            return { mode: "percent", percent: d.samplePercent, ...stratify };
        case "per_domain":
        case "per_provider":
            return { mode: d.sampleMode, count: d.sampleCount };
        default:
            return { mode: "all" };
    }
}

// The sender half of a request, or null while it names nobody.
function selectionBody(d: Draft, opts: { base?: boolean } = {}): PlacementBatchRequest | null {
    if (d.scope === "mailboxes") return d.senderIds.length > 0 ? { sender_account_ids: d.senderIds, sample: { mode: "all" } } : null;
    if (d.scope === "campaign" && !d.scopeCampaignId) return null;
    const scope: PlacementSenderScope = {
        type: d.scope,
        ...(d.scope === "campaign" ? { campaign_id: d.scopeCampaignId } : {}),
        ...(!opts.base && d.providers.length > 0 ? { providers: d.providers } : {}),
        ...(d.domains.length > 0 ? { domains: d.domains } : {}),
        ...(d.tagIds.length > 0 ? { tag_ids: d.tagIds } : {}),
        ...(d.includeInactive ? { include_inactive: true } : {}),
        ...(d.untested && d.untestedDays > 0 ? { untested_days: d.untestedDays } : {}),
    };
    return { sender_scope: scope, sample: opts.base ? { mode: "all" } : sampleOf(d) };
}

// Holds a request body until it has been stable for a moment.
function useSettledBody(body: PlacementBatchRequest | null) {
    const key = body ? JSON.stringify(body) : "";
    const debounced = useDebouncedValue(key, 400);
    const settledBody = React.useMemo(() => (debounced ? (JSON.parse(debounced) as PlacementBatchRequest) : null), [debounced]);
    return { body: settledBody, settled: debounced === key };
}

function usePreview(body: PlacementBatchRequest | null) {
    const settled = useSettledBody(body);
    const q = usePlacementBatchPreview(settled.body);
    const error = q.isError ? batchErrorMessage(q.error as unknown as AppError) : null;
    const ready = !!body && settled.settled && !!q.data && !q.isPlaceholderData && !q.isError;
    return { data: ready ? q.data : undefined, stale: q.data, error, loading: !!body && !ready && !error };
}

const n = (v: number) => v.toLocaleString();
const plural = (v: number, one: string, many = `${one}s`) => `${n(v)} ${v === 1 ? one : many}`;

export default function NewPlacementBatchDialog({
    open,
    onClose,
    prefill,
}: {
    open: boolean;
    onClose: () => void;
    prefill?: NewPlacementBatchPrefill;
}) {
    if (typeof document === "undefined") return null;
    return createPortal(
        <AnimatePresence>{open && <DialogBody key="placement-batch-dialog" onClose={onClose} prefill={prefill} />}</AnimatePresence>,
        document.body,
    );
}

function DialogBody({ onClose, prefill }: { onClose: () => void; prefill?: NewPlacementBatchPrefill }) {
    const navigate = useNavigate();
    const confirm = useConfirm();
    const overview = usePlacementOverview();
    const seeds = usePlacementSeeds();
    const create = useCreatePlacementBatch();
    const canBuy = usePermission("MANAGE_BILLING");

    const [draft, setDraft] = React.useState<Draft>(() => emptyDraft(prefill));
    const initialKey = React.useRef(draftKey(emptyDraft(prefill)));
    const [step, setStep] = React.useState(0);
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [nudged, setNudged] = React.useState(false);
    const [error, setError] = React.useState<{ field: BatchErrorField; message: string } | null>(null);

    // A retried submit of the same draft lands on the batch the first one
    // queued; any edit makes it a new request and clears the last refusal.
    const idemKey = React.useRef(newIdempotencyKey());
    const patch = React.useCallback((p: Partial<Draft>) => {
        idemKey.current = newIdempotencyKey();
        setError(null);
        setDraft((d) => ({ ...d, ...p }));
    }, []);

    // A campaign picked as the senders is also the copy, until the copy is chosen by hand.
    const setScopeCampaign = (id: string) => {
        idemKey.current = newIdempotencyKey();
        setError(null);
        setDraft((d) => {
            const copyUntouched =
                d.source === "custom"
                    ? !d.subject.trim() && !d.bodyPlain.trim() && !d.bodyHtml.trim()
                    : !d.campaignId || d.campaignId === d.scopeCampaignId;
            if (!copyUntouched) return { ...d, scopeCampaignId: id };
            return {
                ...d,
                scopeCampaignId: id,
                source: "step",
                campaignId: id,
                stepId: "",
                contact: null,
                tracking: d.source === "custom" ? "campaign" : d.tracking,
            };
        });
    };

    const scopeCampaign = useCampaign(draft.scope === "campaign" ? draft.scopeCampaignId : "");
    const campaign = useCampaign(draft.source === "step" ? draft.campaignId : "");
    const steps = useCampaignEmailSteps(draft.campaignId, draft.source === "step");

    // Default the step to the campaign's first email step.
    React.useEffect(() => {
        if (draft.source !== "step" || !draft.campaignId || steps.emailSteps.length === 0) return;
        if (steps.emailSteps.some((s) => s.id === draft.stepId)) return;
        setDraft((d) => ({ ...d, stepId: steps.emailSteps[0].id }));
    }, [draft.source, draft.campaignId, draft.stepId, steps.emailSteps]);

    // Default the panel to the first one that can run a test.
    const panels = React.useMemo(() => overview.data?.panels ?? [], [overview.data]);
    const panel = panels.find((p) => p.panel === draft.panel);
    React.useEffect(() => {
        if (panels.length === 0 || panel?.available) return;
        const first = panels.find((p) => p.available);
        if (first) setDraft((d) => ({ ...d, panel: first.panel }));
    }, [panels, panel?.available]);

    const textOnly = draft.source === "step" && !!campaign.data?.text_only;
    React.useEffect(() => {
        if (textOnly && (draft.tracking === "on" || draft.tracking === "compare")) {
            setDraft((d) => ({ ...d, tracking: "campaign" }));
        }
    }, [textOnly, draft.tracking]);

    const mailboxes = React.useMemo(() => (seeds.data ?? []).filter((m) => !m.seed), [seeds.data]);
    const ownSeeds = React.useMemo(() => (seeds.data ?? []).filter((m) => m.seed), [seeds.data]);
    const chosenSeeds = draft.panel === "workspace" ? ownSeeds.filter((m) => draft.seedIds.includes(m.email_account_id) && m.status === "active") : [];
    const panelFamilies = React.useMemo(() => panel?.families ?? [], [panel]);
    const chosenFamilies = draft.panel === "workspace" ? [] : draft.families.filter((f) => panelFamilies.some((p) => p.family === f));
    const familySeeds = chosenFamilies.length
        ? panelFamilies.filter((p) => chosenFamilies.includes(p.family)).reduce((acc, p) => acc + p.seeds, 0)
        : (panel?.seeds ?? 0);

    // Previews: the selection alone, the scope before the provider filter and
    // sample (for the provider chips), and the whole request for the review.
    const selection = selectionBody(draft);
    const senders = usePreview(selection);
    const baseSelection = draft.scope === "mailboxes" ? null : selectionBody(draft, { base: true });
    const base = usePreview(baseSelection);

    const emailIssue: string | null =
        copyIssue(draft, steps) ??
        (!panel || !panel.available
            ? "Pick a panel that can run a test."
            : panel.seeds === 0
              ? panel.panel === "workspace"
                  ? "You have no seed inboxes yet. Mark one on the Seed inboxes tab of Placement tests."
                  : "This panel has no seed inboxes yet."
              : draft.panel === "workspace" && draft.seedIds.length > 0 && chosenSeeds.length === 0
                ? "None of the seed inboxes you chose are connected. Choose others, or clear the choice."
                : chosenFamilies.length > 0 && familySeeds === 0
                  ? "This panel has no seeds at the providers you chose."
                  : null);

    const fullBody: PlacementBatchRequest | null =
        selection && !emailIssue
            ? {
                  ...selection,
                  ...copyBody(draft),
                  tracking: draft.tracking,
                  panel: draft.panel,
                  ...(chosenFamilies.length > 0 ? { families: chosenFamilies } : {}),
                  ...(chosenSeeds.length > 0 ? { seed_ids: chosenSeeds.map((m) => m.email_account_id) } : {}),
                  on_unavailable: draft.onUnavailable,
              }
            : null;
    const full = usePreview(fullBody);

    const tooLarge = (p: PlacementBatchPreview) =>
        p.selected > p.senders_max
            ? `This selection has ${n(p.selected)} senders and a batch can hold ${n(p.senders_max)}. Narrow the filters or take a sample.`
            : null;
    const emptyIssue = (p: PlacementBatchPreview) =>
        p.selected === 0
            ? p.matched === 0
                ? "No sending mailbox matches this selection."
                : "This sample comes to no mailboxes. Raise the number."
            : null;

    const sendersIssue: string | null =
        draft.scope === "mailboxes" && draft.senderIds.length === 0
            ? "Choose at least one mailbox."
            : draft.scope === "campaign" && !draft.scopeCampaignId
              ? "Pick a campaign."
              : senders.error
                ? senders.error.message
                : !senders.data
                  ? "Counting the mailboxes…"
                  : (emptyIssue(senders.data) ?? tooLarge(senders.data));

    const fp = full.data;
    const consented = !!fp && fp.paid_tests > 0 && draft.payConsent === fp.cost_usd;
    const canPay = !fp || fp.paid_tests === 0 || (fp.usage.price_per_test_usd > 0 && (fp.usage.balance_usd == null || fp.usage.balance_usd >= fp.cost_usd));
    const reviewIssue: string | null = full.error
        ? full.error.message
        : !fp
          ? "Working out the cost…"
          : (emptyIssue(fp) ??
            tooLarge(fp) ??
            (fp.paid_tests > 0 && fp.usage.price_per_test_usd === 0
                ? `This month's free tests cover ${n(fp.free_tests)} of the ${n(fp.tests)} tests, and tests past them cannot be paid for. Take a smaller sample, or use your own seed inboxes.`
                : fp.paid_tests > 0 && !canPay
                  ? `The paid tests cost up to ${fmtUsdCost(fp.cost_usd)} and the workspace balance is ${fmtUsd(fp.usage.balance_usd ?? 0)}. Add money under Settings > Billing, or take a smaller sample.`
                  : fp.paid_tests > 0 && !consented
                    ? `Tick the box to pay up to ${fmtUsdCost(fp.cost_usd)} for this batch.`
                    : null));

    const issueOf = React.useCallback(
        (key: StepKey) => (key === "senders" ? sendersIssue : key === "email" ? emailIssue : reviewIssue),
        [sendersIssue, emailIssue, reviewIssue],
    );
    const current = STEPS[step];
    const issue = issueOf(current.key);
    React.useEffect(() => {
        if (!issue) setNudged(false);
    }, [issue]);

    // A step is reachable when every step before it is complete.
    const canReach = React.useCallback(
        (target: number) => {
            for (let i = 0; i < target; i++) if (issueOf(STEPS[i].key)) return false;
            return true;
        },
        [issueOf],
    );
    const goTo = React.useCallback(
        (target: number, force = false) => {
            if (target === step) return;
            if (!force && target > step && !canReach(target)) {
                setNudged(true);
                return;
            }
            setDirection(target > step ? 1 : -1);
            setNudged(false);
            setStep(target);
        },
        [step, canReach],
    );

    const dirty = draftKey(draft) !== initialKey.current;
    const pending = create.isPending;

    const requestClose = React.useCallback(() => {
        if (pending) return;
        if (dirty) {
            confirm.show("Discard this placement batch?", async () => onClose());
            return;
        }
        onClose();
    }, [pending, dirty, confirm, onClose]);

    React.useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            // An open picker or the discard confirm owns this Escape.
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [requestClose]);

    const next = () => {
        if (issue) {
            setNudged(true);
            return;
        }
        goTo(step + 1);
    };

    async function submit() {
        if (pending) return;
        if (issue || !fullBody || !fp) {
            setNudged(true);
            return;
        }
        const body: PlacementBatchRequest = { ...fullBody, ...(fp.paid_tests > 0 ? { max_usd: fp.cost_usd } : {}) };
        try {
            const batch = await create.mutateAsync({ body, idempotencyKey: idemKey.current });
            toast.success("Placement batch queued.");
            onClose();
            navigate({ to: "/app/placement/batches/$id", params: { id: batch.id } });
        } catch (err) {
            const e = batchErrorMessage(err as AppError, { resetsOn: fp.usage.period_end, panel: draft.panel });
            setError(e);
            const at = STEPS.findIndex((s) => s.key === e.field);
            if (at >= 0 && at < step) goTo(at, true);
        }
    }

    const fieldError = (f: BatchErrorField) => (error?.field === f ? <InlineError message={error.message} /> : null);
    const lastStep = STEPS.length - 1;
    // "Counting…" is a wait, not a mistake, so it never reads as a warning.
    const waiting = issue === "Counting the mailboxes…" || issue === "Working out the cost…";

    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            onMouseDown={requestClose}
            className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
        >
            <motion.div
                role="dialog"
                aria-modal="true"
                aria-label="New placement batch"
                initial={{ y: 8, opacity: 0, scale: 0.985 }}
                animate={{ y: 0, opacity: 1, scale: 1 }}
                exit={{ y: 8, opacity: 0, scale: 0.985 }}
                transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                onMouseDown={(e) => e.stopPropagation()}
                className="w-full max-w-[720px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col h-[min(88dvh,820px)]"
            >
                <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                    <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                        <Layers3Icon className="w-3 h-3" />
                    </div>
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">New</span>
                    <div className="h-4 w-px bg-slate-200" />
                    <span className="text-[12.5px] text-slate-900 font-medium">Placement batch</span>
                    <button
                        type="button"
                        onClick={requestClose}
                        aria-label="Close"
                        className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>

                <Stepper step={step} canReach={canReach} goTo={goTo} issueOf={issueOf} />

                <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
                    <AnimatePresence mode="wait" initial={false} custom={direction}>
                        <motion.div
                            key={current.key}
                            custom={direction}
                            variants={paneVariants}
                            initial="enter"
                            animate="center"
                            exit="exit"
                            transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                            className="px-5 py-5 space-y-6"
                        >
                            {current.key === "senders" && (
                                <SendersStep
                                    draft={draft}
                                    patch={patch}
                                    setScopeCampaign={setScopeCampaign}
                                    scopeCampaignName={scopeCampaign.data?.name}
                                    mailboxes={mailboxes}
                                    mailboxesLoading={seeds.isLoading}
                                    preview={senders}
                                    base={base.data ?? base.stale}
                                    error={fieldError("senders")}
                                />
                            )}
                            {current.key === "email" && (
                                <>
                                    <CopySourceFields
                                        value={draft}
                                        patch={patch}
                                        onSource={(v: CopySource) =>
                                            patch({
                                                source: v,
                                                ...(v === "step" && !draft.campaignId && draft.scopeCampaignId ? { campaignId: draft.scopeCampaignId } : {}),
                                                tracking: v === "step" ? "campaign" : draft.tracking === "campaign" ? "off" : draft.tracking,
                                            })
                                        }
                                        campaignName={campaign.data?.name}
                                        steps={steps}
                                    />
                                    <TrackingChoice
                                        value={draft.tracking}
                                        onChange={(v) => patch({ tracking: v })}
                                        source={draft.source}
                                        textOnly={textOnly}
                                        compareHint="Every sender runs two tests to the same seeds, so tests, copies and cost double."
                                    />
                                    <section>
                                        <SectionLabel>Seed panel</SectionLabel>
                                        <PanelChoice
                                            panels={panels}
                                            loading={overview.isLoading}
                                            value={draft.panel}
                                            onChange={(p) => patch({ panel: p })}
                                            usage={overview.data?.usage}
                                        />
                                        {draft.panel !== "workspace" && panel?.available && panelFamilies.length > 1 && (
                                            <FamilyChips families={panelFamilies} value={chosenFamilies} onChange={(families) => patch({ families })} />
                                        )}
                                        {draft.panel === "workspace" && panel?.available && ownSeeds.length > 0 && (
                                            <>
                                                <SeedChooser
                                                    seeds={ownSeeds}
                                                    perTest={overview.data?.seeds_per_test ?? 0}
                                                    value={draft.seedIds}
                                                    onChange={(seedIds) => patch({ seedIds })}
                                                />
                                                <p className="mt-1.5 text-[11px] text-slate-400 leading-relaxed">
                                                    A seed on a sender&apos;s own domain is skipped for that sender.
                                                </p>
                                            </>
                                        )}
                                        {fieldError("email")}
                                    </section>
                                </>
                            )}
                            {current.key === "review" && (
                                <ReviewStep
                                    draft={draft}
                                    patch={patch}
                                    preview={fp ?? full.stale}
                                    loading={full.loading}
                                    consented={consented}
                                    canPay={canPay}
                                    canBuy={canBuy}
                                    spacingSeconds={overview.data?.spacing_seconds ?? 60}
                                    scopeLine={scopeLine(draft, scopeCampaign.data?.name)}
                                    copyLine={
                                        draft.source === "step"
                                            ? `${campaign.data?.name ?? "Campaign"}, ${stepName(steps.emailSteps, draft.stepId)}`
                                            : draft.subject.trim()
                                    }
                                    goTo={(k) => goTo(STEPS.findIndex((s) => s.key === k))}
                                    error={fieldError("review")}
                                />
                            )}
                        </motion.div>
                    </AnimatePresence>
                </div>

                <div className="px-3 min-h-12 py-1.5 sm:py-0 sm:h-12 border-t border-slate-200 flex items-center gap-1.5 shrink-0 bg-slate-50/30">
                    {step > 0 ? (
                        <button
                            type="button"
                            onClick={() => goTo(step - 1)}
                            disabled={pending}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                        >
                            <ChevronLeftIcon className="w-3 h-3" />
                            Back
                        </button>
                    ) : (
                        <button
                            type="button"
                            onClick={requestClose}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                        >
                            Cancel
                        </button>
                    )}

                    <div className="ml-auto flex items-center gap-2 min-w-0">
                        {error?.field === "general" ? (
                            <span className="min-w-0">
                                <InlineError message={error.message} compact />
                            </span>
                        ) : (
                            issue && (
                                <span
                                    role="status"
                                    className={cn(
                                        "text-[11.5px] inline-flex items-center gap-1 min-w-0",
                                        nudged && !waiting ? "text-amber-700" : "text-slate-400",
                                    )}
                                >
                                    {waiting ? <Loader2Icon className="w-3 h-3 shrink-0 animate-spin" /> : <AlertCircleIcon className="w-3 h-3 shrink-0" />}
                                    <span className="truncate" title={issue}>
                                        {issue}
                                    </span>
                                </span>
                            )
                        )}
                        {step < lastStep ? (
                            <button
                                type="button"
                                onClick={next}
                                aria-disabled={!!issue}
                                className={cn(
                                    "h-7 px-3 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors shrink-0",
                                    issue ? "bg-slate-200 text-slate-500 cursor-default" : "bg-sky-600 hover:bg-sky-700 text-white",
                                )}
                            >
                                Continue
                                <ChevronRightIcon className="w-3 h-3" />
                            </button>
                        ) : (
                            <button
                                type="button"
                                onClick={submit}
                                disabled={pending}
                                aria-disabled={!!issue}
                                className={cn(
                                    "h-7 px-3 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors shrink-0",
                                    issue ? "bg-slate-200 text-slate-500 cursor-default" : "bg-sky-600 hover:bg-sky-700 text-white",
                                )}
                            >
                                {pending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <PlayIcon className="w-3.5 h-3.5" />}
                                Start batch
                                {fp && fp.paid_tests > 0 && <span className="opacity-80">· up to {fmtUsdCost(fp.cost_usd)}</span>}
                            </button>
                        )}
                    </div>
                </div>
            </motion.div>
        </motion.div>
    );
}

const paneVariants = {
    enter: (dir: 1 | -1) => ({ x: dir * 28, opacity: 0 }),
    center: { x: 0, opacity: 1 },
    exit: (dir: 1 | -1) => ({ x: dir * -28, opacity: 0 }),
};

function stepName(steps: { id: string; name?: string; subject?: string }[], id: string): string {
    const i = steps.findIndex((s) => s.id === id);
    if (i < 0) return "step";
    return steps[i].name || `step ${i + 1}`;
}

function scopeLine(d: Draft, campaignName?: string): string {
    const parts: string[] = [];
    if (d.scope === "mailboxes") return plural(d.senderIds.length, "chosen mailbox", "chosen mailboxes");
    parts.push(d.scope === "campaign" ? `The senders of ${campaignName ?? "the campaign"}` : "Every sending mailbox");
    if (d.providers.length > 0) parts.push(plural(d.providers.length, "provider"));
    if (d.domains.length > 0) parts.push(d.domains.length === 1 ? d.domains[0] : plural(d.domains.length, "domain"));
    if (d.tagIds.length > 0) parts.push(plural(d.tagIds.length, "tag"));
    if (d.untested && d.untestedDays > 0) parts.push(`not tested in ${d.untestedDays} days`);
    if (d.includeInactive) parts.push("disconnected included");
    switch (d.sampleMode) {
        case "random":
            parts.push(`random ${n(d.sampleCount)}`);
            break;
        case "percent":
            parts.push(`${d.samplePercent}% sample`);
            break;
        case "per_domain":
            parts.push(`${d.sampleCount} per domain`);
            break;
        case "per_provider":
            parts.push(`${d.sampleCount} per provider`);
            break;
    }
    return parts.join(" · ");
}

function Stepper({
    step,
    canReach,
    goTo,
    issueOf,
}: {
    step: number;
    canReach: (s: number) => boolean;
    goTo: (s: number) => void;
    issueOf: (k: StepKey) => string | null;
}) {
    return (
        <div className="px-4 sm:px-5 h-11 border-b border-slate-100 flex items-center shrink-0 bg-slate-50/40">
            {STEPS.map((s, i) => {
                const active = i === step;
                const done = i < step && !issueOf(s.key);
                const reachable = i <= step || canReach(i);
                return (
                    <React.Fragment key={s.key}>
                        <button
                            type="button"
                            onClick={() => goTo(i)}
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
                            <span className="relative flex-1 h-px mx-1 sm:mx-2 bg-slate-200 min-w-3 overflow-hidden">
                                <motion.span
                                    initial={false}
                                    animate={{ scaleX: i < step ? 1 : 0 }}
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

// ---- Senders

const SAMPLE_MODES: { value: PlacementSampleMode; label: string }[] = [
    { value: "all", label: "All" },
    { value: "random", label: "Random" },
    { value: "percent", label: "Percent" },
    { value: "per_domain", label: "Per domain" },
    { value: "per_provider", label: "Per provider" },
];

function chipClass(active: boolean) {
    return cn(
        "h-6 px-2 rounded-md border text-[11px] font-medium inline-flex items-center gap-1 transition-colors",
        active ? "border-sky-200 bg-sky-50 text-sky-700" : "border-slate-200 bg-white text-slate-600 hover:border-slate-300",
    );
}

function SendersStep({
    draft,
    patch,
    setScopeCampaign,
    scopeCampaignName,
    mailboxes,
    mailboxesLoading,
    preview,
    base,
    error,
}: {
    draft: Draft;
    patch: (p: Partial<Draft>) => void;
    setScopeCampaign: (id: string) => void;
    scopeCampaignName?: string;
    mailboxes: PlacementWorkspaceSeed[];
    mailboxesLoading: boolean;
    preview: ReturnType<typeof usePreview>;
    base?: PlacementBatchPreview;
    error: React.ReactNode;
}) {
    const domainSuggestions = React.useMemo(() => {
        const counts = new Map<string, number>();
        for (const m of mailboxes) {
            const d = domainOf(m.email);
            if (d) counts.set(d, (counts.get(d) ?? 0) + 1);
        }
        return [...counts.entries()].sort((a, b) => b[1] - a[1]).map(([d]) => d);
    }, [mailboxes]);

    // The providers the scope has, plus any chosen that dropped out of it.
    const providerChips = React.useMemo(() => {
        const list = (base?.providers ?? []).map((p) => ({ key: p.key, label: p.label, senders: p.senders as number | null }));
        for (const key of draft.providers) {
            if (!list.some((p) => p.key === key)) list.push({ key, label: key, senders: null });
        }
        return list;
    }, [base?.providers, draft.providers]);

    return (
        <>
            <section>
                <SectionLabel>Send from</SectionLabel>
                <OptionSelect<Scope>
                    value={draft.scope}
                    onChange={(v) => patch({ scope: v })}
                    cols={3}
                    aria-label="Senders"
                    options={[
                        { value: "mailboxes", label: "Choose mailboxes", hint: "Pick them from a list." },
                        { value: "campaign", label: "A campaign's senders", hint: "The mailboxes a campaign sends from." },
                        { value: "workspace", label: "All mailboxes", hint: "Every sending mailbox in the workspace." },
                    ]}
                />
            </section>

            {draft.scope === "mailboxes" && (
                <MailboxChooser
                    mailboxes={mailboxes}
                    loading={mailboxesLoading}
                    value={draft.senderIds}
                    onChange={(senderIds) => patch({ senderIds })}
                />
            )}

            {draft.scope === "campaign" && (
                <section className="max-w-sm">
                    <Label>Campaign</Label>
                    <CampaignPicker value={draft.scopeCampaignId} name={scopeCampaignName} onChange={setScopeCampaign} />
                </section>
            )}

            {draft.scope !== "mailboxes" && (
                <section className="space-y-3.5">
                    <SectionLabel>Filters</SectionLabel>
                    <div className="-mt-1">
                        <span className="block mb-1.5 text-[11px] text-slate-500">Providers</span>
                        <div className="flex flex-wrap gap-1">
                            <button
                                type="button"
                                aria-pressed={draft.providers.length === 0}
                                onClick={() => patch({ providers: [] })}
                                className={chipClass(draft.providers.length === 0)}
                            >
                                All
                            </button>
                            {providerChips.map((p) => {
                                const active = draft.providers.includes(p.key);
                                return (
                                    <button
                                        key={p.key}
                                        type="button"
                                        aria-pressed={active}
                                        onClick={() =>
                                            patch({ providers: active ? draft.providers.filter((v) => v !== p.key) : [...draft.providers, p.key] })
                                        }
                                        className={chipClass(active)}
                                    >
                                        {p.label}
                                        {p.senders != null && <span className="font-mono tabular-nums text-slate-400">{n(p.senders)}</span>}
                                    </button>
                                );
                            })}
                        </div>
                    </div>
                    <div>
                        <span className="block mb-1.5 text-[11px] text-slate-500">Sending domains</span>
                        <DomainInput value={draft.domains} onChange={(domains) => patch({ domains })} suggestions={domainSuggestions} />
                    </div>
                    <div>
                        <span className="block mb-1.5 text-[11px] text-slate-500">Mailboxes with any of these tags</span>
                        <TagSelector
                            selected={draft.tagIds}
                            onAdd={(id) => patch({ tagIds: [...draft.tagIds, id] })}
                            onRemove={(id) => patch({ tagIds: draft.tagIds.filter((t) => t !== id) })}
                        />
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                        <label className="inline-flex items-center gap-2 cursor-pointer select-none">
                            <Checkbox tone="slate" checked={draft.untested} onChange={(e) => patch({ untested: e.target.checked })} />
                            <span className="text-[12px] text-slate-700">Only mailboxes not tested in the last</span>
                        </label>
                        <NumberInput
                            value={draft.untestedDays}
                            min={1}
                            max={365}
                            onChange={(v) => patch({ untestedDays: v, untested: true })}
                            suffix="days"
                            className="w-28"
                        />
                    </div>
                    <div className="flex items-center justify-between gap-4">
                        <div className="min-w-0">
                            <div className="text-[12px] text-slate-700">Include disconnected mailboxes</div>
                            <div className="text-[11px] text-slate-400 leading-snug">
                                They wait for a reconnect when retrying, or are skipped.
                            </div>
                        </div>
                        <Toggle
                            value={draft.includeInactive}
                            onChange={(v) => patch({ includeInactive: v })}
                            ariaLabel="Include disconnected mailboxes"
                        />
                    </div>
                </section>
            )}

            {draft.scope !== "mailboxes" && (
                <section>
                    <SectionLabel>Sample</SectionLabel>
                    <div role="radiogroup" aria-label="Sample" className="flex flex-wrap gap-1">
                        {SAMPLE_MODES.map((m) => (
                            <button
                                key={m.value}
                                type="button"
                                role="radio"
                                aria-checked={draft.sampleMode === m.value}
                                onClick={() => patch({ sampleMode: m.value })}
                                className={chipClass(draft.sampleMode === m.value)}
                            >
                                {m.label}
                            </button>
                        ))}
                    </div>
                    {draft.sampleMode !== "all" && (
                        <div className="mt-2.5 flex flex-wrap items-center gap-3">
                            {draft.sampleMode === "percent" ? (
                                <NumberInput
                                    value={draft.samplePercent}
                                    min={1}
                                    max={100}
                                    onChange={(v) => patch({ samplePercent: v })}
                                    suffix="% of the mailboxes"
                                    className="w-48"
                                />
                            ) : (
                                <NumberInput
                                    value={draft.sampleCount}
                                    min={1}
                                    max={100000}
                                    onChange={(v) => patch({ sampleCount: v })}
                                    suffix={
                                        draft.sampleMode === "per_domain"
                                            ? "per domain"
                                            : draft.sampleMode === "per_provider"
                                              ? "per provider"
                                              : "mailboxes"
                                    }
                                    className="w-44"
                                />
                            )}
                            {(draft.sampleMode === "random" || draft.sampleMode === "percent") && (
                                <label className="inline-flex items-center gap-2 cursor-pointer select-none">
                                    <Checkbox tone="slate" checked={draft.spread} onChange={(e) => patch({ spread: e.target.checked })} />
                                    <span className="text-[12px] text-slate-700">Spread across providers</span>
                                </label>
                            )}
                        </div>
                    )}
                </section>
            )}

            <PreviewSummary preview={preview} />
            {error}
        </>
    );
}

function PreviewSummary({ preview }: { preview: ReturnType<typeof usePreview> }) {
    const p = preview.data ?? preview.stale;
    if (preview.error) return <InlineError message={preview.error.message} />;
    if (!p) {
        if (!preview.loading) return null;
        return (
            <div className="rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5 text-[11.5px] text-slate-400 inline-flex items-center gap-1.5">
                <Loader2Icon className="w-3 h-3 animate-spin" />
                Counting the mailboxes…
            </div>
        );
    }
    const spread: string[] = [];
    if (p.providers.length > 1) spread.push(plural(p.providers.length, "provider"));
    if (p.domains > 1) spread.push(plural(p.domains, "domain"));
    const across = spread.length > 0 ? ` across ${spread.join(" and ")}` : "";
    const head =
        p.matched === p.selected
            ? `${plural(p.selected, "mailbox", "mailboxes")}${across}`
            : `${n(p.matched)} match · ${n(p.selected)} selected${across}`;
    return (
        <div className={cn("rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5 transition-opacity", preview.loading && "opacity-60")}>
            <div className="flex items-center gap-1.5 text-[12px] text-slate-700">
                {preview.loading && <Loader2Icon className="w-3 h-3 animate-spin text-slate-400" />}
                <span className="font-medium text-slate-900">{head}</span>
                {p.inactive > 0 && <span className="text-amber-700">· {n(p.inactive)} not connected</span>}
            </div>
            {p.providers.length > 0 && (
                <div className="mt-1.5 flex flex-wrap gap-1">
                    {p.providers.map((pr) => (
                        <span key={pr.key} className="inline-flex items-center gap-1 h-5 px-1.5 rounded bg-white border border-slate-200 text-[10.5px] text-slate-600">
                            {pr.label}
                            <span className="font-mono text-slate-400 tabular-nums">{n(pr.senders)}</span>
                        </span>
                    ))}
                </div>
            )}
        </div>
    );
}

function MailboxChooser({
    mailboxes,
    loading,
    value,
    onChange,
}: {
    mailboxes: PlacementWorkspaceSeed[];
    loading: boolean;
    value: string[];
    onChange: (ids: string[]) => void;
}) {
    const [q, setQ] = React.useState("");
    const needle = q.trim().toLowerCase();
    const shown = React.useMemo(
        () => (needle ? mailboxes.filter((m) => m.email.toLowerCase().includes(needle) || m.label.toLowerCase().includes(needle)) : mailboxes),
        [mailboxes, needle],
    );
    const chosen = React.useMemo(() => new Set(value), [value]);
    const allShown = shown.length > 0 && shown.every((m) => chosen.has(m.email_account_id));
    const toggle = (id: string) => onChange(chosen.has(id) ? value.filter((v) => v !== id) : [...value, id]);
    const toggleShown = () => {
        const ids = new Set(shown.map((m) => m.email_account_id));
        onChange(allShown ? value.filter((v) => !ids.has(v)) : [...value, ...shown.map((m) => m.email_account_id).filter((id) => !chosen.has(id))]);
    };

    return (
        <section className="rounded-md border border-slate-200 overflow-hidden">
            <div className="p-2 border-b border-slate-100 bg-slate-50/60 flex flex-wrap items-center gap-2">
                <div className="flex-1 min-w-[180px]">
                    <SearchInput value={q} onChange={setQ} placeholder="Search mailboxes…" />
                </div>
                <span className="text-[11px] text-slate-500 tabular-nums">{n(value.length)} selected</span>
                {shown.length > 0 && (
                    <button
                        type="button"
                        onClick={toggleShown}
                        className="h-6 px-2 rounded-md text-[11px] font-medium text-sky-700 hover:bg-sky-50 transition-colors"
                    >
                        {allShown ? `Clear ${n(shown.length)} shown` : `Select all ${n(shown.length)} shown`}
                    </button>
                )}
                {value.length > 0 && !allShown && (
                    <button
                        type="button"
                        onClick={() => onChange([])}
                        className="h-6 px-2 rounded-md text-[11px] text-slate-500 hover:text-slate-800 hover:bg-slate-100 transition-colors"
                    >
                        Clear
                    </button>
                )}
            </div>
            <div className="max-h-64 overflow-y-auto divide-y divide-slate-100">
                {loading ? (
                    <div className="px-3 py-3 text-[11.5px] text-slate-400 inline-flex items-center gap-1.5">
                        <Loader2Icon className="w-3 h-3 animate-spin" /> Loading mailboxes…
                    </div>
                ) : shown.length === 0 ? (
                    <div className="px-3 py-3 text-[11.5px] text-slate-400">
                        {mailboxes.length === 0 ? "No mailbox can send a test. Seed inboxes are left out." : "No mailbox matches that."}
                    </div>
                ) : (
                    shown.slice(0, MAILBOX_ROWS_SHOWN).map((m) => (
                        <label key={m.email_account_id} className="h-8 px-3 flex items-center gap-2.5 cursor-pointer hover:bg-slate-50">
                            <Checkbox checked={chosen.has(m.email_account_id)} onChange={() => toggle(m.email_account_id)} />
                            <span className="min-w-0 flex-1 truncate text-[12px] text-slate-800">{m.email}</span>
                            {m.status !== "active" && (
                                <span className="shrink-0 h-4 px-1.5 rounded bg-amber-50 text-amber-700 text-[10px] inline-flex items-center">
                                    Not connected
                                </span>
                            )}
                            <span className="shrink-0 text-[10.5px] text-slate-400">{m.label}</span>
                        </label>
                    ))
                )}
            </div>
            {shown.length > MAILBOX_ROWS_SHOWN && (
                <p className="px-3 py-1.5 border-t border-slate-100 text-[11px] text-slate-400">
                    Showing {n(MAILBOX_ROWS_SHOWN)} of {n(shown.length)}. Search to narrow, or select all shown.
                </p>
            )}
        </section>
    );
}

// Sending domains as chips; typing suggests the workspace's own domains.
function DomainInput({ value, onChange, suggestions }: { value: string[]; onChange: (v: string[]) => void; suggestions: string[] }) {
    const [text, setText] = React.useState("");
    const add = (raw: string) => {
        const d = raw.trim().toLowerCase().replace(/^@/, "");
        if (d && !value.includes(d)) onChange([...value, d]);
        setText("");
    };
    const needle = text.trim().toLowerCase();
    const matches = needle ? suggestions.filter((s) => s.includes(needle) && !value.includes(s)).slice(0, 6) : [];
    return (
        <div>
            <div className="min-h-7 rounded-md border border-slate-200 bg-white px-1.5 py-1 flex flex-wrap items-center gap-1 focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 transition-colors">
                {value.map((d) => (
                    <span key={d} className="inline-flex items-center gap-1 h-5 pl-1.5 pr-1 rounded bg-slate-100 text-[11px] text-slate-700">
                        {d}
                        <button
                            type="button"
                            onClick={() => onChange(value.filter((v) => v !== d))}
                            aria-label={`Remove ${d}`}
                            className="opacity-60 hover:opacity-100"
                        >
                            <XIcon className="w-2.5 h-2.5" />
                        </button>
                    </span>
                ))}
                <input
                    value={text}
                    onChange={(e) => setText(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === "," || e.key === " " || e.key === "Tab") {
                            if (!text.trim()) return;
                            e.preventDefault();
                            add(text);
                        } else if (e.key === "Backspace" && !text && value.length > 0) {
                            onChange(value.slice(0, -1));
                        }
                    }}
                    onBlur={() => text.trim() && add(text)}
                    placeholder={value.length === 0 ? "Every domain. Type one to narrow, e.g. acme.com" : ""}
                    className="flex-1 min-w-[140px] h-5 bg-transparent outline-none text-[16px] md:text-[12px] text-slate-900 placeholder:text-slate-400"
                />
            </div>
            {matches.length > 0 && (
                <div className="mt-1 flex flex-wrap gap-1">
                    {matches.map((s) => (
                        <button
                            key={s}
                            type="button"
                            onMouseDown={(e) => e.preventDefault()}
                            onClick={() => add(s)}
                            className="h-5 px-1.5 rounded border border-dashed border-slate-300 text-[10.5px] text-slate-600 hover:border-slate-400 hover:text-slate-800"
                        >
                            {s}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}

// ---- Review

function ReviewStep({
    draft,
    patch,
    preview: p,
    loading,
    consented,
    canPay,
    canBuy,
    spacingSeconds,
    scopeLine: senders,
    copyLine,
    goTo,
    error,
}: {
    draft: Draft;
    patch: (p: Partial<Draft>) => void;
    preview?: PlacementBatchPreview;
    loading: boolean;
    consented: boolean;
    canPay: boolean;
    canBuy: boolean;
    spacingSeconds: number;
    scopeLine: string;
    copyLine: string;
    goTo: (k: StepKey) => void;
    error: React.ReactNode;
}) {
    const summary: { label: string; value: string; step: StepKey }[] = [
        { label: "Senders", value: senders, step: "senders" },
        { label: "Email", value: copyLine || "(no subject)", step: "email" },
        {
            label: "Panel",
            value: `${PANEL_LABEL[draft.panel]}${draft.tracking === "compare" ? ", with and without tracking" : draft.tracking === "on" ? ", tracked" : draft.tracking === "off" ? ", untracked" : ""}`,
            step: "email",
        },
    ];

    return (
        <>
            <dl className="rounded-md border border-slate-200 divide-y divide-slate-100">
                {summary.map((row) => (
                    <div key={row.label} className="px-3 py-2 flex items-center gap-3">
                        <dt className="w-16 shrink-0 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{row.label}</dt>
                        <dd className="min-w-0 flex-1 truncate text-[12px] text-slate-800" title={row.value}>
                            {row.value}
                        </dd>
                        <button
                            type="button"
                            onClick={() => goTo(row.step)}
                            className="shrink-0 h-6 px-1.5 rounded text-[11px] text-sky-700 hover:bg-sky-50 transition-colors"
                        >
                            Change
                        </button>
                    </div>
                ))}
            </dl>

            {!p ? (
                <div className="h-24 rounded-md bg-slate-50 animate-pulse" />
            ) : (
                <div className={cn("space-y-3 transition-opacity", loading && "opacity-60")}>
                    <Workload preview={p} spacingSeconds={spacingSeconds} />
                    {p.metered && p.paid_tests > 0 && (
                        <div className="rounded-md border border-amber-200 bg-amber-50/60 px-3 py-2.5 text-[11.5px] leading-relaxed text-slate-700">
                            <p>
                                {p.free_tests > 0
                                    ? `This month's free tests cover ${n(p.free_tests)} of the ${n(p.tests)} tests. The other ${n(p.paid_tests)}`
                                    : `This month's free tests are used up, so all ${n(p.paid_tests)} tests`}{" "}
                                cost up to <b className="font-medium text-slate-900">{fmtUsdCost(p.cost_usd)}</b>
                                {p.usage.price_per_test_usd > 0 && ` (${fmtUsdCost(p.usage.price_per_test_usd)} each)`}, paid from the workspace balance.
                                {p.usage.balance_usd != null && ` The balance is ${fmtUsd(p.usage.balance_usd)}.`} A test that delivers no copy is refunded.
                            </p>
                            {p.usage.price_per_test_usd === 0 ? null : canPay ? (
                                <label className="mt-2 flex items-center gap-2 cursor-pointer select-none">
                                    <Checkbox
                                        tone="slate"
                                        checked={consented}
                                        onChange={(e) => patch({ payConsent: e.target.checked ? p.cost_usd : null })}
                                    />
                                    <span className="text-[12px] text-slate-900">Pay up to {fmtUsdCost(p.cost_usd)} for this batch</span>
                                </label>
                            ) : canBuy ? (
                                <Link
                                    to="/app/settings/billing/{-$tab}"
                                    params={{ tab: "balance" }}
                                    target="_blank"
                                    rel="noopener"
                                    className="mt-2 inline-flex text-[12px] font-medium text-sky-700 hover:underline"
                                >
                                    Add money in a new tab
                                </Link>
                            ) : (
                                <p className="mt-2 text-[12px] text-slate-600">Ask someone with the Manage billing permission to add money.</p>
                            )}
                        </div>
                    )}
                    {p.inactive > 0 && (
                        <p className="flex items-start gap-1.5 text-[11.5px] leading-snug text-amber-700">
                            <AlertTriangleIcon className="w-3.5 h-3.5 shrink-0 mt-px" />
                            <span>
                                {plural(p.inactive, "selected mailbox is", "selected mailboxes are")} not connected right now.{" "}
                                {draft.onUnavailable === "defer"
                                    ? `They are tried again as they reconnect, for up to ${BATCH_RETRY_DAYS} days.`
                                    : "They will be skipped."}
                            </span>
                        </p>
                    )}
                </div>
            )}
            {error}

            <section>
                <SectionLabel>When a sender cannot send</SectionLabel>
                <OptionSelect<PlacementUnavailable>
                    value={draft.onUnavailable}
                    onChange={(v) => patch({ onUnavailable: v })}
                    aria-label="When a sender cannot send"
                    options={[
                        {
                            value: "defer",
                            label: "Retry until every sender has been tested",
                            hint: `A mailbox that is out of today's sending, busy or disconnected when its turn comes is tried again later, for up to ${BATCH_RETRY_DAYS} days.`,
                        },
                        {
                            value: "skip",
                            label: "Skip senders that can't send right away",
                            hint: "The batch finishes sooner. Skipped mailboxes are listed with the reason.",
                        },
                    ]}
                />
            </section>
        </>
    );
}

// A batch always sends at the spaced pace.
function Workload({ preview: p, spacingSeconds }: { preview: PlacementBatchPreview; spacingSeconds: number }) {
    const perSender = p.seeds_per_test * p.variants * spacingSeconds;
    const atOnce = Math.max(1, Math.min(p.concurrency || 1, p.selected));
    const total = Math.ceil(p.selected / atOnce) * perSender;
    const freeLeft = p.usage.limit != null ? Math.max(0, p.usage.limit - p.usage.used) : null;
    return (
        <div className="rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5 text-[11.5px] leading-relaxed text-slate-600 space-y-1">
            <p>
                <b className="font-medium text-slate-900">{plural(p.selected, "mailbox", "mailboxes")}</b>
                {p.variants > 1 ? ", two tests each (with and without tracking)" : ""}, {plural(p.seeds_per_test, "seed")} per test: up to{" "}
                <b className="font-medium text-slate-900">{plural(p.max_sends, "copy", "copies")}</b>
                {p.variants > 1 ? ` in ${plural(p.tests, "test")}` : ""}.
            </p>
            <p>
                {p.selected <= atOnce
                    ? `They all send at once, each sending its copies about ${spacingSeconds} seconds apart for ${fmtDuration(perSender)}.`
                    : `About ${n(atOnce)} send at a time, each sending its copies about ${spacingSeconds} seconds apart for ${fmtDuration(perSender)}, so sending takes ${fmtDuration(total)} or longer.`}{" "}
                Every copy counts against its mailbox&apos;s daily limit, and a mailbox with less left today sends fewer. A copy
                not seen within 2 hours counts as never arrived.
            </p>
            {!p.metered ? (
                <p>Not counted toward your monthly tests.</p>
            ) : p.paid_tests === 0 && freeLeft != null ? (
                <p>
                    Uses {n(p.tests)} of the {plural(freeLeft, "free test")} left this month.
                </p>
            ) : null}
        </div>
    );
}
