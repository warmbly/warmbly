// The tour's plans chapter: what every plan includes, how the plans differ,
// and the cards to pick one. Every number comes from the plan catalog or the
// AI credits guide, never from the slide copy.

import type { ReactNode } from "react";
import { motion, useReducedMotion } from "framer-motion";
import {
    ArrowRightIcon,
    ArrowUpRightIcon,
    CheckIcon,
    CloudIcon,
    CircleDollarSignIcon,
    FlameIcon,
    InboxIcon,
    Loader2Icon,
    KeyRoundIcon,
    MegaphoneIcon,
    MinusIcon,
    RefreshCwIcon,
    ShieldCheckIcon,
    SparklesIcon,
    TagIcon,
    UsersIcon,
    WalletIcon,
    ZapIcon,
} from "lucide-react";
import AgentMark from "@/components/app/agent/AgentMark";
import BillingIntervalToggle from "@/components/app/billing/BillingIntervalToggle";
import { PAID_PLANS, getPlan, planOrder, type PlanDef, type PlanID } from "@/lib/plans";
import type { BillingInterval } from "@/lib/pricing";
import { cn } from "@/lib/utils";
import { EASE } from "./sceneTokens";
import { Title } from "./TourCopy";
import type { FlatSlide } from "./tourSteps";
import { EYEBROW } from "./tourUi";

// Monthly AI credits per plan, from the allowance table in the AI credits guide.
const AI_CREDITS: Partial<Record<PlanID, string>> = {
    starter: "250",
    grow: "2,000",
    business: "25,000",
};

// The pricing page's one-line pitch for each plan.
const BEST_FOR: Partial<Record<PlanID, string>> = {
    starter: "Small teams starting cold outreach",
    grow: "Teams sending from many mailboxes",
    business: "Established teams at real volume",
    enterprise: "Custom volume, contract and support",
};

// The default cold limit per mailbox, which is what makes a send budget concrete.
const PER_MAILBOX = 50;

const sends = (p: PlanDef) => (Number.isFinite(p.sendsPerDay) ? p.sendsPerDay.toLocaleString() : "15,000+");
const busy = (p: PlanDef) => (Number.isFinite(p.sendsPerDay) ? (p.sendsPerDay / PER_MAILBOX).toLocaleString() : "300+");

interface PanelProps {
    slide: FlatSlide;
    titleId: string;
    /** Remie's card for this slide, drawn beside the heading. */
    remie?: ReactNode;
    /** Drawn in the heading's corner instead of Remie, when there is no card. */
    aside?: ReactNode;
    /** Replace the slide's own lead-in, title and body. */
    lead?: string;
    title?: string;
    body?: string;
    children: ReactNode;
}

// Sized by the tour's own width (it sits in an @container), not the viewport's.
function Panel({ slide, titleId, remie, aside, lead, title, body, children }: PanelProps) {
    const count = slide.chapter.slides.length;
    return (
        <div className="flex-1 min-w-0 overflow-y-auto px-4 pt-5 pb-6 @2xl:px-10 @2xl:pt-12">
            <div className="flex flex-col gap-4 @4xl:flex-row @4xl:items-start @4xl:gap-10">
                <div className="min-w-0 flex-1 max-w-[620px]">
                    <div className={EYEBROW}>
                        {slide.chapter.name}
                        {count > 1 && ` · ${slide.si + 1} of ${count}`}
                    </div>
                    <div className="mt-3">
                        <Title slide={slide} titleId={titleId} lead={lead} title={title} />
                    </div>
                    <p className="mt-3 text-[13.5px] leading-[1.6] text-slate-500">{body ?? slide.body}</p>
                </div>
                {remie && <div className="@4xl:w-[360px] @4xl:shrink-0">{remie}</div>}
                {aside && <div className="@4xl:ml-auto @4xl:self-end">{aside}</div>}
            </div>
            {children}
        </div>
    );
}

function Rise({ i, className, children }: { i: number; className?: string; children: ReactNode }) {
    const reduced = useReducedMotion();
    return (
        <motion.div
            className={className}
            initial={reduced ? false : { opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.45, delay: 0.08 + i * 0.05, ease: EASE }}
        >
            {children}
        </motion.div>
    );
}

const INCLUDED: { icon: ReactNode; name: string; line: string }[] = [
    { icon: <FlameIcon className="size-4 text-orange-500" />, name: "Mailboxes and warmup", line: "Unlimited mailboxes, every one warming in the premium pool" },
    { icon: <MegaphoneIcon className="size-4 text-sky-600" />, name: "Campaigns", line: "Sequences with branching, Stop on reply and AI-written lines" },
    { icon: <InboxIcon className="size-4 text-sky-600" />, name: "Unified inbox", line: "Every reply in one place, with the inbox agent drafting answers" },
    { icon: <UsersIcon className="size-4 text-emerald-600" />, name: "Contacts and forms", line: "Imports, Google Sheets, segments and hosted forms" },
    { icon: <CircleDollarSignIcon className="size-4 text-emerald-600" />, name: "CRM", line: "Pipelines, deals, tasks and meetings, or your HubSpot, Pipedrive or Salesforce" },
    { icon: <ZapIcon className="size-4 text-amber-500" />, name: "Automations", line: "Triggers and actions across Slack, Zapier, Make, n8n and webhooks" },
    { icon: <ShieldCheckIcon className="size-4 text-emerald-600" />, name: "Deliverability", line: "Placement tests, the Advisor, suppression and one-click unsubscribe" },
    { icon: <AgentMark size={16} />, name: "Remie and AI", line: "AI credits every month for Remie, reply drafts and contact research" },
    { icon: <KeyRoundIcon className="size-4 text-slate-600" />, name: "Team and API", line: "Invite your team with roles, an audit log and API keys" },
];

export function PlanIncluded(props: Omit<PanelProps, "children">) {
    return (
        <Panel {...props}>
            <div className="mt-6 grid grid-cols-1 gap-3 @xl:grid-cols-2 @5xl:grid-cols-3 @2xl:mt-8">
                {INCLUDED.map((f, i) => (
                    <Rise key={f.name} i={i} className="flex items-start gap-3 rounded-[12px] bg-white p-4 ring-1 ring-slate-200">
                        <span className="size-8 shrink-0 rounded-[8px] bg-slate-50 ring-1 ring-slate-200/80 inline-flex items-center justify-center">{f.icon}</span>
                        <span className="min-w-0">
                            <span className="block text-[13.5px] font-medium text-slate-900">{f.name}</span>
                            <span className="mt-0.5 block text-[12.5px] leading-[1.45] text-slate-500">{f.line}</span>
                        </span>
                    </Rise>
                ))}
            </div>
        </Panel>
    );
}

function Yes() {
    return (
        <span className="inline-flex size-5 items-center justify-center rounded-full bg-emerald-50 text-emerald-600" aria-label="Included">
            <CheckIcon className="size-3" strokeWidth={3} />
        </span>
    );
}

function No() {
    return <MinusIcon className="size-4 text-slate-300" aria-label="Not included" />;
}

type PlanProps = { interval: BillingInterval; onInterval: (i: BillingInterval) => void; current: PlanID | null };

export function PlanCompare({ interval, onInterval, current, ...props }: Omit<PanelProps, "children"> & PlanProps) {
    const plans = PAID_PLANS.map(getPlan);
    const rows: { label: string; note?: string; cell: (p: PlanDef) => ReactNode }[] = [
        { label: "Emails a day", cell: (p) => <span className="font-medium text-slate-900 tabular-nums">{sends(p)}</span> },
        { label: "Mailboxes kept busy", note: `at ${PER_MAILBOX} a day each`, cell: (p) => <span className="tabular-nums">{busy(p)}</span> },
        { label: "AI credits a month", cell: (p) => <span className="tabular-nums">{AI_CREDITS[p.id] ?? "Custom"}</span> },
        { label: "Mailboxes and warmup", cell: () => "Unlimited" },
        { label: "Campaigns, inbox, CRM, automations and Remie", cell: () => <Yes /> },
        { label: "Sending kept apart from other customers", cell: (p) => (p.isolatedSending ? <Yes /> : <No />) },
        { label: "Dedicated support", cell: (p) => (p.id === "enterprise" ? <Yes /> : <No />) },
    ];

    return (
        <Panel {...props}>
            <div className="mt-6 flex flex-wrap items-center justify-between gap-2">
                <span className="text-[12px] text-slate-400">Yearly billing is 20% off.</span>
                <BillingIntervalToggle interval={interval} onChange={onInterval} />
            </div>

            {/* Narrow: one card per plan with the same rows. Wide: the table. */}
            <div className="mt-3 grid grid-cols-1 gap-3 @xl:grid-cols-2 @4xl:hidden">
                {plans.map((p, i) => (
                    <Rise key={p.id} i={i} className={cn("rounded-[12px] bg-white p-4 ring-1", p.featured ? "ring-slate-900" : "ring-slate-200")}>
                        <PlanHead plan={p} interval={interval} current={current === p.id} />
                        <p className="mt-1.5 text-[12.5px] text-slate-500">{BEST_FOR[p.id]}</p>
                        <dl className="mt-3 space-y-1.5 border-t border-slate-100 pt-3 text-[12.5px]">
                            {rows.map((r) => (
                                <div key={r.label} className="flex items-center justify-between gap-3">
                                    <dt className="text-slate-500">{r.label}</dt>
                                    <dd className="shrink-0 text-right text-slate-800">{r.cell(p)}</dd>
                                </div>
                            ))}
                        </dl>
                    </Rise>
                ))}
            </div>

            <Rise i={0} className="mt-3 hidden overflow-hidden rounded-[12px] ring-1 ring-slate-200 @4xl:block">
                <table className="w-full table-fixed border-collapse text-left text-[13px]">
                    <thead>
                        <tr>
                            <th className="w-[26%] p-4 align-bottom text-[12px] font-normal text-slate-400">Per month</th>
                            {plans.map((p) => (
                                <th key={p.id} className={cn("p-4 align-bottom font-normal", p.featured && "bg-slate-50 shadow-[inset_0_3px_0_#0f172a]")}>
                                    <PlanHead plan={p} interval={interval} current={current === p.id} />
                                </th>
                            ))}
                        </tr>
                    </thead>
                    <tbody>
                        <tr>
                            <th scope="row" className="border-t border-slate-100 px-4 py-2.5 font-normal text-slate-600">Best for</th>
                            {plans.map((p) => (
                                <td key={p.id} className={cn("border-t border-slate-100 px-4 py-2.5 text-slate-600", p.featured && "bg-slate-50")}>
                                    {BEST_FOR[p.id]}
                                </td>
                            ))}
                        </tr>
                        {rows.map((r) => (
                            <tr key={r.label}>
                                <th scope="row" className="border-t border-slate-100 px-4 py-2.5 font-normal text-slate-600">
                                    {r.label}
                                    {r.note && <span className="block text-[11.5px] text-slate-400">{r.note}</span>}
                                </th>
                                {plans.map((p) => (
                                    <td key={p.id} className={cn("border-t border-slate-100 px-4 py-2.5 text-slate-800", p.featured && "bg-slate-50")}>
                                        {r.cell(p)}
                                    </td>
                                ))}
                            </tr>
                        ))}
                    </tbody>
                </table>
            </Rise>
            <p className="mt-3 text-[12px] leading-[1.5] text-slate-400">
                Mailboxes are unlimited under fair use: one mailbox for every email a day your plan includes, raised on request.
            </p>
        </Panel>
    );
}

function PlanHead({ plan, interval, current }: { plan: PlanDef; interval: BillingInterval; current: boolean }) {
    const price = interval === "annual" ? plan.priceAnnual : plan.priceMonthly;
    const badge = "h-5 px-2 rounded-full text-[10.5px] font-medium inline-flex items-center whitespace-nowrap";
    return (
        <div>
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span className="text-[14px] font-medium text-slate-900">{plan.label}</span>
                {current ? (
                    <span className={cn(badge, "bg-sky-50 text-sky-700")}>Your plan</span>
                ) : plan.featured ? (
                    <span className={cn(badge, "bg-slate-900 text-white")}>Most popular</span>
                ) : null}
            </div>
            <div className="mt-1.5 flex flex-wrap items-baseline gap-x-1">
                {price == null ? (
                    <span className="text-[22px] font-medium tracking-[-0.04em] text-slate-900">Custom</span>
                ) : (
                    <>
                        <span className="text-[24px] font-medium tracking-[-0.04em] text-slate-900 tabular-nums">${price}</span>
                        <span className="text-[11.5px] text-slate-400 whitespace-nowrap">{interval === "annual" ? "billed yearly" : "billed monthly"}</span>
                    </>
                )}
            </div>
        </div>
    );
}

// How billing behaves, from the billing and AI credits guides.
const FACTS: { icon: ReactNode; text: string }[] = [
    { icon: <TagIcon className="size-3.5" />, text: "Yearly billing is 20% off" },
    { icon: <RefreshCwIcon className="size-3.5" />, text: "Change plan any time, prorated" },
    { icon: <WalletIcon className="size-3.5" />, text: "Cancel and keep your mailboxes, warmup and settings" },
    { icon: <SparklesIcon className="size-3.5" />, text: "AI top-ups of 500 to 10,000 credits never expire" },
];

const CHOOSE =
    "mt-auto h-10 w-full rounded-full inline-flex items-center justify-center gap-1.5 text-[13px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400";

function chooseLabel(id: PlanID, current: PlanID | null): string {
    const label = getPlan(id).label;
    if (current === id) return "Your plan";
    if (id === "enterprise") return "Talk to us";
    if (!current) return `Choose ${label}`;
    return planOrder(id) > planOrder(current) ? `Upgrade to ${label}` : `Switch to ${label}`;
}

export function PlanChoose({
    interval,
    onInterval,
    current,
    onChoose,
    pending,
    ownerOnly,
    note,
    hrefFor,
    onOpen,
    children,
    ...props
}: Omit<PanelProps, "aside" | "children"> & { children?: ReactNode } &
    PlanProps & {
        /** Starts checkout or a plan change; absent when this member cannot. */
        onChoose?: (id: PlanID) => void;
        pending: PlanID | null;
        /** Explains why there are no buttons: only the owner changes the plan. */
        ownerOnly: boolean;
        /** A closing line under the cards. */
        note?: string;
        /** Instead of checkout, each card links out to the plan on warmbly.com. */
        hrefFor?: (id: PlanID) => string;
        onOpen?: (id: PlanID) => void;
    }) {
    return (
        <Panel {...props} aside={<BillingIntervalToggle interval={interval} onChange={onInterval} />}>
            <div className="mt-6 grid grid-cols-1 gap-3 @xl:grid-cols-2 @5xl:grid-cols-4 @2xl:mt-8">
                {PAID_PLANS.map((id, i) => {
                    const plan = getPlan(id);
                    const mine = current === id;
                    return (
                        <Rise
                            key={id}
                            i={i}
                            className={cn(
                                "flex flex-col rounded-[12px] p-5 ring-1",
                                mine ? "bg-sky-50/40 ring-sky-300" : "bg-white",
                                !mine && (plan.featured ? "ring-slate-900" : "ring-slate-200"),
                            )}
                        >
                            <PlanHead plan={plan} interval={interval} current={mine} />
                            <p className="mt-2 text-[12.5px] leading-[1.45] text-slate-500">{BEST_FOR[id]}</p>
                            <ul className="mt-4 mb-5 pt-4 border-t border-slate-100 space-y-2 text-[12.5px] text-slate-600">
                                <li>{sends(plan)} emails a day</li>
                                <li>{AI_CREDITS[id] ? `${AI_CREDITS[id]} AI credits a month` : "Custom AI credits"}</li>
                                <li>Unlimited mailboxes and warmup</li>
                                <li>Every feature in this tour</li>
                                {plan.isolatedSending && <li>Sending kept apart from other customers</li>}
                                {id === "enterprise" && <li>Dedicated support</li>}
                            </ul>
                            {hrefFor ? (
                                <a
                                    href={hrefFor(id)}
                                    target="_blank"
                                    rel="noopener noreferrer"
                                    onClick={() => onOpen?.(id)}
                                    className={cn(
                                        CHOOSE,
                                        plan.featured ? "bg-slate-900 text-white hover:bg-slate-800" : "bg-white text-slate-800 ring-1 ring-slate-200 hover:bg-slate-50",
                                    )}
                                >
                                    {id === "enterprise" ? "Talk to us" : `Start on ${plan.label}`}
                                    <ArrowUpRightIcon className="size-3.5" />
                                </a>
                            ) : (
                                onChoose && (
                                    <button
                                        type="button"
                                        disabled={mine || pending !== null}
                                        onClick={() => onChoose(id)}
                                        className={cn(
                                            CHOOSE,
                                            "disabled:cursor-default",
                                            mine
                                                ? "bg-white text-sky-700 ring-1 ring-sky-200"
                                                : plan.featured
                                                  ? "bg-slate-900 text-white hover:bg-slate-800 disabled:opacity-60"
                                                  : "bg-white text-slate-800 ring-1 ring-slate-200 hover:bg-slate-50 disabled:opacity-60",
                                        )}
                                    >
                                        {pending === id && <Loader2Icon className="size-3.5 animate-spin" />}
                                        {chooseLabel(id, current)}
                                    </button>
                                )
                            )}
                        </Rise>
                    );
                })}
            </div>
            <div className="mt-5 grid grid-cols-1 gap-2 @xl:grid-cols-2 @5xl:grid-cols-4">
                {FACTS.map((f) => (
                    <div key={f.text} className="flex items-center gap-2 text-[12px] leading-[1.4] text-slate-500">
                        <span className="shrink-0 text-slate-400">{f.icon}</span>
                        {f.text}
                    </div>
                ))}
            </div>
            {ownerOnly && (
                <p className="mt-5 text-[12.5px] leading-[1.5] text-slate-500">Only the workspace owner can change the plan. Ask them in Settings {">"} Billing.</p>
            )}
            {children}
            {note && <p className="mt-5 text-[12.5px] leading-[1.5] text-slate-400">{note}</p>}
        </Panel>
    );
}

/** Keeping the instance: its mailboxes can still warm in the hosted pool. */
export function CloudCallout({ onLink, docs }: { onLink?: () => void; docs: string }) {
    const cls =
        "h-9 shrink-0 px-3.5 rounded-full inline-flex items-center justify-center gap-1.5 bg-white text-[12.5px] font-medium text-slate-800 ring-1 ring-slate-200 transition-colors hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400";
    return (
        <div className="mt-5 flex flex-col gap-3 rounded-[12px] bg-sky-50/50 p-4 ring-1 ring-sky-100 @2xl:flex-row @2xl:items-center @2xl:gap-5">
            <span className="hidden size-9 shrink-0 items-center justify-center rounded-full bg-white text-sky-600 ring-1 ring-sky-100 @2xl:inline-flex">
                <CloudIcon className="size-4" />
            </span>
            <div className="min-w-0 flex-1">
                <p className="text-[13.5px] font-medium text-slate-900">Keeping this instance? Warm it in our pool.</p>
                <p className="mt-0.5 text-[12.5px] leading-[1.5] text-slate-500">
                    Link it to Warmbly Cloud and its mailboxes warm against thousands of real inboxes, while campaigns, contacts and mail stay on your server. Free for up to 10 mailboxes, or $15 a month for the premium pool with no cap.
                </p>
            </div>
            {onLink ? (
                <button type="button" onClick={onLink} className={cls}>
                    Link Warmbly Cloud
                    <ArrowRightIcon className="size-3.5" />
                </button>
            ) : (
                <a href={docs} target="_blank" rel="noopener noreferrer" className={cls}>
                    How it works
                    <ArrowUpRightIcon className="size-3.5" />
                </a>
            )}
        </div>
    );
}
