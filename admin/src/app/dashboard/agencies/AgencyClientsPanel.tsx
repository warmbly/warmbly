// The agency's client workspaces, with the two things an operator may do to
// the list: bring a workspace under the agency and release one from it.

import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Plus, Unlink } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Panel } from "@/components/ui/kit";
import { useConfirm } from "@/components/ConfirmDialog";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import { ReauthCancelledError } from "@/lib/api/client";
import { attachAgencyClient, detachAgencyClient, type AdminAgencyClient, type AdminAgencyDetail } from "@/lib/api/client/admin/agencies";
import { TONE_TEXT } from "@/lib/tones";
import { cn } from "@/lib/utils";
import { OrgPicker, type PickedOrg } from "../fleet/OrgPicker";
import { useAgencyWrite } from "./useAgencyWrite";

export function AgencyClientsPanel({ detail: d }: { detail: AdminAgencyDetail }) {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const [attaching, setAttaching] = useState(false);
    const a = d.agency;
    // The workspace's own page shows its agency and the plan it receives from it.
    const refreshWorkspaces = () => void qc.invalidateQueries({ queryKey: ["admin", "organizations"] });

    const release = useAgencyWrite(
        a.id,
        (orgId: string) => detachAgencyClient(a.id, orgId),
        { done: "Workspace released", failed: "Failed to release the workspace" },
        refreshWorkspaces,
    );

    async function onRelease(c: AdminAgencyClient) {
        const ok = await confirm({
            title: `Release ${c.name} from ${a.name}?`,
            description:
                "The workspace keeps its data. It loses the agency's plan, and the agency's staff lose their access to it.",
            confirmLabel: "Release",
            destructive: true,
        });
        if (ok) await release.mutateAsync(c.organization_id).catch(() => undefined);
    }

    return (
        <Panel
            title="Clients"
            actions={
                canManage && (
                    <Button size="sm" variant="outline" onClick={() => setAttaching(true)}>
                        <Plus className="size-3.5" /> Attach workspace
                    </Button>
                )
            }
        >
            {d.clients.length === 0 ? (
                <p className="text-sm text-muted-foreground">No clients.</p>
            ) : (
                <table className="w-full text-sm">
                    <thead>
                        <tr className="text-left text-muted-foreground">
                            <th className="py-1.5 font-medium">Workspace</th>
                            <th className="py-1.5 font-medium text-right">Sent this period</th>
                            <th className="py-1.5 font-medium text-right">Monthly cap</th>
                            {canManage && <th className="py-1.5" />}
                        </tr>
                    </thead>
                    <tbody>
                        {d.clients.map((c) => {
                            const used = d.usage.clients.find((u) => u.organization_id === c.organization_id)?.used ?? 0;
                            return (
                                <tr key={c.organization_id} className="group border-t border-border">
                                    <td className="py-1.5">
                                        <Link to={`/organizations/${c.organization_id}`} className="underline underline-offset-2">
                                            {c.name}
                                        </Link>
                                    </td>
                                    <td className="py-1.5 text-right tabular-nums">{used.toLocaleString()}</td>
                                    <td className="py-1.5 text-right tabular-nums">
                                        {c.monthly_send_limit == null ? "None" : c.monthly_send_limit.toLocaleString()}
                                    </td>
                                    {canManage && (
                                        <td className="py-1 pl-3 text-right">
                                            <Button
                                                size="xs"
                                                variant="ghost"
                                                aria-label={`Release ${c.name}`}
                                                className={cn(
                                                    TONE_TEXT.danger,
                                                    "hover:bg-red-500/10 hover:text-red-700 focus-visible:opacity-100 opacity-100 md:opacity-0 md:group-hover:opacity-100 dark:hover:text-red-300",
                                                )}
                                                onClick={() => onRelease(c)}
                                                disabled={release.isPending}
                                            >
                                                <Unlink className="size-3" />
                                                Release
                                            </Button>
                                        </td>
                                    )}
                                </tr>
                            );
                        })}
                    </tbody>
                </table>
            )}

            <Dialog open={attaching && canManage} onOpenChange={setAttaching}>
                <DialogContent
                    onEscapeKeyDown={(e) => {
                        // The workspace picker owns Escape while its list is open.
                        if (document.querySelector("[data-floating]")) e.preventDefault();
                    }}
                >
                    <DialogHeader>
                        <DialogTitle>Attach a workspace</DialogTitle>
                        <DialogDescription>
                            Brings a workspace under {a.name}. It receives the agency's plan, and the agency's staff get access to
                            it. The workspace's owner must already be on the agency's staff, and it cannot have a subscription of
                            its own. You may be asked to confirm it is you.
                        </DialogDescription>
                    </DialogHeader>
                    <AttachForm agencyId={a.id} onClose={() => setAttaching(false)} onAttached={refreshWorkspaces} />
                </DialogContent>
            </Dialog>
        </Panel>
    );
}

function AttachForm({ agencyId, onClose, onAttached }: { agencyId: string; onClose: () => void; onAttached: () => void }) {
    const [org, setOrg] = useState<PickedOrg | null>(null);
    const attach = useAgencyWrite(
        agencyId,
        (orgId: string) => attachAgencyClient(agencyId, orgId),
        { done: "Workspace attached", failed: "Failed to attach the workspace", quiet: true },
        () => {
            onAttached();
            onClose();
        },
    );
    const refused = attach.error && !(attach.error instanceof ReauthCancelledError) ? attach.error : null;

    function submit(e: FormEvent) {
        e.preventDefault();
        if (org && !attach.isPending) attach.mutate(org.id);
    }

    return (
        <form onSubmit={submit} className="grid gap-4">
            <div className="space-y-1.5">
                <Label className="text-xs font-medium text-muted-foreground">Workspace</Label>
                <OrgPicker
                    value={org}
                    onChange={(v) => {
                        setOrg(v);
                        attach.reset();
                    }}
                    autoFocus
                    disabled={attach.isPending}
                />
                {refused && (
                    <p role="alert" className="text-[12.5px] leading-relaxed text-[var(--admin-danger)]">
                        {refused.message || "Failed to attach the workspace."}
                    </p>
                )}
            </div>
            <DialogFooter>
                <Button type="button" variant="outline" onClick={onClose}>
                    Cancel
                </Button>
                <Button type="submit" disabled={!org || attach.isPending}>
                    {attach.isPending ? "Attaching…" : "Attach workspace"}
                </Button>
            </DialogFooter>
        </form>
    );
}
