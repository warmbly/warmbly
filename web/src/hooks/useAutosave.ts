import { useCallback, useEffect, useRef, useState } from "react";

export type AutosaveStatus = "idle" | "saving" | "saved" | "error";

export interface AutosaveHandle<T> {
    status: AutosaveStatus;
    /** Unsaved changes exist (pending debounce, in-flight, or failed save). */
    dirty: boolean;
    /** Save immediately and resolve when done (used by the tab-switch guard). */
    flush: () => Promise<void>;
    /** Retry after an error. */
    retry: () => void;
    /** The last successfully-saved value (used to discard local edits). */
    savedValue: T;
    /** Move the baseline to v without saving (e.g. when server data arrives). */
    markSaved: (v: T) => void;
}

// useAutosave persists `value` automatically whenever it changes away from the
// last-saved snapshot: instantly for toggles (debounceMs 0) or after a quiet
// period for text fields. The first render adopts the value as the baseline
// (no save). It exposes a status for the header indicator plus dirty/flush so
// a navigation guard can offer "save or discard" on tab switch.
export function useAutosave<T>({
    value,
    save,
    debounceMs = 0,
    enabled = true,
    isEqual = defaultEqual,
}: {
    value: T;
    save: (value: T) => Promise<unknown>;
    debounceMs?: number;
    enabled?: boolean;
    isEqual?: (a: T, b: T) => boolean;
}): AutosaveHandle<T> {
    const [status, setStatus] = useState<AutosaveStatus>("idle");
    const [savedValue, setSavedValue] = useState<T>(value);
    const savedRef = useRef<T>(value);
    const valueRef = useRef<T>(value);
    const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const savedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const initializedRef = useRef(false);
    const inflightRef = useRef<Promise<void> | null>(null);
    valueRef.current = value;

    const commitSaved = useCallback((v: T) => {
        savedRef.current = v;
        setSavedValue(v);
    }, []);

    // Single-flight: the change effect re-arms on every render (save is
    // typically an inline closure, so its identity changes each render), and
    // the baseline only moves once the request resolves — without this guard
    // every render during an in-flight save fired another request, snowballing
    // into a request storm. Concurrent callers share the in-flight promise;
    // the loop picks up edits made while a save was running.
    const run = useCallback((): Promise<void> => {
        if (inflightRef.current) return inflightRef.current;
        if (isEqual(valueRef.current, savedRef.current)) return Promise.resolve();
        const p = (async () => {
            if (timerRef.current) {
                clearTimeout(timerRef.current);
                timerRef.current = null;
            }
            setStatus("saving");
            try {
                while (!isEqual(valueRef.current, savedRef.current)) {
                    const toSave = valueRef.current;
                    await save(toSave);
                    commitSaved(toSave);
                }
                setStatus("saved");
                if (savedTimerRef.current) clearTimeout(savedTimerRef.current);
                savedTimerRef.current = setTimeout(() => setStatus("idle"), 2000);
            } catch {
                setStatus("error");
                throw new Error("autosave failed");
            } finally {
                inflightRef.current = null;
            }
        })();
        inflightRef.current = p;
        return p;
    }, [save, isEqual, commitSaved]);

    useEffect(() => {
        if (!initializedRef.current) {
            initializedRef.current = true;
            commitSaved(value);
            return;
        }
        if (!enabled) return;
        if (isEqual(value, savedRef.current)) return;

        if (timerRef.current) clearTimeout(timerRef.current);
        if (debounceMs <= 0) {
            void run().catch(() => {});
        } else {
            timerRef.current = setTimeout(() => void run().catch(() => {}), debounceMs);
        }
        return () => {
            if (timerRef.current) clearTimeout(timerRef.current);
        };
    }, [value, enabled, debounceMs, run, isEqual, commitSaved]);

    // Safety net: flush a pending debounced save on a non-blocked unmount.
    useEffect(() => {
        return () => {
            if (timerRef.current && !isEqual(valueRef.current, savedRef.current)) {
                void save(valueRef.current);
            }
            if (savedTimerRef.current) clearTimeout(savedTimerRef.current);
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const flush = useCallback(async () => {
        // run() resolves immediately when clean and returns the shared
        // promise when a save is already in flight.
        await run();
    }, [run]);

    const retry = useCallback(() => void run().catch(() => {}), [run]);

    const dirty = !isEqual(value, savedValue);

    return { status, dirty, flush, retry, savedValue, markSaved: commitSaved };
}

function defaultEqual<T>(a: T, b: T): boolean {
    return JSON.stringify(a) === JSON.stringify(b);
}
