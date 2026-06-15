// AutomationFlow — the visual automation builder, the same React Flow canvas as
// the campaign sequence editor: a Trigger node fans into Condition (IF) and
// Action nodes connected by edges. Drag from a node's dot to connect; drag from
// a condition's "yes"/"no" dots to branch; drag to empty canvas to drop a new
// action; click a node to edit it; click a line + Delete to remove it. The whole
// flow is edited locally and saved as a {nodes, edges} graph.

"use client";

import React from "react";
import { createPortal } from "react-dom";
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
    useReactFlow,
    type Node,
    type Edge,
    type Connection,
    type NodeProps,
    type EdgeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { AnimatePresence, motion } from "framer-motion";
import dagre from "@dagrejs/dagre";
import {
    ArrowLeftIcon,
    BriefcaseIcon,
    CheckCircle2Icon,
    CheckIcon,
    CheckSquareIcon,
    CopyIcon,
    GitBranchIcon,
    HistoryIcon,
    Link2Icon,
    Loader2Icon,
    MessageSquareIcon,
    PlayIcon,
    PlusIcon,
    SendIcon,
    TagIcon,
    TagsIcon,
    TriangleAlertIcon,
    Trash2Icon,
    UserMinusIcon,
    WandSparklesIcon,
    XCircleIcon,
    XIcon,
    ZapIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import PermissionButton from "@/components/ui/PermissionButton";
import { usePermission } from "@/hooks/usePermission";
import type { AppError } from "@/lib/api/client/normalizeError";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { useUpdateAutomation, useTestAutomation } from "@/lib/api/hooks/app/automations/useAutomationMutations";
import { useAutomationRuns } from "@/lib/api/hooks/app/automations/useAutomationRuns";
import type {
    Automation,
    AutomationCondition,
    AutomationGraph,
    AutomationEdge as GEdge,
    AutomationNodeResult,
    AutomationRun,
    DryRunResponse,
} from "@/lib/api/models/app/automations/Automation";
import {
    PROVIDER_LABELS,
    type IntegrationAction,
    type IntegrationCatalogEntry,
    type IntegrationConnection,
} from "@/lib/api/models/app/integrations/Integration";
import {
    TRIGGER_EVENTS,
    triggerLabel,
    actionLabel,
    actionNeedsChannel,
    actionNeedsURL,
    actionSupportsTemplate,
    conditionLabel,
    triggerConditionFields,
    triggerFieldDef,
    conditionFieldKey,
    conditionFromFieldKey,
    defaultConditionForTrigger,
    operatorsForType,
    triggerVariables,
    NATIVE_CONNECTION,
    NATIVE_ACTIONS,
    isNativeAction,
    nativeActionNeeds,
    triggerCarriesThread,
    triggerIsInboundWebhook,
    sampleEventData,
    type TriggerFieldDef,
} from "@/lib/api/models/app/automations/meta";
import { API_URL } from "@/lib/information";
import CategoryPicker from "@/components/app/contacts/CategoryPicker";
import { ExpressionReference } from "@/components/app/automations/ExpressionReference";
import DealStagePicker from "@/components/app/crm/DealStagePicker";
import TaskTypePicker from "@/components/app/crm/TaskTypePicker";
import AssigneeTeamPicker, { type AssigneeValue } from "@/components/app/crm/AssigneeTeamPicker";
import { useAutomations } from "@/lib/api/hooks/app/automations/useAutomations";
import ProviderGlyph from "@/app/app/integrations/_components/ProviderGlyph";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import { isSelfMutation } from "@/lib/realtime/selfActivity";
import { cn } from "@/lib/utils";

const NODE_W = 248;
const NODE_H = 92;

const uid = () => {
    try {
        return crypto.randomUUID();
    } catch {
        return `n_${Math.floor(performance.now())}_${Math.random().toString(36).slice(2, 8)}`;
    }
};

// ── Layout (dagre) + orphan banding, mirrored from CampaignFlow ──────────────
function layoutGraph(nodes: Node[], edges: Edge[]): Node[] {
    const g = new dagre.graphlib.Graph();
    g.setDefaultEdgeLabel(() => ({}));
    g.setGraph({ rankdir: "TB", nodesep: 180, ranksep: 130, marginx: 32, marginy: 32, edgesep: 100 });
    nodes.forEach((n) => {
        let w = NODE_W;
        let h = NODE_H;
        if (n.type === "condition") {
            w = 220;
            h = 46;
        } else if (n.type === "trigger") {
            h = 76;
        }
        g.setNode(n.id, { width: w, height: h });
    });
    edges.forEach((e) => g.setEdge(e.source, e.target, { weight: 1 }));
    dagre.layout(g);
    return nodes.map((n) => {
        const p = g.node(n.id);
        return p ? { ...n, position: { x: p.x - p.width / 2, y: p.y - p.height / 2 } } : n;
    });
}

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
    if (count <= 1) return nodes;
    const box = new Map<number, { minX: number; minY: number; maxY: number }>();
    for (const n of nodes) {
        const k = comp.get(n.id)!;
        const h = n.type === "condition" ? 46 : NODE_H;
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

// ── Custom nodes ─────────────────────────────────────────────────────────────
function TriggerNode({ data, selected }: NodeProps) {
    const d = data as { label: string };
    return (
        <div
            className={cn(
                "w-[248px] rounded-xl border bg-white shadow-sm transition-shadow duration-200 hover:shadow-md",
                selected ? "border-sky-400 ring-2 ring-sky-100" : "border-slate-200",
            )}
        >
            <div className="flex items-center gap-2 rounded-t-xl border-b border-slate-200/70 bg-gradient-to-r from-sky-50/80 to-white px-2.5 py-1.5">
                <span className="inline-flex size-5 shrink-0 items-center justify-center rounded-md bg-sky-100 text-sky-600 ring-1 ring-sky-200/70">
                    <ZapIcon className="w-3 h-3" />
                </span>
                <span className="text-[9.5px] font-semibold uppercase tracking-[0.12em] text-sky-500">When</span>
                <span className="ml-auto shrink-0 rounded bg-sky-600 px-1.5 py-px text-[9px] font-semibold uppercase tracking-[0.12em] text-white">
                    Trigger
                </span>
            </div>
            <div className="px-2.5 py-2">
                <div className="truncate text-[12.5px] font-semibold text-slate-800">{d.label}</div>
            </div>
            <Handle type="source" id="s" position={Position.Bottom} className="!h-4 !w-4 md:!h-3 md:!w-3 !border-2 !border-white !bg-sky-500" />
        </div>
    );
}

function ConditionNode({ data, selected }: NodeProps) {
    const d = data as { label: string; onDelete: () => void };
    return (
        <div
            className={cn(
                "rounded-lg border bg-gradient-to-b from-sky-50 to-white px-2 py-1 shadow-sm transition-shadow duration-200 hover:shadow-md",
                selected ? "border-sky-400 ring-2 ring-sky-100" : "border-sky-200",
            )}
        >
            <Handle type="target" position={Position.Top} className="!h-3 !w-3 md:!h-2 md:!w-2 !border-2 !border-white !bg-slate-300" />
            {/* Right dot = the YES (true) path */}
            <Handle type="source" id="out" position={Position.Right} className="!h-4 !w-4 md:!h-3 md:!w-3 !border-2 !border-white !bg-sky-500" />
            <div className="flex items-center gap-1.5">
                <GitBranchIcon className="w-3 h-3 shrink-0 text-sky-600" />
                <span className="text-[9.5px] font-semibold uppercase tracking-[0.12em] text-sky-500">if</span>
                <span className="max-w-[150px] truncate text-[11px] font-medium text-sky-800">{d.label}</span>
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        d.onDelete();
                    }}
                    title="Delete this condition"
                    className="nodrag inline-flex size-4 items-center justify-center rounded text-sky-400 hover:bg-rose-50 hover:text-rose-600"
                >
                    <Trash2Icon className="w-3 h-3" />
                </button>
            </div>
            {/* Bottom dot = the NO (false) path */}
            <Handle type="source" id="else" position={Position.Bottom} className="!h-4 !w-4 md:!h-3 md:!w-3 !border-2 !border-white !bg-slate-400" />
        </div>
    );
}

function ActionNode({ data, selected }: NodeProps) {
    const d = data as { title: string; sub: string; provider: string; native?: boolean; onDelete: () => void };
    return (
        <div
            className={cn(
                "w-[248px] rounded-xl border bg-white shadow-sm transition-shadow duration-200 hover:shadow-md",
                selected ? "border-sky-400 ring-2 ring-sky-100" : "border-slate-200",
            )}
        >
            <Handle type="target" position={Position.Top} className="!h-3 !w-3 md:!h-2 md:!w-2 !border-2 !border-white !bg-slate-300" />
            <div className="flex items-center gap-2 rounded-t-xl border-b border-slate-200/70 bg-gradient-to-r from-slate-50 to-white px-2.5 py-1.5">
                {d.provider ? (
                    <ProviderGlyph provider={d.provider} name={d.provider} size={7} />
                ) : d.native ? (
                    <span className="inline-flex size-5 shrink-0 items-center justify-center rounded-md bg-indigo-100 text-indigo-600 ring-1 ring-indigo-200/70">
                        <ZapIcon className="w-3 h-3" />
                    </span>
                ) : (
                    <span className="inline-flex size-5 shrink-0 items-center justify-center rounded-md bg-slate-100 text-slate-400 ring-1 ring-slate-200/70 text-[10px]">
                        ?
                    </span>
                )}
                <span className="min-w-0 flex-1 truncate text-[12.5px] font-semibold text-slate-800">{d.title}</span>
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
                <div className="text-[9.5px] font-semibold uppercase tracking-[0.12em] text-slate-300">Then</div>
                <div className="mt-0.5 truncate text-[11.5px] text-slate-500">{d.sub || "Pick an integration…"}</div>
            </div>
            <Handle type="source" id="s" position={Position.Bottom} className="!h-4 !w-4 md:!h-3 md:!w-3 !border-2 !border-white !bg-sky-500" />
            {/* "On error" branch: drag from here to route a failed action down a recovery path. */}
            <Handle type="source" id="err" position={Position.Right} title="On error" className="!h-3.5 !w-3.5 md:!h-2.5 md:!w-2.5 !border-2 !border-white !bg-rose-500" />
        </div>
    );
}

const nodeTypes = { trigger: TriggerNode, condition: ConditionNode, action: ActionNode };

// ── Convergent edge (labeled bezier), mirrored from CampaignFlow ─────────────
function ConvergeEdge({
    id,
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
    selected,
}: EdgeProps) {
    const { deleteElements } = useReactFlow();
    const [path, labelX, labelY] = getBezierPath({
        sourceX,
        sourceY,
        sourcePosition,
        targetX,
        targetY,
        targetPosition,
    });
    return (
        <>
            <BaseEdge path={path} markerEnd={markerEnd} style={style} />
            {label || selected ? (
                <EdgeLabelRenderer>
                    <div
                        className="nodrag nopan pointer-events-none absolute flex items-center gap-1"
                        style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
                    >
                        {label ? (
                            <div
                                className="rounded border px-1 py-px text-[10px]"
                                style={{
                                    borderColor: (labelBgStyle as { stroke?: string } | undefined)?.stroke ?? "#e2e8f0",
                                    background: (labelBgStyle as { fill?: string } | undefined)?.fill ?? "#fff",
                                    color: (labelStyle as { fill?: string } | undefined)?.fill ?? "#475569",
                                }}
                            >
                                {label}
                            </div>
                        ) : null}
                        {/* Touch-reachable delete: phones have no Delete key, so a
                            selected edge shows an X (deleteKeyCode still works on desktop). */}
                        {selected ? (
                            <button
                                type="button"
                                onClick={(e) => {
                                    e.stopPropagation();
                                    void deleteElements({ edges: [{ id }] });
                                }}
                                aria-label="Remove connection"
                                title="Remove connection"
                                className="pointer-events-auto inline-flex size-5 items-center justify-center rounded-full border border-slate-200 bg-white text-slate-400 shadow-sm hover:border-rose-200 hover:bg-rose-50 hover:text-rose-600"
                            >
                                <XIcon className="w-3 h-3" />
                            </button>
                        ) : null}
                    </div>
                </EdgeLabelRenderer>
            ) : null}
        </>
    );
}

const edgeTypes = { converge: ConvergeEdge };

type When = "" | "true" | "false" | "error";

function handleToWhen(h?: string | null): When {
    return h === "out" ? "true" : h === "else" ? "false" : h === "err" ? "error" : "";
}
function whenToHandle(w?: string): string {
    return w === "true" ? "out" : w === "false" ? "else" : w === "error" ? "err" : "s";
}

function styledEdge(id: string, source: string, target: string, sourceHandle: string, when: When): Edge {
    const color =
        when === "true" ? "#0ea5e9" : when === "false" ? "#94a3b8" : when === "error" ? "#f43f5e" : "#cbd5e1";
    return {
        id,
        source,
        target,
        sourceHandle,
        type: "converge",
        data: { when },
        label: when === "true" ? "yes" : when === "false" ? "no" : when === "error" ? "error" : undefined,
        markerEnd: { type: MarkerType.ArrowClosed, color, width: 16, height: 16 },
        style: { stroke: color, strokeWidth: 1.5 },
        labelStyle: { fill: color },
        labelBgStyle: { fill: "#fff", stroke: color },
    };
}

export default function AutomationFlow({
    automation,
    connections,
    catalog,
    onBack,
}: {
    automation: Automation;
    connections: IntegrationConnection[];
    catalog: IntegrationCatalogEntry[];
    onBack: () => void;
}) {
    const update = useUpdateAutomation();
    const test = useTestAutomation();

    // Collaboration: claim this automation while the builder is open so a
    // teammate sees who's here. Editors show as "editing"; members without the
    // integration permission (view-only) show as "viewing".
    const canEditAutomation = usePermission("USE_INTEGRATIONS");
    usePresenceResource(`automation:${automation.id}`, canEditAutomation ? "editing" : "viewing");

    const [name, setName] = React.useState(automation.name);
    const [enabled, setEnabled] = React.useState(automation.enabled);
    const [trigger, setTrigger] = React.useState(automation.trigger_event);

    const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
    const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
    const [selectedId, setSelectedId] = React.useState<string | null>("trigger");
    // Drag a node's dot to empty canvas -> a menu opens at the drop point to pick
    // what comes next (action / built-in action / condition), mirroring the
    // campaign steps canvas. `when` carries the source handle (yes/no for a
    // condition block) so the new edge keeps its branch label.
    const [dragCreate, setDragCreate] = React.useState<{ x: number; y: number; sourceId: string; when: When } | null>(null);
    const connectStartRef = React.useRef<string | null>(null);
    const connectHandleRef = React.useRef<string | null>(null);
    // Right-side insights panel: dry-run trace ("test") or run history ("history").
    const [panel, setPanel] = React.useState<"test" | "history" | null>(null);
    const [testResult, setTestResult] = React.useState<DryRunResponse | null>(null);
    const seeded = React.useRef(false);
    // A teammate saved while we have unsaved edits — offer to load their version.
    const [remoteUpdate, setRemoteUpdate] = React.useState<Automation | null>(null);

    const confirm = useConfirm();

    // Dirty tracking: a stable signature of everything we persist (name, enabled,
    // trigger, and the graph). The baseline is captured from the seeded canvas and
    // reset on every successful save, so the Save button only lights up — and the
    // leave guard only fires — when there are real unsaved changes. Positions are
    // rounded so sub-pixel drift never reads as a change.
    const baselineRef = React.useRef<string>("");
    const flowSig = React.useCallback(
        (nm: string, en: boolean, tr: string, ns: Node[], es: Edge[]) =>
            JSON.stringify({
                name: nm.trim(),
                enabled: en,
                trigger: tr,
                nodes: ns.map((n) => {
                    const d = n.data as { action?: string; connection_id?: string; config?: unknown; condition?: unknown };
                    return {
                        id: n.id,
                        type: n.type,
                        x: Math.round(n.position.x),
                        y: Math.round(n.position.y),
                        action: d?.action ?? null,
                        connection_id: d?.connection_id ?? null,
                        config: d?.config ?? null,
                        condition: d?.condition ?? null,
                    };
                }),
                edges: es.map((e) => ({ id: e.id, source: e.source, target: e.target, when: (e.data as { when?: string })?.when ?? "" })),
            }),
        [],
    );
    const signature = React.useMemo(() => flowSig(name, enabled, trigger, nodes, edges), [flowSig, name, enabled, trigger, nodes, edges]);
    const dirty = baselineRef.current !== "" && signature !== baselineRef.current;

    // Connection helpers ------------------------------------------------------
    const targets = React.useMemo(
        () =>
            connections.filter(
                (c) =>
                    (c.status === "connected" || c.status === "degraded") &&
                    c.provider !== "calendly" &&
                    c.provider !== "cal_com",
            ),
        [connections],
    );
    const connById = React.useMemo(() => {
        const m: Record<string, IntegrationConnection> = {};
        for (const c of connections) m[c.id] = c;
        return m;
    }, [connections]);
    const actionsForProvider = React.useCallback(
        (provider?: string): string[] => catalog.find((e) => e.provider === provider)?.action_types ?? [],
        [catalog],
    );
    const connLabel = React.useCallback(
        (id?: string) => {
            if (!id) return "";
            const c = connById[id];
            if (!c) return "Unknown integration";
            const provider = PROVIDER_LABELS[c.provider] ?? c.provider;
            return c.label && c.label.toLowerCase() !== c.provider ? `${provider} · ${c.label}` : provider;
        },
        [connById],
    );
    const providerOf = React.useCallback((id?: string) => (id ? connById[id]?.provider ?? "" : ""), [connById]);

    const deleteNode = React.useCallback(
        (id: string) => {
            if (id === "trigger") return;
            setNodes((ns) => ns.filter((n) => n.id !== id));
            setEdges((es) => es.filter((e) => e.source !== id && e.target !== id));
            setSelectedId((cur) => (cur === id ? null : cur));
        },
        [setNodes, setEdges],
    );

    // Build an RF node from graph data.
    const toRFNode = React.useCallback(
        (n: AutomationGraph["nodes"][number]): Node => {
            const position = { x: n.x ?? 0, y: n.y ?? 0 };
            if (n.type === "trigger") {
                return { id: n.id, type: "trigger", position, deletable: false, data: { label: triggerLabel(automation.trigger_event) } };
            }
            if (n.type === "condition") {
                return {
                    id: n.id,
                    type: "condition",
                    position,
                    data: { condition: n.condition ?? { field: "intent", operator: "equals" }, label: conditionLabel(n.condition), onDelete: () => deleteNode(n.id) },
                };
            }
            return {
                id: n.id,
                type: "action",
                position,
                data: {
                    action: n.action,
                    connection_id: n.connection_id,
                    config: n.config ?? {},
                    title: n.action ? actionLabel(n.action) : "Choose an action",
                    sub: isNativeAction(String(n.action ?? "")) ? "Built-in action" : connLabel(n.connection_id),
                    provider: providerOf(n.connection_id),
                    native: isNativeAction(String(n.action ?? "")),
                    onDelete: () => deleteNode(n.id),
                },
            };
        },
        [automation.trigger_event, connLabel, providerOf, deleteNode],
    );

    // Lay a server automation onto the canvas (used on first mount AND when a
    // teammate's save arrives over realtime). Also resets the dirty baseline.
    const seedFrom = React.useCallback(
        (a: Automation) => {
            setName(a.name);
            setEnabled(a.enabled);
            setTrigger(a.trigger_event);
            let g = a.graph?.nodes?.length
                ? a.graph
                : ({ nodes: [{ id: "trigger", type: "trigger", x: 0, y: 0 }], edges: [] } as AutomationGraph);
            if (!g.nodes.some((n) => n.type === "trigger")) {
                g = { nodes: [{ id: "trigger", type: "trigger", x: 0, y: 0 }, ...g.nodes], edges: g.edges };
            }
            let rfNodes = g.nodes.map(toRFNode);
            const rfEdges = (g.edges ?? []).map((e: GEdge) =>
                styledEdge(e.id || uid(), e.source, e.target, whenToHandle(e.when), (e.when ?? "") as When),
            );
            const noPositions = g.nodes.length > 1 && g.nodes.every((n) => (n.x ?? 0) === 0 && (n.y ?? 0) === 0);
            if (noPositions) rfNodes = stackComponents(layoutGraph(rfNodes, rfEdges), rfEdges);
            setNodes(rfNodes);
            setEdges(rfEdges);
            baselineRef.current = flowSig(a.name, a.enabled, a.trigger_event, rfNodes, rfEdges);
        },
        [toRFNode, setNodes, setEdges, flowSig],
    );

    // A monotonic version token for the SERVER automation, used to tell a
    // teammate's save apart from our own. `updated_at` is bumped on every write
    // and our own save's response carries the new value, so comparing it is
    // exact — unlike a deep content signature, which can read back subtly
    // different (key order, defaults) and make the editor falsely think a
    // teammate changed it. Falls back to a content hash only if a record somehow
    // has no timestamp.
    const serverVersion = React.useCallback((a: Automation) => {
        // The API client revives ISO date strings into Date objects, so
        // `updated_at` is a Date here, not a string. Normalize to a primitive:
        // comparing Date instances with === is by reference, so two reads of the
        // same timestamp would never match and the editor would flag its OWN
        // save as a teammate change.
        const u = a.updated_at as unknown;
        if (u instanceof Date) return `t:${u.getTime()}`;
        if (typeof u === "string" && u) return `t:${u}`;
        return JSON.stringify({ name: (a.name || "").trim(), enabled: a.enabled, trigger: a.trigger_event, graph: a.graph ?? null });
    }, []);
    const serverVersionRef = React.useRef("");
    // Our own save broadcasts an AUDIT_CREATED event that invalidates and
    // refetches this automation; that refetch can land before the save's HTTP
    // response updates serverVersionRef, which would mis-read our own write as a
    // teammate's. While this window is open, treat an incoming change as ours.
    const selfSaveUntil = React.useRef(0);

    // Seed once on mount.
    React.useEffect(() => {
        if (seeded.current) return;
        seeded.current = true;
        seedFrom(automation);
        serverVersionRef.current = serverVersion(automation);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // Live collaboration: when the automation changes on the server (a teammate
    // saved), reflect it. If we have no unsaved edits, apply it instantly; if we
    // do, surface a non-destructive banner so we never clobber local work.
    React.useEffect(() => {
        if (!seeded.current) return;
        const incoming = serverVersion(automation);
        if (incoming === serverVersionRef.current) return; // unchanged / our own save
        // Our own change — either an in-editor save whose realtime refetch raced
        // the HTTP response, OR a change WE made elsewhere (the list "Turn on"
        // toggle, another tab) that round-tripped back. Either way it is not a
        // teammate: sync silently, never toast.
        if (Date.now() < selfSaveUntil.current || isSelfMutation("automation", automation.id)) {
            serverVersionRef.current = incoming;
            if (!dirty) seedFrom(automation);
            return;
        }
        if (!dirty) {
            seedFrom(automation);
            serverVersionRef.current = incoming;
            toast.success("Updated by a teammate", { id: "automation-remote" });
        } else {
            serverVersionRef.current = incoming; // mark seen so we don't re-prompt
            setRemoteUpdate(automation);
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [automation, dirty]);

    const updateNodeData = React.useCallback(
        (id: string, patch: Record<string, unknown>) =>
            setNodes((ns) => ns.map((n) => (n.id === id ? { ...n, data: { ...n.data, ...patch } } : n))),
        [setNodes],
    );

    // Connect / drag handlers -------------------------------------------------
    const onConnect = React.useCallback(
        (c: Connection) => {
            if (!c.source || !c.target || c.source === c.target) return;
            const when = handleToWhen(c.sourceHandle);
            setEdges((es) => {
                // one edge per (source, handle) for true/false; replace if re-dragged
                const filtered =
                    when === ""
                        ? es
                        : es.filter((e) => !(e.source === c.source && (e.data as { when?: string })?.when === when));
                return [...filtered, styledEdge(uid(), c.source!, c.target!, whenToHandle(when), when)];
            });
        },
        [setEdges],
    );

    const addNode = React.useCallback(
        (type: "action" | "condition", at?: { x: number; y: number }, presetAction?: string) => {
            const id = uid();
            const maxY = nodes.reduce((m, n) => Math.max(m, n.position.y), 0);
            const position = at ?? { x: 0, y: maxY + 140 };
            const defCond = defaultConditionForTrigger(trigger);
            const native = !!presetAction && isNativeAction(presetAction);
            const node: Node =
                type === "condition"
                    ? {
                          id,
                          type: "condition",
                          position,
                          data: { condition: defCond, label: conditionLabel(defCond), onDelete: () => deleteNode(id) },
                      }
                    : {
                          id,
                          type: "action",
                          position,
                          data: presetAction
                              ? {
                                    action: presetAction,
                                    connection_id: undefined,
                                    config: {},
                                    title: actionLabel(presetAction),
                                    sub: native ? "Built-in action" : "Pick an integration…",
                                    provider: "",
                                    native,
                                    onDelete: () => deleteNode(id),
                                }
                              : { action: "", connection_id: undefined, config: {}, title: "Choose an action", sub: "Pick an integration…", provider: "", onDelete: () => deleteNode(id) },
                      };
            setNodes((ns) => [...ns, node]);
            setSelectedId(id);
            return id;
        },
        [nodes, setNodes, deleteNode, trigger],
    );

    // Create a node from the drag-create menu and wire the source's dot to it.
    // The new node is dropped just below its source so the canvas stays tidy
    // without a full re-layout. A condition's yes/no edge is one-per-handle, so
    // re-dragging the same branch replaces it.
    const createConnectedNode = React.useCallback(
        (choice: string, sourceId: string, when: When) => {
            const src = nodes.find((n) => n.id === sourceId);
            const base = src?.position ?? { x: 0, y: 0 };
            const at = { x: base.x, y: base.y + 140 };
            const newId =
                choice === "condition" ? addNode("condition", at) : addNode("action", at, choice === "action" ? undefined : choice);
            setEdges((es) => {
                const filtered =
                    when === "" ? es : es.filter((e) => !(e.source === sourceId && (e.data as { when?: string })?.when === when));
                return [...filtered, styledEdge(uid(), sourceId, newId, whenToHandle(when), when)];
            });
        },
        [nodes, addNode, setEdges],
    );

    const onReconnect = React.useCallback(
        (oldEdge: Edge, conn: Connection) => {
            if (!conn.target) return;
            setEdges((es) => es.map((e) => (e.id === oldEdge.id ? { ...e, target: conn.target! } : e)));
        },
        [setEdges],
    );

    const save = async (): Promise<boolean> => {
        const actionNodes = nodes.filter((n) => n.type === "action");
        for (const n of actionNodes) {
            const d = n.data as { action?: string; connection_id?: string; config?: Record<string, unknown> };
            if (!d.action) {
                toast.error("Every action needs an action");
                setSelectedId(n.id);
                return false;
            }
            if (isNativeAction(d.action)) {
                const need = nativeActionNeeds(d.action);
                if (need === "tag" && !String(d.config?.category_id ?? "").trim()) {
                    toast.error("A tag action needs a tag");
                    setSelectedId(n.id);
                    return false;
                }
                if (need === "label" && !(Array.isArray(d.config?.label_ids) && d.config.label_ids.length > 0)) {
                    toast.error("A label action needs at least one label");
                    setSelectedId(n.id);
                    return false;
                }
                if (need === "label" && !triggerCarriesThread(trigger)) {
                    toast.error("Label email only runs on a “Reply received” automation");
                    setSelectedId(n.id);
                    return false;
                }
                if (need === "deal" && (!String(d.config?.deal_pipeline_id ?? "").trim() || !String(d.config?.deal_stage_id ?? "").trim())) {
                    toast.error("A deal action needs a pipeline and stage");
                    setSelectedId(n.id);
                    return false;
                }
                if (need === "automation" && !String(d.config?.automation_id ?? "").trim()) {
                    toast.error("A run-automation action needs a target automation");
                    setSelectedId(n.id);
                    return false;
                }
                if (
                    need === "vars" &&
                    !(Array.isArray(d.config?.set_vars) &&
                        (d.config.set_vars as { key?: string }[]).some((v) => String(v?.key ?? "").trim()))
                ) {
                    toast.error("A set-variables action needs at least one named variable");
                    setSelectedId(n.id);
                    return false;
                }
                if (need === "event" && !String(d.config?.event_name ?? "").trim()) {
                    toast.error("A fire-event action needs an event name");
                    setSelectedId(n.id);
                    return false;
                }
                continue; // native actions need no connection
            }
            if (!d.connection_id) {
                toast.error("Every integration action needs an integration");
                setSelectedId(n.id);
                return false;
            }
            if (actionNeedsChannel(d.action) && !String(d.config?.channel ?? "").trim()) {
                toast.error("A Slack action needs a channel");
                setSelectedId(n.id);
                return false;
            }
            if (actionNeedsURL(d.action) && !String(d.config?.url ?? "").trim()) {
                toast.error("A webhook action needs a URL");
                setSelectedId(n.id);
                return false;
            }
        }
        // Filtering is done with condition (IF) nodes now, so no automation-wide
        // filter is sent.
        const filter: Record<string, unknown> = {};
        const graph: AutomationGraph = {
            nodes: nodes.map((n) => {
                const base = { id: n.id, type: n.type as AutomationGraph["nodes"][number]["type"], x: n.position.x, y: n.position.y };
                if (n.type === "condition") {
                    return { ...base, condition: (n.data as { condition: AutomationCondition }).condition };
                }
                if (n.type === "action") {
                    const d = n.data as { action?: string; connection_id?: string; config?: Record<string, unknown> };
                    return { ...base, action: d.action as IntegrationAction, connection_id: d.connection_id, config: d.config };
                }
                return base;
            }),
            edges: edges.map((e) => ({
                id: e.id,
                source: e.source,
                target: e.target,
                when: ((e.data as { when?: string })?.when ?? "") as "" | "true" | "false" | "error",
            })),
        };
        // Open the self-save window NOW so a realtime refetch racing the HTTP
        // response is recognized as our own write, not a teammate's.
        selfSaveUntil.current = Date.now() + 8000;
        try {
            const res = await update.mutateAsync({ id: automation.id, w: { name: name.trim() || "Automation", enabled, trigger_event: trigger, filter, graph } });
            // Re-baseline so the canvas is no longer "dirty" after a successful save.
            baselineRef.current = flowSig(name, enabled, trigger, nodes, edges);
            // Mark this as the known server version so our own refetch doesn't
            // read back as a "teammate changed it" event.
            if (res?.automation) serverVersionRef.current = serverVersion(res.automation);
            selfSaveUntil.current = Date.now() + 8000;
            setRemoteUpdate(null);
            toast.success("Automation saved");
            return true;
        } catch (e) {
            // Show the backend's reason (e.g. "an action node has no integration
            // selected", a permission denial, or the paid-plan gate) instead of a
            // generic message, so a failed save is actually actionable.
            const msg = (e as AppError)?.message;
            toast.error(msg ? `Could not save automation: ${msg}` : "Could not save automation");
            return false;
        }
    };

    // Dry-run (no side effects) against the given sample event, skipping the
    // action steps the user toggled off. Persists the canvas first only when there
    // are unsaved edits, so we test what's on screen.
    const runTest = async (data?: Record<string, unknown>, skipNodeIds?: string[]) => {
        if (dirty && !(await save())) return;
        try {
            const res = await test.mutateAsync({ id: automation.id, data, skipNodeIds });
            setTestResult(res);
            setPanel("test");
        } catch {
            toast.error("Could not run the test");
        }
    };

    // The action steps (in canvas order) the test panel lists with on/off toggles.
    const actionSteps = React.useMemo(
        () =>
            nodes
                .filter((n) => n.type === "action")
                .map((n) => {
                    const a = (n.data as { action?: string }).action;
                    return { id: n.id, label: a ? actionLabel(String(a)) : "Unconfigured action" };
                }),
        [nodes],
    );

    const selectedNode = nodes.find((n) => n.id === selectedId) ?? null;

    // Leaving the builder with unsaved changes asks first; a clean canvas leaves
    // immediately.
    const guardedBack = () => {
        if (dirty) {
            confirm.show("You have unsaved changes. Leave without saving?", () => onBack());
            return;
        }
        onBack();
    };

    return (
        <div className="h-full flex flex-col">
            <header className="min-h-12 py-1.5 md:h-12 md:py-0 px-3 border-b border-slate-200 flex flex-wrap md:flex-nowrap items-center gap-2 gap-y-1.5 shrink-0 bg-white">
                <button
                    type="button"
                    onClick={guardedBack}
                    className="h-7 w-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center"
                    aria-label="Back"
                >
                    <ArrowLeftIcon className="w-4 h-4" />
                </button>
                <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Automation name"
                    className="h-7 px-2 w-56 max-w-[30vw] md:max-w-[36vw] rounded-md text-[13px] font-medium text-slate-900 outline-none hover:bg-slate-50 focus:bg-white focus:border-sky-400 focus:ring-2 focus:ring-sky-100 border border-transparent"
                />
                <ResourceViewers resource={`automation:${automation.id}`} className="shrink-0" />
                <button
                    type="button"
                    role="switch"
                    aria-checked={enabled}
                    aria-label="Enable automation"
                    onClick={() => setEnabled((v) => !v)}
                    title={enabled ? "Automation is live" : "Automation is paused"}
                    className="inline-flex h-7 cursor-pointer select-none items-center gap-2 rounded-md outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                >
                    <span
                        className={cn(
                            "relative inline-flex h-[18px] w-8 shrink-0 items-center rounded-full transition-colors",
                            enabled ? "bg-sky-600" : "bg-slate-300",
                        )}
                    >
                        <span
                            className={cn(
                                "inline-block size-3.5 rounded-full bg-white shadow-sm transition-transform duration-150",
                                enabled ? "translate-x-[16px]" : "translate-x-[2px]",
                            )}
                        />
                    </span>
                    <span className={cn("text-[12px] font-medium transition-colors", enabled ? "text-slate-700" : "text-slate-400")}>
                        {enabled ? "Active" : "Off"}
                    </span>
                </button>
                <div className="ml-auto flex items-center gap-1.5">
                    <button
                        type="button"
                        onClick={() => {
                            setSelectedId(null);
                            setPanel((p) => (p === "history" ? null : "history"));
                        }}
                        aria-label="History"
                        className={cn(
                            "h-7 px-2.5 rounded-md border text-[12px] inline-flex items-center gap-1.5 transition-colors",
                            panel === "history" ? "border-sky-300 bg-sky-50 text-sky-700" : "border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900",
                        )}
                    >
                        <HistoryIcon className="w-3.5 h-3.5" />
                        <span className="hidden md:inline">History</span>
                    </button>
                    <button
                        type="button"
                        onClick={() => {
                            setSelectedId(null);
                            setPanel((p) => (p === "test" ? null : "test"));
                        }}
                        aria-label="Test"
                        className={cn(
                            "h-7 px-2.5 rounded-md border text-[12px] inline-flex items-center gap-1.5 transition-colors",
                            panel === "test"
                                ? "border-sky-300 bg-sky-50 text-sky-700"
                                : "border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900",
                        )}
                    >
                        <PlayIcon className="w-3.5 h-3.5" />
                        <span className="hidden md:inline">Test</span>
                    </button>
                    <button
                        type="button"
                        onClick={() => setNodes((ns) => stackComponents(layoutGraph(ns, edges), edges))}
                        aria-label="Tidy up"
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center transition-colors"
                    >
                        <WandSparklesIcon className="w-3.5 h-3.5 md:hidden" />
                        <span className="hidden md:inline">Tidy up</span>
                    </button>
                    <PermissionButton
                        permission="USE_INTEGRATIONS"
                        type="button"
                        onClick={save}
                        disabled={!dirty || update.isPending}
                        aria-label={dirty ? "Save" : "Saved"}
                        title={dirty ? "Save changes" : "No unsaved changes"}
                        className={cn(
                            "h-7 px-3 rounded-md text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors",
                            dirty
                                ? "bg-sky-600 hover:bg-sky-700 text-white shadow-sm"
                                : "bg-slate-100 text-slate-400 cursor-default",
                        )}
                    >
                        {update.isPending ? (
                            <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                        ) : dirty ? (
                            <CheckIcon className="w-3.5 h-3.5" />
                        ) : (
                            <CheckIcon className="w-3.5 h-3.5 text-slate-300" />
                        )}
                        <span className="hidden md:inline">{dirty ? "Save" : "Saved"}</span>
                    </PermissionButton>
                </div>
            </header>

            <div className="campaign-flow relative flex-1 min-h-0 bg-slate-50/40">
                <ReactFlow
                    nodes={nodes}
                    edges={edges}
                    onNodesChange={onNodesChange}
                    onEdgesChange={onEdgesChange}
                    onConnect={onConnect}
                    onConnectStart={(_, params) => {
                        connectStartRef.current = params.nodeId ?? null;
                        connectHandleRef.current = params.handleId ?? null;
                    }}
                    onConnectEnd={(event, state) => {
                        const fromId = state?.fromNode?.id ?? connectStartRef.current;
                        const fromHandle = state?.fromHandle?.id ?? connectHandleRef.current;
                        connectStartRef.current = null;
                        connectHandleRef.current = null;
                        // Only when the line is dropped on EMPTY canvas (the pane). A
                        // drop on a node/handle is a real connection onConnect handled.
                        // The pane class is the reliable v12 signal (toNode is not).
                        const onPane = (event.target as Element | null)?.classList?.contains("react-flow__pane");
                        if (!fromId || !onPane || !canEditAutomation) return;
                        const pt =
                            "changedTouches" in event && event.changedTouches.length
                                ? event.changedTouches[0]
                                : (event as MouseEvent);
                        setDragCreate({ x: pt.clientX, y: pt.clientY, sourceId: fromId, when: handleToWhen(fromHandle) });
                    }}
                    onReconnect={onReconnect}
                    deleteKeyCode={["Backspace", "Delete"]}
                    nodeTypes={nodeTypes}
                    edgeTypes={edgeTypes}
                    onNodeClick={(_, node) => {
                        setSelectedId(node.id);
                        setPanel(null);
                    }}
                    onPaneClick={() => setSelectedId(null)}
                    zoomOnScroll={false}
                    panOnScroll={false}
                    preventScrolling={false}
                    minZoom={0.2}
                    maxZoom={1.75}
                    fitView
                    proOptions={{ hideAttribution: true }}
                >
                    <Background color="#e9eef5" gap={24} size={1} />
                    <Controls showInteractive={false} />
                    {remoteUpdate && (
                        <Panel position="top-center">
                            <div className="flex items-center gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-1.5 shadow-sm">
                                <span className="text-[12px] font-medium text-amber-800">
                                    A teammate changed this automation.
                                </span>
                                <button
                                    type="button"
                                    onClick={() => {
                                        seedFrom(remoteUpdate);
                                        setRemoteUpdate(null);
                                    }}
                                    className="h-6 px-2 rounded bg-amber-600 hover:bg-amber-700 text-white text-[11.5px] font-medium transition-colors"
                                >
                                    Load their version
                                </button>
                                <button
                                    type="button"
                                    onClick={() => setRemoteUpdate(null)}
                                    className="h-6 px-2 rounded text-[11.5px] font-medium text-amber-700 hover:bg-amber-100 transition-colors"
                                >
                                    Keep mine
                                </button>
                            </div>
                        </Panel>
                    )}
                    <Panel position="top-left">
                        <div className="flex items-center gap-1.5">
                            <button
                                type="button"
                                onClick={() => addNode("action")}
                                className="inline-flex h-8 items-center gap-1.5 rounded-md bg-sky-600 px-2.5 text-[12px] font-medium text-white shadow-sm hover:bg-sky-700"
                            >
                                <PlusIcon className="w-3.5 h-3.5" />
                                Add action
                            </button>
                            <button
                                type="button"
                                onClick={() => addNode("condition")}
                                className="inline-flex h-8 items-center gap-1.5 rounded-md border border-slate-200 bg-white px-2.5 text-[12px] font-medium text-slate-700 shadow-sm hover:border-slate-300 hover:text-slate-900"
                            >
                                <GitBranchIcon className="w-3.5 h-3.5" />
                                Add condition
                            </button>
                        </div>
                    </Panel>
                    <Panel position="bottom-center">
                        <div className="hidden md:block rounded-md bg-white/95 px-3 py-1.5 text-[11px] text-slate-500 shadow-sm">
                            drag a node's dot to connect · IF block: right dot = yes, bottom dot = no · drag to empty canvas to pick what comes next · click a line then Delete to remove
                        </div>
                    </Panel>
                </ReactFlow>

                {dragCreate && (
                    <DragCreateMenu
                        x={dragCreate.x}
                        y={dragCreate.y}
                        onClose={() => setDragCreate(null)}
                        onPick={(choice) => createConnectedNode(choice, dragCreate.sourceId, dragCreate.when)}
                    />
                )}

                <AnimatePresence>
                {selectedNode && !panel && (
                    <SidePanel key="editor">
                    <NodeEditor
                        key={selectedNode.id}
                        node={selectedNode}
                        onClose={() => setSelectedId(null)}
                        selfId={automation.id}
                        // trigger
                        trigger={trigger}
                        onTrigger={(ev) => {
                            setTrigger(ev);
                            updateNodeData("trigger", { label: triggerLabel(ev) });
                        }}
                        inboundUrl={automation.inbound_url}
                        // condition
                        onCondition={(cond) => updateNodeData(selectedNode.id, { condition: cond, label: conditionLabel(cond) })}
                        // action
                        targets={targets}
                        connLabel={connLabel}
                        actionsForProvider={actionsForProvider}
                        providerOf={providerOf}
                        onAction={(patch) => updateNodeData(selectedNode.id, patch)}
                    />
                    </SidePanel>
                )}

                {panel && (
                    <SidePanel key="insights">
                    <InsightsPanel
                        mode={panel}
                        automationId={automation.id}
                        trigger={trigger}
                        steps={actionSteps}
                        testResult={testResult}
                        testing={test.isPending}
                        onRun={runTest}
                        onClose={() => setPanel(null)}
                    />
                    </SidePanel>
                )}
                </AnimatePresence>
            </div>
        </div>
    );
}

// ── Side panels ──────────────────────────────────────────────────────────────
// Shared slide-in wrapper for the builder's right-side panels (node editor,
// test run, history). Same motion language as the app's other sheets.
function SidePanel({ children }: { children: React.ReactNode }) {
    return (
        <motion.div
            initial={{ x: "100%" }}
            animate={{ x: 0 }}
            exit={{ x: "100%" }}
            transition={{ type: "spring", stiffness: 300, damping: 32 }}
            className="absolute top-0 right-0 h-full w-full md:w-80 md:max-w-[88vw] bg-white border-l border-slate-200 shadow-xl flex flex-col z-10"
        >
            {children}
        </motion.div>
    );
}

// ── Insights panel: dry-run trace + run history ─────────────────────────────
function nodeStatusIcon(status: string) {
    if (status === "error") return <XCircleIcon className="w-3.5 h-3.5 text-rose-500" />;
    if (status === "branch_true") return <CheckCircle2Icon className="w-3.5 h-3.5 text-emerald-500" />;
    if (status === "branch_false") return <XCircleIcon className="w-3.5 h-3.5 text-slate-400" />;
    if (status === "skipped") return <span className="inline-block w-3.5 h-3.5 rounded-full border border-slate-300" aria-hidden />;
    return <CheckCircle2Icon className="w-3.5 h-3.5 text-emerald-500" />;
}

function NodeResultRow({ r }: { r: AutomationNodeResult }) {
    return (
        <div className="rounded-md border border-slate-200 px-2.5 py-1.5">
            <div className="flex items-center gap-1.5">
                {nodeStatusIcon(r.status)}
                <span className={cn("text-[11.5px] font-medium", r.status === "skipped" ? "text-slate-400" : "text-slate-700")}>
                    {r.type === "condition" ? "IF" : r.type === "action" ? actionLabel(r.action ?? "") : r.type}
                </span>
                {r.type === "condition" && (
                    <span className="ml-auto text-[10.5px] font-medium text-slate-400">
                        {r.status === "branch_true" ? "→ yes" : "→ no"}
                    </span>
                )}
                {r.type === "action" && r.status === "skipped" && (
                    <span className="ml-auto text-[10.5px] font-medium text-slate-400">skipped</span>
                )}
            </div>
            {r.label && r.type === "condition" && <div className="mt-0.5 text-[11px] text-slate-400">{r.label}</div>}
            {r.error && <div className="mt-0.5 text-[11px] text-rose-600">{r.error}</div>}
            {r.preview && Object.keys(r.preview).length > 0 && (
                <div className="mt-1 space-y-0.5">
                    {Object.entries(r.preview).map(([k, v]) => (
                        <div key={k} className="text-[10.5px] text-slate-500">
                            <span className="text-slate-400">{k}:</span> <span className="font-mono">{String(v)}</span>
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}

function InsightsPanel({
    mode,
    automationId,
    trigger,
    steps,
    testResult,
    testing,
    onRun,
    onClose,
}: {
    mode: "test" | "history";
    automationId: string;
    trigger: string;
    steps: { id: string; label: string }[];
    testResult: DryRunResponse | null;
    testing: boolean;
    onRun: (data: Record<string, unknown>, skipNodeIds: string[]) => void;
    onClose: () => void;
}) {
    const runs = useAutomationRuns(automationId, mode === "history");

    // Editable sample event the dry-run evaluates against, seeded per trigger and
    // re-seeded when the trigger changes (its payload shape changes with it).
    const [sample, setSample] = React.useState<string>(() => JSON.stringify(sampleEventData(trigger), null, 2));
    const [sampleErr, setSampleErr] = React.useState<string | null>(null);
    // Action steps the user toggled OFF for this test (skipped in the dry-run).
    const [disabled, setDisabled] = React.useState<Set<string>>(new Set());
    React.useEffect(() => {
        setSample(JSON.stringify(sampleEventData(trigger), null, 2));
        setSampleErr(null);
    }, [trigger]);
    const resetSample = () => {
        setSample(JSON.stringify(sampleEventData(trigger), null, 2));
        setSampleErr(null);
    };
    const toggleStep = (id: string) =>
        setDisabled((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });
    const runWithSample = () => {
        let parsed: unknown;
        try {
            parsed = JSON.parse(sample);
        } catch (e) {
            setSampleErr((e as Error).message || "Invalid JSON");
            return;
        }
        if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
            setSampleErr("Sample data must be a JSON object.");
            return;
        }
        setSampleErr(null);
        onRun(parsed as Record<string, unknown>, [...disabled]);
    };

    return (
        <div className="flex h-full min-h-0 flex-col">
            <div className="h-11 px-3 flex items-center border-b border-slate-200 shrink-0">
                <span className="text-[12.5px] font-medium text-slate-900">{mode === "test" ? "Test run" : "Run history"}</span>
                <button
                    type="button"
                    onClick={onClose}
                    className="ml-auto h-7 w-7 rounded-md inline-flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-100"
                    aria-label="Close"
                >
                    <XIcon className="w-4 h-4" />
                </button>
            </div>

            <motion.div
                key={mode}
                initial={{ opacity: 0, y: 4 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.15 }}
                className="flex-1 overflow-auto p-3 space-y-2"
            >
                {mode === "test" ? (
                    <div className="space-y-3">
                        <p className="text-[11px] text-slate-400 leading-relaxed">
                            Dry run: no messages are sent and no records change. Edit the sample event your automation receives, then run to see the path it takes and what each action would send.
                        </p>
                        <div>
                            <div className="mb-1 flex items-center justify-between">
                                <span className="text-[11px] font-medium text-slate-600">Sample event data</span>
                                <button
                                    type="button"
                                    onClick={resetSample}
                                    className="text-[11px] text-sky-600 hover:text-sky-700"
                                >
                                    Reset to sample
                                </button>
                            </div>
                            <textarea
                                value={sample}
                                onChange={(e) => setSample(e.target.value)}
                                spellCheck={false}
                                rows={8}
                                className={cn(
                                    "w-full rounded-md border bg-white px-2 py-1.5 text-[11.5px] font-mono text-slate-800 outline-none resize-y focus:ring-2",
                                    sampleErr
                                        ? "border-rose-300 focus:border-rose-400 focus:ring-rose-100"
                                        : "border-slate-200 focus:border-sky-400 focus:ring-sky-100",
                                )}
                            />
                            {sampleErr ? (
                                <p className="mt-1 text-[10.5px] text-rose-600">{sampleErr}</p>
                            ) : (
                                <p className="mt-1 text-[10.5px] text-slate-400">
                                    Conditions branch on these fields, so editing them changes which actions run.
                                </p>
                            )}
                        </div>
                        {steps.length > 0 && (
                            <div>
                                <div className="mb-1 text-[11px] font-medium text-slate-600">
                                    Steps to run <span className="text-slate-400">({steps.length - disabled.size}/{steps.length})</span>
                                </div>
                                <div className="space-y-1">
                                    {steps.map((s) => {
                                        const on = !disabled.has(s.id);
                                        return (
                                            <button
                                                key={s.id}
                                                type="button"
                                                onClick={() => toggleStep(s.id)}
                                                className="w-full flex items-center gap-2 rounded-md border border-slate-200 px-2 py-1.5 text-left hover:border-slate-300"
                                            >
                                                <span
                                                    className={cn(
                                                        "flex h-4 w-4 shrink-0 items-center justify-center rounded",
                                                        on ? "bg-sky-600 text-white" : "border border-slate-300",
                                                    )}
                                                >
                                                    {on && <CheckIcon className="w-3 h-3" />}
                                                </span>
                                                <span className={cn("text-[12px]", on ? "text-slate-700" : "text-slate-400 line-through")}>
                                                    {s.label}
                                                </span>
                                            </button>
                                        );
                                    })}
                                </div>
                                <p className="mt-1 text-[10.5px] text-slate-400">
                                    Turn a step off to skip it in this test. Conditions still decide which steps are reached.
                                </p>
                            </div>
                        )}
                        <button
                            type="button"
                            onClick={runWithSample}
                            disabled={testing}
                            className="h-8 w-full rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center justify-center gap-1.5 disabled:opacity-60"
                        >
                            {testing ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <PlayIcon className="w-3.5 h-3.5" />}
                            {testing ? "Running…" : "Run test"}
                        </button>
                        <div className="border-t border-slate-200 pt-3 space-y-2">
                            {testing ? (
                                <div className="flex items-center gap-2 text-[12px] text-slate-400">
                                    <Loader2Icon className="w-4 h-4 animate-spin" /> Running…
                                </div>
                            ) : testResult ? (
                                testResult.trace.length === 0 ? (
                                    <p className="text-[12px] text-slate-500">No actions ran for this sample (check your conditions).</p>
                                ) : (
                                    testResult.trace.map((r, i) => <NodeResultRow key={`${r.node_id}-${i}`} r={r} />)
                                )
                            ) : (
                                <p className="text-[12px] text-slate-500">Edit the sample event, then Run test.</p>
                            )}
                        </div>
                    </div>
                ) : runs.isLoading ? (
                    <div className="flex items-center gap-2 text-[12px] text-slate-400">
                        <Loader2Icon className="w-4 h-4 animate-spin" /> Loading…
                    </div>
                ) : (runs.data?.runs.length ?? 0) === 0 ? (
                    <p className="text-[12px] text-slate-500">No runs yet. This automation hasn't fired.</p>
                ) : (
                    runs.data!.runs.map((run: AutomationRun) => (
                        <div key={run.id} className="rounded-md border border-slate-200 p-2 space-y-1">
                            <div className="flex items-center gap-1.5 min-w-0">
                                {run.status === "error" ? (
                                    <XCircleIcon className="w-3.5 h-3.5 shrink-0 text-rose-500" />
                                ) : (
                                    <CheckCircle2Icon className="w-3.5 h-3.5 shrink-0 text-emerald-500" />
                                )}
                                <span className="min-w-0 truncate text-[11.5px] font-medium text-slate-700 capitalize">{run.status}</span>
                                <span className="ml-auto shrink-0 whitespace-nowrap tabular-nums text-[10.5px] text-slate-400">{new Date(run.started_at).toLocaleString()}</span>
                            </div>
                            {run.node_results?.filter((r) => r.type === "action").map((r, i) => (
                                <div key={`${run.id}-${i}`} className="pl-1">
                                    <div className="flex items-center gap-1.5">
                                        {r.status === "error" ? (
                                            <XCircleIcon className="w-3 h-3 text-rose-400" />
                                        ) : (
                                            <CheckCircle2Icon className="w-3 h-3 text-emerald-400" />
                                        )}
                                        <span className="text-[11px] text-slate-500">{actionLabel(r.action ?? "")}</span>
                                        {r.error && <span className="text-[10.5px] text-rose-500 truncate">· {r.error}</span>}
                                    </div>
                                    {r.preview && Object.keys(r.preview).length > 0 && (
                                        <div className="mt-0.5 pl-4 space-y-0.5">
                                            {Object.entries(r.preview).map(([k, v]) => (
                                                <div key={k} className="text-[10px] text-slate-400 truncate">
                                                    <span className="text-slate-300">{k}:</span>{" "}
                                                    <span className="font-mono">{String(v)}</span>
                                                </div>
                                            ))}
                                        </div>
                                    )}
                                </div>
                            ))}
                        </div>
                    ))
                )}
            </motion.div>
        </div>
    );
}

// InboundUrlField shows the automation's unique inbound-webhook URL (read-only)
// with a copy button. The stored value is a path; we prefix the API origin so
// the copied value is the full URL an external system POSTs to.
function InboundUrlField({ inboundUrl }: { inboundUrl?: string }) {
    const [copied, setCopied] = React.useState(false);
    if (!inboundUrl) {
        return (
            <p className="text-[11.5px] text-slate-400 leading-relaxed">
                Save this automation to generate its unique webhook URL. An external system POSTs JSON to that URL and
                the body becomes the event payload (reference any field with <code>{"{{.field}}"}</code>).
            </p>
        );
    }
    const full = `${API_URL}${inboundUrl}`;
    const copy = async () => {
        try {
            await navigator.clipboard.writeText(full);
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
        } catch {
            /* clipboard blocked — the value is still selectable in the field */
        }
    };
    return (
        <div>
            <Label>Your webhook URL</Label>
            <div className="flex items-center gap-1.5">
                <input
                    readOnly
                    value={full}
                    onFocus={(e) => e.currentTarget.select()}
                    className="flex-1 h-7 rounded-md border border-slate-200 bg-slate-50 px-2 text-[11.5px] font-mono text-slate-700 focus:border-sky-400 focus:ring-2 focus:ring-sky-100 focus:outline-none"
                />
                <button
                    type="button"
                    onClick={copy}
                    className="h-7 px-2 rounded-md border border-slate-200 inline-flex items-center gap-1 text-[11.5px] text-slate-600 hover:bg-slate-50 shrink-0"
                >
                    {copied ? <CheckIcon className="w-3.5 h-3.5 text-emerald-600" /> : <CopyIcon className="w-3.5 h-3.5" />}
                    {copied ? "Copied" : "Copy"}
                </button>
            </div>
            <p className="text-[11.5px] text-slate-400 mt-1.5 leading-relaxed">
                POST JSON here to run this automation. The body becomes the event payload, so reference its keys with{" "}
                <code>{"{{.field}}"}</code> in actions and conditions.
            </p>
        </div>
    );
}

// ── Editor panel ─────────────────────────────────────────────────────────────
function NodeEditor({
    node,
    onClose,
    selfId,
    trigger,
    onTrigger,
    inboundUrl,
    onCondition,
    targets,
    connLabel,
    actionsForProvider,
    providerOf,
    onAction,
}: {
    node: Node;
    onClose: () => void;
    selfId: string;
    trigger: string;
    onTrigger: (ev: string) => void;
    inboundUrl?: string;
    onCondition: (c: AutomationCondition) => void;
    targets: IntegrationConnection[];
    connLabel: (id?: string) => string;
    actionsForProvider: (provider?: string) => string[];
    providerOf: (id?: string) => string;
    onAction: (patch: Record<string, unknown>) => void;
}) {
    const isTrigger = node.type === "trigger";
    const isCondition = node.type === "condition";

    const triggerOptions: SelectOption[] = TRIGGER_EVENTS.map((ev) => ({ value: ev, label: triggerLabel(ev) }));

    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ duration: 0.15 }}
            className="flex h-full min-h-0 flex-col"
        >
            <div className="h-11 px-3 flex items-center border-b border-slate-200 shrink-0">
                <span className="text-[12.5px] font-medium text-slate-900">
                    {isTrigger ? "Trigger" : isCondition ? "Condition" : "Action"}
                </span>
                <button
                    type="button"
                    onClick={onClose}
                    className="ml-auto h-7 w-7 rounded-md inline-flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-100"
                    aria-label="Close"
                >
                    <XIcon className="w-4 h-4" />
                </button>
            </div>

            <div className="flex-1 overflow-auto p-3 space-y-3">
                {isTrigger ? (
                    <>
                        <div>
                            <Label>When this happens</Label>
                            <SelectMenu value={trigger} onChange={onTrigger} options={triggerOptions} className="w-full" />
                        </div>
                        {triggerIsInboundWebhook(trigger) ? (
                            <InboundUrlField inboundUrl={inboundUrl} />
                        ) : (
                            <p className="text-[11.5px] text-slate-400 leading-relaxed">
                                Add a condition (IF) below the trigger to branch on the event — e.g. only positive replies, or by source.
                            </p>
                        )}
                    </>
                ) : isCondition ? (
                    <ConditionEditor
                        trigger={trigger}
                        condition={(node.data as { condition: AutomationCondition }).condition}
                        onChange={onCondition}
                    />
                ) : (
                    <ActionEditor
                        trigger={trigger}
                        selfId={selfId}
                        data={node.data as { action?: string; connection_id?: string; config?: Record<string, unknown> }}
                        targets={targets}
                        connLabel={connLabel}
                        actionsForProvider={actionsForProvider}
                        providerOf={providerOf}
                        onAction={onAction}
                    />
                )}
            </div>
        </motion.div>
    );
}

function ConditionEditor({
    trigger,
    condition,
    onChange,
}: {
    trigger: string;
    condition: AutomationCondition;
    onChange: (c: AutomationCondition) => void;
}) {
    const fieldOptions: SelectOption[] = triggerConditionFields(trigger).map((f) => ({ value: f.key, label: f.label }));
    const selectedKey = conditionFieldKey(condition);
    const def: TriggerFieldDef | undefined = triggerFieldDef(trigger, selectedKey);
    const set = (patch: Partial<AutomationCondition>) => onChange({ ...condition, ...patch });
    const pickField = (key: string) => onChange(conditionFromFieldKey(trigger, key));

    const isRandom = condition.field === "random";
    const isExpression = condition.field === "expression";
    const op = condition.operator;
    const needsValue = !isRandom && op !== "exists" && op !== "is_true";
    const isConfidence = selectedKey === "confidence";
    const vars = triggerVariables(trigger);

    return (
        <div className="space-y-3">
            <div>
                <Label>If</Label>
                <SelectMenu value={selectedKey} onChange={pickField} options={fieldOptions} className="w-full" fullWidth />
            </div>

            {/* Operator — data fields only (random + expression don't use one). */}
            {!isRandom && !isExpression && def && (
                <div>
                    <Label>Condition</Label>
                    <SelectMenu
                        value={op}
                        onChange={(v) => set({ operator: v, value: v === "exists" || v === "is_true" ? undefined : condition.value })}
                        options={operatorsForType(def.type)}
                        className="w-full"
                        fullWidth
                    />
                </div>
            )}

            {/* Value editor, by field type + operator. */}
            {isExpression ? (
                <div className="space-y-2">
                    <div className="flex items-center justify-between gap-2">
                        <Label className="mb-0">Expression</Label>
                        <ExpressionReference />
                    </div>
                    <textarea
                        value={String(condition.expression ?? "")}
                        onChange={(e) => set({ expression: e.target.value })}
                        rows={3}
                        placeholder={`and (gtf .confidence 0.8) (eq .intent "positive")`}
                        className="w-full px-2.5 py-1.5 rounded-md border border-slate-200 bg-white font-mono text-[12px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 resize-y leading-relaxed"
                    />
                    {vars.length > 0 && (
                        <div className="flex flex-wrap gap-1">
                            {vars.map((v) => (
                                <button
                                    key={v}
                                    type="button"
                                    onClick={() => set({ expression: `${condition.expression ?? ""} .${v}`.replace(/^\s+/, "") })}
                                    className="px-1.5 py-0.5 rounded border border-slate-200 bg-slate-50 font-mono text-[10.5px] text-slate-600 hover:border-sky-300 hover:text-sky-700"
                                >
                                    .{v}
                                </button>
                            ))}
                        </div>
                    )}
                    <p className="text-[10.5px] text-slate-400 leading-relaxed">
                        Passes the “yes” branch when truthy. Reference fields as <code className="font-mono">.field</code>; use{" "}
                        <code className="font-mono">eq gt lt and or not</code>, numeric{" "}
                        <code className="font-mono">gtf ltf add sub mul div</code>, and{" "}
                        <code className="font-mono">contains lower</code>. Example:{" "}
                        <code className="font-mono">{`and (gtf .confidence 0.8) (eq .intent "positive")`}</code>.
                    </p>
                </div>
            ) : isRandom ? (
                <div>
                    <Label>Take the “yes” path</Label>
                    <NumberInput
                        value={Number(condition.value ?? 50)}
                        onChange={(v) => set({ value: Math.max(1, Math.min(99, v)) })}
                        min={1}
                        max={99}
                        step={5}
                        suffix="% of the time"
                        className="w-full"
                    />
                </div>
            ) : needsValue && def?.type === "enum" ? (
                <div>
                    <Label>Value</Label>
                    <SelectMenu
                        value={String(condition.value ?? "")}
                        onChange={(v) => set({ value: v })}
                        options={def.options ?? []}
                        className="w-full"
                        fullWidth
                    />
                </div>
            ) : needsValue && isConfidence ? (
                <div>
                    <Label>At least</Label>
                    <NumberInput
                        value={Math.round(Number(condition.value ?? 0) * 100)}
                        onChange={(v) => set({ value: Math.max(0, Math.min(100, v)) / 100 })}
                        min={0}
                        max={100}
                        step={5}
                        suffix="%"
                        className="w-full"
                    />
                </div>
            ) : needsValue && def?.type === "number" ? (
                <div>
                    <Label>Value</Label>
                    <NumberInput value={Number(condition.value ?? 0)} onChange={(v) => set({ value: v })} className="w-full" />
                </div>
            ) : needsValue ? (
                <div>
                    <Label>Value</Label>
                    <TextInput value={String(condition.value ?? "")} onChange={(v) => set({ value: v })} placeholder="value" className="w-full" />
                </div>
            ) : null}

            <p className="text-[11px] text-slate-400 leading-relaxed">
                Connect the right (yes) and bottom (no) dots of this block to the next steps.
            </p>
        </div>
    );
}

// One visual per action (native + provider): icon, text tint, and a soft bg for
// the editor header. Drives both the action dropdown glyphs and the editor
// header, so the picker reads like the campaign step picker.
const ACTION_VISUAL: Record<string, { Icon: typeof TagIcon; tint: string; bg: string; desc?: string }> = {
    "warmbly.add_tag": { Icon: TagIcon, tint: "text-emerald-600", bg: "bg-emerald-50", desc: "Add a tag to the contact." },
    "warmbly.remove_tag": { Icon: TagIcon, tint: "text-amber-600", bg: "bg-amber-50", desc: "Remove a tag from the contact." },
    "warmbly.create_task": { Icon: CheckSquareIcon, tint: "text-violet-600", bg: "bg-violet-50", desc: "Open a CRM task for the contact." },
    "warmbly.create_deal": { Icon: BriefcaseIcon, tint: "text-sky-600", bg: "bg-sky-50", desc: "Create a CRM deal for the contact." },
    "warmbly.move_deal_stage": { Icon: BriefcaseIcon, tint: "text-sky-600", bg: "bg-sky-50", desc: "Move the contact's open deal to another stage." },
    "warmbly.unsubscribe": { Icon: UserMinusIcon, tint: "text-rose-600", bg: "bg-rose-50", desc: "Unsubscribe the contact from the campaign." },
    "warmbly.run_automation": { Icon: ZapIcon, tint: "text-indigo-600", bg: "bg-indigo-50", desc: "Launch another automation with this event's data." },
    "warmbly.label_email": { Icon: TagsIcon, tint: "text-fuchsia-600", bg: "bg-fuchsia-50", desc: "Label the conversation the contact replied on." },
    "warmbly.set_variables": { Icon: WandSparklesIcon, tint: "text-amber-600", bg: "bg-amber-50", desc: "Compute named values from templates for later steps to reuse." },
    "warmbly.fire_event": { Icon: SendIcon, tint: "text-sky-600", bg: "bg-sky-50", desc: "Publish a custom event to the realtime gateway — your app receives it over the API websocket, no public URL." },
    "slack.notify": { Icon: MessageSquareIcon, tint: "text-violet-600", bg: "bg-violet-50" },
    "discord.notify": { Icon: MessageSquareIcon, tint: "text-indigo-600", bg: "bg-indigo-50" },
    "webhook.ping": { Icon: SendIcon, tint: "text-sky-600", bg: "bg-sky-50" },
    "hubspot.upsert_contact": { Icon: BriefcaseIcon, tint: "text-orange-600", bg: "bg-orange-50" },
    "pipedrive.upsert_person": { Icon: BriefcaseIcon, tint: "text-slate-700", bg: "bg-slate-100" },
    "salesforce.upsert_contact": { Icon: BriefcaseIcon, tint: "text-sky-600", bg: "bg-sky-50" },
    "close.upsert_lead": { Icon: BriefcaseIcon, tint: "text-emerald-600", bg: "bg-emerald-50" },
};

// actionGlyph returns the tinted leading icon for an action's dropdown option.
function actionGlyph(action: string): React.ReactNode {
    const v = ACTION_VISUAL[action];
    const Icon = v?.Icon ?? ZapIcon;
    return <Icon className={cn("w-3.5 h-3.5", v?.tint ?? "text-slate-400")} />;
}

// The menu that opens where you drop a dragged connection on empty canvas: pick
// what the next node is — an integration action, a condition (IF) router, or one
// of the built-in actions as a quick pick. Mirrors the campaign steps canvas.
function DragCreateMenu({
    x,
    y,
    onPick,
    onClose,
}: {
    x: number;
    y: number;
    onPick: (choice: string) => void;
    onClose: () => void;
}) {
    // Animate in/out like the app's other menus. The node is created the moment
    // a row is clicked; the menu plays its exit independently, and onClose (which
    // clears the parent state) only fires once that exit finishes.
    const [open, setOpen] = React.useState(true);
    const vw = typeof window !== "undefined" ? window.innerWidth : x + 240;
    const vh = typeof window !== "undefined" ? window.innerHeight : y + 360;
    const flipX = x > vw - 232;
    const flipY = y > vh - 360;
    const left = Math.max(8, Math.min(x, vw - 232));
    const top = Math.max(8, Math.min(y, vh - 360));
    const pick = (choice: string) => {
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
                        <CreateRow icon={<ZapIcon className="w-3.5 h-3.5 text-sky-600" />} label="Action" onClick={() => pick("action")} />
                        <CreateRow icon={<GitBranchIcon className="w-3.5 h-3.5 text-amber-600" />} label="Condition (branch)" onClick={() => pick("condition")} />
                        <div className="my-1 h-px bg-slate-100" />
                        <div className="px-2 pt-0.5 pb-0.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-slate-400">Built-in actions</div>
                        {NATIVE_ACTIONS.map((a) => (
                            <CreateRow key={a} icon={actionGlyph(a)} label={actionLabel(a)} onClick={() => pick(a)} />
                        ))}
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

function ActionEditor({
    trigger,
    selfId,
    data,
    targets,
    connLabel,
    actionsForProvider,
    providerOf,
    onAction,
}: {
    trigger: string;
    selfId: string;
    data: { action?: string; connection_id?: string; config?: Record<string, unknown> };
    targets: IntegrationConnection[];
    connLabel: (id?: string) => string;
    actionsForProvider: (provider?: string) => string[];
    providerOf: (id?: string) => string;
    onAction: (patch: Record<string, unknown>) => void;
}) {
    const config = data.config ?? {};
    const vars = triggerVariables(trigger);
    const setConfig = (k: string, v: unknown) => onAction({ config: { ...config, [k]: v } });
    const patchConfig = (p: Record<string, unknown>) => onAction({ config: { ...config, ...p } });
    const insertInto = (k: string, token: string) => setConfig(k, `${String(config[k] ?? "")}{{.${token}}}`);

    const isNative = isNativeAction(data.action ?? "");
    const selectedConn = isNative ? NATIVE_CONNECTION : (data.connection_id ?? "");
    const connOptions: SelectOption[] = [
        { value: NATIVE_CONNECTION, label: "Warmbly (built-in)", icon: <ZapIcon className="size-3.5 shrink-0 text-indigo-600" /> },
        ...targets.map((c) => ({
            value: c.id,
            label: connLabel(c.id),
            icon: <Link2Icon className="size-3.5 shrink-0 text-slate-400" />,
        })),
    ];
    const actionOptions: SelectOption[] = (isNative ? NATIVE_ACTIONS : actionsForProvider(providerOf(data.connection_id))).map(
        (a) => ({ value: a, label: actionLabel(a), icon: actionGlyph(a) }),
    );

    const pickConnection = (connId: string) => {
        if (connId === NATIVE_CONNECTION) {
            const first = NATIVE_ACTIONS[0];
            onAction({ connection_id: undefined, action: first, config: {}, sub: "Built-in action", provider: "", native: true, title: actionLabel(first) });
            return;
        }
        const acts = actionsForProvider(providerOf(connId));
        onAction({
            connection_id: connId,
            action: acts[0] ?? "",
            config: {},
            sub: connLabel(connId),
            provider: providerOf(connId),
            native: false,
            title: acts[0] ? actionLabel(acts[0]) : "Choose an action",
        });
    };
    const pickAction = (action: string) =>
        onAction({
            action,
            config: {},
            title: actionLabel(action),
            native: isNativeAction(action),
            ...(isNativeAction(action) ? { connection_id: undefined } : {}),
        });

    return (
        <div className="space-y-3">
            <div>
                <Label>Run</Label>
                <SelectMenu value={selectedConn} onChange={pickConnection} options={connOptions} className="w-full" fullWidth />
            </div>
            <div>
                <Label>Action</Label>
                <SelectMenu value={data.action ?? ""} onChange={pickAction} options={actionOptions} className="w-full" fullWidth />
            </div>

            {/* Config fields swap when the action/target changes; the keyed
                fade keeps that swap from feeling like a hard cut. */}
            <motion.div
                key={`${selectedConn}:${data.action ?? ""}`}
                initial={{ opacity: 0, y: 4 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.15 }}
                className="space-y-3"
            >
            {isNative ? (
                <NativeActionConfig action={data.action ?? ""} trigger={trigger} config={config} patchConfig={patchConfig} selfId={selfId} />
            ) : (
                <>
                    {actionNeedsChannel(data.action ?? "") && (
                        <div>
                            <Label>Channel</Label>
                            <TextInput value={String(config.channel ?? "")} onChange={(v) => setConfig("channel", v)} placeholder="#sales" className="w-full" />
                        </div>
                    )}
                    {actionNeedsURL(data.action ?? "") && (
                        <div>
                            <Label>Webhook URL</Label>
                            <TextInput value={String(config.url ?? "")} onChange={(v) => setConfig("url", v)} placeholder="https://hooks.zapier.com/…" className="w-full" />
                            <VarChips vars={vars} onPick={(t) => insertInto("url", t)} />
                        </div>
                    )}
                    {actionSupportsTemplate(data.action ?? "") && (
                        <div>
                            <Label>Message (optional)</Label>
                            <TextInput
                                value={String(config.message_template ?? "")}
                                onChange={(v) => setConfig("message_template", v)}
                                placeholder="New reply from {{.contact_email}}"
                                className="w-full"
                            />
                            <VarChips vars={vars} onPick={(t) => insertInto("message_template", t)} />
                        </div>
                    )}
                    <p className="text-[11px] text-slate-400 leading-relaxed">
                        Values are full Go templates: <span className="font-mono text-slate-500">{"{{.variable}}"}</span> fields plus{" "}
                        <span className="font-mono text-slate-500">{"{{if}}"}</span>, helpers, and pipelines, rendered against the trigger data when the automation runs.
                    </p>
                </>
            )}
            </motion.div>
        </div>
    );
}

const NATIVE_PRIORITIES = ["low", "medium", "high", "urgent"] as const;
function PrioritySegment({ value, onChange }: { value: string; onChange: (p: string) => void }) {
    const current = value || "medium";
    return (
        <div className="inline-flex rounded-md border border-slate-200 bg-white p-0.5">
            {NATIVE_PRIORITIES.map((p) => (
                <button
                    key={p}
                    type="button"
                    onClick={() => onChange(p)}
                    className={cn(
                        "h-7 px-2.5 rounded text-[11px] font-medium capitalize transition-colors",
                        current === p ? "bg-sky-600 text-white shadow-sm" : "text-slate-500 hover:bg-slate-50 hover:text-slate-700",
                    )}
                >
                    {p}
                </button>
            ))}
        </div>
    );
}

const NATIVE_CURRENCIES: SelectOption[] = ["USD", "EUR", "GBP", "CAD", "AUD"].map((c) => ({ value: c, label: c }));

// NativeActionConfig renders the right editor for a built-in (Warmbly) action.
function NativeActionConfig({
    action,
    trigger,
    config,
    patchConfig,
    selfId,
}: {
    action: string;
    trigger: string;
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
    selfId: string;
}) {
    const need = nativeActionNeeds(action);
    return (
        <div className="space-y-3">
            {need === "tag" && (
                <div>
                    <Label>{action === "warmbly.add_tag" ? "Tag to add" : "Tag to remove"}</Label>
                    <CategoryPicker
                        value={config.category_id ? [String(config.category_id)] : []}
                        onChange={(ids) => patchConfig({ category_id: ids.length ? ids[ids.length - 1] : "" })}
                        placeholder="Pick a tag…"
                    />
                </div>
            )}

            {need === "label" && (
                <div className="space-y-2">
                    <div>
                        <Label>Labels to apply</Label>
                        <CategoryPicker
                            value={Array.isArray(config.label_ids) ? (config.label_ids as string[]) : []}
                            onChange={(ids) => patchConfig({ label_ids: ids })}
                            placeholder="Pick one or more labels…"
                        />
                    </div>
                    {triggerCarriesThread(trigger) ? (
                        <p className="text-[11px] leading-relaxed text-slate-400">
                            Labels the conversation the contact replied on (the same labels you set by hand in the unibox).
                        </p>
                    ) : (
                        <p className="inline-flex items-start gap-1.5 rounded-md border border-amber-200 bg-amber-50 px-2 py-1.5 text-[11px] leading-relaxed text-amber-700">
                            <TriangleAlertIcon className="mt-px w-3.5 h-3.5 shrink-0" /> This labels the email a contact
                            replied on, so it only runs on a &quot;Reply received&quot; automation. This trigger has no
                            inbox thread to label.
                        </p>
                    )}
                </div>
            )}

            {need === "deal" && (
                <>
                    <div>
                        <Label>{action === "warmbly.create_deal" ? "Create the deal in" : "Move the deal to"}</Label>
                        <DealStagePicker
                            pipelineId={config.deal_pipeline_id ? String(config.deal_pipeline_id) : undefined}
                            stageId={config.deal_stage_id ? String(config.deal_stage_id) : undefined}
                            onChange={({ pipelineId, stageId }) => patchConfig({ deal_pipeline_id: pipelineId, deal_stage_id: stageId })}
                        />
                    </div>
                    {action === "warmbly.create_deal" && (
                        <>
                            <div>
                                <Label>Deal name</Label>
                                <TextInput
                                    value={String(config.deal_name ?? "")}
                                    onChange={(v) => patchConfig({ deal_name: v })}
                                    placeholder="{{.company}} ({{.contact_email}})"
                                    className="w-full"
                                />
                                <p className="mt-1.5 text-[11px] text-slate-400">
                                    Full Go template: {"{{.variable}}"} fields plus {"{{if}}"}, helpers, and pipelines.
                                </p>
                            </div>
                            <div className="flex items-end gap-3">
                                <div className="flex-1">
                                    <Label>Value (optional)</Label>
                                    <NumberInput
                                        value={Number(config.deal_value ?? 0)}
                                        onChange={(n) => patchConfig({ deal_value: n > 0 ? n : undefined })}
                                        min={0}
                                        max={1_000_000_000}
                                        className="w-full"
                                    />
                                </div>
                                <div className="w-32">
                                    <Label>Currency</Label>
                                    <SelectMenu
                                        value={String(config.deal_currency ?? "USD")}
                                        onChange={(c) => patchConfig({ deal_currency: c })}
                                        options={NATIVE_CURRENCIES}
                                        className="w-full"
                                        fullWidth
                                    />
                                </div>
                            </div>
                        </>
                    )}
                    {action === "warmbly.move_deal_stage" && (
                        <p className="text-[11px] text-slate-400 leading-relaxed">
                            Moves the contact's most recent open deal in this pipeline. If they have no open deal here, nothing happens.
                        </p>
                    )}
                </>
            )}

            {need === "task" && (
                <>
                    <div>
                        <Label>Task title</Label>
                        <TextInput
                            value={String(config.task_title ?? "")}
                            onChange={(v) => patchConfig({ task_title: v })}
                            placeholder="Follow up with {{.contact_email}}"
                            className="w-full"
                        />
                        <p className="mt-1.5 text-[11px] text-slate-400">
                            Full Go template: {"{{.variable}}"} fields plus {"{{if}}"}, helpers, and pipelines.
                        </p>
                    </div>
                    <div>
                        <Label>Task type</Label>
                        <TaskTypePicker
                            value={String(config.task_type ?? "")}
                            onChange={(name) => patchConfig({ task_type: name })}
                            className="w-full"
                        />
                    </div>
                    <div className="flex flex-wrap items-end gap-4">
                        <div>
                            <Label>Priority</Label>
                            <PrioritySegment value={String(config.task_priority ?? "")} onChange={(p) => patchConfig({ task_priority: p })} />
                        </div>
                        <div>
                            <Label>Due in (days)</Label>
                            <NumberInput
                                value={Number(config.task_due_offset_days ?? 1)}
                                onChange={(n) => patchConfig({ task_due_offset_days: n })}
                                min={0}
                                max={365}
                                className="w-28"
                            />
                        </div>
                    </div>
                    <div>
                        <Label>Assign to</Label>
                        <AssigneeTeamPicker
                            className="w-full"
                            value={{
                                userId: config.task_assigned_to ? String(config.task_assigned_to) : null,
                                teamId: config.task_assigned_team_id ? String(config.task_assigned_team_id) : null,
                            }}
                            onChange={(v: AssigneeValue) => patchConfig({ task_assigned_to: v.userId ?? null, task_assigned_team_id: v.teamId ?? null })}
                        />
                        <p className="mt-1.5 text-[11px] text-slate-400">
                            Assign to a teammate or a whole team. Unassigned falls back to the workspace owner.
                        </p>
                    </div>
                </>
            )}

            {need === "automation" && <RunAnotherAutomationFields config={config} patchConfig={patchConfig} selfId={selfId} />}

            {need === "event" && <FireEventFields config={config} patchConfig={patchConfig} />}

            {need === "vars" && <SetVariablesFields config={config} patchConfig={patchConfig} />}

            {need === "none" && (
                <p className="text-[11px] text-slate-400 leading-relaxed">
                    Works when the event carries a campaign (reply / bounce / unsubscribe triggers).
                </p>
            )}
        </div>
    );
}

type SetVarRow = { key: string; value: string };

// SetVariablesFields edits a list of named template values written back into the
// event data for later steps to reuse (the safe "transform" node).
function SetVariablesFields({
    config,
    patchConfig,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    const rows: SetVarRow[] = Array.isArray(config.set_vars)
        ? (config.set_vars as SetVarRow[]).map((v) => ({ key: String(v?.key ?? ""), value: String(v?.value ?? "") }))
        : [];
    const display = rows.length ? rows : [{ key: "", value: "" }];

    // Keep blank rows while editing (filtering here would delete a just-added row
    // before it can be typed in). Save-time validation requires one named var, and
    // the backend ignores any row whose key is empty.
    const update = (next: SetVarRow[]) => patchConfig({ set_vars: next });
    const setRow = (i: number, patch: Partial<SetVarRow>) => update(display.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
    const addRow = () => update([...display, { key: "", value: "" }]);
    const removeRow = (i: number) => update(display.filter((_, idx) => idx !== i));

    return (
        <div className="space-y-2">
            {display.map((row, i) => (
                <div key={i} className="flex items-center gap-2">
                    <TextInput
                        value={row.key}
                        onChange={(v) => setRow(i, { key: v })}
                        placeholder="name"
                        className="w-28 shrink-0 font-mono"
                    />
                    <span className="text-[12.5px] text-slate-400">=</span>
                    <TextInput
                        value={row.value}
                        onChange={(v) => setRow(i, { value: v })}
                        placeholder="{{.first_name}} at {{.company}}"
                        className="flex-1 min-w-0 font-mono"
                    />
                    <button
                        type="button"
                        onClick={() => removeRow(i)}
                        className="shrink-0 text-slate-400 hover:text-rose-500"
                        aria-label="Remove variable"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>
            ))}
            <button
                type="button"
                onClick={addRow}
                className="inline-flex items-center gap-1 text-[12px] text-sky-600 hover:text-sky-700"
            >
                <PlusIcon className="w-3.5 h-3.5" /> Add variable
            </button>
            <p className="text-[11px] text-slate-400 leading-relaxed">
                Each value is a Go template. Later steps reference it as <code>{`{{.name}}`}</code>.
            </p>
        </div>
    );
}

// FireEventFields configures a custom "fire event": an event name + a list of
// templated key/value fields that become the event payload. The event is
// published to the realtime gateway, so a developer's app receives it over the
// API websocket (API key + REALTIME_SUBSCRIBE) without hosting a webhook URL.
function FireEventFields({
    config,
    patchConfig,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    const rows: SetVarRow[] = Array.isArray(config.event_fields)
        ? (config.event_fields as SetVarRow[]).map((v) => ({ key: String(v?.key ?? ""), value: String(v?.value ?? "") }))
        : [];
    const display = rows.length ? rows : [{ key: "", value: "" }];
    const update = (next: SetVarRow[]) => patchConfig({ event_fields: next });
    const setRow = (i: number, patch: Partial<SetVarRow>) => update(display.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
    const addRow = () => update([...display, { key: "", value: "" }]);
    const removeRow = (i: number) => update(display.filter((_, idx) => idx !== i));

    return (
        <div className="space-y-3">
            <div>
                <Label>Event name</Label>
                <TextInput
                    value={String(config.event_name ?? "")}
                    onChange={(v) => patchConfig({ event_name: v })}
                    placeholder="lead.replied"
                    className="w-full font-mono"
                />
                <p className="mt-1 text-[11px] text-slate-400">What your app subscribes to. Lowercase dotted names work well.</p>
            </div>
            <div>
                <Label>Payload</Label>
                <div className="space-y-2">
                    {display.map((row, i) => (
                        <div key={i} className="flex items-center gap-2">
                            <TextInput
                                value={row.key}
                                onChange={(v) => setRow(i, { key: v })}
                                placeholder="field"
                                className="w-28 shrink-0 font-mono"
                            />
                            <span className="text-[12.5px] text-slate-400">=</span>
                            <TextInput
                                value={row.value}
                                onChange={(v) => setRow(i, { value: v })}
                                placeholder="{{.contact_email}}"
                                className="flex-1 min-w-0 font-mono"
                            />
                            <button
                                type="button"
                                onClick={() => removeRow(i)}
                                className="shrink-0 text-slate-400 hover:text-rose-500"
                                aria-label="Remove field"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                    ))}
                </div>
                <button
                    type="button"
                    onClick={addRow}
                    className="mt-2 inline-flex items-center gap-1 text-[12px] text-sky-600 hover:text-sky-700"
                >
                    <PlusIcon className="w-3.5 h-3.5" /> Add field
                </button>
                <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                    Each value is a Go template against the event data. Your app receives <code>{`{ name, payload }`}</code> over the websocket.
                </p>
            </div>
        </div>
    );
}

// RunAnotherAutomationFields picks the automation to launch. It excludes self and
// flags a disabled / non-campaign-trigger target. Recursion + compute are bounded
// server-side by the chain-depth guard, so this stays safe even if chains nest.
function RunAnotherAutomationFields({
    config,
    patchConfig,
    selfId,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
    selfId: string;
}) {
    const { data } = useAutomations();
    const all = (data?.automations ?? []).filter((a) => a.id !== selfId);
    const options: SelectOption[] = all.map((a) => ({
        value: a.id,
        label: (a.name || "Untitled automation") + (a.enabled ? "" : " · disabled"),
    }));
    const selected = all.find((a) => a.id === String(config.automation_id ?? ""));
    return (
        <div className="space-y-2">
            <div>
                <Label>Automation to run</Label>
                <SelectMenu
                    value={String(config.automation_id ?? "")}
                    onChange={(id) => patchConfig({ automation_id: id })}
                    options={options}
                    placeholder={options.length ? "Choose an automation…" : "No other automations yet"}
                    className="w-full"
                    fullWidth
                />
            </div>
            {selected && !selected.enabled && (
                <p className="inline-flex items-start gap-1.5 rounded-md border border-amber-200 bg-amber-50 px-2 py-1.5 text-[11px] leading-relaxed text-amber-700">
                    <TriangleAlertIcon className="mt-px w-3.5 h-3.5 shrink-0" /> This automation is disabled, so nothing runs until you enable it.
                </p>
            )}
            {selected && selected.enabled && selected.trigger_event !== "campaign.action" && (
                <p className="rounded-md border border-sky-200 bg-sky-50 px-2 py-1.5 text-[11px] leading-relaxed text-sky-700">
                    Built for the &quot;{triggerLabel(selected.trigger_event)}&quot; trigger. It still runs here, but only the variables present in this event are passed through.
                </p>
            )}
            <p className="text-[11px] leading-relaxed text-slate-400">
                The launched automation receives this event&apos;s data. Chains are depth-limited, so automations can&apos;t loop forever.
            </p>
        </div>
    );
}

// VarChips — clickable {{.variable}} fields that insert into a templatable field.
function VarChips({ vars, onPick }: { vars: string[]; onPick: (v: string) => void }) {
    if (!vars.length) return null;
    return (
        <div className="mt-1.5 flex flex-wrap gap-1">
            {vars.map((v) => (
                <button
                    key={v}
                    type="button"
                    onClick={() => onPick(v)}
                    title={`Insert {{.${v}}}`}
                    className="h-5 rounded border border-slate-200 bg-slate-50 px-1.5 font-mono text-[10.5px] text-slate-500 transition-colors hover:border-sky-300 hover:bg-sky-50 hover:text-sky-700"
                >
                    {`{{.${v}}}`}
                </button>
            ))}
        </div>
    );
}
