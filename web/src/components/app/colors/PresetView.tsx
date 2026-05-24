"use client";

import { AnimatePresence, motion } from "framer-motion";
import React from "react";

const colors = [
    "#000000",
    "#C62828",
    "#1976D2",
    "#0D47A1",
    "#388E3C",
    "#2E7D32",
    "#F57C00",
    "#E65100",
    "#7B1FA2",
    "#4A148C",
    "#0288D1",
    "#006064",
    "#C2185B",
    "#AD1457",
    "#FBC02D",
    "#FF1744",
    "#009688",
    "#CDDC39",
]

const PresetView = ({ color, setColor }: { color: string, setColor: (color: string) => void }) => {
    return (
        <div className="flex gap-3 flex-wrap justify-center my-4">
            {colors.map((c) => (
                <button
                    key={c}
                    onClick={() => setColor(c)}
                    className="relative flex justify-center items-center"
                >
                    <div
                        className="w-6 h-6 ripple border-none rounded-full z-10"
                        style={{ backgroundColor: c }}
                    />
                    <AnimatePresence mode="wait">
                        {color === c && (
                            <motion.div
                                initial={{ opacity: 0, scale: 0.6 }}
                                animate={{ opacity: 1, scale: 1 }}
                                exit={{ opacity: 0, scale: 0.6 }}
                                className="z-0 w-7 h-7 ring-2 ring-blue-500 absolute rounded-full"
                            />
                        )}
                    </AnimatePresence>
                </button>
            ))}
        </div>
    )
}

export default PresetView;
