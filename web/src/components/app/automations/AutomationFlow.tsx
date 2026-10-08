// AutomationFlow is the automation builder: the trigger at the top and every
// step below it as a tree the layout draws on its own. A "+" on any line or at
// the end of a branch adds a step there; a condition splits into Yes and No
// columns, a switch into one column per case. Clicking a step edits it in the
// side panel. The whole flow is edited locally and saved as one graph, with
// undo and redo while editing.

"use client";

import React from "react";
import useAutomationPanel from "./useAutomationPanel";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    ArrowLeftIcon,
    CheckIcon,
    CopyIcon,
    HistoryIcon,
    Loader2Icon,
    PlayIcon,
    Redo2Icon,
    ShieldAlertIcon,
    Trash2Icon,
    Undo2Icon,
} from "lucide-react";
import toast from "react-hot-toast/headless";
import PermissionButton from "@/components/ui/PermissionButton";
import { usePermission } from "@/hooks/usePermission";
import type { AppError } from "@/lib/api/client/normalizeError";
import { Label } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuLabel, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { useUpdateAutomation, useTestAutomation } from "@/lib/api/hooks/app/automations/useAutomationMutations";
import type { Automation, AutomationCondition, AutomationGraph, AutomationNode, DryRunResponse } from "@/lib/api/models/app/automations/Automation";
import { PROVIDER_LABELS, type IntegrationCatalogEntry, type IntegrationConnection } from "@/lib/api/models/app/integrations/Integration";
import {
    TRIGGER_EVENTS,
    actionLabel,
    defaultConditionForTrigger,
    triggerIsInboundWebhook,
    triggerLabel,
} from "@/lib/api/models/app/automations/meta";
import { useAutomations } from "@/lib/api/hooks/app/automations/useAutomations";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import CursorChat from "@/components/app/presence/CursorChat";
import { useSuppressGlobalCursors } from "@/components/app/presence/GlobalCursors";
import { usePresenceResource, useResourceViewers } from "@/hooks/PresenceProvider";
import { useUserProfile } from "@/hooks/context/user";
import { cursorColor, useLiveCanvas } from "@/hooks/useLiveCanvas";
import { isSelfMutation } from "@/lib/realtime/selfActivity";
import { cn } from "@/lib/utils";
import FlowCanvas, { FlowOverlay } from "@/components/app/flow/FlowCanvas";
import FlowPanel from "@/components/app/flow/FlowPanel";
import StepPicker, { type PickerItem } from "@/components/app/flow/StepPicker";
import { STEP_H, STEP_W, StepBadge, StepCard, TERMINAL_H, TERMINAL_W, TerminalCard, type StepMenuItem } from "@/components/app/flow/StepCard";
import { layoutFlow, type InsertPoint } from "@/components/app/flow/tree";
import { ActionEditor, ConditionEditor, InboundUrlField } from "./AutomationEditors";
import { defaultConfigForAction } from "./automationActions";
import { InsightsPanel } from "./AutomationInsights";
import {
    addGoto,
    flowSource,
    gotoTargets,
    graphIssues,
    healForSave,
    insertNode,
    isSwitch,
    newId,
    nodeIssue,
    normalizeGraph,
    removalImpact,
    removeEdge,
    rawCases,
    renameCases,
    triggerId,
} from "./automationGraph";
import { pickerItems, stepView, type StepContext } from "./automationSteps";

interface Doc {
    graph: AutomationGraph;
    trigger: string;
}

const HISTORY_LIMIT = 100;
// Keystrokes in one field within this window undo as one change.
const COALESCE_MS = 900;

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
const MOD = isMac ? "⌘" : "Ctrl+";

const sizeOf = (g: AutomationGraph) => (id: string) =>
    g.nodes.find((n) => n.id === id)?.type === "stop" ? { w: TERMINAL_W, h: TERMINAL_H } : { w: STEP_W, h: STEP_H };

// What persists: positions are derived from the structure, so they never make
// the flow dirty.
function signature(name: string, enabled: boolean, doc: Doc): string {
    return JSON.stringify({
        name: name.trim(),
        enabled,
        trigger: doc.trigger,
        nodes: doc.graph.nodes.map((n) => ({ id: n.id, type: n.type, action: n.action ?? null, connection_id: n.connection_id ?? null, config: n.config ?? null, condition: n.condition ?? null })),
        edges: doc.graph.edges.map((e) => ({ s: e.source, t: e.target, w: e.when ?? "" })),
    });
}

function typingTarget(t: EventTarget | null): boolean {
    const el = t as HTMLElement | null;
    return !!el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.tagName === "SELECT" || el.isContentEditable);
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
    const confirm = useConfirm();
    const canEdit = usePermission("MANAGE_SETTINGS");
    const resource = `automation:${automation.id}`;
    usePresenceResource(resource, canEdit ? "editing" : "viewing");
    useSuppressGlobalCursors();

    const [name, setName] = React.useState(automation.name);
    const [enabled, setEnabled] = React.useState(automation.enabled);
    const [doc, setDoc] = React.useState<Doc>(() => ({ graph: normalizeGraph(automation.graph), trigger: automation.trigger_event }));
    const { graph, trigger } = doc;
    const [selectedId, setSelectedId] = React.useState<string | null>(null);
    const [picker, setPicker] = React.useState<{ at: InsertPoint; anchor: DOMRect } | null>(null);
    const { panel, closePanel, showTest, toggleHistory, toggleTest } = useAutomationPanel(automation.id);
    const [testResult, setTestResult] = React.useState<DryRunResponse | null>(null);
    const [remoteUpdate, setRemoteUpdate] = React.useState<Automation | null>(null);

    // ── Undo / redo ──────────────────────────────────────────────────────────
    const past = React.useRef<Doc[]>([]);
    const future = React.useRef<Doc[]>([]);
    const lastCoalesce = React.useRef<{ key: string; at: number } | null>(null);
    const [, bumpHistory] = React.useReducer((x: number) => x + 1, 0);
    const docRef = React.useRef(doc);
    docRef.current = doc;

    const commit = React.useCallback((next: Doc | ((d: Doc) => Doc), coalesce?: string) => {
        const prev = docRef.current;
        const value = typeof next === "function" ? next(prev) : next;
        if (value === prev) return;
        const now = Date.now();
        const merge = coalesce && lastCoalesce.current?.key === coalesce && now - lastCoalesce.current.at < COALESCE_MS;
        if (!merge) {
            past.current = [...past.current.slice(-HISTORY_LIMIT + 1), prev];
        }
        lastCoalesce.current = coalesce ? { key: coalesce, at: now } : null;
        future.current = [];
        docRef.current = value;
        setDoc(value);
        bumpHistory();
    }, []);

    const undo = React.useCallback(() => {
        const prev = past.current.pop();
        if (!prev) return;
        future.current.push(docRef.current);
        lastCoalesce.current = null;
        docRef.current = prev;
        setDoc(prev);
        bumpHistory();
    }, []);
    const redo = React.useCallback(() => {
        const next = future.current.pop();
        if (!next) return;
        past.current.push(docRef.current);
        lastCoalesce.current = null;
        docRef.current = next;
        setDoc(next);
        bumpHistory();
    }, []);

    // ── Dirty tracking + teammate saves ──────────────────────────────────────
    const baselineRef = React.useRef(signature(automation.name, automation.enabled, doc));
    const dirty = signature(name, enabled, doc) !== baselineRef.current;

    const seedFrom = React.useCallback((a: Automation) => {
        const next: Doc = { graph: normalizeGraph(a.graph), trigger: a.trigger_event };
        setName(a.name);
        setEnabled(a.enabled);
        docRef.current = next;
        setDoc(next);
        past.current = [];
        future.current = [];
        bumpHistory();
        baselineRef.current = signature(a.name, a.enabled, next);
    }, []);

    // `updated_at` tells a teammate's save from our own (the client revives it
    // into a Date, so compare its time, not the object).
    const serverVersion = React.useCallback((a: Automation) => {
        const u = a.updated_at as unknown;
        if (u instanceof Date) return `t:${u.getTime()}`;
        if (typeof u === "string" && u) return `t:${u}`;
        return JSON.stringify({ name: (a.name || "").trim(), enabled: a.enabled, trigger: a.trigger_event, graph: a.graph ?? null });
    }, []);
    const serverVersionRef = React.useRef(serverVersion(automation));
    // Our own save's realtime refetch can land before its HTTP response.
    const selfSaveUntil = React.useRef(0);

    React.useEffect(() => {
        const incoming = serverVersion(automation);
        if (incoming === serverVersionRef.current) return;
        serverVersionRef.current = incoming;
        if (Date.now() < selfSaveUntil.current || isSelfMutation("automation", automation.id)) {
            if (!dirty) seedFrom(automation);
            return;
        }
        if (!dirty) {
            seedFrom(automation);
            toast.success("Updated by a teammate", { id: "automation-remote" });
        } else {
            setRemoteUpdate(automation);
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [automation]);

    // ── Connections, labels ──────────────────────────────────────────────────
    const targets = React.useMemo(
        () => connections.filter((c) => (c.status === "connected" || c.status === "degraded") && c.provider !== "calendly" && c.provider !== "cal_com"),
        [connections],
    );
    const connById = React.useMemo(() => new Map(connections.map((c) => [c.id, c])), [connections]);
    const actionsForProvider = React.useCallback((provider?: string) => catalog.find((e) => e.provider === provider)?.action_types ?? [], [catalog]);
    const connLabel = React.useCallback(
        (id?: string) => {
            if (!id) return "";
            const c = connById.get(id);
            if (!c) return "Unknown integration";
            const provider = PROVIDER_LABELS[c.provider] ?? c.provider;
            return c.label && c.label.toLowerCase() !== c.provider ? `${provider} · ${c.label}` : provider;
        },
        [connById],
    );
    const providerOf = React.useCallback((id?: string) => (id ? (connById.get(id)?.provider ?? "") : ""), [connById]);
    const automationsQ = useAutomations();
    const automationName = React.useCallback(
        (id: string) => automationsQ.data?.automations?.find((a: Automation) => a.id === id)?.name,
        [automationsQ.data],
    );
    const ctx: StepContext = React.useMemo(() => ({ trigger, connLabel, providerOf, automationName }), [trigger, connLabel, providerOf, automationName]);

    // ── Layout, issues, test overlay ─────────────────────────────────────────
    const layout = React.useMemo(() => layoutFlow(flowSource(graph, sizeOf(graph))), [graph]);
    const nodeById = React.useMemo(() => new Map(graph.nodes.map((n) => [n.id, n])), [graph]);
    const order = React.useMemo(() => layout.nodes.filter((n) => n.kind === "step").map((n) => n.ref), [layout]);
    const issues = React.useMemo(() => graphIssues(graph, trigger, order), [graph, trigger, order]);
    const blocking = issues.filter((i) => i.blocking);
    const issueById = React.useMemo(() => new Map(issues.map((i) => [i.id, i])), [issues]);

    // A test trace describes the graph it ran on; an edit makes it stale.
    React.useEffect(() => setTestResult(null), [graph]);
    const traceById = React.useMemo(() => {
        const m = new Map<string, string>();
        for (const r of testResult?.trace ?? []) m.set(r.node_id, r.status);
        return m;
    }, [testResult]);
    const showTrace = panel === "test" && !!testResult;
    const dimmed = React.useMemo(() => {
        if (!showTrace) return null;
        const t = triggerId(graph);
        return new Set(graph.nodes.filter((n) => n.id !== t && !traceById.has(n.id)).map((n) => n.id));
    }, [showTrace, graph, traceById]);

    // ── Editing ──────────────────────────────────────────────────────────────
    const select = React.useCallback((id: string | null) => {
        setSelectedId(id);
        if (id) closePanel();
    }, [closePanel]);

    // Each switch's last non-empty case name per row (see renameCases).
    const caseMemory = React.useRef(new Map<string, string[]>());
    const patchNode = React.useCallback(
        (id: string, patch: Partial<AutomationNode>, coalesce?: string) =>
            commit((d) => {
                const prev = d.graph.nodes.find((n) => n.id === id);
                if (!prev) return d;
                const next = { ...prev, ...patch };
                let g: AutomationGraph = { nodes: d.graph.nodes.map((n) => (n.id === id ? next : n)), edges: d.graph.edges };
                if (isSwitch(prev) && isSwitch(next)) {
                    const r = renameCases(g, id, rawCases(prev), rawCases(next), caseMemory.current.get(id));
                    g = r.graph;
                    caseMemory.current.set(id, r.remembered);
                }
                return { ...d, graph: g };
            }, coalesce),
        [commit],
    );

    const removeStep = React.useCallback(
        (id: string) => {
            const n = docRef.current.graph.nodes.find((x) => x.id === id);
            if (!n || n.type === "trigger") return;
            const { graph: next, dropped } = removalImpact(docRef.current.graph, id);
            const apply = () => {
                commit((d) => ({ ...d, graph: next }));
                setSelectedId((cur) => (cur === id || (cur && dropped.includes(cur)) ? null : cur));
            };
            if (dropped.length > 0) {
                confirm.show(`Delete this step and the ${dropped.length === 1 ? "step" : `${dropped.length} steps`} only it leads to?`, async () => apply());
                return;
            }
            apply();
            toast.success(`Step deleted. ${MOD}Z brings it back.`, { id: "automation-step-deleted" });
        },
        [commit, confirm],
    );

    const openPicker = React.useCallback((at: InsertPoint, anchor: DOMRect) => {
        setPicker((cur) => (cur && cur.at.from === at.from && cur.at.port === at.port && cur.at.before === at.before ? null : { at, anchor }));
    }, []);

    const openPickerFromCard = (at: InsertPoint, id: string) => {
        const el = document.querySelector(`[data-flow-step="${CSS.escape(id)}"]`);
        const r = el?.getBoundingClientRect() ?? new DOMRect(window.innerWidth / 2, window.innerHeight / 2, 0, 0);
        setPicker({ at, anchor: new DOMRect(r.left, r.top, r.width, r.height) });
    };

    const pick = (item: PickerItem) => {
        if (!picker) return;
        const { at } = picker;
        setPicker(null);
        if (item.key.startsWith("goto:")) {
            commit((d) => ({ ...d, graph: addGoto(d.graph, at.from, at.port, item.key.slice("goto:".length)) }));
            return;
        }
        let node: AutomationNode | null = null;
        const id = newId();
        if (item.key === "condition") node = { id, type: "condition", condition: defaultConditionForTrigger(trigger), x: 0, y: 0 };
        else if (item.key === "stop") node = { id, type: "stop", x: 0, y: 0 };
        else if (item.key.startsWith("native:")) {
            const action = item.key.slice("native:".length);
            node = { id, type: "action", action: action as AutomationNode["action"], config: defaultConfigForAction(action, trigger), x: 0, y: 0 };
        } else if (item.key.startsWith("int:")) {
            const [, conn, action] = item.key.split(":");
            node = { id, type: "action", action: action as AutomationNode["action"], connection_id: conn, config: {}, x: 0, y: 0 };
        }
        if (!node) return;
        const added = node;
        commit((d) => ({ ...d, graph: insertNode(d.graph, at, added) }));
        if (added.type !== "stop") select(id);
    };

    const duplicate = (id: string) => {
        const src = docRef.current.graph.nodes.find((n) => n.id === id);
        if (!src) return;
        const copy: AutomationNode = { ...src, id: newId(), config: src.config ? structuredClone(src.config) : undefined };
        const next = docRef.current.graph.edges.find((e) => e.source === id && (e.when ?? "") === "")?.target ?? null;
        commit((d) => ({ ...d, graph: insertNode(d.graph, { from: id, port: "", before: next }, copy) }));
        select(copy.id);
    };

    // ── Save, test ───────────────────────────────────────────────────────────
    const save = async (): Promise<boolean> => {
        if (blocking.length) {
            const first = blocking[0];
            select(first.id);
            toast.error(blocking.length === 1 ? first.message : `${blocking.length} steps need setup before saving. ${first.message}`);
            return false;
        }
        const g = healForSave(graph);
        const pos = new Map(layout.nodes.filter((n) => n.kind === "step").map((n) => [n.ref, n]));
        const out: AutomationGraph = {
            nodes: g.nodes.map((n) => ({ ...n, x: Math.round(pos.get(n.id)?.x ?? 0), y: Math.round(pos.get(n.id)?.y ?? 0) })),
            edges: g.edges,
        };
        selfSaveUntil.current = Date.now() + 8000;
        try {
            const res = await update.mutateAsync({ id: automation.id, w: { name: name.trim() || "Automation", enabled, trigger_event: trigger, filter: {}, graph: out } });
            baselineRef.current = signature(name, enabled, doc);
            if (res?.automation) serverVersionRef.current = serverVersion(res.automation);
            selfSaveUntil.current = Date.now() + 8000;
            setRemoteUpdate(null);
            toast.success("Automation saved");
            return true;
        } catch (e) {
            const msg = (e as AppError)?.message;
            toast.error(msg ? `Could not save automation: ${msg}` : "Could not save automation");
            return false;
        }
    };
    const saveRef = React.useRef(save);
    saveRef.current = save;

    const runTest = async (data?: Record<string, unknown>, skipNodeIds?: string[]) => {
        if (dirty && canEdit && !(await save())) return;
        try {
            const res = await test.mutateAsync({ id: automation.id, data, skipNodeIds });
            setTestResult(res);
            showTest();
            setSelectedId(null);
        } catch {
            toast.error("Could not run the test");
        }
    };
    const actionSteps = React.useMemo(
        () =>
            order
                .map((id) => nodeById.get(id))
                .filter((n): n is AutomationNode => n?.type === "action")
                .map((n) => ({ id: n.id, label: n.action ? stepView(n, ctx).title || actionLabel(String(n.action)) : "Unconfigured action" })),
        [order, nodeById, ctx],
    );

    const guardedBack = () => {
        if (dirty) {
            confirm.show("You have unsaved changes. Leave without saving?", () => onBack());
            return;
        }
        onBack();
    };

    // Closing the tab with unsaved work asks first.
    React.useEffect(() => {
        if (!dirty) return;
        const onUnload = (e: BeforeUnloadEvent) => {
            e.preventDefault();
        };
        window.addEventListener("beforeunload", onUnload);
        return () => window.removeEventListener("beforeunload", onUnload);
    }, [dirty]);

    // ── Keyboard ─────────────────────────────────────────────────────────────
    React.useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            const mod = e.metaKey || e.ctrlKey;
            if (mod && e.key.toLowerCase() === "s") {
                e.preventDefault();
                if (canEdit && dirty) void saveRef.current();
                return;
            }
            if (typingTarget(e.target) || document.querySelector("[data-floating], [role='alertdialog']")) return;
            if (mod && e.key.toLowerCase() === "z") {
                e.preventDefault();
                if (e.shiftKey) redo();
                else undo();
            } else if (mod && e.key.toLowerCase() === "y") {
                e.preventDefault();
                redo();
            } else if ((e.key === "Delete" || e.key === "Backspace") && selectedId && canEdit) {
                e.preventDefault();
                removeStep(selectedId);
            } else if (e.key === "Escape" && (selectedId || panel)) {
                setSelectedId(null);
                closePanel();
            }
        };
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [canEdit, dirty, undo, redo, removeStep, selectedId, panel, closePanel]);

    // ── Live collaboration: cursors and selections ───────────────────────────
    const hasPeers = useResourceViewers(resource).length > 0;
    const live = useLiveCanvas(resource, { enabled: hasPeers });
    const { pushSelect } = live;
    React.useEffect(() => pushSelect(selectedId ? [selectedId] : []), [pushSelect, selectedId]);
    const { user: selfUser } = useUserProfile();
    const selfColor = cursorColor(selfUser?.id ?? "");

    // ── Rendering ────────────────────────────────────────────────────────────
    const stepTitle = (id: string) => {
        const n = nodeById.get(id);
        return n ? stepView(n, ctx).title : "a step";
    };

    const renderStep = (id: string, selected: boolean, detached: boolean) => {
        const n = nodeById.get(id);
        if (!n) return null;
        if (n.type === "stop") return <TerminalCard icon={<ShieldAlertIcon />} label="End" selected={selected} onRemove={canEdit ? () => removeStep(id) : undefined} />;
        const v = stepView(n, ctx);
        const issue = issueById.get(id);
        const status = showTrace ? traceById.get(id) : undefined;
        const badge = status ? (
            <StepBadge tone={status === "error" ? "error" : status === "skipped" ? "muted" : status === "branch_false" ? "muted" : "ok"}>
                {status === "error" ? "Failed" : status === "skipped" ? "Skipped" : status === "branch_true" ? "Yes" : status === "branch_false" ? "No" : "Ran"}
            </StepBadge>
        ) : issue?.blocking ? (
            <span title={issue.message} aria-label={`Needs setup: ${issue.message}`} className="size-1.5 shrink-0 rounded-full bg-amber-400" />
        ) : null;
        const menu: StepMenuItem[] = [];
        if (canEdit && n.type === "action") {
            if (!isSwitch(n)) menu.push({ label: "Duplicate", icon: <CopyIcon className="size-3.5" />, onSelect: () => duplicate(id) });
            if (!graph.edges.some((e) => e.source === id && e.when === "error")) {
                menu.push({ label: "Add an error path", icon: <AlertTriangleIcon className="size-3.5" />, onSelect: () => openPickerFromCard({ from: id, port: "error", before: null }, id) });
            }
        }
        if (canEdit && n.type !== "trigger") menu.push({ label: "Delete step", icon: <Trash2Icon className="size-3.5" />, onSelect: () => removeStep(id), danger: true });
        return (
            <StepCard
                icon={v.icon}
                tile={v.tile}
                tone={v.tone}
                kicker={v.kicker}
                title={v.title}
                summary={v.summary}
                badge={badge}
                selected={selected}
                detached={detached}
                hint={issue ? `${issue.blocking ? "Needs setup" : "Not connected"}: ${issue.message}` : undefined}
                menu={menu}
            />
        );
    };

    const selectedNode = selectedId ? (nodeById.get(selectedId) ?? null) : null;
    const pickerAtEnd = !!picker && picker.at.before === null;
    const items = picker
        ? pickerItems({
              atEnd: pickerAtEnd,
              targets,
              connLabel,
              actionsForProvider,
              gotoCandidates: pickerAtEnd
                  ? gotoTargets(graph, picker.at.from)
                        .filter((n) => !graph.edges.some((e) => e.source === picker.at.from && (e.when ?? "") === picker.at.port && e.target === n.id))
                        .sort((a, b) => order.indexOf(a.id) - order.indexOf(b.id))
                        .map((n) => ({ id: n.id, view: stepView(n, ctx) }))
                  : [],
          })
        : [];

    const iconBtn = "inline-flex size-7 items-center justify-center rounded-md text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900 disabled:pointer-events-none disabled:opacity-35";

    return (
        <div className="flex h-full flex-col">
            <header className="flex min-h-12 shrink-0 flex-wrap items-center gap-2 gap-y-1.5 border-b border-slate-200 bg-white px-3 py-1.5 md:h-12 md:flex-nowrap md:py-0">
                <button type="button" onClick={guardedBack} className="inline-flex h-7 w-7 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-900" aria-label="Back to automations">
                    <ArrowLeftIcon className="h-4 w-4" />
                </button>
                <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Automation name"
                    aria-label="Automation name"
                    readOnly={!canEdit}
                    className="h-7 w-56 max-w-[30vw] rounded-md border border-transparent px-2 text-[13px] font-medium text-slate-900 outline-none hover:bg-slate-50 focus:border-sky-400 focus:bg-white focus:ring-2 focus:ring-sky-100 md:max-w-[36vw]"
                />
                <ResourceViewers resource={resource} className="shrink-0" />
                <button
                    type="button"
                    role="switch"
                    aria-checked={enabled}
                    aria-label="Enable automation"
                    disabled={!canEdit}
                    onClick={() => setEnabled((v) => !v)}
                    title={enabled ? "Automation is live" : "Automation is paused"}
                    className="inline-flex h-7 cursor-pointer select-none items-center gap-2 rounded-md outline-none focus-visible:ring-2 focus-visible:ring-sky-200 disabled:cursor-default disabled:opacity-70"
                >
                    <span className={cn("relative inline-flex h-[18px] w-8 shrink-0 items-center rounded-full transition-colors", enabled ? "bg-sky-600" : "bg-slate-300")}>
                        <span className={cn("inline-block size-3.5 rounded-full bg-white shadow-sm transition-transform duration-150", enabled ? "translate-x-[16px]" : "translate-x-[2px]")} />
                    </span>
                    <span className={cn("text-[12px] font-medium transition-colors", enabled ? "text-slate-700" : "text-slate-400")}>{enabled ? "Active" : "Off"}</span>
                </button>
                <div className="ml-auto flex items-center gap-1.5">
                    {issues.length > 0 && (
                        <PopoverMenu align="end">
                            <PopoverMenuTrigger asChild>
                                <button
                                    type="button"
                                    className={cn(
                                        "inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-[12px] font-medium transition-colors",
                                        blocking.length ? "border-amber-200 bg-amber-50 text-amber-700 hover:bg-amber-100" : "border-slate-200 text-slate-600 hover:border-slate-300",
                                    )}
                                >
                                    <AlertTriangleIcon className="h-3.5 w-3.5" />
                                    <span className="hidden md:inline">{issues.length === 1 ? "1 issue" : `${issues.length} issues`}</span>
                                    <span className="md:hidden">{issues.length}</span>
                                </button>
                            </PopoverMenuTrigger>
                            <PopoverMenuContent minWidth={300}>
                                <PopoverMenuLabel>{blocking.length ? "Fix these before saving" : "Worth a look"}</PopoverMenuLabel>
                                {issues.map((i) => (
                                    <PopoverMenuItem key={i.id} onSelect={() => select(i.id)} icon={<AlertTriangleIcon className={cn("h-3.5 w-3.5", i.blocking ? "text-amber-500" : "text-slate-400")} />}>
                                        <span className="font-medium text-slate-800">{stepTitle(i.id)}</span>
                                        <span className="text-slate-400"> · {i.message}</span>
                                    </PopoverMenuItem>
                                ))}
                            </PopoverMenuContent>
                        </PopoverMenu>
                    )}
                    {canEdit && (
                        <div className="hidden items-center md:flex">
                            <button type="button" className={iconBtn} onClick={undo} disabled={past.current.length === 0} aria-label="Undo" title={`Undo (${MOD}Z)`}>
                                <Undo2Icon className="h-3.5 w-3.5" />
                            </button>
                            <button type="button" className={iconBtn} onClick={redo} disabled={future.current.length === 0} aria-label="Redo" title={`Redo (${isMac ? "⇧⌘Z" : "Ctrl+Y"})`}>
                                <Redo2Icon className="h-3.5 w-3.5" />
                            </button>
                        </div>
                    )}
                    <button
                        type="button"
                        onClick={() => {
                            setSelectedId(null);
                            toggleHistory();
                        }}
                        aria-label="Run history"
                        className={cn(
                            "inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-[12px] transition-colors",
                            panel === "history" ? "border-sky-300 bg-sky-50 text-sky-700" : "border-slate-200 text-slate-700 hover:border-slate-300 hover:text-slate-900",
                        )}
                    >
                        <HistoryIcon className="h-3.5 w-3.5" />
                        <span className="hidden md:inline">History</span>
                    </button>
                    <button
                        type="button"
                        onClick={() => {
                            setSelectedId(null);
                            toggleTest();
                        }}
                        aria-label="Test"
                        className={cn(
                            "inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-[12px] transition-colors",
                            panel === "test" ? "border-sky-300 bg-sky-50 text-sky-700" : "border-slate-200 text-slate-700 hover:border-slate-300 hover:text-slate-900",
                        )}
                    >
                        <PlayIcon className="h-3.5 w-3.5" />
                        <span className="hidden md:inline">Test</span>
                    </button>
                    <PermissionButton
                        permission="MANAGE_SETTINGS"
                        type="button"
                        onClick={save}
                        disabled={!dirty || update.isPending}
                        aria-label={dirty ? "Save" : "Saved"}
                        title={dirty ? `Save changes (${MOD}S)` : "No unsaved changes"}
                        className={cn(
                            "inline-flex h-7 items-center gap-1.5 rounded-md px-3 text-[12px] font-medium transition-colors",
                            dirty ? "bg-sky-600 text-white shadow-sm hover:bg-sky-700" : "cursor-default bg-slate-100 text-slate-400",
                        )}
                    >
                        {update.isPending ? <Loader2Icon className="h-3.5 w-3.5 animate-spin" /> : <CheckIcon className={cn("h-3.5 w-3.5", !dirty && "text-slate-300")} />}
                        <span className="hidden md:inline">{dirty ? "Save" : "Saved"}</span>
                    </PermissionButton>
                </div>
            </header>

            <div className="relative flex min-h-0 flex-1">
                <div className="relative min-w-0 flex-1 bg-slate-50/40">
                    <FlowCanvas
                        layout={layout}
                        renderStep={renderStep}
                        stepTitle={stepTitle}
                        selectedId={selectedId}
                        onSelect={select}
                        onInsert={canEdit ? openPicker : undefined}
                        onRemoveGoto={canEdit ? (from, port, target) => commit((d) => ({ ...d, graph: removeEdge(d.graph, from, port, target) })) : undefined}
                        activeInsert={picker?.at ?? null}
                        dimmed={dimmed}
                        revealId={selectedId}
                        cursors={live.cursors}
                        selections={live.selections}
                        onCursor={(p) => (p ? live.active && live.pushCursor(p.x, p.y) : live.clearCursor())}
                    >
                        {remoteUpdate && (
                            <FlowOverlay position="top-center">
                                <div className="flex items-center gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-1.5 shadow-sm">
                                    <span className="text-[12px] font-medium text-amber-800">A teammate changed this automation.</span>
                                    <button
                                        type="button"
                                        onClick={() => {
                                            seedFrom(remoteUpdate);
                                            setRemoteUpdate(null);
                                        }}
                                        className="h-6 rounded bg-amber-600 px-2 text-[11.5px] font-medium text-white transition-colors hover:bg-amber-700"
                                    >
                                        Load their version
                                    </button>
                                    <button type="button" onClick={() => setRemoteUpdate(null)} className="h-6 rounded px-2 text-[11.5px] font-medium text-amber-700 transition-colors hover:bg-amber-100">
                                        Keep mine
                                    </button>
                                </div>
                            </FlowOverlay>
                        )}
                        {showTrace && (
                            <FlowOverlay position="top-center">
                                <div className="flex items-center gap-2 rounded-md border border-sky-200 bg-sky-50 px-3 py-1.5 text-[12px] text-sky-800 shadow-sm">
                                    Showing the path the test took.
                                    <button type="button" onClick={() => setTestResult(null)} className="font-medium text-sky-700 underline-offset-2 hover:underline">
                                        Clear
                                    </button>
                                </div>
                            </FlowOverlay>
                        )}
                    </FlowCanvas>
                    <CursorChat active={live.active} color={selfColor} setChat={live.setChat} />
                </div>

                <AnimatePresence initial={false}>
                    {selectedNode && selectedNode.type !== "stop" && !panel && (
                        <StepPanel
                            key={selectedNode.id}
                            node={selectedNode}
                            ctx={ctx}
                            canEdit={canEdit}
                            selfId={automation.id}
                            inboundUrl={automation.inbound_url}
                            targets={targets}
                            connLabel={connLabel}
                            actionsForProvider={actionsForProvider}
                            providerOf={providerOf}
                            onClose={() => setSelectedId(null)}
                            onTrigger={(ev) => commit((d) => ({ ...d, trigger: ev }))}
                            onPatch={(patch, key) => patchNode(selectedNode.id, patch, key)}
                            onDelete={() => removeStep(selectedNode.id)}
                        />
                    )}
                    {panel && (
                        <FlowPanel key={`insights-${panel}`} bare>
                            <InsightsPanel
                                mode={panel}
                                automationId={automation.id}
                                trigger={trigger}
                                steps={actionSteps}
                                testResult={testResult}
                                testing={test.isPending}
                                onRun={runTest}
                                onClose={closePanel}
                            />
                        </FlowPanel>
                    )}
                </AnimatePresence>
            </div>

            {picker && <StepPicker anchor={picker.anchor} clearance={STEP_W / 2} items={items} title={picker.at.port === "error" ? "Add an error path" : "Add a step"} onPick={pick} onClose={() => setPicker(null)} />}
        </div>
    );
}

// StepPanel edits one step in the side panel. Edits apply to the canvas as they
// are made; the builder's Save writes them.
function StepPanel({
    node,
    ctx,
    canEdit,
    selfId,
    inboundUrl,
    targets,
    connLabel,
    actionsForProvider,
    providerOf,
    onClose,
    onTrigger,
    onPatch,
    onDelete,
}: {
    node: AutomationNode;
    ctx: StepContext;
    canEdit: boolean;
    selfId: string;
    inboundUrl?: string;
    targets: IntegrationConnection[];
    connLabel: (id?: string) => string;
    actionsForProvider: (provider?: string) => string[];
    providerOf: (id?: string) => string;
    onClose: () => void;
    onTrigger: (ev: string) => void;
    onPatch: (patch: Partial<AutomationNode>, coalesce?: string) => void;
    onDelete: () => void;
}) {
    const v = stepView(node, ctx);
    const issue = nodeIssue(node, ctx.trigger);
    const triggerOptions: SelectOption[] = TRIGGER_EVENTS.map((ev) => ({ value: ev, label: triggerLabel(ev) }));
    return (
        <FlowPanel
            readOnly={!canEdit}
            icon={v.icon}
            tile={v.tile}
            tone={v.tone}
            kicker={v.kicker}
            title={v.title}
            onClose={onClose}
            footer={
                canEdit && node.type !== "trigger" ? (
                    <button
                        type="button"
                        onClick={onDelete}
                        className="inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-[12px] font-medium text-rose-600 transition-colors hover:bg-rose-50"
                    >
                        <Trash2Icon className="h-3.5 w-3.5" />
                        Delete step
                    </button>
                ) : undefined
            }
        >
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.15 }} className="space-y-3 p-3">
                {issue && (
                    <div className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-2.5 py-2 text-[11.5px] text-amber-800">
                        <AlertTriangleIcon className="mt-px h-3.5 w-3.5 shrink-0" />
                        {issue}
                    </div>
                )}
                {node.type === "trigger" ? (
                    <>
                        <div>
                            <Label>When this happens</Label>
                            <SelectMenu value={ctx.trigger} onChange={onTrigger} options={triggerOptions} className="w-full" fullWidth />
                        </div>
                        {triggerIsInboundWebhook(ctx.trigger) ? (
                            <InboundUrlField inboundUrl={inboundUrl} />
                        ) : (
                            <p className="text-[11.5px] leading-relaxed text-slate-400">
                                Add an If / else under the trigger to act only on some events, for example only positive replies.
                            </p>
                        )}
                    </>
                ) : node.type === "condition" ? (
                    <ConditionEditor
                        trigger={ctx.trigger}
                        condition={node.condition ?? defaultConditionForTrigger(ctx.trigger)}
                        onChange={(c: AutomationCondition) => onPatch({ condition: c }, `cond:${node.id}`)}
                    />
                ) : (
                    <ActionEditor
                        trigger={ctx.trigger}
                        selfId={selfId}
                        data={{ action: node.action, connection_id: node.connection_id, config: node.config }}
                        targets={targets}
                        connLabel={connLabel}
                        actionsForProvider={actionsForProvider}
                        providerOf={providerOf}
                        onAction={(patch) => {
                            const p: Partial<AutomationNode> = {};
                            if ("action" in patch) p.action = (patch.action || undefined) as AutomationNode["action"];
                            if ("connection_id" in patch) p.connection_id = patch.connection_id as string | undefined;
                            if ("config" in patch) p.config = patch.config as Record<string, unknown>;
                            onPatch(p, "action" in patch || "connection_id" in patch ? undefined : `cfg:${node.id}`);
                        }}
                    />
                )}
            </motion.div>
        </FlowPanel>
    );
}
