import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    twoFactorStatus,
    twoFactorEnrollStart,
    twoFactorEnrollConfirm,
    twoFactorDisable,
    twoFactorVerify,
} from "../../client/auth/twoFactor";

const STATUS_KEY = ["auth", "2fa", "status"];

export function useTwoFactorStatus() {
    return useQuery({ queryKey: STATUS_KEY, queryFn: twoFactorStatus, staleTime: 30_000 });
}

export function useTwoFactorEnrollStart() {
    return useMutation({ mutationFn: () => twoFactorEnrollStart() });
}

export function useTwoFactorEnrollConfirm() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (code: string) => twoFactorEnrollConfirm(code),
        onSuccess: () => qc.invalidateQueries({ queryKey: STATUS_KEY }),
    });
}

export function useTwoFactorDisable() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (code: string) => twoFactorDisable(code),
        onSuccess: () => qc.invalidateQueries({ queryKey: STATUS_KEY }),
    });
}

export function useTwoFactorVerify() {
    return useMutation({
        mutationFn: ({ pending_token, code }: { pending_token: string; code: string }) =>
            twoFactorVerify(pending_token, code),
    });
}
