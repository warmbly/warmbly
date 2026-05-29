// Discount / promo code management. Lists every code with its type, value,
// plan eligibility, usage, status, and expiry; supports create, edit,
// enable/disable, delete, and a per-code redemptions viewer. Codes are
// validated and stored in our database; the billing layer mints a one-off
// Stripe coupon (or trial extension) at redemption time.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Pencil, Plus, Receipt, Trash2 } from "lucide-react";
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
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import {
    createDiscount,
    deleteDiscount,
    listDiscountRedemptions,
    listDiscounts,
    listPlansForEligibility,
    updateDiscount,
} from "@/lib/api/client/admin/discounts";
import type {
    CreateDiscountRequest,
    Discount,
    DiscountCodeStatus,
    DiscountDuration,
    DiscountType,
    Plan,
    UpdateDiscountRequest,
} from "@/lib/api/models/admin";

const STATUS_FILTERS: { value: string; label: string }[] = [
    { value: "all", label: "All statuses" },
    { value: "active", label: "Active" },
    { value: "disabled", label: "Disabled" },
    { value: "expired", label: "Expired" },
];

export default function DiscountsPage() {
    const [status, setStatus] = useState("all");
    const [search, setSearch] = useState("");
    const [creating, setCreating] = useState(false);
    const [editing, setEditing] = useState<Discount | null>(null);
    const [viewing, setViewing] = useState<Discount | null>(null);

    const { data, isLoading, error } = useQuery({
        queryKey: ["admin", "discounts", { status, search }],
        queryFn: () => listDiscounts({ status, search }),
        staleTime: 60_000,
    });

    const discounts = data?.data ?? [];

    return (
        <div>
            <PageHeader
                title="Discounts"
                description="Generate and manage promo codes. Restrict a code to specific plans, set usage limits and expiry, and track redemptions."
            >
                <Input
                    placeholder="Search codes…"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    className="h-8 w-44 text-sm"
                />
                <Select value={status} onValueChange={setStatus}>
                    <SelectTrigger className="h-8 w-36 text-sm">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        {STATUS_FILTERS.map((s) => (
                            <SelectItem key={s.value} value={s.value}>
                                {s.label}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
                <Button
                    size="sm"
                    onClick={() => setCreating(true)}
                    className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                >
                    <Plus className="size-4" /> Add discount
                </Button>
            </PageHeader>

            {isLoading && <Skeleton className="h-48 w-full" />}
            {error && (
                <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                    Failed to load discount codes.
                </div>
            )}

            {!isLoading && !error && (
                <div className="border border-border rounded-lg overflow-hidden bg-card">
                    <table className="w-full text-sm">
                        <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                            <tr>
                                <th className="text-left px-3 py-2 font-medium">Code</th>
                                <th className="text-left px-3 py-2 font-medium">Discount</th>
                                <th className="text-left px-3 py-2 font-medium">Plans</th>
                                <th className="text-right px-3 py-2 font-medium">Used</th>
                                <th className="text-left px-3 py-2 font-medium">Status</th>
                                <th className="text-left px-3 py-2 font-medium">Expires</th>
                                <th className="text-right px-3 py-2 font-medium">Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            {discounts.map((d) => (
                                <DiscountRow
                                    key={d.id}
                                    discount={d}
                                    onEdit={() => setEditing(d)}
                                    onViewRedemptions={() => setViewing(d)}
                                />
                            ))}
                            {discounts.length === 0 && (
                                <tr>
                                    <td
                                        colSpan={7}
                                        className="text-center text-muted-foreground py-8 text-sm"
                                    >
                                        No discount codes yet. Create one to get started.
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            )}

            {creating && (
                <DiscountDialog
                    mode="create"
                    open
                    onOpenChange={(v) => !v && setCreating(false)}
                />
            )}
            {editing && (
                <DiscountDialog
                    mode="edit"
                    discount={editing}
                    open
                    onOpenChange={(v) => !v && setEditing(null)}
                />
            )}
            {viewing && (
                <RedemptionsDialog
                    discount={viewing}
                    open
                    onOpenChange={(v) => !v && setViewing(null)}
                />
            )}
        </div>
    );
}

function formatValue(d: Discount): string {
    if (d.type === "percent") return `${d.percent_off ?? 0}% off`;
    if (d.type === "fixed") {
        return `${(d.currency ?? "usd").toUpperCase()} ${(d.amount_off ?? 0).toFixed(2)} off`;
    }
    return `+${d.trial_extension_days ?? 0} trial days`;
}

function durationLabel(d: Discount): string | null {
    if (d.type === "trial_extension") return null;
    if (d.duration === "forever") return "forever";
    if (d.duration === "repeating") return `${d.duration_in_months ?? 0} mo`;
    return "once";
}

const STATUS_STYLES: Record<DiscountCodeStatus, string> = {
    active: "border-emerald-300 text-emerald-700 bg-emerald-50",
    disabled: "border-zinc-300 text-zinc-600 bg-zinc-50",
    expired: "border-red-300 text-red-700 bg-red-50",
};

function DiscountRow({
    discount: d,
    onEdit,
    onViewRedemptions,
}: {
    discount: Discount;
    onEdit: () => void;
    onViewRedemptions: () => void;
}) {
    const qc = useQueryClient();
    const [confirmDelete, setConfirmDelete] = useState(false);

    const del = useMutation({
        mutationFn: () => deleteDiscount(d.id),
        onSuccess: () => {
            toast.success("Discount deleted");
            qc.invalidateQueries({ queryKey: ["admin", "discounts"] });
        },
        onError: (e: Error) => toast.error(e.message || "Delete failed"),
    });

    const dur = durationLabel(d);

    return (
        <tr className="border-t border-border hover:bg-muted/30 align-middle">
            <td className="px-3 py-2">
                <div className="font-mono font-medium">{d.code}</div>
                {d.description && (
                    <div className="text-[11px] text-muted-foreground max-w-xs truncate">
                        {d.description}
                    </div>
                )}
            </td>
            <td className="px-3 py-2">
                <span className="tabular-nums">{formatValue(d)}</span>
                {dur && (
                    <span className="ml-1.5 text-[10px] text-muted-foreground uppercase">
                        {dur}
                    </span>
                )}
            </td>
            <td className="px-3 py-2">
                {d.applies_to_all_plans ? (
                    <span className="text-muted-foreground">All plans</span>
                ) : (
                    <span>{d.plan_ids.length} plan{d.plan_ids.length === 1 ? "" : "s"}</span>
                )}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {d.times_redeemed}
                {d.max_redemptions != null && (
                    <span className="text-muted-foreground"> / {d.max_redemptions}</span>
                )}
            </td>
            <td className="px-3 py-2">
                <Badge variant="outline" className={`text-[10px] ${STATUS_STYLES[d.status]}`}>
                    {d.status}
                </Badge>
            </td>
            <td className="px-3 py-2 text-muted-foreground text-xs">
                {d.expires_at ? new Date(d.expires_at).toLocaleDateString() : "Never"}
            </td>
            <td className="px-3 py-2">
                <div className="flex items-center justify-end gap-1.5">
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={onViewRedemptions}
                        className="text-xs"
                    >
                        <Receipt className="size-3" /> Redemptions
                    </Button>
                    <Button size="sm" variant="outline" onClick={onEdit} className="text-xs">
                        <Pencil className="size-3" /> Edit
                    </Button>
                    <Button
                        size="sm"
                        variant={confirmDelete ? "destructive" : "outline"}
                        disabled={del.isPending}
                        onClick={() => {
                            if (confirmDelete) {
                                del.mutate();
                            } else {
                                setConfirmDelete(true);
                                setTimeout(() => setConfirmDelete(false), 4000);
                            }
                        }}
                        className="text-xs"
                    >
                        <Trash2 className="size-3" />
                        {confirmDelete ? "Confirm" : ""}
                    </Button>
                </div>
            </td>
        </tr>
    );
}

interface DiscountForm {
    code: string;
    description: string;
    type: DiscountType;
    percent_off: string;
    amount_off: string;
    currency: string;
    trial_extension_days: string;
    duration: DiscountDuration;
    duration_in_months: string;
    max_redemptions: string;
    per_account_limit: string;
    status: DiscountCodeStatus;
    starts_at: string;
    expires_at: string;
    applies_to_all_plans: boolean;
    plan_ids: string[];
}

function emptyForm(): DiscountForm {
    return {
        code: "",
        description: "",
        type: "percent",
        percent_off: "",
        amount_off: "",
        currency: "usd",
        trial_extension_days: "",
        duration: "once",
        duration_in_months: "",
        max_redemptions: "",
        per_account_limit: "1",
        status: "active",
        starts_at: "",
        expires_at: "",
        applies_to_all_plans: true,
        plan_ids: [],
    };
}

function seedForm(d: Discount): DiscountForm {
    return {
        code: d.code,
        description: d.description ?? "",
        type: d.type,
        percent_off: d.percent_off != null ? String(d.percent_off) : "",
        amount_off: d.amount_off != null ? String(d.amount_off) : "",
        currency: d.currency ?? "usd",
        trial_extension_days:
            d.trial_extension_days != null ? String(d.trial_extension_days) : "",
        duration: d.duration,
        duration_in_months:
            d.duration_in_months != null ? String(d.duration_in_months) : "",
        max_redemptions: d.max_redemptions != null ? String(d.max_redemptions) : "",
        per_account_limit: String(d.per_account_limit),
        status: d.status,
        starts_at: toLocalInput(d.starts_at),
        expires_at: toLocalInput(d.expires_at),
        applies_to_all_plans: d.applies_to_all_plans,
        plan_ids: d.plan_ids ?? [],
    };
}

function toLocalInput(iso?: string | null): string {
    if (!iso) return "";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    // Render as a value the datetime-local input understands (local time).
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fromLocalInput(v: string): string | undefined {
    if (!v) return undefined;
    const d = new Date(v);
    if (Number.isNaN(d.getTime())) return undefined;
    return d.toISOString();
}

function numOrUndef(s: string): number | undefined {
    const t = s.trim();
    if (t === "") return undefined;
    const n = Number(t);
    return Number.isNaN(n) ? undefined : n;
}

function DiscountDialog({
    mode,
    discount,
    open,
    onOpenChange,
}: {
    mode: "create" | "edit";
    discount?: Discount;
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    const qc = useQueryClient();
    const [form, setForm] = useState<DiscountForm>(() =>
        discount ? seedForm(discount) : emptyForm(),
    );

    const plansQuery = useQuery({
        queryKey: ["admin", "plans", "eligibility"],
        queryFn: listPlansForEligibility,
        staleTime: 60_000,
    });
    const plans = plansQuery.data ?? [];

    const mutation = useMutation({
        mutationFn: () => {
            if (mode === "create") return createDiscount(buildCreate(form));
            return updateDiscount(discount!.id, buildUpdate(form));
        },
        onSuccess: () => {
            toast.success(mode === "create" ? "Discount created" : "Discount updated");
            qc.invalidateQueries({ queryKey: ["admin", "discounts"] });
            onOpenChange(false);
        },
        onError: (e: Error) => toast.error(e.message || "Save failed"),
    });

    function submit() {
        const reason = validate(form, mode);
        if (reason) {
            toast.error(reason);
            return;
        }
        mutation.mutate();
    }

    function patch<K extends keyof DiscountForm>(key: K, value: DiscountForm[K]) {
        setForm((s) => ({ ...s, [key]: value }));
    }

    const isMoney = form.type !== "trial_extension";

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-2xl max-h-[90vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle>
                        {mode === "create" ? "Create discount code" : `Edit ${discount?.code}`}
                    </DialogTitle>
                    <DialogDescription>
                        {mode === "create"
                            ? "Codes are stored here and applied through Stripe at redemption."
                            : "The discount type can't be changed. Recreate the code to change kinds."}
                    </DialogDescription>
                </DialogHeader>

                <div className="grid gap-3">
                    <div className="grid grid-cols-2 gap-3">
                        <div>
                            <Label className="text-xs font-medium">Code</Label>
                            <Input
                                value={form.code}
                                disabled={mode === "edit"}
                                onChange={(e) => patch("code", e.target.value.toUpperCase())}
                                placeholder="WELCOME10"
                                className="text-sm font-mono"
                            />
                        </div>
                        <div>
                            <Label className="text-xs font-medium">Type</Label>
                            <Select
                                value={form.type}
                                disabled={mode === "edit"}
                                onValueChange={(v) => patch("type", v as DiscountType)}
                            >
                                <SelectTrigger className="text-sm">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="percent">Percentage off</SelectItem>
                                    <SelectItem value="fixed">Fixed amount off</SelectItem>
                                    <SelectItem value="trial_extension">
                                        Free-trial extension
                                    </SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>

                    <div>
                        <Label className="text-xs font-medium">Description</Label>
                        <Input
                            value={form.description}
                            onChange={(e) => patch("description", e.target.value)}
                            placeholder="Internal note shown only in admin"
                            className="text-sm"
                        />
                    </div>

                    {/* Value fields per type */}
                    {form.type === "percent" && (
                        <div className="grid grid-cols-2 gap-3">
                            <div>
                                <Label className="text-xs font-medium">Percent off (1–100)</Label>
                                <Input
                                    value={form.percent_off}
                                    onChange={(e) => patch("percent_off", e.target.value)}
                                    placeholder="10"
                                    className="text-sm"
                                />
                            </div>
                        </div>
                    )}
                    {form.type === "fixed" && (
                        <div className="grid grid-cols-2 gap-3">
                            <div>
                                <Label className="text-xs font-medium">Amount off</Label>
                                <Input
                                    value={form.amount_off}
                                    onChange={(e) => patch("amount_off", e.target.value)}
                                    placeholder="10.00"
                                    className="text-sm"
                                />
                            </div>
                            <div>
                                <Label className="text-xs font-medium">Currency</Label>
                                <Input
                                    value={form.currency}
                                    onChange={(e) =>
                                        patch("currency", e.target.value.toLowerCase())
                                    }
                                    placeholder="usd"
                                    className="text-sm"
                                />
                            </div>
                        </div>
                    )}
                    {form.type === "trial_extension" && (
                        <div className="grid grid-cols-2 gap-3">
                            <div>
                                <Label className="text-xs font-medium">Trial days added</Label>
                                <Input
                                    value={form.trial_extension_days}
                                    onChange={(e) =>
                                        patch("trial_extension_days", e.target.value)
                                    }
                                    placeholder="14"
                                    className="text-sm"
                                />
                            </div>
                        </div>
                    )}

                    {/* Duration (money types only) */}
                    {isMoney && (
                        <div className="grid grid-cols-2 gap-3">
                            <div>
                                <Label className="text-xs font-medium">Duration</Label>
                                <Select
                                    value={form.duration}
                                    onValueChange={(v) =>
                                        patch("duration", v as DiscountDuration)
                                    }
                                >
                                    <SelectTrigger className="text-sm">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="once">Once</SelectItem>
                                        <SelectItem value="repeating">
                                            Repeating (N months)
                                        </SelectItem>
                                        <SelectItem value="forever">Forever</SelectItem>
                                    </SelectContent>
                                </Select>
                            </div>
                            {form.duration === "repeating" && (
                                <div>
                                    <Label className="text-xs font-medium">Months</Label>
                                    <Input
                                        value={form.duration_in_months}
                                        onChange={(e) =>
                                            patch("duration_in_months", e.target.value)
                                        }
                                        placeholder="3"
                                        className="text-sm"
                                    />
                                </div>
                            )}
                        </div>
                    )}

                    <div className="grid grid-cols-3 gap-3">
                        <div>
                            <Label className="text-xs font-medium">Max redemptions</Label>
                            <Input
                                value={form.max_redemptions}
                                onChange={(e) => patch("max_redemptions", e.target.value)}
                                placeholder="∞"
                                className="text-sm"
                            />
                        </div>
                        <div>
                            <Label className="text-xs font-medium">Per-account limit</Label>
                            <Input
                                value={form.per_account_limit}
                                onChange={(e) => patch("per_account_limit", e.target.value)}
                                placeholder="1"
                                className="text-sm"
                            />
                        </div>
                        <div>
                            <Label className="text-xs font-medium">Status</Label>
                            <Select
                                value={form.status}
                                onValueChange={(v) => patch("status", v as DiscountCodeStatus)}
                            >
                                <SelectTrigger className="text-sm">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="active">Active</SelectItem>
                                    <SelectItem value="disabled">Disabled</SelectItem>
                                    <SelectItem value="expired">Expired</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>

                    <div className="grid grid-cols-2 gap-3">
                        <div>
                            <Label className="text-xs font-medium">Starts at</Label>
                            <Input
                                type="datetime-local"
                                value={form.starts_at}
                                onChange={(e) => patch("starts_at", e.target.value)}
                                className="text-sm"
                            />
                        </div>
                        <div>
                            <Label className="text-xs font-medium">Expires at</Label>
                            <Input
                                type="datetime-local"
                                value={form.expires_at}
                                onChange={(e) => patch("expires_at", e.target.value)}
                                className="text-sm"
                            />
                        </div>
                    </div>

                    {/* Plan eligibility */}
                    <div className="rounded-md border border-border p-3">
                        <label className="inline-flex items-center gap-2 text-xs font-medium">
                            <input
                                type="checkbox"
                                checked={form.applies_to_all_plans}
                                onChange={(e) =>
                                    patch("applies_to_all_plans", e.target.checked)
                                }
                                className="accent-[var(--admin-accent)]"
                            />
                            Valid for all plans
                        </label>
                        {!form.applies_to_all_plans && (
                            <div className="mt-2 grid grid-cols-2 gap-1.5 max-h-40 overflow-y-auto">
                                {plans.map((p: Plan) => (
                                    <label
                                        key={p.id}
                                        className="inline-flex items-center gap-2 text-xs"
                                    >
                                        <input
                                            type="checkbox"
                                            checked={form.plan_ids.includes(p.id)}
                                            onChange={(e) =>
                                                setForm((s) => ({
                                                    ...s,
                                                    plan_ids: e.target.checked
                                                        ? [...s.plan_ids, p.id]
                                                        : s.plan_ids.filter((id) => id !== p.id),
                                                }))
                                            }
                                            className="accent-[var(--admin-accent)]"
                                        />
                                        {p.name ?? "(unnamed)"}
                                    </label>
                                ))}
                                {plans.length === 0 && (
                                    <div className="text-[11px] text-muted-foreground col-span-2">
                                        No plans available.
                                    </div>
                                )}
                            </div>
                        )}
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
                        {mutation.isPending
                            ? "Saving…"
                            : mode === "create"
                              ? "Create"
                              : "Save"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function validate(form: DiscountForm, mode: "create" | "edit"): string | null {
    if (mode === "create" && !form.code.trim()) return "Code is required.";
    if (form.type === "percent") {
        const p = numOrUndef(form.percent_off);
        if (p == null || p < 1 || p > 100) return "Percent off must be between 1 and 100.";
    }
    if (form.type === "fixed") {
        const a = numOrUndef(form.amount_off);
        if (a == null || a <= 0) return "Amount off must be greater than 0.";
        if (form.currency.trim().length !== 3) return "Currency must be a 3-letter code.";
    }
    if (form.type === "trial_extension") {
        const t = numOrUndef(form.trial_extension_days);
        if (t == null || t <= 0) return "Trial days must be greater than 0.";
    }
    if (form.type !== "trial_extension" && form.duration === "repeating") {
        const m = numOrUndef(form.duration_in_months);
        if (m == null || m <= 0) return "Repeating duration needs a month count.";
    }
    if (!form.applies_to_all_plans && form.plan_ids.length === 0) {
        return "Select at least one eligible plan, or mark the code valid for all plans.";
    }
    if (form.per_account_limit.trim() !== "") {
        const pal = numOrUndef(form.per_account_limit);
        if (pal == null || pal < 1 || !Number.isInteger(pal)) {
            return "Per-account limit must be a whole number of at least 1.";
        }
    }
    return null;
}

function buildCreate(form: DiscountForm): CreateDiscountRequest {
    const body: CreateDiscountRequest = {
        code: form.code.trim(),
        type: form.type,
        applies_to_all_plans: form.applies_to_all_plans,
    };
    if (form.description.trim()) body.description = form.description.trim();
    applyValue(form, body);
    const maxr = numOrUndef(form.max_redemptions);
    if (maxr != null) body.max_redemptions = maxr;
    const pal = numOrUndef(form.per_account_limit);
    if (pal != null) body.per_account_limit = pal;
    if (form.status) body.status = form.status;
    const sa = fromLocalInput(form.starts_at);
    if (sa) body.starts_at = sa;
    const ea = fromLocalInput(form.expires_at);
    if (ea) body.expires_at = ea;
    if (!form.applies_to_all_plans) body.plan_ids = form.plan_ids;
    return body;
}

function buildUpdate(form: DiscountForm): UpdateDiscountRequest {
    const body: UpdateDiscountRequest = {
        description: form.description.trim(),
        per_account_limit: numOrUndef(form.per_account_limit),
        status: form.status,
        applies_to_all_plans: form.applies_to_all_plans,
    };
    applyValue(form, body);
    const maxr = numOrUndef(form.max_redemptions);
    if (maxr != null) body.max_redemptions = maxr;
    const sa = fromLocalInput(form.starts_at);
    if (sa) body.starts_at = sa;
    const ea = fromLocalInput(form.expires_at);
    if (ea) body.expires_at = ea;
    body.plan_ids = form.applies_to_all_plans ? [] : form.plan_ids;
    return body;
}

// applyValue writes the type-specific value + duration fields onto a request.
function applyValue(
    form: DiscountForm,
    body: CreateDiscountRequest | UpdateDiscountRequest,
) {
    if (form.type === "percent") {
        body.percent_off = numOrUndef(form.percent_off);
    } else if (form.type === "fixed") {
        body.amount_off = numOrUndef(form.amount_off);
        body.currency = form.currency.trim().toLowerCase() || "usd";
    } else {
        body.trial_extension_days = numOrUndef(form.trial_extension_days);
    }
    if (form.type !== "trial_extension") {
        body.duration = form.duration;
        if (form.duration === "repeating") {
            body.duration_in_months = numOrUndef(form.duration_in_months);
        }
    }
}

function RedemptionsDialog({
    discount,
    open,
    onOpenChange,
}: {
    discount: Discount;
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    const { data, isLoading, error } = useQuery({
        queryKey: ["admin", "discounts", discount.id, "redemptions"],
        queryFn: () => listDiscountRedemptions(discount.id),
        staleTime: 30_000,
    });
    const redemptions = data?.data ?? [];

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-xl max-h-[80vh] overflow-y-auto">
                <DialogHeader>
                    <DialogTitle className="font-mono">{discount.code} redemptions</DialogTitle>
                    <DialogDescription>
                        {discount.times_redeemed} total
                        {discount.max_redemptions != null
                            ? ` of ${discount.max_redemptions}`
                            : ""}{" "}
                        redeemed.
                    </DialogDescription>
                </DialogHeader>

                {isLoading && <Skeleton className="h-32 w-full" />}
                {error && (
                    <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                        Failed to load redemptions.
                    </div>
                )}
                {!isLoading && !error && (
                    <table className="w-full text-xs">
                        <thead className="text-muted-foreground uppercase">
                            <tr>
                                <th className="text-left py-1.5 font-medium">Organization</th>
                                <th className="text-left py-1.5 font-medium">Status</th>
                                <th className="text-left py-1.5 font-medium">When</th>
                            </tr>
                        </thead>
                        <tbody>
                            {redemptions.map((r) => (
                                <tr key={r.id} className="border-t border-border">
                                    <td className="py-1.5 font-mono">
                                        {r.organization_id.slice(0, 8)}…
                                    </td>
                                    <td className="py-1.5">{r.status}</td>
                                    <td className="py-1.5 text-muted-foreground">
                                        {new Date(r.redeemed_at).toLocaleString()}
                                    </td>
                                </tr>
                            ))}
                            {redemptions.length === 0 && (
                                <tr>
                                    <td
                                        colSpan={3}
                                        className="text-center text-muted-foreground py-6"
                                    >
                                        No redemptions yet.
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                )}

                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Close
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
