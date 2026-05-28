// Override editor for a single org. Each numeric field accepts:
//   - blank        → leave the field alone on submit (partial PUT)
//   - 0            → remove that column's override (revert to plan/hard cap)
//   - any positive → explicit cap that overrides the plan default
//
// We render plan default + current override + the effective number so
// the admin can see what's being enforced before changing anything.

import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { updateOrganizationOverrides } from "@/lib/api/client/admin/organizations";
import type {
    AdminOrgDetail,
    OrganizationLimitOverrides,
    UpdateOrgOverridesRequest,
} from "@/lib/api/models/admin";

type FieldKey =
    | "max_email_accounts"
    | "max_campaigns"
    | "max_active_campaigns"
    | "max_team_members"
    | "max_contacts"
    | "daily_campaign_limit";

const FIELDS: { key: FieldKey; label: string; hint: string }[] = [
    { key: "max_email_accounts", label: "Mailboxes", hint: "Total connected mailboxes" },
    { key: "max_campaigns", label: "Campaigns (lifetime)", hint: "All campaigns ever created" },
    { key: "max_active_campaigns", label: "Active campaigns", hint: "Running at the same time" },
    { key: "max_team_members", label: "Team members", hint: "Seats in this workspace" },
    { key: "max_contacts", label: "Contacts", hint: "Stored recipient records" },
    { key: "daily_campaign_limit", label: "Daily sends", hint: "Campaign emails per day" },
];

type FormState = Record<FieldKey, string> & { notes: string };

function emptyForm(overrides: OrganizationLimitOverrides | null | undefined): FormState {
    const blank: FormState = {
        max_email_accounts: "",
        max_campaigns: "",
        max_active_campaigns: "",
        max_team_members: "",
        max_contacts: "",
        daily_campaign_limit: "",
        notes: "",
    };
    if (!overrides) return blank;
    return {
        max_email_accounts: String(overrides.max_email_accounts),
        max_campaigns: String(overrides.max_campaigns),
        max_active_campaigns: String(overrides.max_active_campaigns),
        max_team_members: String(overrides.max_team_members),
        max_contacts: String(overrides.max_contacts),
        daily_campaign_limit: String(overrides.daily_campaign_limit),
        notes: overrides.notes,
    };
}

export function OrganizationOverridesDialog({
    org,
    open,
    onOpenChange,
}: {
    org: AdminOrgDetail;
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    const qc = useQueryClient();
    const [form, setForm] = useState<FormState>(() => emptyForm(org.overrides));

    // Reseed the form when the dialog reopens against a fresh org snapshot.
    useEffect(() => {
        if (open) setForm(emptyForm(org.overrides));
    }, [open, org.overrides]);

    const mutation = useMutation({
        mutationFn: (req: UpdateOrgOverridesRequest) =>
            updateOrganizationOverrides(org.id, req),
        onSuccess: () => {
            toast.success("Overrides saved");
            qc.invalidateQueries({ queryKey: ["admin", "organizations", org.id] });
            qc.invalidateQueries({ queryKey: ["admin", "organizations"] });
            onOpenChange(false);
        },
        onError: (err: Error) => {
            toast.error(err.message || "Failed to save overrides");
        },
    });

    function submit() {
        const req: UpdateOrgOverridesRequest = {};
        for (const f of FIELDS) {
            const raw = form[f.key].trim();
            if (raw === "") continue;
            const n = Number(raw);
            if (!Number.isInteger(n) || n < 0) {
                toast.error(`${f.label}: must be a non-negative integer`);
                return;
            }
            req[f.key] = n;
        }
        if (form.notes.trim() !== (org.overrides?.notes ?? "")) {
            req.notes = form.notes;
        }
        if (Object.keys(req).length === 0) {
            toast.error("Nothing changed");
            return;
        }
        mutation.mutate(req);
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-2xl">
                <DialogHeader>
                    <DialogTitle>Limit overrides</DialogTitle>
                    <DialogDescription>
                        Leave blank to keep the current value. <strong>0</strong> removes
                        the override (back to plan default or product hard cap). Positive
                        numbers set an explicit ceiling that overrides everything else.
                    </DialogDescription>
                </DialogHeader>

                <div className="grid gap-3">
                    {FIELDS.map((f) => {
                        const plan = (org.limits ?? {}) as Record<string, number | null | undefined>;
                        const effective = (org.effective_limits ?? {}) as Record<string, number | null | undefined>;
                        return (
                            <div key={f.key} className="grid grid-cols-[1fr_auto_auto_auto] items-center gap-3 text-sm">
                                <div>
                                    <Label htmlFor={f.key} className="text-xs font-medium">
                                        {f.label}
                                    </Label>
                                    <div className="text-[10px] text-muted-foreground">
                                        {f.hint}
                                    </div>
                                </div>
                                <div className="text-[10px] text-muted-foreground text-right">
                                    <div>plan</div>
                                    <div className="tabular-nums">
                                        {plan[f.key] != null ? plan[f.key] : "—"}
                                    </div>
                                </div>
                                <div className="text-[10px] text-muted-foreground text-right">
                                    <div>effective</div>
                                    <div className="tabular-nums font-medium text-foreground">
                                        {effective[f.key] != null ? effective[f.key] : "—"}
                                    </div>
                                </div>
                                <Input
                                    id={f.key}
                                    inputMode="numeric"
                                    placeholder="—"
                                    value={form[f.key]}
                                    onChange={(e) =>
                                        setForm((s) => ({ ...s, [f.key]: e.target.value }))
                                    }
                                    className="w-24 text-right tabular-nums"
                                />
                            </div>
                        );
                    })}

                    <div className="mt-2">
                        <Label htmlFor="notes" className="text-xs font-medium">
                            Notes
                        </Label>
                        <Input
                            id="notes"
                            placeholder="Reason for the change (visible to other admins)"
                            value={form.notes}
                            onChange={(e) =>
                                setForm((s) => ({ ...s, notes: e.target.value }))
                            }
                        />
                    </div>
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button
                        onClick={submit}
                        disabled={mutation.isPending}
                        className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                    >
                        {mutation.isPending ? "Saving…" : "Save overrides"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
