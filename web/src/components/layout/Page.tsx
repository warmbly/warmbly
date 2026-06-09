// Page primitives — dense, edge-to-edge, hairline dividers.
//
// Inspired by brae's dashboard chrome: pages fill the white content panel
// to its edges, sections separate by a single hairline (border-b
// border-slate-200), not by gutter or shadow. Small tracked-uppercase
// eyebrow labels replace large titles. Stats live in a strip cell across
// the full width, not in scattered cards.
//
//   <Page>
//     <PageTopbar eyebrow="Accounts" subtitle="One row per mailbox">
//       <Action>Add account</Action>
//     </PageTopbar>
//     <StatStrip>
//       <Stat label="Total" value={42} />
//       <Stat label="Healthy" value={36} accent />
//       ...
//     </StatStrip>
//     <PageBody>{...table...}</PageBody>
//   </Page>

import React from "react";
import { Link } from "react-router-dom";
import { cn } from "@/lib/utils";

// Internal app paths navigate client-side (React Router Link, no full reload);
// external URLs (http..., mailto:, //) stay as a plain anchor.
function isInternalHref(href: string): boolean {
    return href.startsWith("/") && !href.startsWith("//");
}

/**
 * Page — outer frame. Fills its parent (the white content panel) without
 * a max-width ceiling or padding. Sub-sections paint their own structure.
 */
export function Page({ children, className }: {
    children: React.ReactNode;
    className?: string;
    /** Deprecated — kept so old callers compile. New layout fills the panel. */
    width?: "default" | "wide" | "full";
}) {
    return (
        <div className={cn("flex flex-col min-h-full bg-white", className)}>
            {children}
        </div>
    );
}

/**
 * PageTopbar — 48px topbar at the top of a page. Eyebrow label at the
 * left in small tracked uppercase, an inline subtitle for context, actions
 * on the right.
 */
export function PageTopbar({
    eyebrow,
    subtitle,
    children,
    className,
}: {
    eyebrow: string;
    subtitle?: string;
    children?: React.ReactNode;
    className?: string;
}) {
    return (
        <div
            className={cn(
                "h-12 px-5 border-b border-slate-200 flex items-center gap-3 shrink-0 bg-white sticky top-0 z-10",
                className,
            )}
        >
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                {eyebrow}
            </span>
            {subtitle && (
                <>
                    <div className="h-4 w-px bg-slate-200" />
                    <span className="text-[12.5px] text-slate-600 truncate">
                        {subtitle}
                    </span>
                </>
            )}
            {children && <div className="ml-auto flex items-center gap-1.5">{children}</div>}
        </div>
    );
}

/**
 * Primary topbar action — solid sky pill at 28px tall.
 */
export function TopbarAction({
    children,
    onClick,
    href,
    icon,
    variant = "primary",
    disabled = false,
}: {
    children: React.ReactNode;
    onClick?: () => void;
    href?: string;
    icon?: React.ReactNode;
    variant?: "primary" | "ghost";
    disabled?: boolean;
}) {
    const cls =
        variant === "primary"
            ? "bg-sky-600 hover:bg-sky-700 text-white"
            : "border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 bg-white";
    if (href) {
        const actionCls = cn(
            "h-7 px-2.5 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium transition-colors",
            cls,
        );
        if (isInternalHref(href)) {
            return (
                <Link to={href} className={actionCls}>
                    {icon}
                    {children}
                </Link>
            );
        }
        return (
            <a href={href} className={actionCls}>
                {icon}
                {children}
            </a>
        );
    }
    return (
        <button
            onClick={onClick}
            disabled={disabled}
            className={cn(
                "h-7 px-2.5 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium transition-colors disabled:opacity-60 disabled:cursor-not-allowed",
                cls,
            )}
        >
            {icon}
            {children}
        </button>
    );
}

/**
 * StatStrip — full-width grid of stats with vertical-rule dividers and a
 * bottom hairline. Use Stat as children.
 */
export function StatStrip({ children, cols = 4 }: { children: React.ReactNode; cols?: 2 | 3 | 4 | 5 }) {
    const gridCls = {
        2: "grid-cols-2",
        3: "grid-cols-2 md:grid-cols-3",
        4: "grid-cols-2 md:grid-cols-4",
        5: "grid-cols-2 md:grid-cols-5",
    }[cols];
    return (
        <div className={cn("grid border-b border-slate-200 shrink-0 bg-white", gridCls)}>
            {children}
        </div>
    );
}

/**
 * Stat — one cell of the StatStrip. `accent` shows a small pulsing sky
 * dot next to the label. Each cell has a right border that compounds into
 * the strip's vertical-rule pattern; the last one drops it via `last`.
 */
export function Stat({
    label,
    value,
    sub,
    accent = false,
    href,
    last = false,
    onClick,
}: {
    label: string;
    value: React.ReactNode;
    sub?: string;
    accent?: boolean;
    href?: string;
    onClick?: () => void;
    last?: boolean;
}) {
    const inner = (
        <>
            <div className="flex items-center gap-2">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    {label}
                </span>
                {accent && (
                    <span className="size-1.5 rounded-full bg-sky-500 animate-pulse" />
                )}
                {(href || onClick) && (
                    <span className="ml-auto text-[10px] text-slate-300 group-hover:text-slate-500 transition-colors">
                        →
                    </span>
                )}
            </div>
            <div className="text-[26px] text-slate-900 font-light leading-none mt-2 tabular-nums">
                {typeof value === "number" ? value.toLocaleString() : value}
            </div>
            {sub && (
                <div className="text-[10px] text-slate-400 mt-1.5 font-mono truncate">{sub}</div>
            )}
        </>
    );
    const cls = cn(
        "group px-5 py-4 transition-colors",
        !last && "border-r border-slate-200",
        (href || onClick) && "hover:bg-slate-50 cursor-pointer",
    );
    if (href) {
        return isInternalHref(href) ? (
            <Link to={href} className={cls}>
                {inner}
            </Link>
        ) : (
            <a href={href} className={cls}>
                {inner}
            </a>
        );
    }
    if (onClick) {
        return (
            <button onClick={onClick} className={cn(cls, "text-left")}>
                {inner}
            </button>
        );
    }
    return <div className={cls}>{inner}</div>;
}

/**
 * PageBody — scrollable content area below the topbar/strip. No padding;
 * children paint their own structure (a table, a kanban, etc.).
 */
export function PageBody({ children, className }: { children: React.ReactNode; className?: string }) {
    return (
        <div className={cn("flex-1 min-h-0 overflow-auto", className)}>{children}</div>
    );
}

/**
 * SectionBar — secondary toolbar inside a PageBody. Same anatomy as the
 * PageTopbar but a touch slimmer (h-9) and with a lighter divider, so it
 * reads as a sub-header rather than a peer.
 */
export function SectionBar({
    label,
    count,
    children,
    className,
}: {
    label: string;
    count?: number | string;
    children?: React.ReactNode;
    className?: string;
}) {
    return (
        <div
            className={cn(
                // Single 36px row on >=md; on mobile it wraps so a full-width
                // search + filters stack instead of crowding off the edge.
                "min-h-9 md:h-9 px-5 py-1.5 md:py-0 border-b border-slate-200/60 flex flex-wrap md:flex-nowrap items-center gap-x-2.5 gap-y-1.5 shrink-0",
                className,
            )}
        >
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                {label}
            </span>
            {count !== undefined && (
                <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                    {count}
                </span>
            )}
            {children && (
                <div className="ml-auto flex items-center gap-1.5 flex-wrap justify-end min-w-0">
                    {children}
                </div>
            )}
        </div>
    );
}

/**
 * Row — generic flexible h-11 list row with a hover state and bottom
 * hairline. Compose freely; use status dots / mono IDs / pill labels
 * to taste.
 */
export function Row({
    children,
    href,
    onClick,
    className,
}: {
    children: React.ReactNode;
    href?: string;
    onClick?: () => void;
    className?: string;
}) {
    const cls = cn(
        "group h-11 px-5 flex items-center gap-3 border-b border-slate-200/60 transition-colors",
        (href || onClick) && "hover:bg-slate-50/80 cursor-pointer",
        className,
    );
    if (href) {
        return isInternalHref(href) ? (
            <Link to={href} className={cls}>{children}</Link>
        ) : (
            <a href={href} className={cls}>{children}</a>
        );
    }
    if (onClick) return <button onClick={onClick} className={cn(cls, "w-full text-left")}>{children}</button>;
    return <div className={cls}>{children}</div>;
}

/**
 * EmptyBlock — tight text-led empty state. Sized to slot inside a
 * PageBody or a section, not occupy the whole page.
 */
export function EmptyBlock({
    title,
    body,
    cta,
}: {
    title: string;
    body?: string;
    cta?: React.ReactNode;
}) {
    return (
        <div className="px-5 py-16 text-center">
            <p className="text-[12.5px] text-slate-700 font-medium mb-1">{title}</p>
            {body && (
                <p className="text-[11.5px] text-slate-400 mb-4 max-w-[34ch] mx-auto leading-relaxed">
                    {body}
                </p>
            )}
            {cta && <div className="mt-4 flex justify-center gap-1.5">{cta}</div>}
        </div>
    );
}

// ── Compatibility shims ──────────────────────────────────────────────
// The previous primitives (PageHeader, StatCard, EmptyState, PageSection)
// are still imported across many app pages. Keep thin shims so the new
// chrome lands without breaking other pages — the sweep will migrate
// each page to the new vocabulary at its own pace.

export function PageHeader({
    title,
    subtitle,
    children,
}: {
    title: string;
    subtitle?: string;
    eyebrow?: string;
    children?: React.ReactNode;
    className?: string;
}) {
    return (
        <PageTopbar eyebrow={title} subtitle={subtitle}>
            {children}
        </PageTopbar>
    );
}

export function StatCard({
    label,
    value,
    hint,
}: {
    icon?: React.ReactNode;
    iconTone?: "slate" | "blue" | "emerald" | "amber" | "red" | "violet";
    label: string;
    value: string | number;
    hint?: string;
}) {
    return <Stat label={label} value={value} sub={hint} />;
}

export function EmptyState({
    title,
    description,
    children,
}: {
    icon?: React.ReactNode;
    title: string;
    description?: string;
    children?: React.ReactNode;
}) {
    return <EmptyBlock title={title} body={description} cta={children} />;
}

export function PageSection({
    title,
    description,
    actions,
    children,
    className,
}: {
    title?: string;
    description?: string;
    actions?: React.ReactNode;
    children: React.ReactNode;
    className?: string;
}) {
    return (
        <section className={className}>
            {(title || description || actions) && (
                <SectionBar label={title ?? ""} count={undefined}>
                    {actions}
                </SectionBar>
            )}
            {children}
        </section>
    );
}
