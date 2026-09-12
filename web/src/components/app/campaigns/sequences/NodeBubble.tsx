// The floating bar over a selected block node: an image, a call-to-action
// button. One implementation, because both have to do the same three things —
// follow the node through a scroll or a resize, stay inside the viewport, and
// vanish with the selection rather than with a listener of its own.

import React from "react";
import { createPortal } from "react-dom";
import { motion } from "framer-motion";
import type { Editor } from "@tiptap/react";
import { NodeSelection } from "@tiptap/pm/state";

export type SelectedNode = { pos: number; attrs: Record<string, unknown> };
export type NodeAnchor = { top: number; left: number; bottom: number };

// selectedNode returns the node of this type the caret has selected, or null.
// The bubble only exists for that selection, so clicking away dismisses it.
export function selectedNode(editor: Editor, typeName: string): SelectedNode | null {
    const sel = editor.state.selection;
    if (!(sel instanceof NodeSelection) || sel.node.type.name !== typeName) return null;
    return { pos: sel.from, attrs: sel.node.attrs };
}

// useNodeAnchor tracks where the selected node sits on screen. The editor
// re-renders its host on every transaction (shouldRerenderOnTransaction), so
// the position is read on each render rather than subscribed to again.
export function useNodeAnchor(editor: Editor, pos: number | null) {
    const [anchor, setAnchor] = React.useState<NodeAnchor | null>(null);

    React.useEffect(() => {
        if (pos === null) {
            setAnchor(null);
            return;
        }
        const sync = () => {
            try {
                const box = editor.view.coordsAtPos(pos);
                setAnchor({ top: box.top, left: box.left, bottom: box.bottom });
            } catch {
                setAnchor(null);
            }
        };
        sync();
        window.addEventListener("scroll", sync, true);
        window.addEventListener("resize", sync);
        return () => {
            window.removeEventListener("scroll", sync, true);
            window.removeEventListener("resize", sync);
        };
    }, [pos, editor]);

    return anchor;
}

export function NodeBubble({ anchor, children }: { anchor: NodeAnchor; children: React.ReactNode }) {
    return createPortal(
        <motion.div
            data-floating=""
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.12 }}
            style={{ position: "fixed", top: anchor.top, left: anchor.left, zIndex: 60 }}
            ref={(el) => {
                // Placed from the bar's own size rather than a guess at it:
                // these bars are one row or two depending on what they edit, so
                // a fixed offset either leaves a gap or covers the node. Above
                // the node when there is room and below it when there is not,
                // then clamped inside the viewport on every edge, because a
                // control that is off-screen is a control nobody has.
                if (!el) return;
                const box = el.getBoundingClientRect();
                const above = anchor.top - box.height - 6;
                const top = above >= 8 ? above : anchor.bottom + 6;
                el.style.top = `${Math.max(8, Math.min(top, window.innerHeight - box.height - 8))}px`;
                el.style.left = `${Math.max(8, Math.min(anchor.left, window.innerWidth - box.width - 8))}px`;
            }}
            className="flex max-w-[calc(100vw-16px)] flex-col gap-1 rounded-md border border-slate-200 bg-white p-1 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]"
        >
            {children}
        </motion.div>,
        document.body,
    );
}

export function BubbleDivider() {
    return <span className="mx-0.5 h-4 w-px bg-slate-200" />;
}

// BubbleBtn is every square control in a floating bar: pressed state in sky,
// nothing else.
export function BubbleBtn({
    active,
    title,
    onClick,
    children,
}: {
    active?: boolean;
    title: string;
    onClick: () => void;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            title={title}
            aria-pressed={active}
            onMouseDown={(e) => e.preventDefault()}
            onClick={onClick}
            className={`size-6 inline-flex items-center justify-center rounded transition-colors ${
                active ? "bg-sky-50 text-sky-700" : "text-slate-500 hover:bg-slate-100 hover:text-slate-900"
            }`}
        >
            {children}
        </button>
    );
}
