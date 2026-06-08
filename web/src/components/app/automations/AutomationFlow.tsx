// AutomationFlow — the visual builder for one automation, modeled on the
// campaign sequence canvas (React Flow / @xyflow/react). A single Trigger node
// ("when X fires") fans out to Action nodes ("then do this") across the org's
// integrations. Clicking a node opens its editor on the right; Save persists
// the whole flow (trigger + steps) in one shot.

"use client";

import React from "react";
import {
    ReactFlow,
    Background,
    Controls,
    Panel,
    Handle,
    Position,
    type Node,
    type Edge,
    type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { ArrowLeftIcon, CheckIcon, Loader2Icon, PlusIcon, Trash2Icon, XIcon, ZapIcon } from "lucide-react";
import toast from "react-hot-toast";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { useUpdateAutomation } from "@/lib/api/hooks/app/automations/useAutomationMutations";
import type { Automation } from "@/lib/api/models/app/automations/Automation";
import {
    PROVIDER_LABELS,
    REPLY_INTENT_OPTIONS,
    type IntegrationAction,
    type IntegrationCatalogEntry,
    type IntegrationConnection,
} from "@/lib/api/models/app/integrations/Integration";
import {
    TRIGGER_EVENTS,
    actionLabel,
    actionNeedsChannel,
    actionNeedsURL,
    actionSupportsTemplate,
    triggerLabel,
    triggerSupportsIntentFilter,
} from "@/lib/api/models/app/automations/meta";
import ProviderGlyph from "@/app/app/integrations/_components/ProviderGlyph";
import { cn } from "@/lib/utils";

type StepDraft = {
    key: string;
    connection_id: string;
    action: string;
    config: Record<string, unknown>;
};

const nodeTypes = { trigger: TriggerNode, action: ActionNode };

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

    const [name, setName] = React.useState(automation.name);
    const [enabled, setEnabled] = React.useState(automation.enabled);
    const [trigger, setTrigger] = React.useState(automation.trigger_event);
    const [intents, setIntents] = React.useState<string[]>(
        Array.isArray(automation.filter?.intents) ? (automation.filter!.intents as string[]) : [],
    );
    const [minConf, setMinConf] = React.useState<number>(Number(automation.filter?.min_confidence ?? 0));
    const [steps, setSteps] = React.useState<StepDraft[]>(() =>
        automation.steps.map((s) => ({
            key: s.id ?? crypto.randomUUID(),
            connection_id: s.connection_id,
            action: s.action,
            config: (s.config ?? {}) as Record<string, unknown>,
        })),
    );
    const [selected, setSelected] = React.useState<string | null>("trigger");

    // Connections usable as action targets (exclude inbound-only scheduling
    // providers — they have no outbound action handler).
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
        (id: string) => {
            const c = connById[id];
            if (!c) return "No integration";
            const provider = PROVIDER_LABELS[c.provider] ?? c.provider;
            return c.label && c.label.toLowerCase() !== c.provider ? `${provider} · ${c.label}` : provider;
        },
        [connById],
    );

    const removeStep = React.useCallback(
        (key: string) => {
            setSteps((prev) => prev.filter((s) => s.key !== key));
            setSelected((cur) => (cur === key ? "trigger" : cur));
        },
        [],
    );

    const addAction = () => {
        const first = targets[0];
        const acts = first ? actionsForProvider(first.provider) : [];
        const step: StepDraft = {
            key: crypto.randomUUID(),
            connection_id: first?.id ?? "",
            action: acts[0] ?? "",
            config: {},
        };
        setSteps((prev) => [...prev, step]);
        setSelected(step.key);
    };

    // --- canvas nodes/edges (derived; non-draggable, auto-laid-out) ----------
    const nodes: Node[] = React.useMemo(() => {
        const out: Node[] = [
            {
                id: "trigger",
                type: "trigger",
                position: { x: 0, y: 0 },
                data: { label: triggerLabel(trigger), selected: selected === "trigger" },
                draggable: false,
            },
        ];
        const n = steps.length;
        steps.forEach((s, i) => {
            out.push({
                id: s.key,
                type: "action",
                position: { x: (i - (n - 1) / 2) * 260, y: 200 },
                data: {
                    title: s.action ? actionLabel(s.action) : "Choose an action",
                    sub: connLabel(s.connection_id),
                    provider: connById[s.connection_id]?.provider ?? "",
                    selected: selected === s.key,
                    onDelete: () => removeStep(s.key),
                },
                draggable: false,
            });
        });
        return out;
    }, [trigger, steps, selected, connLabel, connById, removeStep]);

    const edges: Edge[] = React.useMemo(
        () => steps.map((s) => ({ id: `e-${s.key}`, source: "trigger", target: s.key, animated: enabled })),
        [steps, enabled],
    );

    const setStep = (key: string, patch: Partial<StepDraft>) =>
        setSteps((prev) => prev.map((s) => (s.key === key ? { ...s, ...patch } : s)));
    const setStepConfig = (key: string, k: string, v: unknown) =>
        setSteps((prev) => prev.map((s) => (s.key === key ? { ...s, config: { ...s.config, [k]: v } } : s)));
    const pickConnection = (key: string, connId: string) => {
        const provider = connById[connId]?.provider;
        const acts = actionsForProvider(provider);
        setStep(key, { connection_id: connId, action: acts[0] ?? "", config: {} });
    };

    const save = async () => {
        for (const s of steps) {
            if (!s.connection_id || !s.action) {
                toast.error("Every action needs an integration and an action");
                setSelected(s.key);
                return;
            }
            if (actionNeedsChannel(s.action) && !String(s.config.channel ?? "").trim()) {
                toast.error("A Slack action needs a channel");
                setSelected(s.key);
                return;
            }
            if (actionNeedsURL(s.action) && !String(s.config.url ?? "").trim()) {
                toast.error("A webhook action needs a URL");
                setSelected(s.key);
                return;
            }
        }
        const filter: Record<string, unknown> = {};
        if (triggerSupportsIntentFilter(trigger)) {
            if (intents.length) filter.intents = intents;
            if (minConf > 0) filter.min_confidence = minConf;
        }
        try {
            await update.mutateAsync({
                id: automation.id,
                w: {
                    name: name.trim() || "Automation",
                    enabled,
                    trigger_event: trigger,
                    filter,
                    steps: steps.map((s) => ({
                        connection_id: s.connection_id,
                        action: s.action as IntegrationAction,
                        config: s.config,
                    })),
                },
            });
            toast.success("Automation saved");
        } catch {
            toast.error("Could not save automation");
        }
    };

    const selectedStep = steps.find((s) => s.key === selected) ?? null;

    return (
        <div className="h-full flex flex-col">
            {/* Toolbar */}
            <header className="h-12 px-3 border-b border-slate-200 flex items-center gap-2 shrink-0 bg-white">
                <button
                    type="button"
                    onClick={onBack}
                    className="h-7 w-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center"
                    aria-label="Back"
                >
                    <ArrowLeftIcon className="w-4 h-4" />
                </button>
                <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder="Automation name"
                    className="h-7 px-2 w-56 max-w-[40vw] rounded-md text-[13px] font-medium text-slate-900 outline-none hover:bg-slate-50 focus:bg-white focus:border-sky-400 focus:ring-2 focus:ring-sky-100 border border-transparent"
                />
                <button
                    type="button"
                    onClick={() => setEnabled((v) => !v)}
                    className={cn(
                        "h-7 px-2.5 rounded-md text-[12px] font-medium border inline-flex items-center gap-1.5 transition-colors",
                        enabled
                            ? "bg-emerald-50 text-emerald-700 border-emerald-200"
                            : "bg-slate-100 text-slate-500 border-slate-200",
                    )}
                >
                    <span className={cn("size-1.5 rounded-full", enabled ? "bg-emerald-500" : "bg-slate-400")} />
                    {enabled ? "Active" : "Off"}
                </button>
                <div className="ml-auto flex items-center gap-1.5">
                    <button
                        type="button"
                        onClick={addAction}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] inline-flex items-center gap-1.5 transition-colors"
                    >
                        <PlusIcon className="w-3.5 h-3.5" />
                        Add action
                    </button>
                    <button
                        type="button"
                        onClick={save}
                        disabled={update.isPending}
                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                    >
                        {update.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CheckIcon className="w-3.5 h-3.5" />}
                        Save
                    </button>
                </div>
            </header>

            {/* Canvas + editor */}
            <div className="flex-1 min-h-0 relative bg-slate-50/40">
                <ReactFlow
                    nodes={nodes}
                    edges={edges}
                    nodeTypes={nodeTypes}
                    onNodeClick={(_, n) => setSelected(n.id)}
                    onPaneClick={() => setSelected(null)}
                    nodesDraggable={false}
                    nodesConnectable={false}
                    fitView
                    fitViewOptions={{ padding: 0.3, maxZoom: 1 }}
                    minZoom={0.4}
                    maxZoom={1.5}
                    proOptions={{ hideAttribution: true }}
                >
                    <Background color="#e9eef5" gap={24} size={1} />
                    <Controls showInteractive={false} />
                    {steps.length === 0 && (
                        <Panel position="bottom-center">
                            <div className="mb-4 text-[11.5px] text-slate-400">
                                Click <span className="font-medium text-slate-600">Add action</span> to choose what happens when this fires.
                            </div>
                        </Panel>
                    )}
                </ReactFlow>

                {selected && (
                    <NodeEditor
                        onClose={() => setSelected(null)}
                        isTrigger={selected === "trigger"}
                        // trigger props
                        trigger={trigger}
                        setTrigger={setTrigger}
                        intents={intents}
                        setIntents={setIntents}
                        minConf={minConf}
                        setMinConf={setMinConf}
                        // step props
                        step={selectedStep}
                        targets={targets}
                        connLabel={connLabel}
                        actionsForProvider={actionsForProvider}
                        providerOf={(id) => connById[id]?.provider}
                        onPickConnection={pickConnection}
                        onPickAction={(key, action) => setStep(key, { action, config: {} })}
                        onSetConfig={setStepConfig}
                        onDeleteStep={removeStep}
                    />
                )}
            </div>
        </div>
    );
}

// --- nodes ------------------------------------------------------------------

function TriggerNode(props: NodeProps) {
    const data = props.data as { label: string; selected: boolean };
    return (
        <div
            className={cn(
                "w-56 rounded-lg border bg-white px-3 py-2.5 shadow-sm cursor-pointer transition-colors",
                data.selected ? "border-sky-500 ring-2 ring-sky-100" : "border-slate-200 hover:border-slate-300",
            )}
        >
            <div className="flex items-center gap-1.5 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                <ZapIcon className="w-3 h-3 text-sky-500" /> When
            </div>
            <div className="text-[13px] font-semibold text-slate-900 mt-1">{data.label}</div>
            <Handle type="source" position={Position.Bottom} className="!bg-slate-300 !border-white" />
        </div>
    );
}

function ActionNode(props: NodeProps) {
    const data = props.data as {
        title: string;
        sub: string;
        provider: string;
        selected: boolean;
        onDelete: () => void;
    };
    return (
        <div
            className={cn(
                "w-56 rounded-lg border bg-white px-3 py-2.5 shadow-sm cursor-pointer transition-colors relative group",
                data.selected ? "border-sky-500 ring-2 ring-sky-100" : "border-slate-200 hover:border-slate-300",
            )}
        >
            <Handle type="target" position={Position.Top} className="!bg-slate-300 !border-white" />
            <div className="flex items-center gap-2 min-w-0">
                {data.provider ? (
                    <ProviderGlyph provider={data.provider} name={data.provider} size={7} />
                ) : (
                    <span className="size-5 rounded bg-slate-100 inline-flex items-center justify-center text-slate-400 text-[10px]">?</span>
                )}
                <div className="min-w-0">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Then</div>
                    <div className="text-[12.5px] font-semibold text-slate-900 truncate">{data.title}</div>
                </div>
            </div>
            <div className="text-[11px] text-slate-400 truncate mt-1">{data.sub}</div>
            <button
                type="button"
                onClick={(e) => {
                    e.stopPropagation();
                    data.onDelete();
                }}
                className="absolute top-1.5 right-1.5 h-5 w-5 rounded inline-flex items-center justify-center text-slate-300 hover:text-red-600 hover:bg-red-50 opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-all"
                aria-label="Remove action"
            >
                <Trash2Icon className="w-3 h-3" />
            </button>
        </div>
    );
}

// --- editor panel -----------------------------------------------------------

function NodeEditor({
    onClose,
    isTrigger,
    trigger,
    setTrigger,
    intents,
    setIntents,
    minConf,
    setMinConf,
    step,
    targets,
    connLabel,
    actionsForProvider,
    providerOf,
    onPickConnection,
    onPickAction,
    onSetConfig,
    onDeleteStep,
}: {
    onClose: () => void;
    isTrigger: boolean;
    trigger: string;
    setTrigger: (v: string) => void;
    intents: string[];
    setIntents: (v: string[]) => void;
    minConf: number;
    setMinConf: (v: number) => void;
    step: StepDraft | null;
    targets: IntegrationConnection[];
    connLabel: (id: string) => string;
    actionsForProvider: (provider?: string) => string[];
    providerOf: (id: string) => string | undefined;
    onPickConnection: (key: string, connId: string) => void;
    onPickAction: (key: string, action: string) => void;
    onSetConfig: (key: string, k: string, v: unknown) => void;
    onDeleteStep: (key: string) => void;
}) {
    const triggerOptions: SelectOption[] = TRIGGER_EVENTS.map((ev) => ({ value: ev, label: triggerLabel(ev) }));
    const toggleIntent = (v: string) =>
        setIntents(intents.includes(v) ? intents.filter((x) => x !== v) : [...intents, v]);

    return (
        <div className="absolute top-0 right-0 h-full w-80 max-w-[88vw] bg-white border-l border-slate-200 shadow-xl flex flex-col z-10">
            <div className="h-11 px-3 flex items-center border-b border-slate-200 shrink-0">
                <span className="text-[12.5px] font-medium text-slate-900">
                    {isTrigger ? "Trigger" : "Action"}
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
                            <SelectMenu value={trigger} onChange={setTrigger} options={triggerOptions} className="w-full" />
                        </div>
                        {triggerSupportsIntentFilter(trigger) && (
                            <>
                                <div>
                                    <Label>Only for these reply types (optional)</Label>
                                    <div className="flex flex-wrap gap-1 mt-1">
                                        {REPLY_INTENT_OPTIONS.map((opt) => {
                                            const on = intents.includes(opt.value);
                                            return (
                                                <button
                                                    key={opt.value}
                                                    type="button"
                                                    onClick={() => toggleIntent(opt.value)}
                                                    className={cn(
                                                        "h-6 px-2 rounded text-[11px] border transition-colors",
                                                        on
                                                            ? "bg-sky-50 text-sky-700 border-sky-200"
                                                            : "bg-white text-slate-500 border-slate-200 hover:border-slate-300",
                                                    )}
                                                >
                                                    {opt.label}
                                                </button>
                                            );
                                        })}
                                    </div>
                                </div>
                                <div>
                                    <Label>Minimum confidence</Label>
                                    <NumberInput
                                        value={Math.round(minConf * 100)}
                                        onChange={(v) => setMinConf(Math.max(0, Math.min(100, v)) / 100)}
                                        min={0}
                                        max={100}
                                        step={5}
                                        suffix="%"
                                    />
                                </div>
                            </>
                        )}
                    </>
                ) : step ? (
                    <StepEditor
                        step={step}
                        targets={targets}
                        connLabel={connLabel}
                        actionsForProvider={actionsForProvider}
                        providerOf={providerOf}
                        onPickConnection={onPickConnection}
                        onPickAction={onPickAction}
                        onSetConfig={onSetConfig}
                        onDeleteStep={onDeleteStep}
                    />
                ) : null}
            </div>
        </div>
    );
}

function StepEditor({
    step,
    targets,
    connLabel,
    actionsForProvider,
    providerOf,
    onPickConnection,
    onPickAction,
    onSetConfig,
    onDeleteStep,
}: {
    step: StepDraft;
    targets: IntegrationConnection[];
    connLabel: (id: string) => string;
    actionsForProvider: (provider?: string) => string[];
    providerOf: (id: string) => string | undefined;
    onPickConnection: (key: string, connId: string) => void;
    onPickAction: (key: string, action: string) => void;
    onSetConfig: (key: string, k: string, v: unknown) => void;
    onDeleteStep: (key: string) => void;
}) {
    const connOptions: SelectOption[] = targets.map((c) => ({ value: c.id, label: connLabel(c.id) }));
    const actionOptions: SelectOption[] = actionsForProvider(providerOf(step.connection_id)).map((a) => ({
        value: a,
        label: actionLabel(a),
    }));

    if (targets.length === 0) {
        return (
            <p className="text-[12px] text-slate-500 leading-relaxed">
                No integrations are connected yet. Connect one under Integrations, then add it here.
            </p>
        );
    }

    return (
        <div className="space-y-3">
            <div>
                <Label>Integration</Label>
                <SelectMenu
                    value={step.connection_id}
                    onChange={(v) => onPickConnection(step.key, v)}
                    options={connOptions}
                    className="w-full"
                />
            </div>
            <div>
                <Label>Action</Label>
                <SelectMenu
                    value={step.action}
                    onChange={(v) => onPickAction(step.key, v)}
                    options={actionOptions}
                    className="w-full"
                />
            </div>

            {actionNeedsChannel(step.action) && (
                <div>
                    <Label>Channel</Label>
                    <TextInput
                        value={String(step.config.channel ?? "")}
                        onChange={(v) => onSetConfig(step.key, "channel", v)}
                        placeholder="#sales"
                    />
                </div>
            )}
            {actionNeedsURL(step.action) && (
                <div>
                    <Label>Webhook URL</Label>
                    <TextInput
                        value={String(step.config.url ?? "")}
                        onChange={(v) => onSetConfig(step.key, "url", v)}
                        placeholder="https://hooks.zapier.com/…"
                    />
                </div>
            )}
            {actionSupportsTemplate(step.action) && (
                <div>
                    <Label>Message (optional)</Label>
                    <TextInput
                        value={String(step.config.message_template ?? "")}
                        onChange={(v) => onSetConfig(step.key, "message_template", v)}
                        placeholder="New reply from {{contact_email}}"
                    />
                </div>
            )}

            <button
                type="button"
                onClick={() => onDeleteStep(step.key)}
                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-red-300 text-slate-600 hover:text-red-600 text-[12px] inline-flex items-center gap-1.5 transition-colors"
            >
                <Trash2Icon className="w-3.5 h-3.5" />
                Remove this action
            </button>
        </div>
    );
}
