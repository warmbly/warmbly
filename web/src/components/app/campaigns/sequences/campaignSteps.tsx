// How each campaign step reads on the canvas, what it still needs before it can
// send, and what the add-step picker offers.

import React from "react";
import { CornerDownRightIcon, GitBranchIcon, LogInIcon, MailIcon, MessageSquareReplyIcon, SparklesIcon, SplitIcon, WorkflowIcon, ZapIcon } from "lucide-react";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import { isLinkedInAction, LINKEDIN_NOTE_MAX } from "@/lib/api/models/app/campaigns/sequences/Action";
import type { StepTone } from "@/components/app/flow/StepCard";
import type { PickerItem } from "@/components/app/flow/StepPicker";
import { ACTION_META, ADD_ACTION_OPTIONS, LINKEDIN_ACTION_OPTIONS, actionSummary, linkedInNoteLength } from "./campaignActions";
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
    if (s.kind === "action") {
        const type = s.action?.type ?? "add_tag";
        const kicker = type === "switch" ? "Switch" : type === "ai_step" ? "AI step" : isLinkedInAction(type) ? "LinkedIn" : "Action";
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
        case "linkedin_invite":
            return linkedInNoteLength(a.note) > LINKEDIN_NOTE_MAX ? `Shorten the note to ${LINKEDIN_NOTE_MAX} characters.` : null;
        case "linkedin_message":
            return a.body?.trim() ? null : "Write the message.";
        case "linkedin_inmail":
            if (!a.subject?.trim()) return "Write a subject.";
            return a.body?.trim() ? null : "Write the InMail.";
        default:
            return null;
    }
}

// The ready-made LinkedIn flow the picker offers at the end of a path.
export const LINKEDIN_FLOW_KEY = "flow:linkedin_invite";

// Picker keys: "email" | "condition" | "reply" | "goto" (children
// "goto:<id>") | "action:<type>" | LINKEDIN_FLOW_KEY.
export function pickerItems({
    atEnd,
    full,
    canCondition,
    gotoCandidates,
    linkedIn = false,
    roomFor = Infinity,
}: {
    atEnd: boolean;
    full: boolean;
    canCondition: boolean;
    gotoCandidates: { id: string; view: StepView }[];
    // LinkedIn is available to the workspace, so its steps are offered.
    linkedIn?: boolean;
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
    if (linkedIn) {
        for (const o of LINKEDIN_ACTION_OPTIONS) {
            items.push({
                key: `action:${o.type}`,
                group: "LinkedIn",
                label: o.label,
                icon: actionIcon(o.type),
                tone: actionTone(o.type),
                description: o.description,
                keywords: o.keywords,
                disabledReason: block,
            });
        }
        if (atEnd) {
            items.push({
                key: LINKEDIN_FLOW_KEY,
                group: "LinkedIn",
                label: "Invite, then message or email",
                icon: <WorkflowIcon />,
                tone: "sky",
                description: "Invite; message them if they accept within 7 days, else follow up by email",
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
