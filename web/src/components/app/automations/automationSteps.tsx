// How each automation step reads on the canvas (icon, kicker, title, one-line
// summary) and what the add-step picker offers.

import React from "react";
import { FlagIcon, GitBranchIcon, CornerDownRightIcon, SparklesIcon, ZapIcon, PlugIcon } from "lucide-react";
import type { AutomationNode } from "@/lib/api/models/app/automations/Automation";
import {
    actionLabel,
    conditionLabel,
    isAIAction,
    isNativeAction,
    NATIVE_ACTIONS,
    triggerIsInboundWebhook,
    triggerLabel,
} from "@/lib/api/models/app/automations/meta";
import { PROVIDER_LABELS, type IntegrationConnection } from "@/lib/api/models/app/integrations/Integration";
import ProviderGlyph from "@/app/app/integrations/_components/ProviderGlyph";
import type { StepTone } from "@/components/app/flow/StepCard";
import type { PickerItem } from "@/components/app/flow/StepPicker";
import { ACTION_VISUAL } from "./automationActions";
import { isSwitch, switchCases } from "./automationGraph";
import { fmtUsdAbout } from "@/lib/money";
import { AI_ESTIMATE_USD } from "@/lib/aiEstimates";

export interface StepView {
    icon: React.ReactNode;
    tile?: React.ReactNode;
    tone: StepTone;
    kicker: string;
    title: string;
    summary: string;
}

export interface StepContext {
    trigger: string;
    connLabel: (id?: string) => string;
    providerOf: (id?: string) => string;
    automationName: (id: string) => string | undefined;
}

const TONES: StepTone[] = ["sky", "amber", "purple", "violet", "emerald", "rose", "indigo", "fuchsia", "orange", "slate"];

function actionTone(action: string): StepTone {
    const m = /text-([a-z]+)-/.exec(ACTION_VISUAL[action]?.tint ?? "");
    return (TONES.find((t) => t === m?.[1]) ?? "slate") as StepTone;
}

export function actionIcon(action: string): React.ReactNode {
    const Icon = ACTION_VISUAL[action]?.Icon ?? ZapIcon;
    return <Icon />;
}

const clip = (s: string, n = 48) => (s.length > n ? `${s.slice(0, n - 1)}…` : s);
const count = (v: unknown) => (Array.isArray(v) ? v.filter((x) => String(x ?? "").trim()).length : 0);
const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

const AI_MODES: Record<string, string> = { agent: "Agent", classify: "Classify", extract: "Extract", generate: "Generate" };

function nativeSummary(action: string, cfg: Record<string, unknown>, ctx: StepContext): string {
    const str = (k: string) => String(cfg[k] ?? "").trim();
    switch (action) {
        case "warmbly.add_tag":
        case "warmbly.remove_tag": {
            const n = Math.max(count(cfg.category_ids), str("category_id") ? 1 : 0);
            return n ? plural(n, "label") : "No label picked";
        }
        case "warmbly.label_email":
            return count(cfg.label_ids) ? plural(count(cfg.label_ids), "label") : "No label picked";
        case "warmbly.create_task":
            return str("task_title") || "New task for the contact";
        case "warmbly.create_deal":
            return str("deal_name") || "New deal for the contact";
        case "warmbly.move_deal_stage":
            return "Moves the contact's open deal";
        case "warmbly.unsubscribe":
            return "Stops all campaign mail to the contact";
        case "warmbly.run_automation":
            return str("automation_id") ? (ctx.automationName(str("automation_id")) ?? "Another automation") : "No automation picked";
        case "warmbly.set_variables": {
            const n = Array.isArray(cfg.set_vars) ? (cfg.set_vars as { key?: string }[]).filter((v) => String(v?.key ?? "").trim()).length : 0;
            return n ? plural(n, "variable") : "No variables yet";
        }
        case "warmbly.fire_event":
            return str("event_name") || "No event name";
        case "warmbly.upsert_contact":
            return str("email") || "No email set";
        case "warmbly.add_to_campaign":
            return str("campaign_name") || (str("campaign_id") ? "A campaign" : "No campaign picked");
        default:
            return "Built-in action";
    }
}

export function stepView(n: AutomationNode, ctx: StepContext): StepView {
    if (n.type === "trigger") {
        return {
            icon: <ZapIcon />,
            tone: "sky",
            kicker: "Trigger",
            title: triggerLabel(ctx.trigger),
            summary: triggerIsInboundWebhook(ctx.trigger) ? "Runs when your webhook URL is called" : "Starts this automation",
        };
    }
    if (n.type === "condition") {
        const c = n.condition;
        const kind = c?.field === "ai" ? `Ask AI · ${fmtUsdAbout(AI_ESTIMATE_USD.aiNode)}` : c?.field === "random" ? "Random split" : c?.field === "expression" ? "Expression" : "Checks the event";
        return { icon: <GitBranchIcon />, tone: "amber", kicker: "If / else", title: conditionLabel(c), summary: kind };
    }
    if (n.type === "stop") return { icon: <FlagIcon />, tone: "rose", kicker: "End", title: "End", summary: "" };
    const action = String(n.action ?? "");
    const cfg = n.config ?? {};
    const instruction = String(cfg.instruction ?? "").trim();
    if (!action) return { icon: <ZapIcon />, tone: "slate", kicker: "Action", title: "Choose an action", summary: "Not set up yet" };
    if (isSwitch(n)) {
        const value = String(cfg.switch_on ?? "ai") === "value";
        const cases = switchCases(n).length;
        return {
            icon: <GitBranchIcon />,
            tone: "purple",
            kicker: value ? "Value switch" : "AI switch",
            title: value ? `Match ${clip(String(cfg.switch_value ?? "").trim() || "a value", 32)}` : clip(instruction) || "New AI switch",
            summary: `${plural(cases, "case")}${value ? "" : ` · ${fmtUsdAbout(AI_ESTIMATE_USD.aiNode)}`}`,
        };
    }
    if (isAIAction(action)) {
        const mode = AI_MODES[String(cfg.mode ?? "agent")] ?? "Agent";
        return { icon: <SparklesIcon />, tone: "purple", kicker: "AI step", title: clip(instruction) || "New AI step", summary: `${mode} · charged by usage` };
    }
    if (isNativeAction(action)) {
        return { icon: actionIcon(action), tone: actionTone(action), kicker: "Action", title: actionLabel(action), summary: nativeSummary(action, cfg, ctx) };
    }
    const provider = ctx.providerOf(n.connection_id);
    const channel = String(cfg.channel ?? "").trim();
    return {
        icon: actionIcon(action),
        tile: provider ? <ProviderGlyph provider={provider} name={provider} size={7} /> : undefined,
        tone: actionTone(action),
        kicker: PROVIDER_LABELS[provider as keyof typeof PROVIDER_LABELS] ?? "Integration",
        title: actionLabel(action),
        summary: n.connection_id ? [ctx.connLabel(n.connection_id), channel].filter(Boolean).join(" · ") : "No integration picked",
    };
}

// Picker keys: "condition" | "stop" | "goto" (children "goto:<id>") |
// "native:<action>" | "int:<connectionId>:<action>".
export function pickerItems({
    atEnd,
    targets,
    connLabel,
    actionsForProvider,
    gotoCandidates,
}: {
    atEnd: boolean;
    targets: IntegrationConnection[];
    connLabel: (id?: string) => string;
    actionsForProvider: (provider?: string) => string[];
    gotoCandidates: { id: string; view: StepView }[];
}): PickerItem[] {
    const items: PickerItem[] = [
        {
            key: "condition",
            group: "Logic",
            label: "If / else",
            icon: <GitBranchIcon />,
            tone: "amber",
            description: "Split into Yes and No on the event, a random share, or an AI question",
            keywords: "condition branch filter split random",
        },
        {
            key: "native:warmbly.ai_switch",
            group: "Logic",
            label: "AI switch",
            icon: <GitBranchIcon />,
            tone: "purple",
            description: "Route to one of several cases, picked by AI or by a value",
            keywords: "router route cases classify",
        },
    ];
    if (atEnd) {
        items.push(
            {
                key: "goto",
                group: "Logic",
                label: "Go to a step",
                icon: <CornerDownRightIcon />,
                tone: "sky",
                description: "Continue at a step that is already in the flow",
                keywords: "jump merge join",
                childrenTitle: "Go to",
                childrenEmpty: "There is no other step this path can continue at without looping.",
                children: gotoCandidates.map(({ id, view }) => ({
                    key: `goto:${id}`,
                    group: "Steps",
                    label: view.title,
                    description: view.kicker,
                    icon: view.icon,
                    tile: view.tile,
                    tone: view.tone,
                })),
            },
            {
                key: "stop",
                group: "Logic",
                label: "End here",
                icon: <FlagIcon />,
                tone: "rose",
                description: "Nothing after this runs on this path",
                keywords: "stop finish end",
            },
        );
    }
    items.push({
        key: "native:warmbly.ai_step",
        group: "AI",
        label: "AI step",
        icon: <SparklesIcon />,
        tone: "purple",
        description: "An agent that labels, tasks and deals, or a classify, extract or generate step",
        keywords: "agent classify extract generate llm",
    });
    for (const a of NATIVE_ACTIONS) {
        if (isAIAction(a)) continue;
        items.push({
            key: `native:${a}`,
            group: "Warmbly",
            label: actionLabel(a),
            icon: actionIcon(a),
            tone: actionTone(a),
            description: ACTION_VISUAL[a]?.desc,
        });
    }
    if (targets.length === 0) {
        items.push({
            key: "int:none",
            group: "Integrations",
            label: "Integration action",
            icon: <PlugIcon />,
            tone: "slate",
            disabledReason: "Connect Slack, a CRM or a webhook in Integrations first",
        });
    }
    for (const c of targets) {
        for (const a of actionsForProvider(c.provider)) {
            items.push({
                key: `int:${c.id}:${a}`,
                group: "Integrations",
                label: actionLabel(a),
                icon: actionIcon(a),
                tile: <ProviderGlyph provider={c.provider} name={c.provider} size={7} />,
                tone: actionTone(a),
                description: connLabel(c.id),
                keywords: c.provider,
            });
        }
    }
    return items;
}
