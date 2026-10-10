// The frame every tour scene sits in, matching the marketing site's: a window
// hung from the top-left of a photo, running off the far edges. Scenes are
// drawn SCENE_W wide and scaled to the window; their height follows the
// window's, read from useSceneHeight, so nothing ends short of the frame.

import { useEffect, useRef, useState, type ReactNode } from "react";
import { cn } from "@/lib/utils";
import frost from "./frost.webp";
import { HeightContext, SCENE_W } from "./sceneTokens";

export default function TourStage({ children, className }: { children: ReactNode; className?: string }) {
    const box = useRef<HTMLDivElement>(null);
    const [size, setSize] = useState({ k: 0, h: 440 });

    useEffect(() => {
        const el = box.current;
        if (!el) return;
        const fit = () => {
            const k = el.clientWidth / SCENE_W;
            if (k > 0) setSize({ k, h: Math.ceil(el.clientHeight / k) });
        };
        fit();
        const ro = new ResizeObserver(fit);
        ro.observe(el);
        return () => ro.disconnect();
    }, []);

    return (
        <div className={cn("relative h-full w-full overflow-hidden bg-slate-100", className)}>
            <img
                src={frost}
                alt=""
                aria-hidden
                draggable={false}
                className="absolute inset-0 h-full w-full object-cover select-none dark:opacity-10"
            />
            <span className="absolute left-3 top-3 z-10 inline-flex h-5 items-center gap-1.5 rounded-full bg-white/80 px-2 text-[10.5px] font-medium text-slate-600 ring-1 ring-slate-900/[0.06] backdrop-blur-sm">
                <span className="size-1.5 rounded-full bg-sky-500" />
                Sample data
            </span>
            <div
                ref={box}
                className="absolute left-[5%] top-[9%] right-0 bottom-0 overflow-hidden rounded-tl-[12px] bg-[#f8fafc] ring-1 ring-slate-900/[0.08] shadow-[0_40px_90px_-40px_rgba(15,23,42,0.6)]"
            >
                {size.k > 0 && (
                    <HeightContext.Provider value={size.h}>
                        <div
                            aria-hidden
                            className="absolute left-0 top-0 origin-top-left font-sans"
                            style={{ width: SCENE_W, height: size.h, transform: `scale(${size.k})` }}
                        >
                            {children}
                        </div>
                    </HeightContext.Provider>
                )}
            </div>
        </div>
    );
}
