// Small pieces every result view shares: reach meter, outreach badge, Warmbly marks and signal icons.
import { Building2Icon, CircleDotIcon, MegaphoneIcon, RepeatIcon, SparklesIcon, UserCheckIcon, UsersIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { FinderHit, FinderReach } from "@/lib/api/models/app/social/Social";
import { cn } from "@/lib/utils";
import { campaignsOf, contactOf, OUTREACH_LABELS, outreachStatus } from "./view";

export function Tip({ content, children }: { content: ReactNode; children: ReactNode }) {
    if (!content) return <>{children}</>;
    return (
        <Tooltip>
            <TooltipTrigger asChild>{children}</TooltipTrigger>
            <TooltipContent className="max-w-xs">{content}</TooltipContent>
        </Tooltip>
    );
}

const LEVEL_TONE: Record<string, string> = { high: "bg-emerald-500", medium: "bg-amber-400", low: "bg-slate-300" };
const LEVEL_LABEL: Record<string, string> = { high: "Easy to reach", medium: "Reachable", low: "Hard to reach" };

export function ReachReasons({ reach }: { reach: FinderReach }) {
    return (
        <ul className="grid gap-1">
            {reach.reasons.map((r) => (
                <li key={r.code} className="flex items-baseline justify-between gap-3">
                    <span>{r.label}</span>
                    <span className={cn("tabular-nums", r.weight < 0 ? "text-red-500" : "text-emerald-500")}>
                        {r.weight > 0 ? "+" : r.weight < 0 ? "-" : ""}
                        {Math.abs(r.weight)}
                    </span>
                </li>
            ))}
        </ul>
    );
}

/** Reach 0 to 100 as five segments and the number, the reasons in a tooltip. */
export function ReachMeter({ reach, className, showLabel }: { reach: FinderReach | null | undefined; className?: string; showLabel?: boolean }) {
    if (!reach) return <span className={cn("text-[12px] text-slate-300", className)}>-</span>;
    const filled = Math.max(0, Math.min(5, Math.round(reach.score / 20)));
    const level = LEVEL_LABEL[reach.level] ?? "Reach";
    return (
        <Tip
            content={
                <div className="grid min-w-44 gap-1.5 py-0.5">
                    <p className="font-semibold">
                        Reach {reach.score} · {level}
                    </p>
                    {reach.reasons.length ? <ReachReasons reach={reach} /> : <p className="opacity-80">No signals either way.</p>}
                </div>
            }
        >
            <span tabIndex={0} className={cn("inline-flex items-center gap-1.5 rounded outline-none focus-visible:ring-2 focus-visible:ring-sky-200", className)} aria-label={`Reach ${reach.score} of 100, ${level.toLowerCase()}`}>
                <span aria-hidden className="inline-flex gap-[2px]">
                    {Array.from({ length: 5 }, (_, i) => (
                        <span key={i} className={cn("h-2.5 w-1.5 rounded-[2px]", i < filled ? (LEVEL_TONE[reach.level] ?? "bg-slate-400") : "bg-slate-100")} />
                    ))}
                </span>
                <span className="text-[12px] font-medium tabular-nums text-slate-700">{reach.score}</span>
                {showLabel && <span className="text-[12px] text-slate-500">{level}</span>}
            </span>
        </Tip>
    );
}

const OUTREACH_TONE: Record<string, string> = {
    none: "bg-slate-50 text-slate-500 ring-slate-200",
    invite_pending: "bg-sky-50 text-sky-700 ring-sky-200",
    connected: "bg-emerald-50 text-emerald-700 ring-emerald-200",
    messaged: "bg-violet-50 text-violet-700 ring-violet-200",
    replied: "bg-emerald-50 text-emerald-700 ring-emerald-200",
};

export function Badge({ tone, children, className }: { tone: string; children: ReactNode; className?: string }) {
    return <span className={cn("inline-flex h-5 items-center gap-1 rounded-full px-1.5 text-[11px] font-medium ring-1 ring-inset whitespace-nowrap [&>svg]:size-3", tone, className)}>{children}</span>;
}

export const TONE = {
    sky: "bg-sky-50 text-sky-700 ring-sky-200",
    emerald: "bg-emerald-50 text-emerald-700 ring-emerald-200",
    violet: "bg-violet-50 text-violet-700 ring-violet-200",
    slate: "bg-slate-50 text-slate-600 ring-slate-200",
    amber: "bg-amber-50 text-amber-800 ring-amber-200",
};

/** What the workspace already did with this person; nothing for "not contacted" unless `always`. */
export function OutreachBadge({ hit, always }: { hit: FinderHit; always?: boolean }) {
    const s = outreachStatus(hit);
    if (s === "none" && !always) return null;
    return <Badge tone={OUTREACH_TONE[s]}>{OUTREACH_LABELS[s]}</Badge>;
}

/** "Contact" and "In a campaign": what Warmbly already holds for this person. */
export function WarmblyMarks({ hit, added, onOpenContact }: { hit: FinderHit; added?: boolean; onOpenContact?: (id: string) => void }) {
    const contactId = contactOf(hit);
    const campaigns = campaignsOf(hit);
    return (
        <>
            {added && !contactId && <Badge tone={TONE.emerald}>Added</Badge>}
            {contactId && (
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        onOpenContact?.(contactId);
                    }}
                    className="inline-flex rounded-full outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                    aria-label="Already a contact. Open the contact"
                >
                    <Badge tone={TONE.emerald}>Contact</Badge>
                </button>
            )}
            {campaigns.length > 0 && (
                <Tip content={campaigns.map((c) => c.name).join(", ")}>
                    <span tabIndex={0} className="inline-flex outline-none">
                        <Badge tone={TONE.violet}>{campaigns.length === 1 ? `In ${campaigns[0].name}` : `${campaigns.length} campaigns`}</Badge>
                    </span>
                </Tip>
            )}
        </>
    );
}

export interface Signal {
    key: string;
    label: string;
    icon: ReactNode;
}

export function signalsOf(hit: FinderHit): Signal[] {
    const ins = hit.insights;
    const out: Signal[] = [];
    if (ins?.recently_hired) out.push({ key: "hired", label: "Changed jobs", icon: <RepeatIcon /> });
    if (ins?.recently_promoted) out.push({ key: "promoted", label: "Promoted", icon: <SparklesIcon /> });
    if (ins?.posted_recently) out.push({ key: "posted", label: "Posted recently", icon: <MegaphoneIcon /> });
    if (ins?.open_to_work) out.push({ key: "open", label: "Open to work", icon: <CircleDotIcon /> });
    if (ins?.viewed_your_profile) out.push({ key: "viewed", label: "Viewed your profile", icon: <UserCheckIcon /> });
    if (ins?.follows_company) out.push({ key: "follows", label: "Follows your company", icon: <Building2Icon /> });
    if (ins?.past_colleague) out.push({ key: "colleague", label: "Past colleague", icon: <UsersIcon /> });
    return out;
}

export function Spotlight({ icon, children }: { icon: ReactNode; children: ReactNode }) {
    return (
        <span className="inline-flex h-5 items-center gap-1 rounded-full bg-slate-50 px-2 text-[11px] font-medium text-slate-700 ring-1 ring-inset ring-slate-200 [&>svg]:size-3 [&>svg]:text-sky-600">
            {icon}
            {children}
        </span>
    );
}

/** Signals as icons with tooltips, for dense rows. */
export function SignalIcons({ hit }: { hit: FinderHit }) {
    const sigs = signalsOf(hit);
    const shared = hit.insights?.shared_connections ?? 0;
    if (!sigs.length && !shared) return <span className="text-[12px] text-slate-300">-</span>;
    return (
        <span className="flex min-w-0 items-center gap-1">
            {sigs.map((s) => (
                <Tip key={s.key} content={s.label}>
                    <span tabIndex={0} aria-label={s.label} className="grid size-5 shrink-0 place-items-center rounded-full bg-slate-50 text-sky-600 ring-1 ring-inset ring-slate-200 outline-none [&>svg]:size-3">
                        {s.icon}
                    </span>
                </Tip>
            ))}
            {shared > 0 && (
                <Tip content={`${shared} shared connection${shared === 1 ? "" : "s"}`}>
                    <span tabIndex={0} className="inline-flex shrink-0 items-center gap-0.5 text-[11.5px] text-slate-500 tabular-nums outline-none">
                        <UsersIcon aria-hidden className="size-3" />
                        {shared}
                    </span>
                </Tip>
            )}
        </span>
    );
}
