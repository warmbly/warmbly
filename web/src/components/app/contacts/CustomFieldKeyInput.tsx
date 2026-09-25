// A custom-field name input that suggests the workspace's existing fields as the user types.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { BracesIcon } from "lucide-react";
import { TextInput } from "@/components/ui/field";
import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import { cn } from "@/lib/utils";
import { suggestKeys } from "./customFields";

export default function CustomFieldKeyInput({
    value,
    onChange,
    onPick,
    keys,
    placeholder = "Field name",
    autoFocus,
    invalid,
    title,
    className,
}: {
    value: string;
    onChange: (v: string) => void;
    // Called with an existing field chosen from the list; defaults to onChange.
    onPick?: (key: string) => void;
    // The workspace's fields, most used first.
    keys: string[];
    placeholder?: string;
    autoFocus?: boolean;
    invalid?: boolean;
    title?: string;
    className?: string;
}) {
    const [open, setOpen] = React.useState(false);
    const [active, setActive] = React.useState(-1);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));

    // Nothing to offer once the name already is a field.
    const matches = keys.includes(value) ? [] : suggestKeys(value, keys);
    const shown = open && matches.length > 0;
    const placement = useFlipPlacement(ref, shown, Math.min(matches.length, 8) * 28 + 32);

    function pick(key: string) {
        (onPick ?? onChange)(key);
        setOpen(false);
        setActive(-1);
    }

    function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
        if (e.key === "ArrowDown" || e.key === "ArrowUp") {
            if (matches.length === 0) return;
            e.preventDefault();
            setOpen(true);
            const step = e.key === "ArrowDown" ? 1 : -1;
            setActive((i) => (i + step + matches.length) % matches.length);
        } else if (e.key === "Enter") {
            // Never submits the surrounding form; it only picks a highlighted field.
            e.preventDefault();
            if (shown && active >= 0 && active < matches.length) pick(matches[active]);
        } else if (e.key === "Escape" && shown) {
            // The list is the innermost layer; the dialog behind it stays open.
            e.stopPropagation();
            setOpen(false);
        }
    }

    return (
        <div
            ref={ref}
            className={cn("relative min-w-0", className)}
            onFocus={() => setOpen(true)}
            onBlur={(e) => {
                if (!ref.current?.contains(e.relatedTarget as Node | null)) setOpen(false);
            }}
        >
            <TextInput
                value={value}
                onChange={(v) => {
                    onChange(v);
                    setOpen(true);
                    setActive(-1);
                }}
                onKeyDown={onKeyDown}
                placeholder={placeholder}
                autoFocus={autoFocus}
                autoComplete="off"
                invalid={invalid}
                title={title}
                className="w-full"
            />
            <AnimatePresence>
                {shown && (
                    <motion.div
                        data-floating
                        role="listbox"
                        initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        className={cn(
                            "absolute left-0 z-30 w-full min-w-[200px] rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] py-1",
                            placement === "top" ? "bottom-full mb-1" : "top-full mt-1",
                        )}
                    >
                        <div className="px-2.5 pt-0.5 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            Your custom fields
                        </div>
                        {matches.map((k, i) => (
                            <button
                                key={k}
                                type="button"
                                role="option"
                                aria-selected={i === active}
                                // Keep focus in the input so the list does not close first.
                                onMouseDown={(e) => e.preventDefault()}
                                onMouseEnter={() => setActive(i)}
                                onClick={() => pick(k)}
                                className={cn(
                                    "w-full px-2.5 h-7 flex items-center gap-2 text-left text-[12px] text-slate-700 transition-colors",
                                    i === active ? "bg-slate-100 text-slate-900" : "hover:bg-slate-100",
                                )}
                            >
                                <BracesIcon className="w-3 h-3 text-slate-400 shrink-0" />
                                <span className="truncate">{k}</span>
                            </button>
                        ))}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
