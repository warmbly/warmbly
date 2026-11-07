// Side panel editors for the campaign step builder: a path's condition, a
// step's type and action settings, and the campaign-wide reply controls.

import React from "react";
import {
    AlertTriangleIcon,
    BracesIcon,
    CheckIcon,
    ChevronDownIcon,
    ChevronUpIcon,
    ClockIcon,
    InfoIcon,
    MailIcon,
    PlusIcon,
    SparklesIcon,
    SplitIcon,
    Trash2Icon,
    XIcon,
    ZapIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import type { SequenceBranch, BranchCondition, BranchField } from "@/lib/api/models/app/campaigns/sequences/Branching";
import { REPLY_INTENTS, isReplyBranchField, isInstantCapableField } from "@/lib/api/models/app/campaigns/sequences/Branching";
import updateSequence from "@/lib/api/client/app/campaigns/sequences/updateSequence";
import { useUserProfile } from "@/hooks/context/user";
import useClickOutside from "@/hooks/useClickOutside";
import { NumberInput, Label, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { PopoverMenu, PopoverMenuContent, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import CategoryPicker from "@/components/app/contacts/CategoryPicker";
import { SegmentMultiPicker } from "@/components/app/segments/SegmentPickers";
import type { ActionKV, AITagRef, SequenceAction, SequenceActionType } from "@/lib/api/models/app/campaigns/sequences/Action";
import { useAutomations } from "@/lib/api/hooks/app/automations/useAutomations";
import { triggerLabel } from "@/lib/api/models/app/automations/meta";
import TaskTypePicker from "@/components/app/crm/TaskTypePicker";
import AssigneeTeamPicker, { type AssigneeValue } from "@/components/app/crm/AssigneeTeamPicker";
import DealStagePicker from "@/components/app/crm/DealStagePicker";
import { CrmDealNote, CrmTaskNote } from "@/components/app/crm/crmMode";
import { useReportDirty } from "@/components/app/flow/dirty";
import { ACTION_META, ADD_ACTION_OPTIONS, AI_STEP_OPTION, BRANCH_PATH_OPTIONS, DEAL_NAME_VARIABLES, STOP_ON_REPLY_HELP, SWITCH_OPTION, defaultActionFor } from "./campaignActions";
import { caseKey } from "./campaignGraph";
import { fmtUsdAbout } from "@/lib/money";
import { AI_ESTIMATE_USD } from "@/lib/aiEstimates";

const stepName = (s: Sequence | undefined) => (s?.name?.trim() ? s.name : "Untitled step");


export function StopOnReplyToggle({ on, onToggle, disabled }: { on: boolean; onToggle: (next: boolean) => void; disabled?: boolean }) {
    return (
        <div
            className="flex items-center gap-2"
            title={STOP_ON_REPLY_HELP}
        >
            <span className="text-[11.5px] text-slate-600">Stop on reply</span>
            {/* Tooltips never show on touch; surface the same copy on tap. Hidden
                at md+ where the title attribute keeps desktop pixel-identical. */}
            <PopoverMenu align="start">
                <PopoverMenuTrigger asChild>
                    <button
                        type="button"
                        aria-label="What does stop on reply do?"
                        className="inline-flex size-5 items-center justify-center rounded text-slate-400 hover:bg-slate-100 hover:text-slate-600 md:hidden"
                    >
                        <InfoIcon className="w-3.5 h-3.5" />
                    </button>
                </PopoverMenuTrigger>
                <PopoverMenuContent minWidth={240} className="max-w-[280px] p-2.5">
                    <p className="text-[11.5px] leading-relaxed text-slate-600">{STOP_ON_REPLY_HELP}</p>
                </PopoverMenuContent>
            </PopoverMenu>
            <button
                type="button"
                role="switch"
                aria-checked={on}
                aria-label="Stop on reply"
                disabled={disabled}
                onClick={() => onToggle(!on)}
                className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:cursor-default disabled:opacity-60 ${
                    on ? "bg-sky-600" : "bg-slate-200"
                }`}
            >
                <span
                    className={`inline-block size-4 transform rounded-full bg-white shadow-sm transition-transform ${
                        on ? "translate-x-4" : "translate-x-0.5"
                    }`}
                />
            </button>
        </div>
    );
}

// Shown on the canvas when stop-on-reply is OFF and the sequence keeps sending.
// Continuing to cold-email a contact who replied is the classic deliverability
// mistake; turning stop-on-reply on still runs the reply branches (routing is
// reply-flow aware), so it is strictly safer. Copy adapts to whether the
// campaign has any reply handling at all.
export function ReplyStopWarning({ hasReplyBranch, onEnable }: { hasReplyBranch: boolean; onEnable: () => void }) {
    const text = hasReplyBranch
        ? "Off: replies outside a reply path still get follow-ups."
        : "Off: people who reply keep getting follow-ups.";
    return (
        <div className="flex min-w-0 items-center gap-1.5 text-[11.5px] text-amber-700" title={STOP_ON_REPLY_HELP}>
            <AlertTriangleIcon className="h-3.5 w-3.5 shrink-0 text-amber-500" />
            <span className="truncate">{text}</span>
            <button type="button" onClick={onEnable} className="shrink-0 font-medium text-amber-800 underline underline-offset-2 hover:text-amber-900">
                Turn it on
            </button>
        </div>
    );
}

export function WaitRow({ value, onCommit }: { value: number; onCommit: (v: number) => void }) {
    const [draft, setDraft] = React.useState(value);
    React.useEffect(() => setDraft(value), [value]);
    return (
        <div className="flex items-center gap-1.5 text-[12px] text-slate-600">
            <ClockIcon className="w-3.5 h-3.5 text-slate-400" />
            <span>wait</span>
            <NumberInput
                value={draft}
                onChange={setDraft}
                onCommit={(v) => onCommit(Math.max(0, Math.round(v)))}
                min={0}
                max={60}
                className="w-16"
                align="center"
            />
            <span>days before it</span>
        </div>
    );
}



export function PathEditor({
    source,
    branch,
    steps,
    order,
    condCount,
    onMove,
    waitDays,
    onSetWait,
    onSave,
    onDelete,
}: {
    source: Sequence;
    branch: SequenceBranch;
    steps: Sequence[];
    order: number;
    condCount: number;
    onMove: (dir: -1 | 1) => void;
    waitDays: number;
    onSetWait: (days: number) => void;
    onSave: (b: SequenceBranch) => void;
    onDelete: () => void;
}) {
    const c0 = branch.conditions?.[0];
    const [field, setField] = React.useState<string>(c0?.field ?? "always");
    const [value, setValue] = React.useState<number>(c0?.value ?? (c0?.field === "random" ? 50 : 3));
    // The intent a reply_intent path routes on; stored in the condition's label.
    const [intent, setIntent] = React.useState<string>(c0?.field === "reply_intent" ? (c0.label ?? "agreed") : "agreed");
    // Instant-capable branches (reply intent, opened, clicked) fire the moment
    // the event lands by default; this lets the user opt out so the path routes
    // at the next step boundary instead.
    const [instant, setInstant] = React.useState<boolean>(branch.instant !== false);

    const isAlways = field === "always";
    const isRandom = field === "random";
    // A Switch case path: the case is named on the step itself, so this
    // editor only handles the target/wait — the condition itself is fixed.
    const isCasePath = c0?.field === "ai_label";
    const caseName = isCasePath ? (c0?.label ?? "").trim() : "";
    const isReply = isReplyBranchField(field as BranchField);
    const isIntent = field === "reply_intent";
    const isInstantCapable = !isCasePath && isInstantCapableField(field as BranchField);
    const instantVerb = field === "opened" ? "open" : field === "clicked" ? "click" : "reply";
    const isNegative = field === "not_opened" || field === "not_clicked" || field === "not_replied";
    const target = steps.find((s) => s.id === branch.target_step_id);
    const targetLabel = branch.target_step_id === null ? "the end of the sequence" : target ? `“${stepName(target)}”` : "a step that no longer exists";
    const aiSwitch = source.action?.switch_on !== "value";

    const buildConditions = (): BranchCondition[] => {
        if (isCasePath) return branch.conditions ?? [];
        if (isAlways) return [];
        if (isRandom) return [{ field: "random", operator: "chance", value }];
        // Reply-class conditions are checked once, ever (no day window / value).
        if (isReply) return [{ field: field as BranchField, operator: "ever" }];
        if (isIntent) return [{ field: "reply_intent", operator: "is", label: intent }];
        return [{ field: field as BranchField, operator: "within_days", value }];
    };
    // Compared field by field: a server row can carry keys an edit never writes.
    const sig = (cs: BranchCondition[], inst: boolean) => JSON.stringify([cs.map((c) => [c.field, c.operator, c.value ?? null, c.label ?? null]), inst]);
    useReportDirty(sig(buildConditions(), isInstantCapable ? instant : branch.instant !== false) !== sig(branch.conditions ?? [], branch.instant !== false));
    const save = (target_step_id: string | null) => {
        onSave({
            branch_id: branch.branch_id,
            target_step_id,
            conditions: buildConditions(),
            // Persist the instant opt-out for any instant-capable signal (reply
            // intent, opened, clicked). Other fields can't fire instantly.
            instant: isInstantCapable ? instant : undefined,
        });
    };

    return (
        <div className="space-y-3 p-3">
            {order >= 0 && condCount > 1 && (
                <div className="flex items-center justify-between rounded-md bg-slate-50 px-2 py-1 text-[11px] text-slate-500">
                    <span>Checked {order + 1} of {condCount}. The first path that matches wins.</span>
                    <span className="flex items-center gap-0.5">
                        <button
                            type="button"
                            onClick={() => onMove(-1)}
                            disabled={order === 0}
                            className="inline-flex size-5 items-center justify-center rounded text-slate-400 hover:bg-white hover:text-slate-700 disabled:opacity-30"
                        >
                            <ChevronUpIcon className="w-3.5 h-3.5" />
                        </button>
                        <button
                            type="button"
                            onClick={() => onMove(1)}
                            disabled={order === condCount - 1}
                            className="inline-flex size-5 items-center justify-center rounded text-slate-400 hover:bg-white hover:text-slate-700 disabled:opacity-30"
                        >
                            <ChevronDownIcon className="w-3.5 h-3.5" />
                        </button>
                    </span>
                </div>
            )}

            <div className="space-y-2 text-[12px] text-slate-600">
                <div className="flex flex-wrap items-center gap-1.5">
                    <span>then go to</span>
                    <span className="font-medium text-slate-800">{targetLabel}</span>
                    {branch.target_step_id !== null && (
                        <button
                            type="button"
                            onClick={() => save(null)}
                            className="text-[10.5px] font-medium text-rose-500 hover:underline"
                        >
                            end the sequence here instead
                        </button>
                    )}
                </div>

                {!isCasePath && (
                    <div>
                        <p className="mb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Take this path</p>
                        <SelectMenu
                            className="w-full"
                            value={field}
                            options={BRANCH_PATH_OPTIONS}
                            onChange={(f) => {
                                setField(f);
                                if (f === "random") setValue((v) => (v >= 1 && v <= 99 ? v : 50));
                                else if (f !== "always" && f !== "reply_intent" && !isReplyBranchField(f as BranchField))
                                    setValue((v) => (v >= 1 && v <= 60 ? v : 3));
                            }}
                        />
                    </div>
                )}

                {isRandom && (
                    <div className="flex flex-wrap items-center gap-1.5">
                        <NumberInput value={value} onChange={(v) => setValue(Math.max(1, Math.min(99, Math.round(v) || 1)))} min={1} max={99} className="w-16" align="center" />
                        <span>% of contacts (chosen at random)</span>
                    </div>
                )}
                {isIntent && (
                    <div>
                        <p className="mb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Reply intent</p>
                        <SelectMenu className="w-full" value={intent} options={REPLY_INTENTS} onChange={setIntent} />
                    </div>
                )}
                {isCasePath && (
                    <p className="rounded-md bg-slate-50 px-2 py-1.5 text-[11px] leading-relaxed text-slate-600 ring-1 ring-slate-200">
                        The “{caseName}” case of this switch: contacts take this path when{" "}
                        {aiSwitch ? "the AI picks" : "the value matches"} “{caseName}”. Rename or remove the case on the
                        step itself; routing happens at the step boundary at no extra cost.
                    </p>
                )}
                {!isAlways && !isRandom && !isReply && !isIntent && !isCasePath && (
                    <div className="flex flex-wrap items-center gap-1.5">
                        <span>within</span>
                        <NumberInput value={value} onChange={(v) => setValue(Math.max(1, Math.min(60, Math.round(v) || 1)))} min={1} max={60} className="w-16" align="center" />
                        <span>days</span>
                    </div>
                )}
                {isInstantCapable && (
                    <div className="space-y-1">
                        <div
                            className={`flex items-center gap-2 rounded-md px-2 py-1.5 ring-1 transition-colors ${
                                instant ? "bg-violet-50 ring-violet-200" : "bg-slate-50 ring-slate-200"
                            }`}
                        >
                            <ZapIcon
                                className={`w-3.5 h-3.5 shrink-0 transition-colors ${
                                    instant ? "text-violet-600" : "text-slate-400"
                                }`}
                            />
                            <span
                                className={`min-w-0 flex-1 text-[11px] font-medium leading-tight transition-colors ${
                                    instant ? "text-violet-700" : "text-slate-500"
                                }`}
                            >
                                {instant ? `Instant: runs the moment they ${instantVerb}` : "Routes at the next step"}
                            </span>
                            <button
                                type="button"
                                role="switch"
                                aria-checked={instant}
                                aria-label="Run instantly"
                                onClick={() => setInstant((v) => !v)}
                                title="Toggle whether this path fires the moment it happens or waits for the next step"
                                className={`relative inline-flex h-[18px] w-8 shrink-0 items-center rounded-full transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-300 ${
                                    instant ? "bg-violet-600" : "bg-slate-300"
                                }`}
                            >
                                <span
                                    className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow-sm transition-transform duration-200 ${
                                        instant ? "translate-x-[15px]" : "translate-x-[2px]"
                                    }`}
                                />
                            </button>
                        </div>
                        {isReply || isIntent ? (
                            <>
                                <p className="text-[10.5px] text-slate-400">
                                    {isIntent
                                        ? "Routes when automatic inbox tagging read the contact's reply as this intent, at 70% confidence or more. Needs inbox tagging on; an untagged reply takes the otherwise path."
                                        : field === "reply_automated"
                                        ? "Routes when the contact's reply is an auto-reply or out-of-office bounce, not a real human reply. Pair this with action steps (create deal, move stage, notify) to react."
                                        : "Routes when the contact's reply is classified this way. Chain action steps after it, for example create deal then move stage then notify."}
                                </p>
                                <p className="text-[10.5px] text-amber-600">
                                    A reply triggers the reply path of the specific email it answers (matched by the reply's threading headers): the earlier email, not the latest step, and never both. It falls back to the latest step only when the reply can't be threaded. With stop-on-reply on, the sequence also pauses.
                                </p>
                            </>
                        ) : (
                            <p className="text-[10.5px] text-slate-400">
                                {instant
                                    ? `Runs the moment they ${instantVerb}, as long as they ${instantVerb} within the ${value}-day window above. If they don't ${instantVerb} in that time, this path never runs. Chain action steps after it (create deal, move stage, notify).`
                                    : `Waits up to ${value} day${value === 1 ? "" : "s"} for them to ${instantVerb}, then checks at the next step. If they don't ${instantVerb} in that window, this path never runs.`}
                            </p>
                        )}
                    </div>
                )}
                {isNegative && (
                    <p className="text-[10.5px] text-slate-400">
                        We keep checking until {value} day{value === 1 ? "" : "s"} pass, then take this path if it still hasn’t happened.
                    </p>
                )}

                {branch.target_step_id !== null && !(isInstantCapable && instant) && (
                    <WaitRow value={waitDays} onCommit={onSetWait} />
                )}
            </div>

            <div className="flex items-center gap-2 border-t border-slate-200 pt-3">
                <button
                    type="button"
                    onClick={onDelete}
                    title="Remove this path. Steps it led to stay, unconnected."
                    className="inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-[12px] font-medium text-rose-500 transition-colors hover:bg-rose-50 hover:text-rose-600"
                >
                    <Trash2Icon className="w-3.5 h-3.5" />
                    Remove path
                </button>
                <button
                    type="button"
                    onClick={() => save(branch.target_step_id)}
                    className="ml-auto h-7 rounded-md bg-sky-600 px-3 text-[12px] font-medium text-white hover:bg-sky-700"
                >
                    Save
                </button>
            </div>
        </div>
    );
}


// ── Node-type switcher (top of the editor drawer) ───────────────────────────
// One place to switch ANY node between Send email and every action type —
// mirrors the unified "Add" menu. Switching persists immediately and the drawer
// body re-renders to the matching editor (SequenceView for email, ActionEditor
// for actions).
export function NodeTypeSwitcher({
    campaignId,
    sequence,
    onChanged,
}: {
    campaignId: string;
    sequence: Sequence;
    onChanged: () => void;
}) {
    const [busy, setBusy] = React.useState(false);
    const current: "email" | SequenceActionType =
        sequence.kind === "email" ? "email" : sequence.action?.type ?? "add_tag";

    const items: { value: "email" | SequenceActionType; label: string; Icon: typeof MailIcon; tint: string }[] = [
        { value: "email", label: "Send email", Icon: MailIcon, tint: "text-sky-600" },
        { value: "switch", label: SWITCH_OPTION.label, Icon: SplitIcon, tint: "text-purple-600" },
        { value: "ai_step", label: AI_STEP_OPTION.label, Icon: SparklesIcon, tint: "text-purple-600" },
        ...ADD_ACTION_OPTIONS.map((o) => ({
            value: o.type,
            label: o.label,
            Icon: ACTION_META[o.type].Icon,
            tint: ACTION_META[o.type].tint,
        })),
    ];

    const pick = async (value: "email" | SequenceActionType) => {
        if (busy || value === current) return;
        setBusy(true);
        try {
            if (value === "email") {
                await updateSequence(campaignId, sequence.id, { kind: "email" });
            } else {
                await updateSequence(campaignId, sequence.id, {
                    kind: "action",
                    action: defaultActionFor(value),
                });
            }
            onChanged();
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setBusy(false);
        }
    };

    return (
        <div className="mb-4">
            <Label>Step type</Label>
            <div className="grid grid-cols-2 gap-1.5 sm:grid-cols-3">
                {items.map((it) => {
                    const active = it.value === current;
                    const Icon = it.Icon;
                    return (
                        <button
                            key={it.value}
                            type="button"
                            disabled={busy}
                            onClick={() => pick(it.value)}
                            className={`flex items-center gap-1.5 rounded-md border px-2 py-1.5 text-left text-[11.5px] transition-colors disabled:opacity-60 ${
                                active
                                    ? "border-sky-300 bg-sky-50 text-sky-700"
                                    : "border-slate-200 bg-white text-slate-700 hover:border-slate-300"
                            }`}
                        >
                            <Icon className={`w-3.5 h-3.5 shrink-0 ${active ? "text-sky-600" : it.tint}`} />
                            <span className="truncate">{it.label}</span>
                        </button>
                    );
                })}
            </div>
        </div>
    );
}

// ── Action editor (drawer body for non-email nodes) ─────────────────────────
export function ActionEditor({
    campaignId,
    sequence,
    onSaved,
}: {
    campaignId: string;
    sequence: Sequence;
    onSaved: () => void;
}) {
    const [action, setAction] = React.useState<SequenceAction>(sequence.action ?? { type: "add_tag" });
    const [name, setName] = React.useState(sequence.name ?? "");
    const [saving, setSaving] = React.useState(false);
    useReportDirty(JSON.stringify(action) !== JSON.stringify(sequence.action ?? { type: "add_tag" }) || name !== (sequence.name ?? ""));
    React.useEffect(() => {
        setAction(sequence.action ?? { type: "add_tag" });
        setName(sequence.name ?? "");
    }, [sequence.id, sequence.action, sequence.kind, sequence.name]);

    const save = async () => {
        setSaving(true);
        try {
            // Switch: a renamed case keeps its path (matched by position); a
            // removed case's path is dropped, so no path hangs off a case that is gone.
            let healed: SequenceBranch[] | null = null;
            if (action.type === "switch") {
                const prev = (sequence.action?.type === "switch" ? sequence.action.switch_cases ?? [] : []).map((c) => c.trim());
                const cur = (action.switch_cases ?? []).map((c) => c.trim());
                const renamed = new Map<string, string>();
                // A name still on another row is still that case; it never moves.
                if (prev.length === cur.length) prev.forEach((p, i) => p && cur[i] && caseKey(p) !== caseKey(cur[i]) && !cur.some((c) => caseKey(c) === caseKey(p)) && renamed.set(caseKey(p), cur[i]));
                const keep = new Set(cur.map((c) => caseKey(c)).filter(Boolean));
                const all = sequence.conditions?.branches ?? [];
                const next = all.flatMap((b) => {
                    const c0 = (b.conditions ?? []).find((c) => c.field === "ai_label");
                    if (!c0) return [b];
                    const to = renamed.get(caseKey(c0.label ?? ""));
                    if (to) return [{ ...b, conditions: (b.conditions ?? []).map((c) => (c === c0 ? { ...c, label: to } : c)) }];
                    return keep.has(caseKey(c0.label ?? "")) ? [b] : [];
                });
                if (JSON.stringify(next) !== JSON.stringify(all)) healed = next;
            }
            await updateSequence(campaignId, sequence.id, {
                name,
                kind: "action",
                action,
                ...(healed ? { conditions: { branches: healed } } : {}),
            });
            onSaved();
            toast.success("Action saved");
        } catch (err) {
            toast.error(buildError(err as AppError));
        } finally {
            setSaving(false);
        }
    };

    return (
        <div className="space-y-5">
            <div>
                <Label>Step name</Label>
                <TextInput
                    value={name}
                    onChange={setName}
                    placeholder={ACTION_META[action.type]?.label ?? "Action"}
                    className="w-full max-w-[320px]"
                />
                <p className="mt-1.5 text-[11px] text-slate-400">Internal label only — shown on the node.</p>
            </div>

            <ActionConfigFields action={action} setAction={setAction} />
            {action.type === "switch" && <SwitchStepFields action={action} setAction={setAction} />}
            {action.type === "ai_step" && <AIStepFields action={action} setAction={setAction} />}

            <div className="flex items-center justify-end pt-1">
                <button
                    type="button"
                    onClick={save}
                    disabled={saving}
                    className="h-7 rounded-md bg-sky-600 px-3 text-[12px] font-medium text-white transition-colors hover:bg-sky-700 disabled:opacity-60"
                >
                    {saving ? "Saving…" : "Save action"}
                </button>
            </div>
        </div>
    );
}

// ActionConfigFields renders the per-type configuration for one action config.
// The "switch" type renders nothing here — SwitchStepFields carries its whole
// editor.
function ActionConfigFields({
    action,
    setAction,
}: {
    action: SequenceAction;
    setAction: React.Dispatch<React.SetStateAction<SequenceAction>>;
}) {
    return (
        <>
            {(action.type === "add_tag" || action.type === "remove_tag") && (
                <div>
                    <Label>{action.type === "add_tag" ? "Label to add" : "Label to remove"}</Label>
                    <CategoryPicker
                        value={action.category_id ? [action.category_id] : []}
                        onChange={(ids) =>
                            setAction((a) => ({ ...a, category_id: ids.length ? ids[ids.length - 1] : null }))
                        }
                        placeholder="Pick a label…"
                    />
                </div>
            )}

            {(action.type === "add_to_segment" || action.type === "remove_from_segment") && (
                <div>
                    <Label>{action.type === "add_to_segment" ? "Segment to add to" : "Segment to remove from"}</Label>
                    <SegmentMultiPicker
                        value={action.segment_id ? [action.segment_id] : []}
                        onChange={(ids) =>
                            setAction((a) => ({ ...a, segment_id: ids.length ? ids[ids.length - 1] : null }))
                        }
                    />
                    <p className="mt-1.5 text-[11px] text-slate-400">
                        {action.type === "add_to_segment"
                            ? "The contact stays in the segment whatever its conditions say."
                            : "The contact stays out of the segment even while its conditions match."}
                    </p>
                </div>
            )}

            {action.type === "label_email" && (
                <div>
                    <Label>Labels to apply</Label>
                    <CategoryPicker
                        value={action.label_ids ?? []}
                        onChange={(ids) => setAction((a) => ({ ...a, label_ids: ids }))}
                        placeholder="Pick one or more labels…"
                    />
                    <p className="mt-1.5 rounded-md border border-fuchsia-200 bg-fuchsia-50/60 px-2.5 py-2 text-[11px] leading-relaxed text-fuchsia-700">
                        Labels the conversation in your inbox (the same labels you set by hand in the unibox). Place this
                        on a reply branch — it runs once the contact has replied, so there is a thread to label, and is a
                        no-op otherwise.
                    </p>
                </div>
            )}

            {action.type === "unsubscribe" && (
                <p className="rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5 text-[11.5px] leading-relaxed text-slate-600">
                    Suppresses this contact across your workspace — they won't receive further campaign emails, and a{" "}
                    <code className="font-mono">campaign.unsubscribed</code> event fires to your integrations.
                </p>
            )}

            {action.type === "create_task" && (
                <div className="space-y-4">
                    <CrmTaskNote className="max-w-[420px]" />
                    <div>
                        <Label>Task title</Label>
                        <TextInput
                            value={action.task_title ?? ""}
                            onChange={(v) => setAction((a) => ({ ...a, task_title: v }))}
                            placeholder="e.g. Call this lead"
                            className="w-full max-w-[320px]"
                        />
                        <p className="mt-1.5 text-[11px] text-slate-400">
                            Left blank, it defaults to “Follow up: {"{contact}"}”.
                        </p>
                    </div>
                    <div>
                        <Label>Task type</Label>
                        <TaskTypePicker
                            value={action.task_type ?? ""}
                            onChange={(name) => setAction((a) => ({ ...a, task_type: name }))}
                            className="max-w-[280px]"
                        />
                    </div>
                    <div className="flex flex-wrap items-end gap-4">
                        <div>
                            <Label>Priority</Label>
                            <div className="inline-flex rounded-md border border-slate-200 bg-white p-0.5">
                                {(["low", "medium", "high", "urgent"] as const).map((p) => (
                                    <button
                                        key={p}
                                        type="button"
                                        onClick={() => setAction((a) => ({ ...a, task_priority: p }))}
                                        className={`h-7 px-2.5 rounded text-[11px] font-medium capitalize transition-colors ${
                                            (action.task_priority ?? "medium") === p
                                                ? "bg-sky-600 text-white shadow-sm"
                                                : "text-slate-500 hover:bg-slate-50 hover:text-slate-700"
                                        }`}
                                    >
                                        {p}
                                    </button>
                                ))}
                            </div>
                        </div>
                        <div>
                            <Label>Due in (days)</Label>
                            <NumberInput
                                value={action.task_due_offset_days ?? 1}
                                onChange={(n) => setAction((a) => ({ ...a, task_due_offset_days: n }))}
                                min={0}
                                max={365}
                                className="w-28"
                            />
                        </div>
                    </div>
                    <div>
                        <Label>Assign to</Label>
                        <AssigneeTeamPicker
                            className="w-full max-w-[320px]"
                            fallbackLabel="Campaign owner"
                            value={{ userId: action.task_assigned_to ?? null, teamId: action.task_assigned_team_id ?? null }}
                            onChange={(v: AssigneeValue) =>
                                setAction((a) => ({ ...a, task_assigned_to: v.userId ?? null, task_assigned_team_id: v.teamId ?? null }))
                            }
                        />
                        <p className="mt-1.5 text-[11px] text-slate-400">Assign to a teammate or a whole team. Unassigned falls back to the campaign owner.</p>
                    </div>
                </div>
            )}

            {(action.type === "create_deal" || action.type === "move_deal_stage") && (
                <div className="space-y-4">
                    <CrmDealNote className="max-w-[420px]" creates={action.type === "create_deal"} />
                    <div>
                        <Label>{action.type === "create_deal" ? "Create the deal in" : "Move the deal to"}</Label>
                        <DealStagePicker
                            pipelineId={action.deal_pipeline_id}
                            stageId={action.deal_stage_id}
                            onChange={({ pipelineId, stageId }) =>
                                setAction((a) => ({ ...a, deal_pipeline_id: pipelineId, deal_stage_id: stageId }))
                            }
                        />
                        {action.type === "move_deal_stage" && (
                            <p className="mt-1.5 text-[11px] text-slate-400">
                                Moves the contact's most recent open deal in this pipeline to this stage. If they have no
                                open deal here, nothing happens.
                            </p>
                        )}
                    </div>

                    {action.type === "create_deal" && (
                        <>
                            <div>
                                <div className="mb-1.5 flex items-center justify-between gap-2">
                                    <Label className="mb-0">Deal name</Label>
                                    <DealNameVariableMenu
                                        onPick={(token) =>
                                            setAction((a) => ({ ...a, deal_name: (a.deal_name ?? "") + token }))
                                        }
                                    />
                                </div>
                                <TextInput
                                    value={action.deal_name ?? ""}
                                    onChange={(v) => setAction((a) => ({ ...a, deal_name: v }))}
                                    placeholder="{{.Company}} ({{.FirstName}})"
                                    className="w-full max-w-[320px]"
                                />
                                <p className="mt-1.5 text-[11px] text-slate-400">
                                    Supports the same {"{{.FirstName}}"} / {"{{.Company}}"} variables as your email copy.
                                </p>
                            </div>
                            <div className="flex flex-wrap items-end gap-4">
                                <div>
                                    <Label>Value (optional)</Label>
                                    <NumberInput
                                        value={action.deal_value ?? 0}
                                        onChange={(n) =>
                                            setAction((a) => ({ ...a, deal_value: n > 0 ? n : undefined }))
                                        }
                                        min={0}
                                        max={1_000_000_000}
                                        className="w-36"
                                    />
                                </div>
                                <div>
                                    <Label>Currency</Label>
                                    <CurrencyPicker
                                        value={action.deal_currency ?? "USD"}
                                        onChange={(c) => setAction((a) => ({ ...a, deal_currency: c }))}
                                    />
                                </div>
                            </div>
                        </>
                    )}
                </div>
            )}

            {action.type === "run_automation" && <RunAutomationFields action={action} setAction={setAction} />}
            {action.type === "fire_event" && <FireEventStepFields action={action} setAction={setAction} />}
        </>
    );
}

// RunAutomationFields — pick an automation to launch + the templated key/value
// inputs passed to it as event data (values render against the contact).
function RunAutomationFields({
    action,
    setAction,
}: {
    action: SequenceAction;
    setAction: React.Dispatch<React.SetStateAction<SequenceAction>>;
}) {
    const { data } = useAutomations();
    const automations = data?.automations ?? [];
    const options: SelectOption[] = automations.map((a) => ({
        value: a.id,
        label: (a.name || "Untitled automation") + (a.enabled ? "" : " · disabled"),
    }));
    const selected = automations.find((a) => a.id === action.automation_id);
    const values = action.automation_values ?? [];
    const setValues = (next: ActionKV[]) => setAction((a) => ({ ...a, automation_values: next }));
    const updateRow = (i: number, patch: Partial<ActionKV>) =>
        setValues(values.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
    const addRow = () => setValues([...values, { key: "", value: "" }]);
    const removeRow = (i: number) => setValues(values.filter((_, idx) => idx !== i));

    return (
        <div className="space-y-4">
            <div>
                <Label>Automation to run</Label>
                <SelectMenu
                    value={action.automation_id ?? ""}
                    onChange={(id) => setAction((a) => ({ ...a, automation_id: id }))}
                    options={options}
                    placeholder={options.length ? "Choose an automation…" : "No automations yet"}
                    className="w-full max-w-[320px]"
                    fullWidth
                />
                <p className="mt-1.5 text-[11px] text-slate-400">
                    Launches the automation's flow for this contact when they reach this step. The automation receives{" "}
                    <span className="font-mono text-slate-500">contact_email</span>,{" "}
                    <span className="font-mono text-slate-500">first_name</span>,{" "}
                    <span className="font-mono text-slate-500">last_name</span>,{" "}
                    <span className="font-mono text-slate-500">company</span>,{" "}
                    <span className="font-mono text-slate-500">campaign_name</span> plus your values below. Reference them as{" "}
                    <span className="font-mono text-slate-500">{"{{.key}}"}</span> in the automation's actions.
                </p>
                {selected && !selected.enabled && (
                    <p className="mt-1.5 rounded-md border border-amber-200 bg-amber-50 px-2 py-1.5 text-[11px] leading-relaxed text-amber-700">
                        This automation is disabled, so this step will be skipped (and logged) until you enable it.
                    </p>
                )}
                {selected && selected.enabled && selected.trigger_event !== "campaign.action" && (
                    <p className="mt-1.5 rounded-md border border-sky-200 bg-sky-50 px-2 py-1.5 text-[11px] leading-relaxed text-sky-700">
                        Built for the "{triggerLabel(selected.trigger_event)}" trigger. It still runs here, but only contact and
                        campaign variables are filled in. Its trigger-specific variables (like{" "}
                        <span className="font-mono">{"{{.invitee_name}}"}</span>) will be empty.
                    </p>
                )}
            </div>

            <div>
                <div className="mb-1.5 flex items-center justify-between gap-2">
                    <Label className="mb-0">Pass values (optional)</Label>
                    <button
                        type="button"
                        onClick={addRow}
                        className="inline-flex h-6 items-center gap-1 rounded-md border border-slate-200 bg-white px-2 text-[11.5px] font-medium text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-900"
                    >
                        <PlusIcon className="w-3 h-3" /> Add value
                    </button>
                </div>
                {values.length === 0 ? (
                    <p className="text-[11px] text-slate-400">
                        Add key/value pairs to pass into the automation. Values support {"{{.FirstName}}"} / {"{{.Company}}"}.
                    </p>
                ) : (
                    <div className="space-y-1.5">
                        {values.map((row, i) => (
                            <div key={i} className="flex items-center gap-1.5">
                                <TextInput
                                    value={row.key}
                                    onChange={(v) => updateRow(i, { key: v })}
                                    placeholder="key"
                                    className="w-28 shrink-0"
                                />
                                <span className="text-slate-300">=</span>
                                <TextInput
                                    value={row.value}
                                    onChange={(v) => updateRow(i, { value: v })}
                                    placeholder="{{.FirstName}}"
                                    className="flex-1 min-w-0"
                                />
                                <DealNameVariableMenu onPick={(token) => updateRow(i, { value: (row.value ?? "") + token })} />
                                <button
                                    type="button"
                                    onClick={() => removeRow(i)}
                                    title="Remove"
                                    className="inline-flex size-6 shrink-0 items-center justify-center rounded text-slate-300 transition-colors hover:bg-rose-50 hover:text-rose-600"
                                >
                                    <Trash2Icon className="w-3.5 h-3.5" />
                                </button>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
}

// FireEventStepFields — publish a custom event to the realtime gateway when the
// lead reaches this step. The developer's app subscribes over the API websocket
// (API key + REALTIME_SUBSCRIBE) and receives { name, payload } — no public URL.
function FireEventStepFields({
    action,
    setAction,
}: {
    action: SequenceAction;
    setAction: React.Dispatch<React.SetStateAction<SequenceAction>>;
}) {
    const fields = action.event_fields ?? [];
    const setFields = (next: ActionKV[]) => setAction((a) => ({ ...a, event_fields: next }));
    const updateRow = (i: number, patch: Partial<ActionKV>) => setFields(fields.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
    const addRow = () => setFields([...fields, { key: "", value: "" }]);
    const removeRow = (i: number) => setFields(fields.filter((_, idx) => idx !== i));

    return (
        <div className="space-y-3">
            <div>
                <Label>Event name</Label>
                <TextInput
                    value={action.event_name ?? ""}
                    onChange={(v) => setAction((a) => ({ ...a, event_name: v }))}
                    placeholder="lead.reached_step"
                    className="w-full font-mono"
                />
                <p className="mt-1 text-[11px] text-slate-400">What your app subscribes to over the realtime websocket.</p>
            </div>
            <div>
                <div className="mb-1.5 flex items-center justify-between gap-2">
                    <Label className="mb-0">Payload (optional)</Label>
                    <button
                        type="button"
                        onClick={addRow}
                        className="inline-flex h-6 items-center gap-1 rounded-md border border-slate-200 bg-white px-2 text-[11.5px] font-medium text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-900"
                    >
                        <PlusIcon className="w-3 h-3" /> Add field
                    </button>
                </div>
                {fields.length === 0 ? (
                    <p className="text-[11px] text-slate-400">Add key/value fields. Values support {"{{.FirstName}}"} / {"{{.Company}}"}.</p>
                ) : (
                    <div className="space-y-1.5">
                        {fields.map((row, i) => (
                            <div key={i} className="flex items-center gap-1.5">
                                <TextInput value={row.key} onChange={(v) => updateRow(i, { key: v })} placeholder="field" className="w-28 shrink-0 font-mono" />
                                <span className="text-slate-300">=</span>
                                <TextInput value={row.value} onChange={(v) => updateRow(i, { value: v })} placeholder="{{.Email}}" className="flex-1 min-w-0 font-mono" />
                                <button
                                    type="button"
                                    onClick={() => removeRow(i)}
                                    title="Remove"
                                    className="inline-flex size-6 shrink-0 items-center justify-center rounded text-slate-300 transition-colors hover:bg-rose-50 hover:text-rose-600"
                                >
                                    <XIcon className="w-3.5 h-3.5" />
                                </button>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
}

// The guarded set of reversible actions a campaign AI agent step may call.
// Mirrors the backend isCampaignAgentAction allowlist; never a send/reply.
const CAMPAIGN_AI_ALLOWLIST: SequenceActionType[] = [
    "add_tag",
    "remove_tag",
    "label_email",
    "unsubscribe",
    "create_task",
    "create_deal",
    "move_deal_stage",
];

// Actions the agent fully drives from the instruction (no pinned config): it
// writes the task title / deal name and picks a pipeline/stage itself.
const CAMPAIGN_AI_NO_CONFIG: SequenceActionType[] = [
    "unsubscribe",
    "create_task",
    "create_deal",
    "move_deal_stage",
];

// Which optional tag/label pool an action configures (add_tag/remove_tag/label).
const CAMPAIGN_AI_POOL_KEY: Partial<Record<SequenceActionType, "ai_add_tags" | "ai_remove_tags" | "ai_labels">> = {
    add_tag: "ai_add_tags",
    remove_tag: "ai_remove_tags",
    label_email: "ai_labels",
};

// TagPoolField — a multi-select pool of tags the agent may add/remove. The
// display name is stored alongside the id so the backend can offer the tag to
// the model and resolve its pick without a category lookup.
function TagPoolField({
    label,
    value,
    onChange,
}: {
    label: string;
    value: AITagRef[];
    onChange: (refs: AITagRef[]) => void;
}) {
    const { user } = useUserProfile();
    const titleById = React.useMemo(() => {
        const m = new Map<string, string>();
        for (const c of user.categories ?? []) m.set(c.id, c.title);
        return m;
    }, [user.categories]);
    const knownName = React.useMemo(() => new Map(value.map((r) => [r.id, r.name])), [value]);
    return (
        <div>
            <Label>{label}</Label>
            <CategoryPicker
                value={value.map((r) => r.id)}
                onChange={(ids) => onChange(ids.map((id) => ({ id, name: titleById.get(id) ?? knownName.get(id) ?? id })))}
                placeholder="Any — leave empty to let the agent choose"
            />
            <p className="mt-1.5 text-[11px] text-slate-400">
                {value.length
                    ? "The agent chooses among these for each contact."
                    : "Empty, so the agent may use any of your labels for each contact."}
            </p>
        </div>
    );
}

// AIStepFields — the campaign AI agent step. An instruction plus the reversible
// actions the model may call; the agent decides which to run per contact. When
// an enabled action needs a target (which tag, which pipeline), that config
// sits inline under its row (one accent rule, not a nested card) so the editor
// stays flat.
function AIStepFields({
    action,
    setAction,
}: {
    action: SequenceAction;
    setAction: React.Dispatch<React.SetStateAction<SequenceAction>>;
}) {
    const enabled = action.ai_allowed_actions ?? [];
    const toggle = (id: string) =>
        setAction((a) => {
            const cur = a.ai_allowed_actions ?? [];
            return { ...a, ai_allowed_actions: cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id] };
        });
    // Whether any enabled tag/label capability has an empty pool (agent may use
    // any tag) — only then does the "create new tags" opt-in matter.
    const anyOpenPool = (["add_tag", "remove_tag", "label_email"] as SequenceActionType[]).some(
        (id) => enabled.includes(id) && !((action[CAMPAIGN_AI_POOL_KEY[id]!] ?? []).length),
    );
    return (
        <div className="space-y-4">
            <div>
                <Label>Instruction</Label>
                <textarea
                    value={action.ai_instruction ?? ""}
                    onChange={(e) => setAction((a) => ({ ...a, ai_instruction: e.target.value }))}
                    rows={3}
                    placeholder="Read the reply. If they ask about pricing, label them 'pricing' and create a follow-up task."
                    className="w-full resize-y rounded-md border border-slate-200 px-2.5 py-1.5 text-[12.5px] text-slate-700 focus:border-sky-400 focus:outline-none focus:ring-2 focus:ring-sky-100"
                />
            </div>
            <div>
                <div className="mb-1.5 text-[10px] font-semibold uppercase tracking-[0.14em] text-slate-400">
                    Actions the agent can take
                </div>
                <div className="divide-y divide-slate-100">
                    {CAMPAIGN_AI_ALLOWLIST.map((id) => {
                        const on = enabled.includes(id);
                        const Icon = ACTION_META[id]?.Icon ?? SparklesIcon;
                        const hasConfig = !CAMPAIGN_AI_NO_CONFIG.includes(id);
                        return (
                            <div key={id} className="py-1">
                                <button
                                    type="button"
                                    onClick={() => toggle(id)}
                                    className="flex w-full items-center gap-2 rounded px-1.5 py-1.5 text-left text-[12.5px] text-slate-700 transition-colors hover:bg-slate-50"
                                >
                                    <span
                                        className={`inline-flex size-4 shrink-0 items-center justify-center rounded border ${
                                            on ? "border-sky-500 bg-sky-500 text-white" : "border-slate-300 bg-white"
                                        }`}
                                    >
                                        {on && <CheckIcon className="w-3 h-3" />}
                                    </span>
                                    <Icon className={`w-3.5 h-3.5 ${ACTION_META[id]?.tint ?? "text-slate-400"}`} />
                                    <span className={on ? "font-medium text-slate-800" : ""}>
                                        {ACTION_META[id]?.label ?? id}
                                    </span>
                                </button>
                                {on && hasConfig && CAMPAIGN_AI_POOL_KEY[id] && (
                                    <div className="ml-[1.35rem] mt-1 space-y-3 border-l border-slate-200 pl-3 pb-1.5">
                                        <TagPoolField
                                            label={
                                                id === "add_tag"
                                                    ? "Labels the agent can add"
                                                    : id === "remove_tag"
                                                      ? "Labels the agent can remove"
                                                      : "Conversation labels the agent can apply"
                                            }
                                            value={action[CAMPAIGN_AI_POOL_KEY[id]!] ?? []}
                                            onChange={(refs) =>
                                                setAction((a) => ({ ...a, [CAMPAIGN_AI_POOL_KEY[id]!]: refs }))
                                            }
                                        />
                                    </div>
                                )}
                            </div>
                        );
                    })}
                </div>
                {anyOpenPool && (
                    <button
                        type="button"
                        onClick={() => setAction((a) => ({ ...a, ai_allow_create_tags: !a.ai_allow_create_tags }))}
                        className="mt-2 flex w-full items-center gap-2 rounded px-1.5 py-1.5 text-left text-[12px] text-slate-600 transition-colors hover:bg-slate-50"
                    >
                        <span
                            className={`inline-flex size-4 shrink-0 items-center justify-center rounded border ${
                                action.ai_allow_create_tags ? "border-sky-500 bg-sky-500 text-white" : "border-slate-300 bg-white"
                            }`}
                        >
                            {action.ai_allow_create_tags && <CheckIcon className="w-3 h-3" />}
                        </span>
                        Let the agent create a new label when none fits
                    </button>
                )}
                <p className="mt-2 text-[11px] leading-relaxed text-slate-400">
                    The agent decides which of these to use for each contact, writes any task or deal details itself, and
                    can chain several. It only takes the actions you enable, never sending or replying. Each step it takes
                    is charged to the workspace balance by what it uses, usually {fmtUsdAbout(AI_ESTIMATE_USD.agentIteration)}.
                </p>
            </div>
        </div>
    );
}

// SwitchStepFields — the multi-way router. Configure the case names (each one
// becomes its own column under the step) and the decider: an AI prompt over the
// contact's data, or a template value matched against the case names.
function SwitchStepFields({
    action,
    setAction,
}: {
    action: SequenceAction;
    setAction: React.Dispatch<React.SetStateAction<SequenceAction>>;
}) {
    const aiMode = action.switch_on !== "value";
    const cases = action.switch_cases ?? [];
    const setCases = (next: string[]) => setAction((a) => ({ ...a, switch_cases: next }));
    return (
        <div className="space-y-4">
            <div>
                <Label>Decided by</Label>
                <div className="grid grid-cols-2 gap-1.5">
                    {(
                        [
                            {
                                mode: true,
                                Icon: SparklesIcon,
                                title: "AI prompt",
                                detail: `A model reads the contact and picks a case. ${fmtUsdAbout(AI_ESTIMATE_USD.aiNode)} per contact.`,
                            },
                            {
                                mode: false,
                                Icon: BracesIcon,
                                title: "Value",
                                detail: "A field or template is matched to the cases. Free, deterministic.",
                            },
                        ] as const
                    ).map(({ mode, Icon, title, detail }) => {
                        const active = aiMode === mode;
                        return (
                            <button
                                key={title}
                                type="button"
                                onClick={() => setAction((a) => ({ ...a, switch_on: mode ? "ai" : "value" }))}
                                className={`flex items-start gap-1.5 rounded-md border px-2 py-1.5 text-left transition-colors ${
                                    active
                                        ? "border-purple-300 bg-purple-50"
                                        : "border-slate-200 bg-white hover:border-slate-300"
                                }`}
                            >
                                <Icon className={`mt-0.5 w-3.5 h-3.5 shrink-0 ${active ? "text-purple-600" : "text-slate-400"}`} />
                                <span className="min-w-0">
                                    <span className={`block text-[11.5px] font-medium ${active ? "text-purple-700" : "text-slate-700"}`}>
                                        {title}
                                    </span>
                                    <span className="block text-[10.5px] leading-snug text-slate-400">{detail}</span>
                                </span>
                            </button>
                        );
                    })}
                </div>
            </div>

            {aiMode ? (
                <div>
                    <div className="mb-1.5 flex items-center justify-between gap-2">
                        <Label className="mb-0">Tell AI how to route</Label>
                        <DealNameVariableMenu
                            onPick={(token) => setAction((a) => ({ ...a, ai_instruction: (a.ai_instruction ?? "") + token }))}
                        />
                    </div>
                    <textarea
                        value={action.ai_instruction ?? ""}
                        onChange={(e) => setAction((a) => ({ ...a, ai_instruction: e.target.value }))}
                        rows={4}
                        maxLength={4000}
                        placeholder={
                            "Read this contact's reply and company details. Decide whether they're interested, not ready yet, or the wrong person."
                        }
                        className="w-full px-2.5 py-1.5 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-purple-400 focus:ring-2 focus:ring-purple-100 resize-y leading-relaxed"
                    />
                    <p className="mt-1 text-[11px] text-slate-400">
                        One model call per contact reaching this step picks exactly one case. Supports the same{" "}
                        {"{{.FirstName}}"} / {"{{.Company}}"} variables as your email copy. Costs {fmtUsdAbout(AI_ESTIMATE_USD.aiNode)} per
                        contact, more on long calls.
                    </p>
                    <div className="mt-3">
                        <Label>Capabilities</Label>
                        <div className="space-y-1">
                            <AIContextToggle
                                label="Web search"
                                detail={`Looks up the contact's company on the web before deciding. Adds ${fmtUsdAbout(AI_ESTIMATE_USD.webSearch)} when results are found`}
                                on={!!action.ai_web_search}
                                onToggle={() => setAction((a) => ({ ...a, ai_web_search: !a.ai_web_search || undefined }))}
                            />
                            <AIContextToggle
                                label="Extended thinking"
                                detail="Uses the stronger model with a bigger reasoning budget. Costs more through usage metering"
                                on={!!action.ai_thinking}
                                onToggle={() => setAction((a) => ({ ...a, ai_thinking: !a.ai_thinking || undefined }))}
                            />
                        </div>
                    </div>
                </div>
            ) : (
                <div>
                    <div className="mb-1.5 flex items-center justify-between gap-2">
                        <Label className="mb-0">Value to match</Label>
                        <DealNameVariableMenu
                            onPick={(token) => setAction((a) => ({ ...a, switch_value: (a.switch_value ?? "") + token }))}
                        />
                    </div>
                    <TextInput
                        value={action.switch_value ?? ""}
                        onChange={(v) => setAction((a) => ({ ...a, switch_value: v.slice(0, 500) }))}
                        placeholder="e.g. {{.Industry}}"
                        className="w-full font-mono"
                    />
                    <p className="mt-1 text-[11px] text-slate-400">
                        Rendered per contact and matched to the case names. Matching ignores casing and extra spaces
                        (“ VIP  Customer” matches the case “vip customer”); wrap a case in slashes for a regex, e.g.{" "}
                        <code className="font-mono">/^(vip|enterprise)/</code>. First matching case wins. No model call,
                        no charge.
                    </p>
                </div>
            )}

            <div>
                <div className="mb-1.5 flex items-center justify-between gap-2">
                    <Label className="mb-0">Cases</Label>
                    <button
                        type="button"
                        onClick={() => {
                            if (cases.length >= 20) return;
                            setCases([...cases, ""]);
                        }}
                        className="inline-flex h-6 items-center gap-1 rounded-md border border-slate-200 bg-white px-2 text-[11.5px] font-medium text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-900"
                    >
                        <PlusIcon className="w-3 h-3" /> Add case
                    </button>
                </div>
                {cases.length === 0 ? (
                    <p className="text-[11px] text-slate-400">
                        Each case becomes its own column under the step.
                    </p>
                ) : (
                    <div className="space-y-1.5">
                        {cases.map((row, i) => (
                            <div key={i} className="flex items-center gap-1.5">
                                <TextInput
                                    value={row}
                                    onChange={(v) => setCases(cases.map((x, idx) => (idx === i ? v.slice(0, 80) : x)))}
                                    placeholder="e.g. interested"
                                    className="flex-1 min-w-0"
                                />
                                <button
                                    type="button"
                                    onClick={() => setCases(cases.filter((_, idx) => idx !== i))}
                                    title="Remove case"
                                    className="inline-flex size-6 shrink-0 items-center justify-center rounded text-slate-300 transition-colors hover:bg-rose-50 hover:text-rose-600"
                                >
                                    <XIcon className="w-3.5 h-3.5" />
                                </button>
                            </div>
                        ))}
                    </div>
                )}
            </div>

            {aiMode && (
                <div>
                    <Label>What AI can see</Label>
                    <div className="space-y-1">
                        <AIContextToggle
                            label="Campaign history"
                            detail="Which steps ran, opens, clicks, replies, and earlier outcomes"
                            on={!action.ai_no_engagement}
                            onToggle={() => setAction((a) => ({ ...a, ai_no_engagement: !a.ai_no_engagement || undefined }))}
                        />
                        <AIContextToggle
                            label="Incoming email"
                            detail="The newest email received from the contact (subject + preview)"
                            on={!action.ai_no_replies}
                            onToggle={() => setAction((a) => ({ ...a, ai_no_replies: !a.ai_no_replies || undefined }))}
                        />
                    </div>
                    <p className="mt-1 text-[11px] text-slate-400">Contact fields are always included.</p>
                </div>
            )}

            <p className="rounded-md bg-slate-50 px-2.5 py-2 text-[11px] leading-relaxed text-slate-600 ring-1 ring-slate-200">
                Every case gets its own column under the step, and Otherwise catches contacts no case matched. Put
                normal action steps (label, deal, task) under a case to make things happen for the contacts routed down it.
            </p>
        </div>
    );
}

// AIContextToggle — one row of the switch step's "what AI can see" section.
function AIContextToggle({
    label,
    detail,
    on,
    onToggle,
}: {
    label: string;
    detail: string;
    on: boolean;
    onToggle: () => void;
}) {
    return (
        <div
            className={`flex items-center gap-2 rounded-md px-2 py-1.5 ring-1 transition-colors ${
                on ? "bg-purple-50 ring-purple-200" : "bg-slate-50 ring-slate-200"
            }`}
        >
            <div className="min-w-0 flex-1">
                <div className={`text-[11.5px] font-medium ${on ? "text-purple-700" : "text-slate-500"}`}>{label}</div>
                <div className="text-[10.5px] text-slate-400">{detail}</div>
            </div>
            <button
                type="button"
                role="switch"
                aria-checked={on}
                aria-label={label}
                onClick={onToggle}
                className={`relative inline-flex h-[18px] w-8 shrink-0 items-center rounded-full transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-300 ${
                    on ? "bg-purple-600" : "bg-slate-300"
                }`}
            >
                <span
                    className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow-sm transition-transform duration-200 ${
                        on ? "translate-x-[15px]" : "translate-x-[2px]"
                    }`}
                />
            </button>
        </div>
    );
}

// DealNameVariableMenu — a compact "insert variable" trigger for the deal-name
// field, mirroring the VariableMenu affordance the email subject/body use.
// Appends a {{.Token}} into the deal name so it can personalize per contact.
function DealNameVariableMenu({ onPick }: { onPick: (token: string) => void }) {
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(open, () => setOpen(false), ref);
    return (
        <div ref={ref} className="relative">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                title="Insert a personalization variable"
                className="inline-flex h-7 items-center gap-1 rounded px-1.5 text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900"
            >
                <BracesIcon className="w-3.5 h-3.5" />
                <ChevronDownIcon className="w-3 h-3" />
            </button>
            {open && (
                <div className="absolute right-0 top-full z-30 mt-1 w-44 overflow-hidden rounded-md border border-slate-200 bg-white py-1 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]">
                    {DEAL_NAME_VARIABLES.map((v) => (
                        <button
                            key={v}
                            type="button"
                            onClick={() => {
                                onPick(v);
                                setOpen(false);
                            }}
                            className="flex w-full items-center px-2.5 py-1.5 text-left font-mono text-[11.5px] text-slate-700 transition-colors hover:bg-slate-100"
                        >
                            {v}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}

// CurrencyPicker — a small themed dropdown for the deal currency (ISO code).
// Defaults to USD; the value persisted is the bare ISO code (e.g. "USD").
const DEAL_CURRENCIES = ["USD", "EUR", "GBP", "CAD", "AUD", "JPY", "CHF", "SEK", "INR", "BRL"];

function CurrencyPicker({ value, onChange }: { value: string; onChange: (c: string) => void }) {
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(open, () => setOpen(false), ref);
    return (
        <div ref={ref} className="relative inline-flex">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                className="inline-flex h-7 w-24 items-center gap-1.5 rounded-md border border-slate-200 bg-white px-2.5 text-[12px] text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-900"
            >
                <span className="flex-1 truncate text-left">{value || "USD"}</span>
                <ChevronDownIcon className="w-3 h-3 text-slate-400" />
            </button>
            {open && (
                <div className="absolute left-0 top-full z-30 mt-1 max-h-56 w-24 overflow-y-auto rounded-md border border-slate-200 bg-white py-1 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]">
                    {DEAL_CURRENCIES.map((c) => (
                        <button
                            key={c}
                            type="button"
                            onClick={() => {
                                onChange(c);
                                setOpen(false);
                            }}
                            className={`flex w-full items-center px-2.5 py-1.5 text-left text-[12px] transition-colors hover:bg-slate-100 ${
                                c === value ? "font-medium text-slate-900" : "text-slate-700"
                            }`}
                        >
                            {c}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}
