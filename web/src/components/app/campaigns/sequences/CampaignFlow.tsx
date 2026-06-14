// Visual flow canvas for a campaign's steps (React Flow) — a branching tree
// where each condition is its own "IF" block.
//
// SHAPE
//   Step ──▶ [ IF opened within 3d ] ──▶ Step …   (the branch's steps flow below
//   its IF block). An unconditional path is a plain line straight to the step.
//
// BUILD
// - Drag from a STEP's bottom dot → a plain "just go there" connection.
// - Drag from an IF block's SIDE dots → a new "if" branch from that step
//   (onto a step, or empty for a new step). Drag the IF block's BOTTOM dot →
//   change where that if leads.
// - Click a step → edit its email. Click an IF block → edit its condition.
// - Nothing connects automatically; a step with no outgoing path ends in STOP.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowRightLeftIcon,
    BellOffIcon,
    AlertTriangleIcon,
    BracesIcon,
    CheckSquareIcon,
    ChevronDownIcon,
    ChevronUpIcon,
    ClockIcon,
    FlagIcon,
    GitBranchIcon,
    HandshakeIcon,
    InfoIcon,
    Loader2Icon,
    MailIcon,
    PlusIcon,
    SendIcon,
    TagIcon,
    TagsIcon,
    Trash2Icon,
    UnlinkIcon,
    XIcon,
    ZapIcon,
} from "lucide-react";
import {
    ReactFlow,
    Background,
    BaseEdge,
    Controls,
    EdgeLabelRenderer,
    Panel,
    Handle,
    MarkerType,
    Position,
    getBezierPath,
    useNodesState,
    useEdgesState,
    type Node,
    type Edge,
    type Connection,
    type NodeProps,
    type EdgeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import dagre from "@dagrejs/dagre";
import { useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import type { SequenceBranch, BranchCondition, BranchField } from "@/lib/api/models/app/campaigns/sequences/Branching";
import { BRANCH_FIELD_LABELS, isReplyBranchField, isInstantCapableField } from "@/lib/api/models/app/campaigns/sequences/Branching";
import useSequences from "@/lib/api/hooks/app/campaigns/sequences/useSequences";
import useCreateSequence from "@/lib/api/hooks/app/campaigns/sequences/useCreateSequence";
import useDeleteSequence from "@/lib/api/hooks/app/campaigns/sequences/useDeleteSequence";
import updateSequence from "@/lib/api/client/app/campaigns/sequences/updateSequence";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import useUpdateCampaign from "@/lib/api/hooks/app/campaigns/useUpdateCampaign";
import { useConfirm } from "@/hooks/context/confirm";
import useClickOutside from "@/hooks/useClickOutside";
import { usePermission, showPermissionDenied } from "@/hooks/usePermission";
import { LockIcon } from "lucide-react";
import { NumberInput, Label, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { PopoverMenu, PopoverMenuContent, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import StepEmailArms from "./StepEmailArms";
import CategoryPicker from "@/components/app/contacts/CategoryPicker";
import type { ActionKV, SequenceAction, SequenceActionType } from "@/lib/api/models/app/campaigns/sequences/Action";
import { useAutomations } from "@/lib/api/hooks/app/automations/useAutomations";
import { triggerLabel } from "@/lib/api/models/app/automations/meta";
import TaskTypePicker from "@/components/app/crm/TaskTypePicker";
import AssigneeTeamPicker, { type AssigneeValue } from "@/components/app/crm/AssigneeTeamPicker";
import DealStagePicker from "@/components/app/crm/DealStagePicker";

// Personalization tokens available in templated copy. Mirrors SequenceView's
// VARIABLES so a deal name can use the same {{.FirstName}}/{{.Company}} tokens
// every other campaign copy field does.
const DEAL_NAME_VARIABLES = ["{{.FirstName}}", "{{.LastName}}", "{{.Company}}", "{{.Email}}"];

const STOP_ID = "__stop__";
const IF_PREFIX = "if-";
const NODE_W = 248;
const NODE_H = 92;
const MAX_STEPS = 50;
const SEQ_KEY = (id: string) => ["campaigns", id, "sequences"] as const;

const ifNodeId = (branchId: string) => `${IF_PREFIX}${branchId}`;
const isIfId = (id: string) => id.startsWith(IF_PREFIX);

function newBranchId(): string {
    try {
        return crypto.randomUUID();
    } catch {
        return `b_${Math.floor(performance.now())}_${Math.random().toString(36).slice(2, 8)}`;
    }
}

const isCond = (b: SequenceBranch) => (b.conditions?.length ?? 0) > 0;
const stepName = (s: Sequence | undefined) => (s?.name?.trim() ? s.name : "Untitled step");

// "Positive" reply fields are the ones that route a contact INTO a reply flow
// (a human reply, or a classified reply intent). Mirrors the backend's
// branchHasPositiveReplyCondition. not_replied is excluded — that is the cold
// sequence's "didn't reply" continuation, not reply handling. Used to decide
// whether a campaign has any reply handling at all (the stop-on-reply warning).
const POSITIVE_REPLY_FIELDS: BranchField[] = [
    "replied",
    "reply_positive",
    "reply_negative",
    "reply_neutral",
    "reply_automated",
];
const isPositiveReplyField = (f: BranchField) => POSITIVE_REPLY_FIELDS.includes(f);

// A branch is "instant" when its condition is an instant-capable signal
// (reply intent, opened, or clicked) and the per-branch toggle isn't opted out.
// Such a branch's action chain fires the moment the event lands (reply recorded,
// or open / click tracked), so the target step's wait_after is irrelevant.
function isInstantBranch(b: SequenceBranch): boolean {
    return (b.conditions ?? []).some((c) => isInstantCapableField(c.field)) && b.instant !== false;
}

// Conditions first-match; unconditional ("just go there") paths are the fallback.
function ordered(branches: SequenceBranch[]): SequenceBranch[] {
    return [...branches.filter(isCond), ...branches.filter((b) => !isCond(b))];
}

function conditionText(b: SequenceBranch): string {
    return (b.conditions ?? [])
        .map((c) => {
            if (c.field === "random") return `${c.value ?? 50}% random`;
            const f = BRANCH_FIELD_LABELS[c.field] ?? c.field;
            // Reply-class conditions are "ever" (no day window).
            if (isReplyBranchField(c.field)) return f;
            return `${f} within ${c.value ?? 3}d`;
        })
        .join(" + ");
}

function layoutGraph(nodes: Node[], edges: Edge[]): Node[] {
    const g = new dagre.graphlib.Graph();
    g.setDefaultEdgeLabel(() => ({}));
    g.setGraph({ rankdir: "TB", nodesep: 180, ranksep: 130, marginx: 32, marginy: 32, edgesep: 100 });
    nodes.forEach((n) => {
        let w = NODE_W;
        let h = NODE_H;
        if (n.id === STOP_ID) {
            w = 96;
            h = 40;
        } else if (isIfId(n.id)) {
            w = 210;
            h = 40;
        }
        g.setNode(n.id, { width: w, height: h });
    });
    edges.forEach((e) => {
        const text = typeof e.label === "string" ? e.label : "";
        // Keep the if / else-if spine (in / chain / else edges) straight and
        // aligned; let the "then" steps fan out to the side.
        const spine = e.id.startsWith("in-") || e.id.startsWith("chain-") || e.id.startsWith("else-");
        const label = text ? { width: Math.min(160, text.length * 6 + 24), height: 30, labelpos: "c" } : {};
        g.setEdge(e.source, e.target, { ...label, weight: spine ? 6 : 1 });
    });
    dagre.layout(g);
    return nodes.map((n) => {
        const p = g.node(n.id);
        return p ? { ...n, position: { x: p.x - p.width / 2, y: p.y - p.height / 2 } } : n;
    });
}

// dagre can pile disconnected pieces on top of each other at the origin, which
// hides orphaned steps (an upstream step was deleted) so they can't be clicked,
// deleted, or connected. Split the graph into connected components and stack
// them in vertical bands — the main flow (with the entry) first, orphans below.
function stackComponents(nodes: Node[], edges: Edge[]): Node[] {
    const adj = new Map<string, string[]>();
    const link = (a: string, b: string) => {
        const list = adj.get(a) ?? [];
        list.push(b);
        adj.set(a, list);
    };
    for (const e of edges) {
        link(e.source, e.target);
        link(e.target, e.source);
    }
    const comp = new Map<string, number>();
    let count = 0;
    for (const n of nodes) {
        if (comp.has(n.id)) continue;
        const queue = [n.id];
        comp.set(n.id, count);
        while (queue.length) {
            const id = queue.shift()!;
            for (const m of adj.get(id) ?? []) {
                if (!comp.has(m)) {
                    comp.set(m, count);
                    queue.push(m);
                }
            }
        }
        count++;
    }
    if (count <= 1) return nodes; // a single connected flow needs no banding

    const box = new Map<number, { minX: number; minY: number; maxY: number }>();
    for (const n of nodes) {
        const k = comp.get(n.id)!;
        const h = n.id === STOP_ID ? 40 : NODE_H;
        const b = box.get(k) ?? { minX: Infinity, minY: Infinity, maxY: -Infinity };
        b.minX = Math.min(b.minX, n.position.x);
        b.minY = Math.min(b.minY, n.position.y);
        b.maxY = Math.max(b.maxY, n.position.y + h);
        box.set(k, b);
    }
    const baseX = Math.min(...[...box.values()].map((b) => b.minX));
    const GAP = 130;
    let cursorY = 0;
    const offset = new Map<number, { dx: number; dy: number }>();
    for (const k of [...box.keys()].sort((a, b) => a - b)) {
        const b = box.get(k)!;
        offset.set(k, { dx: baseX - b.minX, dy: cursorY - b.minY });
        cursorY += b.maxY - b.minY + GAP;
    }
    return nodes.map((n) => {
        const o = offset.get(comp.get(n.id)!)!;
        return { ...n, position: { x: n.position.x + o.dx, y: n.position.y + o.dy } };
    });
}

// ── Custom nodes ────────────────────────────────────────────────────────────
type StepNodeData = {
    label: string;
    subtitle: string;
    isStart: boolean;
    endsHere: boolean;
    orphan: boolean;
    onDelete: () => void;
    onAddReply: () => void;
};

function StepNode({ data, selected }: NodeProps) {
    const d = data as StepNodeData;
    return (
        <div
            className={`w-[248px] rounded-xl border bg-white shadow-sm transition-shadow duration-200 hover:shadow-md ${
                d.orphan ? "border-dashed border-amber-300" : "border-slate-200"
            } ${selected ? "border-sky-400 ring-2 ring-sky-100" : ""}`}
        >
            <Handle type="target" position={Position.Top} className="!h-2 !w-2 !border-2 !border-white !bg-slate-300" />
            <div className="flex items-center gap-2 rounded-t-xl border-b border-slate-200/70 bg-gradient-to-r from-sky-50/80 to-white px-2.5 py-1.5">
                <span className="inline-flex size-5 shrink-0 items-center justify-center rounded-md bg-sky-100 text-sky-600 ring-1 ring-sky-200/70">
                    <MailIcon className="w-3 h-3" />
                </span>
                <span className="min-w-0 flex-1 truncate text-[12.5px] font-semibold text-slate-800">
                    {d.label || "Untitled step"}
                </span>
                {d.isStart && (
                    <span className="shrink-0 rounded bg-sky-600 px-1.5 py-px text-[9px] font-semibold uppercase tracking-[0.12em] text-white">
                        Start
                    </span>
                )}
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        d.onDelete();
                    }}
                    title="Delete step"
                    className="nodrag inline-flex size-5 shrink-0 items-center justify-center rounded text-slate-300 transition-colors hover:bg-rose-50 hover:text-rose-600"
                >
                    <Trash2Icon className="w-3 h-3" />
                </button>
            </div>
            <div className="px-2.5 py-2">
                <div className="text-[9.5px] font-semibold uppercase tracking-[0.12em] text-slate-300">Email</div>
                <div className="mt-0.5 truncate text-[11.5px] text-slate-500">{d.subtitle || "No subject yet"}</div>
            </div>
            {d.orphan ? (
                <div className="flex items-center gap-1 border-t border-amber-200/70 px-2.5 py-1 text-[10.5px] text-amber-600">
                    <UnlinkIcon className="w-3 h-3" />
                    Not connected — drag a link in
                </div>
            ) : d.endsHere ? (
                <div className="flex items-center gap-1 border-t border-slate-200/70 px-2.5 py-1 text-[10.5px] text-slate-400">
                    <FlagIcon className="w-3 h-3 text-slate-400" />
                    Ends here
                </div>
            ) : null}
            {/* Explicit, discoverable way to build a reply-triggered step (the
                reply branch is otherwise only reachable via a connection's
                condition dropdown). Creates an action step that fires instantly
                on reply. */}
            <button
                type="button"
                onClick={(e) => {
                    e.stopPropagation();
                    d.onAddReply();
                }}
                title="Create a step that runs the moment the contact replies"
                className="nodrag flex w-full items-center gap-1.5 border-t border-slate-200/70 px-2.5 py-1.5 text-[10.5px] font-medium text-violet-600 transition-colors hover:bg-violet-50"
            >
                <ZapIcon className="w-3 h-3" />
                On reply
            </button>
            {/* One output dot: drag it out to add the next step, action, or condition. */}
            <Handle type="source" id="s" position={Position.Bottom} className="!h-3 !w-3 pointer-coarse:!h-5 pointer-coarse:!w-5 !border-2 !border-white !bg-sky-500" />
        </div>
    );
}

type IfNodeData = { label: string; instant: boolean; onDelete: () => void };

function IfNode({ data, selected }: NodeProps) {
    const d = data as IfNodeData;
    // Instant-capable branches (reply intent, opened, clicked) fire the moment
    // the event lands, not at the next scheduled step. A violet "instant" badge
    // distinguishes them from the branches checked at the next step boundary
    // (negative "didn't" signals, within-N-days windows, random / always). The
    // whole node carries a tooltip explaining it.
    return (
        <div
            title={
                d.instant
                    ? "Runs the moment it happens (reply / open / click), not at the next scheduled step."
                    : undefined
            }
            className={`rounded-lg border bg-gradient-to-b from-sky-50 to-white px-2 py-1 shadow-sm transition-shadow duration-200 hover:shadow-md ${
                selected ? "border-sky-400 ring-2 ring-sky-100" : d.instant ? "border-violet-200" : "border-sky-200"
            }`}
        >
            <Handle type="target" position={Position.Top} className="!h-2 !w-2 !border-2 !border-white !bg-slate-300" />
            {/* Right dot = the THEN path: where this if leads (drag to change). */}
            <Handle type="source" id="out" position={Position.Right} className="!h-3 !w-3 pointer-coarse:!h-5 pointer-coarse:!w-5 !border-2 !border-white !bg-sky-500" />
            <div className="flex items-center gap-1.5">
                <GitBranchIcon className="w-3 h-3 shrink-0 text-sky-600" />
                <span className="text-[9.5px] font-semibold uppercase tracking-[0.12em] text-sky-500">if</span>
                <span className="max-w-[150px] truncate text-[11px] font-medium text-sky-800">{d.label}</span>
                {d.instant && (
                    <span className="inline-flex shrink-0 items-center gap-0.5 rounded bg-violet-50 px-1 py-px text-[8.5px] font-semibold uppercase tracking-[0.1em] text-violet-600 ring-1 ring-violet-200/70">
                        <ZapIcon className="w-2.5 h-2.5" />
                        instant
                    </span>
                )}
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        d.onDelete();
                    }}
                    title="Delete this branch"
                    className="nodrag inline-flex size-4 items-center justify-center rounded text-sky-400 hover:bg-rose-50 hover:text-rose-600"
                >
                    <Trash2Icon className="w-3 h-3" />
                </button>
            </div>
            {/* Bottom dot = the ELSE path: drag to add the next condition (else-if). */}
            <Handle type="source" id="else" position={Position.Bottom} className="!h-3 !w-3 pointer-coarse:!h-5 pointer-coarse:!w-5 !border-2 !border-white !bg-slate-400" />
        </div>
    );
}

function StopNode() {
    return (
        <div className="inline-flex items-center gap-1.5 rounded-md border border-rose-200 bg-rose-50 px-3 py-1.5 text-[11.5px] font-medium text-rose-600">
            <Handle type="target" position={Position.Top} className="!h-2 !w-2 !bg-rose-300" />
            <FlagIcon className="w-3 h-3" />
            Stop
        </div>
    );
}

// Per-type chrome for action nodes (icon + label + accent).
const ACTION_META: Record<string, { label: string; Icon: typeof ClockIcon; tint: string }> = {
    add_tag: { label: "Add tag", Icon: TagIcon, tint: "text-emerald-600" },
    remove_tag: { label: "Remove tag", Icon: TagIcon, tint: "text-amber-600" },
    label_email: { label: "Label email", Icon: TagsIcon, tint: "text-fuchsia-600" },
    create_task: { label: "Create task", Icon: CheckSquareIcon, tint: "text-violet-600" },
    create_deal: { label: "Create deal", Icon: HandshakeIcon, tint: "text-emerald-600" },
    move_deal_stage: { label: "Move deal stage", Icon: ArrowRightLeftIcon, tint: "text-sky-600" },
    unsubscribe: { label: "Unsubscribe", Icon: BellOffIcon, tint: "text-rose-600" },
    run_automation: { label: "Run automation", Icon: ZapIcon, tint: "text-indigo-600" },
    http_request: { label: "HTTP request", Icon: BracesIcon, tint: "text-teal-600" },
    fire_event: { label: "Fire event", Icon: SendIcon, tint: "text-sky-600" },
};

// actionSummary is the one-line subtitle shown on an action node.
function actionSummary(a?: SequenceAction | null): string {
    if (!a) return "Not configured";
    switch (a.type) {
        case "add_tag":
            return a.category_id ? "Add a tag" : "Pick a tag…";
        case "remove_tag":
            return a.category_id ? "Remove a tag" : "Pick a tag…";
        case "label_email":
            return a.label_ids && a.label_ids.length ? "Label the conversation" : "Pick a label…";
        case "create_deal":
            return a.deal_pipeline_id && a.deal_stage_id ? "Create a CRM deal" : "Pick a pipeline and stage…";
        case "move_deal_stage":
            return a.deal_pipeline_id && a.deal_stage_id ? "Move the deal forward" : "Pick a pipeline and stage…";
        case "unsubscribe":
            return "Unsubscribe the contact";
        case "run_automation":
            return a.automation_id ? "Launch an automation" : "Pick an automation…";
        case "http_request":
            return a.http_url ? "Send an HTTP request" : "Set a URL…";
        case "fire_event":
            return a.event_name ? `Fire "${a.event_name}"` : "Name the event…";
        default:
            return "Action";
    }
}

type ActionNodeData = {
    actionType: string;
    label: string;
    subtitle: string;
    endsHere: boolean;
    orphan: boolean;
    onDelete: () => void;
};

function ActionNode({ data, selected }: NodeProps) {
    const d = data as ActionNodeData;
    const meta = ACTION_META[d.actionType] ?? { label: "Action", Icon: ZapIcon, tint: "text-slate-500" };
    const Icon = meta.Icon;
    return (
        <div
            className={`w-[248px] rounded-xl border bg-white shadow-sm transition-shadow duration-200 hover:shadow-md ${
                d.orphan ? "border-dashed border-amber-300" : "border-slate-200"
            } ${selected ? "border-sky-400 ring-2 ring-sky-100" : ""}`}
        >
            <Handle type="target" position={Position.Top} className="!h-2 !w-2 !border-2 !border-white !bg-slate-300" />
            <div className="flex items-center gap-2 rounded-t-xl border-b border-slate-200/70 bg-gradient-to-r from-slate-50 to-white px-2.5 py-1.5">
                <span className="inline-flex size-5 shrink-0 items-center justify-center rounded-md bg-slate-100 ring-1 ring-slate-200/70">
                    <Icon className={`w-3 h-3 ${meta.tint}`} />
                </span>
                <span className="min-w-0 flex-1 truncate text-[12.5px] font-semibold text-slate-800">{d.label}</span>
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        d.onDelete();
                    }}
                    title="Delete action"
                    className="nodrag inline-flex size-5 shrink-0 items-center justify-center rounded text-slate-300 transition-colors hover:bg-rose-50 hover:text-rose-600"
                >
                    <Trash2Icon className="w-3 h-3" />
                </button>
            </div>
            <div className="px-2.5 py-2">
                <div className="text-[9.5px] font-semibold uppercase tracking-[0.12em] text-slate-300">{meta.label}</div>
                <div className="mt-0.5 truncate text-[11.5px] text-slate-500">{d.subtitle}</div>
            </div>
            {d.orphan ? (
                <div className="flex items-center gap-1 border-t border-amber-200/70 px-2.5 py-1 text-[10.5px] text-amber-600">
                    <UnlinkIcon className="w-3 h-3" />
                    Not connected — drag a link in
                </div>
            ) : d.endsHere ? (
                <div className="flex items-center gap-1 border-t border-slate-200/70 px-2.5 py-1 text-[10.5px] text-slate-400">
                    <FlagIcon className="w-3 h-3 text-slate-400" />
                    Ends here
                </div>
            ) : null}
            {/* One output dot: drag it out to add the next step, action, or condition. */}
            <Handle type="source" id="s" position={Position.Bottom} className="!h-3 !w-3 pointer-coarse:!h-5 pointer-coarse:!w-5 !border-2 !border-white !bg-sky-500" />
        </div>
    );
}

type ConditionNodeData = { label: string; endsHere: boolean; orphan: boolean; onDelete: () => void };

// A Condition node is a pure router (no email, no action): it just branches.
// Drag from it to add conditional paths, and chain Condition nodes to build
// nested decision trees. Persisted as a no-op step (kind "wait").
function ConditionNode({ data, selected }: NodeProps) {
    const d = data as ConditionNodeData;
    return (
        <div
            className={`w-[200px] rounded-xl border bg-white shadow-sm transition-shadow duration-200 hover:shadow-md ${
                d.orphan ? "border-dashed border-amber-300" : "border-amber-200"
            } ${selected ? "border-sky-400 ring-2 ring-sky-100" : ""}`}
        >
            <Handle type="target" position={Position.Top} className="!h-2 !w-2 !border-2 !border-white !bg-slate-300" />
            <div className="flex items-center gap-2 rounded-t-xl border-b border-amber-200/60 bg-gradient-to-r from-amber-50/80 to-white px-2.5 py-1.5">
                <span className="inline-flex size-5 shrink-0 items-center justify-center rounded-md bg-amber-100 text-amber-600 ring-1 ring-amber-200/70">
                    <GitBranchIcon className="w-3 h-3" />
                </span>
                <span className="min-w-0 flex-1 truncate text-[12.5px] font-semibold text-slate-800">
                    {d.label || "Condition"}
                </span>
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        d.onDelete();
                    }}
                    title="Delete condition"
                    className="nodrag inline-flex size-5 shrink-0 items-center justify-center rounded text-slate-300 transition-colors hover:bg-rose-50 hover:text-rose-600"
                >
                    <Trash2Icon className="w-3 h-3" />
                </button>
            </div>
            <div className="px-2.5 py-1.5 text-[10.5px] text-slate-400">
                {d.endsHere ? "Drag out to add an if branch" : "Routes by your conditions"}
            </div>
            {/* One output dot: drag out to add each conditional path. */}
            <Handle type="source" id="s" position={Position.Bottom} className="!h-3 !w-3 pointer-coarse:!h-5 pointer-coarse:!w-5 !border-2 !border-white !bg-sky-500" />
        </div>
    );
}

const nodeTypes = { step: StepNode, ifcond: IfNode, stop: StopNode, action: ActionNode, condition: ConditionNode };

// Convergent edge.
// Several branches can route to the same next step (many in -> one node). They
// all terminate at the node's single top handle, so by default the curves pile
// onto one point and the arrowheads stack. This edge fans the incoming lines
// across the top of the target node by index, so each inbound connection lands
// at its own point and stays readable. A lone inbound edge renders unchanged.
//
// data.conIndex / data.conTotal are filled in by the layout effect once it
// knows how many edges share a target.
type ConvergeEdgeData = { conIndex?: number; conTotal?: number };

function ConvergeEdge({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    markerEnd,
    style,
    label,
    labelStyle,
    labelBgStyle,
    data,
}: EdgeProps) {
    const d = (data ?? {}) as ConvergeEdgeData;
    const total = d.conTotal ?? 1;
    const index = d.conIndex ?? 0;
    // Spread the landing points across ~70% of the node's width, centred on the
    // handle. One inbound edge keeps dead-centre (offset 0).
    const span = Math.min(170, NODE_W * 0.7);
    const offset = total > 1 ? (index - (total - 1) / 2) * (span / Math.max(1, total - 1)) : 0;
    const [path, labelX, labelY] = getBezierPath({
        sourceX,
        sourceY,
        sourcePosition,
        targetX: targetX + offset,
        targetY,
        targetPosition,
    });
    return (
        <>
            <BaseEdge path={path} markerEnd={markerEnd} style={style} />
            {label ? (
                <EdgeLabelRenderer>
                    <div
                        className="nodrag nopan pointer-events-none absolute rounded border px-1 py-px text-[10px]"
                        style={{
                            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
                            borderColor: (labelBgStyle as { stroke?: string } | undefined)?.stroke ?? "#e2e8f0",
                            background: (labelBgStyle as { fill?: string } | undefined)?.fill ?? "#fff",
                            color: (labelStyle as { fill?: string } | undefined)?.fill ?? "#475569",
                        }}
                    >
                        {label}
                    </div>
                </EdgeLabelRenderer>
            ) : null}
        </>
    );
}

const edgeTypes = { converge: ConvergeEdge };

type IfMeta = { sourceId: string; branchId: string };

export default function CampaignFlow({ campaignId }: { campaignId: string }) {
    const { data: sequences } = useSequences(campaignId);
    const createSequence = useCreateSequence(campaignId);
    const deleteSequence = useDeleteSequence(campaignId);
    const { data: campaign } = useCampaign(campaignId);
    const updateCampaign = useUpdateCampaign(campaignId);
    const confirm = useConfirm();
    const qc = useQueryClient();

    const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
    const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
    const [selectedEdge, setSelectedEdge] = React.useState<{ sourceId: string; branchId: string } | null>(null);
    const [editStepId, setEditStepId] = React.useState<string | null>(null);
    const [adding, setAdding] = React.useState(false);
    // When you drag a node's dot out to empty canvas, we open a create menu at
    // the drop point instead of immediately making an email step.
    const [dragCreate, setDragCreate] = React.useState<{
        x: number;
        y: number;
        sourceId: string;
        // The drag started on a Condition node, so the new node becomes a
        // conditional ("if") path rather than an unconditional one.
        conditional?: boolean;
        ifSource?: { sourceId: string; branchId: string; handle: string };
    } | null>(null);
    const connectStartRef = React.useRef<string | null>(null);
    // While dragging a node, kill the position transition so the drag is 1:1.
    const [dragging, setDragging] = React.useState(false);
    // Editing the sequence flow (add a step, drag a node, draw a branch) needs
    // the manage-sequences permission. View-only members can pan/zoom/inspect.
    const canEditFlow = usePermission("MANAGE_SEQUENCES");
    const ifMetaRef = React.useRef<Record<string, IfMeta>>({});

    const seqById = React.useMemo(() => {
        const m = new Map<string, Sequence>();
        for (const s of sequences) m.set(s.id, s);
        return m;
    }, [sequences]);

    const invalidate = React.useCallback(() => qc.invalidateQueries({ queryKey: SEQ_KEY(campaignId) }), [qc, campaignId]);

    // Does the campaign handle replies at all? True when any step has a branch
    // routing on a positive reply signal. Drives the stop-on-reply warning copy.
    const hasReplyBranch = React.useMemo(
        () =>
            (sequences ?? []).some((s) =>
                (s.conditions?.branches ?? []).some((b) =>
                    (b.conditions ?? []).some((c) => isPositiveReplyField(c.field)),
                ),
            ),
        [sequences],
    );

    // Single place that flips stop_on_reply (optimistic + persisted). Shared by
    // the toolbar toggle and the warning's "turn it on" shortcut.
    const setStopOnReply = React.useCallback(
        (next: boolean) => {
            qc.setQueryData(["campaigns", campaignId], (old: unknown) =>
                old ? { ...(old as object), stop_on_reply: next } : old,
            );
            updateCampaign
                .mutateAsync({ stop_on_reply: next })
                .catch((err) => toast.error(buildError(err as AppError)));
        },
        [qc, campaignId, updateCampaign],
    );

    const openCondition = React.useCallback((sourceId: string, branchId: string) => {
        setSelectedEdge({ sourceId, branchId });
        setEditStepId(null);
    }, []);
    const openEditStep = React.useCallback((id: string) => {
        setEditStepId(id);
        setSelectedEdge(null);
    }, []);

    const reachableFrom = React.useCallback(
        (rootId: string) => {
            const set = new Set<string>([rootId]);
            const queue = [rootId];
            while (queue.length) {
                const id = queue.shift()!;
                for (const b of seqById.get(id)?.conditions?.branches ?? []) {
                    const t = b.target_step_id;
                    if (t && !set.has(t)) {
                        set.add(t);
                        queue.push(t);
                    }
                }
            }
            return set;
        },
        [seqById],
    );

    const saveBranches = React.useCallback(
        async (sourceId: string, branches: SequenceBranch[]) => {
            const b = ordered(branches);
            qc.setQueryData<Sequence[]>(SEQ_KEY(campaignId), (old) =>
                old?.map((s) => (s.id === sourceId ? { ...s, conditions: { branches: b } } : s)),
            );
            try {
                await updateSequence(campaignId, sourceId, { conditions: { branches: b } });
            } catch {
                toast.error("Couldn't save the connection");
            } finally {
                invalidate();
            }
        },
        [campaignId, qc, invalidate],
    );

    const saveWait = React.useCallback(
        async (targetId: string, days: number) => {
            const d = Math.max(0, Math.round(days));
            qc.setQueryData<Sequence[]>(SEQ_KEY(campaignId), (old) =>
                old?.map((s) => (s.id === targetId ? { ...s, wait_after: d } : s)),
            );
            try {
                await updateSequence(campaignId, targetId, { wait_after: d });
            } catch {
                toast.error("Couldn't save the wait");
            } finally {
                invalidate();
            }
        },
        [campaignId, qc, invalidate],
    );

    const addUnconditional = React.useCallback(
        (sourceId: string, target: string | null) => {
            const src = seqById.get(sourceId);
            if (!src) return;
            saveBranches(sourceId, [
                ...(src.conditions?.branches ?? []),
                { branch_id: newBranchId(), target_step_id: target, conditions: [] },
            ]);
        },
        [seqById, saveBranches],
    );
    const addIfTo = React.useCallback(
        (sourceId: string, target: string | null) => {
            const src = seqById.get(sourceId);
            if (!src) return;
            const branch: SequenceBranch = {
                branch_id: newBranchId(),
                target_step_id: target,
                conditions: [{ field: "opened", operator: "within_days", value: 3 }],
            };
            // Drop a default ("else") line that already points to the same step
            // the if routes to — otherwise adding the if looks like it
            // auto-created a duplicate else to the same step. The else stays
            // something you add and connect yourself.
            const existing = (src.conditions?.branches ?? []).filter(
                (b) => !(!isCond(b) && b.target_step_id === target),
            );
            saveBranches(sourceId, [...existing, branch]);
            openCondition(sourceId, branch.branch_id);
        },
        [seqById, saveBranches, openCondition],
    );
    const retargetBranch = React.useCallback(
        (sourceId: string, branchId: string, target: string | null) => {
            const src = seqById.get(sourceId);
            if (!src) return;
            saveBranches(
                sourceId,
                (src.conditions?.branches ?? []).map((b) =>
                    b.branch_id === branchId ? { ...b, target_step_id: target } : b,
                ),
            );
        },
        [seqById, saveBranches],
    );
    const deleteBranch = React.useCallback(
        (sourceId: string, branchId: string) => {
            const src = seqById.get(sourceId);
            if (!src) return;
            saveBranches(
                sourceId,
                (src.conditions?.branches ?? []).filter((b) => b.branch_id !== branchId),
            );
            setSelectedEdge((cur) => (cur?.branchId === branchId ? null : cur));
        },
        [seqById, saveBranches],
    );
    const moveBranch = React.useCallback(
        (sourceId: string, branchId: string, dir: -1 | 1) => {
            const src = seqById.get(sourceId);
            if (!src) return;
            const all = src.conditions?.branches ?? [];
            const conds = all.filter(isCond);
            const i = conds.findIndex((b) => b.branch_id === branchId);
            const j = i + dir;
            if (i < 0 || j < 0 || j >= conds.length) return;
            const next = [...conds];
            [next[i], next[j]] = [next[j], next[i]];
            saveBranches(sourceId, [...next, ...all.filter((b) => !isCond(b))]);
        },
        [seqById, saveBranches],
    );

    // Step drops standalone; you connect it by dragging.
    const addStep = React.useCallback(async () => {
        if (adding || sequences.length >= MAX_STEPS) return;
        setAdding(true);
        try {
            await toast.promise(createSequence.mutateAsync(), {
                loading: "Adding step…",
                success: "Step added — drag a dot to connect it.",
                error: (err: AppError) => buildError(err),
            });
        } finally {
            setAdding(false);
        }
    }, [adding, sequences.length, createSequence]);

    // Add an action node. It drops standalone like a step; you connect it by
    // dragging, and configure it by clicking it open.
    const addAction = React.useCallback(
        async (type: SequenceActionType) => {
            if (adding || sequences.length >= MAX_STEPS) return;
            setAdding(true);
            try {
                const created = (await createSequence.mutateAsync()) as Sequence;
                await updateSequence(campaignId, created.id, {
                    kind: "action",
                    action: { type },
                });
                invalidate();
                toast.success("Action added — drag a dot to connect it.");
            } catch (err) {
                toast.error(buildError(err as AppError));
            } finally {
                setAdding(false);
            }
        },
        [adding, sequences.length, createSequence, campaignId, invalidate],
    );

    // Drag from a step's dot to empty -> new step, plain ("just go there") link.
    const dragOutStep = React.useCallback(
        async (sourceId: string) => {
            if (adding || sequences.length >= MAX_STEPS) return;
            const src = seqById.get(sourceId);
            setAdding(true);
            try {
                const created = (await createSequence.mutateAsync()) as Sequence;
                await saveBranches(sourceId, [
                    ...(src?.conditions?.branches ?? []),
                    { branch_id: newBranchId(), target_step_id: created.id, conditions: [] },
                ]);
            } catch {
                toast.error("Couldn't add the step");
            } finally {
                setAdding(false);
            }
        },
        [adding, sequences.length, seqById, createSequence, saveBranches],
    );
    // Drag out -> new action step of the chosen type, connected unconditionally.
    const dragOutAction = React.useCallback(
        async (sourceId: string, type: SequenceActionType) => {
            if (adding || sequences.length >= MAX_STEPS) return;
            const src = seqById.get(sourceId);
            setAdding(true);
            try {
                const created = (await createSequence.mutateAsync()) as Sequence;
                await updateSequence(campaignId, created.id, { kind: "action", action: defaultActionFor(type) });
                await saveBranches(sourceId, [
                    ...(src?.conditions?.branches ?? []),
                    { branch_id: newBranchId(), target_step_id: created.id, conditions: [] },
                ]);
            } catch {
                toast.error("Couldn't add the action");
            } finally {
                setAdding(false);
            }
        },
        [adding, sequences.length, seqById, createSequence, campaignId, saveBranches],
    );
    // Create a step of the chosen type and return its id (used by the menu when
    // dragging out of an IF block, which then points the branch at the new node).
    const createTypedStep = React.useCallback(
        async (choice: CreateChoice): Promise<string | null> => {
            if (adding || sequences.length >= MAX_STEPS) return null;
            setAdding(true);
            try {
                const created = (await createSequence.mutateAsync()) as Sequence;
                if (choice === "condition") {
                    await updateSequence(campaignId, created.id, { kind: "wait", name: "Condition" });
                } else if (choice !== "email") {
                    await updateSequence(campaignId, created.id, { kind: "action", action: defaultActionFor(choice) });
                }
                return created.id;
            } catch {
                toast.error("Couldn't add the step");
                return null;
            } finally {
                setAdding(false);
            }
        },
        [adding, sequences.length, createSequence, campaignId],
    );
    // "On reply" on a step: create a new action step + a reply branch that fires
    // it the moment the contact replies, then open the branch so you can pick the
    // reply type (positive/negative/automated) and build the action chain. Reuses
    // the same instant reply-branch mechanism the backend executes on reply.
    const addReplyStep = React.useCallback(
        async (sourceId: string) => {
            if (adding || sequences.length >= MAX_STEPS) return;
            const src = seqById.get(sourceId);
            setAdding(true);
            try {
                const created = (await createSequence.mutateAsync()) as Sequence;
                await updateSequence(campaignId, created.id, {
                    kind: "action",
                    action: defaultActionFor("create_task"),
                });
                const branch: SequenceBranch = {
                    branch_id: newBranchId(),
                    target_step_id: created.id,
                    conditions: [{ field: "reply_positive", operator: "ever" }],
                };
                await saveBranches(sourceId, [...(src?.conditions?.branches ?? []), branch]);
                invalidate();
                openCondition(sourceId, branch.branch_id);
                toast.success("Reply step added. It runs the moment they reply.");
            } catch {
                toast.error("Couldn't add the reply step");
            } finally {
                setAdding(false);
            }
        },
        [adding, sequences.length, seqById, createSequence, campaignId, saveBranches, openCondition, invalidate],
    );


    const deleteStep = React.useCallback(
        (id: string) => {
            const label = stepName(seqById.get(id));
            const referencing = sequences.filter(
                (s) => s.id !== id && (s.conditions?.branches ?? []).some((b) => b.target_step_id === id),
            );
            const extra = referencing.length
                ? ` ${referencing.length} connection${referencing.length === 1 ? "" : "s"} into it will be removed too.`
                : "";
            confirm.show(`Delete “${label}”? This can't be undone.${extra}`, async () => {
                try {
                    await Promise.all(
                        referencing.map((s) =>
                            updateSequence(campaignId, s.id, {
                                conditions: {
                                    branches: (s.conditions?.branches ?? []).filter((b) => b.target_step_id !== id),
                                },
                            }),
                        ),
                    );
                    await deleteSequence.mutateAsync(id);
                    invalidate();
                    setEditStepId((cur) => (cur === id ? null : cur));
                    setSelectedEdge((cur) => (cur?.sourceId === id ? null : cur));
                    toast.success("Step deleted");
                } catch {
                    toast.error("Couldn't delete the step");
                    throw new Error("delete-failed");
                }
            });
        },
        [sequences, seqById, confirm, campaignId, deleteSequence, invalidate],
    );

    // Node-data callbacks via refs so the layout effect deps don't churn.
    const deleteStepRef = React.useRef(deleteStep);
    const deleteBranchRef = React.useRef(deleteBranch);
    const addReplyStepRef = React.useRef(addReplyStep);
    React.useEffect(() => {
        deleteStepRef.current = deleteStep;
        deleteBranchRef.current = deleteBranch;
        addReplyStepRef.current = addReplyStep;
    }, [deleteStep, deleteBranch, addReplyStep]);

    React.useEffect(() => {
        const waitTag = (targetId: string | null) => {
            if (!targetId) return "";
            const w = seqById.get(targetId)?.wait_after ?? 0;
            return w > 0 ? `wait ${w}d` : "";
        };

        // Steps reachable from the entry (first step). Anything else became an
        // orphan — e.g. an upstream step was deleted — and is flagged so it can
        // be spotted and re-linked (it stays fully connectable).
        const reachable = new Set<string>();
        if (sequences[0]) {
            const queue = [sequences[0].id];
            reachable.add(sequences[0].id);
            while (queue.length) {
                const id = queue.shift()!;
                for (const b of seqById.get(id)?.conditions?.branches ?? []) {
                    const t = b.target_step_id;
                    if (t && !reachable.has(t)) {
                        reachable.add(t);
                        queue.push(t);
                    }
                }
            }
        }

        let emailNum = 0;
        const allNodes: Node[] = sequences.map((s, i) => {
            const branches = s.conditions?.branches ?? [];
            const isAction = s.kind !== "email";
            if (s.kind === "wait") {
                // Pure router: a Condition node that only branches.
                return {
                    id: s.id,
                    type: "condition",
                    position: { x: 0, y: 0 },
                    data: {
                        label: s.name?.trim() || "Condition",
                        endsHere: branches.length === 0,
                        orphan: !reachable.has(s.id),
                        onDelete: () => deleteStepRef.current(s.id),
                    } satisfies ConditionNodeData,
                };
            }
            if (isAction) {
                const at = s.action?.type ?? "add_tag";
                const fallback = ACTION_META[at]?.label ?? "Action";
                return {
                    id: s.id,
                    type: "action",
                    position: { x: 0, y: 0 },
                    data: {
                        actionType: at,
                        label: s.name?.trim() || fallback,
                        subtitle: actionSummary(s.action),
                        endsHere: branches.length === 0,
                        orphan: !reachable.has(s.id),
                        onDelete: () => deleteStepRef.current(s.id),
                    } satisfies ActionNodeData,
                };
            }
            emailNum += 1;
            return {
                id: s.id,
                type: "step",
                position: { x: 0, y: 0 },
                data: {
                    label: s.name?.trim() || `Email ${emailNum}`,
                    subtitle: s.subject,
                    isStart: i === 0,
                    endsHere: branches.length === 0,
                    orphan: !reachable.has(s.id),
                    onDelete: () => deleteStepRef.current(s.id),
                    onAddReply: () => addReplyStepRef.current(s.id),
                } satisfies StepNodeData,
            };
        });

        const ifMeta: Record<string, IfMeta> = {};
        const flowEdges: Edge[] = [];
        const edgeStyle = (cond: boolean) =>
            cond ? { stroke: "#0ea5e9", strokeWidth: 2 } : { stroke: "#94a3b8" };

        sequences.forEach((s) => {
            const branches = ordered(s.conditions?.branches ?? []);
            const conds = branches.filter(isCond);
            const uncond = branches.find((b) => !isCond(b));

            conds.forEach((b, i) => {
                const nid = ifNodeId(b.branch_id);
                ifMeta[nid] = { sourceId: s.id, branchId: b.branch_id };
                allNodes.push({
                    id: nid,
                    type: "ifcond",
                    position: { x: 0, y: 0 },
                    data: {
                        label: conditionText(b),
                        // Instant-capable branches (reply intent / opened / clicked)
                        // run the moment the event lands; flag the node.
                        instant: isInstantBranch(b),
                        onDelete: () => deleteBranchRef.current(s.id, b.branch_id),
                    } satisfies IfNodeData,
                });
                // Incoming: from the step (first condition) or the PREVIOUS
                // condition's ELSE — so conditions chain as if / else-if.
                if (i === 0) {
                    flowEdges.push({
                        id: `in-${b.branch_id}`,
                        source: s.id,
                        sourceHandle: "s",
                        target: nid,
                        style: edgeStyle(true),
                        data: { sourceId: s.id, branchId: b.branch_id },
                    });
                } else {
                    flowEdges.push({
                        id: `chain-${b.branch_id}`,
                        source: ifNodeId(conds[i - 1].branch_id),
                        sourceHandle: "else",
                        target: nid,
                        label: "else",
                        style: edgeStyle(false),
                        labelStyle: { fill: "#94a3b8", fontSize: 10 },
                        labelBgStyle: { fill: "#fff", stroke: "#e2e8f0" },
                        labelBgPadding: [4, 2],
                        labelBgBorderRadius: 5,
                        data: { sourceId: s.id, branchId: b.branch_id },
                    });
                }
                // THEN path -> the condition's target. Engagement branches
                // (opened/clicked) carry a window the event must land inside; an
                // instant branch fires the MOMENT it happens BUT only within that
                // window, so surface it ("instant <=10d") rather than just
                // "instant" or the target step's (irrelevant) wait_after. Reply
                // branches have no window.
                const withinDays = (b.conditions ?? []).find((c) => c.operator === "within_days")?.value;
                const wt = isInstantBranch(b)
                    ? withinDays
                        ? `instant <=${withinDays}d`
                        : "instant"
                    : withinDays
                      ? `within ${withinDays}d`
                      : waitTag(b.target_step_id);
                // A condition with no THEN target yet leaves its right dot OPEN to
                // drag to the next step, rather than auto-wiring it to STOP.
                if (b.target_step_id) {
                    flowEdges.push({
                        id: `then-${b.branch_id}`,
                        source: nid,
                        sourceHandle: "out",
                        target: b.target_step_id,
                        label: wt || undefined,
                        reconnectable: true,
                        style: edgeStyle(true),
                        labelStyle: { fill: "#0369a1", fontSize: 10 },
                        labelBgStyle: { fill: "#fff", stroke: "#bae6fd" },
                        labelBgPadding: [5, 3],
                        labelBgBorderRadius: 5,
                        data: { sourceId: s.id, branchId: b.branch_id },
                    });
                }
            });

            if (uncond) {
                const wt = waitTag(uncond.target_step_id);
                const target = uncond.target_step_id ?? STOP_ID;
                if (conds.length > 0) {
                    // The final ELSE hangs off the LAST condition's else.
                    flowEdges.push({
                        id: `else-${uncond.branch_id}`,
                        source: ifNodeId(conds[conds.length - 1].branch_id),
                        sourceHandle: "else",
                        target,
                        label: wt ? `else · ${wt}` : "else",
                        reconnectable: true,
                        style: edgeStyle(false),
                        labelStyle: { fill: "#475569", fontSize: 10 },
                        labelBgStyle: { fill: "#fff", stroke: "#e2e8f0" },
                        labelBgPadding: [5, 3],
                        labelBgBorderRadius: 5,
                        data: { sourceId: s.id, branchId: uncond.branch_id },
                    });
                } else {
                    // No conditions on this step: a plain "just go there" line.
                    flowEdges.push({
                        id: `u-${uncond.branch_id}`,
                        source: s.id,
                        sourceHandle: "s",
                        target,
                        label: wt || undefined,
                        reconnectable: true,
                        style: edgeStyle(false),
                        labelStyle: { fill: "#475569", fontSize: 10 },
                        labelBgStyle: { fill: "#fff", stroke: "#e2e8f0" },
                        labelBgPadding: [5, 3],
                        labelBgBorderRadius: 5,
                        data: { sourceId: s.id, branchId: uncond.branch_id },
                    });
                }
            }
        });
        ifMetaRef.current = ifMeta;

        // Show STOP only when an edge actually routes there (a deliberate
        // "otherwise → end"), never for a condition whose THEN is still open.
        const anyStop = flowEdges.some((e) => e.target === STOP_ID);
        if (anyStop) allNodes.push({ id: STOP_ID, type: "stop", position: { x: 0, y: 0 }, data: {} });

        // Convergence: more than one branch can route to the SAME next step, so
        // a node can have several inbound edges. Count them per target and number
        // each one, so the custom "converge" edge can fan the lines across the
        // target's top instead of piling them all onto its single handle. Targets
        // with one inbound edge stay on the plain bezier (no fan, no overhead).
        const inboundCount = new Map<string, number>();
        for (const e of flowEdges) inboundCount.set(e.target, (inboundCount.get(e.target) ?? 0) + 1);
        const inboundSeen = new Map<string, number>();

        // Flowing curved (bezier) connectors with arrowheads — no boxy right
        // angles. The arrow takes the edge's own stroke colour.
        const smoothEdges: Edge[] = flowEdges.map((e) => {
            const total = inboundCount.get(e.target) ?? 1;
            const index = inboundSeen.get(e.target) ?? 0;
            inboundSeen.set(e.target, index + 1);
            // Always use the converge edge type. With a single inbound edge it
            // renders identically (offset 0); with several it fans them out. The
            // point is the type NEVER changes in-session: when a second edge is
            // added to a target, only conTotal updates (1 -> 2). Flipping an
            // existing edge's `type` live makes React Flow drop the whole edge
            // layer until reload. The "lines disappeared after I connected two to
            // the same step" bug.
            return {
                ...e,
                type: "converge",
                data: { ...e.data, conIndex: index, conTotal: total },
                markerEnd: {
                    type: MarkerType.ArrowClosed,
                    width: 16,
                    height: 16,
                    color: (e.style as { stroke?: string } | undefined)?.stroke ?? "#94a3b8",
                },
            };
        });

        const laid = stackComponents(layoutGraph(allNodes, smoothEdges), smoothEdges);
        setNodes((cur) => {
            // Smoothness: never re-arrange the whole canvas on a connect/disconnect.
            // Keep every existing node exactly where the user left it; only a NEW
            // node gets a position — dropped just below the node it was dragged
            // from (or the computed layout position on first load / unknown source).
            // The "Tidy up" button is the one explicit full re-layout.
            const pos = new Map(cur.map((n) => [n.id, n.position]));
            if (pos.size === 0) return laid; // first load: use the computed layout
            const sourceOf = new Map<string, { source: string; handle?: string | null }>();
            smoothEdges.forEach((e) => {
                if (!sourceOf.has(e.target)) sourceOf.set(e.target, { source: e.source, handle: e.sourceHandle });
            });
            const laidPos = new Map(laid.map((n) => [n.id, n.position]));
            return laid.map((n) => {
                if (pos.has(n.id)) return { ...n, position: pos.get(n.id)! };
                const inc = sourceOf.get(n.id);
                const srcPos = inc ? pos.get(inc.source) : undefined;
                if (srcPos) {
                    // A node hung off an IF box's THEN (right "out" dot) gets its
                    // own space to the RIGHT, so the yes-line flows cleanly
                    // rightward instead of curving back under the condition. The
                    // ELSE (bottom dot) keeps flowing straight down.
                    if (inc && isIfId(inc.source) && inc.handle === "out") {
                        return { ...n, position: { x: srcPos.x + 300, y: srcPos.y + 36 } };
                    }
                    return { ...n, position: { x: srcPos.x, y: srcPos.y + 170 } };
                }
                return { ...n, position: laidPos.get(n.id) ?? n.position };
            });
        });
        setEdges(smoothEdges);
    }, [sequences, seqById, setNodes, setEdges]);

    // Subtree highlight: select a step/if → light up what's reachable, dim rest.
    React.useEffect(() => {
        let root: string | null = null;
        if (editStepId) root = editStepId;
        else if (selectedEdge) {
            const br = seqById
                .get(selectedEdge.sourceId)
                ?.conditions?.branches?.find((b) => b.branch_id === selectedEdge.branchId);
            root = br?.target_step_id ?? selectedEdge.sourceId;
        }
        const hl = root ? reachableFrom(root) : null;
        const stepIn = (id: string) => {
            if (!hl) return true;
            if (id === STOP_ID) return true;
            if (isIfId(id)) {
                const m = ifMetaRef.current[id];
                return m ? hl.has(m.sourceId) : true;
            }
            return hl.has(id);
        };
        setNodes((ns) =>
            ns.map((n) => ({ ...n, style: { ...n.style, opacity: hl && !stepIn(n.id) ? 0.3 : 1 } })),
        );
        setEdges((es) =>
            es.map((e) => {
                const sid = (e.data as { sourceId?: string } | undefined)?.sourceId;
                const on = !hl || (sid ? hl.has(sid) : true);
                return { ...e, style: { ...e.style, opacity: on ? 1 : 0.15 } };
            }),
        );
    }, [editStepId, selectedEdge, seqById, reachableFrom, setNodes, setEdges]);

    const onConnect = React.useCallback(
        (c: Connection) => {
            if (!c.source || !c.target || c.source === c.target || isIfId(c.target)) return;
            const target = c.target === STOP_ID ? null : c.target;
            if (isIfId(c.source)) {
                const m = ifMetaRef.current[c.source];
                if (!m) return;
                // "out" = retarget the then-target; "else" (bottom gray dot) =
                // an unconditional "always / just go there" fallback by default.
                if (c.sourceHandle === "out") retargetBranch(m.sourceId, m.branchId, target);
                else addUnconditional(m.sourceId, target);
            } else if (c.sourceHandle === "if") {
                addIfTo(c.source, target);
            } else if (seqById.get(c.source)?.kind === "wait") {
                // A Condition node is a branch point: every path out of it is a
                // condition ("if X, go here"), chained as if / else-if. The final
                // catch-all is the else dot on the last IF box.
                addIfTo(c.source, target);
            } else {
                addUnconditional(c.source, target);
            }
        },
        [seqById, retargetBranch, addIfTo, addUnconditional],
    );

    const selected = React.useMemo(() => {
        if (!selectedEdge) return null;
        const src = seqById.get(selectedEdge.sourceId);
        const br = src?.conditions?.branches?.find((b) => b.branch_id === selectedEdge.branchId);
        return src && br ? { source: src, branch: br } : null;
    }, [selectedEdge, seqById]);

    // Touch / coarse-pointer devices: don't let a drag MOVE nodes (so a swipe
    // pans to navigate instead of "placing a card"), and let page scroll through.
    const isCoarse = React.useMemo(
        () => typeof window !== "undefined" && !!window.matchMedia && window.matchMedia("(pointer: coarse)").matches,
        [],
    );

    const editStep = editStepId ? seqById.get(editStepId) : undefined;
    const editIndex = editStep ? sequences.findIndex((s) => s.id === editStep.id) : -1;
    const atMax = sequences.length >= MAX_STEPS;

    return (
        <div
            className={`campaign-flow relative h-[70dvh] w-full overflow-hidden rounded-md border border-slate-200 bg-slate-50/40 sm:h-[78vh] ${
                dragging ? "rf-dragging" : ""
            }`}
        >
            <ReactFlow
                nodes={nodes}
                edges={edges}
                onNodesChange={(changes) => {
                    if (!canEditFlow) {
                        // Block structural edits (drag-move, delete); keep
                        // select/dimension so the canvas still renders for viewing.
                        onNodesChange(
                            changes.filter((c) => c.type !== "position" && c.type !== "remove"),
                        );
                        return;
                    }
                    onNodesChange(changes);
                }}
                onEdgesChange={(changes) => {
                    if (!canEditFlow) {
                        onEdgesChange(changes.filter((c) => c.type !== "remove"));
                        return;
                    }
                    onEdgesChange(changes);
                }}
                onNodeDragStart={() => {
                    if (!canEditFlow) {
                        showPermissionDenied("MANAGE_SEQUENCES");
                        return;
                    }
                    setDragging(true);
                }}
                onNodeDragStop={() => setDragging(false)}
                onConnect={(c) => {
                    if (!canEditFlow) {
                        showPermissionDenied("MANAGE_SEQUENCES");
                        return;
                    }
                    onConnect(c);
                }}
                onConnectStart={(_, params) => {
                    connectStartRef.current = params.nodeId ?? null;
                }}
                nodesConnectable={canEditFlow}
                nodesDraggable={!isCoarse}
                zoomOnScroll={false}
                panOnScroll={false}
                preventScrolling={false}
                minZoom={0.2}
                maxZoom={1.75}
                onConnectEnd={(event, state) => {
                    const fromId = state?.fromNode?.id ?? connectStartRef.current;
                    connectStartRef.current = null;
                    // Only when the line is dropped on EMPTY canvas (the pane). A
                    // drop on a node/handle is a real connection that onConnect
                    // already handled. The pane class is the reliable v12 signal.
                    const onPane = (event.target as Element | null)?.classList?.contains("react-flow__pane");
                    if (!fromId || !onPane) return;
                    if (!canEditFlow) {
                        showPermissionDenied("MANAGE_SEQUENCES");
                        return;
                    }
                    const pt =
                        "changedTouches" in event && event.changedTouches.length
                            ? event.changedTouches[0]
                            : (event as MouseEvent);
                    if (isIfId(fromId)) {
                        // Dragging out of an IF block: the menu lets the then/else
                        // path lead to an email, action, or another condition (so
                        // you can chain IF -> condition -> IF into nested trees).
                        const m = ifMetaRef.current[fromId];
                        if (!m) return;
                        const handle = state?.fromHandle?.id ?? "out";
                        setDragCreate({
                            x: pt.clientX,
                            y: pt.clientY,
                            sourceId: fromId,
                            ifSource: { sourceId: m.sourceId, branchId: m.branchId, handle },
                        });
                    } else {
                        setDragCreate({
                            x: pt.clientX,
                            y: pt.clientY,
                            sourceId: fromId,
                            conditional: seqById.get(fromId)?.kind === "wait",
                        });
                    }
                }}
                onReconnect={(oldEdge, conn) => {
                    const d = oldEdge.data as { sourceId?: string; branchId?: string } | undefined;
                    if (!d?.sourceId || !d?.branchId || !conn.target || isIfId(conn.target)) return;
                    retargetBranch(d.sourceId, d.branchId, conn.target === STOP_ID ? null : conn.target);
                }}
                onEdgesDelete={(deleted) =>
                    deleted.forEach((e) => {
                        // Cancel a line: removing an edge removes its branch (the
                        // then/else/“go there” path it represents).
                        const d = e.data as { sourceId?: string; branchId?: string } | undefined;
                        if (d?.sourceId && d?.branchId) deleteBranch(d.sourceId, d.branchId);
                    })
                }
                nodeTypes={nodeTypes}
                edgeTypes={edgeTypes}
                onEdgeClick={(_, edge) => {
                    const d = edge.data as { sourceId?: string; branchId?: string } | undefined;
                    if (d?.sourceId && d?.branchId) openCondition(d.sourceId, d.branchId);
                }}
                onNodeClick={(_, node) => {
                    if (node.type === "ifcond") {
                        const m = ifMetaRef.current[node.id];
                        if (m) openCondition(m.sourceId, m.branchId);
                    } else if (node.id !== STOP_ID) {
                        openEditStep(node.id);
                    }
                }}
                fitView
                proOptions={{ hideAttribution: true }}
            >
                <Background color="#e9eef5" gap={24} size={1} />
                <Controls showInteractive={false} />

                <Panel position="top-left">
                    <div className="flex max-w-[calc(100vw-1.5rem)] flex-col gap-1.5">
                        <div className="flex flex-wrap items-center gap-1.5">
                            <AddNodeMenu
                                onAddEmail={
                                    canEditFlow ? addStep : () => showPermissionDenied("MANAGE_SEQUENCES")
                                }
                                onAddAction={
                                    canEditFlow
                                        ? addAction
                                        : () => showPermissionDenied("MANAGE_SEQUENCES")
                                }
                                disabled={adding || atMax}
                                busy={adding}
                                locked={!canEditFlow}
                            />
                            <button
                                type="button"
                                onClick={() => setNodes((ns) => stackComponents(layoutGraph(ns, edges), edges))}
                                className="inline-flex h-8 items-center gap-1.5 rounded-md border border-slate-200 bg-white px-2.5 text-[12px] font-medium text-slate-700 shadow-sm transition-colors hover:border-slate-300 hover:text-slate-900"
                            >
                                Tidy up
                            </button>
                            <StopOnReplyToggle on={!!campaign?.stop_on_reply} onToggle={setStopOnReply} />
                        </div>
                        {campaign && !campaign.stop_on_reply && (sequences?.length ?? 0) > 1 && (
                            <ReplyStopWarning
                                hasReplyBranch={hasReplyBranch}
                                onEnable={() => setStopOnReply(true)}
                            />
                        )}
                    </div>
                </Panel>

                <Panel position="bottom-center">
                    {/* Mouse/keyboard-only instructions; hidden on phones where the
                        narrow auto-width panel would wrap into a tall block over the
                        canvas (touch users remove edges via the editor's Disconnect). */}
                    <div className="hidden md:flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md bg-white/95 px-3 py-1.5 text-[11px] text-slate-500 shadow-sm">
                        <span className="text-slate-400">
                            drag a node’s bottom dot onto another node to connect, or onto empty space to pick what to add (email, action, or a condition) · click a line to set its condition · add Condition nodes to branch, and chain them for nested trees · click a line then press Delete to remove it · no match = stop
                        </span>
                    </div>
                </Panel>
            </ReactFlow>

            {dragCreate && (
                <DragCreateMenu
                    x={dragCreate.x}
                    y={dragCreate.y}
                    onClose={() => setDragCreate(null)}
                    onPick={async (choice) => {
                        const dc = dragCreate;
                        if (dc.ifSource) {
                            // From an IF block: create the node, then point this
                            // branch's then/else path at it.
                            const id = await createTypedStep(choice);
                            if (id) {
                                if (dc.ifSource.handle === "out") {
                                    retargetBranch(dc.ifSource.sourceId, dc.ifSource.branchId, id);
                                } else {
                                    addUnconditional(dc.ifSource.sourceId, id);
                                }
                            }
                        } else if (dc.conditional) {
                            // Dragging out of a Condition node: create the node,
                            // then attach it as a conditional ("if") branch so the
                            // router actually branches (and open the condition
                            // editor). Drag again for the next if / else-if.
                            const id = await createTypedStep(choice);
                            if (id) addIfTo(dc.sourceId, id);
                        } else if (choice === "email") {
                            dragOutStep(dc.sourceId);
                        } else if (choice === "condition") {
                            // "Condition" IS the if-branch: add it straight onto
                            // the source (the IF box has its own then/else), rather
                            // than a separate router node you'd drag from. Opens the
                            // condition editor; connect then/else to the next steps.
                            addIfTo(dc.sourceId, null);
                        } else {
                            dragOutAction(dc.sourceId, choice);
                        }
                    }}
                />
            )}

            {selected && (
                <ConnectionEditor
                    source={selected.source}
                    branch={selected.branch}
                    steps={sequences}
                    order={ordered(selected.source.conditions?.branches ?? [])
                        .filter(isCond)
                        .findIndex((b) => b.branch_id === selected.branch.branch_id)}
                    condCount={(selected.source.conditions?.branches ?? []).filter(isCond).length}
                    onMove={(dir) => moveBranch(selected.source.id, selected.branch.branch_id, dir)}
                    waitDays={seqById.get(selected.branch.target_step_id ?? "")?.wait_after ?? 0}
                    onClose={() => setSelectedEdge(null)}
                    onSetWait={(days) => {
                        if (selected.branch.target_step_id) saveWait(selected.branch.target_step_id, days);
                    }}
                    onSave={(updated) => {
                        saveBranches(
                            selected.source.id,
                            (selected.source.conditions?.branches ?? []).map((b) =>
                                b.branch_id === updated.branch_id ? updated : b,
                            ),
                        );
                        setSelectedEdge(null);
                    }}
                    onDelete={() => {
                        deleteBranch(selected.source.id, selected.branch.branch_id);
                        setSelectedEdge(null);
                    }}
                />
            )}

            {editStep && (
                <div className="fixed inset-0 z-30 w-full overflow-y-auto overflow-x-hidden bg-white md:absolute md:left-auto md:z-10 md:max-w-[760px] md:border-l md:border-slate-200 md:shadow-[0_0_40px_-12px_rgba(15,23,42,0.25)] xl:max-w-[880px]">
                    <div className="sticky top-0 z-10 flex items-center justify-between border-b border-slate-200 bg-white px-3 py-2">
                        <span className="truncate text-[12.5px] font-medium text-slate-700">Edit “{stepName(editStep)}”</span>
                        <div className="flex items-center gap-1">
                            <button
                                type="button"
                                onClick={() => deleteStep(editStep.id)}
                                className="inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-[12px] text-slate-500 hover:bg-rose-50 hover:text-rose-600"
                            >
                                <Trash2Icon className="w-3.5 h-3.5" /> Delete
                            </button>
                            <button
                                type="button"
                                onClick={() => setEditStepId(null)}
                                className="inline-flex size-7 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-900"
                            >
                                <XIcon className="w-4 h-4" />
                            </button>
                        </div>
                    </div>
                    <div className="p-3">
                        <NodeTypeSwitcher campaignId={campaignId} sequence={editStep} onChanged={invalidate} />
                        {editStep.kind !== "email" ? (
                            <ActionEditor campaignId={campaignId} sequence={editStep} onSaved={invalidate} />
                        ) : (
                            <StepEmailArms campaignId={campaignId} sequence={editStep} index={editIndex} />
                        )}
                    </div>
                </div>
            )}
        </div>
    );
}

// ── Stop-on-reply toggle ────────────────────────────────────────────────────
const STOP_ON_REPLY_HELP =
    "When a contact replies, the rest of the sequence stops for them, " +
    "while the reply branch you connected to the email they answered still " +
    "runs (it fires instantly). So on reply: that reply flow runs, every other " +
    "remaining step is cancelled. Auto-replies and out-of-office messages don't " +
    "count as a reply, so the sequence keeps going.";

function StopOnReplyToggle({ on, onToggle }: { on: boolean; onToggle: (next: boolean) => void }) {
    return (
        <div
            className="flex items-center gap-2 rounded-md border border-slate-200 bg-white px-2.5 py-1.5 shadow-sm"
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
                onClick={() => onToggle(!on)}
                className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors ${
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
function ReplyStopWarning({ hasReplyBranch, onEnable }: { hasReplyBranch: boolean; onEnable: () => void }) {
    return (
        <div className="flex max-w-[19rem] items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-2.5 py-1.5 text-[11px] text-amber-700 shadow-sm">
            <AlertTriangleIcon className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-500" />
            <div className="space-y-1">
                <p className="leading-snug">
                    {hasReplyBranch
                        ? "Stop on reply is off. Replies that don't match a reply branch (say, a reply to an older email) keep getting cold emails. Turning it on still runs your reply branches."
                        : "No reply handling. With stop on reply off, contacts who reply keep moving through the cold sequence. Turn it on, or add a reply branch."}
                </p>
                <button
                    type="button"
                    onClick={onEnable}
                    className="font-medium text-amber-800 underline underline-offset-2 hover:text-amber-900"
                >
                    Turn on stop on reply
                </button>
            </div>
        </div>
    );
}

function WaitRow({ value, onCommit }: { value: number; onCommit: (v: number) => void }) {
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

// ── Connection editor (optional condition + wait behind a connection) ───────
const BRANCH_PATH_OPTIONS: SelectOption[] = [
    { value: "always", label: "always (right after the wait)" },
    { value: "opened", label: "if opened the email", group: "Engagement" },
    { value: "clicked", label: "if clicked a link", group: "Engagement" },
    { value: "replied", label: "if replied", group: "Engagement" },
    { value: "not_opened", label: "if didn't open", group: "Engagement" },
    { value: "not_clicked", label: "if didn't click", group: "Engagement" },
    { value: "not_replied", label: "if didn't reply", group: "Engagement" },
    { value: "reply_positive", label: "if replied: positive", group: "Reply intent" },
    { value: "reply_negative", label: "if replied: negative", group: "Reply intent" },
    { value: "reply_neutral", label: "if replied: neutral", group: "Reply intent" },
    { value: "reply_automated", label: "if auto-reply / out of office", group: "Reply intent" },
    { value: "random", label: "random split" },
];

function ConnectionEditor({
    source,
    branch,
    steps,
    order,
    condCount,
    onMove,
    waitDays,
    onSetWait,
    onClose,
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
    onClose: () => void;
    onSave: (b: SequenceBranch) => void;
    onDelete: () => void;
}) {
    const c0 = branch.conditions?.[0];
    const [field, setField] = React.useState<string>(c0 ? c0.field : "always");
    const [value, setValue] = React.useState<number>(c0?.value ?? (c0?.field === "random" ? 50 : 3));
    // Instant-capable branches (reply intent, opened, clicked) fire the moment
    // the event lands by default; this lets the user opt out so the path routes
    // at the next step boundary instead.
    const [instant, setInstant] = React.useState<boolean>(branch.instant !== false);

    const isAlways = field === "always";
    const isRandom = field === "random";
    const isReply = isReplyBranchField(field as BranchField);
    const isInstantCapable = isInstantCapableField(field as BranchField);
    const instantVerb = field === "opened" ? "open" : field === "clicked" ? "click" : "reply";
    const isNegative = field === "not_opened" || field === "not_clicked" || field === "not_replied";
    const target = steps.find((s) => s.id === branch.target_step_id);
    const targetLabel = branch.target_step_id === null ? "Stop the sequence" : target ? `“${stepName(target)}”` : "—";

    const buildConditions = (): BranchCondition[] => {
        if (isAlways) return [];
        if (isRandom) return [{ field: "random", operator: "chance", value }];
        // Reply-class conditions are checked once, ever (no day window / value).
        if (isReply) return [{ field: field as BranchField, operator: "ever" }];
        return [{ field: field as BranchField, operator: "within_days", value }];
    };
    const save = (target_step_id: string | null) =>
        onSave({
            branch_id: branch.branch_id,
            target_step_id,
            conditions: buildConditions(),
            // Persist the instant opt-out for any instant-capable signal (reply
            // intent, opened, clicked). Other fields can't fire instantly.
            instant: isInstantCapable ? instant : undefined,
        });

    return (
        <div className="absolute right-3 top-3 z-20 w-[300px] max-w-[calc(100vw-1.5rem)] max-h-[calc(100%-1.5rem)] overflow-y-auto rounded-md border border-slate-200 bg-white p-3 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]">
            <div className="mb-2 flex items-center justify-between">
                <span className="truncate text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">
                    From “{stepName(source)}”
                </span>
                <button
                    type="button"
                    onClick={onClose}
                    className="inline-flex size-6 items-center justify-center rounded text-slate-400 hover:bg-slate-100 hover:text-slate-900"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            </div>

            {order >= 0 && condCount > 1 && (
                <div className="mb-2 flex items-center justify-between rounded-md bg-slate-50 px-2 py-1 text-[11px] text-slate-500">
                    <span>When several match, this is checked {order + 1} of {condCount}</span>
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
                            set to stop
                        </button>
                    )}
                </div>

                <div>
                    <p className="mb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Take this path</p>
                    <SelectMenu
                        className="w-full"
                        value={field}
                        options={BRANCH_PATH_OPTIONS}
                        onChange={(f) => {
                            setField(f);
                            if (f === "random") setValue((v) => (v >= 1 && v <= 99 ? v : 50));
                            else if (f !== "always" && !isReplyBranchField(f as BranchField))
                                setValue((v) => (v >= 1 && v <= 60 ? v : 3));
                        }}
                    />
                </div>

                {isRandom && (
                    <div className="flex flex-wrap items-center gap-1.5">
                        <NumberInput value={value} onChange={(v) => setValue(Math.max(1, Math.min(99, Math.round(v) || 1)))} min={1} max={99} className="w-16" align="center" />
                        <span>% of contacts (chosen at random)</span>
                    </div>
                )}
                {!isAlways && !isRandom && !isReply && (
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
                        {isReply ? (
                            <>
                                <p className="text-[10.5px] text-slate-400">
                                    {field === "reply_automated"
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

            <div className="mt-3 flex items-center gap-2">
                <button
                    type="button"
                    onClick={onDelete}
                    title="Remove this line and the branch it represents"
                    className="inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-[12px] font-medium text-rose-500 transition-colors hover:bg-rose-50 hover:text-rose-600"
                >
                    <Trash2Icon className="w-3.5 h-3.5" />
                    Disconnect
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

// ── Add-action menu (Panel) ─────────────────────────────────────────────────
// Note: there is no "end" action — a path ends simply by leaving a node's
// bottom dot unconnected (shows "Ends here") or routing a branch to Stop. That
// keeps the cleaner Stop/"Ends here" visual instead of a configurable end node.
const ADD_ACTION_OPTIONS: { type: SequenceActionType; label: string }[] = [
    { type: "add_tag", label: "Add tag" },
    { type: "remove_tag", label: "Remove tag" },
    { type: "label_email", label: "Label email" },
    { type: "create_task", label: "Create task" },
    { type: "create_deal", label: "Create deal" },
    { type: "move_deal_stage", label: "Move deal stage" },
    { type: "unsubscribe", label: "Unsubscribe" },
    { type: "run_automation", label: "Run automation" },
    { type: "http_request", label: "HTTP request" },
    { type: "fire_event", label: "Fire event" },
];

type CreateChoice = "email" | "condition" | SequenceActionType;

// The menu that opens where you drop a dragged connection on empty canvas:
// pick what the next node is (email, an action, or a condition router).
function DragCreateMenu({
    x,
    y,
    onPick,
    onClose,
}: {
    x: number;
    y: number;
    onPick: (choice: CreateChoice) => void;
    onClose: () => void;
}) {
    // Animate in/out like the app's other menus. The step is created the moment
    // a row is clicked; the menu plays its exit independently, and onClose (which
    // clears the parent state) only fires once that exit finishes.
    const [open, setOpen] = React.useState(true);
    const vw = typeof window !== "undefined" ? window.innerWidth : x + 240;
    const vh = typeof window !== "undefined" ? window.innerHeight : y + 360;
    const flipX = x > vw - 232;
    const flipY = y > vh - 360;
    const left = Math.max(8, Math.min(x, vw - 232));
    const top = Math.max(8, Math.min(y, vh - 360));
    const pick = (choice: CreateChoice) => {
        onPick(choice);
        setOpen(false);
    };
    return createPortal(
        <>
            {open && <div className="fixed inset-0 z-40" onMouseDown={() => setOpen(false)} />}
            <AnimatePresence onExitComplete={onClose}>
                {open && (
                    <motion.div
                        key="drag-create-menu"
                        className="fixed z-50 max-h-[340px] w-56 overflow-y-auto rounded-lg border border-slate-200 bg-white p-1 shadow-xl"
                        style={{
                            left,
                            top,
                            transformOrigin: `${flipY ? "bottom" : "top"} ${flipX ? "right" : "left"}`,
                            willChange: "transform, opacity",
                        }}
                        role="menu"
                        initial={{ opacity: 0, scale: 0.95, y: flipY ? 4 : -4 }}
                        animate={{ opacity: 1, scale: 1, y: 0 }}
                        exit={{ opacity: 0, scale: 0.97, y: flipY ? 2 : -2 }}
                        transition={{
                            opacity: { duration: 0.14, ease: [0.16, 1, 0.3, 1] },
                            scale: { duration: 0.18, ease: [0.16, 1, 0.3, 1] },
                            y: { duration: 0.18, ease: [0.16, 1, 0.3, 1] },
                        }}
                    >
                        <div className="px-2 pt-1 pb-0.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-slate-400">Add</div>
                        <CreateRow icon={<MailIcon className="w-3.5 h-3.5 text-sky-600" />} label="Email step" onClick={() => pick("email")} />
                        <CreateRow icon={<GitBranchIcon className="w-3.5 h-3.5 text-amber-600" />} label="Condition (branch)" onClick={() => pick("condition")} />
                        <div className="my-1 h-px bg-slate-100" />
                        <div className="px-2 pt-0.5 pb-0.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-slate-400">Actions</div>
                        {ADD_ACTION_OPTIONS.map((o) => {
                            const meta = ACTION_META[o.type];
                            const Icon = meta?.Icon ?? ZapIcon;
                            return (
                                <CreateRow
                                    key={o.type}
                                    icon={<Icon className={`w-3.5 h-3.5 ${meta?.tint ?? "text-slate-500"}`} />}
                                    label={o.label}
                                    onClick={() => pick(o.type)}
                                />
                            );
                        })}
                    </motion.div>
                )}
            </AnimatePresence>
        </>,
        document.body,
    );
}

function CreateRow({ icon, label, onClick }: { icon: React.ReactNode; label: string; onClick: () => void }) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[12.5px] text-slate-700 transition-colors hover:bg-slate-100"
        >
            {icon}
            {label}
        </button>
    );
}

function AddNodeMenu({
    onAddEmail,
    onAddAction,
    disabled,
    busy,
    locked,
}: {
    onAddEmail: () => void;
    onAddAction: (t: SequenceActionType) => void;
    disabled: boolean;
    busy: boolean;
    locked?: boolean;
}) {
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));
    return (
        <div ref={ref} className="relative inline-flex">
            {/* Primary click = add an email step (the default, common case). */}
            <button
                type="button"
                disabled={locked ? false : disabled}
                onClick={onAddEmail}
                className={`inline-flex h-8 items-center gap-1.5 rounded-l-md bg-sky-600 px-3 text-[12px] font-medium text-white shadow-sm transition-colors hover:bg-sky-700 disabled:opacity-60 ${locked ? "opacity-60" : ""}`}
            >
                {locked ? (
                    <LockIcon className="w-3.5 h-3.5" />
                ) : busy ? (
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                ) : (
                    <PlusIcon className="w-3.5 h-3.5" />
                )}
                Add step
            </button>
            {/* Chevron = the full list of node types (email + actions). */}
            <button
                type="button"
                disabled={disabled}
                onClick={() => setOpen((o) => !o)}
                aria-label="More step types"
                className="inline-flex h-8 items-center rounded-r-md border-l border-sky-500/60 bg-sky-600 px-1.5 text-white shadow-sm transition-colors hover:bg-sky-700 disabled:opacity-60"
            >
                <ChevronDownIcon className="w-3.5 h-3.5" />
            </button>
            <AnimatePresence>
                {open && (
                    <motion.div
                        key="add-node-menu"
                        className="absolute left-0 top-full z-30 mt-1 w-52 overflow-hidden rounded-md border border-slate-200 bg-white py-1 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]"
                        style={{ transformOrigin: "top left", willChange: "transform, opacity" }}
                        initial={{ opacity: 0, scale: 0.95, y: -4 }}
                        animate={{ opacity: 1, scale: 1, y: 0 }}
                        exit={{ opacity: 0, scale: 0.97, y: -2 }}
                        transition={{
                            opacity: { duration: 0.14, ease: [0.16, 1, 0.3, 1] },
                            scale: { duration: 0.18, ease: [0.16, 1, 0.3, 1] },
                            y: { duration: 0.18, ease: [0.16, 1, 0.3, 1] },
                        }}
                    >
                        <button
                            type="button"
                            onClick={() => {
                                onAddEmail();
                                setOpen(false);
                            }}
                            className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[12px] font-medium text-slate-800 transition-colors hover:bg-slate-100"
                        >
                            <MailIcon className="w-3.5 h-3.5 text-sky-600" />
                            Send email
                            <span className="ml-auto rounded bg-slate-100 px-1 py-px text-[9px] uppercase tracking-[0.1em] text-slate-400">
                                default
                            </span>
                        </button>
                        <div className="my-1 border-t border-slate-100" />
                        {ADD_ACTION_OPTIONS.map((o) => {
                            const meta = ACTION_META[o.type];
                            const Icon = meta.Icon;
                            return (
                                <button
                                    key={o.type}
                                    type="button"
                                    onClick={() => {
                                        onAddAction(o.type);
                                        setOpen(false);
                                    }}
                                    className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[12px] text-slate-700 transition-colors hover:bg-slate-100"
                                >
                                    <Icon className={`w-3.5 h-3.5 ${meta.tint}`} />
                                    {o.label}
                                </button>
                            );
                        })}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// defaultActionFor returns a fresh action config for a newly-picked type.
function defaultActionFor(type: SequenceActionType): SequenceAction {
    if (type === "create_task") {
        return { type, task_priority: "medium", task_due_offset_days: 1 };
    }
    if (type === "create_deal") {
        return { type, deal_currency: "USD", deal_name: "{{.Company}} ({{.FirstName}})" };
    }
    if (type === "move_deal_stage") {
        return { type };
    }
    if (type === "run_automation") {
        return { type, automation_values: [] };
    }
    if (type === "http_request") {
        return { type, http_method: "POST", http_headers: {} };
    }
    if (type === "fire_event") {
        return { type, event_fields: [] };
    }
    if (type === "label_email") {
        return { type, label_ids: [] };
    }
    return { type };
}

// ── Node-type switcher (top of the editor drawer) ───────────────────────────
// One place to switch ANY node between Send email and every action type —
// mirrors the unified "Add" menu. Switching persists immediately and the drawer
// body re-renders to the matching editor (SequenceView for email, ActionEditor
// for actions).
function NodeTypeSwitcher({
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
function ActionEditor({
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
    React.useEffect(() => {
        setAction(sequence.action ?? { type: "add_tag" });
        setName(sequence.name ?? "");
    }, [sequence.id, sequence.action, sequence.kind, sequence.name]);

    const save = async () => {
        setSaving(true);
        try {
            await updateSequence(campaignId, sequence.id, {
                name,
                kind: "action",
                action,
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

            {(action.type === "add_tag" || action.type === "remove_tag") && (
                <div>
                    <Label>{action.type === "add_tag" ? "Tag to add" : "Tag to remove"}</Label>
                    <CategoryPicker
                        value={action.category_id ? [action.category_id] : []}
                        onChange={(ids) =>
                            setAction((a) => ({ ...a, category_id: ids.length ? ids[ids.length - 1] : null }))
                        }
                        placeholder="Pick a tag…"
                    />
                    <p className="mt-1.5 text-[11px] text-slate-400">Tags are your contact categories.</p>
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
            {action.type === "http_request" && <HttpRequestStepFields action={action} setAction={setAction} />}
            {action.type === "fire_event" && <FireEventStepFields action={action} setAction={setAction} />}

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

// HttpRequestStepFields — a configurable outbound call when the lead reaches this
// step. URL/headers/body template against the contact and are SSRF-guarded server
// side. Best-effort: a failure is logged and the campaign continues.
function HttpRequestStepFields({
    action,
    setAction,
}: {
    action: SequenceAction;
    setAction: React.Dispatch<React.SetStateAction<SequenceAction>>;
}) {
    const headers = action.http_headers ?? {};
    const headerRows = Object.entries(headers);
    const setHeaders = (next: Record<string, string>) => setAction((a) => ({ ...a, http_headers: next }));
    const methodOpts: SelectOption[] = ["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => ({ value: m, label: m }));

    return (
        <div className="space-y-3">
            <div className="flex items-end gap-2">
                <div className="shrink-0">
                    <Label>Method</Label>
                    <SelectMenu
                        value={action.http_method || "POST"}
                        onChange={(v) => setAction((a) => ({ ...a, http_method: v }))}
                        options={methodOpts}
                        minWidth={110}
                    />
                </div>
                <div className="flex-1 min-w-0">
                    <Label>URL</Label>
                    <TextInput
                        value={action.http_url ?? ""}
                        onChange={(v) => setAction((a) => ({ ...a, http_url: v }))}
                        placeholder="https://api.yourapp.com/hook"
                        className="w-full font-mono"
                    />
                </div>
            </div>
            <div>
                <Label>Body</Label>
                <textarea
                    value={action.http_body ?? ""}
                    onChange={(e) => setAction((a) => ({ ...a, http_body: e.target.value }))}
                    rows={3}
                    placeholder={'{"email": "{{.Email}}", "company": "{{.Company}}"}'}
                    className="w-full rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[12px] font-mono text-slate-900 placeholder:text-slate-400 outline-none resize-y focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                />
            </div>
            <div>
                <div className="mb-1.5 flex items-center justify-between gap-2">
                    <Label className="mb-0">Headers (optional)</Label>
                    <button
                        type="button"
                        onClick={() => setHeaders({ ...headers, "": "" })}
                        className="inline-flex h-6 items-center gap-1 rounded-md border border-slate-200 bg-white px-2 text-[11.5px] font-medium text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-900"
                    >
                        <PlusIcon className="w-3 h-3" /> Add header
                    </button>
                </div>
                {headerRows.length === 0 ? (
                    <p className="text-[11px] text-slate-400">Content-Type defaults to application/json when a body is set.</p>
                ) : (
                    <div className="space-y-1.5">
                        {headerRows.map(([k, v], i) => (
                            <div key={i} className="flex items-center gap-1.5">
                                <TextInput
                                    value={k}
                                    onChange={(nk) => {
                                        const next: Record<string, string> = {};
                                        headerRows.forEach(([hk, hv], idx) => {
                                            next[idx === i ? nk : hk] = hv;
                                        });
                                        setHeaders(next);
                                    }}
                                    placeholder="Authorization"
                                    className="w-36 shrink-0 font-mono"
                                />
                                <span className="text-slate-300">:</span>
                                <TextInput
                                    value={v}
                                    onChange={(nv) => setHeaders({ ...headers, [k]: nv })}
                                    placeholder="Bearer …"
                                    className="flex-1 min-w-0 font-mono"
                                />
                                <button
                                    type="button"
                                    onClick={() => {
                                        const next = { ...headers };
                                        delete next[k];
                                        setHeaders(next);
                                    }}
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
            <p className="text-[11px] text-slate-400 leading-relaxed">
                URL, headers, and body are templated per contact ({"{{.Email}}"}, {"{{.FirstName}}"}). Only public https URLs are allowed.
            </p>
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

// DealNameVariableMenu — a compact "insert variable" trigger for the deal-name
// field, mirroring the VariableMenu affordance the email subject/body use.
// Appends a {{.Token}} into the deal name so it can personalize per contact.
function DealNameVariableMenu({ onPick }: { onPick: (token: string) => void }) {
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));
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
    useClickOutside(ref, () => setOpen(false));
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
