import { MailIcon } from "lucide-react";
import type { Lead, LeadStatus } from "@/lib/api/models/app/leads/Lead";
import { cn } from "@/lib/utils";
import { STATUS_LABEL, emailVerdict } from "./verdict";

const STATUS_STYLE: Record<LeadStatus, string> = {
    new: "bg-sky-50 text-sky-700",
    saved: "bg-violet-50 text-violet-700",
    contacted: "bg-emerald-50 text-emerald-700",
    dismissed: "bg-slate-100 text-slate-500",
};

export function StatusBadge({ status }: { status: LeadStatus }) {
    return <span className={cn("inline-flex h-5 items-center rounded px-1.5 text-[10.5px] font-medium", STATUS_STYLE[status])}>{STATUS_LABEL[status]}</span>;
}

export function ScoreBar({ score }: { score: number }) {
    const tone = score >= 70 ? "bg-sky-700" : score >= 40 ? "bg-sky-500" : "bg-slate-300";
    return (
        <div className="flex items-center gap-1.5" title={`Reachability score ${score} of 100`}>
            <div className="h-1.5 flex-1 rounded-full bg-slate-100 overflow-hidden">
                <div className={cn("h-full rounded-full", tone)} style={{ width: `${Math.max(4, score)}%` }} />
            </div>
            <span className="w-5 text-right text-[10.5px] tabular-nums text-slate-500">{score}</span>
        </div>
    );
}

export function EmailBadge({ lead }: { lead: Lead }) {
    const primary = lead.emails.find((e) => e.email === lead.email);
    const v = emailVerdict(primary);
    const more = lead.emails.filter((e) => e.email !== lead.email && e.status !== "invalid" && e.status !== "suppressed").length;
    return (
        <span className="inline-flex min-w-0 items-center gap-1 text-[11.5px]">
            <MailIcon className="size-3 shrink-0 text-slate-400" />
            <span className="truncate text-slate-800">{lead.email}</span>
            <span className={cn("shrink-0", v.tone)} title={v.label}>
                {v.icon}
            </span>
            {more > 0 && <span className="shrink-0 text-slate-400">+{more}</span>}
        </span>
    );
}
