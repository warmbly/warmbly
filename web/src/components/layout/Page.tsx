// Page primitives.
//
// Every page lives inside the white content panel of the new shell. That
// panel has a rounded top-left corner (where it meets the sky chrome).
// To make titles read well across all pages without each one inventing
// its own header, three primitives cover ~95% of cases:
//
//   <Page>           outer container with consistent padding
//   <PageHeader>     title + optional subtitle + optional right-side actions
//   <PageSection>    a labeled block inside a page (heading + children)
//
// Use them like:
//
//   <Page>
//     <PageHeader title="Accounts" subtitle="One row per mailbox">
//       <Button>Connect new</Button>
//     </PageHeader>
//     <PageSection title="Active">{...}</PageSection>
//   </Page>
//
// Anything more bespoke can drop down to raw classnames — the primitives
// are a default, not a straitjacket.

import React from "react";
import { cn } from "@/lib/utils";

export function Page({
    children,
    className,
    width = "default",
}: {
    children: React.ReactNode;
    className?: string;
    /**
     * default → constrained to a comfortable reading column
     * wide → 1280px ceiling, used for data tables / dashboards
     * full → no max-width, used for editors/maps/anything that wants the room
     */
    width?: "default" | "wide" | "full";
}) {
    const widthClass = {
        default: "max-w-3xl",
        wide: "max-w-7xl",
        full: "",
    }[width];
    return (
        <div className={cn("px-8 pt-8 pb-16", widthClass, "mx-auto", className)}>
            {children}
        </div>
    );
}

export function PageHeader({
    title,
    subtitle,
    eyebrow,
    children,
    className,
}: {
    title: string;
    subtitle?: string;
    /** Tiny uppercase label above the title (e.g. section group). Optional. */
    eyebrow?: string;
    /** Right-side actions (buttons, search box, etc.). */
    children?: React.ReactNode;
    className?: string;
}) {
    return (
        <div className={cn("flex items-start gap-6 mb-8", className)}>
            <div className="min-w-0 flex-1">
                {eyebrow && (
                    <div className="text-[10.5px] uppercase tracking-[0.18em] text-slate-400 font-semibold mb-2">
                        {eyebrow}
                    </div>
                )}
                {/* Serif page title — uses --font-serif (Georgia) which lends
                    the dashboard the same character as the marketing site
                    without screaming "branding." Tight tracking + crisp
                    optical sizing in slate-950 (one notch darker than 900). */}
                <h1
                    style={{ fontFamily: "var(--font-serif)" }}
                    className="text-[28px] leading-[1.1] font-medium text-slate-950 tracking-[-0.01em]"
                >
                    {title}
                </h1>
                {subtitle && (
                    <p className="text-[13.5px] text-slate-500 mt-2 leading-snug max-w-2xl">
                        {subtitle}
                    </p>
                )}
            </div>
            {children && (
                <div className="flex items-center gap-2 shrink-0 pt-1">{children}</div>
            )}
        </div>
    );
}

export function PageSection({
    title,
    description,
    children,
    className,
    actions,
}: {
    title?: string;
    description?: string;
    children: React.ReactNode;
    className?: string;
    actions?: React.ReactNode;
}) {
    return (
        <section className={cn("mb-8", className)}>
            {(title || description || actions) && (
                <div className="flex items-end justify-between gap-4 mb-3">
                    <div>
                        {title && (
                            <h2 className="text-[14px] font-semibold text-slate-800 tracking-tight">
                                {title}
                            </h2>
                        )}
                        {description && (
                            <p className="text-[12.5px] text-slate-500 mt-0.5">
                                {description}
                            </p>
                        )}
                    </div>
                    {actions && (
                        <div className="flex items-center gap-2 shrink-0">{actions}</div>
                    )}
                </div>
            )}
            {children}
        </section>
    );
}

/**
 * Stat card — small icon, label, large number.
 *
 * Two-layer shadow gives Linear-style soft elevation without a hard
 * border line — the card reads as "sitting on" the page rather than
 * "drawn on" it. Tabular-nums for the value so digits don't dance
 * when numbers change.
 */
export function StatCard({
    icon,
    iconTone = "slate",
    label,
    value,
    hint,
}: {
    icon: React.ReactNode;
    /** Hint color for the icon tile. */
    iconTone?: "slate" | "blue" | "emerald" | "amber" | "red" | "violet";
    label: string;
    value: string | number;
    hint?: string;
}) {
    const toneCls = {
        slate:   "bg-slate-100   text-slate-700",
        blue:    "bg-sky-100     text-sky-700",
        emerald: "bg-emerald-100 text-emerald-700",
        amber:   "bg-amber-100   text-amber-700",
        red:     "bg-red-100     text-red-700",
        violet:  "bg-violet-100  text-violet-700",
    }[iconTone];
    return (
        <div className="rounded-2xl bg-white p-5 shadow-[0_1px_2px_rgba(15,23,42,0.04),0_8px_24px_-12px_rgba(15,23,42,0.08)] ring-1 ring-slate-200/60">
            <div className="flex items-center gap-2.5 mb-3">
                <div className={cn("w-7 h-7 rounded-lg flex items-center justify-center", toneCls)}>
                    {icon}
                </div>
                <span className="text-[12px] uppercase tracking-[0.08em] text-slate-500 font-medium">
                    {label}
                </span>
            </div>
            <div className="text-[28px] font-medium text-slate-950 tracking-[-0.02em] tabular-nums leading-none">
                {value}
            </div>
            {hint && (
                <div className="text-[11.5px] text-slate-400 mt-2">{hint}</div>
            )}
        </div>
    );
}

/**
 * Empty state — when there's nothing to show. Composed of a small icon
 * tile, a serif headline (matches PageHeader voice), a supporting line,
 * and an optional CTA row.
 */
export function EmptyState({
    icon,
    title,
    description,
    children,
}: {
    icon?: React.ReactNode;
    title: string;
    description?: string;
    children?: React.ReactNode;
}) {
    return (
        <div className="rounded-2xl bg-[#fafbfd] ring-1 ring-slate-200/60 px-8 py-14 text-center">
            {icon && (
                <div className="w-11 h-11 mx-auto mb-4 rounded-xl bg-white ring-1 ring-slate-200/70 flex items-center justify-center text-slate-400 shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
                    {icon}
                </div>
            )}
            <h3
                style={{ fontFamily: "var(--font-serif)" }}
                className="text-[17px] text-slate-900 tracking-[-0.005em]"
            >
                {title}
            </h3>
            {description && (
                <p className="text-[13px] text-slate-500 mt-1.5 max-w-md mx-auto leading-relaxed">
                    {description}
                </p>
            )}
            {children && <div className="mt-5 flex justify-center gap-2">{children}</div>}
        </div>
    );
}
