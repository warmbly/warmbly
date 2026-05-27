// Settings → Cloud Providers.
//
// Operators paste a Hetzner Cloud API token here. The token is stored
// server-side (envelope-encrypted) and only ever returned in redacted
// form. Each saved credential gets a connection-test action so we can
// catch broken or revoked tokens before a real provisioning job needs
// them.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    Cloud,
    CheckCircle2,
    XCircle,
    Plus,
    Trash2,
    PlayCircle,
    KeyRound,
} from "lucide-react";

import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
} from "@/components/ui/card";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import {
    createCloudCredential,
    deleteCloudCredential,
    listCloudCredentials,
    testHetznerCredential,
} from "@/lib/api/client/admin/cloud";
import type {
    CloudCredential,
    CloudCredentialTestResult,
} from "@/lib/api/models/admin";

export default function CloudProvidersPage() {
    const qc = useQueryClient();
    const credsQ = useQuery({
        queryKey: ["admin", "cloud-credentials"],
        queryFn: listCloudCredentials,
        retry: false,
    });

    const invalidate = () =>
        qc.invalidateQueries({ queryKey: ["admin", "cloud-credentials"] });

    return (
        <div>
            <PageHeader
                title="Cloud Providers"
                description="API credentials used to provision worker machines. Tokens are stored server-side, encrypted, and surfaced here only in redacted form."
            >
                <AddCredentialDialog onSaved={invalidate} />
            </PageHeader>

            {credsQ.isLoading && (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                    <Skeleton className="h-44 w-full" />
                    <Skeleton className="h-44 w-full" />
                </div>
            )}

            {!credsQ.isLoading && credsQ.data && credsQ.data.length === 0 && (
                <Card>
                    <CardContent className="py-12 text-center">
                        <Cloud className="size-8 text-muted-foreground mx-auto mb-3" />
                        <div className="text-sm font-medium">
                            No cloud providers connected
                        </div>
                        <p className="text-xs text-muted-foreground mt-1 max-w-md mx-auto">
                            Connect a Hetzner Cloud project to enable provisioning of new
                            worker machines. The backend endpoints
                            <code className="px-1">/admin/cloud-credentials</code>
                            need to be wired before this works end-to-end.
                        </p>
                        <div className="mt-4 flex justify-center">
                            <AddCredentialDialog onSaved={invalidate} />
                        </div>
                    </CardContent>
                </Card>
            )}

            {credsQ.data && credsQ.data.length > 0 && (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                    {credsQ.data.map((c) => (
                        <CredentialCard key={c.id} cred={c} onChanged={invalidate} />
                    ))}
                </div>
            )}

            <div className="mt-6 text-xs text-muted-foreground border-l-2 border-[var(--admin-accent)] pl-3">
                Provisioning never reads these tokens directly. The control plane signs
                a short-lived request envelope per provisioning job, so a revoked token
                stops new jobs without affecting workers already running.
            </div>
        </div>
    );
}

// --------------------------------------------------------------------
// Add credential dialog
// --------------------------------------------------------------------

function AddCredentialDialog({ onSaved }: { onSaved: () => void }) {
    const [open, setOpen] = useState(false);
    const [name, setName] = useState("");
    const [token, setToken] = useState("");
    const [provider, setProvider] = useState<"hetzner">("hetzner");

    const mut = useMutation({
        mutationFn: () =>
            createCloudCredential({ provider, name: name.trim(), token: token.trim() }),
        onSuccess: () => {
            toast.success("Credential saved");
            setName("");
            setToken("");
            setOpen(false);
            onSaved();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const canSubmit = name.trim().length > 0 && token.trim().length > 0;

    return (
        <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
                <Button
                    size="sm"
                    className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                >
                    <Plus className="size-4" />
                    Add cloud provider
                </Button>
            </DialogTrigger>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>Connect a cloud provider</DialogTitle>
                    <DialogDescription>
                        Paste a Hetzner Cloud API token from your Hetzner project
                        (Security → API Tokens). The token needs read+write scope to create
                        servers, IPs, and firewalls.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-3">
                    <div className="space-y-1.5">
                        <Label htmlFor="provider">Provider</Label>
                        <Select
                            value={provider}
                            onValueChange={(v) => setProvider(v as "hetzner")}
                        >
                            <SelectTrigger id="provider">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="hetzner">Hetzner Cloud</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="cred-name">Label</Label>
                        <Input
                            id="cred-name"
                            placeholder="e.g. fleet-prod"
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                            autoComplete="off"
                        />
                        <p className="text-[11px] text-muted-foreground">
                            Free-form label; shown in the templates dropdown.
                        </p>
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="cred-token">API token</Label>
                        <Input
                            id="cred-token"
                            type="password"
                            placeholder="hcloud_xxxxxxxxxxxxxxxx"
                            value={token}
                            onChange={(e) => setToken(e.target.value)}
                            autoComplete="off"
                        />
                        <p className="text-[11px] text-muted-foreground">
                            Stored server-side, envelope-encrypted with the platform KMS.
                        </p>
                    </div>
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={() => setOpen(false)}>
                        Cancel
                    </Button>
                    <Button
                        onClick={() => mut.mutate()}
                        disabled={!canSubmit || mut.isPending}
                        className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                    >
                        <KeyRound className="size-4" />
                        {mut.isPending ? "Saving..." : "Save credential"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

// --------------------------------------------------------------------
// Credential card
// --------------------------------------------------------------------

function CredentialCard({
    cred,
    onChanged,
}: {
    cred: CloudCredential;
    onChanged: () => void;
}) {
    const [testResult, setTestResult] = useState<CloudCredentialTestResult | null>(
        cred.last_test_ok != null
            ? {
                ok: cred.last_test_ok,
                error: cred.last_test_error,
            }
            : null,
    );
    const [confirmDelete, setConfirmDelete] = useState(false);

    const testMut = useMutation({
        mutationFn: () => testHetznerCredential(cred.id),
        onSuccess: (r) => {
            setTestResult(r);
            if (r.ok) toast.success("Connection verified");
            else toast.error(r.error || "Connection failed");
            onChanged();
        },
        onError: (e: Error) => {
            setTestResult({ ok: false, error: e.message });
            toast.error(e.message);
        },
    });

    const delMut = useMutation({
        mutationFn: () => deleteCloudCredential(cred.id),
        onSuccess: () => {
            toast.success("Credential removed");
            onChanged();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const statusTone = testResult
        ? testResult.ok
            ? "border-emerald-200 bg-emerald-50/60"
            : "border-red-200 bg-red-50/60"
        : "border-border bg-background";

    return (
        <Card className={statusTone}>
            <CardHeader>
                <CardTitle className="flex items-center gap-2">
                    <Cloud className="size-4 text-[var(--admin-accent)]" />
                    <span className="truncate">{cred.name}</span>
                    <Badge variant="outline" className="text-[10px]">
                        {cred.provider}
                    </Badge>
                </CardTitle>
                <CardDescription className="font-mono text-[11px]">
                    {cred.token_redacted}
                </CardDescription>
            </CardHeader>
            <CardContent className="pt-0 space-y-3">
                <div className="grid grid-cols-2 gap-2 text-[11px] text-muted-foreground">
                    <KV
                        label="Added"
                        value={new Date(cred.created_at).toLocaleString()}
                    />
                    <KV
                        label="Last used"
                        value={
                            cred.last_used_at
                                ? new Date(cred.last_used_at).toLocaleString()
                                : "never"
                        }
                    />
                </div>

                {testResult && (
                    <div
                        className={`rounded-md p-2.5 text-xs flex items-start gap-2 ${
                            testResult.ok
                                ? "bg-emerald-100/60 text-emerald-800"
                                : "bg-red-100/60 text-red-800"
                        }`}
                    >
                        {testResult.ok ? (
                            <CheckCircle2 className="size-3.5 mt-0.5 shrink-0" />
                        ) : (
                            <XCircle className="size-3.5 mt-0.5 shrink-0" />
                        )}
                        <div className="min-w-0">
                            <div className="font-medium">
                                {testResult.ok ? "Connection healthy" : "Connection failed"}
                            </div>
                            {testResult.ok && testResult.account_email && (
                                <div className="text-[11px] mt-0.5">
                                    Account: <span className="font-mono">{testResult.account_email}</span>
                                    {testResult.quota_servers != null && (
                                        <>
                                            {" "}- {testResult.used_servers ?? 0}/{testResult.quota_servers} servers
                                        </>
                                    )}
                                </div>
                            )}
                            {!testResult.ok && testResult.error && (
                                <div className="text-[11px] mt-0.5 font-mono break-words">
                                    {testResult.error}
                                </div>
                            )}
                        </div>
                    </div>
                )}

                <div className="flex items-center gap-2 pt-1">
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => testMut.mutate()}
                        disabled={testMut.isPending}
                    >
                        <PlayCircle className="size-4" />
                        {testMut.isPending ? "Testing..." : "Test connection"}
                    </Button>
                    <Button
                        size="sm"
                        variant={confirmDelete ? "destructive" : "ghost"}
                        onClick={() =>
                            confirmDelete ? delMut.mutate() : setConfirmDelete(true)
                        }
                        disabled={delMut.isPending}
                    >
                        <Trash2 className="size-4" />
                        {confirmDelete ? "Confirm remove" : "Remove"}
                    </Button>
                    {confirmDelete && (
                        <button
                            onClick={() => setConfirmDelete(false)}
                            className="text-[11px] underline text-muted-foreground"
                        >
                            cancel
                        </button>
                    )}
                </div>
            </CardContent>
        </Card>
    );
}

function KV({ label, value }: { label: string; value: string }) {
    return (
        <div>
            <div className="uppercase tracking-wide text-[9px]">{label}</div>
            <div className="text-[11px] text-foreground">{value}</div>
        </div>
    );
}
