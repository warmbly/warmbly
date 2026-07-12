// Settings → AWS Credentials.
//
// Operators register AWS access key pairs here. Worker infrastructure uses
// them for KMS envelope encryption and S3 storage. The secret access key is
// stored encrypted server-side and never shown again; the list only exposes
// a "secret set" indicator, and editing with a blank secret keeps the stored
// one while a filled secret rotates it.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    KeyRound,
    Pencil,
    Plus,
    ShieldCheck,
    ShieldAlert,
    Trash2,
} from "lucide-react";

import { PageHeader } from "@/components/layout/PageHeader";
import { ErrorState } from "@/components/ErrorState";
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
import { Skeleton } from "@/components/ui/skeleton";
import {
    createAwsCredential,
    deleteAwsCredential,
    listAwsCredentials,
    updateAwsCredential,
    type AWSCredentials,
    type AWSCredentialsInput,
} from "@/lib/api/client/admin/awsCredentials";

export default function AwsCredentialsPage() {
    const qc = useQueryClient();
    const credsQ = useQuery({
        queryKey: ["admin", "aws-credentials"],
        queryFn: listAwsCredentials,
        retry: false,
    });

    const invalidate = () =>
        qc.invalidateQueries({ queryKey: ["admin", "aws-credentials"] });

    return (
        <div>
            <PageHeader
                title="AWS Credentials"
                description="Access key pairs used by worker infrastructure for KMS envelope encryption and S3 storage. Secrets are stored encrypted and never shown again."
            >
                <CredentialDialog onSaved={invalidate}>
                    <Button
                        size="sm"
                        className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                    >
                        <Plus className="size-4" />
                        Add credential
                    </Button>
                </CredentialDialog>
            </PageHeader>

            {credsQ.isLoading && (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                    <Skeleton className="h-44 w-full" />
                    <Skeleton className="h-44 w-full" />
                </div>
            )}

            {credsQ.isError && (
                <ErrorState
                    error={credsQ.error}
                    title="Could not load AWS credentials"
                    onRetry={() => credsQ.refetch()}
                />
            )}

            {!credsQ.isLoading && credsQ.data && credsQ.data.length === 0 && (
                <Card>
                    <CardContent className="py-12 text-center">
                        <KeyRound className="size-8 text-muted-foreground mx-auto mb-3" />
                        <div className="text-sm font-medium">
                            No AWS credentials yet
                        </div>
                        <p className="text-xs text-muted-foreground mt-1 max-w-md mx-auto">
                            Add an AWS access key pair (from IAM → Users → Security
                            credentials) so workers can reach KMS for envelope encryption
                            and S3 for storage.
                        </p>
                        <div className="mt-4 flex justify-center">
                            <CredentialDialog onSaved={invalidate}>
                                <Button
                                    size="sm"
                                    className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                                >
                                    <Plus className="size-4" />
                                    Add credential
                                </Button>
                            </CredentialDialog>
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
                The secret access key is sealed server-side on save and can only be
                replaced, never read back. To rotate a key, edit the credential and
                fill in the new secret.
            </div>
        </div>
    );
}

// --------------------------------------------------------------------
// Create / edit dialog
// --------------------------------------------------------------------

function CredentialDialog({
    initial,
    onSaved,
    children,
}: {
    initial?: AWSCredentials;
    onSaved: () => void;
    children: React.ReactNode;
}) {
    const isEdit = initial != null;
    const [open, setOpen] = useState(false);
    const [form, setForm] = useState<AWSCredentialsInput>(() => emptyForm(initial));

    const set = (patch: Partial<AWSCredentialsInput>) =>
        setForm((f) => ({ ...f, ...patch }));

    const mut = useMutation({
        mutationFn: async () => {
            const body: AWSCredentialsInput = {
                name: form.name.trim(),
                description: form.description.trim(),
                region: form.region.trim(),
                access_key_id: form.access_key_id.trim(),
                secret_access_key: form.secret_access_key.trim(),
            };
            if (isEdit) await updateAwsCredential(initial.id, body);
            else await createAwsCredential(body);
        },
        onSuccess: () => {
            toast.success(isEdit ? "Credential updated" : "Credential saved");
            setOpen(false);
            onSaved();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const canSubmit =
        form.name.trim().length > 0 &&
        form.region.trim().length > 0 &&
        form.access_key_id.trim().length > 0 &&
        (isEdit || form.secret_access_key.trim().length > 0);

    return (
        <Dialog
            open={open}
            onOpenChange={(o) => {
                setOpen(o);
                if (o) setForm(emptyForm(initial));
            }}
        >
            <DialogTrigger asChild>{children}</DialogTrigger>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>
                        {isEdit ? "Edit AWS credential" : "Add an AWS credential"}
                    </DialogTitle>
                    <DialogDescription>
                        {isEdit
                            ? "Update the key pair details. Leave the secret blank to keep the stored one; fill it to rotate the secret."
                            : "Paste an AWS access key pair. The IAM user needs access to KMS (envelope encryption) and S3 (storage)."}
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-3">
                    <div className="space-y-1.5">
                        <Label htmlFor="aws-name">Name</Label>
                        <Input
                            id="aws-name"
                            placeholder="e.g. workers-prod"
                            value={form.name}
                            onChange={(e) => set({ name: e.target.value })}
                            autoComplete="off"
                        />
                        <p className="text-[11px] text-muted-foreground">
                            Free-form label; shown wherever a credential is picked.
                        </p>
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="aws-description">Description</Label>
                        <Input
                            id="aws-description"
                            placeholder="Optional note, e.g. which account or team owns it"
                            value={form.description}
                            onChange={(e) => set({ description: e.target.value })}
                            autoComplete="off"
                        />
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="aws-region">Region</Label>
                        <Input
                            id="aws-region"
                            placeholder="e.g. eu-central-1"
                            value={form.region}
                            onChange={(e) => set({ region: e.target.value })}
                            autoComplete="off"
                        />
                        <p className="text-[11px] text-muted-foreground">
                            The AWS region where the KMS keys and S3 buckets live.
                        </p>
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="aws-access-key">Access key ID</Label>
                        <Input
                            id="aws-access-key"
                            placeholder="the key ID, starts with AKIA"
                            className="font-mono"
                            value={form.access_key_id}
                            onChange={(e) => set({ access_key_id: e.target.value })}
                            autoComplete="off"
                        />
                    </div>

                    <div className="space-y-1.5">
                        <Label htmlFor="aws-secret-key">Secret access key</Label>
                        <Input
                            id="aws-secret-key"
                            type="password"
                            placeholder={
                                isEdit
                                    ? "leave blank to keep current secret, fill to rotate"
                                    : "the secret access key"
                            }
                            value={form.secret_access_key}
                            onChange={(e) => set({ secret_access_key: e.target.value })}
                            autoComplete="off"
                        />
                        <p className="text-[11px] text-muted-foreground">
                            Stored encrypted server-side and never shown again.
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
                        {mut.isPending
                            ? "Saving..."
                            : isEdit
                              ? "Save changes"
                              : "Save credential"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function emptyForm(initial?: AWSCredentials): AWSCredentialsInput {
    return {
        name: initial?.name ?? "",
        description: initial?.description ?? "",
        region: initial?.region ?? "",
        access_key_id: initial?.access_key_id ?? "",
        secret_access_key: "",
    };
}

// --------------------------------------------------------------------
// Credential card
// --------------------------------------------------------------------

function CredentialCard({
    cred,
    onChanged,
}: {
    cred: AWSCredentials;
    onChanged: () => void;
}) {
    return (
        <Card>
            <CardHeader>
                <CardTitle className="flex items-center gap-2">
                    <KeyRound className="size-4 text-[var(--admin-accent)]" />
                    <span className="truncate">{cred.name}</span>
                    <Badge variant="outline" className="text-[10px]">
                        {cred.region}
                    </Badge>
                </CardTitle>
                {cred.description && (
                    <CardDescription>{cred.description}</CardDescription>
                )}
            </CardHeader>
            <CardContent className="pt-0 space-y-3">
                <div className="grid grid-cols-2 gap-2 text-[11px] text-muted-foreground">
                    <KV label="Access key ID" value={cred.access_key_id} mono />
                    <div>
                        <div className="uppercase tracking-wide text-[9px]">Secret</div>
                        {cred.has_secret ? (
                            <div className="flex items-center gap-1.5 text-[11px] text-foreground">
                                <ShieldCheck className="size-3.5 text-emerald-600" />
                                <span className="font-mono">••••••••••••</span>
                                <span className="text-muted-foreground">secret set</span>
                            </div>
                        ) : (
                            <div className="flex items-center gap-1.5 text-[11px] text-amber-700">
                                <ShieldAlert className="size-3.5" />
                                no secret stored
                            </div>
                        )}
                    </div>
                    <KV
                        label="Added"
                        value={new Date(cred.created_at).toLocaleString()}
                    />
                    <KV
                        label="Updated"
                        value={new Date(cred.updated_at).toLocaleString()}
                    />
                </div>

                <div className="flex items-center gap-2 pt-1">
                    <CredentialDialog initial={cred} onSaved={onChanged}>
                        <Button size="sm" variant="outline">
                            <Pencil className="size-4" />
                            Edit
                        </Button>
                    </CredentialDialog>
                    <DeleteCredentialDialog cred={cred} onDeleted={onChanged} />
                </div>
            </CardContent>
        </Card>
    );
}

function DeleteCredentialDialog({
    cred,
    onDeleted,
}: {
    cred: AWSCredentials;
    onDeleted: () => void;
}) {
    const [open, setOpen] = useState(false);

    const delMut = useMutation({
        mutationFn: () => deleteAwsCredential(cred.id),
        onSuccess: () => {
            toast.success("Credential removed");
            setOpen(false);
            onDeleted();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    return (
        <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
                <Button size="sm" variant="ghost">
                    <Trash2 className="size-4" />
                    Delete
                </Button>
            </DialogTrigger>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>Delete this credential?</DialogTitle>
                    <DialogDescription>
                        This removes "{cred.name}" permanently. Any worker profiles that
                        reference it will break until they are pointed at another
                        credential. The stored secret cannot be recovered.
                    </DialogDescription>
                </DialogHeader>
                <DialogFooter>
                    <Button variant="outline" onClick={() => setOpen(false)}>
                        Cancel
                    </Button>
                    <Button
                        variant="destructive"
                        onClick={() => delMut.mutate()}
                        disabled={delMut.isPending}
                    >
                        <Trash2 className="size-4" />
                        {delMut.isPending ? "Deleting..." : "Delete credential"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function KV({
    label,
    value,
    mono,
}: {
    label: string;
    value: string;
    mono?: boolean;
}) {
    return (
        <div>
            <div className="uppercase tracking-wide text-[9px]">{label}</div>
            <div
                className={`text-[11px] text-foreground break-all ${mono ? "font-mono" : ""}`}
            >
                {value}
            </div>
        </div>
    );
}
