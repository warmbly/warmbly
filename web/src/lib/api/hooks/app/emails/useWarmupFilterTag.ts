import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    clearWarmupFilterTag,
    getWarmupFilterTag,
    setWarmupFilterTag,
} from "@/lib/api/client/app/emails/warmupFilterTag";

// Under ["emails"], so a teammate's change reaches this drawer through the email_account audit spine.
const key = (id: string) => ["emails", id, "warmup-filter-tag"];

export function useWarmupFilterTag(id: string, enabled = true) {
    return useQuery({ queryKey: key(id), queryFn: () => getWarmupFilterTag(id), enabled: !!id && enabled });
}

export function useSetWarmupFilterTag(id: string) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (tag: string | null | undefined) => (tag === null ? clearWarmupFilterTag(id) : setWarmupFilterTag(id, tag)),
        onSuccess: (data) => queryClient.setQueryData(key(id), data),
        onSettled: () => queryClient.invalidateQueries({ queryKey: key(id) }),
    });
}
