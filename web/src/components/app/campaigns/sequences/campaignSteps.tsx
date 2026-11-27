// How each campaign step reads on the canvas, what it still needs before it can
// send, and what the add-step picker offers.

import React from "react";
import { CornerDownRightIcon, GitBranchIcon, LogInIcon, MailIcon, MessageSquareReplyIcon, MessagesSquareIcon, SparklesIcon, SplitIcon, UserCheckIcon, UserRoundSearchIcon, WorkflowIcon, ZapIcon } from "lucide-react";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import type { BranchCondition } from "@/lib/api/models/app/campaigns/sequences/Branching";
import type { ChannelSpec } from "@/lib/api/models/app/social/Social";
import { socialChannelsQuery } from "@/lib/api/hooks/app/social/useSocial";
import { queryClient } from "@/lib/queryClient";
import { channelLabel } from "@/lib/social/channels";
import type { StepTone } from "@/components/app/flow/StepCard";
import type { PickerItem } from "@/components/app/flow/StepPicker";
import { ACTION_META, ADD_ACTION_OPTIONS, SOCIAL_OPTION, SocialStepIcon, actionSummary } from "./campaignActions";
import { socialIssues, socialStepTitle, socialSummary } from "./socialStep";
import { emailBodyEmpty, isRouterStep } from "./campaignGraph";

export interface StepView {
    icon: React.ReactNode;
    tone: StepTone;
    kicker: string;
    title: string;
    summary: string;
}

const TONES: StepTone[] = ["sky", "amber", "purple", "violet", "emerald", "rose", "indigo", "fuchsia", "orange", "slate"];
const toneOf = (tint?: string): StepTone => (TONES.find((t) => t === /text-([a-z]+)-/.exec(tint ?? "")?.[1]) ?? "slate") as StepTone;

export function actionIcon(type: string): React.ReactNode {
    const Icon = ACTION_META[type]?.Icon ?? ZapIcon;
    return <Icon />;
}
export const actionTone = (type: string) => toneOf(ACTION_META[type]?.tint);

export function entryView(delayLabel: string): StepView {
    return { icon: <LogInIcon />, tone: "violet", kicker: "Start", title: "Contact enters the campaign", summary: delayLabel };
}

export function stepView(s: Sequence, ctx: { index: number; emailNumber: number; threadSubject: string | null }): StepView {
    if (isRouterStep(s)) {
        const n = (s.conditions?.branches ?? []).filter((b) => (b.conditions?.length ?? 0) > 0).length;
        return { icon: <GitBranchIcon />, tone: "amber", kicker: "If / else", title: s.name?.trim() || "Condition", summary: n === 1 ? "1 condition" : `${n} conditions` };
    }
    if (s.kind === "action" && s.action?.type === "social") {
        const cfg = s.action.social;
        return {
            icon: <SocialStepIcon social={cfg} />,
            tone: "sky",
            kicker: cfg ? channelLabel(cfg.channel, catalog()) : "Social",
            title: s.name?.trim() || socialStepTitle(cfg),
            summary: socialSummary(cfg, specFor(cfg?.channel)),
        };
    }
    if (s.kind === "action") {
        const type = s.action?.type ?? "add_tag";
        const kicker = type === "switch" ? "Switch" : type === "ai_step" ? "AI step" : "Action";
        return { icon: actionIcon(type), tone: actionTone(type), kicker, title: s.name?.trim() || ACTION_META[type]?.label || "Action", summary: actionSummary(s.action) };
    }
    const threads = ctx.threadSubject !== null && s.thread_reply;
    const subject = (threads ? ctx.threadSubject || s.subject : s.subject)?.trim();
    return {
        icon: <MailIcon />,
        tone: "sky",
        kicker: ctx.index === 0 ? "First email" : threads ? "Reply in thread" : "Email",
        title: s.name?.trim() || `Email ${ctx.emailNumber}`,
        summary: subject || "No subject yet",
    };
}

// The channel catalog, when it has loaded; the canvas reads it without subscribing.
const catalog = () => queryClient.getQueryData<ChannelSpec[]>(socialChannelsQuery.queryKey);
const specFor = (channel: string | undefined) => catalog()?.find((c) => c.channel === channel);

// The first thing a step is missing, worded for the person fixing it.
export function stepIssue(s: Sequence, threadSubject: string | null): string | null {
    if (isRouterStep(s)) return (s.conditions?.branches ?? []).some((b) => (b.conditions?.length ?? 0) > 0) ? null : "Add a condition to split on.";
    if (s.kind === "email") {
        const threads = threadSubject !== null && s.thread_reply;
        if (!threads && !s.subject?.trim()) return "Write a subject.";
        if (emailBodyEmpty(s.body_html ?? "") && !s.body_plain?.trim()) return "Write the email.";
        return null;
    }
    const a = s.action;
    if (!a) return "Choose what this step does.";
    switch (a.type) {
        case "add_tag":
        case "remove_tag":
            return a.category_id ? null : "Pick a label.";
        case "add_to_segment":
        case "remove_from_segment":
            return a.segment_id ? null : "Pick a segment.";
        case "label_email":
            return a.label_ids?.length ? null : "Pick at least one label.";
        case "create_deal":
        case "move_deal_stage":
            return a.deal_pipeline_id && a.deal_stage_id ? null : "Pick a pipeline and a stage.";
        case "run_automation":
            return a.automation_id ? null : "Pick the automation to run.";
        case "fire_event":
            return a.event_name?.trim() ? null : "Name the event to fire.";
        case "switch":
            if ((a.switch_cases ?? []).filter((c) => c.trim()).length < 2) return "Add at least two cases.";
            if (a.switch_on === "value") return a.switch_value?.trim() ? null : "Set the value to match.";
            return a.ai_instruction?.trim() ? null : "Tell AI how to pick a case.";
        case "ai_step":
            if (!a.ai_instruction?.trim()) return "Tell the agent what to do.";
            return a.ai_allowed_actions?.length ? null : "Allow the agent at least one action.";
        case "social":
            return socialIssues(a.social, specFor(a.social?.channel))[0]?.message ?? null;
        default:
            return null;
    }
}

// The ready-made invite flow the picker offers at the end of a path.
export const SOCIAL_FLOW_KEY = "flow:social_invite";

// Social conditions the picker offers directly; picking one opens a draft path on that field.
export const SOCIAL_CONDITION_PREFIX = "cond:";
export const SOCIAL_CONDITIONS: Record<string, BranchCondition> = {
    social_replied: { field: "social_replied", operator: "within_days", value: 3 },
    social_connected: { field: "social_connected", operator: "within_days", value: 7 },
    social_invite_pending: { field: "social_invite_pending", operator: "ever" },
    social_open_profile: { field: "social_open_profile", operator: "ever" },
};

// Picker keys: "email" | "condition" | "reply" | "goto" (children
// "goto:<id>") | "action:<type>" | "cond:<field>" | SOCIAL_FLOW_KEY.
export function pickerItems({
    atEnd,
    full,
    canCondition,
    gotoCandidates,
    social = false,
    roomFor = Infinity,
}: {
    atEnd: boolean;
    full: boolean;
    canCondition: boolean;
    gotoCandidates: { id: string; view: StepView }[];
    // Social accounts are available to the workspace, so social steps are offered.
    social?: boolean;
    // Steps the campaign can still take, for items that add several.
    roomFor?: number;
}): PickerItem[] {
    const block = full ? "This campaign has the most steps it can hold" : undefined;
    const items: PickerItem[] = [
        {
            key: "email",
            group: "Steps",
            label: "Email",
            icon: <MailIcon />,
            tone: "sky",
            description: "Send an email, or reply in the conversation",
            keywords: "send message follow up",
            disabledReason: block,
        },
        {
            key: "condition",
            group: "Logic",
            label: "If / else",
            icon: <GitBranchIcon />,
            tone: "amber",
            description: "Split on opens, clicks, replies, reply intent or a random share",
            keywords: "condition branch split opened clicked replied random",
            disabledReason: canCondition ? undefined : block,
        },
        {
            key: "reply",
            group: "Logic",
            label: "If they reply",
            icon: <MessageSquareReplyIcon />,
            tone: "violet",
            description: "A path that runs the moment a positive reply lands",
            keywords: "reply positive answer respond",
            disabledReason: canCondition ? undefined : block,
        },
        {
            key: "action:switch",
            group: "Logic",
            label: "Switch",
            icon: <SplitIcon />,
            tone: "purple",
            description: "Route to one of several cases, picked by AI or by a value",
            keywords: "router route cases ai",
            disabledReason: block,
        },
    ];
    if (atEnd) {
        items.push({
            key: "goto",
            group: "Logic",
            label: "Go to a step",
            icon: <CornerDownRightIcon />,
            tone: "sky",
            description: "Continue at a step that is already in the flow",
            keywords: "jump merge join",
            childrenTitle: "Go to",
            childrenEmpty: "There is no other step this path can continue at without looping.",
            children: gotoCandidates.map(({ id, view }) => ({ key: `goto:${id}`, group: "Steps", label: view.title, description: view.kicker, icon: view.icon, tone: view.tone })),
        });
    }
    items.push({
        key: "action:ai_step",
        group: "AI",
        label: "AI step",
        icon: <SparklesIcon />,
        tone: "purple",
        description: "An agent that labels, opens tasks and moves deals for this contact",
        keywords: "agent llm",
        disabledReason: block,
    });
    if (social) {
        items.push({
            key: `action:${SOCIAL_OPTION.type}`,
            group: "Social",
            label: SOCIAL_OPTION.label,
            icon: <MessagesSquareIcon />,
            tone: "sky",
            description: SOCIAL_OPTION.description,
            keywords: SOCIAL_OPTION.keywords,
            disabledReason: block,
        });
        const condBlock = canCondition ? undefined : block;
        items.push(
            {
                key: `${SOCIAL_CONDITION_PREFIX}social_replied`,
                group: "Social",
                label: "If replied on social",
                icon: <MessageSquareReplyIcon />,
                tone: "amber",
                description: "A path for contacts who answered on any channel, or on one",
                keywords: "condition branch reply answered linkedin instagram whatsapp telegram facebook",
                disabledReason: condBlock,
            },
            {
                key: `${SOCIAL_CONDITION_PREFIX}social_connected`,
                group: "Social",
                label: "If accepted the LinkedIn invite",
                icon: <UserCheckIcon />,
                tone: "amber",
                description: "A path for contacts who accepted the connection request",
                keywords: "condition branch linkedin connected accept invite connection",
                disabledReason: condBlock,
            },
            {
                key: `${SOCIAL_CONDITION_PREFIX}social_invite_pending`,
                group: "Social",
                label: "If the invite is still pending",
                icon: <GitBranchIcon />,
                tone: "amber",
                description: "A path for contacts who have not answered the LinkedIn invite yet",
                keywords: "condition branch linkedin pending invite waiting withdraw",
                disabledReason: condBlock,
            },
            {
                key: `${SOCIAL_CONDITION_PREFIX}social_open_profile`,
                group: "Social",
                label: "If Open Profile",
                icon: <UserRoundSearchIcon />,
                tone: "amber",
                description: "A path for contacts any LinkedIn member can InMail for free",
                keywords: "condition branch linkedin open profile inmail premium sales navigator",
                disabledReason: condBlock,
            },
        );
        if (atEnd) {
            items.push({
                key: SOCIAL_FLOW_KEY,
                group: "Social",
                label: "Invite, then message or email",
                icon: <WorkflowIcon />,
                tone: "sky",
                description: "LinkedIn invite; message them if they accept within 7 days, else follow up by email",
                keywords: "linkedin template snippet flow accept invite message email follow up",
                disabledReason: block ?? (roomFor < 4 ? "There is no room left for the four steps this adds" : undefined),
            });
        }
    }
    for (const o of ADD_ACTION_OPTIONS) {
        items.push({ key: `action:${o.type}`, group: "Actions", label: o.label, icon: actionIcon(o.type), tone: actionTone(o.type), disabledReason: block });
    }
    return items;
}
