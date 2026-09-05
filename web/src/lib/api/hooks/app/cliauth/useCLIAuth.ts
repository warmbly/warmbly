import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { approveCLIAuthCode, denyCLIAuthCode, describeCLIAuthCode } from "@/lib/api/client/app/cliauth/cliAuth";

export const CLI_AUTH_KEY = ["cli-auth"];

export function useCLIAuthCode(code: string) {
    return useQuery({
        queryKey: [...CLI_AUTH_KEY, "code", code],
        queryFn: () => describeCLIAuthCode(code),
        enabled: code.length === 9,
        retry: false,
        staleTime: 0,
    });
}

export function useApproveCLIAuthCode() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ code, organizationId }: { code: string; organizationId: string }) => approveCLIAuthCode(code, organizationId),
        // The approval mints a key, so the API keys list is stale everywhere.
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["api-keys"] }),
    });
}

export function useDenyCLIAuthCode() {
    return useMutation({ mutationFn: (code: string) => denyCLIAuthCode(code) });
}
