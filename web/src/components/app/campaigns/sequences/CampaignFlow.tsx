// Visual flow canvas for a campaign's steps (React Flow) — an explicit branching
// tree. Nothing connects automatically: a contact only moves where you've drawn
// a connection.
//
// HOW IT WORKS
// - Each step is a box, identified by its name. The entry step is marked "Start".
// - Draw a connection from a step's bottom dot to another step (or empty canvas
//   to make a new one). A connection with NO condition just means "go there
//   after the wait" — you're never forced to pick a condition.
// - Add conditions to branch: click a connection and choose "if opened / clicked
//   / replied / didn't… within N days" (or a random split). Draw several
//   connections from one step for multiple branches; the first match wins.
// - Timing lives on the connections (wait N days before the step it points to).
// - Anything with no matching connection just ends — every path ends in STOP,
//   automatically. A step with no outgoing connection shows "Ends here".

import React from "react";
import {
    ChevronDownIcon,
    ChevronUpIcon,
    ClockIcon,
    FlagIcon,
    Loader2Icon,
    MailIcon,
    PlusIcon,
    Trash2Icon,
    XIcon,
} from "lucide-react";
import {
    ReactFlow,
    Background,
    Controls,
    Panel,
    Handle,
    Position,
    useNodesState,
    useEdgesState,
    type Node,
    type Edge,
    type Connection,
    type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import dagre from "@dagrejs/dagre";
import { useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import type { SequenceBranch, BranchCondition, BranchField } from "@/lib/api/models/app/campaigns/sequences/Branching";
import { BRANCH_FIELD_LABELS } from "@/lib/api/models/app/campaigns/sequences/Branching";
import useSequences from "@/lib/api/hooks/app/campaigns/sequences/useSequences";
import useCreateSequence from "@/lib/api/hooks/app/campaigns/sequences/useCreateSequence";
import useDeleteSequence from "@/lib/api/hooks/app/campaigns/sequences/useDeleteSequence";
import updateSequence from "@/lib/api/client/app/campaigns/sequences/updateSequence";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import useUpdateCampaign from "@/lib/api/hooks/app/campaigns/useUpdateCampaign";
import { useConfirm } from "@/hooks/context/confirm";
import { NumberInput } from "@/components/ui/field";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import SequenceView from "./SequenceView";

const STOP_ID = "__stop__";
const NODE_W = 248;
const NODE_H = 92;
const MAX_STEPS = 50;
const SEQ_KEY = (id: string) => ["campaigns", id, "sequences"] as const;

function newBranchId(): string {
    try {
        return crypto.randomUUID();
    } catch {
        return `b_${Math.floor(performance.now())}_${Math.random().toString(36).slice(2, 8)}`;
    }
}

const isCond = (b: SequenceBranch) => (b.conditions?.length ?? 0) > 0;
const stepName = (s: Sequence | undefined) => (s?.name?.trim() ? s.name : "Untitled step");

// Conditions are first-match; unconditional ("just go there") connections are the
// fallback, so keep them after the conditional ones.
function ordered(branches: SequenceBranch[]): SequenceBranch[] {
    return [...branches.filter(isCond), ...branches.filter((b) => !isCond(b))];
}

function layoutGraph(nodes: Node[], edges: Edge[]): Node[] {
    const g = new dagre.graphlib.Graph();
    g.setDefaultEdgeLabel(() => ({}));
    // Generous spacing so step cards fan out as a clear tree and the little
    // label cards on the connections never sit on top of each other.
    g.setGraph({ rankdir: "TB", nodesep: 90, ranksep: 130, marginx: 20, marginy: 20, edgesep: 40 });
    nodes.forEach((n) => {
        const stop = n.id === STOP_ID;
        g.setNode(n.id, { width: stop ? 96 : NODE_W, height: stop ? 40 : NODE_H });
    });
    edges.forEach((e) => {
        const text = typeof e.label === "string" ? e.label : "";
        // Give dagre the label card's footprint so it reserves room for it and
        // never lets two condition cards land on top of each other.
        g.setEdge(
            e.source,
            e.target,
            text ? { width: Math.min(260, text.length * 6 + 24), height: 28, labelpos: "c" } : {},
        );
    });
    dagre.layout(g);
    return nodes.map((n) => {
        const p = g.node(n.id);
        return p ? { ...n, position: { x: p.x - p.width / 2, y: p.y - p.height / 2 } } : n;
    });
}

function conditionText(b: SequenceBranch): string {
    return (b.conditions ?? [])
        .map((c) => {
            if (c.field === "random") return `${c.value ?? 50}% random`;
            const f = BRANCH_FIELD_LABELS[c.field] ?? c.field;
            return `${f} within ${c.value ?? 3}d`;
        })
        .join(" + ");
}

// ── Custom nodes ────────────────────────────────────────────────────────────
type StepNodeData = {
    label: string;
    subtitle: string;
    isStart: boolean;
    endsHere: boolean;
    onDelete: () => void;
};

function StepNode({ data, selected }: NodeProps) {
    const d = data as StepNodeData;
    return (
        <div
            className={`w-[248px] rounded-md border bg-white shadow-sm transition-colors ${
                selected ? "border-sky-400 ring-2 ring-sky-100" : "border-slate-200"
            }`}
        >
            <Handle type="target" position={Position.Top} className="!h-2 !w-2 !bg-slate-300" />
            <div className="flex items-center gap-1.5 border-b border-slate-200/70 px-2.5 py-1.5">
                <MailIcon className="w-3 h-3 shrink-0 text-sky-600" />
                {d.isStart && (
                    <span className="rounded bg-sky-50 px-1.5 py-px text-[9.5px] font-semibold uppercase tracking-[0.12em] text-sky-700">
                        Start
                    </span>
                )}
                <span className="ml-auto" />
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        d.onDelete();
                    }}
                    title="Delete step"
                    className="nodrag inline-flex size-5 items-center justify-center rounded text-slate-300 transition-colors hover:bg-rose-50 hover:text-rose-600"
                >
                    <Trash2Icon className="w-3 h-3" />
                </button>
            </div>
            <div className="px-2.5 py-2">
                <div className="truncate text-[12.5px] font-medium text-slate-800">{d.label || "Untitled step"}</div>
                <div className="truncate text-[11px] text-slate-400">{d.subtitle || "No subject yet"}</div>
            </div>
            {d.endsHere && (
                <div className="flex items-center gap-1 border-t border-slate-200/70 px-2.5 py-1 text-[10.5px] text-slate-400">
                    <FlagIcon className="w-3 h-3 text-slate-400" />
                    Ends here
                </div>
            )}
            <Handle type="source" position={Position.Bottom} className="!h-2.5 !w-2.5 !bg-sky-500" />
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

const nodeTypes = { step: StepNode, stop: StopNode };

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
    const structureSig = React.useRef("");

    const seqById = React.useMemo(() => {
        const m = new Map<string, Sequence>();
        for (const s of sequences) m.set(s.id, s);
        return m;
    }, [sequences]);

    const invalidate = React.useCallback(
        () => qc.invalidateQueries({ queryKey: SEQ_KEY(campaignId) }),
        [qc, campaignId],
    );

    // All steps reachable downstream of a step — i.e. everything "inside" a branch.
    const reachableFrom = React.useCallback(
        (rootId: string) => {
            const set = new Set<string>([rootId]);
            const queue = [rootId];
            while (queue.length) {
                const id = queue.shift()!;
                for (const b of seqById.get(id)?.conditions?.branches ?? []) {
                    const t = b.target_sequence_id;
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

    // Only ever one editor open at a time.
    const openCondition = React.useCallback((sourceId: string, branchId: string) => {
        setSelectedEdge({ sourceId, branchId });
        setEditStepId(null);
    }, []);
    const openEditStep = React.useCallback((id: string) => {
        setEditStepId(id);
        setSelectedEdge(null);
    }, []);

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

    // Timing lives on connections: stored as the TARGET step's wait_after ("days
    // before this step"), edited from the arrow leading to it.
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

    // Reorder a conditional connection among its siblings — i.e. change the
    // if / else-if priority (which condition is checked first).
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

    // Add step drops a STANDALONE step (its own node) — nothing auto-connects.
    // Connect it by dragging from another step, or drag from this one to extend.
    // The first step is the entry (where new contacts start).
    const addStep = React.useCallback(async () => {
        if (adding || sequences.length >= MAX_STEPS) return;
        setAdding(true);
        try {
            await toast.promise(createSequence.mutateAsync(), {
                loading: "Adding step…",
                success: "Step added — drag a step's dot to connect it.",
                error: (err: AppError) => buildError(err),
            });
        } finally {
            setAdding(false);
        }
    }, [adding, sequences.length, createSequence]);

    // Drag from a step onto empty canvas -> new step connected (unconditionally)
    // from THAT step. No forced condition; add one later if you want a branch.
    const dragOut = React.useCallback(
        async (sourceId: string) => {
            if (adding || sequences.length >= MAX_STEPS) return;
            const src = seqById.get(sourceId);
            setAdding(true);
            try {
                const created = (await createSequence.mutateAsync()) as Sequence;
                await saveBranches(sourceId, [
                    ...(src?.conditions?.branches ?? []),
                    { branch_id: newBranchId(), target_sequence_id: created.id, conditions: [] },
                ]);
            } catch {
                toast.error("Couldn't add the step");
            } finally {
                setAdding(false);
            }
        },
        [adding, sequences.length, seqById, createSequence, saveBranches],
    );

    const deleteStep = React.useCallback(
        (id: string) => {
            const label = stepName(seqById.get(id));
            const referencing = sequences.filter(
                (s) => s.id !== id && (s.conditions?.branches ?? []).some((b) => b.target_sequence_id === id),
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
                                    branches: (s.conditions?.branches ?? []).filter((b) => b.target_sequence_id !== id),
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

    const deleteRef = React.useRef(deleteStep);
    React.useEffect(() => {
        deleteRef.current = deleteStep;
    }, [deleteStep]);

    React.useEffect(() => {
        const stepNodes: Node[] = sequences.map((s, i) => {
            const branches = s.conditions?.branches ?? [];
            return {
                id: s.id,
                type: "step",
                position: { x: 0, y: 0 },
                data: {
                    label: stepName(s),
                    subtitle: s.subject,
                    isStart: i === 0,
                    endsHere: branches.length === 0,
                    onDelete: () => deleteRef.current(s.id),
                } satisfies StepNodeData,
            };
        });

        const anyStop = sequences.some((s) => (s.conditions?.branches ?? []).some((b) => b.target_sequence_id === null));
        if (anyStop) stepNodes.push({ id: STOP_ID, type: "stop", position: { x: 0, y: 0 }, data: {} });

        const waitTag = (targetId: string | null) => {
            if (!targetId) return "";
            const w = seqById.get(targetId)?.wait_after ?? 0;
            return w > 0 ? `wait ${w}d` : "";
        };

        const flowEdges: Edge[] = [];
        sequences.forEach((s) => {
            const branches = ordered(s.conditions?.branches ?? []);
            for (const b of branches) {
                const cond = isCond(b);
                const wt = waitTag(b.target_sequence_id);
                // Each connection shows ONLY its own condition — no auto
                // "if" / "else if" / "else" labels imposed by position. An
                // unconditional connection just shows its wait (or nothing).
                const label: string | undefined = cond
                    ? wt
                        ? `${conditionText(b)} · ${wt}`
                        : conditionText(b)
                    : wt || undefined;
                flowEdges.push({
                    id: `br-${s.id}-${b.branch_id}`,
                    source: s.id,
                    target: b.target_sequence_id ?? STOP_ID,
                    label,
                    reconnectable: true,
                    style: cond ? { stroke: "#0ea5e9", strokeWidth: 2 } : { stroke: "#94a3b8" },
                    labelStyle: cond
                        ? { fill: "#0369a1", fontSize: 10.5, fontWeight: 600 }
                        : { fill: "#475569", fontSize: 10.5, fontWeight: 500 },
                    // A little card: filled, bordered, rounded, with room to breathe.
                    labelBgStyle: {
                        fill: "#ffffff",
                        stroke: cond ? "#bae6fd" : "#e2e8f0",
                        strokeWidth: 1,
                    },
                    labelBgPadding: [6, 4] as [number, number],
                    labelBgBorderRadius: 6,
                    data: { sourceId: s.id, branchId: b.branch_id },
                });
            }
        });

        const laid = layoutGraph(stepNodes, flowEdges);
        const sig =
            stepNodes.map((n) => n.id).sort().join(",") +
            "|" +
            flowEdges.map((e) => `${e.source}>${e.target}`).sort().join(",");
        const changed = sig !== structureSig.current;
        structureSig.current = sig;
        setNodes((cur) => {
            if (changed) return laid;
            const pos = new Map(cur.map((n) => [n.id, n.position]));
            return laid.map((n) => (pos.has(n.id) ? { ...n, position: pos.get(n.id)! } : n));
        });
        setEdges(flowEdges);
    }, [sequences, seqById, setNodes, setEdges]);

    // Highlight the subtree of the selected connection (its "if" branch) or the
    // selected step, dimming everything else so it's clear what's inside it.
    React.useEffect(() => {
        let root: string | null = null;
        if (selectedEdge) {
            const br = seqById
                .get(selectedEdge.sourceId)
                ?.conditions?.branches?.find((b) => b.branch_id === selectedEdge.branchId);
            root = br?.target_sequence_id ?? null;
        } else if (editStepId) {
            root = editStepId;
        }
        const hl = root ? reachableFrom(root) : null;
        setNodes((ns) =>
            ns.map((n) => ({
                ...n,
                style: { ...n.style, opacity: hl && n.id !== STOP_ID && !hl.has(n.id) ? 0.3 : 1 },
            })),
        );
        setEdges((es) =>
            es.map((e) => ({
                ...e,
                style: { ...e.style, opacity: hl && !(hl.has(e.source) && hl.has(e.target)) ? 0.15 : 1 },
            })),
        );
    }, [selectedEdge, editStepId, seqById, reachableFrom, setNodes, setEdges]);

    // Drag a node's dot onto another node (or Stop) -> new unconditional link.
    const onConnect = React.useCallback(
        (c: Connection) => {
            if (!c.source || !c.target || c.source === c.target) return;
            const src = seqById.get(c.source);
            if (!src) return;
            saveBranches(c.source, [
                ...(src.conditions?.branches ?? []),
                {
                    branch_id: newBranchId(),
                    target_sequence_id: c.target === STOP_ID ? null : c.target,
                    conditions: [],
                },
            ]);
        },
        [seqById, saveBranches],
    );

    const selected = React.useMemo(() => {
        if (!selectedEdge) return null;
        const src = seqById.get(selectedEdge.sourceId);
        const br = src?.conditions?.branches?.find((b) => b.branch_id === selectedEdge.branchId);
        return src && br ? { source: src, branch: br } : null;
    }, [selectedEdge, seqById]);

    const editStep = editStepId ? seqById.get(editStepId) : undefined;
    const editIndex = editStep ? sequences.findIndex((s) => s.id === editStep.id) : -1;
    const atMax = sequences.length >= MAX_STEPS;

    return (
        <div className="relative h-[78vh] w-full overflow-hidden rounded-md border border-slate-200 bg-slate-50/40">
            <ReactFlow
                nodes={nodes}
                edges={edges}
                onNodesChange={onNodesChange}
                onEdgesChange={onEdgesChange}
                onConnect={onConnect}
                onConnectEnd={(_, state) => {
                    if (state.fromNode && !state.toNode) dragOut(state.fromNode.id);
                }}
                onReconnect={(oldEdge, conn) => {
                    const d = oldEdge.data as { sourceId?: string; branchId?: string } | undefined;
                    if (!d?.sourceId || !d?.branchId || !conn.target) return;
                    const src = seqById.get(d.sourceId);
                    if (!src) return;
                    const newTarget = conn.target === STOP_ID ? null : conn.target;
                    saveBranches(
                        d.sourceId,
                        (src.conditions?.branches ?? []).map((b) =>
                            b.branch_id === d.branchId ? { ...b, target_sequence_id: newTarget } : b,
                        ),
                    );
                }}
                nodeTypes={nodeTypes}
                onEdgeClick={(_, edge) => {
                    const d = edge.data as { sourceId?: string; branchId?: string } | undefined;
                    if (d?.sourceId && d?.branchId) openCondition(d.sourceId, d.branchId);
                }}
                onNodeClick={(_, node) => {
                    if (node.id !== STOP_ID) openEditStep(node.id);
                }}
                fitView
                proOptions={{ hideAttribution: true }}
            >
                <Background color="#e2e8f0" gap={18} />
                <Controls showInteractive={false} />

                <Panel position="top-left">
                    <div className="flex items-center gap-1.5">
                        <button
                            type="button"
                            onClick={addStep}
                            disabled={adding || atMax}
                            className="inline-flex h-8 items-center gap-1.5 rounded-md bg-sky-600 px-3 text-[12px] font-medium text-white shadow-sm transition-colors hover:bg-sky-700 disabled:opacity-60"
                        >
                            {adding ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <PlusIcon className="w-3.5 h-3.5" />}
                            Add step
                        </button>
                        <button
                            type="button"
                            onClick={() => setNodes((ns) => layoutGraph(ns, edges))}
                            className="inline-flex h-8 items-center gap-1.5 rounded-md border border-slate-200 bg-white px-2.5 text-[12px] font-medium text-slate-700 shadow-sm transition-colors hover:border-slate-300 hover:text-slate-900"
                        >
                            Tidy up
                        </button>
                        <StopOnReplyToggle
                            on={!!campaign?.stop_on_reply}
                            onToggle={(next) => {
                                qc.setQueryData(["campaigns", campaignId], (old: unknown) =>
                                    old ? { ...(old as object), stop_on_reply: next } : old,
                                );
                                updateCampaign
                                    .mutateAsync({ stop_on_reply: next })
                                    .catch((err) => toast.error(buildError(err as AppError)));
                            }}
                        />
                    </div>
                </Panel>

                <Panel position="bottom-center">
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md bg-white/95 px-3 py-1.5 text-[11px] text-slate-500 shadow-sm">
                        <span className="inline-flex items-center gap-1">
                            <span className="inline-block h-0 w-4 border-t-2 border-sky-500" /> condition
                        </span>
                        <span className="inline-flex items-center gap-1">
                            <span className="inline-block h-0 w-4 border-t-2 border-slate-400" /> just go there
                        </span>
                        <span className="text-slate-400">drag a step's dot to connect · no match = stop</span>
                    </div>
                </Panel>
            </ReactFlow>

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
                    waitDays={seqById.get(selected.branch.target_sequence_id ?? "")?.wait_after ?? 0}
                    onClose={() => setSelectedEdge(null)}
                    onSetWait={(days) => {
                        if (selected.branch.target_sequence_id) saveWait(selected.branch.target_sequence_id, days);
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
                        saveBranches(
                            selected.source.id,
                            (selected.source.conditions?.branches ?? []).filter(
                                (b) => b.branch_id !== selected.branch.branch_id,
                            ),
                        );
                        setSelectedEdge(null);
                    }}
                />
            )}

            {editStep && (
                <div className="absolute inset-y-0 right-0 z-10 w-full max-w-[760px] overflow-y-auto overflow-x-hidden border-l border-slate-200 bg-white shadow-[0_0_40px_-12px_rgba(15,23,42,0.25)] xl:max-w-[880px]">
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
                        <SequenceView campaignId={campaignId} sequence={editStep} index={editIndex} />
                    </div>
                </div>
            )}
        </div>
    );
}

// ── Stop-on-reply toggle ────────────────────────────────────────────────────
function StopOnReplyToggle({ on, onToggle }: { on: boolean; onToggle: (next: boolean) => void }) {
    return (
        <div className="flex items-center gap-2 rounded-md border border-slate-200 bg-white px-2.5 py-1.5 shadow-sm">
            <span className="text-[11.5px] text-slate-600">Stop on reply</span>
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

// ── Connection editor (optional condition + wait behind an arrow) ───────────
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
    order: number; // index among the step's conditional branches, -1 if unconditional
    condCount: number;
    onMove: (dir: -1 | 1) => void;
    waitDays: number;
    onSetWait: (days: number) => void;
    onClose: () => void;
    onSave: (b: SequenceBranch) => void;
    onDelete: () => void;
}) {
    const c0 = branch.conditions?.[0];
    // "always" = no condition (just go there). Otherwise an engagement field.
    const [field, setField] = React.useState<string>(c0 ? c0.field : "always");
    const [value, setValue] = React.useState<number>(c0?.value ?? (c0?.field === "random" ? 50 : 3));

    const sel =
        "h-7 w-full rounded-md border border-slate-200 bg-white px-2 text-[12px] text-slate-800 focus:border-sky-400 focus:outline-none focus:ring-2 focus:ring-sky-100";
    const isAlways = field === "always";
    const isRandom = field === "random";
    const isNegative = field === "not_opened" || field === "not_clicked" || field === "not_replied";
    const target = steps.find((s) => s.id === branch.target_sequence_id);
    const targetLabel = branch.target_sequence_id === null ? "Stop the sequence" : target ? `“${stepName(target)}”` : "—";

    const buildConditions = (): BranchCondition[] => {
        if (isAlways) return [];
        if (isRandom) return [{ field: "random", operator: "chance", value }];
        return [{ field: field as BranchField, operator: "within_days", value }];
    };
    const save = (target_sequence_id: string | null) =>
        onSave({ branch_id: branch.branch_id, target_sequence_id, conditions: buildConditions() });

    return (
        <div className="absolute right-3 top-3 z-20 w-[300px] rounded-md border border-slate-200 bg-white p-3 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]">
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
                    <span>When several conditions match, this is checked {order + 1} of {condCount}</span>
                    <span className="flex items-center gap-0.5">
                        <button
                            type="button"
                            onClick={() => onMove(-1)}
                            disabled={order === 0}
                            title="Check this earlier"
                            className="inline-flex size-5 items-center justify-center rounded text-slate-400 hover:bg-white hover:text-slate-700 disabled:opacity-30"
                        >
                            <ChevronUpIcon className="w-3.5 h-3.5" />
                        </button>
                        <button
                            type="button"
                            onClick={() => onMove(1)}
                            disabled={order === condCount - 1}
                            title="Check this later"
                            className="inline-flex size-5 items-center justify-center rounded text-slate-400 hover:bg-white hover:text-slate-700 disabled:opacity-30"
                        >
                            <ChevronDownIcon className="w-3.5 h-3.5" />
                        </button>
                    </span>
                </div>
            )}

            <div className="space-y-2 text-[12px] text-slate-600">
                <div className="flex items-center gap-1.5">
                    <span>then go to</span>
                    <span className="font-medium text-slate-800">{targetLabel}</span>
                    {branch.target_sequence_id !== null && (
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
                    <select
                        className={sel}
                        value={field}
                        onChange={(e) => {
                            const f = e.target.value;
                            setField(f);
                            if (f === "random") setValue((v) => (v >= 1 && v <= 99 ? v : 50));
                            else if (f !== "always") setValue((v) => (v >= 1 && v <= 60 ? v : 3));
                        }}
                    >
                        <option value="always">always (right after the wait)</option>
                        <option value="opened">if opened the email</option>
                        <option value="clicked">if clicked a link</option>
                        <option value="replied">if replied</option>
                        <option value="not_opened">if didn’t open</option>
                        <option value="not_clicked">if didn’t click</option>
                        <option value="not_replied">if didn’t reply</option>
                        <option value="random">random split</option>
                    </select>
                </div>

                {isRandom && (
                    <div className="flex flex-wrap items-center gap-1.5">
                        <NumberInput value={value} onChange={(v) => setValue(Math.max(1, Math.min(99, Math.round(v) || 1)))} min={1} max={99} className="w-16" align="center" />
                        <span>% of contacts (chosen at random)</span>
                    </div>
                )}
                {!isAlways && !isRandom && (
                    <div className="flex flex-wrap items-center gap-1.5">
                        <span>within</span>
                        <NumberInput value={value} onChange={(v) => setValue(Math.max(1, Math.min(60, Math.round(v) || 1)))} min={1} max={60} className="w-16" align="center" />
                        <span>days</span>
                    </div>
                )}
                {isNegative && (
                    <p className="text-[10.5px] text-slate-400">
                        We keep checking until {value} day{value === 1 ? "" : "s"} pass, then take this path if it still hasn’t happened.
                    </p>
                )}

                {branch.target_sequence_id !== null && <WaitRow value={waitDays} onCommit={onSetWait} />}
                <p className="text-[10.5px] text-slate-400">Drag the arrow’s end onto another step to change where it goes.</p>
            </div>

            <div className="mt-3 flex items-center gap-2">
                <button
                    type="button"
                    onClick={onDelete}
                    title="Delete connection"
                    className="inline-flex size-7 items-center justify-center rounded-md text-slate-400 hover:bg-rose-50 hover:text-rose-600"
                >
                    <Trash2Icon className="w-3.5 h-3.5" />
                </button>
                <button
                    type="button"
                    onClick={() => save(branch.target_sequence_id)}
                    className="ml-auto h-7 rounded-md bg-sky-600 px-3 text-[12px] font-medium text-white hover:bg-sky-700"
                >
                    Save
                </button>
            </div>
        </div>
    );
}
