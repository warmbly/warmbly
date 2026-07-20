// AnimatedHeight — eases a panel section's height as its content changes
// (search results arriving, filters narrowing the list) instead of snapping
// between sizes. The inner content is measured with a ResizeObserver and the
// outer element animates to that height; any max-height on `className` still
// clamps it, with overflow scrolling past the clamp.

import React from "react";
import { motion } from "framer-motion";

export default function AnimatedHeight({
    className,
    children,
}: {
    className?: string;
    children: React.ReactNode;
}) {
    const innerRef = React.useRef<HTMLDivElement>(null);
    const [height, setHeight] = React.useState<number | "auto">("auto");

    React.useLayoutEffect(() => {
        const el = innerRef.current;
        if (!el) return;
        setHeight(el.offsetHeight);
        const ro = new ResizeObserver(() => setHeight(el.offsetHeight));
        ro.observe(el);
        return () => ro.disconnect();
    }, []);

    return (
        <motion.div
            initial={false}
            animate={{ height }}
            transition={{ duration: 0.18, ease: [0.16, 1, 0.3, 1] }}
            className={className}
        >
            <div ref={innerRef}>{children}</div>
        </motion.div>
    );
}
