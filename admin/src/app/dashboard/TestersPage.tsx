// Tester accounts — the operator view.
//
// A tester is an account handed to somebody outside the team: a vendor's
// reviewer during an OAuth verification, an auditor, a support engineer. It is
// an ordinary account with its own workspace, marked exempt from the emailed
// login code because the holder cannot read this instance's mail.
//
// The list exists because the way this goes wrong is not creating one, it is
// forgetting it. An exemption taken out for a two-week review is still there a
// year later unless something shows it.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { CopyIcon, FlaskConicalIcon, TrashIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import { createTester, listTesters, revokeTester } from "@/lib/api/client/admin/testers";
import type { CreatedTester } from "@/lib/api/models/admin";

function fmt(ts?: string | null) {
    if (!ts) return "—";
    return new Date(ts).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

export default function TestersPage() {
    const qc = useQueryClient();
    const canManage = useAdminPerm(AdminPerm.ManageTesters);

    const [email, setEmail] = useState("");
    const [orgName, setOrgName] = useState("");
    const [reason, setReason] = useState("");
    // Held in state, never refetched: the server returns it once and cannot
    // produce it again.
    const [created, setCreated] = useState<CreatedTester | null>(null);

    const testers = useQuery({
        queryKey: ["admin", "testers"],
        queryFn: () => listTesters(),
    });

    const create = useMutation({
        mutationFn: () =>
            createTester({ email: email.trim(), org_name: orgName.trim() || undefined, reason: reason.trim() }),
        onSuccess: (t) => {
            setCreated(t);
            setEmail("");
            setOrgName("");
            setReason("");
            qc.invalidateQueries({ queryKey: ["admin", "testers"] });
            toast.success("Tester created");
        },
        onError: (e: Error) => toast.error(e.message || "Could not create the tester"),
    });

    const revoke = useMutation({
        mutationFn: (id: string) => revokeTester(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["admin", "testers"] });
            toast.success("Exemption revoked; the account now follows the instance policy");
        },
        onError: (e: Error) => toast.error(e.message || "Could not revoke the exemption"),
    });

    function copy(text: string) {
        navigator.clipboard?.writeText(text).then(
            () => toast.success("Copied"),
            () => toast.error("Could not copy"),
        );
    }

    const rows = testers.data?.data ?? [];

    return (
        <div className="p-4 space-y-6">
            <div>
                <h1 className="text-sm font-semibold flex items-center gap-2">
                    <FlaskConicalIcon className="size-4 text-muted-foreground" />
                    Testers
                </h1>
                <p className="text-xs text-muted-foreground mt-1 max-w-[70ch]">
                    Accounts for people outside the team. Each gets its own workspace and skips the emailed
                    login code, because the holder cannot read this instance&apos;s mail. Everything else still
                    applies: the password, the captcha and the sign-in risk assessment.
                </p>
            </div>

            {created && (
                <div className="border border-amber-200 bg-amber-50 rounded-lg p-3">
                    <div className="text-[12.5px] font-medium text-amber-900">
                        Copy these now. The password is not stored anywhere readable.
                    </div>
                    <dl className="mt-2 grid gap-1.5 text-xs">
                        {[
                            ["Sign in at", window.location.origin.replace("admin.", "dev.")],
                            ["Email", created.email],
                            ["Password", created.password],
                        ].map(([k, v]) => (
                            <div key={k} className="flex items-center gap-2">
                                <dt className="w-20 shrink-0 text-amber-800">{k}</dt>
                                <dd className="font-mono break-all">{v}</dd>
                                <button
                                    type="button"
                                    onClick={() => copy(String(v))}
                                    className="shrink-0 rounded p-1 hover:bg-amber-100"
                                    aria-label={`Copy ${k}`}
                                >
                                    <CopyIcon className="size-3" />
                                </button>
                            </div>
                        ))}
                    </dl>
                    <Button size="sm" variant="ghost" className="mt-2 h-7" onClick={() => setCreated(null)}>
                        Done
                    </Button>
                </div>
            )}

            {canManage && (
                <div className="border border-border rounded-lg bg-card p-3 space-y-2 max-w-[520px]">
                    <div className="text-[12.5px] font-medium">Create a tester</div>
                    <input
                        className="w-full h-8 rounded-md border border-border bg-background px-2 text-xs"
                        placeholder="reviewer@example.com"
                        value={email}
                        onChange={(e) => setEmail(e.target.value)}
                    />
                    <input
                        className="w-full h-8 rounded-md border border-border bg-background px-2 text-xs"
                        placeholder="Workspace name (optional)"
                        value={orgName}
                        onChange={(e) => setOrgName(e.target.value)}
                    />
                    <input
                        className="w-full h-8 rounded-md border border-border bg-background px-2 text-xs"
                        placeholder="Why this account exists, e.g. Google OAuth verification"
                        value={reason}
                        onChange={(e) => setReason(e.target.value)}
                    />
                    <Button
                        size="sm"
                        className="h-8"
                        disabled={!email.trim() || !reason.trim() || create.isPending}
                        onClick={() => create.mutate()}
                    >
                        Create
                    </Button>
                </div>
            )}

            <div className="border border-border rounded-lg bg-card">
                <div className="px-3 py-2 border-b border-border text-[12.5px] font-medium">
                    Active testers {rows.length > 0 && <Badge variant="outline" className="ml-1">{rows.length}</Badge>}
                </div>
                {testers.isLoading ? (
                    <div className="p-3"><Skeleton className="h-16 w-full" /></div>
                ) : testers.isError ? (
                    // Never fall through to the empty state here: "nothing is
                    // skipping the login code" is exactly the wrong thing to
                    // tell someone when the query failed.
                    <div className="p-3 text-xs text-red-600">
                        Could not load tester accounts, so this list is not authoritative. Retry before
                        concluding that none exist.
                    </div>
                ) : rows.length === 0 ? (
                    <div className="p-3 text-xs text-muted-foreground">
                        None. Nothing on this instance is skipping the login code.
                    </div>
                ) : (
                    <ul className="divide-y divide-border">
                        {rows.map((t) => (
                            <li key={t.user_id} className="flex items-center gap-3 px-3 py-2 text-xs">
                                <div className="flex-1 min-w-0">
                                    <div className="font-medium truncate">{t.email}</div>
                                    <div className="text-muted-foreground truncate">
                                        {t.reason || "no reason recorded"} · since {fmt(t.granted_at)}
                                    </div>
                                </div>
                                {canManage && (
                                    <Button
                                        size="sm"
                                        variant="outline"
                                        className="h-7 shrink-0"
                                        disabled={revoke.isPending}
                                        onClick={() => revoke.mutate(t.user_id)}
                                    >
                                        <TrashIcon className="size-3 mr-1" /> Revoke
                                    </Button>
                                )}
                            </li>
                        ))}
                    </ul>
                )}
            </div>
        </div>
    );
}
