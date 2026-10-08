import { resolveValue, toast, useToaster, type Toast } from "react-hot-toast/headless";
import { useLayoutEffect, useRef } from "react";
import { motion } from "motion/react";
import { CheckIcon, CircleXIcon, LoaderCircleIcon, XIcon } from "lucide-react";

const TOAST_MAX_WIDTH = 640;
const ERROR_DURATION = 12000;

function ToastCard({ t, offset, updateHeight }: { t: Toast; offset: number; updateHeight: (id: string, height: number) => void }) {
    const ref = useRef<HTMLDivElement>(null);
    useLayoutEffect(() => {
        const node = ref.current;
        if (!node) return;
        const measure = () => updateHeight(t.id, node.getBoundingClientRect().height);
        measure();
        const observer = new ResizeObserver(measure);
        observer.observe(node);
        return () => observer.disconnect();
    }, [t.id, updateHeight]);
    const position = t.position || "top-center";
    const top = position.startsWith("top");
    const icon = t.icon ?? (t.type === "loading" ? <LoaderCircleIcon className="size-5 animate-spin text-slate-400" /> : t.type === "success" ? <CheckIcon className="size-5 rounded-full bg-green-500 p-0.5 text-white" /> : t.type === "error" ? <CircleXIcon className="size-5 text-red-500" /> : null);
    return (
        <div
            ref={ref}
            className={`absolute inset-x-0 flex transition-transform duration-200 motion-reduce:transition-none ${top ? "top-0" : "bottom-0"} ${position.endsWith("center") ? "justify-center" : position.endsWith("right") ? "justify-end" : "justify-start"}`}
            style={{ transform: `translateY(${offset * (top ? 1 : -1)}px)`, zIndex: t.visible ? 9999 : undefined }}
        >
            <motion.div
                className={t.visible ? "pointer-events-auto" : "pointer-events-none"}
                initial={{ opacity: 0, y: -8, scale: 0.96 }}
                animate={{ opacity: t.visible ? 1 : 0, y: t.visible ? 0 : -8, scale: t.visible ? 1 : 0.96 }}
                transition={{ duration: 0.25, ease: "easeOut" }}
            >
                {t.type === "custom" ? resolveValue(t.message, t) : (
                    <div className={`flex items-center gap-2 rounded-lg bg-white px-2.5 py-2 text-slate-800 shadow-[0_3px_10px_rgb(0_0_0/0.1),0_3px_3px_rgb(0_0_0/0.05)] ${t.className ?? ""}`} style={{ maxWidth: TOAST_MAX_WIDTH, ...t.style }}>
                        {icon && <span className="shrink-0">{icon}</span>}
                        <div {...t.ariaProps} className="min-w-0 flex-1 px-1.5 py-0.5 text-[12.5px] leading-[1.5] break-words">{resolveValue(t.message, t)}</div>
                        {t.type === "error" && (
                            <button type="button" aria-label="Dismiss" onClick={() => toast.dismiss(t.id)} className="shrink-0 self-start -mr-1 size-6 rounded-md inline-flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-100 transition-colors">
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        )}
                    </div>
                )}
            </motion.div>
        </div>
    );
}

export function Toaster() {
    const { toasts, handlers } = useToaster({ duration: 4000, error: { duration: ERROR_DURATION } });
    return (
        <div
            data-rht-toaster=""
            className="pointer-events-none fixed inset-4 z-[9999]"
            onMouseEnter={handlers.startPause}
            onMouseLeave={handlers.endPause}
        >
            {toasts.map((t) => (
                <ToastCard key={t.id} t={t} offset={handlers.calculateOffset(t, { defaultPosition: "top-center", gutter: 8 })} updateHeight={handlers.updateHeight} />
            ))}
        </div>
    );
}
