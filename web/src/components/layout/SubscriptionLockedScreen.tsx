// Full-page screen a hosted workspace sees on a locked page before it has a
// plan: the auth screen's sky, one floating card, and the three ways forward.

import React from "react";
import { Link } from "react-router-dom";
import { motion } from "framer-motion";
import { ArrowRightIcon, CheckIcon, CloudIcon, InboxIcon, ServerIcon, SparklesIcon } from "lucide-react";
import { getPlan } from "@/lib/plans";
import { useAppStore } from "@/stores";
import { useUpgradeDialog } from "@/hooks/context/upgrade";

const SELF_HOST_DOCS = "https://docs.warmbly.com/development/deployment-guide/";

export default function SubscriptionLockedScreen({ feature }: { feature: string }) {
    const isOwner = useAppStore((s) => s.currentOrganization?.role === "owner");
    const upgradeDialog = useUpgradeDialog();
    const starter = getPlan("starter");
    const openPlans = () =>
        upgradeDialog.open({
            feature,
            minPlan: "starter",
            blurb: "Campaigns, the unified inbox, contacts, CRM, automations and integrations on our infrastructure. Pick a plan and it is live the moment checkout completes.",
        });

    return (
        <div className="relative min-h-full w-full overflow-hidden flex items-center justify-center px-4 py-10">
            <div className="absolute inset-0" aria-hidden="true">
                <div className="sky-base" />
                <div className="sky-breathe" />
                <div className="sun-glow" />
                <img src="/backdrops/cloud-3.webp" alt="" decoding="async" className="cloud-drift cloud-1 absolute select-none" style={{ top: "6%", left: "-10%", width: 360, opacity: 0.55, height: "auto" }} />
                <img src="/backdrops/cloud-4.webp" alt="" decoding="async" className="cloud-drift cloud-2 absolute select-none" style={{ bottom: "8%", right: "-8%", width: 320, opacity: 0.5, height: "auto" }} />
                <img src="/backdrops/cloud-1.webp" alt="" decoding="async" className="cloud-drift cloud-1 absolute select-none" style={{ top: "40%", right: "12%", width: 220, opacity: 0.35, height: "auto" }} />
            </div>

            <motion.div
                initial={{ y: 14, opacity: 0 }}
                animate={{ y: 0, opacity: 1 }}
                transition={{ duration: 0.4, ease: [0.16, 1, 0.3, 1] }}
                className="relative z-10 w-full max-w-[920px] animate-card-float rounded-3xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04),0_30px_70px_-32px_rgba(15,23,42,0.32)] overflow-hidden"
            >
                <div className="px-7 pt-8 pb-6 md:px-10 md:pt-10 text-center">
                    <span className="inline-flex items-center gap-1.5 h-6 px-2.5 rounded-full bg-sky-50 text-sky-700 text-[11px] font-medium">
                        <SparklesIcon className="w-3 h-3" /> Free workspace
                    </span>
                    <h1 className="mt-4 text-[26px] md:text-[34px] font-semibold tracking-[-0.03em] leading-[1.08] text-slate-900">
                        {feature} unlocks with a plan.
                    </h1>
                    <p className="mt-3 text-[14px] text-slate-500 leading-relaxed max-w-xl mx-auto">
                        Your workspace is free forever for warming mailboxes. Pick how you want to send: connect mailboxes here, run Warmbly on your own server,
                        or choose a plan for the full hosted product.
                    </p>
                </div>

                <div className="grid md:grid-cols-3 gap-px bg-slate-200/70 border-y border-slate-200/70">
                    <Path
                        icon={InboxIcon}
                        eyebrow="Included"
                        title="Warm up to 10 mailboxes"
                        body="Connect mailboxes and they warm in the Warmbly pool at no cost, with replies and spam rescue handled for you."
                        cta="Go to mailboxes"
                        to="/app/emails"
                    />
                    <Path
                        icon={ServerIcon}
                        eyebrow="Free"
                        title="Self-host Warmbly"
                        body="Run the whole platform on your server, unlimited, then link the instance so this workspace warms its mailboxes."
                        cta="Self-host guide"
                        href={SELF_HOST_DOCS}
                        secondary={{ label: "Linked instances", to: "/app/settings/warmbly-cloud" }}
                    />
                    <Path
                        icon={CloudIcon}
                        eyebrow={starter.priceMonthly != null ? `From $${starter.priceMonthly}/mo` : "Plans"}
                        title="Send from Warmbly Cloud"
                        body="Campaigns, the unified inbox, contacts, CRM, automations and integrations on our infrastructure."
                        bullets={starter.bullets}
                        cta={isOwner ? "Choose a plan" : "See plans"}
                        onClick={openPlans}
                        primary
                    />
                </div>

                <div className="px-7 py-4 md:px-10 flex flex-wrap items-center justify-between gap-2 text-[12px] text-slate-500">
                    <span>Settings, billing and your mailboxes stay open on the free workspace.</span>
                    {!isOwner && <span>Only the workspace owner can choose a plan.</span>}
                </div>
            </motion.div>
        </div>
    );
}

function Path({
    icon: Icon,
    eyebrow,
    title,
    body,
    bullets,
    cta,
    to,
    href,
    onClick,
    secondary,
    primary = false,
}: {
    icon: React.ComponentType<{ className?: string }>;
    eyebrow: string;
    title: string;
    body: string;
    bullets?: string[];
    cta: string;
    to?: string;
    href?: string;
    /** Opens something in place (the upgrade dialog) instead of navigating. */
    onClick?: () => void;
    secondary?: { label: string; to: string };
    primary?: boolean;
}) {
    const btn = primary
        ? "inline-flex items-center gap-1.5 h-9 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[13px] font-medium transition-colors"
        : "inline-flex items-center gap-1.5 h-9 px-3.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[13px] font-medium text-slate-800 transition-colors";
    return (
        <div className={`bg-white px-6 py-6 flex flex-col ${primary ? "bg-gradient-to-b from-sky-50/60 to-white" : ""}`}>
            <span className={`size-9 rounded-lg inline-flex items-center justify-center ${primary ? "bg-sky-600 text-white" : "bg-slate-100 text-slate-700"}`}>
                <Icon className="w-4 h-4" />
            </span>
            <span className="mt-4 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{eyebrow}</span>
            <h2 className="mt-1 text-[15px] font-semibold text-slate-900">{title}</h2>
            <p className="mt-1.5 text-[12.5px] text-slate-500 leading-relaxed">{body}</p>
            {bullets && (
                <ul className="mt-3 space-y-1">
                    {bullets.map((b) => (
                        <li key={b} className="flex items-start gap-1.5 text-[12px] text-slate-700">
                            <CheckIcon className="w-3 h-3 text-emerald-600 mt-0.5 shrink-0" />
                            {b}
                        </li>
                    ))}
                </ul>
            )}
            <div className="mt-auto pt-5 flex flex-wrap items-center gap-2">
                {onClick ? (
                    <button type="button" onClick={onClick} className={btn}>
                        {cta} <ArrowRightIcon className="w-3.5 h-3.5" />
                    </button>
                ) : href ? (
                    <a href={href} target="_blank" rel="noreferrer" className={btn}>
                        {cta} <ArrowRightIcon className="w-3.5 h-3.5" />
                    </a>
                ) : (
                    <Link to={to ?? "/app"} className={btn}>
                        {cta} <ArrowRightIcon className="w-3.5 h-3.5" />
                    </Link>
                )}
                {secondary && (
                    <Link to={secondary.to} className="text-[12px] font-medium text-slate-500 hover:text-slate-900">
                        {secondary.label}
                    </Link>
                )}
            </div>
        </div>
    );
}
