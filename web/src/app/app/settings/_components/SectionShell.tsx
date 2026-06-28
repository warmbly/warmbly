// Shared primitives for the Settings outlet.
//
// We dropped the per-item Card pattern because the page felt like a
// pile of rectangles. The new shape is a continuous document:
//
//   <SectionShell title="…" description="…">
//     <Section eyebrow="Identity" description="…">
//       <Row label="First name" description="…">
//         <TextInput … />
//       </Row>
//       <Row label="Email">
//         <TextInput disabled … />
//       </Row>
//     </Section>
//
//     <Section eyebrow="Avatar">
//       …
//     </Section>
//   </SectionShell>
//
// Sections are separated by a single hairline; rows inside a section
// are also separated by hairlines. There is no outer box.

import React from "react";

export function SectionShell({
    title,
    description,
    actions,
    children,
}: {
    title: string;
    description?: string;
    actions?: React.ReactNode;
    children: React.ReactNode;
}) {
    return (
        <div>
            <div className="px-4 pt-5 pb-4 md:px-8 md:pt-7 md:pb-5 flex flex-wrap md:flex-nowrap items-start gap-4 border-b border-slate-200/70">
                <div className="min-w-0 flex-1 basis-48 md:basis-0">
                    <h2 className="text-[16px] font-semibold text-slate-900 tracking-tight">
                        {title}
                    </h2>
                    {description && (
                        <p className="text-[12.5px] text-slate-500 mt-0.5 leading-relaxed">
                            {description}
                        </p>
                    )}
                </div>
                {actions && <div className="flex items-center gap-1.5 shrink-0">{actions}</div>}
            </div>
            <div className="divide-y divide-slate-200/70">{children}</div>
        </div>
    );
}

/**
 * A titled group inside the SectionShell. Default `stack` layout puts a
 * compact header above and gives the content the full panel width so
 * wide content (plan grids, tables) is never squeezed into a narrow
 * column. Pass `layout="split"` for the older title-rail look when a
 * section is just a couple of short key/value rows.
 */
export function Section({
    eyebrow,
    description,
    actions,
    children,
    layout = "stack",
    className,
}: {
    eyebrow: string;
    description?: string;
    actions?: React.ReactNode;
    children: React.ReactNode;
    layout?: "stack" | "split";
    className?: string;
}) {
    if (layout === "split") {
        return (
            <section className={`px-4 py-5 md:px-8 md:py-6 grid grid-cols-1 lg:grid-cols-[200px_1fr] gap-6 ${className ?? ""}`}>
                <header className="flex flex-col">
                    <div className="flex items-center gap-2 mb-1">
                        <h3 className="text-[12.5px] font-semibold text-slate-900 tracking-tight">
                            {eyebrow}
                        </h3>
                        {actions}
                    </div>
                    {description && (
                        <p className="text-[11.5px] text-slate-500 leading-relaxed">{description}</p>
                    )}
                </header>
                <div className="min-w-0 space-y-3">{children}</div>
            </section>
        );
    }
    return (
        <section className={`px-4 py-5 md:px-8 md:py-6 ${className ?? ""}`}>
            <header className="mb-3 flex flex-wrap items-center gap-x-2 gap-y-1">
                <h3 className="text-[12.5px] font-semibold text-slate-900 tracking-tight">
                    {eyebrow}
                </h3>
                {actions && <div className="flex items-center gap-1.5 ml-auto">{actions}</div>}
                {description && (
                    <p className="basis-full text-[11.5px] text-slate-500 leading-relaxed">
                        {description}
                    </p>
                )}
            </header>
            <div className="min-w-0 space-y-3">{children}</div>
        </section>
    );
}

/**
 * A single setting row: label + optional sub-description on the
 * left, control on the right. Stacks on small screens.
 */
export function Row({
    label,
    description,
    children,
    align = "center",
    danger,
}: {
    label?: React.ReactNode;
    description?: React.ReactNode;
    children?: React.ReactNode;
    align?: "center" | "start";
    danger?: boolean;
}) {
    return (
        <div
            className={`flex flex-col sm:flex-row gap-2 sm:gap-4 ${
                align === "start" ? "sm:items-start" : "sm:items-center"
            }`}
        >
            {(label || description) && (
                <div className="min-w-0 flex-1">
                    {label && (
                        <div
                            className={`text-[12.5px] font-medium leading-tight ${
                                danger ? "text-red-700" : "text-slate-900"
                            }`}
                        >
                            {label}
                        </div>
                    )}
                    {description && (
                        <div className="text-[11.5px] text-slate-500 leading-tight mt-0.5">
                            {description}
                        </div>
                    )}
                </div>
            )}
            {children && (
                <div className={`shrink-0 ${label || description ? "sm:ml-auto" : "w-full"}`}>
                    {children}
                </div>
            )}
        </div>
    );
}

/**
 * Thin horizontal toggle, used inside <Row>. Controlled.
 */
export function Toggle({
    on,
    onChange,
    disabled,
}: {
    on: boolean;
    onChange: (next: boolean) => void;
    disabled?: boolean;
}) {
    return (
        <button
            type="button"
            onClick={() => !disabled && onChange(!on)}
            disabled={disabled}
            role="switch"
            aria-checked={on}
            className={`relative h-4 w-7 rounded-full transition-colors shrink-0 after:absolute after:-inset-3 after:content-[''] ${
                on ? "bg-slate-900" : "bg-slate-200"
            } ${disabled ? "opacity-50 cursor-not-allowed" : ""}`}
        >
            <span
                className={`absolute top-0.5 left-0.5 size-3 rounded-full bg-white transition-transform ${
                    on ? "translate-x-3" : "translate-x-0"
                }`}
            />
        </button>
    );
}

/**
 * Self-contained toggle row — toggles its own state. Keep the
 * stateless <Toggle> + <Row> combo when the state lives in the
 * parent.
 */
export function ToggleRow({
    label,
    description,
    defaultOn,
    checked,
    onChange,
    disabled,
}: {
    label: string;
    description: string;
    defaultOn?: boolean;
    /** Controlled value. When provided, the row is controlled and `onChange` fires on toggle. */
    checked?: boolean;
    onChange?: (next: boolean) => void;
    disabled?: boolean;
}) {
    const [internal, setInternal] = React.useState(!!defaultOn);
    const controlled = checked !== undefined;
    const on = controlled ? !!checked : internal;
    return (
        <Row label={label} description={description}>
            <Toggle
                on={on}
                disabled={disabled}
                onChange={(next) => {
                    if (!controlled) setInternal(next);
                    onChange?.(next);
                }}
            />
        </Row>
    );
}

/**
 * Action row — label + description + button on the right. Optional
 * pill for state (e.g. "Off" / "On").
 */
export function RowLink({
    title,
    description,
    cta,
    statusLabel,
    statusTone = "muted",
    onClick,
}: {
    title: string;
    description: string;
    cta: string;
    statusLabel?: string;
    statusTone?: "muted" | "ok" | "warn";
    onClick: () => void;
}) {
    return (
        <Row
            label={
                statusLabel ? (
                    <span className="inline-flex items-center gap-2">
                        {title}
                        <span
                            className={`text-[10px] uppercase tracking-[0.08em] font-medium rounded-sm px-1 ${
                                statusTone === "ok"
                                    ? "bg-emerald-50 text-emerald-700"
                                    : statusTone === "warn"
                                        ? "bg-amber-50 text-amber-700"
                                        : "bg-slate-100 text-slate-500"
                            }`}
                        >
                            {statusLabel}
                        </span>
                    </span>
                ) : (
                    title
                )
            }
            description={description}
        >
            <button
                type="button"
                onClick={onClick}
                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors"
            >
                {cta}
            </button>
        </Row>
    );
}

/**
 * Wraps tabular content (members table, invoices, etc.) so the table
 * paints cleanly inside a Section's content column.
 */
export function TableSurface({ children }: { children: React.ReactNode }) {
    return (
        <div className="rounded-md border border-slate-200 overflow-hidden bg-white">
            <div className="overflow-x-auto">{children}</div>
        </div>
    );
}

/**
 * Legacy <Card> kept so older settings pages still compile while we
 * migrate them off. New code should compose Section + Row instead.
 */
export function Card({
    title,
    description,
    children,
    footer,
    className,
}: {
    title?: string;
    description?: string;
    children: React.ReactNode;
    footer?: React.ReactNode;
    className?: string;
}) {
    return (
        <div className={`rounded-md border border-slate-200 bg-white overflow-hidden ${className ?? ""}`}>
            {(title || description) && (
                <div className="px-4 py-3 border-b border-slate-200">
                    {title && (
                        <div className="text-[12.5px] font-semibold text-slate-900">{title}</div>
                    )}
                    {description && (
                        <p className="text-[11.5px] text-slate-500 mt-0.5 leading-relaxed">
                            {description}
                        </p>
                    )}
                </div>
            )}
            <div className="px-4 py-4">{children}</div>
            {footer && (
                <div className="px-3 h-12 border-t border-slate-200 bg-slate-50/40 flex items-center gap-1.5">
                    {footer}
                </div>
            )}
        </div>
    );
}

export function RolePill({ role, color }: { role: string; color?: string }) {
    // Roles are data: tint from the role's stored color when known;
    // owner keeps its fixed sky accent, anything else falls back to slate.
    const cls =
        role === "owner"
            ? "bg-sky-50 text-sky-700 border-sky-100"
            : color
                ? ""
                : "bg-slate-50 text-slate-600 border-slate-200";
    const style =
        role !== "owner" && color
            ? { backgroundColor: `${color}14`, borderColor: `${color}55`, color }
            : undefined;
    return (
        <span
            style={style}
            className={`inline-flex items-center text-[10px] uppercase tracking-[0.08em] font-semibold rounded-sm px-1.5 py-0.5 border ${cls}`}
        >
            {role}
        </span>
    );
}

export function safeEmail(s: string | undefined | null): string {
    return (s ?? "").trim();
}

export function initials(email: string | undefined | null, fallback = "?") {
    const e = safeEmail(email);
    if (!e) return fallback.slice(0, 2).toUpperCase();
    return e.slice(0, 2).toUpperCase();
}
