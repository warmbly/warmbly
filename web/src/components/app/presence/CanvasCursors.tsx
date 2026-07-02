// CanvasCursors — teammates' live pointers on an @xyflow canvas. Rendered as a
// child of <ReactFlow> so it can read the live viewport and place each cursor in
// flow space: a cursor sits on the same logical canvas point for everyone,
// regardless of how each person has panned or zoomed.

import React from "react";
import { useViewport } from "@xyflow/react";
import type { RemoteCursor } from "@/hooks/useLiveCursors";
import Cursor from "./Cursor";

export default function CanvasCursors({ cursors }: { cursors: RemoteCursor[] }) {
    const { x, y, zoom } = useViewport();
    if (!cursors.length) return null;
    return (
        <div className="pointer-events-none absolute inset-0 z-20 overflow-hidden">
            {cursors.map((c) => (
                <Cursor
                    key={c.userId}
                    color={c.color}
                    name={c.name}
                    avatar={c.avatar}
                    chat={c.chat}
                    left={x + c.x * zoom}
                    top={y + c.y * zoom}
                />
            ))}
        </div>
    );
}
