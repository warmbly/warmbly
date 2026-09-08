// Grant or edit an admin's bits. Grant mode starts with a user search; edit
// mode is prefilled from the row. A preset fills the checkboxes, and the mask
// sent is always the OR of what is ticked.

import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { grantAdminPermissions, type PermissionInfo } from "@/lib/api/client/admin/admins";
import { cn } from "@/lib/utils";
import { UserPicker, type PickedUser } from "./UserPicker";
import { userName } from "../fleet/format";
import { groupByCategory, hasBit, humanize, matchPreset, presetMask, PRESETS } from "./permissions";

export interface GrantTarget {
    id: string;
    email: string;
    first_name?: string;
    last_name?: string;
    admin_permissions: number;
}

export function GrantAdminDialog({
    open,
    onOpenChange,
    catalog,
    target,
    selfId,
}: {
    open: boolean;
    onOpenChange: (v: boolean) => void;
    catalog: PermissionInfo[];
    /** Present in edit mode; absent in grant mode. */
    target: GrantTarget | null;
    selfId?: string;
}) {
    const qc = useQueryClient();
    const edit = !!target;
    const [user, setUser] = useState<PickedUser | null>(null);
    const [mask, setMask] = useState(0);

    // Reset per open so a reopened dialog never carries a stale draft.
    useEffect(() => {
        if (!open) return;
        setUser(target ? { ...target, first_name: target.first_name ?? "", last_name: target.last_name ?? "" } : null);
        setMask(target?.admin_permissions ?? 0);
    }, [open, target]);

    // Picking a user who is already an admin starts from their current bits.
    function pickUser(u: PickedUser | null) {
        setUser(u);
        if (u && u.admin_permissions > 0) setMask(u.admin_permissions);
    }

    const liveMask = catalog.reduce((m, c) => m | c.permission, 0);
    const shown = mask & liveMask;
    const active = matchPreset(shown, catalog);
    const count = catalog.filter((c) => hasBit(shown, c.permission)).length;
    const editingSelf = !!user && user.id === selfId;
    const grantBit = catalog.find((c) => c.name === "grant_admin_access")?.permission ?? 0;
    const dropsOwnGrant = editingSelf && grantBit > 0 && !hasBit(shown, grantBit);

    const mutation = useMutation({
        mutationFn: () => grantAdminPermissions(user!.id, shown),
        onSuccess: () => {
            toast.success(edit ? `Permissions updated for ${user?.email}` : `${user?.email} is now an admin`);
            qc.invalidateQueries({ queryKey: ["admin", "admins"] });
            qc.invalidateQueries({ queryKey: ["admin", "users"] });
            if (editingSelf) qc.invalidateQueries({ queryKey: ["me"] });
            onOpenChange(false);
        },
        onError: (e: Error) => toast.error(e.message || "Grant failed"),
    });

    const canSubmit = !!user && shown > 0 && !mutation.isPending;

    return (
        <Dialog
            open={open}
            onOpenChange={(v) => {
                if (!v && mutation.isPending) return;
                onOpenChange(v);
            }}
        >
            <DialogContent
                className="sm:max-w-xl"
                onEscapeKeyDown={(e) => {
                    if (document.querySelector("[data-floating]")) e.preventDefault();
                }}
            >
                <DialogHeader>
                    <DialogTitle>{edit ? "Edit admin permissions" : "Grant admin access"}</DialogTitle>
                    <DialogDescription>
                        These bits gate the operator panel only. They are separate from any role the user holds inside a
                        workspace.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4">
                    <div className="space-y-1.5">
                        <div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">User</div>
                        {edit && user ? (
                            <div className="rounded-md border border-border bg-card px-2.5 py-1.5 text-[12.5px]">
                                <span className="font-medium">{userName(user)}</span>
                                <span className="ml-1.5 text-[11px] text-muted-foreground">{user.email}</span>
                            </div>
                        ) : (
                            <UserPicker value={user} onChange={pickUser} autoFocus />
                        )}
                    </div>

                    <div className="space-y-1.5">
                        <div className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">Preset</div>
                        <div className="flex flex-wrap gap-1.5">
                            {PRESETS.map((p) => (
                                <button
                                    key={p.id}
                                    type="button"
                                    title={p.hint}
                                    onClick={() => setMask(presetMask(p.id, catalog))}
                                    className={cn(
                                        "rounded-md border px-2.5 py-1 text-[12px] transition-colors",
                                        active === p.id
                                            ? "border-[var(--admin-accent)] bg-[var(--admin-accent-soft)] font-medium text-[var(--admin-accent-strong)]"
                                            : "border-border text-muted-foreground hover:bg-muted/60 hover:text-foreground",
                                    )}
                                >
                                    {p.label}
                                </button>
                            ))}
                            <button
                                type="button"
                                onClick={() => setMask(0)}
                                className="rounded-md border border-border px-2.5 py-1 text-[12px] text-muted-foreground hover:bg-muted/60 hover:text-foreground"
                            >
                                Clear
                            </button>
                        </div>
                        <p className="text-[11px] text-muted-foreground">
                            {active ? PRESETS.find((p) => p.id === active)?.hint : "Custom selection."}
                        </p>
                    </div>

                    <div className="max-h-72 space-y-3 overflow-auto rounded-md border border-border bg-card p-3">
                        {groupByCategory(catalog).map((g) => (
                            <div key={g.category}>
                                <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                                    {g.category}
                                </div>
                                <div className="grid gap-1.5 sm:grid-cols-2">
                                    {g.items.map((p) => {
                                        const on = hasBit(shown, p.permission);
                                        return (
                                            <label
                                                key={p.name}
                                                className="flex cursor-pointer items-start gap-2 rounded px-1.5 py-1 text-[12.5px] hover:bg-muted/50"
                                            >
                                                <Checkbox
                                                    checked={on}
                                                    onCheckedChange={(v) =>
                                                        setMask((m) => (v ? m | p.permission : m & ~p.permission))
                                                    }
                                                    className="mt-0.5"
                                                />
                                                <span className="min-w-0">
                                                    <span className="block leading-tight">{humanize(p.name)}</span>
                                                    <span className="block text-[11px] leading-tight text-muted-foreground">
                                                        {p.description}
                                                    </span>
                                                </span>
                                            </label>
                                        );
                                    })}
                                </div>
                            </div>
                        ))}
                        {catalog.length === 0 && (
                            <div className="text-xs text-muted-foreground">The permission catalog is empty.</div>
                        )}
                    </div>

                    {dropsOwnGrant && (
                        <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[12px] text-amber-800">
                            You are removing your own ability to grant admin access. You will not be able to undo this
                            from the panel.
                        </div>
                    )}
                </div>

                <DialogFooter className="sm:items-center sm:justify-between">
                    <span className="text-[11px] tabular-nums text-muted-foreground">
                        {count} of {catalog.length} bits · mask {shown}
                    </span>
                    <div className="flex gap-2">
                        <Button variant="outline" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>
                            Cancel
                        </Button>
                        <Button onClick={() => mutation.mutate()} disabled={!canSubmit}>
                            {mutation.isPending ? "Saving…" : edit ? "Save permissions" : "Grant access"}
                        </Button>
                    </div>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
