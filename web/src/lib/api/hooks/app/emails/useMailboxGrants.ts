// Admin grants and their directories, under their own root so a mailbox
// invalidation does not refetch them; the mailbox_grant audit spine keeps
// every teammate's view live.
import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import switchToAppPassword from "@/lib/api/client/app/emails/switchToAppPassword";
import {
    checkGrant,
    connectGrantUsers,
    deleteGrant,
    getGrantConfig,
    getSigninMigration,
    listGrantUsers,
    listGrants,
} from "@/lib/api/client/app/emails/imports/mailboxGrants";
import type { MailboxImport } from "@/lib/api/models/app/emails/MailboxImport";
import type { DirectoryUser, DomainGrant, GrantConnectRequest } from "@/lib/api/models/app/emails/MailboxSources";
import { sourceMutationKey } from "./mailboxSourceBusy";
import { SENDING_DOMAINS_KEY } from "./useSendingDomains";
import { MAILBOX_SIGNIN_KEY } from "./useMailboxSignin";

export const GRANTS_KEY = ["mailbox-grants"] as const;
export const SIGNIN_MIGRATION_KEY = [...GRANTS_KEY, "migration"] as const;
// Mutations the sign-in migration dialog waits for before it can close.
export const SIGNIN_MIGRATION_MUTATION = ["signin-migration"] as const;

// Boot-time configuration; a backend without the service answers 404 and reads as off.
export function useGrantConfig(enabled = true) {
    return useQuery({
        queryKey: [...GRANTS_KEY, "config"],
        queryFn: getGrantConfig,
        enabled,
        staleTime: 10 * 60_000,
        retry: false,
    });
}

export function useGrants(enabled = true) {
    return useQuery({
        queryKey: [...GRANTS_KEY, "list"],
        queryFn: listGrants,
        enabled,
        staleTime: 15_000,
    });
}

// Mailboxes on the retiring per-mailbox Google sign-in; a backend without the route answers 404 and reads as none.
export const signinMigrationQuery = queryOptions({
    queryKey: SIGNIN_MIGRATION_KEY,
    queryFn: getSigninMigration,
    staleTime: 60_000,
    retry: false,
});

export function useSigninMigration(enabled = true) {
    return useQuery({
        ...signinMigrationQuery,
        enabled,
    });
}

// Each read asks Google or Microsoft for the directory, so it is not refetched on focus.
export function useGrantUsers(id: string | null) {
    return useQuery({
        queryKey: grantUsersKey(id ?? ""),
        queryFn: () => listGrantUsers(id!),
        enabled: !!id,
        staleTime: 60_000,
        refetchOnWindowFocus: false,
        retry: false,
    });
}

// Writes the grant into the list so the caller sees it before the refetch lands.
export function useStoreGrant() {
    const qc = useQueryClient();
    return (g: DomainGrant) => {
        qc.setQueryData<{ data: DomainGrant[] }>([...GRANTS_KEY, "list"], (prev) =>
            prev ? { data: [g, ...prev.data.filter((x) => x.id !== g.id)] } : prev,
        );
        qc.invalidateQueries({ queryKey: GRANTS_KEY });
    };
}

export function useCheckGrant() {
    const store = useStoreGrant();
    const qc = useQueryClient();
    return useMutation({
        mutationKey: sourceMutationKey("grant-check"),
        mutationFn: (id: string) => checkGrant(id),
        onSuccess: (g) => {
            store(g);
            // A recovered grant restarts its mailboxes.
            qc.invalidateQueries({ queryKey: ["emails", "list"] });
        },
    });
}

export function useDeleteGrant() {
    const qc = useQueryClient();
    return useMutation({
        mutationKey: sourceMutationKey("grant-delete"),
        mutationFn: (id: string) => deleteGrant(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: GRANTS_KEY });
            qc.invalidateQueries({ queryKey: ["emails", "list"] });
            qc.invalidateQueries({ queryKey: SENDING_DOMAINS_KEY });
        },
    });
}

export function useConnectGrantUsers() {
    const qc = useQueryClient();
    return useMutation({
        mutationKey: sourceMutationKey("grant-connect"),
        mutationFn: ({ id, body }: { id: string; body: GrantConnectRequest }) => connectGrantUsers(id, body),
        onSuccess: (job) => {
            qc.setQueryData<MailboxImport>(["emails", "imports", job.id], job);
            qc.invalidateQueries({ queryKey: ["emails", "imports"] });
            qc.invalidateQueries({ queryKey: GRANTS_KEY });
        },
    });
}

export function grantUsersKey(id: string) {
    return [...GRANTS_KEY, id, "users"] as const;
}

/** Moves mailboxes on per-mailbox sign-in onto a grant: they connect through it in place, keeping their history. */
export function useMoveToGrant() {
    const qc = useQueryClient();
    const connect = useConnectGrantUsers();
    return useMutation({
        mutationKey: [...SIGNIN_MIGRATION_MUTATION, "move"],
        mutationFn: async ({ grantId, emails }: { grantId: string; emails: string[] }) => {
            // A fresh directory read: the one cached for the picker may predate these mailboxes.
            const users = await qc.fetchQuery({ queryKey: grantUsersKey(grantId), queryFn: () => listGrantUsers(grantId), staleTime: 0 });
            const want = new Set(emails.map((e) => e.toLowerCase()));
            const movable = users.data.filter((u: DirectoryUser) => u.upgrade && u.enabled && want.has(u.email.toLowerCase()));
            if (movable.length === 0) throw new MoveError(emails.length);
            const job = await connect.mutateAsync({
                id: grantId,
                body: { user_ids: movable.map((u) => u.id), options: { on_existing: "update", settings: {} } },
            });
            return { job, moved: movable.length, missing: want.size - movable.length };
        },
    });
}

/** None of the mailboxes asked for is in the grant's directory as an active account. */
export class MoveError extends Error {
    constructor(count: number) {
        super(
            count === 1
                ? "This address is not an active account in the domain's directory, so the grant cannot sign it in. Switch it to an app password instead."
                : "None of these addresses is an active account in the domain's directory, so the grant cannot sign them in.",
        );
    }
}

/** Switches a Google sign-in mailbox to an app password in place. */
export function useSwitchToAppPassword() {
    const qc = useQueryClient();
    return useMutation({
        mutationKey: [...SIGNIN_MIGRATION_MUTATION, "app-password"],
        mutationFn: ({ id, appPassword }: { id: string; appPassword: string }) => switchToAppPassword(id, appPassword),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: SIGNIN_MIGRATION_KEY });
            qc.invalidateQueries({ queryKey: ["emails"] });
            qc.invalidateQueries({ queryKey: ["analytics", "accounts"] });
            qc.invalidateQueries({ queryKey: MAILBOX_SIGNIN_KEY });
        },
    });
}
