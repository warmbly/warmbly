// Motion and class constants the tour scenes share.

import { createContext, useContext, useEffect, useState } from "react";
import { useReducedMotion } from "framer-motion";

export const EASE = [0.22, 1, 0.36, 1] as const;

/** Bumps every `ms` while mounted, so a scene can key on it to replay its story. */
export function useLoop(ms: number): number {
    const reduced = useReducedMotion();
    const [n, setN] = useState(0);
    useEffect(() => {
        if (reduced) return;
        const t = window.setInterval(() => setN((v) => v + 1), ms);
        return () => window.clearInterval(t);
    }, [ms, reduced]);
    return n;
}

/** Scenes are drawn this many pixels wide and scaled to their window. */
export const SCENE_W = 800;

export const HeightContext = createContext(440);

/** The scene canvas's height in scene pixels, which follows the window's. */
export const useSceneHeight = () => useContext(HeightContext);
