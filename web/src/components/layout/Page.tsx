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
        <div className={cn("px-6 pt-6 pb-12", widthClass, "mx-auto", className)}>
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
        <div className={cn("flex items-start gap-4 mb-6", className)}>
            <div className="min-w-0 flex-1">
                {eyebrow && (
                    <div className="text-[11px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5">
                        {eyebrow}
                    </div>
                )}
                <h1 className="text-[22px] font-semibold text-slate-900 tracking-tight leading-tight">
                    {title}
                </h1>
                {subtitle && (
                    <p className="text-[13px] text-slate-500 mt-1 leading-snug">
                        {subtitle}
                    </p>
                )}
            </div>
            {children && (
                <div className="flex items-center gap-2 shrink-0">{children}</div>
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
 * A standard stat card — small icon, label, big number, optional trend.
 * Used on dashboards: emails, analytics, admin workers, etc.
 */
export function StatCard({
    icon,
    iconTone = "slate",
    label,
    value,
    hint,
}: {
    icon: React.ReactNode;
    /** Hint color for the icon background. */
    iconTone?: "slate" | "blue" | "emerald" | "amber" | "red" | "violet";
    label: string;
    value: string | number;
    hint?: string;
}) {
    const toneCls = {
        slate:   "bg-slate-100   text-slate-600",
        blue:    "bg-sky-100     text-sky-700",
        emerald: "bg-emerald-100 text-emerald-700",
        amber:   "bg-amber-100   text-amber-700",
        red:     "bg-red-100     text-red-700",
        violet:  "bg-violet-100  text-violet-700",
    }[iconTone];
    return (
        <div className="rounded-xl border border-slate-200/80 bg-white p-4">
            <div className="flex items-center gap-2.5 mb-2.5">
                <div className={cn("w-8 h-8 rounded-lg flex items-center justify-center", toneCls)}>
                    {icon}
                </div>
                <span className="text-[12.5px] text-slate-500">{label}</span>
            </div>
            <div className="text-[22px] font-semibold text-slate-900 tracking-tight tabular-nums">
                {value}
            </div>
            {hint && (
                <div className="text-[11.5px] text-slate-400 mt-1">{hint}</div>
            )}
        </div>
    );
}

/**
 * Empty state — used when a list is empty. Icon + headline + supporting
 * line + optional CTA.
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
        <div className="rounded-xl border border-dashed border-slate-200 bg-slate-50/50 px-6 py-12 text-center">
            {icon && (
                <div className="w-12 h-12 mx-auto mb-3 rounded-xl bg-white border border-slate-200 flex items-center justify-center text-slate-400">
                    {icon}
                </div>
            )}
            <h3 className="text-[14px] font-semibold text-slate-800">{title}</h3>
            {description && (
                <p className="text-[12.5px] text-slate-500 mt-1 max-w-sm mx-auto">
                    {description}
                </p>
            )}
            {children && <div className="mt-4 flex justify-center gap-2">{children}</div>}
        </div>
    );
}
