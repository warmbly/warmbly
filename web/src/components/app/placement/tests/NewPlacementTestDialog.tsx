// Starts an inbox placement test: one mailbox sends a campaign step or custom
// copy to a panel of seed inboxes, one copy at a time, and the detail page
// shows where each landed. Every refusal the server can give is shown beside
// the part of the form it is about.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
    AlertCircleIcon,
    Loader2Icon,
    MailCheckIcon,
    MailIcon,
    MegaphoneIcon,
    PlayIcon,
    UserRoundIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { Label, SearchInput, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import { SelectMenu } from "@/components/ui/select-menu";
import { OptionSelect, Segmented } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import RichTextEditor from "@/components/app/campaigns/sequences/RichTextEditor";
import { VARIABLES, htmlToPlain } from "@/components/app/campaigns/sequences/emailPreview";
import { contactLabel } from "@/components/app/campaigns/sequences/previewContext";
import { LINK_VARIABLES } from "@/lib/templateVars";
import { useConfirm } from "@/hooks/context/confirm";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import useCampaignSenders from "@/lib/api/hooks/app/campaigns/useCampaignSenders";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import getSequences from "@/lib/api/client/app/campaigns/sequences/getSequences";
import { useCreatePlacementTest, usePlacementOverview, usePlacementSeeds } from "@/lib/api/hooks/app/placement/usePlacement";
import {
    PANEL_LABEL,
    type CreatePlacementTestRequest,
    type PlacementPanel,
    type PlacementTracking,
} from "@/lib/api/models/app/placement/Placement";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type { AppError } from "@/lib/api/client/normalizeError";
import { cn } from "@/lib/utils";
import { placementErrorMessage, type PlacementErrorField } from "./placementTests";

type Source = "step" | "custom";

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
    };
}

// What the user typed or picked, for the discard prompt. The sender and panel
// defaults the dialog fills in itself do not count.
function draftKey(d: Draft): string {
    return JSON.stringify([d.source, d.campaignId, d.stepId, d.subject, d.bodyHtml, d.contact?.id ?? "", d.tracking]);
}

function newKey(): string {
    return typeof crypto !== "undefined" && "randomUUID" in crypto
        ? crypto.randomUUID()
        : `${Date.now()}-${Math.random().toString(36).slice(2)}`;
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
    const steps = useQuery({
        queryKey: ["campaigns", draft.campaignId, "sequences"],
        queryFn: () => getSequences(draft.campaignId),
        enabled: draft.source === "step" && !!draft.campaignId,
    });
    const emailSteps = React.useMemo(
        () => (steps.data ?? []).filter((s) => (s.kind ?? "email") === "email"),
        [steps.data],
    );

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
    const perTest = panel ? Math.min(panel.seeds, seedsPerTest || panel.seeds) : 0;
    const copies = perTest * (compare ? 2 : 1);
    const spacing = overview.data?.spacing_seconds ?? 60;
    const minutes = Math.max(1, Math.round((copies * spacing) / 60));
    const usage = overview.data?.usage;
    const testsNeeded = compare ? 2 : 1;
    const overQuota =
        !!panel?.metered && usage?.limit != null && usage.used + testsNeeded > usage.limit;

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
            if (tests[0]) navigate(`/app/placement/${tests[0].id}`);
        } catch (err) {
            setError(placementErrorMessage(err as AppError, { resetsOn: usage?.period_end, panel: draft.panel }));
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
                    <section className="space-y-3">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">What to test</span>
                            <Segmented<Source>
                                value={draft.source}
                                onChange={(v) =>
                                    patch({
                                        source: v,
                                        tracking: v === "step" ? "campaign" : draft.tracking === "campaign" ? "off" : draft.tracking,
                                    })
                                }
                                options={[
                                    { value: "step", label: "Campaign step" },
                                    { value: "custom", label: "Custom copy" },
                                ]}
                            />
                        </div>

                        {draft.source === "step" ? (
                            <div className="grid gap-3 sm:grid-cols-2">
                                <div className="min-w-0">
                                    <Label>Campaign</Label>
                                    <CampaignPicker
                                        value={draft.campaignId}
                                        name={campaign.data?.name}
                                        onChange={(id) => patch({ campaignId: id, stepId: "", contact: null })}
                                    />
                                </div>
                                <div className="min-w-0">
                                    <Label>Step</Label>
                                    <SelectMenu
                                        value={draft.stepId}
                                        onChange={(v) => patch({ stepId: v })}
                                        disabled={!draft.campaignId || steps.isLoading}
                                        fullWidth
                                        placeholder={
                                            !draft.campaignId
                                                ? "Pick a campaign first"
                                                : steps.isLoading
                                                  ? "Loading steps…"
                                                  : emailSteps.length === 0
                                                    ? "No email steps"
                                                    : "Pick a step"
                                        }
                                        options={emailSteps.map((s, i) => ({
                                            value: s.id,
                                            label: `${s.name || `Step ${i + 1}`}${s.subject ? `: ${s.subject}` : ""}`,
                                        }))}
                                        aria-label="Step"
                                    />
                                </div>
                                <p className="sm:col-span-2 text-[11px] text-slate-400 leading-relaxed">
                                    The saved step is rendered exactly as the campaign sends it: merge fields, spintax,
                                    signature, opt-out footer and unsubscribe header.
                                </p>
                            </div>
                        ) : (
                            <div className="space-y-3">
                                <div>
                                    <Label>Subject</Label>
                                    <TextInput
                                        value={draft.subject}
                                        onChange={(v) => patch({ subject: v })}
                                        placeholder="Quick question, {{.FirstName}}"
                                    />
                                </div>
                                <div>
                                    <Label>Body</Label>
                                    <RichTextEditor
                                        html={draft.bodyHtml}
                                        onChange={(html) =>
                                            patch({ bodyHtml: html, bodyPlain: draft.bodyCode ? "" : htmlToPlain(html) })
                                        }
                                        code={draft.bodyCode}
                                        onCodeChange={(c) => patch({ bodyCode: c })}
                                        variables={VARIABLES}
                                        links={LINK_VARIABLES}
                                        placeholder="Hi {{.FirstName}}, …"
                                    />
                                </div>
                            </div>
                        )}
                        {fieldError("source")}

                        <div>
                            <Label>Render for</Label>
                            <ContactPicker
                                campaignId={draft.source === "step" ? draft.campaignId : ""}
                                value={draft.contact}
                                onChange={(c) => patch({ contact: c })}
                            />
                            <p className="mt-1.5 text-[11px] text-slate-400 leading-relaxed">
                                Fills the merge fields. Nobody but the seed inboxes receives the copies.
                            </p>
                        </div>
                    </section>

                    {/* Tracking */}
                    <section>
                        <span className="block mb-2 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Tracking</span>
                        <OptionSelect<PlacementTracking>
                            value={draft.tracking}
                            onChange={(v) => patch({ tracking: v })}
                            cols={2}
                            aria-label="Tracking"
                            options={[
                                ...(draft.source === "step"
                                    ? [{ value: "campaign" as const, label: "As the campaign", hint: "Uses the campaign's open and click tracking." }]
                                    : []),
                                ...(textOnly
                                    ? []
                                    : [{ value: "on" as const, label: "On", hint: "Open pixel and tracked links." }]),
                                { value: "off" as const, label: "Off", hint: "No pixel, links left as written." },
                                ...(textOnly
                                    ? []
                                    : [
                                          {
                                              value: "compare" as const,
                                              label: "Compare with and without",
                                              hint: "Two tests to the same seeds. Counts as 2 tests.",
                                          },
                                      ]),
                            ]}
                        />
                        {textOnly && (
                            <p className="mt-1.5 text-[11px] text-slate-400">This campaign sends plain text, which carries no tracking.</p>
                        )}
                        {fieldError("tracking")}
                    </section>

                    {/* Panel */}
                    <section>
                        <span className="block mb-2 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Seed panel</span>
                        {overview.isLoading ? (
                            <div className="h-16 rounded-md bg-slate-50 animate-pulse" />
                        ) : (
                            <div role="radiogroup" aria-label="Seed panel" className="grid gap-1.5">
                                {panels.map((p) => {
                                    const active = p.panel === draft.panel;
                                    return (
                                        <button
                                            key={p.panel}
                                            type="button"
                                            role="radio"
                                            aria-checked={active}
                                            disabled={!p.available}
                                            onClick={() => patch({ panel: p.panel })}
                                            className={cn(
                                                "flex w-full items-start gap-2.5 rounded-md border px-3 py-2 text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-100",
                                                active && p.available
                                                    ? "border-sky-300 bg-sky-50"
                                                    : "border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50",
                                                !p.available && "opacity-60 cursor-not-allowed hover:bg-white hover:border-slate-200",
                                            )}
                                        >
                                            <span className="min-w-0 flex-1">
                                                <span className="flex items-center gap-2">
                                                    <span className={cn("text-[12px] font-medium", active && p.available ? "text-sky-700" : "text-slate-700")}>
                                                        {PANEL_LABEL[p.panel]}
                                                    </span>
                                                    <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                                                        {p.seeds} seed{p.seeds === 1 ? "" : "s"}
                                                    </span>
                                                    {p.metered && p.available && (
                                                        <span className="h-4 px-1.5 rounded bg-slate-100 text-[10px] text-slate-500 inline-flex items-center">
                                                            Counted
                                                        </span>
                                                    )}
                                                </span>
                                                <span className="mt-0.5 block text-[11px] leading-snug text-slate-400">
                                                    {!p.available
                                                        ? p.reason || "Not available on this workspace."
                                                        : p.metered
                                                          ? usage?.limit != null
                                                              ? `Counts toward your monthly tests: ${usage.used} of ${usage.limit} used.`
                                                              : "Counts toward your monthly tests."
                                                          : "Not counted toward your monthly tests."}
                                                </span>
                                            </span>
                                            <span
                                                className={cn(
                                                    "mt-0.5 size-4 shrink-0 rounded-full border transition-colors",
                                                    active && p.available ? "border-sky-600 bg-sky-600 ring-2 ring-inset ring-white" : "border-slate-300 bg-white",
                                                )}
                                                aria-hidden="true"
                                            />
                                        </button>
                                    );
                                })}
                            </div>
                        )}
                        {fieldError("panel")}
                    </section>

                    {/* Cost */}
                    {sender && panel?.available && copies > 0 && (
                        <div className="rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5 text-[11.5px] leading-relaxed text-slate-600">
                            Sends up to <b className="font-medium text-slate-900">{copies}</b> email{copies === 1 ? "" : "s"} from{" "}
                            <b className="font-medium text-slate-900">{sender.email}</b>, one every ~{spacing} seconds, counted
                            against its daily limit, and fewer when the mailbox has less than that left today. Sending takes about {minutes} minute{minutes === 1 ? "" : "s"}; a copy
                            not seen within 2 hours counts as never arrived. Seeds on the sender&apos;s own domain are skipped.
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
                    </button>
                </div>
            </motion.div>
        </motion.div>
    );
}

function InlineError({ message, compact = false }: { message: string; compact?: boolean }) {
    return (
        <p className={cn("flex items-start gap-1.5 text-[11.5px] leading-snug text-rose-600", !compact && "mt-1.5")}>
            <AlertCircleIcon className="w-3.5 h-3.5 shrink-0 mt-px" />
            <span>{message}</span>
        </p>
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

function CampaignPicker({ value, name, onChange }: { value: string; name?: string; onChange: (id: string) => void }) {
    const [open, setOpen] = React.useState(false);
    const [q, setQ] = React.useState("");
    const debounced = useDebouncedValue(q.trim(), 250);
    const list = useCampaigns({ query: debounced, folder: "", limit: 20, enabled: open });
    return (
        <PopoverMenu open={open} onOpenChange={setOpen}>
            <PopoverMenuTrigger asChild>
                <SelectButton
                    icon={<MegaphoneIcon className="w-3.5 h-3.5" />}
                    label={value ? (name ?? "Loading…") : "Pick a campaign"}
                    className="w-full [&>span:nth-child(2)]:max-w-none [&>span:nth-child(2)]:flex-1 [&>span:nth-child(2)]:text-left"
                />
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={280} className="p-1 max-h-80">
                <div className="p-1.5">
                    <SearchInput value={q} onChange={setQ} placeholder="Search campaigns…" autoFocus />
                </div>
                {list.isLoading && list.campaigns.length === 0 ? (
                    <div className="px-3 py-2 text-[11.5px] text-slate-400 inline-flex items-center gap-1.5">
                        <Loader2Icon className="w-3 h-3 animate-spin" /> Loading…
                    </div>
                ) : list.campaigns.length === 0 ? (
                    <div className="px-3 py-2 text-[11.5px] text-slate-400">No campaign matches that.</div>
                ) : (
                    list.campaigns.map((c) => (
                        <PopoverMenuItem key={c.id} selected={c.id === value} onSelect={() => onChange(c.id)}>
                            {c.name}
                        </PopoverMenuItem>
                    ))
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

// Whose merge fields fill the copy. Empty = the campaign's first lead, or the
// built-in sample contact for custom copy.
function ContactPicker({
    campaignId,
    value,
    onChange,
}: {
    campaignId: string;
    value: Contact | null;
    onChange: (c: Contact | null) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const [q, setQ] = React.useState("");
    const debounced = useDebouncedValue(q.trim(), 250);
    const searching = debounced.length > 0;
    const search = useSearchContacts({
        options: {
            query: debounced,
            custom_field_filters: [],
            campaign_ids: searching || !campaignId ? [] : [campaignId],
            sort_by: "updated_at",
            reverse: false,
        },
        limit: 8,
        enabled: open,
        keepPrevious: true,
    });
    const contacts = search.contacts ?? [];
    const fallback = campaignId ? "The campaign's first lead" : "A sample contact";
    return (
        <PopoverMenu open={open} onOpenChange={setOpen}>
            <PopoverMenuTrigger asChild>
                <SelectButton
                    icon={<UserRoundIcon className="w-3.5 h-3.5" />}
                    label={value ? contactLabel(value) : fallback}
                    className="w-full [&>span:nth-child(2)]:max-w-none [&>span:nth-child(2)]:flex-1 [&>span:nth-child(2)]:text-left"
                />
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={300} matchTriggerWidth className="p-1">
                <div className="p-1.5">
                    <SearchInput value={q} onChange={setQ} placeholder="Search contacts…" autoFocus />
                </div>
                <PopoverMenuItem selected={value === null} onSelect={() => onChange(null)} icon={<UserRoundIcon className="w-3.5 h-3.5" />}>
                    {fallback}
                </PopoverMenuItem>
                <PopoverMenuSeparator />
                <PopoverMenuLabel>{searching || !campaignId ? "Contacts" : "Leads in this campaign"}</PopoverMenuLabel>
                <div className="max-h-56 overflow-y-auto">
                    {search.isLoading && contacts.length === 0 ? (
                        <div className="px-3 py-2 text-[11.5px] text-slate-400 inline-flex items-center gap-1.5">
                            <Loader2Icon className="w-3 h-3 animate-spin" /> Loading…
                        </div>
                    ) : contacts.length === 0 ? (
                        <div className="px-3 py-2 text-[11.5px] text-slate-400">
                            {searching ? "No contact matches that." : "No contacts yet. Type to search."}
                        </div>
                    ) : (
                        contacts.map((c) => (
                            <PopoverMenuItem key={c.id} selected={value?.id === c.id} onSelect={() => onChange(c)}>
                                <span className="text-slate-800">{contactLabel(c)}</span>
                                <span className="ml-1.5 text-[11px] text-slate-400">{c.email}</span>
                            </PopoverMenuItem>
                        ))
                    )}
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
