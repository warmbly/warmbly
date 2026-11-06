// Starts an inbox placement test: one mailbox sends a campaign step or custom
// copy to a panel of seed inboxes, one copy at a time, and the detail page
// shows where each landed. Every refusal the server can give is shown beside
// the part of the form it is about.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { Link, useNavigate } from "@tanstack/react-router";
import { Loader2Icon, MailCheckIcon, MailIcon, PlayIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { Label, SearchInput } from "@/components/ui/field";
import { Checkbox } from "@/components/ui/checkbox";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import useCampaignSenders from "@/lib/api/hooks/app/campaigns/useCampaignSenders";
import { htmlToPlain } from "@/components/app/campaigns/sequences/emailPreview";
import { useCreatePlacementTest, usePlacementOverview, usePlacementSeeds } from "@/lib/api/hooks/app/placement/usePlacement";
import type {
    CreatePlacementTestRequest,
    PlacementPace,
    PlacementPanel,
    PlacementTracking,
} from "@/lib/api/models/app/placement/Placement";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type { AppError } from "@/lib/api/client/normalizeError";
import { cn } from "@/lib/utils";
import { fmtUsd, fmtUsdCost } from "@/lib/money";
import { placementErrorMessage, seedBlocker, testCost, type PlacementErrorField } from "./placementTests";
import { CopySourceFields, FamilyChips, InlineError, PaceChoice, PanelChoice, TrackingChoice } from "./PlacementFormParts";
import {
    QUICK_SPACING_SECONDS,
    newIdempotencyKey as newKey,
    useCampaignEmailSteps,
    type CopySource as Source,
} from "./placementCopy";
import SeedChooser from "./SeedChooser";

interface Draft {
    senderId: string;
    source: Source;
    campaignId: string;
    stepId: string;
    subject: string;
    bodyHtml: string;
    bodyPlain: string;
    bodyCode: boolean;
    contact: Contact | null;
    tracking: PlacementTracking;
    panel: PlacementPanel;
    // Own seed inboxes to send to; empty means the usual pick.
    seedIds: string[];
    // Provider families on a shared panel; empty means every provider.
    families: string[];
    pace: PlacementPace;
    // The dollar price the user agreed to pay; a different price needs asking again.
    payConsent: number | null;
}

export interface NewPlacementTestPrefill {
    campaignId?: string;
    stepId?: string;
}

function emptyDraft(prefill?: NewPlacementTestPrefill): Draft {
    const fromStep = !!prefill?.campaignId;
    return {
        senderId: "",
        source: fromStep ? "step" : "custom",
        campaignId: prefill?.campaignId ?? "",
        stepId: prefill?.stepId ?? "",
        subject: "",
        bodyHtml: "",
        bodyPlain: "",
        bodyCode: false,
        contact: null,
        tracking: fromStep ? "campaign" : "off",
        panel: "instance",
        seedIds: [],
        families: [],
        pace: "spaced",
        payConsent: null,
    };
}

// What the user typed or picked, for the discard prompt. The sender and panel
// defaults the dialog fills in itself do not count.
function draftKey(d: Draft): string {
    return JSON.stringify([d.source, d.campaignId, d.stepId, d.subject, d.bodyHtml, d.contact?.id ?? "", d.tracking, d.seedIds, d.families, d.pace]);
}

export default function NewPlacementTestDialog({
    open,
    onClose,
    prefill,
}: {
    open: boolean;
    onClose: () => void;
    prefill?: NewPlacementTestPrefill;
}) {
    if (typeof document === "undefined") return null;
    return createPortal(
        <AnimatePresence>{open && <DialogBody key="placement-dialog" onClose={onClose} prefill={prefill} />}</AnimatePresence>,
        document.body,
    );
}

function DialogBody({ onClose, prefill }: { onClose: () => void; prefill?: NewPlacementTestPrefill }) {
    const navigate = useNavigate();
    const confirm = useConfirm();
    const overview = usePlacementOverview();
    const seeds = usePlacementSeeds();
    const create = useCreatePlacementTest();
    const canBuy = usePermission("MANAGE_BILLING");

    const [draft, setDraft] = React.useState<Draft>(() => emptyDraft(prefill));
    const initialKey = React.useRef(draftKey(emptyDraft(prefill)));
    const [error, setError] = React.useState<{ field: PlacementErrorField; message: string } | null>(null);
    const [nudged, setNudged] = React.useState(false);

    // A retried submit of the same form lands on the tests the first one
    // started; any edit makes it a new request and clears the last refusal.
    const idemKey = React.useRef(newKey());
    const patch = (p: Partial<Draft>) => {
        idemKey.current = newKey();
        setError(null);
        setDraft((d) => ({ ...d, ...p }));
    };

    const campaign = useCampaign(draft.source === "step" ? draft.campaignId : "");
    const campaignSenders = useCampaignSenders(draft.campaignId, draft.source === "step" && !!draft.campaignId);
    const steps = useCampaignEmailSteps(draft.campaignId, draft.source === "step");
    const emailSteps = steps.emailSteps;

    // Senders: connected mailboxes that are not seeds, the campaign's own first.
    const inCampaign = React.useMemo(
        () => new Set((campaignSenders.data ?? []).filter((s) => s.enabled).map((s) => s.email_account_id)),
        [campaignSenders.data],
    );
    const senders = React.useMemo(() => {
        const list = (seeds.data ?? []).filter((m) => m.status === "active" && !m.seed);
        return [...list].sort((a, b) => Number(inCampaign.has(b.email_account_id)) - Number(inCampaign.has(a.email_account_id)));
    }, [seeds.data, inCampaign]);
    const sender = senders.find((s) => s.email_account_id === draft.senderId) ?? null;

    // Default the sender to the campaign's first usable mailbox, else the first.
    React.useEffect(() => {
        if (sender || senders.length === 0) return;
        const pick = senders.find((s) => inCampaign.has(s.email_account_id)) ?? senders[0];
        setDraft((d) => ({ ...d, senderId: pick.email_account_id }));
    }, [sender, senders, inCampaign]);

    // Default the step to the campaign's first email step.
    React.useEffect(() => {
        if (draft.source !== "step" || !draft.campaignId || emailSteps.length === 0) return;
        if (emailSteps.some((s) => s.id === draft.stepId)) return;
        setDraft((d) => ({ ...d, stepId: emailSteps[0].id }));
    }, [draft.source, draft.campaignId, draft.stepId, emailSteps]);

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

    const compare = draft.tracking === "compare";
    const seedsPerTest = overview.data?.seeds_per_test ?? 0;
    const ownSeeds = React.useMemo(() => (seeds.data ?? []).filter((m) => m.seed), [seeds.data]);
    const chosenSeeds =
        draft.panel === "workspace"
            ? ownSeeds.filter((m) => draft.seedIds.includes(m.email_account_id) && !seedBlocker(m, sender?.email))
            : [];
    const panelFamilies = React.useMemo(() => panel?.families ?? [], [panel]);
    const chosenFamilies =
        draft.panel === "workspace" ? [] : draft.families.filter((f) => panelFamilies.some((p) => p.family === f));
    const familySeeds = chosenFamilies.length
        ? panelFamilies.filter((p) => chosenFamilies.includes(p.family)).reduce((n, p) => n + p.seeds, 0)
        : (panel?.seeds ?? 0);
    // Seeds chosen by hand all get a copy; otherwise a test takes up to the instance's cap.
    const perTest = !panel ? 0 : chosenSeeds.length > 0 ? chosenSeeds.length : Math.min(familySeeds, seedsPerTest || familySeeds);
    const copies = perTest * (compare ? 2 : 1);
    const baseSpacing = overview.data?.spacing_seconds ?? 60;
    const spacing = draft.pace === "quick" ? Math.min(baseSpacing, QUICK_SPACING_SECONDS) : baseSpacing;
    const sendSeconds = copies * spacing;
    const sendTime = sendSeconds < 90 ? "under 2 minutes" : `about ${Math.round(sendSeconds / 60)} minutes`;
    const usage = overview.data?.usage;
    const testsNeeded = compare ? 2 : 1;
    // The instance panel's free tests run out into paid ones; the cloud's allowance is the cloud's.
    const cost = testCost(usage, draft.panel === "instance" && !!panel?.metered, testsNeeded);
    const consented = cost.paid > 0 && draft.payConsent === cost.usd;
    const freeLeft = usage?.limit != null ? Math.max(0, usage.limit - usage.used) : null;
    const overQuota =
        draft.panel === "cloud" && !!panel?.metered && usage?.limit != null && usage.used + testsNeeded > usage.limit;

    // Why the form cannot be sent yet, shown in the footer instead of a
    // silently disabled button.
    const issue: string | null = !sender
        ? senders.length === 0 && !seeds.isLoading
            ? "Connect a mailbox first. Seed inboxes cannot send a test."
            : "Pick the mailbox to send from."
        : draft.source === "step" && !draft.campaignId
          ? "Pick a campaign."
          : draft.source === "step" && !draft.stepId
            ? emailSteps.length === 0 && !steps.isLoading
                ? "This campaign has no email step to test."
                : "Pick a step."
            : draft.source === "custom" && !draft.subject.trim()
              ? "Write a subject."
              : draft.source === "custom" && !(draft.bodyPlain.trim() || (draft.bodyCode && draft.bodyHtml.trim()))
                ? "Write the email body."
                : !panel || !panel.available
                  ? "Pick a panel that can run a test."
                  : panel.seeds === 0
                    ? panel.panel === "workspace"
                        ? "You have no seed inboxes yet. Mark one on the Seed inboxes tab of Placement tests."
                        : "This panel has no seed inboxes yet."
                    : draft.panel === "workspace" && draft.seedIds.length > 0 && chosenSeeds.length === 0
                      ? "None of the seed inboxes you chose can take a test from this sender. Choose others, or clear the choice."
                    : chosenFamilies.length > 0 && familySeeds === 0
                      ? "This panel has no seeds at the providers you chose."
                    : cost.paid > 0 && !cost.payable
                      ? "This month's free tests are used up. Your own seed inboxes are never counted."
                    : cost.paid > 0 && !cost.canPay
                      ? `This test costs ${fmtUsdCost(cost.usd)} and the workspace balance is ${fmtUsd(usage?.balance_usd ?? 0)}. Add money under Settings > Billing.`
                    : cost.paid > 0 && !consented
                      ? `This month's free tests are used up. Tick the box to pay ${fmtUsdCost(cost.usd)} for this test.`
                    : overQuota
                      ? `This month's tests are used up${compare && usage && usage.limit != null && usage.used < usage.limit ? " (a comparison counts as 2)" : ""}. Your own seed inboxes are never counted.`
                      : null;

    const dirty = draftKey(draft) !== initialKey.current;
    const pending = create.isPending;

    const requestClose = React.useCallback(() => {
        if (pending) return;
        if (dirty) {
            confirm.show("Discard this placement test?", async () => onClose());
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

    async function submit() {
        if (pending) return;
        if (issue) {
            setNudged(true);
            return;
        }
        const body: CreatePlacementTestRequest = {
            sender_account_id: draft.senderId,
            tracking: draft.tracking,
            panel: draft.panel,
            ...(draft.contact ? { contact_id: draft.contact.id } : {}),
            ...(chosenSeeds.length > 0 ? { seed_ids: chosenSeeds.map((m) => m.email_account_id) } : {}),
            ...(chosenFamilies.length > 0 ? { families: chosenFamilies } : {}),
            ...(draft.pace !== "spaced" ? { pace: draft.pace } : {}),
            ...(consented ? { max_usd: cost.usd } : {}),
        };
        if (draft.source === "step") {
            body.campaign_id = draft.campaignId;
            body.sequence_id = draft.stepId;
        } else {
            body.subject = draft.subject.trim();
            body.body_html = draft.bodyHtml;
            body.body_plain = draft.bodyCode ? htmlToPlain(draft.bodyHtml) : draft.bodyPlain;
        }
        try {
            const tests = await create.mutateAsync({ body, idempotencyKey: idemKey.current });
            toast.success(tests.length > 1 ? "Comparison started." : "Placement test started.");
            onClose();
            if (tests[0]) navigate({ to: "/app/placement/$id", params: { id: tests[0].id } });
        } catch (err) {
            setError(placementErrorMessage(err as AppError, { resetsOn: usage?.period_end, panel: draft.panel, chosen: chosenSeeds.length > 0 }));
        }
    }

    const fieldError = (f: PlacementErrorField) =>
        error?.field === f ? <InlineError message={error.message} /> : null;

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
                aria-label="New placement test"
                initial={{ y: 8, opacity: 0, scale: 0.985 }}
                animate={{ y: 0, opacity: 1, scale: 1 }}
                exit={{ y: 8, opacity: 0, scale: 0.985 }}
                transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                onMouseDown={(e) => e.stopPropagation()}
                className="w-full max-w-[680px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[88dvh]"
            >
                <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                    <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                        <MailCheckIcon className="w-3 h-3" />
                    </div>
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">New</span>
                    <div className="h-4 w-px bg-slate-200" />
                    <span className="text-[12.5px] text-slate-900 font-medium">Placement test</span>
                    <button
                        type="button"
                        onClick={requestClose}
                        aria-label="Close"
                        className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>

                <div className="flex-1 min-h-0 overflow-y-auto px-5 py-5 space-y-6">
                    {/* Sender */}
                    <section>
                        <Label>Send from</Label>
                        <SenderPicker
                            senders={senders}
                            inCampaign={inCampaign}
                            value={draft.senderId}
                            loading={seeds.isLoading}
                            onChange={(id) => patch({ senderId: id })}
                        />
                        <p className="mt-1.5 text-[11px] text-slate-400 leading-relaxed">
                            Only connected mailboxes can send. Seed inboxes are left out.
                        </p>
                        {fieldError("sender")}
                    </section>

                    {/* What to test */}
                    <CopySourceFields
                        value={draft}
                        patch={patch}
                        onSource={(v) =>
                            patch({
                                source: v,
                                tracking: v === "step" ? "campaign" : draft.tracking === "campaign" ? "off" : draft.tracking,
                            })
                        }
                        campaignName={campaign.data?.name}
                        steps={steps}
                        error={fieldError("source")}
                    />

                    {/* Tracking */}
                    <TrackingChoice
                        value={draft.tracking}
                        onChange={(v) => patch({ tracking: v })}
                        source={draft.source}
                        textOnly={textOnly}
                        error={fieldError("tracking")}
                    />

                    {/* Pace */}
                    <PaceChoice value={draft.pace} onChange={(v) => patch({ pace: v })} />

                    {/* Panel */}
                    <section>
                        <span className="block mb-2 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Seed panel</span>
                        <PanelChoice
                            panels={panels}
                            loading={overview.isLoading}
                            value={draft.panel}
                            onChange={(p) => patch({ panel: p })}
                            usage={usage}
                        />
                        {draft.panel !== "workspace" && panel?.available && panelFamilies.length > 1 && (
                            <FamilyChips
                                families={panelFamilies}
                                value={chosenFamilies}
                                onChange={(families) => patch({ families })}
                            />
                        )}
                        {draft.panel === "workspace" && panel?.available && ownSeeds.length > 0 && (
                            <SeedChooser
                                seeds={ownSeeds}
                                senderEmail={sender?.email}
                                perTest={seedsPerTest}
                                value={draft.seedIds}
                                onChange={(seedIds) => patch({ seedIds })}
                            />
                        )}
                        {fieldError("panel")}
                    </section>

                    {/* Cost */}
                    {sender && panel?.available && copies > 0 && (
                        <div className="rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5 text-[11.5px] leading-relaxed text-slate-600">
                            Sends up to <b className="font-medium text-slate-900">{copies}</b> email{copies === 1 ? "" : "s"} from{" "}
                            <b className="font-medium text-slate-900">{sender.email}</b>, one every ~{spacing} seconds, counted
                            against its daily limit, and fewer when the mailbox has less than that left today. Sending takes {sendTime}; a copy
                            not seen within 2 hours counts as never arrived. Seeds on the sender&apos;s own domain are skipped.
                            {draft.panel === "instance" && freeLeft != null && cost.paid === 0 && (
                                <>
                                    {" "}
                                    Uses {testsNeeded === 1 ? "one" : "two"} of the {freeLeft} free test{freeLeft === 1 ? "" : "s"} left this month.
                                </>
                            )}
                        </div>
                    )}
                    {sender && panel?.available && cost.paid > 0 && cost.payable && (
                        <div className="rounded-md border border-amber-200 bg-amber-50/60 px-3 py-2.5 text-[11.5px] leading-relaxed text-slate-700">
                            <p>
                                {cost.paid < testsNeeded
                                    ? "One free test is left this month, so the second half of this comparison"
                                    : "This month's free tests are used up, so this test"}{" "}
                                costs <b className="font-medium text-slate-900">{fmtUsdCost(cost.usd)}</b>, paid from the workspace balance.
                                {usage?.balance_usd != null && ` The balance is ${fmtUsd(usage.balance_usd)}.`} A test that delivers no copy is refunded.
                            </p>
                            {cost.canPay ? (
                                <label className="mt-2 flex items-center gap-2 cursor-pointer select-none">
                                    <Checkbox
                                        tone="slate"
                                        checked={consented}
                                        onChange={(e) => patch({ payConsent: e.target.checked ? cost.usd : null })}
                                    />
                                    <span className="text-[12px] text-slate-900">Pay {fmtUsdCost(cost.usd)} for this test</span>
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
                </div>

                <div className="shrink-0 border-t border-slate-200 px-4 py-3 flex items-center gap-2">
                    <div className="min-w-0 flex-1">
                        {error?.field === "general" ? (
                            <InlineError message={error.message} compact />
                        ) : nudged && issue ? (
                            <InlineError message={issue} compact />
                        ) : null}
                    </div>
                    <button
                        type="button"
                        onClick={requestClose}
                        disabled={pending}
                        className="h-7 px-3 text-[12px] font-medium text-slate-600 hover:text-slate-900 border border-slate-200 hover:border-slate-300 rounded-md transition-colors disabled:opacity-60"
                    >
                        Cancel
                    </button>
                    <button
                        type="button"
                        onClick={submit}
                        disabled={pending}
                        title={issue ?? undefined}
                        className={cn(
                            "h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60",
                            issue && "opacity-60",
                        )}
                    >
                        {pending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <PlayIcon className="w-3.5 h-3.5" />}
                        {compare ? "Start comparison" : "Start test"}
                        {cost.paid > 0 && cost.payable && <span className="opacity-80">· {fmtUsdCost(cost.usd)}</span>}
                    </button>
                </div>
            </motion.div>
        </motion.div>
    );
}

function SenderPicker({
    senders,
    inCampaign,
    value,
    loading,
    onChange,
}: {
    senders: { email_account_id: string; email: string; label: string }[];
    inCampaign: Set<string>;
    value: string;
    loading: boolean;
    onChange: (id: string) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const [q, setQ] = React.useState("");
    const current = senders.find((s) => s.email_account_id === value);
    const needle = q.trim().toLowerCase();
    const shown = needle ? senders.filter((s) => s.email.toLowerCase().includes(needle)) : senders;
    const campaignRows = shown.filter((s) => inCampaign.has(s.email_account_id));
    const otherRows = shown.filter((s) => !inCampaign.has(s.email_account_id));

    const row = (s: (typeof senders)[number]) => (
        <PopoverMenuItem key={s.email_account_id} selected={s.email_account_id === value} onSelect={() => onChange(s.email_account_id)}>
            <span className="text-slate-800">{s.email}</span>
            {s.label && <span className="ml-1.5 text-[11px] text-slate-400">{s.label}</span>}
        </PopoverMenuItem>
    );

    return (
        <PopoverMenu open={open} onOpenChange={setOpen}>
            <PopoverMenuTrigger asChild>
                <SelectButton
                    icon={<MailIcon className="w-3.5 h-3.5" />}
                    label={current ? current.email : loading ? "Loading mailboxes…" : "Pick a mailbox"}
                    className="w-full [&>span:nth-child(2)]:max-w-none [&>span:nth-child(2)]:flex-1 [&>span:nth-child(2)]:text-left"
                />
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={300} matchTriggerWidth className="p-1 max-h-80">
                <div className="p-1.5">
                    <SearchInput value={q} onChange={setQ} placeholder="Search mailboxes…" autoFocus />
                </div>
                {shown.length === 0 ? (
                    <div className="px-3 py-2 text-[11.5px] text-slate-400">
                        {senders.length === 0 ? "No connected mailbox can send a test." : "No mailbox matches that."}
                    </div>
                ) : (
                    <>
                        {campaignRows.length > 0 && (
                            <>
                                <PopoverMenuLabel>In this campaign</PopoverMenuLabel>
                                {campaignRows.map(row)}
                                {otherRows.length > 0 && <PopoverMenuSeparator />}
                            </>
                        )}
                        {otherRows.length > 0 && (
                            <>
                                {campaignRows.length > 0 && <PopoverMenuLabel>Other mailboxes</PopoverMenuLabel>}
                                {otherRows.map(row)}
                            </>
                        )}
                    </>
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
