// FormBuilder — the full-page form editor: Build (palette + live canvas +
// field settings), Design, Settings, Share and Submissions tabs. State lives
// in a local draft; saving PATCHes the whole document (last write wins).

import React from "react";
import { Link, useSearchParams } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowLeftIcon,
    ChartNoAxesColumnIcon,
    ExternalLinkIcon,
    EyeIcon,
    GlobeIcon,
    InboxIcon,
    PaletteIcon,
    SettingsIcon,
    Share2Icon,
    WrenchIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import {
    DndContext,
    DragOverlay,
    KeyboardSensor,
    PointerSensor,
    closestCenter,
    pointerWithin,
    useDraggable,
    useDroppable,
    useSensor,
    useSensors,
    type CollisionDetection,
    type DragEndEvent,
    type DragOverEvent,
    type DragStartEvent,
    type UniqueIdentifier,
} from "@dnd-kit/core";
import { sortableKeyboardCoordinates } from "@dnd-kit/sortable";
import type { LucideIcon } from "lucide-react";

import { TextInput } from "@/components/ui/field";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import { useWriteGuard } from "@/hooks/usePermission";
import { useFormsConfig, useUpdateForm } from "@/lib/api/hooks/app/forms";
import type Form from "@/lib/api/models/app/forms/Form";
import type { FormDesign, FormField, FormWrite } from "@/lib/api/models/app/forms/Form";
import { isInputType } from "@/lib/api/models/app/forms/Form";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

import AnalyticsTab from "./AnalyticsTab";
import DesignPanel from "./DesignPanel";
import FieldSettingsPanel from "./FieldSettingsPanel";
import FormPreview from "./FormPreview";
import SettingsPanel from "./SettingsPanel";
import ShareTab from "./ShareTab";
import SubmissionsTab from "./SubmissionsTab";
import {
    CANVAS_DROPPABLE_ID,
    PALETTE_PREFIX,
    insertionIndex,
    isNoopMove,
    isPaletteDrag,
    moveField,
} from "./dropSlot";
import { PALETTE, newField, paletteFor, type PaletteItem } from "./fieldCatalog";

type TabKey = "build" | "design" | "settings" | "share" | "analytics" | "submissions";

const TABS: { key: TabKey; label: string; Icon: typeof WrenchIcon }[] = [
    { key: "build", label: "Build", Icon: WrenchIcon },
    { key: "design", label: "Design", Icon: PaletteIcon },
    { key: "settings", label: "Settings", Icon: SettingsIcon },
    { key: "share", label: "Share", Icon: Share2Icon },
    { key: "analytics", label: "Analytics", Icon: ChartNoAxesColumnIcon },
    { key: "submissions", label: "Submissions", Icon: InboxIcon },
];

interface Draft {
    name: string;
    fields: FormField[];
    design: FormDesign;
    success_message: string;
    redirect_url: string;
    campaign_id: string | null;
    category_ids: string[];
    allowed_domains: string[];
    captcha_enabled: boolean;
}

function draftFrom(f: Form): Draft {
    return {
        name: f.name,
        fields: f.fields.map((x) => ({ ...x })),
        design: { ...f.design },
        success_message: f.success_message,
        redirect_url: f.redirect_url,
        campaign_id: f.campaign_id ?? null,
        category_ids: [...f.category_ids],
        allowed_domains: [...f.allowed_domains],
        captcha_enabled: f.captcha_enabled,
    };
}

const sig = (d: Draft) => JSON.stringify(d);

const STATUS_PILL: Record<Form["status"], string> = {
    draft: "bg-slate-100 text-slate-600",
    published: "bg-emerald-50 text-emerald-700",
    archived: "bg-amber-50 text-amber-700",
};

/** The chip the cursor carries; a field drag borrows its type's palette icon. */
interface DragChip {
    label: string;
    Icon: LucideIcon;
}

// Fields and the end-of-form slot are the drop targets; the canvas droppable
// is only a fence. closestCenter always names a target however far away the
// pointer is, so without the fence a palette item released back over the
// palette would still be added, and without excluding the canvas from the
// ranking its pane-sized rect out-competed the field the pointer was over.
const collisionDetection: CollisionDetection = (args) => {
    const canvas = args.droppableContainers.find((c) => c.id === CANVAS_DROPPABLE_ID)?.rect.current;
    const p = args.pointerCoordinates;
    if (p && canvas && (p.x < canvas.left || p.x > canvas.right || p.y < canvas.top || p.y > canvas.bottom)) return [];
    const droppableContainers = args.droppableContainers.filter((c) => c.id !== CANVAS_DROPPABLE_ID);
    const over = pointerWithin({ ...args, droppableContainers });
    return over.length > 0 ? over : closestCenter({ ...args, droppableContainers });
};

function PaletteButton({ item, onAdd, disabled }: { item: PaletteItem; onAdd: () => void; disabled: boolean }) {
    const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
        id: `${PALETTE_PREFIX}${item.type}`,
        data: { type: item.type },
        disabled,
    });
    return (
        <button
            ref={setNodeRef}
            type="button"
            onClick={onAdd}
            disabled={disabled}
            className={`h-8 px-2 w-full inline-flex items-center gap-2 rounded-md text-[12px] text-slate-700 hover:bg-slate-100 transition-colors text-left disabled:opacity-50 ${
                isDragging ? "opacity-40" : ""
            }`}
            {...attributes}
            {...listeners}
        >
            <item.icon className="w-3.5 h-3.5 text-slate-400" />
            {item.label}
        </button>
    );
}

export default function FormBuilder({ form }: { form: Form }) {
    const write = useWriteGuard("MANAGE_CONTACTS");
    const canEdit = write.allowed;
    const update = useUpdateForm();
    const config = useFormsConfig();

    const [params, setParams] = useSearchParams();
    const rawTab = params.get("tab") as TabKey | null;
    const tab: TabKey = rawTab && TABS.some((t) => t.key === rawTab) ? rawTab : "build";
    const setTab = (t: TabKey) =>
        setParams(
            (p) => {
                const next = new URLSearchParams(p);
                if (t === "build") next.delete("tab");
                else next.set("tab", t);
                return next;
            },
            { replace: true },
        );

    const [draft, setDraft] = React.useState<Draft>(() => draftFrom(form));
    const [status, setStatus] = React.useState<Form["status"]>(form.status);
    const [shareUrl] = React.useState(form.share_url ?? "");
    // Images are saved the moment they are picked, so they live outside the
    // draft and Discard must not roll them back.
    const [assets, setAssets] = React.useState({
        logo: form.logo_url,
        cover: form.cover_url,
        background: form.background_url,
    });
    // The last-saved draft: Discard returns here, not to the original prop.
    const savedRef = React.useRef<Draft>(draftFrom(form));
    const baselineRef = React.useRef(sig(draftFrom(form)));
    const dirty = sig(draft) !== baselineRef.current;
    const [selectedId, setSelectedId] = React.useState<string | null>(null);
    const [dragging, setDragging] = React.useState<DragChip | null>(null);
    // The slot the drag would land in, as an index into draft.fields before
    // the move. Nothing reflows during a drag, so this caret is the only thing
    // telling the user where the field goes.
    const [dropIndex, setDropIndex] = React.useState<number | null>(null);
    // Escape cancels a keyboard drag; the listener below must not also read it
    // as "clear the selection" and take the settings panel with it.
    const draggingRef = React.useRef(false);

    usePresenceResource(`form:${form.id}`, canEdit ? "editing" : "viewing");

    // Escape clears the selection; a data-floating popover keeps priority.
    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key !== "Escape") return;
            if (draggingRef.current) return;
            if (document.querySelector("[data-floating],[role='alertdialog']")) return;
            setSelectedId(null);
        }
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, []);

    const patchDraft = (patch: Partial<Draft>) => setDraft((d) => ({ ...d, ...patch }));
    const patchField = (id: string, patch: Partial<FormField>) =>
        setDraft((d) => ({ ...d, fields: d.fields.map((f) => (f.id === id ? { ...f, ...patch } : f)) }));

    function addField(item: PaletteItem, atIndex?: number) {
        const f = newField(item.type);
        setDraft((d) => {
            const fields = [...d.fields];
            fields.splice(atIndex ?? fields.length, 0, f);
            return { ...d, fields };
        });
        setSelectedId(f.id);
    }

    function deleteField(id: string) {
        setDraft((d) => ({ ...d, fields: d.fields.filter((f) => f.id !== id) }));
        setSelectedId((cur) => (cur === id ? null : cur));
    }

    function duplicateField(id: string) {
        setDraft((d) => {
            const i = d.fields.findIndex((f) => f.id === id);
            if (i < 0) return d;
            const copy: FormField = { ...d.fields[i], id: newField(d.fields[i].type).id };
            if (copy.options) copy.options = [...copy.options];
            // A duplicated field must not steal the original's contact column.
            if (copy.type !== "email") copy.map_to = "";
            const fields = [...d.fields];
            fields.splice(i + 1, 0, copy);
            return { ...d, fields };
        });
    }

    const sensors = useSensors(
        useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
        // The grip announces itself as sortable, so it has to be sortable
        // without a pointer: space picks a field up, the arrows move it.
        useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
    );
    const { setNodeRef: setCanvasRef } = useDroppable({ id: CANVAS_DROPPABLE_ID });

    const slotOf = (activeId: string, overId: string | null) => insertionIndex(draft.fields, activeId, overId);
    const isNoop = (activeId: string, at: number | null) => isNoopMove(draft.fields, activeId, at);

    function blockName(id: string): string {
        if (isPaletteDrag(id)) {
            return PALETTE.find((p) => `${PALETTE_PREFIX}${p.type}` === id)?.label ?? "Block";
        }
        const f = draft.fields.find((x) => x.id === id);
        return f ? f.label.trim() || paletteFor(f.type)?.label || "Block" : "Block";
    }

    // dnd-kit announces raw field ids by default, which is no help on the
    // keyboard path this sensor opens up: name the block and the slot instead.
    const announcements = {
        onDragStart: ({ active }: { active: { id: UniqueIdentifier } }) => `Picked up ${blockName(String(active.id))}.`,
        onDragOver: ({ active, over }: DragOverEvent) => slotMessage(String(active.id), over ? String(over.id) : null, "will be"),
        onDragEnd: ({ active, over }: DragEndEvent) => slotMessage(String(active.id), over ? String(over.id) : null, "was"),
        onDragCancel: ({ active }: { active: { id: UniqueIdentifier } }) => `${blockName(String(active.id))} was left where it was.`,
    };

    function slotMessage(activeId: string, overId: string | null, tense: "will be" | "was"): string {
        const name = blockName(activeId);
        const at = slotOf(activeId, overId);
        if (at === null || isNoop(activeId, at)) return `${name} ${tense} left where it was.`;
        if (at >= draft.fields.length) return `${name} ${tense} placed last.`;
        return `${name} ${tense} placed before ${blockName(draft.fields[at].id)}.`;
    }

    function onDragStart(e: DragStartEvent) {
        const id = String(e.active.id);
        draggingRef.current = true;
        setDropIndex(null);
        if (isPaletteDrag(id)) {
            const item = PALETTE.find((p) => `${PALETTE_PREFIX}${p.type}` === id);
            setDragging(item ? { label: item.label, Icon: item.icon } : null);
            return;
        }
        const field = draft.fields.find((f) => f.id === id);
        const item = field ? paletteFor(field.type) : undefined;
        setDragging(item ? { label: field?.label.trim() || item.label, Icon: item.icon } : null);
    }

    function onDragOver(e: DragOverEvent) {
        const activeId = String(e.active.id);
        const at = slotOf(activeId, e.over ? String(e.over.id) : null);
        setDropIndex(isNoop(activeId, at) ? null : at);
    }

    function onDragCancel() {
        draggingRef.current = false;
        setDragging(null);
        setDropIndex(null);
    }

    function onDragEnd(e: DragEndEvent) {
        draggingRef.current = false;
        setDragging(null);
        setDropIndex(null);
        const activeId = String(e.active.id);
        const at = slotOf(activeId, e.over ? String(e.over.id) : null);
        if (at === null) return;
        if (isPaletteDrag(activeId)) {
            const item = PALETTE.find((p) => `${PALETTE_PREFIX}${p.type}` === activeId);
            if (item) addField(item, at);
            return;
        }
        setDraft((d) => {
            const fields = moveField(d.fields, activeId, at);
            return fields === d.fields ? d : { ...d, fields };
        });
    }

    function writeFrom(d: Draft, s?: Form["status"]): FormWrite {
        return {
            name: d.name.trim() || "Untitled form",
            fields: d.fields,
            design: d.design,
            success_message: d.success_message,
            redirect_url: d.redirect_url,
            campaign_id: d.campaign_id,
            category_ids: d.category_ids,
            allowed_domains: d.allowed_domains,
            captcha_enabled: d.captcha_enabled,
            ...(s ? { status: s } : {}),
        };
    }

    async function save(nextStatus?: Form["status"]): Promise<boolean> {
        for (const f of draft.fields) {
            if (isInputType(f.type) && f.type !== "hidden" && !f.label.trim()) {
                toast.error("Every field needs a label");
                setTab("build");
                setSelectedId(f.id);
                return false;
            }
        }
        if (nextStatus === "published") {
            if (!draft.fields.some((f) => isInputType(f.type) && f.type !== "hidden")) {
                toast.error("Add at least one input field before publishing");
                setTab("build");
                return false;
            }
            if (!draft.fields.some((f) => f.type === "email")) {
                toast("Without an email field, submissions are stored but never become contacts", { icon: "⚠️" });
            }
            const firstInput = draft.fields.findIndex((f) => f.type !== "page_break");
            const lastInput = draft.fields.map((f) => f.type !== "page_break").lastIndexOf(true);
            if (draft.fields.some((f, i) => f.type === "page_break" && (i < firstInput || i > lastInput))) {
                toast("A page break before the first field or after the last one makes an empty page", { icon: "⚠️" });
            }
        }
        try {
            const saved = await update.mutateAsync({ id: form.id, w: writeFrom(draft, nextStatus) });
            savedRef.current = draft;
            baselineRef.current = sig(draft);
            setStatus(saved.status);
            // Force a dirty re-eval without changing content.
            setDraft((d) => ({ ...d }));
            return true;
        } catch (err) {
            toast.error(buildError(err as AppError));
            return false;
        }
    }

    const guarded = (fn: () => void) => () => write.guard(fn)({});

    const selected = draft.fields.find((f) => f.id === selectedId) ?? null;
    const captchaAvailable = config.data?.captcha_available ?? false;
    const baseUrl = config.data?.base_url ?? "";

    const canvas = (
        <div ref={setCanvasRef} className="flex-1 min-w-0 overflow-y-auto bg-slate-100/60">
            <FormPreview
                fields={draft.fields}
                design={draft.design}
                logoUrl={assets.logo}
                coverUrl={assets.cover}
                backgroundUrl={assets.background}
                selectedId={selectedId}
                editable={canEdit && tab === "build"}
                showCaptchaBadge={draft.captcha_enabled && captchaAvailable}
                previewPaging={tab === "design"}
                dropIndex={dropIndex}
                onSelect={(id) => setSelectedId(id || null)}
                onDelete={(id) => deleteField(id)}
                onDuplicate={(id) => duplicateField(id)}
            />
        </div>
    );

    return (
        <div className="h-full flex flex-col min-h-0">
            {/* Header */}
            <div className="shrink-0 px-4 h-14 flex items-center gap-3 border-b border-slate-200 bg-white">
                <Link
                    to="/app/forms"
                    className="inline-flex items-center gap-1 text-[12px] text-slate-500 hover:text-slate-900 shrink-0"
                >
                    <ArrowLeftIcon className="w-3.5 h-3.5" /> Forms
                </Link>
                <div className="w-px h-5 bg-slate-200 shrink-0" />
                <TextInput
                    value={draft.name}
                    onChange={(v) => patchDraft({ name: v })}
                    disabled={!canEdit}
                    className="max-w-[260px] font-medium"
                    placeholder="Form name"
                />
                <span className={`inline-flex items-center h-4 px-1.5 rounded text-[10px] font-medium shrink-0 ${STATUS_PILL[status]}`}>
                    {status}
                </span>
                <ResourceViewers resource={`form:${form.id}`} className="shrink-0" />
                <div className="flex-1" />
                {status === "published" && shareUrl && (
                    <a
                        href={shareUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="hidden sm:inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:bg-slate-100 shrink-0"
                    >
                        <EyeIcon className="w-3.5 h-3.5" /> View live <ExternalLinkIcon className="w-3 h-3" />
                    </a>
                )}
                {status === "published" ? (
                    <button
                        type="button"
                        onClick={guarded(() => void save("draft"))}
                        className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-slate-50 shrink-0"
                    >
                        Unpublish
                    </button>
                ) : (
                    <button
                        type="button"
                        onClick={guarded(() => void save("published"))}
                        className="h-7 px-3 rounded-md bg-sky-600 text-white text-[12px] font-medium hover:bg-sky-700 inline-flex items-center gap-1.5 shrink-0"
                    >
                        <GlobeIcon className="w-3.5 h-3.5" /> Publish
                    </button>
                )}
            </div>

            {/* Tab bar */}
            <div className="shrink-0 px-3 flex items-center gap-1 border-b border-slate-200 bg-white overflow-x-auto">
                {TABS.map(({ key, label, Icon }) => {
                    const active = tab === key;
                    return (
                        <button
                            key={key}
                            type="button"
                            onClick={() => setTab(key)}
                            className={`relative h-10 px-2.5 inline-flex items-center gap-1.5 text-[12.5px] transition-colors shrink-0 ${
                                active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-900"
                            }`}
                        >
                            <Icon className="w-3.5 h-3.5" />
                            {label}
                            {key === "submissions" && form.submissions_count > 0 && (
                                <span className="inline-flex items-center h-4 px-1 rounded bg-sky-50 text-sky-700 text-[10px] font-medium tabular-nums">
                                    {form.submissions_count.toLocaleString()}
                                </span>
                            )}
                            {active && (
                                <motion.span
                                    layoutId="form-builder-tab-underline"
                                    className="absolute left-1.5 right-1.5 -bottom-px h-0.5 rounded-full bg-sky-600"
                                    transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                />
                            )}
                        </button>
                    );
                })}
            </div>

            {/* Body */}
            <div className="flex-1 min-h-0 flex">
                {tab === "build" && (
                    <DndContext
                        sensors={sensors}
                        collisionDetection={collisionDetection}
                        onDragStart={onDragStart}
                        onDragOver={onDragOver}
                        onDragEnd={onDragEnd}
                        onDragCancel={onDragCancel}
                        accessibility={{ announcements }}
                    >
                        <aside className="hidden md:flex w-52 shrink-0 flex-col gap-3 border-r border-slate-200 bg-white overflow-y-auto p-3">
                            {(["Fields", "Layout"] as const).map((group) => (
                                <div key={group}>
                                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium px-2 mb-1">{group}</div>
                                    <div className="flex flex-col">
                                        {PALETTE.filter((p) => p.group === group).map((item) => (
                                            <PaletteButton
                                                key={item.type}
                                                item={item}
                                                disabled={!canEdit}
                                                onAdd={() => {
                                                    const at = selectedId
                                                        ? draft.fields.findIndex((f) => f.id === selectedId) + 1
                                                        : undefined;
                                                    addField(item, at);
                                                }}
                                            />
                                        ))}
                                    </div>
                                </div>
                            ))}
                            <p className="text-[10.5px] text-slate-400 px-2">Click to add, or drag onto the canvas.</p>
                        </aside>
                        {canvas}
                        <aside className="hidden lg:block w-80 shrink-0 border-l border-slate-200 bg-white overflow-y-auto">
                            {selected ? (
                                <FieldSettingsPanel
                                    key={selected.id}
                                    field={selected}
                                    fields={draft.fields}
                                    onChange={(patch) => patchField(selected.id, patch)}
                                />
                            ) : (
                                <div className="p-6 text-[12px] text-slate-400">Select a field on the canvas to edit it.</div>
                            )}
                        </aside>
                        {/* A dragged field rides the cursor as a chip and its
                            row stays put, so the canvas keeps its shape. */}
                        <DragOverlay dropAnimation={null}>
                            {dragging && (
                                <div className="h-8 px-2 inline-flex items-center gap-2 rounded-md border border-sky-200 bg-white shadow-md text-[12px] text-slate-700 max-w-[220px]">
                                    <dragging.Icon className="w-3.5 h-3.5 shrink-0 text-sky-600" />
                                    <span className="truncate">{dragging.label}</span>
                                </div>
                            )}
                        </DragOverlay>
                    </DndContext>
                )}

                {tab === "design" && (
                    <>
                        {canvas}
                        <aside className="w-full sm:w-80 shrink-0 border-l border-slate-200 bg-white overflow-y-auto">
                            <DesignPanel
                                formId={form.id}
                                design={draft.design}
                                logoUrl={assets.logo}
                                coverUrl={assets.cover}
                                backgroundUrl={assets.background}
                                canEdit={canEdit}
                                onChange={(patch) => patchDraft({ design: { ...draft.design, ...patch } })}
                                onReplaceDesign={(design) => patchDraft({ design })}
                                onAssetsSaved={(f) =>
                                    setAssets({ logo: f.logo_url, cover: f.cover_url, background: f.background_url })
                                }
                            />
                        </aside>
                    </>
                )}

                {tab === "settings" && (
                    <div className="flex-1 overflow-y-auto">
                        <SettingsPanel draft={draft} captchaAvailable={captchaAvailable} onChange={(patch) => patchDraft(patch)} />
                    </div>
                )}

                {tab === "share" && (
                    <div className="flex-1 overflow-y-auto">
                        <ShareTab form={{ ...form, status, name: draft.name }} baseUrl={baseUrl} />
                    </div>
                )}

                {tab === "analytics" && (
                    <div className="flex-1 overflow-y-auto">
                        <AnalyticsTab form={{ ...form, fields: draft.fields }} />
                    </div>
                )}

                {tab === "submissions" && (
                    <div className="flex-1 overflow-y-auto">
                        <SubmissionsTab form={{ ...form, fields: draft.fields }} />
                    </div>
                )}
            </div>

            {/* Save bar */}
            <AnimatePresence>
                {dirty && canEdit && (
                    <motion.div
                        initial={{ y: 56, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 56, opacity: 0 }}
                        transition={{ type: "spring", damping: 28, stiffness: 360 }}
                        className="fixed bottom-4 left-1/2 -translate-x-1/2 z-30 flex items-center gap-2 rounded-lg border border-slate-200 bg-white shadow-lg px-3 py-2"
                    >
                        <span className="text-[12px] text-slate-600">Unsaved changes</span>
                        <button
                            type="button"
                            onClick={() => {
                                setDraft(savedRef.current);
                                baselineRef.current = sig(savedRef.current);
                                setSelectedId(null);
                            }}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:bg-slate-100"
                        >
                            Discard
                        </button>
                        <button
                            type="button"
                            disabled={update.isPending}
                            onClick={() => void save()}
                            className="h-7 px-3 rounded-md bg-sky-600 text-white text-[12px] font-medium hover:bg-sky-700 disabled:opacity-60"
                        >
                            {update.isPending ? "Saving…" : "Save"}
                        </button>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
