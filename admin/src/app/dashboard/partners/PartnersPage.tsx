// Partners: resellers that create and run workspaces through /v1/partner. The partner API answers
// nothing until one exists here (or from `warmblyctl partner create`).

import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { Plus } from "lucide-react";
import { toast } from "sonner";
import { PageHeader } from "@/components/layout/PageHeader";
import { DataTable, type Column } from "@/components/data/DataTable";
import { StatusBadge } from "@/components/ui/kit";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import { createPartner, listPartners, type AdminPartner } from "@/lib/api/client/admin/partners";

const FIELD_LABEL = "text-xs font-medium text-muted-foreground";

const COLUMNS: Column<AdminPartner>[] = [
    { id: "name", header: "Partner", cell: (p) => <span className="font-medium">{p.name}</span>, csv: (p) => p.name },
    { id: "slug", header: "Slug", cell: (p) => <span className="font-mono text-xs text-muted-foreground">{p.slug}</span>, csv: (p) => p.slug },
    { id: "display", header: "Members see", cell: (p) => `Billed through ${p.display_name}`, csv: (p) => p.display_name },
    {
        id: "status",
        header: "Status",
        cell: (p) => <StatusBadge tone={p.status === "active" ? "success" : "warning"}>{p.status}</StatusBadge>,
        csv: (p) => p.status,
    },
    { id: "webhook", header: "Webhook", cell: (p) => (p.webhook_url && p.webhook_secret_set ? "Set" : "Not set"), csv: (p) => p.webhook_url ?? "" },
];

export default function PartnersPage() {
    const nav = useNavigate();
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);
    const [creating, setCreating] = useState(false);
    const q = useQuery({ queryKey: ["admin", "partners"], queryFn: listPartners });
    const rows = q.data?.data ?? [];

    return (
        <>
            <PageHeader
                title="Partners"
                description="Resellers that create, sign people in to and bill workspaces through the partner API."
                meta={rows.length ? rows.length.toLocaleString() : undefined}
            >
                {canManage && (
                    <Button size="sm" onClick={() => setCreating(true)}>
                        <Plus className="size-3.5" /> New partner
                    </Button>
                )}
            </PageHeader>
            <NewPartnerDialog open={creating && canManage} onOpenChange={setCreating} />
            <DataTable
                columns={COLUMNS}
                rows={rows}
                getRowId={(p) => p.id}
                loading={q.isLoading}
                error={q.error}
                onRetry={() => void q.refetch()}
                onRowClick={(p) => nav(`/partners/${p.id}`)}
                emptyTitle="No partners"
                emptyHint="The partner API is off on this instance until a partner exists. Add one only for a reseller you have an agreement with."
                csvName="partners"
                noun="partners"
            />
        </>
    );
}

function NewPartnerDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>New partner</DialogTitle>
                    <DialogDescription>
                        Adds a reseller with no keys yet. Issue a test key on the next page, then a live one when it is ready.
                    </DialogDescription>
                </DialogHeader>
                <NewPartnerForm onClose={() => onOpenChange(false)} />
            </DialogContent>
        </Dialog>
    );
}

function NewPartnerForm({ onClose }: { onClose: () => void }) {
    const nav = useNavigate();
    const qc = useQueryClient();
    const [slug, setSlug] = useState("");
    const [name, setName] = useState("");
    const [display, setDisplay] = useState("");
    const [hosts, setHosts] = useState("");
    const ready = /^[a-z0-9][a-z0-9-]{1,39}$/.test(slug.trim()) && !!name.trim();

    const create = useMutation({
        mutationFn: () =>
            createPartner({
                slug: slug.trim(),
                name: name.trim(),
                display_name: display.trim() || undefined,
                allowed_hosts: hosts.split(/[\s,]+/).filter(Boolean),
            }),
        onSuccess: (p) => {
            qc.setQueryData(["admin", "partners", p.id], p);
            void qc.invalidateQueries({ queryKey: ["admin", "partners"], exact: true });
            toast.success("Partner created");
            onClose();
            nav(`/partners/${p.id}`);
        },
    });

    function submit(e: FormEvent) {
        e.preventDefault();
        if (ready && !create.isPending) create.mutate();
    }

    return (
        <form onSubmit={submit} noValidate className="grid gap-4">
            <div className="space-y-1.5">
                <Label htmlFor="new-partner-slug" className={FIELD_LABEL}>
                    Slug
                </Label>
                <Input id="new-partner-slug" placeholder="graphkit" value={slug} onChange={(e) => setSlug(e.target.value.toLowerCase())} autoFocus />
                <p className="text-xs leading-relaxed text-muted-foreground">Lowercase letters, digits and dashes. It cannot be changed later.</p>
            </div>
            <div className="space-y-1.5">
                <Label htmlFor="new-partner-name" className={FIELD_LABEL}>
                    Name
                </Label>
                <Input id="new-partner-name" placeholder="GraphKit" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="space-y-1.5">
                <Label htmlFor="new-partner-display" className={FIELD_LABEL}>
                    Display name
                </Label>
                <Input id="new-partner-display" placeholder={name || "GraphKit"} value={display} onChange={(e) => setDisplay(e.target.value)} />
                <p className="text-xs leading-relaxed text-muted-foreground">Members of its workspaces read "Billed through {display || name || "…"}".</p>
            </div>
            <div className="space-y-1.5">
                <Label htmlFor="new-partner-hosts" className={FIELD_LABEL}>
                    Allowed hosts
                </Label>
                <Input id="new-partner-hosts" placeholder="app.graphkit.com" value={hosts} onChange={(e) => setHosts(e.target.value)} />
                <p className="text-xs leading-relaxed text-muted-foreground">Hosts a return URL the partner sends may point at, separated by commas.</p>
            </div>
            {create.error && (
                <p role="alert" className="text-[12.5px] leading-relaxed text-[var(--admin-danger)]">
                    {create.error.message || "Failed to create the partner."}
                </p>
            )}
            <DialogFooter>
                <Button type="button" variant="outline" onClick={onClose}>
                    Cancel
                </Button>
                <Button type="submit" disabled={!ready || create.isPending}>
                    {create.isPending ? "Creating…" : "Create partner"}
                </Button>
            </DialogFooter>
        </form>
    );
}
