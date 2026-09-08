// Start an archive build for a workspace. With `org` fixed (organization
// detail page) the picker is hidden; without it the operator searches first.
// Secrets only travel under a passphrase, which is used once and never stored.

import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import {
    createOrgExport,
    expandGroups,
    MIN_EXPORT_PASSPHRASE,
    ORG_DATA_GROUP_CATALOG,
    type OrgDataGroup,
} from "@/lib/api/client/admin/transfers";
import { OrgPicker, type PickedOrg } from "../fleet/OrgPicker";
import { GroupPicker } from "./GroupPicker";

function defaultGroups(): Set<OrgDataGroup> {
    return expandGroups(ORG_DATA_GROUP_CATALOG.filter((g) => !g.heavy).map((g) => g.key));
}

export function ExportDialog({
    open,
    onOpenChange,
    org,
    onStarted,
}: {
    open: boolean;
    onOpenChange: (v: boolean) => void;
    org?: { id: string; name: string } | null;
    onStarted?: () => void;
}) {
    const qc = useQueryClient();
    const [picked, setPicked] = useState<PickedOrg | null>(null);
    const [groups, setGroups] = useState<Set<OrgDataGroup>>(defaultGroups);
    const [secrets, setSecrets] = useState(false);
    const [passphrase, setPassphrase] = useState("");
    const [confirmPass, setConfirmPass] = useState("");

    useEffect(() => {
        if (!open) return;
        setPicked(null);
        setGroups(defaultGroups());
        setSecrets(false);
        setPassphrase("");
        setConfirmPass("");
    }, [open]);

    const target = org ?? (picked ? { id: picked.id, name: picked.name } : null);
    const passOk = !secrets || (passphrase.length >= MIN_EXPORT_PASSPHRASE && passphrase === confirmPass);
    const heavyOn = ORG_DATA_GROUP_CATALOG.filter((g) => g.heavy && groups.has(g.key));

    const mutation = useMutation({
        mutationFn: () =>
            createOrgExport(target!.id, {
                groups: Array.from(groups),
                include_secrets: secrets,
                passphrase: secrets ? passphrase : undefined,
            }),
        onSuccess: () => {
            toast.success(`Export started for ${target?.name}`);
            qc.invalidateQueries({ queryKey: ["admin", "transfers"] });
            onStarted?.();
            onOpenChange(false);
        },
        onError: (e: Error) => toast.error(e.message || "Could not start the export"),
    });

    return (
        <Dialog
            open={open}
            onOpenChange={(v) => {
                if (!v && mutation.isPending) return;
                onOpenChange(v);
            }}
        >
            <DialogContent
                className="sm:max-w-2xl"
                onEscapeKeyDown={(e) => {
                    if (document.querySelector("[data-floating]")) e.preventDefault();
                }}
            >
                <DialogHeader>
                    <DialogTitle>Export a workspace</DialogTitle>
                    <DialogDescription>
                        Builds the same archive an owner gets from Settings &gt; Data. It stays downloadable for 7 days.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4">
                    {!org && (
                        <div className="space-y-1.5">
                            <Label className="text-xs">Workspace</Label>
                            <OrgPicker value={picked} onChange={setPicked} autoFocus />
                        </div>
                    )}

                    <div className="space-y-1.5">
                        <Label className="text-xs">Data groups</Label>
                        <GroupPicker selected={groups} onChange={setGroups} />
                        {heavyOn.length > 0 && (
                            <p className="text-[11px] text-amber-700">
                                {heavyOn.map((g) => g.label).join(", ")} can multiply the archive size on a busy workspace.
                            </p>
                        )}
                    </div>

                    <div className="rounded-md border border-border bg-card p-3">
                        <label className="flex items-start gap-3">
                            <Switch checked={secrets} onCheckedChange={setSecrets} className="mt-0.5" />
                            <span>
                                <span className="block text-[12.5px] font-medium">Include credentials</span>
                                <span className="block text-[11px] text-muted-foreground">
                                    Re-seals mailbox and integration credentials into the archive under a passphrase, so
                                    the destination brings mailboxes back up without every user reconnecting.
                                </span>
                            </span>
                        </label>
                        {secrets && (
                            <div className="mt-3 grid gap-2 sm:grid-cols-2">
                                <div className="space-y-1">
                                    <Label htmlFor="exp-pass" className="text-xs">
                                        Passphrase
                                    </Label>
                                    <Input
                                        id="exp-pass"
                                        type="password"
                                        autoComplete="new-password"
                                        value={passphrase}
                                        onChange={(e) => setPassphrase(e.target.value)}
                                        className="h-8 text-[12.5px]"
                                    />
                                </div>
                                <div className="space-y-1">
                                    <Label htmlFor="exp-pass2" className="text-xs">
                                        Repeat passphrase
                                    </Label>
                                    <Input
                                        id="exp-pass2"
                                        type="password"
                                        autoComplete="new-password"
                                        value={confirmPass}
                                        onChange={(e) => setConfirmPass(e.target.value)}
                                        className="h-8 text-[12.5px]"
                                    />
                                </div>
                                <p className="text-[11px] text-muted-foreground sm:col-span-2">
                                    At least {MIN_EXPORT_PASSPHRASE} characters. It is never stored: whoever imports the
                                    archive needs it, and there is no recovery.
                                    {passphrase && passphrase.length < MIN_EXPORT_PASSPHRASE && (
                                        <span className="ml-1 text-red-600">Too short.</span>
                                    )}
                                    {passphrase.length >= MIN_EXPORT_PASSPHRASE && confirmPass && passphrase !== confirmPass && (
                                        <span className="ml-1 text-red-600">Passphrases differ.</span>
                                    )}
                                </p>
                            </div>
                        )}
                    </div>
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>
                        Cancel
                    </Button>
                    <Button onClick={() => mutation.mutate()} disabled={!target || !passOk || mutation.isPending}>
                        {mutation.isPending ? "Starting…" : "Start export"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
