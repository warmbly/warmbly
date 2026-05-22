// Page primitives — Linear density.
//
// Pages are dense. Title sits in a 40px toolbar row alongside actions, not
// in a block of hero space. Body uses 13px text with 6px row spacing. No
// serif type — everything's the system sans through Tailwind defaults.
//
//   <Page>
//     <PageHeader title="Accounts">
//       <Button>Connect</Button>
//     </PageHeader>
//     <PageSection title="Active">{...}</PageSection>
//   </Page>

import React from "react";
import { cn } from "@/lib/utils";

export function Page({
    children,
    className,
    width = "default",
}: {
    children: React.ReactNode;
    className?: string;
    width?: "default" | "wide" | "full";
}) {
    const widthClass = {
        default: "max-w-5xl",
        wide: "max-w-[1400px]",
        full: "",
    }[width];
    return (
        <div className={cn("px-4 pt-3 pb-10", widthClass, "mx-auto", className)}>
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
    eyebrow?: string;
    children?: React.ReactNode;
    className?: string;
}) {
    return (
        <div className={cn("flex items-center gap-3 h-9 mb-3", className)}>
            <div className="min-w-0 flex-1 flex items-baseline gap-2">
                {eyebrow && (
                    <span className="text-[11px] text-slate-400 font-medium shrink-0">
                        {eyebrow}
                    </span>
                )}
                <h1 className="text-[14px] font-semibold text-slate-900 tracking-tight truncate">
                    {title}
                </h1>
                {subtitle && (
                    <span className="text-[12px] text-slate-500 truncate">
                        {subtitle}
                    </span>
                )}
            </div>
            {children && (
                <div className="flex items-center gap-1.5 shrink-0">{children}</div>
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
        <section className={cn("mb-6", className)}>
            {(title || description || actions) && (
                <div className="flex items-center justify-between gap-3 mb-2 h-7">
                    <div className="min-w-0 flex items-baseline gap-2">
                        {title && (
                            <h2 className="text-[12.5px] font-semibold text-slate-700 tracking-tight">
                                {title}
                            </h2>
                        )}
                        {description && (
                            <span className="text-[12px] text-slate-500 truncate">
                                {description}
                            </span>
                        )}
                    </div>
                    {actions && (
                        <div className="flex items-center gap-1.5 shrink-0">{actions}</div>
                    )}
                </div>
            )}
            {children}
        </section>
    );
}

/**
 * Stat card — Linear-compact. Label + value on the same baseline, divided
 * by hairlines instead of cards. Group these in a row with no gaps and you
 * get a strip of stats that reads at a glance.
 */
export function StatCard({
    icon,
    label,
    value,
    hint,
}: {
    icon?: React.ReactNode;
    iconTone?: never;
    label: string;
    value: string | number;
    hint?: string;
}) {
    return (
        <div className="flex-1 min-w-0 px-3 py-2 border-r border-slate-200 last:border-r-0">
            <div className="flex items-center gap-1.5 text-[11px] text-slate-500 font-medium mb-1">
                {icon && <span className="text-slate-400">{icon}</span>}
                <span className="truncate">{label}</span>
            </div>
            <div className="text-[15px] font-semibold text-slate-900 tabular-nums leading-none">
                {value}
            </div>
            {hint && (
                <div className="text-[11px] text-slate-500 mt-1 truncate">{hint}</div>
            )}
        </div>
    );
}

/**
 * StatRow — wrap StatCards in this to get the dividing-line strip layout.
 * Use directly:
 *   <StatRow><StatCard ... /><StatCard ... /></StatRow>
 */
export function StatRow({ children }: { children: React.ReactNode }) {
    return (
        <div className="flex border border-slate-200 rounded-md bg-white mb-4">
            {children}
        </div>
    );
}

/**
 * Empty state — small, text-led. Designed to slot inside a table or
 * section, not occupy a whole page.
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
        <div className="border border-dashed border-slate-200 rounded-md py-10 px-4 text-center">
            {icon && (
                <div className="w-7 h-7 mx-auto mb-2 text-slate-400 flex items-center justify-center">
                    {icon}
                </div>
            )}
            <h3 className="text-[13px] font-medium text-slate-900">{title}</h3>
            {description && (
                <p className="text-[12px] text-slate-500 mt-1 max-w-sm mx-auto leading-relaxed">
                    {description}
                </p>
            )}
            {children && <div className="mt-3 flex justify-center gap-1.5">{children}</div>}
        </div>
    );
}
