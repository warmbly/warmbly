// The per-account exemption from the emailed sign-in code, for an account an
// outside reviewer (an App Store review, an OAuth verification) signs in with.

import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useConfirm } from "@/components/ConfirmDialog";
import { Button } from "@/components/ui/button";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import { setUserLoginCodeExempt } from "@/lib/api/client/admin/users";
import type { AdminUserDetail } from "@/lib/api/models/admin";

const REASON_MAX = 500;

export function UserLoginCodeCard({ user }: { user: AdminUserDetail }) {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const canManage = useAdminPerm(AdminPerm.ManageTesters);
    const [granting, setGranting] = useState(false);
    const [reason, setReason] = useState("");
    const exempt = !!user.login_code_exempt;

    function refresh() {
        qc.invalidateQueries({ queryKey: ["admin", "users", user.id] });
        qc.invalidateQueries({ queryKey: ["admin", "users"] });
        qc.invalidateQueries({ queryKey: ["admin", "testers"] });
    }

    const grant = useMutation({
        mutationFn: (why: string) => setUserLoginCodeExempt(user.id, { exempt: true, reason: why }),
        onSuccess: () => {
            refresh();
            toast.success("This account will not be asked for an emailed sign-in code");
            setGranting(false);
            setReason("");
        },
        onError: (e: Error) => toast.error(e.message || "Could not grant the exemption"),
    });

    const clear = useMutation({
        mutationFn: () => setUserLoginCodeExempt(user.id, { exempt: false }),
        onSuccess: (res) => {
            refresh();
            toast.success(
                res.password_cleared
                    ? "Exemption removed, the handed-out password cleared and every session signed out"
                    : "Exemption removed and every session signed out",
            );
        },
        onError: (e: Error) => toast.error(e.message || "Could not remove the exemption"),
    });

    async function onToggle(next: boolean) {
        if (next) {
            setGranting(true);
            return;
        }
        const ok = await confirm({
            title: `Require the emailed sign-in code for ${user.email} again?`,
            description: user.password_expires_at
                ? "Every session this account holds is signed out, and the password handed out with it stops working."
                : "Every session this account holds is signed out. An account created under Testers also loses the password its holder chose; any other keeps its password.",
            confirmLabel: "Remove exemption",
            destructive: true,
        });
        if (ok) await clear.mutateAsync().catch(() => undefined);
    }

    const trimmed = reason.trim();
    const busy = grant.isPending || clear.isPending;

    return (
        <div className="surface-lit rounded-xl border border-border bg-card px-4 py-3">
            <div className="flex items-start justify-between gap-4">
                <div className="min-w-0">
                    <label htmlFor="login-code-exempt" className="text-[13px] font-medium text-foreground">
                        Skip emailed sign-in code
                    </label>
                    <p className="mt-0.5 text-[12.5px] leading-relaxed text-muted-foreground">
                        For an App Store or OAuth review account whose holder cannot read this instance's mail. Only
                        the emailed code is skipped: the password, the captcha or App Attest check, the attempt limit
                        and two-factor authentication still apply.
                    </p>
                    {exempt && (
                        <p className="mt-1.5 text-[12.5px] text-foreground">
                            <span className="text-muted-foreground">Reason: </span>
                            {user.login_code_exempt_reason || "none recorded"}
                            {user.login_code_exempt_at && (
                                <span className="text-muted-foreground">
                                    {" "}
                                    (since {new Date(user.login_code_exempt_at).toLocaleDateString()})
                                </span>
                            )}
                        </p>
                    )}
                    {!canManage && (
                        <p className="mt-1.5 text-xs text-subtle-foreground">Changing this needs manage_testers.</p>
                    )}
                </div>
                <Switch
                    id="login-code-exempt"
                    checked={exempt}
                    disabled={!canManage || busy}
                    onCheckedChange={(v) => void onToggle(v)}
                    className="mt-0.5"
                />
            </div>

            <Dialog
                open={granting}
                onOpenChange={(open) => {
                    if (!open && !grant.isPending) {
                        setGranting(false);
                        setReason("");
                    }
                }}
            >
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Skip the emailed sign-in code</DialogTitle>
                        <DialogDescription>
                            <span className="font-mono">{user.email}</span> will sign in with its password alone, without
                            an emailed code, on any device. Every other check still applies. The reason is kept on the
                            account and in the admin audit log.
                        </DialogDescription>
                    </DialogHeader>
                    <form
                        className="space-y-1.5"
                        onSubmit={(e) => {
                            e.preventDefault();
                            if (trimmed !== "") grant.mutate(trimmed);
                        }}
                    >
                        <Label htmlFor="login-code-exempt-reason" className="text-xs font-medium text-muted-foreground">
                            Reason
                        </Label>
                        <Input
                            id="login-code-exempt-reason"
                            placeholder="e.g. App Store review account"
                            value={reason}
                            maxLength={REASON_MAX}
                            onChange={(e) => setReason(e.target.value)}
                            autoFocus
                        />
                        <p className="text-xs text-subtle-foreground">
                            Required. Remove the exemption when the review ends.
                        </p>
                    </form>
                    <DialogFooter>
                        <Button
                            variant="outline"
                            disabled={grant.isPending}
                            onClick={() => {
                                setGranting(false);
                                setReason("");
                            }}
                        >
                            Cancel
                        </Button>
                        <Button
                            disabled={trimmed === "" || grant.isPending}
                            title={trimmed === "" ? "A reason is required" : undefined}
                            onClick={() => grant.mutate(trimmed)}
                        >
                            {grant.isPending ? "Working…" : "Skip the code"}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
