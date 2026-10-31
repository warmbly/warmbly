// Tester accounts: the operator view.
//
// A tester is an account handed to somebody outside the team: a vendor's
// reviewer during an OAuth verification, an auditor, a support engineer. It is
// an ordinary account marked exempt from the emailed login code, because the
// holder cannot read this instance's mail.
//
// It can either get a workspace of its own or join one that already exists. The
// second is for a review judged on the app doing real work, where an empty
// workspace shows none of it. Joining names a role explicitly: this is the one
// path that grants workspace access without anybody in that workspace asking
// for it, so there is no default.
//
// The list exists because the way this goes wrong is not creating one, it is
// forgetting it. An exemption taken out for a two-week review is still there a
// year later unless something shows it.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, CopyIcon, FlaskConicalIcon, RotateCw, TrashIcon } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Callout, EmptyState, Panel, Segmented, StatusBadge } from "@/components/ui/kit";
import { cn } from "@/lib/utils";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import { createTester, listTesters, listOrganizationRoles, revokeTester, seedTesterWorkspace } from "@/lib/api/client/admin/testers";
import { listOrganizations } from "@/lib/api/client/admin/organizations";
import { DASHBOARD_URL } from "@/lib/env";
import type { AdminOrgListItem, CreatedTester } from "@/lib/api/models/admin";

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
    const [mode, setMode] = useState<"new" | "existing">("new");
    const [orgQuery, setOrgQuery] = useState("");
    const [org, setOrg] = useState<AdminOrgListItem | null>(null);
    const [roleID, setRoleID] = useState("");
    const [passwordHours, setPasswordHours] = useState(48);
    // Held in state, never refetched: the server returns it once and cannot
    // produce it again.
    const [created, setCreated] = useState<CreatedTester | null>(null);
    const [sampleTarget, setSampleTarget] = useState<{ organizationID: string; email: string } | null>(null);

    const testers = useQuery({
        queryKey: ["admin", "testers"],
        queryFn: () => listTesters(),
    });

    // Only searched once there is something to search on: the unfiltered first
    // page is a list of arbitrary workspaces, which is not a picker.
    const orgs = useQuery({
        queryKey: ["admin", "testers", "orgs", orgQuery],
        queryFn: () => listOrganizations({ q: orgQuery.trim() }),
        enabled: mode === "existing" && orgQuery.trim().length >= 2,
    });

    const roles = useQuery({
        queryKey: ["admin", "testers", "roles", org?.id],
        queryFn: () => listOrganizationRoles(org!.id),
        enabled: mode === "existing" && !!org,
    });

    const joining = mode === "existing";
    const ready = !!email.trim() && !!reason.trim() && (!joining || (!!org && !!roleID));

    const create = useMutation({
        mutationFn: () =>
            createTester({
                email: email.trim(),
                reason: reason.trim(),
                password_hours: passwordHours,
                ...(joining
                    ? { organization_id: org!.id, role_id: roleID }
                    : { org_name: orgName.trim() || undefined }),
            }),
        onSuccess: (t) => {
            setCreated(t);
            setEmail("");
            setOrgName("");
            setReason("");
            setOrgQuery("");
            setOrg(null);
            setRoleID("");
            setPasswordHours(48);
            qc.invalidateQueries({ queryKey: ["admin", "testers"] });
            toast.success("Tester created");
        },
        onError: (e: Error) => toast.error(e.message || "Could not create the tester"),
    });

    const revoke = useMutation({
        mutationFn: (id: string) => revokeTester(id),
        onSuccess: (r) => {
            qc.invalidateQueries({ queryKey: ["admin", "testers"] });
            toast.success(
                r.password_cleared
                    ? "Revoked: the password no longer works and every session is signed out"
                    : "Exemption revoked and every session signed out; the account now follows the instance policy",
            );
        },
        onError: (e: Error) => toast.error(e.message || "Could not revoke the exemption"),
    });

    const seed = useMutation({
        mutationFn: (orgID: string) => seedTesterWorkspace(orgID),
        onSuccess: (result) => {
            qc.invalidateQueries({ queryKey: ["admin", "testers"] });
            setSampleTarget(null);
            toast.success(result.created ? "Sample data added. Connect a real test mailbox separately." : "Sample data was already added; nothing was changed.");
        },
        onError: (e: Error) => toast.error(e.message || "Could not add sample data"),
    });

    function copy(text: string) {
        navigator.clipboard?.writeText(text).then(
            () => toast.success("Copied"),
            () => toast.error("Could not copy"),
        );
    }

    const rows = testers.data?.data ?? [];
    // A wrong sign-in address that looks plausible is the failure mode here: it
    // is copied straight into a vendor's verification form. The local default
    // surviving onto a deployed panel means nobody configured one.
    const looksUnset =
        /^https?:\/\/localhost(:|\/|$)/.test(DASHBOARD_URL) &&
        !/^https?:\/\/localhost(:|\/|$)/.test(window.location.origin);

    return (
        <div>
            <PageHeader
                title="Testers"
                meta={
                    rows.length > 0 ? (
                        <span className="text-[12.5px] tabular-nums text-muted-foreground">{rows.length}</span>
                    ) : undefined
                }
                description={
                    <>
                        Accounts for people outside the team. Each skips the emailed login code, because the
                        holder cannot read this instance&apos;s mail, and arrives with onboarding complete. Authentication still requires the password,
                        the captcha and the sign-in risk assessment. A tester either gets a workspace of its own
                        or joins one that already exists. A new Test workspace includes paid-feature access and 100 test credits until the password expires.
                        An existing workspace keeps its plan and the role you select. The password it is issued is an initial password: valid
                        for at most 48 hours, it cannot be extended, and the holder must replace it with their own at first sign-in.
                        Revoking a tester ends its password, issued or chosen, and signs out every session at once.
                        Use Add sample data on an active dedicated Test workspace to populate contacts, a draft campaign, templates and CRM examples.
                        Mailboxes must be connected separately.
                    </>
                }
            />

            {created && (
                <div className="mb-8 overflow-hidden rounded-lg border border-amber-500/25 bg-amber-500/[0.07]">
                    <div className="flex items-start gap-3 px-4 py-3">
                        <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
                        <div className="min-w-0 flex-1">
                            <div className="text-[13px] font-medium text-foreground">
                                Copy these now. The password is not stored anywhere readable.
                            </div>
                            <div className="mt-0.5 text-[12.5px] text-muted-foreground">
                                {created.joined_existing
                                    ? "This account is a member of an existing workspace and will land in it on sign-in."
                                    : "This account owns a new Test workspace with paid-feature access and 100 test credits until the password expires."}
                            </div>
                            <div className="mt-0.5 text-[12.5px] text-muted-foreground">
                                This is an initial password. It is valid until {fmt(created.password_expires_at)} (48 hours at most) and
                                must be changed at first sign-in, when the holder is asked to choose their own.
                            </div>
                        </div>
                        <Button size="sm" variant="outline" onClick={() => setCreated(null)}>
                            Done
                        </Button>
                    </div>
                    <dl className="divide-y divide-border/70 border-t border-amber-500/20 bg-card">
                        {[
                            ["Sign in at", DASHBOARD_URL],
                            ["Email", created.email],
                            ["Password", created.password],
                            ["Valid until", fmt(created.password_expires_at)],
                        ].map(([k, v]) => (
                            <div key={k} className="flex min-h-10 items-center gap-3 px-4 py-1.5 text-[13px]">
                                <dt className="w-24 shrink-0 text-muted-foreground">{k}</dt>
                                <dd
                                    className="min-w-0 flex-1 font-mono text-[12.5px] break-all text-foreground"
                                    data-ph-mask={k === "Password" ? "" : undefined}
                                >
                                    {v}
                                </dd>
                                <Button
                                    size="icon-xs"
                                    variant="ghost"
                                    onClick={() => copy(String(v))}
                                    aria-label={`Copy ${k}`}
                                >
                                    <CopyIcon className="size-3" />
                                </Button>
                            </div>
                        ))}
                    </dl>
                    {looksUnset && (
                        <div className="border-t border-red-500/20 bg-red-500/[0.06] px-4 py-2.5 text-[12.5px] text-red-700 dark:text-red-400">
                            That sign-in address is this panel&apos;s build-time default, not this
                            deployment&apos;s dashboard. Set <code className="font-mono">VITE_DASHBOARD_URL</code> (or{" "}
                            <code className="font-mono">WARMBLY_DASHBOARD_URL</code>) before sending it to anyone.
                        </div>
                    )}
                </div>
            )}

            <div className={cn("grid items-start gap-8", canManage && "lg:grid-cols-[minmax(0,1fr)_380px]")}>
                <section className="min-w-0">
                    <h2 className="mb-3 text-[13px] font-semibold text-foreground">
                        Active testers
                        {rows.length > 0 && (
                            <span className="ml-1.5 font-normal tabular-nums text-muted-foreground">{rows.length}</span>
                        )}
                    </h2>
                    {testers.isLoading ? (
                        <div className="space-y-px overflow-hidden surface-lit rounded-xl border border-border bg-card">
                            {Array.from({ length: 3 }).map((_, i) => (
                                <div key={i} className="flex h-14 items-center px-4">
                                    <Skeleton className="h-4 w-2/3" />
                                </div>
                            ))}
                        </div>
                    ) : testers.isError ? (
                        // Never fall through to the empty state here: "nothing is
                        // skipping the login code" is exactly the wrong thing to
                        // tell someone when the query failed.
                        <Callout
                            tone="danger"
                            icon={AlertTriangle}
                            title="Could not load tester accounts"
                            actions={
                                <Button size="sm" variant="outline" onClick={() => testers.refetch()}>
                                    <RotateCw className="size-3.5" />
                                    Retry
                                </Button>
                            }
                        >
                            This list is not authoritative. Retry before concluding that none exist.
                        </Callout>
                    ) : rows.length === 0 ? (
                        <div className="surface-lit rounded-xl border border-border bg-card">
                            <EmptyState
                                icon={FlaskConicalIcon}
                                title="No testers"
                                hint="None. Nothing on this instance is skipping the login code."
                            />
                        </div>
                    ) : (
                        <ul className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
                            {rows.map((t) => {
                                const expired =
                                    !!t.password_expires_at && new Date(t.password_expires_at) <= new Date();
                                return (
                                    <li
                                        key={t.user_id}
                                        className="flex items-center gap-3 border-b border-border/70 px-4 py-2.5 transition-colors last:border-b-0 hover:bg-accent/50"
                                    >
                                        <span
                                            aria-hidden
                                            className="grid size-7 shrink-0 place-items-center rounded-full bg-muted text-subtle-foreground"
                                        >
                                            <FlaskConicalIcon className="size-3.5" />
                                        </span>
                                        <div className="min-w-0 flex-1">
                                            <div className="truncate text-[13px] font-medium text-foreground">{t.email}</div>
                                            <div className="truncate text-xs text-muted-foreground">
                                                {t.reason || "no reason recorded"} · since {fmt(t.granted_at)}
                                            </div>
                                            {t.test_workspace_id && (
                                                <div className="text-xs text-muted-foreground">
                                                    Test workspace{t.sample_data_seeded_at ? ` · Sample data added ${fmt(t.sample_data_seeded_at)}` : " · No sample data added"}
                                                </div>
                                            )}
                                        </div>
                                        {t.password_expires_at && (
                                            <StatusBadge
                                                tone={expired ? "danger" : "neutral"}
                                                dot={expired}
                                                className="hidden sm:inline-flex"
                                            >
                                                Initial password {expired ? "expired" : "valid until"} {fmt(t.password_expires_at)}
                                            </StatusBadge>
                                        )}
                                        {canManage && (
                                            <>
                                                {t.test_workspace_id && (
                                                    <Button
                                                        size="xs"
                                                        variant="outline"
                                                        disabled={expired || !t.password_expires_at || !!t.sample_data_seeded_at || seed.isPending || revoke.isPending}
                                                        aria-label={`Add sample data for ${t.email}`}
                                                        onClick={() => setSampleTarget({ organizationID: t.test_workspace_id!, email: t.email })}
                                                    >
                                                        {t.sample_data_seeded_at ? "Sample data added" : "Add sample data"}
                                                    </Button>
                                                )}
                                                <Button
                                                    size="xs"
                                                    variant="ghost"
                                                    className="shrink-0 text-red-600 hover:bg-red-500/10 hover:text-red-700 dark:text-red-400 dark:hover:text-red-300"
                                                    disabled={revoke.isPending || seed.isPending}
                                                    onClick={() => revoke.mutate(t.user_id)}
                                                >
                                                    <TrashIcon className="size-3" /> Revoke
                                                </Button>
                                            </>
                                        )}
                                    </li>
                                );
                            })}
                        </ul>
                    )}
                </section>

                {canManage && (
                    <Panel title="Create a tester" className="lg:sticky lg:top-16" bodyClassName="space-y-4">
                        <Field label="Email">
                            <Input
                                type="email"
                                placeholder="reviewer@example.com"
                                value={email}
                                onChange={(e) => setEmail(e.target.value)}
                            />
                        </Field>

                        <Field label="Workspace">
                            <Segmented
                                ariaLabel="Workspace"
                                value={mode}
                                onChange={setMode}
                                fullWidth
                                options={[
                                    { value: "new", label: "New workspace" },
                                    { value: "existing", label: "Existing workspace" },
                                ]}
                            />
                            {mode === "new" ? (
                                <Input
                                    className="mt-2"
                                    placeholder="Workspace name (optional)"
                                    maxLength={64}
                                    value={orgName}
                                    onChange={(e) => setOrgName(e.target.value)}
                                />
                            ) : (
                                <div className="mt-2 space-y-2">
                                    <p className="text-xs leading-relaxed text-muted-foreground">
                                        The tester becomes a member of this workspace and gets none of its own, so
                                        signing in lands straight in it. It can see whatever the role below allows,
                                        including real mailboxes and real contacts.
                                    </p>

                                    {org ? (
                                        <div className="flex h-8 items-center gap-2 rounded-md border border-border bg-muted/40 px-2.5 text-[13px]">
                                            <span className="truncate font-medium text-foreground">{org.name}</span>
                                            <span className="truncate text-xs text-muted-foreground">{org.owner_email}</span>
                                            <Button
                                                size="xs"
                                                variant="ghost"
                                                className="-mr-1.5 ml-auto"
                                                onClick={() => {
                                                    setOrg(null);
                                                    setRoleID("");
                                                }}
                                            >
                                                Change
                                            </Button>
                                        </div>
                                    ) : (
                                        <>
                                            <Input
                                                placeholder="Search workspaces by name or owner"
                                                value={orgQuery}
                                                onChange={(e) => setOrgQuery(e.target.value)}
                                            />
                                            {orgs.isError ? (
                                                <div className="text-xs text-red-600 dark:text-red-400">
                                                    Could not search workspaces. Retry before concluding there is no match.
                                                </div>
                                            ) : orgs.isFetching ? (
                                                <Skeleton className="h-8 w-full" />
                                            ) : (orgs.data?.data.length ?? 0) > 0 ? (
                                                <ul className="max-h-40 divide-y divide-border/70 overflow-auto rounded-md border border-border">
                                                    {orgs.data!.data.map((o) => (
                                                        <li key={o.id}>
                                                            <button
                                                                type="button"
                                                                onClick={() => setOrg(o)}
                                                                className="w-full px-2.5 py-1.5 text-left transition-colors hover:bg-accent/60"
                                                            >
                                                                <div className="truncate text-[13px] font-medium text-foreground">{o.name}</div>
                                                                <div className="truncate text-xs text-muted-foreground">
                                                                    {o.owner_email} · {o.member_count} members
                                                                </div>
                                                            </button>
                                                        </li>
                                                    ))}
                                                </ul>
                                            ) : orgQuery.trim().length >= 2 ? (
                                                <div className="text-xs text-muted-foreground">No workspace matches that.</div>
                                            ) : null}
                                        </>
                                    )}

                                    {org && (
                                        <Select value={roleID} onValueChange={setRoleID}>
                                            <SelectTrigger className="w-full">
                                                <SelectValue
                                                    placeholder={roles.isFetching ? "Loading roles…" : "Choose a role…"}
                                                />
                                            </SelectTrigger>
                                            <SelectContent>
                                                {(roles.data?.data ?? []).map((r) => (
                                                    <SelectItem key={r.id} value={r.id}>
                                                        {r.name}
                                                    </SelectItem>
                                                ))}
                                            </SelectContent>
                                        </Select>
                                    )}
                                    {org && roles.isError && (
                                        <div className="text-xs text-red-600 dark:text-red-400">
                                            Could not load this workspace&apos;s roles, so there is nothing safe to pick.
                                        </div>
                                    )}
                                </div>
                            )}
                        </Field>

                        <Field label="Reason">
                            <Input
                                placeholder="Why this account exists, e.g. Google OAuth verification"
                                value={reason}
                                onChange={(e) => setReason(e.target.value)}
                            />
                        </Field>

                        <Field label="Password works for">
                            <Select value={String(passwordHours)} onValueChange={(v) => setPasswordHours(Number(v))}>
                                <SelectTrigger className="w-full">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    {[4, 12, 24, 48].map((d) => (
                                        <SelectItem key={d} value={String(d)}>
                                            {d} hours
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                            <p className="mt-1.5 text-xs text-muted-foreground">
                                Valid 48 hours at most and never extendable. The holder must change it at first sign-in.
                            </p>
                        </Field>

                        <div className="flex justify-end border-t border-border pt-4">
                            <Button size="sm" disabled={!ready || create.isPending} onClick={() => create.mutate()}>
                                Create
                            </Button>
                        </div>
                    </Panel>
                )}
            </div>

            <Dialog open={!!sampleTarget} onOpenChange={(open) => { if (!open && !seed.isPending) setSampleTarget(null); }}>
                <DialogContent showCloseButton={!seed.isPending}>
                    <DialogHeader>
                        <DialogTitle>Add sample data?</DialogTitle>
                        <DialogDescription>
                            Populate the dedicated Test workspace for {sampleTarget?.email}. Existing data is left untouched.
                        </DialogDescription>
                    </DialogHeader>
                    <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
                        <li>Six fictional, unsubscribed contacts in a sample contact list</li>
                        <li>One draft campaign with two connected sequence steps</li>
                        <li>Two reusable templates</li>
                        <li>A sample CRM pipeline, two deals, a task and a contact note</li>
                    </ul>
                    <Callout>
                        No mailbox is created and no email is sent. Connect a real test mailbox separately to review OAuth and inbox features.
                        Sample data is added once; repeating this action does not restore deleted examples or overwrite edits.
                    </Callout>
                    <DialogFooter>
                        <Button variant="outline" disabled={seed.isPending} onClick={() => setSampleTarget(null)}>Cancel</Button>
                        <Button disabled={seed.isPending || !sampleTarget || !canManage} onClick={() => sampleTarget && seed.mutate(sampleTarget.organizationID)}>
                            {seed.isPending ? "Adding sample data…" : "Add sample data"}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div>
            <div className="mb-1.5 text-xs font-medium text-muted-foreground">{label}</div>
            {children}
        </div>
    );
}
