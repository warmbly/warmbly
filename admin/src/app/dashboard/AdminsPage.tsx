// Admins: who holds operator-panel bits. Gated on GrantAdminAccess by the
// route. Grant and edit share one dialog; revoke confirms and warns when the
// target is you or the last admin who can still grant access. No poll: the
// list invalidates after each mutation.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Pencil, ShieldOff, UserPlus } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useConfirm } from "@/components/ConfirmDialog";
import { useMe } from "@/hooks/useMe";
import { useCursorPager } from "@/lib/useCursorPager";
import { AdminPerm } from "@/lib/auth/permissions";
import {
    listAdminPermissions,
    listAdmins,
    revokeAdminPermissions,
    type AdminInfo,
} from "@/lib/api/client/admin/admins";
import { GrantAdminDialog, type GrantTarget } from "./admins/GrantAdminDialog";
import { PermissionChips } from "./admins/PermissionChips";
import { hasBit } from "./admins/permissions";
import { fmtDate, userName } from "./fleet/format";

export default function AdminsPage() {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const me = useMe();
    const pager = useCursorPager();

    const adminsQ = useQuery({
        queryKey: ["admin", "admins", pager.cursor],
        queryFn: () => listAdmins(pager.cursor),
    });
    const catalogQ = useQuery({
        queryKey: ["admin", "permissions"],
        queryFn: listAdminPermissions,
        staleTime: 5 * 60_000,
    });
    const catalog = catalogQ.data ?? [];
    const rows = adminsQ.data?.data ?? [];

    const [dialog, setDialog] = useState<{ open: boolean; target: GrantTarget | null }>({ open: false, target: null });

    const revoke = useMutation({
        mutationFn: (userId: string) => revokeAdminPermissions(userId),
        onSuccess: (_res, userId) => {
            toast.success("Admin access revoked");
            qc.invalidateQueries({ queryKey: ["admin", "admins"] });
            qc.invalidateQueries({ queryKey: ["admin", "users"] });
            if (userId === me.data?.id) qc.invalidateQueries({ queryKey: ["me"] });
        },
        onError: (e: Error) => toast.error(e.message || "Revoke failed"),
    });

    const granters = rows.filter((a) => hasBit(a.admin_permissions, AdminPerm.GrantAdminAccess));

    async function onRevoke(a: AdminInfo) {
        const isSelf = a.id === me.data?.id;
        const lastGranter =
            hasBit(a.admin_permissions, AdminPerm.GrantAdminAccess) && granters.length <= 1 && !adminsQ.data?.pagination.has_more;
        const warnings: string[] = [];
        if (isSelf) warnings.push("This is your own account: you will lose access to this panel immediately.");
        if (lastGranter)
            warnings.push(
                "This is the only admin who can grant admin access. After this, nobody can add or edit admins from the panel; it would take a database change to recover.",
            );
        const ok = await confirm({
            title: `Revoke all admin permissions from ${a.email}?`,
            description: [
                "Every operator-panel bit is removed. Their workspace roles are untouched.",
                ...warnings,
            ].join(" "),
            confirmLabel: isSelf ? "Revoke my access" : "Revoke",
            destructive: true,
        });
        if (ok) revoke.mutate(a.id);
    }

    const columns: Column<AdminInfo>[] = [
        {
            id: "admin",
            header: "Admin",
            cell: (a) => (
                <div>
                    <div className="font-medium">
                        {userName(a)}
                        {a.id === me.data?.id && (
                            <span className="ml-1.5 text-[10px] uppercase tracking-wider text-muted-foreground">you</span>
                        )}
                    </div>
                    <div className="text-[11px] text-muted-foreground">{a.email}</div>
                </div>
            ),
            csv: (a) => a.email,
        },
        {
            id: "granted",
            header: "Granted",
            cell: (a) => (
                <div className="text-xs text-muted-foreground">
                    <div>{fmtDate(a.admin_granted_at)}</div>
                    {a.granted_by_user ? (
                        <div className="text-[11px]">by {a.granted_by_user.email}</div>
                    ) : a.admin_granted_by ? (
                        <div className="font-mono text-[10px]">by {a.admin_granted_by.slice(0, 8)}</div>
                    ) : null}
                </div>
            ),
            csv: (a) => a.admin_granted_at || "",
        },
        {
            id: "permissions",
            header: "Permissions",
            cell: (a) => <PermissionChips mask={a.admin_permissions} catalog={catalog} />,
            csv: (a) => catalog.filter((p) => hasBit(a.admin_permissions, p.permission)).map((p) => p.name).join(" "),
        },
        {
            id: "actions",
            header: "",
            align: "right",
            cell: (a) => (
                <div className="flex justify-end gap-1">
                    <Button
                        size="xs"
                        variant="outline"
                        onClick={(e) => {
                            e.stopPropagation();
                            setDialog({ open: true, target: a });
                        }}
                    >
                        <Pencil className="size-3" />
                        Edit
                    </Button>
                    <Button
                        size="xs"
                        variant="outline"
                        className="text-red-700 hover:bg-red-50"
                        disabled={revoke.isPending}
                        onClick={(e) => {
                            e.stopPropagation();
                            void onRevoke(a);
                        }}
                    >
                        <ShieldOff className="size-3" />
                        Revoke
                    </Button>
                </div>
            ),
        },
    ];

    return (
        <div>
            <PageHeader
                title="Admins"
                description="Who can open this panel and what they can do in it. These permission bits gate the operator surface only; they are not workspace roles and grant nothing inside a customer's workspace."
            >
                <Button size="sm" onClick={() => setDialog({ open: true, target: null })}>
                    <UserPlus className="size-4" />
                    Grant admin
                </Button>
            </PageHeader>

            <DataTable
                columns={columns}
                rows={rows}
                getRowId={(a) => a.id}
                loading={adminsQ.isLoading || catalogQ.isLoading}
                error={adminsQ.error ?? catalogQ.error}
                onRetry={() => {
                    adminsQ.refetch();
                    catalogQ.refetch();
                }}
                errorTitle="Failed to load admins"
                pager={{
                    canPrev: pager.canPrev,
                    canNext: !!adminsQ.data?.pagination.has_more,
                    onPrev: pager.prev,
                    onNext: () => pager.next(adminsQ.data?.pagination.next_cursor),
                    page: pager.page,
                    shown: rows.length,
                    total: adminsQ.data?.pagination.total,
                }}
                storageKey="admin.admins"
                csvName="warmbly-admins"
                noun="admins"
                emptyTitle="No admins"
                emptyHint="Nobody holds operator bits yet, which should not be possible while you are reading this. Grant one above."
            />

            <GrantAdminDialog
                open={dialog.open}
                onOpenChange={(v) => setDialog((d) => ({ ...d, open: v }))}
                catalog={catalog}
                target={dialog.target}
                selfId={me.data?.id}
            />
        </div>
    );
}
