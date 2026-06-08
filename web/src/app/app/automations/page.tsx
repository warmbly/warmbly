// Automations — the central place to wire "when X fires → do these things"
// across every integration, built as a visual flow (the builder lives at
// /app/automations/:id). This page lists the automations as cards.

import React from "react";
import { motion } from "framer-motion";
import { PlusIcon, Trash2Icon, ZapIcon, Loader2Icon } from "lucide-react";
import { useNavigate } from "react-router-dom";
import toast from "react-hot-toast";
import {
    EmptyBlock,
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
    TopbarAction,
} from "@/components/layout/Page";
import { useConfirm } from "@/hooks/context/confirm";
import { useAutomations } from "@/lib/api/hooks/app/automations/useAutomations";
import {
    useCreateAutomation,
    useDeleteAutomation,
    useUpdateAutomation,
} from "@/lib/api/hooks/app/automations/useAutomationMutations";
import useIntegrationConnections from "@/lib/api/hooks/app/integrations/useIntegrationConnections";
import type { Automation, AutomationWrite } from "@/lib/api/models/app/automations/Automation";
import { triggerLabel } from "@/lib/api/models/app/automations/meta";
import { AUTOMATION_TEMPLATES, type AutomationTemplate } from "@/lib/api/models/app/automations/templates";
import { cn } from "@/lib/utils";
import ProviderGlyph from "@/app/app/integrations/_components/ProviderGlyph";

function toWrite(a: Automation): AutomationWrite {
    return {
        name: a.name,
        enabled: a.enabled,
        trigger_event: a.trigger_event,
        filter: a.filter,
        graph: a.graph,
    };
}

// Count the action nodes in a graph (the "steps" of the flow).
function actionCount(a: Automation): number {
    return (a.graph?.nodes ?? []).filter((n) => n.type === "action").length;
}

export default function AutomationsPage() {
    const navigate = useNavigate();
    const { data, isLoading } = useAutomations();
    const create = useCreateAutomation();
    const automations = data?.automations ?? [];

    const enabledCount = automations.filter((a) => a.enabled).length;
    const stepCount = automations.reduce((n, a) => n + actionCount(a), 0);

    const newAutomation = () => {
        create.mutate(
            {
                name: "New automation",
                enabled: false,
                trigger_event: "meeting.booked",
                graph: { nodes: [{ id: "trigger", type: "trigger", x: 0, y: 0 }], edges: [] },
            },
            {
                onSuccess: (res) => navigate(`/app/automations/${res.automation.id}`),
                onError: () => toast.error("Could not create automation"),
            },
        );
    };

    const newFromTemplate = (t: AutomationTemplate) => {
        create.mutate(
            { name: t.name, enabled: false, trigger_event: t.trigger_event, graph: t.graph },
            {
                onSuccess: (res) => navigate(`/app/automations/${res.automation.id}`),
                onError: () => toast.error("Could not create automation"),
            },
        );
    };

    return (
        <Page>
            <PageTopbar eyebrow="Automations" subtitle="When something happens in Warmbly, do this across your integrations">
                <TopbarAction
                    onClick={newAutomation}
                    icon={create.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <PlusIcon className="w-3.5 h-3.5" />}
                >
                    New automation
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={3}>
                <Stat label="Automations" value={automations.length} accent={enabledCount > 0} />
                <Stat label="Active" value={enabledCount} sub={`${automations.length - enabledCount} off`} />
                <Stat label="Action steps" value={stepCount} last />
            </StatStrip>

            <PageBody>
                <SectionBar label="Your automations" count={automations.length || undefined} />
                {isLoading ? (
                    <div className="px-5 py-16 flex justify-center">
                        <Loader2Icon className="w-5 h-5 text-slate-300 animate-spin" />
                    </div>
                ) : automations.length === 0 ? (
                    <EmptyBlock
                        title="No automations yet"
                        body="Build a flow: pick a trigger like “meeting booked”, then connect what should happen — ping Slack, create a deal, push to your CRM, or fire a webhook."
                        cta={
                            <TopbarAction onClick={newAutomation} icon={<PlusIcon className="w-3.5 h-3.5" />}>
                                New automation
                            </TopbarAction>
                        }
                    />
                ) : (
                    <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-px bg-slate-200/60 border-b border-slate-200/60">
                        {automations.map((a, i) => (
                            <AutomationCard key={a.id} index={i} automation={a} onOpen={() => navigate(`/app/automations/${a.id}`)} />
                        ))}
                    </div>
                )}

                <div className="mt-8">
                    <SectionBar label="Start from a template" />
                    <TemplateGallery onPick={newFromTemplate} busy={create.isPending} />
                </div>
            </PageBody>
        </Page>
    );
}

// TemplateGallery — one-click prebuilt automations (start points the user tweaks).
function TemplateGallery({ onPick, busy }: { onPick: (t: AutomationTemplate) => void; busy: boolean }) {
    return (
        <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-px bg-slate-200/60 border-b border-slate-200/60">
            {AUTOMATION_TEMPLATES.map((t) => (
                <button
                    key={t.id}
                    type="button"
                    disabled={busy}
                    onClick={() => onPick(t)}
                    className="text-left bg-white p-4 hover:bg-slate-50/60 transition-colors disabled:opacity-60"
                >
                    <div className="flex items-center gap-2">
                        <span className="inline-flex size-6 shrink-0 items-center justify-center rounded-md bg-indigo-50 text-indigo-600 ring-1 ring-indigo-200/60">
                            <ZapIcon className="w-3.5 h-3.5" />
                        </span>
                        <span className="text-[12.5px] font-medium text-slate-800">{t.name}</span>
                    </div>
                    <p className="mt-1.5 text-[11.5px] text-slate-500 leading-relaxed">{t.description}</p>
                    <div className="mt-2 text-[10px] uppercase tracking-[0.14em] text-slate-400">{triggerLabel(t.trigger_event)}</div>
                </button>
            ))}
        </div>
    );
}

function AutomationCard({
    automation,
    onOpen,
    index,
}: {
    automation: Automation;
    onOpen: () => void;
    index: number;
}) {
    const confirm = useConfirm();
    const del = useDeleteAutomation();
    const update = useUpdateAutomation();
    const { data: connData } = useIntegrationConnections();
    const connById = React.useMemo(() => {
        const m: Record<string, string> = {};
        for (const c of connData?.connections ?? []) m[c.id] = c.provider;
        return m;
    }, [connData]);

    const a = automation;
    const steps = actionCount(a);
    const providers = Array.from(
        new Set(
            (a.graph?.nodes ?? [])
                .filter((n) => n.type === "action" && n.connection_id)
                .map((n) => connById[n.connection_id as string])
                .filter(Boolean),
        ),
    );

    const toggle = (e: React.MouseEvent) => {
        e.stopPropagation();
        update.mutate({ id: a.id, w: { ...toWrite(a), enabled: !a.enabled } });
    };

    const remove = (e: React.MouseEvent) => {
        e.stopPropagation();
        confirm.show(`Delete "${a.name}"? Its steps will stop running.`, async () => {
            await del.mutateAsync(a.id);
            toast.success("Automation deleted");
        });
    };

    return (
        <motion.button
            type="button"
            onClick={onOpen}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: [0.16, 1, 0.3, 1], delay: Math.min(index, 12) * 0.03 }}
            className="text-left bg-white p-5 flex flex-col min-h-[140px] hover:bg-slate-50/60 transition-colors group"
        >
            <div className="flex items-start justify-between gap-3">
                <div className="flex items-center gap-2 min-w-0">
                    <span className="size-7 rounded-md bg-sky-50 text-sky-600 inline-flex items-center justify-center shrink-0">
                        <ZapIcon className="w-3.5 h-3.5" />
                    </span>
                    <div className="text-[13px] font-semibold text-slate-900 truncate">{a.name}</div>
                </div>
                <span
                    className={cn(
                        "inline-flex items-center h-5 px-1.5 rounded text-[10px] font-medium border shrink-0",
                        a.enabled
                            ? "bg-emerald-50 text-emerald-700 border-emerald-200"
                            : "bg-slate-100 text-slate-500 border-slate-200",
                    )}
                >
                    {a.enabled ? "Active" : "Off"}
                </span>
            </div>

            <div className="mt-3 text-[12px] text-slate-600">
                <span className="text-slate-400">When </span>
                <span className="font-medium text-slate-800">{triggerLabel(a.trigger_event)}</span>
            </div>

            <div className="mt-2 flex items-center gap-2">
                <span className="text-[11px] text-slate-400">
                    {steps} action{steps === 1 ? "" : "s"}
                </span>
                <div className="flex -space-x-1">
                    {providers.slice(0, 5).map((p) => (
                        <span key={p} className="ring-2 ring-white rounded-md">
                            <ProviderGlyph provider={p} name={p} size={7} />
                        </span>
                    ))}
                </div>
            </div>

            <div className="mt-auto pt-3 flex items-center justify-between gap-2">
                <span
                    role="button"
                    tabIndex={-1}
                    onClick={toggle}
                    className="text-[11px] text-slate-500 hover:text-slate-900 underline decoration-dotted underline-offset-2"
                >
                    {a.enabled ? "Turn off" : "Turn on"}
                </span>
                <span
                    role="button"
                    tabIndex={-1}
                    onClick={remove}
                    className="opacity-100 md:opacity-0 md:group-hover:opacity-100 h-6 w-6 rounded inline-flex items-center justify-center text-slate-400 hover:text-red-600 hover:bg-red-50 transition-all"
                    title="Delete automation"
                >
                    <Trash2Icon className="w-3.5 h-3.5" />
                </span>
            </div>
        </motion.button>
    );
}
