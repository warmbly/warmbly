// Opens an agency for an existing account. It starts with no plan; the
// operator gives it one from the agency's page.

import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { createAgency } from "@/lib/api/client/admin/agencies";

const FIELD_LABEL = "text-xs font-medium text-muted-foreground";

export function NewAgencyDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>New agency</DialogTitle>
                    <DialogDescription>
                        Opens an agency owned by an account that already exists. It starts without a plan, so give it one on the
                        next page.
                    </DialogDescription>
                </DialogHeader>
                <NewAgencyForm onClose={() => onOpenChange(false)} />
            </DialogContent>
        </Dialog>
    );
}

function NewAgencyForm({ onClose }: { onClose: () => void }) {
    const nav = useNavigate();
    const qc = useQueryClient();
    const [email, setEmail] = useState("");
    const [name, setName] = useState("");
    const ready = !!email.trim() && !!name.trim();

    const create = useMutation({
        mutationFn: () => createAgency({ owner_email: email.trim(), name: name.trim() }),
        onSuccess: (detail) => {
            qc.setQueryData(["admin", "agencies", detail.agency.id], detail);
            void qc.invalidateQueries({ queryKey: ["admin", "agencies"], predicate: (q) => q.queryKey[2] !== detail.agency.id });
            toast.success("Agency created");
            onClose();
            nav(`/agencies/${detail.agency.id}`);
        },
    });

    function submit(e: FormEvent) {
        e.preventDefault();
        if (ready && !create.isPending) create.mutate();
    }

    return (
        <form onSubmit={submit} noValidate className="grid gap-4">
            <div className="space-y-1.5">
                <Label htmlFor="new-agency-email" className={FIELD_LABEL}>
                    Owner's email
                </Label>
                <Input
                    id="new-agency-email"
                    type="email"
                    placeholder="owner@example.com"
                    value={email}
                    onChange={(e) => {
                        setEmail(e.target.value);
                        create.reset();
                    }}
                    autoComplete="off"
                    autoFocus
                />
                <p className="text-xs leading-relaxed text-muted-foreground">The address of an account on this instance.</p>
            </div>
            <div className="space-y-1.5">
                <Label htmlFor="new-agency-name" className={FIELD_LABEL}>
                    Agency name
                </Label>
                <Input
                    id="new-agency-name"
                    placeholder="Northwind Outreach"
                    value={name}
                    onChange={(e) => {
                        setName(e.target.value);
                        create.reset();
                    }}
                />
            </div>
            {create.error && (
                <p role="alert" className="text-[12.5px] leading-relaxed text-[var(--admin-danger)]">
                    {create.error.message || "Failed to create the agency."}
                </p>
            )}
            <DialogFooter>
                <Button type="button" variant="outline" onClick={onClose}>
                    Cancel
                </Button>
                <Button type="submit" disabled={!ready || create.isPending}>
                    {create.isPending ? "Creating…" : "Create agency"}
                </Button>
            </DialogFooter>
        </form>
    );
}
