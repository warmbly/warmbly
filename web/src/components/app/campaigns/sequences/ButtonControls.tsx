// Insert and edit for the campaign body's call-to-action button (issue #433).
//
// The toolbar places a default button and selects it, so the bar that edits it
// is already open; everything after that happens there, the way an image's size
// and alt text do. The markup it writes lives in nodes/EmailButtonNode.ts.

import {
    AlignCenterIcon,
    AlignLeftIcon,
    AlignRightIcon,
    CircleIcon,
    MousePointerClickIcon,
    SquareIcon,
    SquircleIcon,
    StretchHorizontalIcon,
    Trash2Icon,
} from "lucide-react";
import type { Editor } from "@tiptap/react";
import { BubbleBtn, BubbleDivider, NodeBubble, selectedNode, useNodeAnchor } from "./NodeBubble";
import {
    BUTTON_RADII,
    BUTTON_SIZES,
    BUTTON_SWATCHES,
    BUTTON_DEFAULT_LABEL,
    readableTextColor,
    type ButtonAlign,
} from "./nodes/EmailButtonNode";
import { absoluteHref } from "./nodes/EmailImageNode";

const ALIGNMENTS: { value: ButtonAlign; title: string; Icon: typeof AlignLeftIcon }[] = [
    { value: "left", title: "Align left", Icon: AlignLeftIcon },
    { value: "center", title: "Centre", Icon: AlignCenterIcon },
    { value: "right", title: "Align right", Icon: AlignRightIcon },
];

const CORNER_ICONS = [SquareIcon, SquircleIcon, CircleIcon];

export function ButtonInsert({ editor }: { editor: Editor }) {
    return (
        <button
            type="button"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => editor.chain().focus().insertEmailButton().run()}
            title="Insert a call-to-action button"
            className="size-7 inline-flex items-center justify-center rounded text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-900"
        >
            <MousePointerClickIcon className="w-3.5 h-3.5" />
        </button>
    );
}

export function ButtonBubble({ editor }: { editor: Editor }) {
    const selection = selectedNode(editor, "emailButton");
    const anchor = useNodeAnchor(editor, selection?.pos ?? null);

    if (typeof document === "undefined" || !selection || !anchor) return null;

    const label = (selection.attrs.label as string) ?? "";
    const href = (selection.attrs.href as string) ?? "";
    const background = (selection.attrs.background as string) ?? "";
    const padding = selection.attrs.padding as string;
    const radius = selection.attrs.radius as string;
    const align = (selection.attrs.align as ButtonAlign) ?? "center";
    const fullWidth = selection.attrs.width === "100%";

    // No focus() here on purpose: the label and link fields are part of this
    // bar, and pulling focus back into the editor on every keystroke would make
    // them impossible to type in. ProseMirror keeps the node selected anyway.
    const set = (attrs: Record<string, unknown>) => editor.commands.updateAttributes("emailButton", attrs);

    return (
        <NodeBubble anchor={anchor}>
            <div className="flex items-center gap-1">
                <input
                    value={label}
                    onChange={(e) => set({ label: e.target.value })}
                    // An empty button is a coloured box nobody can read, and the
                    // field is the only place to notice that.
                    onBlur={(e) => !e.target.value.trim() && set({ label: BUTTON_DEFAULT_LABEL })}
                    placeholder="Button text"
                    title="Merge fields and spintax work here, the same as in the body"
                    className="h-6 w-32 rounded border border-slate-200 px-1.5 text-[11px] text-slate-800 outline-none focus:border-sky-400"
                />
                <input
                    value={href}
                    onChange={(e) => set({ href: e.target.value })}
                    // A bare host is a relative path to a mail client, so it
                    // goes nowhere and is never counted as a click.
                    onBlur={(e) => set({ href: absoluteHref(e.target.value) })}
                    placeholder="https://…"
                    title={
                        href.trim()
                            ? "Where the button goes"
                            : "A button with no link does nothing when a reader presses it"
                    }
                    className={`h-6 w-48 max-w-[40vw] rounded border px-1.5 text-[11px] text-slate-800 outline-none focus:border-sky-400 ${
                        href.trim() ? "border-slate-200" : "border-amber-300 bg-amber-50/50"
                    }`}
                />
            </div>
            <div className="flex flex-wrap items-center gap-1">
                <div className="flex items-center gap-0.5">
                    {BUTTON_SWATCHES.map((s) => (
                        <button
                            key={s.value}
                            type="button"
                            title={s.label}
                            aria-pressed={background.toLowerCase() === s.value}
                            onMouseDown={(e) => e.preventDefault()}
                            onClick={() => set({ background: s.value, color: readableTextColor(s.value) })}
                            style={{ background: s.value }}
                            className={`size-5 rounded-full border transition-transform hover:scale-110 ${
                                background.toLowerCase() === s.value
                                    ? "border-slate-900 ring-2 ring-sky-100"
                                    : "border-slate-200"
                            }`}
                        />
                    ))}
                </div>
                <BubbleDivider />
                {BUTTON_SIZES.map((s) => (
                    <button
                        key={s.key}
                        type="button"
                        title={s.title}
                        aria-pressed={padding === s.padding}
                        onMouseDown={(e) => e.preventDefault()}
                        onClick={() => set({ padding: s.padding, fontSize: s.fontSize })}
                        className={`h-6 px-1.5 rounded text-[11px] font-medium transition-colors ${
                            padding === s.padding
                                ? "bg-sky-50 text-sky-700"
                                : "text-slate-500 hover:bg-slate-100 hover:text-slate-900"
                        }`}
                    >
                        {s.label}
                    </button>
                ))}
                <BubbleDivider />
                {BUTTON_RADII.map((r, i) => {
                    const Icon = CORNER_ICONS[i];
                    return (
                        <BubbleBtn
                            key={r.value}
                            title={r.title}
                            active={radius === r.value}
                            onClick={() => set({ radius: r.value })}
                        >
                            <Icon className="w-3 h-3" />
                        </BubbleBtn>
                    );
                })}
                <BubbleDivider />
                {ALIGNMENTS.map(({ value, title, Icon }) => (
                    <BubbleBtn
                        key={value}
                        title={title}
                        active={align === value}
                        onClick={() => set({ align: value })}
                    >
                        <Icon className="w-3 h-3" />
                    </BubbleBtn>
                ))}
                <BubbleBtn
                    title="Stretch to the full width of the column"
                    active={fullWidth}
                    onClick={() => set({ width: fullWidth ? null : "100%" })}
                >
                    <StretchHorizontalIcon className="w-3 h-3" />
                </BubbleBtn>
                <BubbleDivider />
                <button
                    type="button"
                    title="Remove button"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => editor.chain().focus().deleteSelection().run()}
                    className="size-6 inline-flex items-center justify-center rounded text-slate-400 transition-colors hover:bg-rose-50 hover:text-rose-600"
                >
                    <Trash2Icon className="w-3 h-3" />
                </button>
            </div>
        </NodeBubble>
    );
}
