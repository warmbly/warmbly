// Per-action defaults, labels and one-line summaries shared by the campaign
// step builder's cards, picker and editors.

import type React from "react";
import {
    ArrowRightLeftIcon,
    BellOffIcon,
    AwardIcon,
    CheckSquareIcon,
    EyeIcon,
    HandshakeIcon,
    HeartIcon,
    LayersIcon,
    MailIcon,
    MessageCircleIcon,
    MessageSquareIcon,
    MessagesSquareIcon,
    MicIcon,
    SendIcon,
    SparklesIcon,
    SplitIcon,
    TagIcon,
    TagsIcon,
    UserCheckIcon,
    UserMinusIcon,
    UserPlusIcon,
    ZapIcon,
    type LucideIcon,
} from "lucide-react";
import ChannelMark from "@/components/app/social/ChannelMark";
import type { SelectOption } from "@/components/ui/select-menu";
import type { SequenceAction, SequenceActionType, SocialStepConfig } from "@/lib/api/models/app/campaigns/sequences/Action";
import type { SocialChannel } from "@/lib/api/models/app/social/Social";
import { cn } from "@/lib/utils";
import { defaultSocialConfig, socialSummary } from "./socialStep";

type ActionIcon = React.ComponentType<{ className?: string }>;

const SOCIAL_ACTION_ICON: Record<string, LucideIcon> = {
    invite: UserPlusIcon,
    message: MessageSquareIcon,
    inmail: MailIcon,
    voice_note: MicIcon,
    profile_visit: EyeIcon,
    follow: UserCheckIcon,
    like_post: HeartIcon,
    comment_post: MessageCircleIcon,
    endorse: AwardIcon,
    withdraw_invite: UserMinusIcon,
};

// A social step's icon: what it does, with its channel's mark in the corner so the channel reads at a glance.
export function SocialStepIcon({ social, className }: { social?: SocialStepConfig | null; className?: string }) {
    const Base = (social && SOCIAL_ACTION_ICON[social.action]) || MessagesSquareIcon;
    return (
        <span className={cn("relative inline-flex", className)}>
            <Base className="size-full" />
            {social?.channel && <ChannelMark channel={social.channel} className="absolute -bottom-1 -right-1 !size-[9px] rounded-[2px] bg-white" />}
        </span>
    );
}

// Personalization tokens a deal name can use, the same ones campaign copy offers.
export const DEAL_NAME_VARIABLES = ["{{.FirstName}}", "{{.LastName}}", "{{.Company}}", "{{.Email}}"];

// Per-type chrome for action nodes (icon + label + accent).
export const ACTION_META: Record<string, { label: string; Icon: ActionIcon; tint: string }> = {
    add_tag: { label: "Add label", Icon: TagIcon, tint: "text-emerald-600" },
    remove_tag: { label: "Remove label", Icon: TagIcon, tint: "text-amber-600" },
    add_to_segment: { label: "Add to segment", Icon: LayersIcon, tint: "text-emerald-600" },
    remove_from_segment: { label: "Remove from segment", Icon: LayersIcon, tint: "text-amber-600" },
    label_email: { label: "Label conversation", Icon: TagsIcon, tint: "text-fuchsia-600" },
    create_task: { label: "Create task", Icon: CheckSquareIcon, tint: "text-violet-600" },
    create_deal: { label: "Create deal", Icon: HandshakeIcon, tint: "text-emerald-600" },
    move_deal_stage: { label: "Move deal stage", Icon: ArrowRightLeftIcon, tint: "text-sky-600" },
    unsubscribe: { label: "Unsubscribe", Icon: BellOffIcon, tint: "text-rose-600" },
    run_automation: { label: "Run automation", Icon: ZapIcon, tint: "text-indigo-600" },
    fire_event: { label: "Fire event", Icon: SendIcon, tint: "text-sky-600" },
    switch: { label: "Switch", Icon: SplitIcon, tint: "text-purple-600" },
    ai_step: { label: "AI step", Icon: SparklesIcon, tint: "text-purple-600" },
    social: { label: "Social step", Icon: MessagesSquareIcon, tint: "text-sky-600" },
};

// The one picker item for every social channel and action.
export const SOCIAL_OPTION = {
    type: "social" as SequenceActionType,
    label: "Social",
    description: "LinkedIn, Instagram, WhatsApp, Telegram or Facebook: messages, invites, voice notes, follows, likes, comments…",
    keywords:
        "social linkedin instagram insta ig whatsapp wa telegram tg facebook fb messenger dm direct message chat invite connect connection request note inmail sales navigator recruiter voice note audio recording visit view profile follow like react reaction post comment endorse skills withdraw cancel",
};

// actionSummary is the one-line subtitle shown on an action node.
export function actionSummary(a?: SequenceAction | null): string {
    if (!a) return "Not configured";
    switch (a.type) {
        case "add_tag":
            return a.category_id ? "Add a label" : "Pick a label…";
        case "remove_tag":
            return a.category_id ? "Remove a label" : "Pick a label…";
        case "add_to_segment":
            return a.segment_id ? "Pin into a segment" : "Pick a segment…";
        case "remove_from_segment":
            return a.segment_id ? "Pin out of a segment" : "Pick a segment…";
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
        case "fire_event":
            return a.event_name ? `Fire "${a.event_name}"` : "Name the event…";
        case "switch": {
            if (a.switch_on === "value") {
                const v = a.switch_value?.trim();
                return v ? `Match ${v}` : "Pick a value to match…";
            }
            const t = a.ai_instruction?.trim();
            if (!t) return "Tell AI how to route…";
            return t.length > 64 ? `${t.slice(0, 61)}…` : t;
        }
        case "ai_step": {
            const n = a.ai_allowed_actions?.length ?? 0;
            if (!a.ai_instruction?.trim()) return "Tell the agent what to do…";
            if (n === 0) return "Pick actions the agent may take…";
            return `Agent · ${n} action${n === 1 ? "" : "s"}`;
        }
        case "social":
            return socialSummary(a.social);
        default:
            return "Action";
    }
}

// ── Stop-on-reply toggle ────────────────────────────────────────────────────
export const STOP_ON_REPLY_HELP =
    "When a contact replies, the rest of the sequence stops for them, " +
    "while the reply branch you connected to the email they answered still " +
    "runs (it fires instantly). So on reply: that reply flow runs, every other " +
    "remaining step is cancelled. Auto-replies and out-of-office messages don't " +
    "count as a reply, so the sequence keeps going.";

// ── Connection editor (optional condition + wait behind a connection) ───────
export const BRANCH_PATH_OPTIONS: SelectOption[] = [
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
    { value: "reply_intent", label: "if reply intent is…", group: "Reply intent" },
    { value: "social_replied", label: "if replied on social", group: "Social" },
    { value: "not_social_replied", label: "if didn't reply on social", group: "Social" },
    { value: "social_connected", label: "if accepted the LinkedIn invite", group: "Social" },
    { value: "not_social_connected", label: "if didn't accept the LinkedIn invite", group: "Social" },
    { value: "social_invite_pending", label: "if the LinkedIn invite is still pending", group: "Social" },
    { value: "social_open_profile", label: "if they have an Open Profile", group: "Social" },
    { value: "not_social_open_profile", label: "if they have no Open Profile", group: "Social" },
    { value: "random", label: "random split" },
];

// The action types the step picker offers. A path simply ends where nothing
// follows it, so there is no "end" action.
export const ADD_ACTION_OPTIONS: { type: SequenceActionType; label: string }[] = [
    { type: "add_tag", label: "Add label" },
    { type: "remove_tag", label: "Remove label" },
    { type: "add_to_segment", label: "Add to segment" },
    { type: "remove_from_segment", label: "Remove from segment" },
    { type: "label_email", label: "Label conversation" },
    { type: "create_task", label: "Create task" },
    { type: "create_deal", label: "Create deal" },
    { type: "move_deal_stage", label: "Move deal stage" },
    { type: "unsubscribe", label: "Unsubscribe" },
    { type: "run_automation", label: "Run automation" },
    { type: "fire_event", label: "Fire event" },
];

// Switch is a router like Condition, not a side effect — the menus list it in
// the routing group, so it stays out of ADD_ACTION_OPTIONS.
export const SWITCH_OPTION = { type: "switch" as SequenceActionType, label: "Switch (AI / value)" };
export const AI_STEP_OPTION = { type: "ai_step" as SequenceActionType, label: "AI step (agent)" };

// defaultActionFor returns a fresh action config for a newly-picked type.
export function defaultActionFor(type: SequenceActionType, social?: { channel: SocialChannel; action: string }): SequenceAction {
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
    if (type === "fire_event") {
        return { type, event_fields: [] };
    }
    if (type === "label_email") {
        return { type, label_ids: [] };
    }
    if (type === "switch") {
        // Two starter cases so the step shows its case columns at once.
        return { type, switch_on: "ai", switch_cases: ["interested", "not interested"] };
    }
    if (type === "ai_step") {
        return { type, ai_instruction: "", ai_allowed_actions: [] };
    }
    if (type === "social") {
        return { type, social: defaultSocialConfig(social?.channel ?? "linkedin", social?.action ?? "message") };
    }
    return { type };
}
