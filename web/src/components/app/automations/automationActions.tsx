// Per-action defaults and visuals shared by the automation builder's picker,
// cards and editors.

import React from "react";
import {
    BriefcaseIcon,
    CheckSquareIcon,
    GitBranchIcon,
    MegaphoneIcon,
    MessageSquareIcon,
    SendIcon,
    SparklesIcon,
    TagIcon,
    TagsIcon,
    UserMinusIcon,
    UserPlusIcon,
    WandSparklesIcon,
    ZapIcon,
} from "lucide-react";
import { triggerVariables } from "@/lib/api/models/app/automations/meta";
import { cn } from "@/lib/utils";
import { fmtUsdAbout } from "@/lib/money";
import { AI_ESTIMATE_USD } from "@/lib/aiEstimates";

// Fresh config for a newly-created action node: agent mode for the AI step, two
// starter cases for the AI switch (so its case branches show at once), else empty.
// The event key that carries the person's address differs per trigger, so a
// fresh create-or-update-contact node starts from the one this trigger has.
function defaultEmailTemplate(trigger: string): string {
    const vars = triggerVariables(trigger);
    if (vars.includes("contact_email")) return "{{.contact_email}}";
    if (vars.includes("invitee_email")) return "{{.invitee_email}}";
    return "{{.email}}";
}

export function defaultConfigForAction(action: string, trigger: string): Record<string, unknown> {
    if (action === "warmbly.ai_step") return { mode: "agent" };
    if (action === "warmbly.upsert_contact") return { email: defaultEmailTemplate(trigger), if_exists: "update" };
    if (action === "warmbly.ai_switch") return { switch_on: "ai", cases: ["interested", "not interested"] };
    return {};
}

// One visual per action (native + provider): icon, text tint, and a soft bg for
// the editor header. Drives both the action dropdown glyphs and the editor
// header, so the picker reads like the campaign step picker.
export const ACTION_VISUAL: Record<string, { Icon: typeof TagIcon; tint: string; bg: string; desc?: string }> = {
    "warmbly.add_tag": { Icon: TagIcon, tint: "text-emerald-600", bg: "bg-emerald-50", desc: "Add a label to the contact." },
    "warmbly.remove_tag": { Icon: TagIcon, tint: "text-amber-600", bg: "bg-amber-50", desc: "Remove a label from the contact." },
    "warmbly.create_task": { Icon: CheckSquareIcon, tint: "text-violet-600", bg: "bg-violet-50", desc: "Open a CRM task for the contact." },
    "warmbly.create_deal": { Icon: BriefcaseIcon, tint: "text-sky-600", bg: "bg-sky-50", desc: "Create a CRM deal for the contact." },
    "warmbly.move_deal_stage": { Icon: BriefcaseIcon, tint: "text-sky-600", bg: "bg-sky-50", desc: "Move the contact's open deal to another stage." },
    "warmbly.unsubscribe": { Icon: UserMinusIcon, tint: "text-rose-600", bg: "bg-rose-50", desc: "Unsubscribe the contact from the campaign." },
    "warmbly.run_automation": { Icon: ZapIcon, tint: "text-indigo-600", bg: "bg-indigo-50", desc: "Launch another automation with this event's data." },
    "warmbly.label_email": { Icon: TagsIcon, tint: "text-fuchsia-600", bg: "bg-fuchsia-50", desc: "Label the conversation the contact replied on." },
    "warmbly.set_variables": { Icon: WandSparklesIcon, tint: "text-amber-600", bg: "bg-amber-50", desc: "Compute named values from templates for later steps to reuse." },
    "warmbly.fire_event": { Icon: SendIcon, tint: "text-sky-600", bg: "bg-sky-50", desc: "Publish a custom event to the realtime gateway — your app receives it over the API websocket, no public URL." },
    "warmbly.upsert_contact": { Icon: UserPlusIcon, tint: "text-emerald-600", bg: "bg-emerald-50", desc: "Create a contact from the event's fields, or enrich the one with that email, then tag it and enrol it in a campaign." },
    "warmbly.add_to_campaign": { Icon: MegaphoneIcon, tint: "text-sky-600", bg: "bg-sky-50", desc: "Enrol the event's contact in a campaign. Sending still follows the campaign's mailboxes, caps and spacing." },
    "warmbly.ai_step": { Icon: SparklesIcon, tint: "text-purple-600", bg: "bg-purple-50", desc: "One AI step: an agent that takes reversible actions (tag, task, deal, label…), or a single-shot classify, extract, or generate over the event. Charged to the balance by usage." },
    "warmbly.ai_switch": { Icon: GitBranchIcon, tint: "text-purple-600", bg: "bg-purple-50", desc: `Route the event: AI picks one of your cases, or match a value template. AI mode costs ${fmtUsdAbout(AI_ESTIMATE_USD.aiNode)}; value mode is free.` },
    "slack.notify": { Icon: MessageSquareIcon, tint: "text-violet-600", bg: "bg-violet-50" },
    "discord.notify": { Icon: MessageSquareIcon, tint: "text-indigo-600", bg: "bg-indigo-50" },
    "webhook.ping": { Icon: SendIcon, tint: "text-sky-600", bg: "bg-sky-50" },
    "clay.send_row": { Icon: SendIcon, tint: "text-violet-600", bg: "bg-violet-50", desc: "Add the event as a row in a Clay table." },
    "hubspot.upsert_contact": { Icon: BriefcaseIcon, tint: "text-orange-600", bg: "bg-orange-50" },
    "pipedrive.upsert_person": { Icon: BriefcaseIcon, tint: "text-slate-700", bg: "bg-slate-100" },
    "salesforce.upsert_contact": { Icon: BriefcaseIcon, tint: "text-sky-600", bg: "bg-sky-50" },
    "close.upsert_lead": { Icon: BriefcaseIcon, tint: "text-emerald-600", bg: "bg-emerald-50" },
};

// actionGlyph returns the tinted leading icon for an action's dropdown option.
export function actionGlyph(action: string): React.ReactNode {
    const v = ACTION_VISUAL[action];
    const Icon = v?.Icon ?? ZapIcon;
    return <Icon className={cn("w-3.5 h-3.5", v?.tint ?? "text-slate-400")} />;
}
