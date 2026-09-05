// RollingNumber — an odometer for prices.
//
// Each digit is its own column holding 0-9 stacked vertically; changing the
// value slides the column to the new digit, so 29 → 23 rolls the last wheel
// down through 8,7,6,5,4 instead of cross-fading two numbers. Digits are keyed
// by their position from the right, so a price gaining or losing a digit
// (89 → 263) keeps the shared wheels and animates the new one in.
//
// Sizing is in `em`, so it inherits whatever type scale it is dropped into.
// This is the showy variant; `ui/AnimatedNumber` stays the calm count-up used
// by live stats.

import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";

const DIGITS = ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9"];

export default function RollingNumber({
    value,
    format,
    className,
}: {
    value: number;
    /** Renders the value to the string that gets rolled. Default: rounded. */
    format?: (n: number) => string;
    className?: string;
}) {
    const reduced = useReducedMotion();
    const text = format ? format(value) : String(Math.round(value));
    const chars = text.split("");

    if (reduced) {
        return <span className={className}>{text}</span>;
    }

    return (
        <span className={className} style={{ display: "inline-flex", alignItems: "flex-end", lineHeight: 1 }}>
            {/* The wheels each hold every digit 0-9, so assistive tech would
                read a wall of digits. Expose the real value once instead. */}
            <span className="sr-only">{text}</span>
            <span aria-hidden="true" style={{ display: "inline-flex", alignItems: "flex-end", lineHeight: 1 }}>
            <AnimatePresence initial={false} mode="popLayout">
                {chars.map((ch, i) => {
                    // Position from the right keeps a wheel's identity when the
                    // number grows or shrinks a digit.
                    const key = `${chars.length - i}-${/\d/.test(ch) ? "d" : ch}`;
                    return /\d/.test(ch) ? (
                        <Wheel key={key} digit={Number(ch)} />
                    ) : (
                        <motion.span
                            key={key}
                            layout
                            initial={{ opacity: 0, width: 0 }}
                            animate={{ opacity: 1, width: "auto" }}
                            exit={{ opacity: 0, width: 0 }}
                            transition={{ duration: 0.2 }}
                            style={{ display: "inline-block", lineHeight: 1 }}
                        >
                            {ch}
                        </motion.span>
                    );
                })}
            </AnimatePresence>
            </span>
        </span>
    );
}

function Wheel({ digit }: { digit: number }) {
    return (
        <motion.span
            layout
            initial={{ opacity: 0, width: 0 }}
            animate={{ opacity: 1, width: "0.62em" }}
            exit={{ opacity: 0, width: 0 }}
            transition={{ duration: 0.24 }}
            style={{
                display: "inline-block",
                position: "relative",
                height: "1em",
                overflow: "hidden",
                lineHeight: 1,
            }}
        >
            <motion.span
                animate={{ y: `${-digit}em` }}
                transition={{ type: "spring", stiffness: 240, damping: 26, mass: 0.7 }}
                style={{
                    position: "absolute",
                    inset: 0,
                    display: "flex",
                    flexDirection: "column",
                    alignItems: "center",
                }}
            >
                {DIGITS.map((d) => (
                    // flexShrink pins the geometry: the -Nem offsets below
                    // assume each digit is exactly 1em tall.
                    <span key={d} style={{ height: "1em", lineHeight: 1, flexShrink: 0 }}>
                        {d}
                    </span>
                ))}
            </motion.span>
        </motion.span>
    );
}
