// CanvasSelections — colored outlines around the nodes each teammate has
// selected on a shared @xyflow canvas, with a name tag on the topmost one so it
// is always clear whose selection it is. Rendered as a child of <ReactFlow> so
// it can read the live viewport and node geometry; outlines track remote drags
// because those already update node positions.

import React from "react";
import { useNodes, useViewport, type Node } from "@xyflow/react";
import type { RemoteSelection } from "@/hooks/useLiveCanvas";

interface Box {
    node: Node;
    w: number;
    h: number;
}

export default function CanvasSelections({ selections }: { selections: RemoteSelection[] }) {
    const { x, y, zoom } = useViewport();
    const nodes = useNodes();
    if (!selections.length) return null;
    const byId = new Map(nodes.map((n) => [n.id, n]));
    return (
        <div className="pointer-events-none absolute inset-0 z-10 overflow-hidden">
            {selections.map((s, si) => {
                const picked: Box[] = [];
                for (const id of s.ids) {
                    const node = byId.get(id);
                    const w = node?.measured?.width;
                    const h = node?.measured?.height;
                    if (node && w && h) picked.push({ node, w, h });
                }
                if (!picked.length) return null;
                // Overlapping selections stagger their padding so both stay visible.
                const pad = 5 + (si % 3) * 3;
                let tag = picked[0];
                for (const b of picked) if (b.node.position.y < tag.node.position.y) tag = b;
                return (
                    <React.Fragment key={s.userId}>
                        {picked.map(({ node, w, h }) => (
                            <div
                                key={node.id}
                                className="absolute rounded-[14px] border-2"
                                style={{
                                    left: x + node.position.x * zoom - pad,
                                    top: y + node.position.y * zoom - pad,
                                    width: w * zoom + pad * 2,
                                    height: h * zoom + pad * 2,
                                    borderColor: s.color,
                                }}
                            >
                                {node.id === tag.node.id ? (
                                    <span
                                        className="absolute -top-[19px] left-0 max-w-[160px] truncate rounded px-1.5 py-px text-[10px] font-medium leading-4 text-white"
                                        style={{ backgroundColor: s.color }}
                                    >
                                        {s.name ?? "Teammate"}
                                    </span>
                                ) : null}
                            </div>
                        ))}
                    </React.Fragment>
                );
            })}
        </div>
    );
}
