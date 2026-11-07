// CampaignFlow is the campaign step builder. The contact's way through the
// campaign is drawn top down from where they enter: each email or action
// follows the last, a step with conditions splits into one column per path
// (checked left to right, first match wins) plus Otherwise, and a switch into
// one column per case. The line into a step carries its wait. A "+" on any
// line or at the end of a path adds a step there. Every edit is saved as it
// is made.

import React from "react";
import { AnimatePresence } from "framer-motion";
import { ArrowDownIcon, ArrowUpIcon, FlagIcon, GitBranchIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast/headless";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import type { SequenceBranch } from "@/lib/api/models/app/campaigns/sequences/Branching";
import type { SequenceActionType } from "@/lib/api/models/app/campaigns/sequences/Action";
import useSequences from "@/lib/api/hooks/app/campaigns/sequences/useSequences";
import useCreateSequence from "@/lib/api/hooks/app/campaigns/sequences/useCreateSequence";
import useDeleteSequence from "@/lib/api/hooks/app/campaigns/sequences/useDeleteSequence";
import updateSequence from "@/lib/api/client/app/campaigns/sequences/updateSequence";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import useUpdateCampaign from "@/lib/api/hooks/app/campaigns/useUpdateCampaign";
import { useCampaignABVariants } from "@/lib/api/hooks/app/campaigns/useCampaignABVariants";
import { cursorColor, useLiveCanvas } from "@/hooks/useLiveCanvas";
import CursorChat from "@/components/app/presence/CursorChat";
import { useSuppressGlobalCursors } from "@/components/app/presence/GlobalCursors";
import { useResourceViewers } from "@/hooks/PresenceProvider";
import { useUserProfile } from "@/hooks/context/user";
import { useConfirm } from "@/hooks/context/confirm";
import { useAccessRestricted, usePermission } from "@/hooks/usePermission";
import { SCOPE_STEP_ACTIONS } from "@/lib/accessScope";
import { NumberInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent } from "@/components/ui/popover-menu";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import EntryDelayPicker from "@/components/app/campaigns/schedule/EntryDelayPicker";
import { entryDelayLabel } from "@/components/app/campaigns/schedule/entryDelay";
import FlowCanvas from "@/components/app/flow/FlowCanvas";
import FlowPanel from "@/components/app/flow/FlowPanel";
import StepPicker, { type PickerItem } from "@/components/app/flow/StepPicker";
import { STEP_H, STEP_W, StepBadge, StepCard, TERMINAL_H, TERMINAL_W, TerminalCard, type StepMenuItem } from "@/components/app/flow/StepCard";
import { DirtyContext, useDirtyRegistry } from "@/components/app/flow/dirty";
import { layoutFlow, type InsertPoint, type PlacedEdge } from "@/components/app/flow/tree";
import { cn } from "@/lib/utils";
import StepEmailArms from "./StepEmailArms";
import { conversationOpenerFor, conversationSubjectFor, earlierEmailsFor } from "./threading";
import { ActionEditor, NodeTypeSwitcher, PathEditor, ReplyStopWarning, StopOnReplyToggle } from "./CampaignEditors";
import { defaultActionFor } from "./campaignActions";
import {
    DEFAULT_CONDITION,
    ELSE_PORT,
    ENTRY_ID,
    REPLY_CONDITION,
    draftCondition,
    insertCondition,
    branchAt,
    campaignSource,
    conditionText,
    gotoCandidates,
    isCond,
    isInstantBranch,
    isRouterStep,
    isStopId,
    isSwitchStep,
    moveCondition,
    newBranchId,
    normalize,
    planRemoval,
    reachableSteps,
    routePort,
} from "./campaignGraph";
import { entryView, pickerItems, stepIssue, stepView } from "./campaignSteps";

const MAX_STEPS = 50;
const MAX_WAIT_DAYS = 60;
const SEQ_KEY = (id: string) => ["campaigns", id, "sequences"] as const;
const POSITIVE_REPLY_FIELDS = ["replied", "reply_positive", "reply_negative", "reply_neutral", "reply_automated"];

// A path with a draft is a new condition not saved yet.
type Selection = { kind: "entry" } | { kind: "step"; id: string } | { kind: "path"; stepId: string; port: string; draft?: SequenceBranch } | null;
type Choice = "email" | "router" | SequenceActionType;

const sizeOf = (id: string) => (isStopId(id) ? { w: TERMINAL_W, h: TERMINAL_H } : { w: STEP_W, h: STEP_H });
const days = (n: number) => (n === 1 ? "1 day" : `${n} days`);
const sameSelection = (a: Selection, b: Selection) => JSON.stringify(a) === JSON.stringify(b);

function typingTarget(t: EventTarget | null): boolean {
    const el = t as HTMLElement | null;
    return !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT" || el.isContentEditable);
}

export default function CampaignFlow({ campaignId }: { campaignId: string }) {
    const { data: sequences } = useSequences(campaignId);
    const steps = React.useMemo(() => sequences ?? [], [sequences]);
    const createSequence = useCreateSequence(campaignId);
    const deleteSequence = useDeleteSequence(campaignId);
    const { data: campaign } = useCampaign(campaignId);
    const updateCampaign = useUpdateCampaign(campaignId);
    const { data: variants } = useCampaignABVariants(campaignId);
    const confirm = useConfirm();
    const qc = useQueryClient();
    const canEdit = usePermission("MANAGE_CAMPAIGNS");
    // A restricted member edits emails, waits and paths; steps that act outside the campaign stay as set.
    const restricted = useAccessRestricted();
    const canEditStep = (s: Sequence) => canEdit && (!restricted || s.kind !== "action" || SCOPE_STEP_ACTIONS.has(s.action?.type ?? ""));

    const [selection, setSelection] = React.useState<Selection>(null);
    const [picker, setPicker] = React.useState<{ at: InsertPoint; anchor: DOMRect } | null>(null);
    const [waitEdit, setWaitEdit] = React.useState<{ target: string; anchor: { x: number; y: number } } | null>(null);
    const [busy, setBusy] = React.useState(false);
    const dirty = useDirtyRegistry();

    // Moving away from a panel with unsaved edits asks first.
    const selectionRef = React.useRef(selection);
    selectionRef.current = selection;
    const select = React.useCallback(
        (next: Selection) => {
            if (sameSelection(selectionRef.current, next)) return;
            if (dirty.isDirty()) {
                confirm.show("Discard your unsaved changes to this step?", async () => {
                    dirty.clear();
                    setSelection(next);
                });
                return;
            }
            setSelection(next);
        },
        [confirm, dirty],
    );

    // ── Derived ──────────────────────────────────────────────────────────────
    const byId = React.useMemo(() => new Map(steps.map((s) => [s.id, s])), [steps]);
    const indexOf = React.useMemo(() => new Map(steps.map((s, i) => [s.id, i])), [steps]);
    const emailNumber = React.useMemo(() => {
        const m = new Map<string, number>();
        let n = 0;
        for (const s of steps) if (s.kind === "email") m.set(s.id, ++n);
        return m;
    }, [steps]);
    const threadSubject = React.useCallback((id: string) => conversationSubjectFor(steps, indexOf.get(id) ?? 0), [steps, indexOf]);
    const viewOf = React.useCallback(
        (s: Sequence) => stepView(s, { index: indexOf.get(s.id) ?? 0, emailNumber: emailNumber.get(s.id) ?? 0, threadSubject: threadSubject(s.id) }),
        [indexOf, emailNumber, threadSubject],
    );
    const reachable = React.useMemo(() => reachableSteps(steps), [steps]);
    const variantCount = React.useMemo(() => {
        const m = new Map<string, number>();
        for (const v of variants ?? []) if (v.step_id && !v.is_control) m.set(v.step_id, (m.get(v.step_id) ?? 0) + 1);
        return m;
    }, [variants]);
    const entryDelay = campaign?.entry_delay_minutes ?? 0;

    const note = React.useCallback(
        (from: string, port: string, target: string) => {
            if (isStopId(target)) return undefined;
            if (from === ENTRY_ID) return entryDelay > 0 ? entryDelayLabel(entryDelay).toLowerCase() : undefined;
            const branch = branchAt(byId.get(from), port);
            if (branch && isInstantBranch(branch)) return undefined;
            const w = byId.get(target)?.wait_after ?? 0;
            return w > 0 ? days(w) : undefined;
        },
        [byId, entryDelay],
    );
    const layout = React.useMemo(() => layoutFlow(campaignSource(steps, sizeOf, note)), [steps, note]);
    const order = React.useMemo(() => layout.nodes.filter((n) => n.kind === "step").map((n) => n.ref), [layout]);
    const hasReplyBranch = React.useMemo(
        () => steps.some((s) => (s.conditions?.branches ?? []).some((b) => (b.conditions ?? []).some((c) => POSITIVE_REPLY_FIELDS.includes(c.field)))),
        [steps],
    );

    // ── Server writes ────────────────────────────────────────────────────────
    const invalidate = React.useCallback(() => qc.invalidateQueries({ queryKey: SEQ_KEY(campaignId) }), [qc, campaignId]);
    const patchCache = React.useCallback(
        (id: string, patch: Partial<Sequence>) => qc.setQueryData<Sequence[]>(SEQ_KEY(campaignId), (old) => old?.map((s) => (s.id === id ? { ...s, ...patch } : s))),
        [qc, campaignId],
    );

    const saveBranches = React.useCallback(
        async (id: string, branches: SequenceBranch[]) => {
            const b = normalize(branches);
            // A refetch still in flight would land on top of this patch and undo it.
            await qc.cancelQueries({ queryKey: SEQ_KEY(campaignId) });
            patchCache(id, { conditions: { branches: b } });
            try {
                await updateSequence(campaignId, id, { conditions: { branches: b } });
            } catch (err) {
                toast.error(buildError(err as AppError));
                throw err;
            } finally {
                void invalidate();
            }
        },
        [campaignId, patchCache, invalidate, qc],
    );

    const saveWait = React.useCallback(
        async (id: string, value: number) => {
            const d = Math.max(0, Math.min(MAX_WAIT_DAYS, Math.round(value)));
            await qc.cancelQueries({ queryKey: SEQ_KEY(campaignId) });
            patchCache(id, { wait_after: d });
            try {
                await updateSequence(campaignId, id, { wait_after: d });
            } catch (err) {
                toast.error(buildError(err as AppError));
            } finally {
                void invalidate();
            }
        },
        [campaignId, patchCache, invalidate, qc],
    );

    // Entry delay commits are chained: the PATCH has no revision check, so two
    // in flight could land out of order.
    const entryChain = React.useRef<Promise<unknown>>(Promise.resolve());
    const saveEntryDelay = (next: number) => {
        if (next === entryDelay) return;
        qc.setQueryData(["campaigns", campaignId], (old: unknown) => (old ? { ...(old as object), entry_delay_minutes: next } : old));
        entryChain.current = entryChain.current
            .catch(() => {})
            .then(() => updateCampaign.mutateAsync({ entry_delay_minutes: next }))
            .catch(async (err) => {
                toast.error(buildError(err as AppError));
                await qc.invalidateQueries({ queryKey: ["campaigns", campaignId] });
            });
    };

    const setStopOnReply = (next: boolean) => {
        qc.setQueryData(["campaigns", campaignId], (old: unknown) => (old ? { ...(old as object), stop_on_reply: next } : old));
        updateCampaign.mutateAsync({ stop_on_reply: next }).catch(async (err) => {
            toast.error(buildError(err as AppError));
            await qc.invalidateQueries({ queryKey: ["campaigns", campaignId] });
        });
    };

    // A new step, typed and pre-wired before anything points at it, so the
    // canvas never shows it unconnected.
    const createStep = async (choice: Choice, branches: SequenceBranch[]): Promise<string> => {
        const created = (await createSequence.mutateAsync()) as Sequence;
        const patch: Parameters<typeof updateSequence>[2] = {};
        if (choice === "router") Object.assign(patch, { kind: "wait", name: "Condition" });
        else if (choice !== "email") Object.assign(patch, { kind: "action", action: defaultActionFor(choice) });
        if (branches.length) patch.conditions = { branches: normalize(branches) };
        if (Object.keys(patch).length) {
            patchCache(created.id, patch as Partial<Sequence>);
            await updateSequence(campaignId, created.id, patch);
        }
        return created.id;
    };

    const continueTo = (before: string | null): SequenceBranch[] =>
        before && !isStopId(before) ? [{ branch_id: newBranchId(), target_step_id: before, conditions: [] }] : [];

    // Conditions go straight onto a step's own way on; anywhere else they need
    // a Condition step of their own.
    // Opening the picker would replace a panel with unsaved edits, so it asks first.
    const openPicker = (at: InsertPoint, anchor: DOMRect) => {
        const open = () => setPicker((cur) => (cur && JSON.stringify(cur.at) === JSON.stringify(at) ? null : { at, anchor }));
        if (!dirty.isDirty()) return open();
        confirm.show("Discard your unsaved changes to this step?", async () => {
            dirty.clear();
            setSelection(null);
            open();
        });
    };

    const canAttachCondition = (at: InsertPoint) => {
        const s = byId.get(at.from);
        return !!s && !isSwitchStep(s) && at.port === ELSE_PORT;
    };

    const insert = async (at: InsertPoint, item: PickerItem) => {
        if (busy) return;
        const from = at.from === ENTRY_ID ? null : byId.get(at.from);
        setBusy(true);
        try {
            if (item.key.startsWith("goto:")) {
                if (from) await saveBranches(from.id, routePort(from, at.port, item.key.slice("goto:".length)));
                return;
            }
            if (item.key === "condition" || item.key === "reply") {
                // A new condition is a draft until it is saved in the path panel, so
                // nothing routes on a default the user never chose.
                const draft = draftCondition(item.key === "reply" ? REPLY_CONDITION : DEFAULT_CONDITION);
                if (from && canAttachCondition(at)) {
                    setSelection({ kind: "path", stepId: from.id, port: `b:${draft.branch_id}`, draft });
                    return;
                }
                const id = await createStep("router", continueTo(at.before));
                if (from) await saveBranches(from.id, routePort(from, at.port, id));
                setSelection({ kind: "path", stepId: id, port: `b:${draft.branch_id}`, draft });
                return;
            }
            const choice: Choice = item.key === "email" ? "email" : (item.key.slice("action:".length) as SequenceActionType);
            const id = await createStep(choice, continueTo(at.before));
            if (from) await saveBranches(from.id, routePort(from, at.port, id));
            setSelection({ kind: "step", id });
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setBusy(false);
            void invalidate();
        }
    };

    const removeStep = (id: string) => {
        const s = byId.get(id);
        if (!s) return;
        const name = viewOf(s).title;
        const plan = planRemoval(steps, id);
        const after = plan.successor
            ? ` Whatever led to it will continue at “${viewOf(byId.get(plan.successor)!).title}”.`
            : plan.stranded.length
              ? ` ${plan.stranded.length === 1 ? "1 step" : `${plan.stranded.length} steps`} after it will be left unconnected and never run until something leads to them again.`
              : "";
        const first = indexOf.get(id) === 0 && steps.length > 1 ? " The next step in the order they were added becomes the first email." : "";
        confirm.show(`Delete “${name}”?${after}${first} This can't be undone.`, async () => {
            try {
                for (const r of plan.rewrites) {
                    patchCache(r.id, { conditions: { branches: r.branches } });
                    await updateSequence(campaignId, r.id, { conditions: { branches: r.branches } });
                }
                await deleteSequence.mutateAsync(id);
                // Only the panel of the step being deleted loses its unsaved edits.
                const cur = selectionRef.current;
                if (cur && cur.kind !== "entry" && (cur.kind === "step" ? cur.id : cur.stepId) === id) {
                    dirty.clear();
                    setSelection(null);
                }
                toast.success("Step deleted");
            } catch (err) {
                toast.error(buildError(err as AppError));
                throw err;
            } finally {
                void invalidate();
            }
        });
    };

    const removePath = (stepId: string, port: string) => {
        const s = byId.get(stepId);
        const branch = branchAt(s, port);
        if (!s || !branch) return;
        const apply = async () => {
            await saveBranches(stepId, (s.conditions?.branches ?? []).filter((b) => b.branch_id !== branch.branch_id));
            dirty.clear();
            setSelection({ kind: "step", id: stepId });
        };
        if (branch.target_step_id) {
            confirm.show("Remove this path? The steps it led to stay, unconnected, until something leads to them again.", apply);
            return;
        }
        void apply();
    };

    // ── Keyboard ─────────────────────────────────────────────────────────────
    React.useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (typingTarget(e.target) || document.querySelector("[data-floating], [role='alertdialog']")) return;
            if ((e.key === "Delete" || e.key === "Backspace") && selection?.kind === "step" && canEdit) {
                e.preventDefault();
                removeStep(selection.id);
            } else if (e.key === "Escape" && selection) {
                select(null);
            }
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    });

    // ── Live collaboration: cursors and selections ───────────────────────────
    const resource = `campaign:${campaignId}`;
    useSuppressGlobalCursors();
    const hasPeers = useResourceViewers(resource).length > 0;
    const live = useLiveCanvas(resource, { enabled: hasPeers });
    const { pushSelect } = live;
    const selectedStep = selection?.kind === "step" ? selection.id : selection?.kind === "path" ? selection.stepId : selection?.kind === "entry" ? ENTRY_ID : null;
    React.useEffect(() => pushSelect(selectedStep ? [selectedStep] : []), [pushSelect, selectedStep]);
    const { user: selfUser } = useUserProfile();
    const selfColor = cursorColor(selfUser?.id ?? "");

    // The canvas fills the rest of the window, like a design tool's surface.
    const boxRef = React.useRef<HTMLDivElement>(null);
    const [boxHeight, setBoxHeight] = React.useState(560);
    React.useLayoutEffect(() => {
        const measure = () => {
            const el = boxRef.current;
            if (el) setBoxHeight(Math.max(480, Math.round(window.innerHeight - el.getBoundingClientRect().top - 16)));
        };
        measure();
        window.addEventListener("resize", measure);
        return () => window.removeEventListener("resize", measure);
    }, []);

    // ── Rendering ────────────────────────────────────────────────────────────
    const stepTitle = (id: string) => {
        const s = byId.get(id);
        return s ? viewOf(s).title : "a step";
    };

    const renderStep = (id: string, selected: boolean) => {
        if (id === ENTRY_ID) {
            const v = entryView(entryDelay > 0 ? `First email after ${entryDelayLabel(entryDelay).toLowerCase()}` : "First email right away");
            return <StepCard icon={v.icon} tone={v.tone} kicker={v.kicker} title={v.title} summary={v.summary} selected={selected} />;
        }
        if (isStopId(id)) return <TerminalCard icon={<FlagIcon />} label="End" selected={selected} />;
        const s = byId.get(id);
        if (!s) return null;
        const v = viewOf(s);
        const issue = stepIssue(s, threadSubject(id));
        const variantsHere = variantCount.get(id) ?? 0;
        const badge = issue ? (
            <span title={issue} aria-label={`Needs setup: ${issue}`} className="size-1.5 shrink-0 rounded-full bg-amber-400" />
        ) : variantsHere > 0 ? (
            <StepBadge tone="muted" title={`A/B test with ${variantsHere + 1} versions`}>
                A/B
            </StepBadge>
        ) : null;
        const menu: StepMenuItem[] = [];
        if (canEdit && !isSwitchStep(s)) {
            menu.push({
                label: "Add a condition",
                icon: <GitBranchIcon className="size-3.5" />,
                onSelect: () => {
                    const draft = draftCondition(DEFAULT_CONDITION);
                    select({ kind: "path", stepId: s.id, port: `b:${draft.branch_id}`, draft });
                },
            });
        }
        if (canEdit) menu.push({ label: "Delete step", icon: <Trash2Icon className="size-3.5" />, onSelect: () => removeStep(id), danger: true });
        const hint = !reachable.has(id) ? "Not connected: nothing leads here, so contacts never reach this step" : issue ? `Needs setup: ${issue}` : undefined;
        return <StepCard icon={v.icon} tone={v.tone} kicker={v.kicker} title={v.title} summary={v.summary} badge={badge} hint={hint} selected={selected} detached={!reachable.has(id)} menu={menu} />;
    };

    const onSelectNode = (id: string | null) => {
        if (!id || isStopId(id)) return select(null);
        if (id === ENTRY_ID) return select({ kind: "entry" });
        select({ kind: "step", id });
    };

    const pickerAtEnd = !!picker && picker.at.before === null;
    const items = picker
        ? pickerItems({
              atEnd: pickerAtEnd && picker.at.from !== ENTRY_ID,
              full: steps.length >= MAX_STEPS,
              canCondition: canAttachCondition(picker.at),
              gotoCandidates: pickerAtEnd
                  ? gotoCandidates(steps, picker.at.from)
                        .filter((s) => branchAt(byId.get(picker.at.from), picker.at.port)?.target_step_id !== s.id)
                        .sort((a, b) => order.indexOf(a.id) - order.indexOf(b.id))
                        .map((s) => ({ id: s.id, view: viewOf(s) }))
                  : [],
          })
              .filter((i) => picker.at.from !== ENTRY_ID || i.key === "email" || i.key.startsWith("action:"))
              .filter((i) => !restricted || !i.key.startsWith("action:") || SCOPE_STEP_ACTIONS.has(i.key.slice("action:".length)))
        : [];

    const activeEdge = selection?.kind === "path" ? (layout.edges.find((e) => e.from === selection.stepId && e.port === selection.port)?.key ?? null) : null;

    const onPillClick = (e: PlacedEdge) => select({ kind: "path", stepId: e.from, port: e.port });
    const onNoteClick = (e: PlacedEdge, r: DOMRect) => {
        if (!canEdit) return;
        setWaitEdit({ target: e.from === ENTRY_ID ? ENTRY_ID : (e.target ?? e.to), anchor: { x: r.left, y: r.bottom } });
    };

    const panelStep = selection?.kind === "step" ? byId.get(selection.id) : undefined;
    const pathStep = selection?.kind === "path" ? byId.get(selection.stepId) : undefined;
    const waitStep = waitEdit && waitEdit.target !== ENTRY_ID ? byId.get(waitEdit.target) : undefined;

    return (
        <div ref={boxRef} className="relative flex w-full overflow-hidden rounded-md border border-slate-200 bg-white" style={{ height: boxHeight }}>
            <div className="relative flex min-w-0 flex-1 flex-col bg-slate-50/40">
                <div className="flex min-h-10 flex-wrap items-center gap-x-4 gap-y-1 rounded-t-md border-b border-slate-200 bg-white px-3 py-1.5">
                    <StopOnReplyToggle on={!!campaign?.stop_on_reply} onToggle={setStopOnReply} disabled={!canEdit} />
                    {campaign && !campaign.stop_on_reply && steps.length > 1 && <ReplyStopWarning hasReplyBranch={hasReplyBranch} onEnable={() => setStopOnReply(true)} />}
                </div>
                <div className="min-h-0 flex-1">
                    <FlowCanvas
                        layout={layout}
                        renderStep={renderStep}
                        stepTitle={stepTitle}
                        selectedId={selectedStep}
                        onSelect={onSelectNode}
                        onInsert={canEdit && !busy ? openPicker : undefined}
                        onRemoveGoto={
                            canEdit
                                ? (from, port) => {
                                      const s = byId.get(from);
                                      if (s) void saveBranches(from, routePort(s, port, null).filter((b) => isCond(b) || port !== ELSE_PORT || b.target_step_id !== null));
                                  }
                                : undefined
                        }
                        onPillClick={onPillClick}
                        onNoteClick={canEdit ? onNoteClick : undefined}
                        activeEdge={activeEdge}
                        activeInsert={picker?.at ?? null}
                        revealId={selectedStep}
                        cursors={live.cursors}
                        selections={live.selections}
                        onCursor={(p) => (p ? live.active && live.pushCursor(p.x, p.y) : live.clearCursor())}
                    />
                </div>
                <CursorChat active={live.active} color={selfColor} setChat={live.setChat} />
            </div>

            <DirtyContext.Provider value={dirty.report}>
                <AnimatePresence initial={false}>
                    {selection?.kind === "entry" && (
                        <FlowPanel key="entry" readOnly={!canEdit} {...entryView("")} title="Contact enters the campaign" onClose={() => select(null)}>
                            <div className="space-y-2 p-3">
                                <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Wait before the first email</div>
                                <EntryDelayPicker value={entryDelay} onCommit={saveEntryDelay} disabled={!canEdit} />
                                <p className="text-[11.5px] leading-relaxed text-slate-400">
                                    Counted from when the contact entered this campaign. Later steps wait the time shown on the line into them.
                                </p>
                            </div>
                        </FlowPanel>
                    )}
                    {panelStep && (
                        <StepPanel
                            key={panelStep.id}
                            campaignId={campaignId}
                            step={panelStep}
                            steps={steps}
                            index={indexOf.get(panelStep.id) ?? 0}
                            view={viewOf(panelStep)}
                            canEdit={canEditStep(panelStep)}
                            onClose={() => select(null)}
                            onChanged={() => void invalidate()}
                            onDelete={() => removeStep(panelStep.id)}
                            onSelectPath={(port) => select({ kind: "path", stepId: panelStep.id, port })}
                            onAddCondition={() => {
                                const draft = draftCondition(DEFAULT_CONDITION);
                                select({ kind: "path", stepId: panelStep.id, port: `b:${draft.branch_id}`, draft });
                            }}
                        />
                    )}
                    {pathStep && selection?.kind === "path" && (
                        <PathPanel
                            key={`${pathStep.id}:${selection.port}`}
                            step={pathStep}
                            steps={steps}
                            port={selection.port}
                            draft={selection.draft}
                            title={viewOf(pathStep).title}
                            canEdit={canEdit}
                            onClose={() => select(null)}
                            onSave={async (updated) => {
                                const all = pathStep.conditions?.branches ?? [];
                                // A step has one Otherwise; a second unconditional path would replace it.
                                if (!isCond(updated) && all.some((b) => !isCond(b) && b.branch_id !== updated.branch_id)) {
                                    toast.error("This step already has an Otherwise path. Keep a condition here, or remove that path first.");
                                    return;
                                }
                                const exists = all.some((b) => b.branch_id === updated.branch_id);
                                await saveBranches(pathStep.id, exists ? all.map((b) => (b.branch_id === updated.branch_id ? updated : b)) : insertCondition(pathStep, updated));
                                dirty.clear();
                                if (!exists) setSelection({ kind: "path", stepId: pathStep.id, port: `b:${updated.branch_id}` });
                                toast.success("Path saved");
                            }}
                            onMove={(branchId, dir) => void saveBranches(pathStep.id, moveCondition(pathStep, branchId, dir))}
                            onRemove={() => (selection.draft ? select(null) : removePath(pathStep.id, selection.port))}
                            waitDays={byId.get(branchAt(pathStep, selection.port)?.target_step_id ?? "")?.wait_after ?? 0}
                            onSetWait={(d) => {
                                const t = branchAt(pathStep, selection.port)?.target_step_id;
                                if (t) void saveWait(t, d);
                            }}
                        />
                    )}
                </AnimatePresence>
            </DirtyContext.Provider>

            {picker && (
                <StepPicker
                    anchor={picker.anchor}
                    clearance={STEP_W / 2}
                    items={items}
                    title="Add a step"
                    onPick={(item) => {
                        const at = picker.at;
                        setPicker(null);
                        void insert(at, item);
                    }}
                    onClose={() => setPicker(null)}
                />
            )}

            <PopoverMenu open={!!waitEdit} onOpenChange={(o) => !o && setWaitEdit(null)} anchorPoint={waitEdit?.anchor ?? null}>
                <PopoverMenuContent minWidth={260} className="p-3">
                    {waitEdit?.target === ENTRY_ID ? (
                        <div onClick={(e) => e.stopPropagation()} className="space-y-2">
                            <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Wait before the first email</div>
                            <EntryDelayPicker value={entryDelay} onCommit={saveEntryDelay} disabled={!canEdit} />
                        </div>
                    ) : waitStep ? (
                        <WaitEditor key={waitStep.id} value={waitStep.wait_after ?? 0} name={viewOf(waitStep).title} onCommit={(d) => void saveWait(waitStep.id, d)} />
                    ) : null}
                </PopoverMenuContent>
            </PopoverMenu>
        </div>
    );
}

function WaitEditor({ value, name, onCommit }: { value: number; name: string; onCommit: (days: number) => void }) {
    const [draft, setDraft] = React.useState(value);
    return (
        <div className="space-y-2" onClick={(e) => e.stopPropagation()}>
            <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Wait before “{name}”</div>
            <div className="flex items-center gap-2 text-[12px] text-slate-600">
                <NumberInput value={draft} onChange={setDraft} onCommit={(v) => onCommit(v)} min={0} max={MAX_WAIT_DAYS} className="w-20" align="center" />
                <span>{draft === 1 ? "day" : "days"} after the step before it</span>
            </div>
            <p className="text-[11px] leading-snug text-slate-400">Applies to every path into this step. Paths that run instantly skip it.</p>
        </div>
    );
}

// StepPanel edits one step: its type, then the email or the action. A Condition
// step lists its paths instead.
function StepPanel({
    campaignId,
    step,
    steps,
    index,
    view,
    canEdit,
    onClose,
    onChanged,
    onDelete,
    onSelectPath,
    onAddCondition,
}: {
    campaignId: string;
    step: Sequence;
    steps: Sequence[];
    index: number;
    view: { icon: React.ReactNode; tone: Parameters<typeof StepCard>[0]["tone"]; kicker: string; title: string };
    canEdit: boolean;
    onClose: () => void;
    onChanged: () => void;
    onDelete: () => void;
    onSelectPath: (port: string) => void;
    onAddCondition: () => void;
}) {
    // Parses earlier bodies; kept stable so the editor's AI context is too.
    const earlierEmails = React.useMemo(() => earlierEmailsFor(steps, index), [steps, index]);
    const footer = canEdit ? (
        <button type="button" onClick={onDelete} className="inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-[12px] font-medium text-rose-600 transition-colors hover:bg-rose-50">
            <Trash2Icon className="h-3.5 w-3.5" />
            Delete step
        </button>
    ) : undefined;
    if (isRouterStep(step)) {
        const conds = (step.conditions?.branches ?? []).filter(isCond);
        return (
            <FlowPanel readOnly={!canEdit} icon={view.icon} tone={view.tone} kicker={view.kicker} title={view.title} onClose={onClose} footer={footer}>
                <div className="space-y-3 p-3">
                    <p className="text-[11.5px] leading-relaxed text-slate-500">
                        Contacts reaching this step take the first path that matches, checked in this order. Anyone else goes on under Otherwise.
                    </p>
                    <div className="space-y-1">
                        {conds.map((b, i) => (
                            <button
                                key={b.branch_id}
                                type="button"
                                onClick={() => onSelectPath(`b:${b.branch_id}`)}
                                className="flex w-full items-center gap-2 rounded-md border border-slate-200 px-2.5 py-2 text-left text-[12px] text-slate-700 transition-colors hover:border-slate-300"
                            >
                                <span className="text-[10.5px] tabular-nums text-slate-400">{i + 1}</span>
                                <span className="min-w-0 flex-1 truncate">{conditionText(b)}</span>
                            </button>
                        ))}
                    </div>
                    {canEdit && (
                        <button
                            type="button"
                            onClick={onAddCondition}
                            className="inline-flex h-7 items-center gap-1.5 rounded-md border border-slate-200 px-2.5 text-[12px] font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-900"
                        >
                            <PlusIcon className="h-3.5 w-3.5" />
                            Add a condition
                        </button>
                    )}
                </div>
            </FlowPanel>
        );
    }
    return (
        <FlowPanel readOnly={!canEdit} icon={view.icon} tone={view.tone} kicker={view.kicker} title={view.title} onClose={onClose} footer={footer} wide={step.kind === "email"}>
            <div className="p-3">
                <NodeTypeSwitcher campaignId={campaignId} sequence={step} onChanged={onChanged} />
                {step.kind === "email" ? (
                    <StepEmailArms
                        campaignId={campaignId}
                        sequence={step}
                        index={index}
                        conversationSubject={conversationSubjectFor(steps, index)}
                        conversationOpener={conversationOpenerFor(steps, index)}
                        earlierEmails={earlierEmails}
                    />
                ) : (
                    <ActionEditor campaignId={campaignId} sequence={step} onSaved={onChanged} />
                )}
            </div>
        </FlowPanel>
    );
}

// PathPanel edits one path out of a step: its condition, where it sits in the
// order, and the wait into the step it leads to.
function PathPanel({
    step,
    steps,
    port,
    draft,
    title,
    canEdit,
    onClose,
    onSave,
    onMove,
    onRemove,
    waitDays,
    onSetWait,
}: {
    step: Sequence;
    steps: Sequence[];
    port: string;
    // A new condition not saved yet.
    draft?: SequenceBranch;
    title: string;
    canEdit: boolean;
    onClose: () => void;
    onSave: (b: SequenceBranch) => Promise<void>;
    onMove: (branchId: string, dir: -1 | 1) => void;
    onRemove: () => void;
    waitDays: number;
    onSetWait: (days: number) => void;
}) {
    const branch = branchAt(step, port) ?? draft;
    const conds = (step.conditions?.branches ?? []).filter(isCond);
    const index = branch ? conds.findIndex((b) => b.branch_id === branch.branch_id) : -1;
    const label = port === ELSE_PORT ? "Otherwise" : branch ? conditionText(branch) : "Path";
    return (
        <FlowPanel readOnly={!canEdit} icon={<GitBranchIcon />} tone="amber" kicker={draft ? `New path from ${title}` : `Path from ${title}`} title={label} onClose={onClose}>
            {branch ? (
                <>
                    {index >= 0 && conds.length > 1 && canEdit && (
                        <div className="flex items-center gap-1 px-3 pt-3">
                            <button
                                type="button"
                                disabled={index === 0}
                                onClick={() => onMove(branch.branch_id, -1)}
                                className="inline-flex h-7 items-center gap-1 rounded-md border border-slate-200 px-2 text-[11.5px] text-slate-600 transition-colors hover:border-slate-300 disabled:opacity-40"
                            >
                                <ArrowUpIcon className="h-3 w-3" />
                                Check earlier
                            </button>
                            <button
                                type="button"
                                disabled={index === conds.length - 1}
                                onClick={() => onMove(branch.branch_id, 1)}
                                className="inline-flex h-7 items-center gap-1 rounded-md border border-slate-200 px-2 text-[11.5px] text-slate-600 transition-colors hover:border-slate-300 disabled:opacity-40"
                            >
                                <ArrowDownIcon className="h-3 w-3" />
                                Check later
                            </button>
                        </div>
                    )}
                    <PathEditor
                        key={JSON.stringify(branch)}
                        source={step}
                        branch={branch}
                        steps={steps}
                        order={index}
                        condCount={conds.length}
                        onMove={(dir) => onMove(branch.branch_id, dir)}
                        waitDays={waitDays}
                        onSetWait={onSetWait}
                        onSave={(b) => void onSave(b)}
                        onDelete={onRemove}
                    />
                </>
            ) : (
                <p className={cn("p-3 text-[12px] leading-relaxed text-slate-500")}>
                    {isSwitchStep(step)
                        ? "Contacts sent down this case go here. Press + under it to add the first step."
                        : "Contacts that match none of the paths beside it go here. Press + under it to add the first step, or leave it empty to end the sequence for them."}
                </p>
            )}
        </FlowPanel>
    );
}
