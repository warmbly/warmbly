import { useCallback, useContext, useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";
import type { z } from "zod";
import { UserContext } from "./context/user";
import { useAppStore } from "@/stores/useAppStore";
import { BROWSE_STORAGE_PREFIX, browseSessionGeneration, canPersistBrowseState } from "@/lib/browseState";

export default function useBrowseState<T>(
    name: string,
    initial: T | (() => T),
    schema: z.ZodType<T>,
    options?: { initialOverride: T },
): [T, Dispatch<SetStateAction<T>>] {
    const userID = useContext(UserContext)?.user.id;
    const generation = useRef(browseSessionGeneration());
    const organizationID = useAppStore((state) => state.currentOrganization?.id ?? "personal");
    const owner = userID ? `${userID}:${organizationID}` : null;
    const key = owner ? `${BROWSE_STORAGE_PREFIX}${owner}:${name}` : null;
    const read = (applyIntent = true) => {
        if (applyIntent && options) return options.initialOverride;
        if (key) {
            try {
                const saved = sessionStorage.getItem(key);
                if (saved !== null) {
                    const result = schema.safeParse(JSON.parse(saved)?.value);
                    if (result.success) return result.data;
                }
            } catch { /* Storage may be unavailable or malformed. */ }
        }
        return typeof initial === "function" ? (initial as () => T)() : initial;
    };
    const [memory, setMemory] = useState(() => ({ key, owner, value: read() }));
    let current = memory;
    // Restore before committing, so a workspace switch never queries or saves the old filters.
    if (memory.key !== key) {
        current = { key, owner, value: read(memory.owner === null || memory.owner === owner) };
        setMemory(current);
    }
    const setValue = useCallback<Dispatch<SetStateAction<T>>>((next) => {
        setMemory((previous) => previous.key !== key ? previous : ({
            key: previous.key,
            owner: previous.owner,
            value: typeof next === "function" ? (next as (value: T) => T)(previous.value) : next,
        }));
    }, [key]);
    useEffect(() => {
        if (memory.key === null || memory.key !== key || !canPersistBrowseState(generation.current)) return;
        try { sessionStorage.setItem(memory.key, JSON.stringify({ value: memory.value })); }
        catch { /* Browsing must work when storage is disabled or full. */ }
    }, [key, memory]);
    return [current.value, setValue];
}
