// Settings → Provisioning Templates. Full CRUD over reusable
// Hetzner provisioning configs. The same form is reused inside the
// Workers → Provision new modal.

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    ArrowLeft,
    FilePlus2,
    Pencil,
    Server,
    Trash2,
    Star,
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
import { Skeleton } from "@/components/ui/skeleton";

import {
    createProvisioningTemplate,
    deleteProvisioningTemplate,
    listProvisioningTemplates,
    updateProvisioningTemplate,
} from "@/lib/api/client/admin/provisioning";
import type { ProvisioningTemplate } from "@/lib/api/models/admin";

import {
    ProvisioningTemplateForm,
    makeEmptyFormValue,
    templateToFormValue,
    type TemplateFormValue,
} from "./ProvisioningTemplateForm";

type Mode =
    | { kind: "list" }
    | { kind: "create" }
    | { kind: "edit"; id: string };

export default function ProvisioningTemplatesPage() {
    const [mode, setMode] = useState<Mode>({ kind: "list" });

    const qc = useQueryClient();
    const listQ = useQuery({
        queryKey: ["admin", "provisioning-templates"],
        queryFn: listProvisioningTemplates,
        retry: false,
    });

    const invalidate = () =>
        qc.invalidateQueries({ queryKey: ["admin", "provisioning-templates"] });

    if (mode.kind === "create") {
        return (
            <TemplateEditor
                editing={null}
                onCancel={() => setMode({ kind: "list" })}
                onSaved={() => {
                    invalidate();
                    setMode({ kind: "list" });
                }}
            />
        );
    }

    if (mode.kind === "edit") {
        const editing = (listQ.data ?? []).find((t) => t.id === mode.id) ?? null;
        return (
            <TemplateEditor
                editing={editing}
                onCancel={() => setMode({ kind: "list" })}
                onSaved={() => {
                    invalidate();
                    setMode({ kind: "list" });
                }}
            />
        );
    }

    return (
        <div>
            <PageHeader
                title="Provisioning Templates"
                description="Reusable Hetzner provisioning configs. Pick one when launching a new worker box or set one as the auto-provision default for a tier."
            >
                <Button
                    size="sm"
                    className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                    onClick={() => setMode({ kind: "create" })}
                >
                    <FilePlus2 className="size-4" />
                    New template
                </Button>
            </PageHeader>

            {listQ.isLoading && (
                <div className="space-y-2">
                    <Skeleton className="h-24 w-full" />
                    <Skeleton className="h-24 w-full" />
                </div>
            )}

            {!listQ.isLoading && (listQ.data ?? []).length === 0 && (
                <Card>
                    <CardContent className="py-12 text-center">
                        <Server className="size-8 text-muted-foreground mx-auto mb-3" />
                        <div className="text-sm font-medium">No templates yet</div>
                        <p className="text-xs text-muted-foreground mt-1 max-w-md mx-auto">
                            Templates let operators provision a new worker box with one
                            click instead of refilling the form each time. The backend
                            endpoint <code>/admin/provisioning-templates</code> needs to be
                            wired before this works end-to-end.
                        </p>
                        <div className="mt-4 flex justify-center">
                            <Button
                                size="sm"
                                className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                                onClick={() => setMode({ kind: "create" })}
                            >
                                <FilePlus2 className="size-4" />
                                New template
                            </Button>
                        </div>
                    </CardContent>
                </Card>
            )}

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                {(listQ.data ?? []).map((t) => (
                    <TemplateCard
                        key={t.id}
                        tpl={t}
                        onEdit={() => setMode({ kind: "edit", id: t.id })}
                        onChanged={invalidate}
                    />
                ))}
            </div>
        </div>
    );
}

// --------------------------------------------------------------------
// Template card (list view)
// --------------------------------------------------------------------

function TemplateCard({
    tpl,
    onEdit,
    onChanged,
}: {
    tpl: ProvisioningTemplate;
    onEdit: () => void;
    onChanged: () => void;
}) {
    const [confirm, setConfirm] = useState(false);
    const delMut = useMutation({
        mutationFn: () => deleteProvisioningTemplate(tpl.id),
        onSuccess: () => {
            toast.success("Template removed");
            onChanged();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    return (
        <Card>
            <CardHeader>
                <CardTitle className="flex items-center gap-2">
                    <Server className="size-4 text-[var(--admin-accent)]" />
                    <span className="truncate">{tpl.name}</span>
                    {tpl.is_draft && (
                        <Badge variant="outline" className="text-[10px]">
                            draft
                        </Badge>
                    )}
                    {tpl.auto_provision_tier && (
                        <Badge className="bg-[var(--admin-accent)] text-white text-[10px]">
                            <Star className="size-3" />
                            auto · {tpl.auto_provision_tier}
                        </Badge>
                    )}
                </CardTitle>
                {tpl.description && (
                    <CardDescription>{tpl.description}</CardDescription>
                )}
            </CardHeader>
            <CardContent className="pt-0 space-y-3">
                <div className="grid grid-cols-2 gap-2 text-[11px]">
                    <KV label="Provider" value={tpl.config.provider} mono />
                    <KV label="Location" value={tpl.config.location} mono />
                    <KV label="Server type" value={tpl.config.server_type} mono />
                    <KV
                        label="Servers"
                        value={`${tpl.config.server_count}x`}
                    />
                    <KV
                        label="IPv4 / server"
                        value={String(tpl.config.ipv4_per_server)}
                    />
                    <KV label="Tier" value={tpl.config.worker_tier} />
                    <KV label="Egress" value={tpl.config.egress_kind} />
                    <KV label="Image" value={tpl.config.image} mono />
                </div>

                <div className="flex items-center gap-2 pt-1">
                    <Button size="sm" variant="outline" onClick={onEdit}>
                        <Pencil className="size-4" />
                        Edit
                    </Button>
                    <Button
                        size="sm"
                        variant={confirm ? "destructive" : "ghost"}
                        onClick={() => (confirm ? delMut.mutate() : setConfirm(true))}
                        disabled={delMut.isPending}
                    >
                        <Trash2 className="size-4" />
                        {confirm ? "Confirm remove" : "Remove"}
                    </Button>
                    {confirm && (
                        <button
                            onClick={() => setConfirm(false)}
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

// --------------------------------------------------------------------
// Editor
// --------------------------------------------------------------------

function TemplateEditor({
    editing,
    onCancel,
    onSaved,
}: {
    editing: ProvisioningTemplate | null;
    onCancel: () => void;
    onSaved: () => void;
}) {
    const initial = useMemo(
        () => (editing ? templateToFormValue(editing) : makeEmptyFormValue()),
        [editing],
    );
    const [value, setValue] = useState<TemplateFormValue>(initial);

    const saveMut = useMutation({
        mutationFn: (asDraft: boolean) => {
            const body = {
                name: value.name.trim(),
                description: value.description.trim() || undefined,
                config: value.config,
                auto_provision_tier:
                    value.auto_provision_tier === ""
                        ? undefined
                        : value.auto_provision_tier,
                is_draft: asDraft,
            };
            return editing
                ? updateProvisioningTemplate(editing.id, body)
                : createProvisioningTemplate(body);
        },
        onSuccess: () => {
            toast.success(editing ? "Template updated" : "Template created");
            onSaved();
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const canSave = value.name.trim().length > 0 && !saveMut.isPending;

    return (
        <div>
            <button
                onClick={onCancel}
                className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-2"
            >
                <ArrowLeft className="size-3" />
                Back to templates
            </button>
            <PageHeader
                title={editing ? `Edit: ${editing.name}` : "New template"}
                description="Every field below mirrors the Hetzner Cloud API. Sensible defaults are pre-filled; tweak the ones that matter."
            >
                <Button variant="outline" onClick={onCancel}>
                    Cancel
                </Button>
                <Button
                    variant="outline"
                    onClick={() => saveMut.mutate(true)}
                    disabled={!canSave}
                >
                    Save as draft
                </Button>
                <Button
                    onClick={() => saveMut.mutate(false)}
                    disabled={!canSave}
                    className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                >
                    {saveMut.isPending ? "Saving..." : "Save and activate"}
                </Button>
            </PageHeader>

            <Card>
                <CardContent className="py-6">
                    <ProvisioningTemplateForm
                        editing={editing}
                        mode="template"
                        value={value}
                        onChange={setValue}
                    />
                </CardContent>
            </Card>
        </div>
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
            <div className="uppercase tracking-wide text-[9px] text-muted-foreground">
                {label}
            </div>
            <div className={`text-[12px] ${mono ? "font-mono" : ""}`}>{value}</div>
        </div>
    );
}
