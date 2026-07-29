// Shared definitions for the StateLegend component. One source of truth for
// what each enum state actually means, so every page explains the same term
// the same way. Tones mirror the badge tone maps used by the tables.

import type { LegendEntry } from "@/components/StateLegend";

// Shared workers are bucketed by acceptable mailbox risk so one bad tenant
// can't burn the IP reputation healthy senders depend on.
export const WORKER_RISK_POOL_LEGEND: LegendEntry[] = [
    {
        term: "clean",
        tone: "border-emerald-300 bg-emerald-50 text-emerald-700",
        description:
            "Carries only mailboxes with healthy reputation. Protect this pool's IPs first.",
    },
    {
        term: "risky",
        tone: "border-amber-300 bg-amber-50 text-amber-700",
        description:
            "Accepts unproven or recovering mailboxes. Expect noisier deliverability here.",
    },
    {
        term: "quarantine",
        tone: "border-red-300 bg-red-50 text-red-700",
        description:
            "Isolation pool after abuse or deliverability incidents. Never place healthy traffic here.",
    },
];

// Worker health is the rolled-up label maintained by the assignment loop.
// It gates whether a worker can accept new mailboxes; it mirrors the mailbox
// health vocabulary but applies to the whole machine.
export const WORKER_HEALTH_LEGEND: LegendEntry[] = [
    {
        term: "healthy",
        tone: "border-emerald-300 bg-emerald-50 text-emerald-700",
        description: "Accepting new mailbox assignments normally.",
    },
    {
        term: "watch",
        tone: "border-amber-300 bg-amber-50 text-amber-700",
        description: "Deliverability signals trending down. Placement is deprioritized.",
    },
    {
        term: "throttled",
        tone: "border-orange-300 bg-orange-50 text-orange-700",
        description: "Receives fewer new assignments while its mailboxes recover.",
    },
    {
        term: "quarantined",
        tone: "border-red-300 bg-red-50 text-red-700",
        description: "No new assignments. Existing mailboxes should be migrated off.",
    },
    {
        term: "blocked",
        tone: "border-red-300 bg-red-50 text-red-700",
        description: "Excluded from placement entirely until an admin clears it.",
    },
];

// Mailbox health drives warmup pool membership and campaign throttling.
export const MAILBOX_HEALTH_LEGEND: LegendEntry[] = [
    {
        term: "healthy",
        tone: "border-emerald-300 bg-emerald-50 text-emerald-700",
        description: "Normal sending and warmup. Only healthy mailboxes are picked for pools.",
    },
    {
        term: "watch",
        tone: "border-amber-300 bg-amber-50 text-amber-700",
        description:
            "Early warning signals (spam placement or complaints trending up). Volume lowered, monitoring increased.",
    },
    {
        term: "throttled",
        tone: "border-orange-300 bg-orange-50 text-orange-700",
        description: "Sending slowed after repeated warnings while signals recover.",
    },
    {
        term: "quarantined",
        tone: "border-red-300 bg-red-50 text-red-700",
        description:
            "Removed from the shared warmup pool for a cooldown after crossing the quarantine band.",
    },
    {
        term: "blocked",
        tone: "border-red-300 bg-red-50 text-red-700",
        description:
            "Barred from warmup until the cooldown expires and re-entry checks pass. Requires review.",
    },
];

// Offline warmup-content generation job lifecycle.
export const GENERATION_JOB_LEGEND: LegendEntry[] = [
    {
        term: "pending",
        tone: "border-amber-300 bg-amber-50 text-amber-700",
        description: "Queued and waiting to start.",
    },
    {
        term: "running",
        tone: "border-amber-300 bg-amber-50 text-amber-700",
        description: "Generating threads right now. Counts update as it goes.",
    },
    {
        term: "completed",
        tone: "border-emerald-300 bg-emerald-50 text-emerald-700",
        description:
            "Finished. Check generated vs lint-rejected counts; rejected threads never enter the library.",
    },
    {
        term: "failed",
        tone: "border-red-300 bg-red-50 text-red-700",
        description: "Produced no usable threads. The error column has the reason.",
    },
    {
        term: "cancelled",
        tone: "border-zinc-300 bg-zinc-50 text-zinc-600",
        description: "Stopped by an admin before it finished.",
    },
];
