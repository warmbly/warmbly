// Plan catalog admin. Lists every plan with public/private status,
// price, the four limit columns, and a click-to-edit dialog for the
// limit + price fields. Stripe IDs are surfaced but read-only — those
// must be edited in Stripe and synced back, never the other way around.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Eye, EyeOff, Pencil } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { listPlans, updatePlan } from "@/lib/api/client/admin/plans";
import type { Plan, UpdatePlanRequest } from "@/lib/api/models/admin";

export default function PlansPage() {
    const { data, isLoading, error } = useQuery({
        queryKey: ["admin", "plans"],
        queryFn: listPlans,
        staleTime: 60_000,
    });

    const [editing, setEditing] = useState<Plan | null>(null);
    const plans = data?.data ?? [];

    return (
        <div>
            <PageHeader
                title="Plans & Billing"
                description="Catalog of pricing tiers. Public plans appear on the marketing site; private plans are reserved for enterprise contracts."
            />

            {isLoading && <Skeleton className="h-48 w-full" />}
            {error && (
                <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                    Failed to load plans.
                </div>
            )}

            {!isLoading && !error && (
                <div className="border border-border rounded-lg overflow-hidden bg-card">
                    <table className="w-full text-sm">
                        <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                            <tr>
                                <th className="text-left px-3 py-2 font-medium">Name</th>
                                <th className="text-left px-3 py-2 font-medium">Visibility</th>
                                <th className="text-right px-3 py-2 font-medium">Price</th>
                                <th className="text-right px-3 py-2 font-medium">Mailboxes</th>
                                <th className="text-right px-3 py-2 font-medium">Campaigns</th>
                                <th className="text-right px-3 py-2 font-medium">Members</th>
                                <th className="text-right px-3 py-2 font-medium">Contacts</th>
                                <th className="text-right px-3 py-2 font-medium">Daily</th>
                                <th className="text-right px-3 py-2 font-medium">Action</th>
                            </tr>
                        </thead>
                        <tbody>
                            {plans.map((p) => (
                                <PlanRow key={p.id} plan={p} onEdit={() => setEditing(p)} />
                            ))}
                            {plans.length === 0 && (
                                <tr>
                                    <td colSpan={9} className="text-center text-muted-foreground py-8 text-sm">
                                        No plans configured.
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            )}

            {editing && (
                <PlanEditDialog
                    plan={editing}
                    open
                    onOpenChange={(v) => !v && setEditing(null)}
                />
            )}
        </div>
    );
}

function PlanRow({ plan, onEdit }: { plan: Plan; onEdit: () => void }) {
    return (
        <tr className="border-t border-border hover:bg-muted/30">
            <td className="px-3 py-2">
                <div className="font-medium">{plan.name ?? "(unnamed)"}</div>
                {plan.stripe_price_id && (
                    <div className="text-[10px] text-muted-foreground font-mono">
                        {plan.stripe_price_id}
                    </div>
                )}
            </td>
            <td className="px-3 py-2">
                {plan.public ? (
                    <Badge
                        variant="outline"
                        className="text-[10px] border-emerald-300 text-emerald-700 bg-emerald-50"
                    >
                        <Eye className="size-2.5" /> public
                    </Badge>
                ) : (
                    <Badge
                        variant="outline"
                        className="text-[10px] border-purple-300 text-purple-700 bg-purple-50"
                    >
                        <EyeOff className="size-2.5" /> private
                    </Badge>
                )}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                ${plan.discounted_price.toFixed(2)}
                {plan.price !== plan.discounted_price && (
                    <span className="text-muted-foreground line-through ml-1 text-[10px]">
                        ${plan.price.toFixed(2)}
                    </span>
                )}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {plan.max_email_accounts ?? "∞"}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {plan.max_campaigns ?? "∞"}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {plan.max_team_members ?? "∞"}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {plan.max_contacts.toLocaleString()}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {plan.daily_emails}
            </td>
            <td className="px-3 py-2 text-right">
                <Button size="sm" variant="outline" onClick={onEdit} className="text-xs">
                    <Pencil className="size-3" />
                    Edit
                </Button>
            </td>
        </tr>
    );
}

interface FormState {
    name: string;
    price: string;
    discounted_price: string;
    max_email_accounts: string;
    max_campaigns: string;
    max_active_campaigns: string;
    max_team_members: string;
    max_contacts: string;
    daily_emails: string;
    daily_campaign_limit: string;
    account_limit: string;
    dedicated_workers: string;
    public: boolean;
}

type FormStringKey = Exclude<keyof FormState, "public">;

function seedForm(plan: Plan): FormState {
    return {
        name: plan.name ?? "",
        price: String(plan.price),
        discounted_price: String(plan.discounted_price),
        max_email_accounts: plan.max_email_accounts != null ? String(plan.max_email_accounts) : "",
        max_campaigns: plan.max_campaigns != null ? String(plan.max_campaigns) : "",
        max_active_campaigns: plan.max_active_campaigns != null ? String(plan.max_active_campaigns) : "",
        max_team_members: plan.max_team_members != null ? String(plan.max_team_members) : "",
        max_contacts: String(plan.max_contacts),
        daily_emails: String(plan.daily_emails),
        daily_campaign_limit: plan.daily_campaign_limit != null ? String(plan.daily_campaign_limit) : "",
        account_limit: String(plan.account_limit),
        dedicated_workers: String(plan.dedicated_workers),
        public: plan.public,
    };
}

function PlanEditDialog({
    plan,
    open,
    onOpenChange,
}: {
    plan: Plan;
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    const qc = useQueryClient();
    const [form, setForm] = useState<FormState>(() => seedForm(plan));

    const mutation = useMutation({
        mutationFn: (body: UpdatePlanRequest) => updatePlan(plan.id, body),
        onSuccess: () => {
            toast.success("Plan updated");
            qc.invalidateQueries({ queryKey: ["admin", "plans"] });
            onOpenChange(false);
        },
        onError: (err: Error) => toast.error(err.message || "Update failed"),
    });

    function submit() {
        const body: UpdatePlanRequest = {};
        const num = (key: FormStringKey): number | undefined => {
            const raw = form[key].trim();
            if (raw === "") return undefined;
            const n = Number(raw);
            if (Number.isNaN(n)) return undefined;
            return n;
        };
        if (form.name !== (plan.name ?? "")) body.name = form.name;
        const price = num("price");
        if (price != null && price !== plan.price) body.price = price;
        const disc = num("discounted_price");
        if (disc != null && disc !== plan.discounted_price) body.discounted_price = disc;
        const mailboxes = num("max_email_accounts");
        if (mailboxes != null) body.max_email_accounts = mailboxes;
        const campaigns = num("max_campaigns");
        if (campaigns != null) body.max_campaigns = campaigns;
        const active = num("max_active_campaigns");
        if (active != null) body.max_active_campaigns = active;
        const members = num("max_team_members");
        if (members != null) body.max_team_members = members;
        const contacts = num("max_contacts");
        if (contacts != null) body.max_contacts = contacts;
        const daily = num("daily_emails");
        if (daily != null) body.daily_emails = daily;
        const dailyCamp = num("daily_campaign_limit");
        if (dailyCamp != null) body.daily_campaign_limit = dailyCamp;
        const acc = num("account_limit");
        if (acc != null) body.account_limit = acc;
        const ded = num("dedicated_workers");
        if (ded != null) body.dedicated_workers = ded;
        if (form.public !== plan.public) body.public = form.public;

        if (Object.keys(body).length === 0) {
            toast.error("Nothing changed");
            return;
        }
        mutation.mutate(body);
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-2xl max-h-[90vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>Edit plan</DialogTitle>
                    <DialogDescription>
                        Changes affect every active subscriber on this plan. Stripe
                        IDs are not editable here — sync those through Stripe's
                        dashboard instead.
                    </DialogDescription>
                </DialogHeader>

                <div className="grid gap-3">
                    <Field label="Name" id="name" form={form} setForm={setForm} />
                    <div className="grid grid-cols-2 gap-3">
                        <Field label="Price" id="price" form={form} setForm={setForm} />
                        <Field
                            label="Discounted price"
                            id="discounted_price"
                            form={form}
                            setForm={setForm}
                        />
                    </div>
                    <div className="grid grid-cols-3 gap-3">
                        <Field label="Mailboxes" id="max_email_accounts" form={form} setForm={setForm} />
                        <Field label="Campaigns" id="max_campaigns" form={form} setForm={setForm} />
                        <Field label="Active campaigns" id="max_active_campaigns" form={form} setForm={setForm} />
                    </div>
                    <div className="grid grid-cols-3 gap-3">
                        <Field label="Team members" id="max_team_members" form={form} setForm={setForm} />
                        <Field label="Contacts" id="max_contacts" form={form} setForm={setForm} />
                        <Field label="Daily emails" id="daily_emails" form={form} setForm={setForm} />
                    </div>
                    <div className="grid grid-cols-3 gap-3">
                        <Field label="Daily campaign limit" id="daily_campaign_limit" form={form} setForm={setForm} />
                        <Field label="Account limit" id="account_limit" form={form} setForm={setForm} />
                        <Field label="Dedicated workers" id="dedicated_workers" form={form} setForm={setForm} />
                    </div>
                    <label className="inline-flex items-center gap-2 text-xs">
                        <input
                            type="checkbox"
                            checked={form.public}
                            onChange={(e) => setForm((s) => ({ ...s, public: e.target.checked }))}
                            className="accent-[var(--admin-accent)]"
                        />
                        Public (appears in the marketing-site plan list)
                    </label>
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
                        {mutation.isPending ? "Saving…" : "Save"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function Field({
    label,
    id,
    form,
    setForm,
}: {
    label: string;
    id: FormStringKey;
    form: FormState;
    setForm: React.Dispatch<React.SetStateAction<FormState>>;
}) {
    return (
        <div>
            <Label htmlFor={id} className="text-xs font-medium">
                {label}
            </Label>
            <Input
                id={id}
                value={form[id]}
                onChange={(e) => setForm((s) => ({ ...s, [id]: e.target.value }))}
                className="text-sm"
            />
        </div>
    );
}
