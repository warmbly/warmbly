// AnimatedNumber — a value that smoothly counts to its new target when it
// changes, so live (realtime-refreshed) stats tick up instead of jumping.
//
// This is the calm count-up tween, NOT the slot-machine odometer: a short
// ease-out interpolation from the previous value to the next. On first mount
// it shows the value immediately (no count-from-zero on page load); only later
// changes animate. tabular-nums keeps the width steady so layout never jitters.

import { useEffect } from "react";
import { animate, motion, useMotionValue, useTransform } from "framer-motion";

export default function AnimatedNumber({
    value,
    format,
    className,
    duration = 0.6,
}: {
    value: number;
    /** Render the (possibly fractional) interpolated value. Default: rounded + locale-grouped. */
    format?: (n: number) => string;
    className?: string;
    duration?: number;
}) {
    const mv = useMotionValue(value);
    const text = useTransform(mv, (v) =>
        format ? format(v) : Math.round(v).toLocaleString(),
    );

    useEffect(() => {
        const controls = animate(mv, value, { duration, ease: [0.16, 1, 0.3, 1] });
        return controls.stop;
    }, [value, duration, mv]);

    return <motion.span className={className}>{text}</motion.span>;
}
