import { useCallback, useEffect, useState } from "react";
import { useSearchParam, useSearchParams } from "./useSearchParams";

export default function useMailboxTagFilter(owner: string, validTags: readonly { id: string }[] | undefined) {
    const key = `warmbly:mailbox-tag:${owner}`;
    const [params] = useSearchParams();
    const [urlTag, setURLTag] = useSearchParam("tag");
    const read = useCallback(() => {
        try { return sessionStorage.getItem(key) ?? ""; } catch { return ""; }
    }, [key]);
    const [memory, setMemory] = useState(() => ({ key, value: read() }));
    const remembered = memory.key === key ? memory.value : read();
    const selected = params.has("tag") ? urlTag : remembered;
    const tag = validTags === undefined || validTags.some((candidate) => candidate.id === selected) ? selected : "";
    const remember = useCallback((value: string) => {
        setMemory({ key, value });
        try { sessionStorage.setItem(key, value); } catch { /* Storage may be disabled. */ }
    }, [key]);

    useEffect(() => {
        remember(tag);
        if (tag !== urlTag) setURLTag(tag);
    }, [tag, urlTag, remember, setURLTag]);

    const setTag = useCallback((value: string) => {
        remember(value);
        setURLTag(value);
    }, [remember, setURLTag]);
    return [tag, setTag] as const;
}
