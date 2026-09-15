// FormPreview — the builder's live canvas. Renders the draft through the same
// designCore + form-theme.css pipeline as the hosted page, with selection,
// drag-reorder (dnd-kit) and quick actions layered on top in build mode, and
// a full paged/focus preview in design mode.

import React from "react";
import { ChevronLeftIcon, ChevronRightIcon, CopyIcon, GripVerticalIcon, Trash2Icon } from "lucide-react";
import { useDroppable } from "@dnd-kit/core";
import { SortableContext, useSortable, type SortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import { cn } from "@/lib/utils";
import type { FormDesign, FormField } from "@/lib/api/models/app/forms/Form";

import "./form-theme.css";
import { designVars, ensureFont, focusSteps, resolveDesign, splitPages } from "./designCore";
import { CANVAS_END_DROPPABLE_ID, isHalfWidth, rowStarts } from "./dropSlot";
import useBrand from "@/hooks/useBrand";

// Fields wrap into rows and a half-width one is half a row wide, so no
// built-in strategy can say where a neighbour lands: rectSortingStrategy
// scaled every sibling by the ratio of two mismatched rects and tore fields
// out of the card (#497). Nothing moves during a drag; the caret says where
// the field lands instead.
const noReflow: SortingStrategy = () => null;

/** Which edge of a field the insertion caret sits against, if any. */
type CaretSide = "top" | "left" | null;

// The caret sits in the gutter between two rows, or between two half-width
// fields on one row, so it has to be offset by half the form's own gap.
function DropCaret({ side }: { side: NonNullable<CaretSide> }) {
    const base = "absolute z-20 rounded-full bg-sky-500 pointer-events-none";
    return side === "left" ? (
        <span className={`${base} top-0 bottom-0 w-[3px]`} style={{ left: "calc(var(--wf-gap, 16px) / -2 - 1.5px)" }} aria-hidden />
    ) : (
        <span className={`${base} left-0 right-0 h-[3px]`} style={{ top: "calc(var(--wf-gap, 16px) / -2 - 1.5px)" }} aria-hidden />
    );
}

// The end-of-form drop target. Without it the only way to reach the last slot
// would be to out-guess closestCenter, which is why dragging the first field
// to the bottom did nothing.
function DropEndZone({ caret, children }: { caret: boolean; children: React.ReactNode }) {
    const { setNodeRef } = useDroppable({ id: CANVAS_END_DROPPABLE_ID });
    return (
        <div ref={setNodeRef} className="btnrow" style={{ position: "relative" }}>
            {caret && <DropCaret side="top" />}
            {children}
        </div>
    );
}

// FieldBody renders one block with the shared .wf classes. Inputs are real
// but inert (readOnly/disabled, tabIndex -1) so form-theme.css styles them
// exactly like the hosted page.
function FieldBody({ field }: { field: FormField }) {
    const label = (
        <label className="l">
            {field.label || <em style={{ color: "var(--wf-placeholder)" }}>Untitled</em>}
            {field.required && <span className="req"> *</span>}
        </label>
    );
    const help = field.help_text ? <p className="help">{field.help_text}</p> : null;
    const opts = field.options ?? [];

    switch (field.type) {
        case "heading":
            return <h2 className="h">{field.label || "Heading"}</h2>;
        case "paragraph":
            return <p className="p">{field.value || "Text block"}</p>;
        case "divider":
            return <hr className="d" />;
        case "hidden":
            return (
                <div
                    style={{
                        fontSize: 11.5,
                        color: "var(--wf-placeholder)",
                        border: "1px dashed var(--wf-input-border)",
                        borderRadius: "var(--wf-input-radius)",
                        padding: "6px 10px",
                    }}
                >
                    Hidden field · {field.id}
                    {field.value ? ` = ${field.value}` : ""} (not shown to visitors)
                </div>
            );
        case "textarea":
            return (
                <div>
                    {label}
                    <textarea readOnly tabIndex={-1} rows={field.rows || 4} placeholder={field.placeholder} value="" />
                    {help}
                </div>
            );
        case "select":
            return (
                <div>
                    {label}
                    <select disabled tabIndex={-1}>
                        <option>{field.placeholder || "Select…"}</option>
                    </select>
                    {help}
                </div>
            );
        case "radio":
        case "checkboxes":
            return (
                <div>
                    {label}
                    <div className="opts">
                        {opts.map((o, i) => (
                            <label key={i} className="opt">
                                <input type={field.type === "radio" ? "radio" : "checkbox"} disabled tabIndex={-1} />
                                {o}
                            </label>
                        ))}
                    </div>
                    {help}
                </div>
            );
        case "checkbox":
            return (
                <div>
                    <label className="opt">
                        <input type="checkbox" disabled tabIndex={-1} />
                        <span>
                            {field.placeholder || field.label || "I agree"}
                            {field.required && <span className="req"> *</span>}
                        </span>
                    </label>
                    {help}
                </div>
            );
        default:
            return (
                <div>
                    {label}
                    <input type="text" readOnly tabIndex={-1} placeholder={field.placeholder} value="" />
                    {help}
                </div>
            );
    }
}

// PageBreakRow is the build-tab stand-in for a page_break: a hairline with an
// accent-tinted pill naming the page the break opens.
function PageBreakRow({ pageNum, label }: { pageNum: number; label: string }) {
    const line = (
        <span className="flex-1 border-t" style={{ borderColor: "var(--wf-accent)", opacity: 0.35 }} aria-hidden />
    );
    return (
        <div className="w-full flex items-center gap-2 py-1">
            {line}
            <span
                className="px-2 py-0.5 rounded-full text-[11px] font-medium whitespace-nowrap"
                style={{ color: "var(--wf-accent)", background: "color-mix(in srgb, var(--wf-accent) 10%, transparent)" }}
            >
                Page {pageNum}
                {label ? ` · ${label}` : ""}
            </span>
            {line}
        </div>
    );
}

function CaptchaBadge({ caret }: { caret?: boolean }) {
    return (
        <div
            className="fld"
            style={{
                position: "relative",
                border: "1px dashed var(--wf-input-border)",
                borderRadius: "var(--wf-input-radius)",
                padding: "10px 12px",
                fontSize: 12,
                color: "var(--wf-placeholder)",
            }}
        >
            {caret && <DropCaret side="top" />}
            Spam protection challenge appears here
        </div>
    );
}

function SortableField({
    field,
    pageNum,
    selected,
    editable,
    caret,
    onSelect,
    onDelete,
    onDuplicate,
}: {
    field: FormField;
    /** For page_break fields: the number of the page this break opens. */
    pageNum: number;
    selected: boolean;
    editable: boolean;
    /** Edge to draw the insertion caret against while a drag is in flight. */
    caret: CaretSide;
    onSelect: (id: string) => void;
    onDelete: (id: string) => void;
    onDuplicate: (id: string) => void;
}) {
    const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging, isSorting } =
        useSortable({ id: field.id, disabled: !editable });
    return (
        <div
            ref={setNodeRef}
            style={{
                // Translate only. CSS.Transform also emits the scale dnd-kit
                // derives from two mismatched rects, which is what stretched
                // fields across the canvas (#497).
                transform: CSS.Translate.toString(transform),
                transition,
                opacity: isDragging ? 0.4 : 1,
            }}
            onClick={(e) => {
                e.stopPropagation();
                onSelect(field.id);
            }}
            // .fld carries the width, so a half field stacks below 480px in
            // the canvas exactly as it does on the hosted page.
            className={cn(
                "fld",
                isHalfWidth(field) && "half",
                "relative rounded-md cursor-pointer group/field",
                selected && "ring-2 ring-sky-400 ring-offset-2",
                !selected && !isSorting && "hover:ring-1 hover:ring-sky-200 hover:ring-offset-2",
            )}
            data-field-id={field.id}
        >
            {caret && <DropCaret side={caret} />}
            <div style={{ pointerEvents: "none" }}>
                {field.type === "page_break" ? (
                    <PageBreakRow pageNum={pageNum} label={field.label} />
                ) : (
                    <FieldBody field={field} />
                )}
            </div>
            {editable && (
                <div
                    className={cn(
                        "absolute -top-2.5 right-1 z-10 flex items-center gap-0.5 rounded-md border border-slate-200 bg-white shadow-sm px-0.5 transition-opacity",
                        // A toolbar popping up under the cursor mid-drag reads
                        // as a second drop target, so every one goes away.
                        isSorting
                            ? "opacity-0 pointer-events-none"
                            : selected
                              ? "opacity-100"
                              : "opacity-100 md:opacity-0 md:group-hover/field:opacity-100",
                    )}
                    onClick={(e) => e.stopPropagation()}
                >
                    <button
                        ref={setActivatorNodeRef}
                        type="button"
                        aria-label="Drag to reorder"
                        // touch-action must be none or a touch drag scrolls the
                        // canvas instead of picking the field up.
                        className="size-5 inline-flex items-center justify-center text-slate-400 hover:text-slate-700 cursor-grab active:cursor-grabbing touch-none"
                        {...attributes}
                        {...listeners}
                    >
                        <GripVerticalIcon className="w-3 h-3" />
                    </button>
                    <button
                        type="button"
                        aria-label="Duplicate field"
                        onClick={() => onDuplicate(field.id)}
                        className="size-5 inline-flex items-center justify-center text-slate-400 hover:text-slate-700"
                    >
                        <CopyIcon className="w-3 h-3" />
                    </button>
                    <button
                        type="button"
                        aria-label="Delete field"
                        onClick={() => onDelete(field.id)}
                        className="size-5 inline-flex items-center justify-center text-slate-400 hover:text-rose-600"
                    >
                        <Trash2Icon className="w-3 h-3" />
                    </button>
                </div>
            )}
        </div>
    );
}

// PreviewPager is the dashboard-styled page switcher above the design-tab
// canvas; it lives outside the .wf shell on purpose.
function PreviewPager({
    focus,
    total,
    page,
    onPage,
}: {
    focus: boolean;
    total: number;
    page: number;
    onPage: (p: number) => void;
}) {
    return (
        <div className="shrink-0 flex items-center justify-center gap-1 py-2 px-3 bg-white border-b border-slate-200">
            {focus ? (
                <>
                    <button
                        type="button"
                        aria-label="Previous question"
                        disabled={page === 0}
                        onClick={() => onPage(page - 1)}
                        className="size-6 inline-flex items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 disabled:opacity-40"
                    >
                        <ChevronLeftIcon className="w-3.5 h-3.5" />
                    </button>
                    <span className="px-1 text-[12px] text-slate-600 tabular-nums">
                        {page + 1}/{total}
                    </span>
                    <button
                        type="button"
                        aria-label="Next question"
                        disabled={page >= total - 1}
                        onClick={() => onPage(page + 1)}
                        className="size-6 inline-flex items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 disabled:opacity-40"
                    >
                        <ChevronRightIcon className="w-3.5 h-3.5" />
                    </button>
                </>
            ) : (
                Array.from({ length: total }, (_, i) => (
                    <button
                        key={i}
                        type="button"
                        aria-label={`Page ${i + 1}`}
                        onClick={() => onPage(i)}
                        className={`h-6 min-w-6 px-1.5 rounded-full text-[11px] font-medium tabular-nums transition-colors ${
                            i === page ? "bg-slate-900 text-white" : "bg-slate-100 text-slate-600 hover:bg-slate-200"
                        }`}
                    >
                        {i + 1}
                    </button>
                ))
            )}
        </div>
    );
}

export default function FormPreview({
    fields,
    design,
    logoUrl,
    coverUrl,
    backgroundUrl,
    selectedId,
    editable,
    showCaptchaBadge,
    previewPaging,
    dropIndex,
    onSelect,
    onDelete,
    onDuplicate,
}: {
    fields: FormField[];
    design: FormDesign;
    logoUrl?: string;
    coverUrl?: string;
    backgroundUrl?: string;
    selectedId: string | null;
    /** false renders a plain preview with no selection/drag affordances. */
    editable: boolean;
    /** Show the "protected by captcha" placeholder above the button. */
    showCaptchaBadge?: boolean;
    /** true renders the paged/focus preview instead of the flat build list. */
    previewPaging?: boolean;
    /** Index a dragged field or palette item would land at; null when idle. */
    dropIndex?: number | null;
    onSelect: (id: string) => void;
    onDelete: (id: string) => void;
    onDuplicate: (id: string) => void;
}) {
    const r = React.useMemo(() => resolveDesign(design), [design]);
    React.useEffect(() => ensureFont(r), [r]);
    // Same rule as the served page: no attribution when this deployment
    // configured none, so the preview is what a visitor will actually see.
    const brand = useBrand();

    const [page, setPage] = React.useState(0);
    const screens = React.useMemo(
        () =>
            r.mode === "focus"
                ? focusSteps(fields).map((step) => ({ title: "", fields: step }))
                : splitPages(fields),
        [fields, r.mode],
    );
    const total = Math.max(screens.length, 1);
    const cur = Math.min(page, total - 1);
    const screen = screens[cur] ?? { title: "", fields: [] };
    const isLast = cur >= total - 1;
    const shown = screen.fields.filter((f) => f.type !== "hidden" && f.type !== "page_break");

    // Page numbers for build-tab break rows: the first break opens page 2.
    let breakCount = 0;
    const breakPage = fields.map((f) => (f.type === "page_break" ? ++breakCount + 1 : 0));

    const logo = logoUrl ? <img className="wf-logo" src={logoUrl} alt="" /> : null;
    // With a header bar the logo belongs to it, not to the body below.
    // The header only claims the logo when it is set to show one; otherwise the
    // logo keeps its own placement and the header carries just the title.
    const headerLogo = r.headerEnabled && r.headerShowLogo ? logo : null;
    const bodyLogo = headerLogo ? null : logo;
    const headerEl = r.headerEnabled ? (
        <header
            className={cn(
                "wf-header",
                `h-${r.headerAlign}`,
                r.headerPlacement === "inline" && "wf-header--inline",
                r.headerSticky && "is-sticky",
            )}
        >
            {headerLogo}
            {r.headerTitle && <span className="wf-header-title">{r.headerTitle}</span>}
            {/* An empty bar reads as a stray line in the canvas, so say what
                belongs in it instead of showing nothing. */}
            {!headerLogo && !r.headerTitle && (
                <span className="wf-header-title" style={{ color: "var(--wf-placeholder)", fontWeight: 400 }}>
                    Add a logo or a header title
                </span>
            )}
        </header>
    ) : null;
    const submitBtn = (
        <button type="button" tabIndex={-1} className={r.btnFullWidth ? "submit full" : "submit"} style={{ pointerEvents: "none" }}>
            {r.btnLabel}
        </button>
    );

    // A caret for a slot inside a row is vertical; everywhere else it is a
    // rule above the row.
    const opensRow = rowStarts(fields);
    const caretFor = (i: number): CaretSide => (dropIndex === i ? (opensRow[i] ? "top" : "left") : null);
    const endCaret = fields.length > 0 && dropIndex === fields.length;

    const buildList = (
        <SortableContext items={fields.map((f) => f.id)} strategy={noReflow}>
            <div className="grid">
                {fields.length === 0 && (
                    <div
                        className={cn(
                            "w-full rounded-md border-2 border-dashed p-8 text-center text-[12.5px] transition-colors",
                            dropIndex != null ? "border-sky-400 bg-sky-50/60 text-sky-700" : "border-slate-200 text-slate-400",
                        )}
                    >
                        Add fields from the left panel
                    </div>
                )}
                {fields.map((f, i) => (
                    <SortableField
                        key={f.id}
                        field={f}
                        pageNum={breakPage[i]}
                        selected={selectedId === f.id}
                        editable={editable}
                        caret={caretFor(i)}
                        onSelect={onSelect}
                        onDelete={onDelete}
                        onDuplicate={onDuplicate}
                    />
                ))}
                {showCaptchaBadge && <CaptchaBadge caret={endCaret} />}
                {/* The last slot is above the captcha badge when there is one,
                    so the caret hangs off whichever comes first. */}
                <DropEndZone caret={endCaret && !showCaptchaBadge}>{submitBtn}</DropEndZone>
            </div>
        </SortableContext>
    );

    // Mirrors the hosted FormRenderer's per-screen markup, minus interactivity.
    const pagedPreview = (
        <>
            {r.showProgress && total > 1 && (
                <div className="wf-progress" role="presentation">
                    <span style={{ width: `${((cur + 1) / total) * 100}%` }} />
                </div>
            )}
            <div key={cur} className="wf-step" data-dir="fwd">
                <div className="grid">
                    {r.mode === "classic" && screen.title && <p className="wf-pagetitle">{screen.title}</p>}
                    {shown.length === 0 && (
                        <div className="w-full rounded-md border-2 border-dashed border-slate-200 p-8 text-center text-[12.5px] text-slate-400">
                            Nothing on this page yet
                        </div>
                    )}
                    {shown.map((f) => (
                        <div key={f.id} className={f.width === "half" ? "fld half" : "fld"} style={{ pointerEvents: "none" }}>
                            <FieldBody field={f} />
                        </div>
                    ))}
                    {isLast && showCaptchaBadge && <CaptchaBadge />}
                    {isLast ? (
                        <div className={cur > 0 ? "wf-pagenav" : "btnrow"}>
                            {cur > 0 && (
                                <button type="button" className="wf-back" onClick={() => setPage(cur - 1)}>
                                    Back
                                </button>
                            )}
                            {submitBtn}
                        </div>
                    ) : (
                        <div className="wf-pagenav">
                            {cur > 0 ? (
                                <button type="button" className="wf-back" onClick={() => setPage(cur - 1)}>
                                    Back
                                </button>
                            ) : (
                                <span />
                            )}
                            <button type="button" className="submit" onClick={() => setPage(cur + 1)}>
                                Next
                            </button>
                        </div>
                    )}
                    {r.mode === "focus" && <p className="wf-hint">Press Enter to continue</p>}
                </div>
            </div>
        </>
    );

    return (
        <div className="min-h-full w-full flex flex-col">
            {previewPaging && total > 1 && (
                <PreviewPager focus={r.mode === "focus"} total={total} page={cur} onPage={setPage} />
            )}
            <div
                // wf-inline pins the background layers to this scroll pane
                // instead of the viewport; minHeight overrides the 100vh.
                className={cn("wf", "wf-inline", "flex-1", `layout-${r.layout}`, `mode-${r.mode}`, `align-${r.align}`)}
                style={
                    {
                        ...designVars(r),
                        ...(backgroundUrl ? { "--wf-bg-image": `url(${JSON.stringify(backgroundUrl)})` } : {}),
                        minHeight: 0,
                    } as React.CSSProperties
                }
                onClick={() => onSelect("")}
            >
                {r.headerPlacement === "page" && headerEl}
                <div className="wf-body" style={{ minHeight: 0 }}>
                    {r.layout === "split" && (
                        <aside
                            className="wf-cover"
                            style={{
                                minHeight: 0,
                                ...(coverUrl ? { backgroundImage: `url(${JSON.stringify(coverUrl)})` } : {}),
                            }}
                        >
                            {bodyLogo}
                            {r.coverTitle && <h2 className="wf-cover-title">{r.coverTitle}</h2>}
                            {r.coverSubtitle && <p className="wf-cover-sub">{r.coverSubtitle}</p>}
                        </aside>
                    )}
                    <main className="wf-main" style={{ minHeight: 0 }}>
                        {r.layout !== "split" && r.logoOnPage && bodyLogo}
                        <div className={r.layout === "card" ? "card" : "wf-bare"}>
                            {r.headerPlacement === "inline" && headerEl}
                            {r.layout !== "split" && !r.logoOnPage && bodyLogo}
                            {previewPaging ? pagedPreview : buildList}
                        </div>
                        {brand.website_url && (
                            <div className="brand">
                                <a href={brand.website_url} target="_blank" rel="noopener noreferrer" onClick={(e) => e.preventDefault()}>
                                    Powered by {brand.name}
                                </a>
                            </div>
                        )}
                    </main>
                </div>
            </div>
        </div>
    );
}
