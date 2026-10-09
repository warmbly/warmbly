// Brae-density dropdown menu.
//
// Drop-in replacement for the legacy HeadSelectMenu and the shadcn
// DropdownMenu where we want the slim look used across the new chrome:
// 28px trigger, hairline-border surface, h-7 items, mono accents.
//
// Composition:
//
//   <PopoverMenu>
//     <PopoverMenuTrigger>
//       <button>Trigger</button>
//     </PopoverMenuTrigger>
//     <PopoverMenuContent>
//       <PopoverMenuLabel>Section</PopoverMenuLabel>
//       <PopoverMenuItem onSelect={...} icon={...} selected>
//         Acme
//         <PopoverMenuKbd>⌘1</PopoverMenuKbd>
//       </PopoverMenuItem>
//       <PopoverMenuSeparator />
//       <PopoverMenuItem icon={<PlusIcon className="w-3 h-3" />}>
//         New workspace
//       </PopoverMenuItem>
//     </PopoverMenuContent>
//   </PopoverMenu>
//
// Implementation notes:
//   - Built on Radix-style refs for trigger/content, but with our own
//     click-outside + Esc handling instead of dragging in the full
//     primitive surface. Keeps the bundle small and the styles
//     authoritative (no defaults to override).
//   - Positions itself underneath the trigger by default; pass
//     `align="end"` to right-align, `side="top"` to flip above.

import React, {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useId,
    useLayoutEffect,
    useRef,
    useState,
} from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { cn } from "@/lib/utils";
import useClickOutside from "@/hooks/useClickOutside";

interface MenuCtx {
    id: string;
    open: boolean;
    setOpen: (o: boolean) => void;
    triggerRef: React.RefObject<HTMLElement | null>;
    side: "bottom" | "top";
    align: "start" | "end" | "center";
    sideOffset: number;
    anchorPoint: { x: number; y: number } | null;
}

const Ctx = createContext<MenuCtx | null>(null);

function useMenu() {
    const c = useContext(Ctx);
    if (!c) throw new Error("PopoverMenu primitives must be used inside <PopoverMenu>");
    return c;
}

export function PopoverMenu({
    children,
    side = "bottom",
    align = "start",
    sideOffset = 6,
    open: controlledOpen,
    onOpenChange,
    anchorPoint = null,
}: {
    children: React.ReactNode;
    side?: "bottom" | "top";
    align?: "start" | "end" | "center";
    sideOffset?: number;
    open?: boolean;
    onOpenChange?: (o: boolean) => void;
    /** Open at a viewport point instead of under the trigger (a context menu). */
    anchorPoint?: { x: number; y: number } | null;
}) {
    const id = useId();
    const triggerRef = useRef<HTMLElement>(null);
    const [internalOpen, setInternalOpen] = useState(false);
    const open = controlledOpen ?? internalOpen;
    const setOpen = useCallback(
        (o: boolean) => {
            if (controlledOpen === undefined) setInternalOpen(o);
            onOpenChange?.(o);
        },
        [controlledOpen, onOpenChange],
    );
    return (
        <Ctx.Provider value={{ id, open, setOpen, triggerRef, side, align, sideOffset, anchorPoint }}>
            {children}
        </Ctx.Provider>
    );
}

export function PopoverMenuTrigger({
    children,
    asChild = false,
}: {
    children: React.ReactNode;
    asChild?: boolean;
}) {
    const { open, setOpen, triggerRef } = useMenu();
    const onClick = (e: React.MouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
        setOpen(!open);
    };

    if (asChild && React.isValidElement(children)) {
        return React.cloneElement(children as React.ReactElement<{
            ref?: React.Ref<HTMLElement>;
            onClick?: (e: React.MouseEvent) => void;
            "aria-expanded"?: boolean;
            "data-state"?: "open" | "closed";
        }>, {
            ref: triggerRef,
            onClick,
            "aria-expanded": open,
            "data-state": open ? "open" : "closed",
        });
    }
    return (
        <button
            ref={triggerRef as React.RefObject<HTMLButtonElement>}
            type="button"
            onClick={onClick}
            aria-expanded={open}
            data-state={open ? "open" : "closed"}
        >
            {children}
        </button>
    );
}

export function PopoverMenuContent({
    children,
    className,
    minWidth = 200,
    matchTriggerWidth = false,
    role = "menu",
    "aria-label": ariaLabel,
}: {
    children: React.ReactNode;
    className?: string;
    minWidth?: number;
    /** Pin the panel to the trigger's measured width (for full-width selects). */
    matchTriggerWidth?: boolean;
    role?: "menu" | "dialog";
    "aria-label"?: string;
}) {
    const { open, setOpen, triggerRef, side, align, sideOffset, anchorPoint } = useMenu();
    const ref = useRef<HTMLDivElement>(null);
    // Where focus was when the menu opened, so closing gives it back.
    const returnFocus = useRef<HTMLElement | null>(null);
    const [pos, setPos] = useState<{ top: number; left: number; width?: number } | null>(null);

    useLayoutEffect(() => {
        if (!open) {
            setPos(null);
            return;
        }
        const compute = () => {
            const t = triggerRef.current;
            const c = ref.current;
            if ((!t && !anchorPoint) || !c) return;
            const r = anchorPoint
                ? new DOMRect(anchorPoint.x, anchorPoint.y, 0, 0)
                : t!.getBoundingClientRect();
            const cw = c.offsetWidth;
            const ch = c.offsetHeight;
            let top: number;
            if (side === "bottom") {
                top = r.bottom + sideOffset;
                if (top + ch > window.innerHeight - 8) top = r.top - ch - sideOffset;
            } else {
                top = r.top - ch - sideOffset;
                if (top < 8) top = r.bottom + sideOffset;
            }
            // Never let the top edge leave the viewport (a flipped-up panel
            // taller than the space above would otherwise be clipped with no
            // way to scroll to it); the panel's max-height handles the rest.
            if (top < 8) top = 8;
            let left: number;
            if (align === "end") left = r.right - cw;
            else if (align === "center") left = r.left + r.width / 2 - cw / 2;
            else left = r.left;
            if (left + cw > window.innerWidth - 8) left = window.innerWidth - 8 - cw;
            if (left < 8) left = 8;
            setPos({ top, left, width: r.width });
        };
        compute();
        // A context menu has no element to follow, so a scroll elsewhere closes it.
        const onScroll = (e: Event) => {
            if (anchorPoint && !ref.current?.contains(e.target as Node)) setOpen(false);
            else compute();
        };
        window.addEventListener("resize", compute);
        window.addEventListener("scroll", onScroll, true);

        // Recompute when the popover's own content changes size — e.g.
        // swapping a preset list for a datetime picker inside. Without
        // this the popover stays anchored to its old size and either
        // floats away from the trigger or gets clipped off-screen.
        let observer: ResizeObserver | null = null;
        if (ref.current && typeof ResizeObserver !== "undefined") {
            observer = new ResizeObserver(compute);
            observer.observe(ref.current);
        }

        return () => {
            window.removeEventListener("resize", compute);
            window.removeEventListener("scroll", onScroll, true);
            observer?.disconnect();
        };
    }, [open, side, align, sideOffset, triggerRef, anchorPoint, setOpen]);

    // A press outside, Escape (innermost first), or another menu opening closes it.
    useClickOutside(open, () => setOpen(false), [triggerRef, ref]);

    // Keyboard: focus lands on the panel (never over an autofocused input), and
    // closing hands it back to whatever held it before, if it is still there.
    useEffect(() => {
        if (!open) return;
        const panel = ref.current;
        const active = document.activeElement as HTMLElement | null;
        if (!panel?.contains(active)) returnFocus.current = active;
        const frame = requestAnimationFrame(() => {
            if (panel && !panel.contains(document.activeElement)) panel.focus({ preventScroll: true });
        });
        return () => {
            cancelAnimationFrame(frame);
            const back = returnFocus.current;
            returnFocus.current = null;
            const now = document.activeElement;
            const inside = !now || now === document.body || !!panel?.contains(now);
            if (back?.isConnected && inside) back.focus({ preventScroll: true });
        };
    }, [open]);

    // Arrow keys, Home/End and typeahead over this panel's own items. Keys it
    // handles stop here, so a nested menu's keys never reach this one and a
    // letter never also runs a global shortcut.
    const onMenuKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
        const target = e.target as HTMLElement;
        if (!ref.current?.contains(target)) return;
        if (target.matches("input, textarea, select, [contenteditable='true']")) return;
        if (e.ctrlKey || e.metaKey || e.altKey) return;
        const items = Array.from(
            ref.current.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)'),
        );
        const typeahead = e.key.length === 1 && /\S/.test(e.key);
        if (!items.length || (!typeahead && !["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key))) return;
        e.stopPropagation();
        const at = items.indexOf(target);
        let next = -1;
        if (e.key === "ArrowDown") next = at < 0 ? 0 : (at + 1) % items.length;
        else if (e.key === "ArrowUp") next = at < 0 ? items.length - 1 : (at - 1 + items.length) % items.length;
        else if (e.key === "Home") next = 0;
        else if (e.key === "End") next = items.length - 1;
        else {
            // The next item after the focused one whose label starts with the key.
            const k = e.key.toLowerCase();
            for (let i = 1; i <= items.length; i++) {
                const idx = (at + i + items.length) % items.length;
                if (items[idx].textContent?.trim().toLowerCase().startsWith(k)) {
                    next = idx;
                    break;
                }
            }
        }
        if (next < 0) return;
        e.preventDefault();
        items[next].focus();
    };

    // Anchor the animation origin to the side the menu opens from so
    // the scale + lift feels like it's growing out of the trigger
    // rather than floating in from nowhere. `align="end"` opens
    // top-right, `center` opens top-center, default opens top-left.
    const originX = align === "end" ? "right" : align === "center" ? "center" : "left";
    const originY = side === "bottom" ? "top" : "bottom";
    const transformOrigin = `${originY} ${originX}`;

    // Direction of the entrance slide depends on which side the menu
    // anchors to — bottom-anchored menus settle down 4px, top-anchored
    // ones settle up 4px.
    const enterY = side === "bottom" ? -4 : 4;

    if (typeof document === "undefined") return null;

    // Render through a portal to <body>. The menu is `position: fixed` with
    // viewport coordinates, but a transformed ancestor (e.g. the sidebar
    // <aside>, which uses translate-x for its drawer slide — even
    // `md:translate-x-0` on desktop) becomes the containing block for fixed
    // descendants, so the menu would otherwise anchor to the sidebar instead
    // of the viewport and render trapped inside it. The portal lifts it out of
    // any transformed/overflow-clipped ancestor.
    return createPortal(
        <AnimatePresence>
            {open && (
                <motion.div
                    ref={ref}
                    key="popover"
                    role={role}
                    aria-label={ariaLabel}
                    tabIndex={-1}
                    data-floating="true"
                    onKeyDown={onMenuKeyDown}
                    layout
                    initial={{ opacity: 0, scale: 0.96, y: enterY }}
                    animate={{ opacity: 1, scale: 1, y: 0 }}
                    exit={{ opacity: 0, scale: 0.97, y: enterY * 0.5 }}
                    transition={{
                        opacity: { duration: 0.14, ease: [0.16, 1, 0.3, 1] },
                        scale: { duration: 0.18, ease: [0.16, 1, 0.3, 1] },
                        y: { duration: 0.18, ease: [0.16, 1, 0.3, 1] },
                        // Smooth resize when the inner content swaps
                        // (e.g. preset list → datetime picker). The
                        // ResizeObserver above re-anchors position; this
                        // makes the height/width travel feel intentional.
                        layout: { duration: 0.22, ease: [0.16, 1, 0.3, 1] },
                    }}
                    style={{
                        position: "fixed",
                        top: pos?.top ?? -9999,
                        left: pos?.left ?? -9999,
                        // Clamp to the visual viewport so wide menus (long
                        // mailbox emails, campaign names) never run off a
                        // phone screen; desktop menus never approach this.
                        minWidth: Math.min(minWidth, window.innerWidth - 16),
                        maxWidth: "calc(100vw - 16px)",
                        width: matchTriggerWidth ? pos?.width : undefined,
                        visibility: pos ? "visible" : "hidden",
                        // Above the contact/import drawers (z-110..130) but
                        // below the global confirm dialog (z-200).
                        zIndex: 150,
                        transformOrigin,
                        willChange: "transform, opacity",
                    }}
                    className={cn(
                        "outline-none rounded-md border border-slate-200 bg-white shadow-[0_4px_12px_-2px_rgba(15,23,42,0.08),0_2px_4px_rgba(15,23,42,0.04)] overflow-hidden py-1",
                        // Default viewport cap so tall menus scroll instead of
                        // clipping off a phone screen; callers with a tighter
                        // max-h still win via the cn merge below.
                        "max-h-[calc(100dvh-16px)] overflow-y-auto",
                        className,
                    )}
                    onClick={(e) => e.stopPropagation()}
                >
                    {children}
                </motion.div>
            )}
        </AnimatePresence>,
        document.body,
    );
}

export function PopoverMenuLabel({ children }: { children: React.ReactNode }) {
    return (
        <div className="px-3 pt-2 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
            {children}
        </div>
    );
}

export function PopoverMenuItem({
    children,
    onSelect,
    icon,
    selected = false,
    danger = false,
    disabled = false,
    closeOnSelect = true,
    trailing,
}: {
    children: React.ReactNode;
    onSelect?: () => void;
    icon?: React.ReactNode;
    selected?: boolean;
    danger?: boolean;
    disabled?: boolean;
    closeOnSelect?: boolean;
    /** Custom right-aligned content. When set, it replaces the default
     *  `selected` dot — e.g. pass a checkmark for multi-select menus. */
    trailing?: React.ReactNode;
}) {
    const { setOpen } = useMenu();
    return (
        <button
            type="button"
            disabled={disabled}
            role="menuitem"
            onClick={(e) => {
                e.stopPropagation();
                if (disabled) return;
                onSelect?.();
                if (closeOnSelect) setOpen(false);
            }}
            className={cn(
                "w-full h-7 px-3 flex items-center gap-2 text-[12.5px] text-left transition-colors outline-none",
                danger
                    ? "text-red-600 hover:bg-red-50 focus-visible:bg-red-50"
                    : "text-slate-700 hover:bg-slate-50 hover:text-slate-900 focus-visible:bg-slate-50 focus-visible:text-slate-900",
                selected && !danger && "text-slate-900 font-medium",
                disabled && "opacity-50 cursor-not-allowed",
            )}
        >
            {icon && <span className={cn("shrink-0", danger ? "text-red-500" : "text-slate-400")}>{icon}</span>}
            <span className="flex-1 truncate">{children}</span>
            {trailing !== undefined ? (
                <span className="shrink-0">{trailing}</span>
            ) : selected ? (
                <span className="text-[10px] text-sky-600 shrink-0">●</span>
            ) : null}
        </button>
    );
}

export function PopoverMenuSeparator() {
    return <div className="my-1 h-px bg-slate-200" />;
}

export function PopoverMenuKbd({ children }: { children: React.ReactNode }) {
    return (
        <span className="ml-auto font-mono text-[10px] text-slate-400 tabular-nums shrink-0">
            {children}
        </span>
    );
}

/**
 * SelectButton — convenience trigger styled to match the rest of the
 * brae chrome. Pairs with PopoverMenu out of the box.
 *
 * Implementation note: forwardRef + {...rest} spread is required so
 * that `<PopoverMenuTrigger asChild>` can inject its onClick / ref /
 * aria-expanded via React.cloneElement. Without these, the trigger
 * was a silent no-op — every dropdown across the dashboard refused
 * to open. Same applies to any custom button used as a trigger.
 */
type SelectButtonProps = {
    icon?: React.ReactNode;
    label?: string;
    placeholder?: string;
    className?: string;
} & Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "children">;

export const SelectButton = React.forwardRef<HTMLButtonElement, SelectButtonProps>(
    function SelectButton({ icon, label, placeholder, className, ...rest }, ref) {
        return (
            <button
                ref={ref}
                type="button"
                {...rest}
                className={cn(
                    "h-7 px-2 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 text-[12px] font-medium transition-colors",
                    className,
                )}
            >
                {icon && <span className="text-slate-400 shrink-0">{icon}</span>}
                <span className="truncate max-w-[160px]">{label ?? placeholder ?? ""}</span>
                <svg
                    width="10"
                    height="10"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    className="text-slate-400 shrink-0"
                >
                    <path d="M6 9l6 6 6-6" />
                </svg>
            </button>
        );
    },
);
