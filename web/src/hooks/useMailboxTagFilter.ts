import { useCallback, useEffect, useRef, useState } from "react";
import { browseSessionGeneration, canPersistBrowseState } from "@/lib/browseState";
import { useSearchParam, useSearchParams } from "./useSearchParams";

export default function useMailboxTagFilter(owner: string, validTags: readonly { id: string }[] | undefined) {
    const generation = useRef(browseSessionGeneration());
    const key = `warmbly:mailbox-tag:${owner}`;
    const [params] = useSearchParams();
    const [urlTag, setURLTag] = useSearchParam("tag");
    const read = useCallback(() => {
        try { return sessionStorage.getItem(key) ?? ""; } catch { return ""; }
    }, [key]);
    const [memory, setMemory] = useState(() => ({ key, value: read() }));
    const remembered = memory.key === key ? memory.value : read();
    const restoringOwner = memory.key !== key;
    const selected = !restoringOwner && params.has("tag") ? urlTag : remembered;
    const tag = validTags === undefined || validTags.some((candidate) => candidate.id === selected) ? selected : "";
    const remember = useCallback((value: string) => {
        setMemory({ key, value });
        if (!canPersistBrowseState(generation.current)) return;
        try { sessionStorage.setItem(key, value); } catch { /* Storage may be disabled. */ }
    }, [key]);

    useEffect(() => {
        if (restoringOwner && tag !== urlTag) {
            setURLTag(tag);
            return;
        }
        remember(tag);
        if (tag !== urlTag) setURLTag(tag);
    }, [restoringOwner, tag, urlTag, remember, setURLTag]);

    const setTag = useCallback((value: string) => {
        remember(value);
        setURLTag(value);
    }, [remember, setURLTag]);
    return [tag, setTag] as const;
}
