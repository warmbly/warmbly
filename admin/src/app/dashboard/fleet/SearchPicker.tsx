// Debounced type-to-search picker for records too numerous for a Select
// (organizations, users). Closes on click-away (capture phase, so a dialog's
// mousedown stopPropagation cannot swallow it) and on Escape, which it stops
// from reaching the surrounding dialog so only the innermost layer closes.

import { useEffect, useRef, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Loader2, Search, X } from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

export interface SearchPickerProps<T> {
    value: T | null;
    onChange: (v: T | null) => void;
    search: (q: string) => Promise<T[]>;
    /** Query key prefix; the debounced term is appended. */
    queryKey: string[];
    getKey: (t: T) => string;
    renderItem: (t: T) => React.ReactNode;
    renderSelected: (t: T) => React.ReactNode;
    placeholder?: string;
    minChars?: number;
    emptyText?: string;
    autoFocus?: boolean;
    disabled?: boolean;
}

export function SearchPicker<T>({
    value,
    onChange,
    search,
    queryKey,
    getKey,
    renderItem,
    renderSelected,
    placeholder = "Search…",
    minChars = 2,
    emptyText = "No matches.",
    autoFocus,
    disabled,
}: SearchPickerProps<T>) {
    const [term, setTerm] = useState("");
    const [debounced, setDebounced] = useState("");
    const [open, setOpen] = useState(false);
    const [highlight, setHighlight] = useState(0);
    const rootRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        const t = setTimeout(() => setDebounced(term.trim()), 250);
        return () => clearTimeout(t);
    }, [term]);

    const enabled = debounced.length >= minChars;
    const { data, isFetching } = useQuery({
        queryKey: [...queryKey, debounced],
        queryFn: () => search(debounced),
        enabled,
        placeholderData: keepPreviousData,
        staleTime: 30_000,
    });
    const items = enabled ? (data ?? []) : [];

    useEffect(() => {
        if (!open) return;
        const onDown = (e: MouseEvent) => {
            if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
        };
        document.addEventListener("mousedown", onDown, true);
        return () => document.removeEventListener("mousedown", onDown, true);
    }, [open]);

    function pick(t: T) {
        onChange(t);
        setTerm("");
        setDebounced("");
        setOpen(false);
    }

    function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
        if (e.key === "Escape") {
            if (!open) return;
            e.preventDefault();
            e.stopPropagation();
            setOpen(false);
            return;
        }
        if (!open || items.length === 0) return;
        if (e.key === "ArrowDown") {
            e.preventDefault();
            setHighlight((h) => Math.min(items.length - 1, h + 1));
        } else if (e.key === "ArrowUp") {
            e.preventDefault();
            setHighlight((h) => Math.max(0, h - 1));
        } else if (e.key === "Enter") {
            e.preventDefault();
            const t = items[highlight];
            if (t) pick(t);
        }
    }

    if (value) {
        return (
            <div className="flex items-center justify-between gap-2 rounded-md border border-border bg-card px-2.5 py-1.5 text-[12.5px]">
                <div className="min-w-0 flex-1">{renderSelected(value)}</div>
                {!disabled && (
                    <button
                        type="button"
                        onClick={() => onChange(null)}
                        className="grid size-6 shrink-0 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-foreground"
                        aria-label="Clear selection"
                    >
                        <X className="size-3.5" />
                    </button>
                )}
            </div>
        );
    }

    return (
        <div ref={rootRef} className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
                value={term}
                disabled={disabled}
                autoFocus={autoFocus}
                onChange={(e) => {
                    setTerm(e.target.value);
                    setHighlight(0);
                    setOpen(true);
                }}
                onFocus={() => setOpen(true)}
                onKeyDown={onKeyDown}
                placeholder={placeholder}
                className="h-8 pl-8 pr-8 text-[12.5px]"
                role="combobox"
                aria-expanded={open}
            />
            {isFetching && (
                <Loader2 className="absolute right-2.5 top-1/2 size-3.5 -translate-y-1/2 animate-spin text-muted-foreground" />
            )}
            {open && term.trim().length > 0 && (
                <div
                    data-floating
                    className="absolute left-0 right-0 z-40 mt-1 max-h-64 overflow-auto rounded-md border border-border bg-popover p-1 shadow-md"
                >
                    {!enabled ? (
                        <div className="px-2 py-1.5 text-xs text-muted-foreground">
                            Type at least {minChars} characters.
                        </div>
                    ) : items.length === 0 && !isFetching ? (
                        <div className="px-2 py-1.5 text-xs text-muted-foreground">{emptyText}</div>
                    ) : (
                        items.map((t, i) => (
                            <button
                                key={getKey(t)}
                                type="button"
                                onMouseEnter={() => setHighlight(i)}
                                onClick={() => pick(t)}
                                className={cn(
                                    "block w-full rounded px-2 py-1.5 text-left text-[12.5px]",
                                    i === highlight ? "bg-muted" : "hover:bg-muted/60",
                                )}
                            >
                                {renderItem(t)}
                            </button>
                        ))
                    )}
                </div>
            )}
        </div>
    );
}
