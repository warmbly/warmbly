// Create or edit a promo code.
//
// Modelled on what a coupon builder has to ask (value, how long it lasts, what
// it applies to, how many times it can be used, when it is live) but drawn in
// the admin theme, and worded for the two things that actually go wrong:
// confusing the global redemption cap with the per-workspace one, and not
// realising a months-long repeating discount covers a whole annual invoice.

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { createDiscount, updateDiscount } from "@/lib/api/client/admin/discounts";
import { listAdminPlans } from "@/lib/api/client/admin/organizations";
import type {
    CreateDiscountCodeRequest,
    DiscountCode,
    DiscountCodeStatus,
    DiscountDuration,
    DiscountType,
    UpdateDiscountCodeRequest,
} from "@/lib/api/models/admin";
import { describeDiscount, describeIntervalEffect } from "./summary";

// --- date <-> input plumbing ------------------------------------------------
// The inputs are plain dates; the column is an instant. A start is the
// beginning of that day and an expiry is the end of it, both in the operator's
// own timezone, which is the only reading that matches what they typed.

function toDateInput(iso?: string | null): string {
    if (!iso) return "";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

function startOfDayISO(date: string): string | undefined {
    if (!date) return undefined;
    const d = new Date(`${date}T00:00:00`);
    return Number.isNaN(d.getTime()) ? undefined : d.toISOString();
}

function endOfDayISO(date: string): string | undefined {
    if (!date) return undefined;
    const d = new Date(`${date}T23:59:59.999`);
    return Number.isNaN(d.getTime()) ? undefined : d.toISOString();
}

function parseNum(s: string): number | undefined {
    if (s.trim() === "") return undefined;
    const n = Number(s);
    return Number.isFinite(n) ? n : undefined;
}

const TYPE_OPTIONS: { value: DiscountType; label: string; hint: string }[] = [
    { value: "percent", label: "Percentage off", hint: "Scales with the plan price." },
    { value: "fixed", label: "Fixed amount off", hint: "One currency only." },
    { value: "trial_extension", label: "Free trial days", hint: "Adds trial days at checkout." },
];

const DURATION_OPTIONS: { value: DiscountDuration; label: string }[] = [
    { value: "once", label: "First invoice" },
    { value: "repeating", label: "Multiple months" },
    { value: "forever", label: "Forever" },
];

// --- small themed building blocks ------------------------------------------

function Section({
    title,
    description,
    children,
}: {
    title: string;
    description?: string;
    children: React.ReactNode;
}) {
    return (
        <section className="border-t border-border px-4 py-3.5 first:border-t-0">
            <div className="mb-2.5">
                <h3 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                    {title}
                </h3>
                {description && (
                    <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">{description}</p>
                )}
            </div>
            <div className="space-y-3">{children}</div>
        </section>
    );
}

function Segmented<T extends string>({
    value,
    onChange,
    options,
    disabled,
}: {
    value: T;
    onChange: (v: T) => void;
    options: { value: T; label: string }[];
    disabled?: boolean;
}) {
    return (
        <div
            className={cn(
                "flex gap-0.5 rounded-md border border-border bg-card p-0.5 text-[11px]",
                disabled && "pointer-events-none opacity-50",
            )}
        >
            {options.map((o) => (
                <button
                    key={o.value}
                    type="button"
                    onClick={() => onChange(o.value)}
                    className={cn(
                        "flex-1 rounded px-2 py-1.5 transition-colors",
                        value === o.value
                            ? "bg-[var(--admin-accent)] font-medium text-white"
                            : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
                    )}
                >
                    {o.label}
                </button>
            ))}
        </div>
    );
}

function FieldRow({
    label,
    htmlFor,
    hint,
    children,
}: {
    label: string;
    htmlFor?: string;
    hint?: string;
    children: React.ReactNode;
}) {
    return (
        <div>
            <Label htmlFor={htmlFor} className="mb-1 block text-[11px] font-medium">
                {label}
            </Label>
            {children}
            {hint && <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">{hint}</p>}
        </div>
    );
}

/** A cap that is off by default. Unchecking it clears the value, which for an
 *  edit means sending an explicit null rather than omitting the key. */
function OptionalCap({
    enabled,
    onEnabledChange,
    label,
    hint,
    children,
}: {
    enabled: boolean;
    onEnabledChange: (v: boolean) => void;
    label: string;
    hint?: string;
    children: React.ReactNode;
}) {
    return (
        <div>
            <label className="flex cursor-pointer items-center gap-2 text-[12.5px]">
                <input
                    type="checkbox"
                    checked={enabled}
                    onChange={(e) => onEnabledChange(e.target.checked)}
                    className="size-3.5 accent-[var(--admin-accent)]"
                />
                {label}
            </label>
            {hint && <p className="mt-1 text-[11px] leading-relaxed text-muted-foreground">{hint}</p>}
            {enabled && <div className="mt-2 pl-6">{children}</div>}
        </div>
    );
}

// --- the dialog -------------------------------------------------------------

export function DiscountBuilderDialog({
    existing,
    open,
    onOpenChange,
}: {
    existing?: DiscountCode | null;
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    const qc = useQueryClient();
    const editing = !!existing;

    const [code, setCode] = useState("");
    const [description, setDescription] = useState("");
    const [type, setType] = useState<DiscountType>("percent");
    const [percentOff, setPercentOff] = useState("");
    const [amountOff, setAmountOff] = useState("");
    const [currency, setCurrency] = useState("USD");
    const [trialDays, setTrialDays] = useState("");
    const [duration, setDuration] = useState<DiscountDuration>("once");
    const [months, setMonths] = useState("3");
    const [allPlans, setAllPlans] = useState(true);
    const [planIds, setPlanIds] = useState<string[]>([]);
    const [capTotal, setCapTotal] = useState(false);
    const [maxRedemptions, setMaxRedemptions] = useState("");
    const [perAccount, setPerAccount] = useState("1");
    const [startsOn, setStartsOn] = useState("");
    const [expiresOn, setExpiresOn] = useState("");
    const [status, setStatus] = useState<DiscountCodeStatus>("active");

    // Reset to the edited row (or to defaults) every time the dialog opens, so
    // a cancelled edit never leaks into the next one.
    useEffect(() => {
        if (!open) return;
        if (existing) {
            setCode(existing.code);
            setDescription(existing.description ?? "");
            setType(existing.type);
            setPercentOff(existing.percent_off != null ? String(existing.percent_off) : "");
            setAmountOff(existing.amount_off != null ? String(existing.amount_off) : "");
            setCurrency((existing.currency || "USD").toUpperCase());
            setTrialDays(
                existing.trial_extension_days != null ? String(existing.trial_extension_days) : "",
            );
            setDuration(existing.duration);
            setMonths(existing.duration_in_months != null ? String(existing.duration_in_months) : "3");
            setAllPlans(existing.applies_to_all_plans);
            setPlanIds(existing.plan_ids ?? []);
            setCapTotal(existing.max_redemptions != null);
            setMaxRedemptions(
                existing.max_redemptions != null ? String(existing.max_redemptions) : "",
            );
            setPerAccount(String(existing.per_account_limit ?? 1));
            setStartsOn(toDateInput(existing.starts_at));
            setExpiresOn(toDateInput(existing.expires_at));
            setStatus(existing.status);
        } else {
            setCode("");
            setDescription("");
            setType("percent");
            setPercentOff("");
            setAmountOff("");
            setCurrency("USD");
            setTrialDays("");
            setDuration("once");
            setMonths("3");
            setAllPlans(true);
            setPlanIds([]);
            setCapTotal(false);
            setMaxRedemptions("");
            setPerAccount("1");
            setStartsOn("");
            setExpiresOn("");
            setStatus("active");
        }
    }, [open, existing]);

    const plans = useQuery({
        queryKey: ["admin", "plans"],
        queryFn: () => listAdminPlans(),
        enabled: open,
        staleTime: 5 * 60_000,
    });

    const isMoney = type !== "trial_extension";
    const shape = {
        type,
        percent_off: parseNum(percentOff),
        amount_off: parseNum(amountOff),
        currency,
        trial_extension_days: parseNum(trialDays),
        duration: isMoney ? duration : ("once" as DiscountDuration),
        duration_in_months: parseNum(months),
    };

    // Mirrors validateShape in internal/app/discount/service.go, so the
    // operator sees the objection before the round trip rather than after.
    const problem = useMemo((): string | null => {
        if (!editing && !code.trim()) return "Give the code something to type at checkout.";
        if (type === "percent") {
            const p = parseNum(percentOff);
            if (p === undefined || p < 1 || p > 100) return "A percentage has to be between 1 and 100.";
        }
        if (type === "fixed") {
            const a = parseNum(amountOff);
            if (a === undefined || a <= 0) return "A fixed discount needs an amount above zero.";
            if (currency.trim().length !== 3) return "A fixed discount needs a 3-letter currency.";
        }
        if (type === "trial_extension") {
            const d = parseNum(trialDays);
            if (d === undefined || d <= 0) return "Trial days have to be above zero.";
        }
        if (isMoney && duration === "repeating") {
            const m = parseNum(months);
            if (m === undefined || m <= 0) return "A repeating discount needs a number of months.";
        }
        if (!allPlans && planIds.length === 0) {
            return "Pick at least one plan, or make the code valid for all of them.";
        }
        if (capTotal) {
            const m = parseNum(maxRedemptions);
            if (m === undefined || m < 1) return "A total redemption cap has to be at least 1.";
        }
        const pa = parseNum(perAccount);
        if (pa === undefined || pa < 1) return "The per-workspace limit has to be at least 1.";
        if (startsOn && expiresOn && startsOn > expiresOn) {
            return "The code would expire before it starts.";
        }
        return null;
    }, [
        editing, code, type, percentOff, amountOff, currency, trialDays,
        isMoney, duration, months, allPlans, planIds, capTotal, maxRedemptions,
        perAccount, startsOn, expiresOn,
    ]);

    const save = useMutation({
        mutationFn: async () => {
            const common = {
                description: description.trim(),
                percent_off: type === "percent" ? parseNum(percentOff) : undefined,
                amount_off: type === "fixed" ? parseNum(amountOff) : undefined,
                currency: type === "fixed" ? currency.trim().toUpperCase() : undefined,
                trial_extension_days:
                    type === "trial_extension" ? parseNum(trialDays) : undefined,
                duration: isMoney ? duration : undefined,
                duration_in_months:
                    isMoney && duration === "repeating" ? parseNum(months) : undefined,
                per_account_limit: parseNum(perAccount),
                applies_to_all_plans: allPlans,
                plan_ids: allPlans ? undefined : planIds,
                status,
            };

            if (editing && existing) {
                // Explicit nulls: an omitted key leaves the column alone, which
                // is not what "uncapped" or "no expiry" means.
                const body: UpdateDiscountCodeRequest = {
                    ...common,
                    max_redemptions: capTotal ? (parseNum(maxRedemptions) ?? null) : null,
                    starts_at: startsOn ? (startOfDayISO(startsOn) ?? null) : null,
                    expires_at: expiresOn ? (endOfDayISO(expiresOn) ?? null) : null,
                };
                return updateDiscount(existing.id, body);
            }

            const body: CreateDiscountCodeRequest = {
                ...common,
                code: code.trim(),
                type,
                max_redemptions: capTotal ? parseNum(maxRedemptions) : undefined,
                starts_at: startOfDayISO(startsOn),
                expires_at: endOfDayISO(expiresOn),
            };
            return createDiscount(body);
        },
        onSuccess: (dc) => {
            qc.invalidateQueries({ queryKey: ["admin", "discounts"] });
            toast.success(editing ? `${dc.code} updated` : `${dc.code} created`);
            onOpenChange(false);
        },
        onError: (e: Error) => toast.error(e.message || "Could not save the code"),
    });

    const planRows = plans.data?.plans ?? [];
    const intervalNote = describeIntervalEffect(shape);

    // `expired` is a real stored status, so a code already in it has to show it
    // rather than being silently rewritten to active by an unrelated edit.
    const statusOptions: { value: DiscountCodeStatus; label: string }[] = [
        { value: "active", label: "Active" },
        { value: "disabled", label: "Disabled" },
        ...(existing?.status === "expired"
            ? [{ value: "expired" as DiscountCodeStatus, label: "Expired" }]
            : []),
    ];

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="flex max-h-[90vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
                <DialogHeader className="shrink-0 px-4 pt-4 pb-3">
                    <DialogTitle className="text-[13px]">
                        {editing ? `Edit ${existing?.code}` : "Create a promo code"}
                    </DialogTitle>
                    <DialogDescription className="text-[11px] leading-relaxed">
                        Customers type this at checkout or on the billing page. Warmbly validates it
                        and mints a matching one-off Stripe coupon per redemption, so there is
                        nothing to create in the Stripe dashboard.
                    </DialogDescription>
                </DialogHeader>

                <div className="min-h-0 flex-1 overflow-y-auto border-t border-border">
                    <Section title="Code">
                        <FieldRow
                            label="Code"
                            htmlFor="discount-code"
                            hint={
                                editing
                                    ? "The code itself cannot change once it exists. Redemptions already point at it."
                                    : "Uppercased on save. This is the literal string a customer types."
                            }
                        >
                            <Input
                                id="discount-code"
                                value={code}
                                disabled={editing}
                                onChange={(e) => setCode(e.target.value.toUpperCase())}
                                placeholder="LAUNCH50"
                                className="h-8 font-mono text-[12.5px] uppercase"
                            />
                        </FieldRow>
                        <FieldRow
                            label="Internal note"
                            htmlFor="discount-note"
                            hint="For the team. Customers never see it."
                        >
                            <Textarea
                                id="discount-note"
                                value={description}
                                onChange={(e) => setDescription(e.target.value)}
                                placeholder="Launch offer, shared in the announcement post"
                                rows={2}
                                className="text-[12.5px]"
                            />
                        </FieldRow>
                    </Section>

                    <Section
                        title="Discount"
                        description={
                            editing
                                ? "The kind of discount is fixed once the code exists; its value can still change."
                                : undefined
                        }
                    >
                        <Segmented
                            value={type}
                            onChange={setType}
                            options={TYPE_OPTIONS}
                            disabled={editing}
                        />
                        <p className="text-[11px] text-muted-foreground">
                            {TYPE_OPTIONS.find((t) => t.value === type)?.hint}
                        </p>

                        {type === "percent" && (
                            <FieldRow label="Percentage off" htmlFor="discount-percent">
                                <div className="relative w-32">
                                    <Input
                                        id="discount-percent"
                                        inputMode="numeric"
                                        value={percentOff}
                                        onChange={(e) => setPercentOff(e.target.value)}
                                        placeholder="50"
                                        className="h-8 pr-7 text-[12.5px] tabular-nums"
                                    />
                                    <span className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-[11px] text-muted-foreground">
                                        %
                                    </span>
                                </div>
                            </FieldRow>
                        )}

                        {type === "fixed" && (
                            <div className="flex gap-3">
                                <FieldRow label="Amount off" htmlFor="discount-amount">
                                    <Input
                                        id="discount-amount"
                                        inputMode="decimal"
                                        value={amountOff}
                                        onChange={(e) => setAmountOff(e.target.value)}
                                        placeholder="25.00"
                                        className="h-8 w-32 text-[12.5px] tabular-nums"
                                    />
                                </FieldRow>
                                <FieldRow
                                    label="Currency"
                                    htmlFor="discount-currency"
                                    hint="A fixed discount only applies to prices in this currency."
                                >
                                    <Input
                                        id="discount-currency"
                                        value={currency}
                                        onChange={(e) => setCurrency(e.target.value.toUpperCase())}
                                        placeholder="USD"
                                        maxLength={3}
                                        className="h-8 w-20 font-mono text-[12.5px] uppercase"
                                    />
                                </FieldRow>
                            </div>
                        )}

                        {type === "trial_extension" && (
                            <FieldRow
                                label="Extra trial days"
                                htmlFor="discount-trial"
                                hint="Added to the subscription at checkout. Nothing is discounted."
                            >
                                <Input
                                    id="discount-trial"
                                    inputMode="numeric"
                                    value={trialDays}
                                    onChange={(e) => setTrialDays(e.target.value)}
                                    placeholder="14"
                                    className="h-8 w-32 text-[12.5px] tabular-nums"
                                />
                            </FieldRow>
                        )}
                    </Section>

                    {isMoney && (
                        <Section title="How long it lasts">
                            <Segmented value={duration} onChange={setDuration} options={DURATION_OPTIONS} />
                            {duration === "repeating" && (
                                <FieldRow label="Number of months" htmlFor="discount-months">
                                    <Input
                                        id="discount-months"
                                        inputMode="numeric"
                                        value={months}
                                        onChange={(e) => setMonths(e.target.value)}
                                        placeholder="3"
                                        className="h-8 w-32 text-[12.5px] tabular-nums"
                                    />
                                </FieldRow>
                            )}
                            {intervalNote && (
                                <div className="flex gap-2 rounded-md border border-sky-200 bg-sky-50 px-2.5 py-2">
                                    <Info className="mt-px size-3.5 shrink-0 text-sky-600" />
                                    <p className="text-[11px] leading-relaxed text-sky-900">{intervalNote}</p>
                                </div>
                            )}
                        </Section>
                    )}

                    <Section title="Eligible plans">
                        <div className="flex items-center justify-between">
                            <div>
                                <div className="text-[12.5px]">Valid on every plan</div>
                                <p className="text-[11px] text-muted-foreground">
                                    Turn off to restrict the code to specific plans.
                                </p>
                            </div>
                            <Switch checked={allPlans} onCheckedChange={setAllPlans} />
                        </div>

                        {!allPlans && (
                            <div className="rounded-md border border-border">
                                {plans.isLoading ? (
                                    <div className="px-2.5 py-2 text-[11px] text-muted-foreground">
                                        Loading plans…
                                    </div>
                                ) : plans.isError ? (
                                    <div className="px-2.5 py-2 text-[11px] text-red-600">
                                        Could not load plans, so this list is not the full set.
                                    </div>
                                ) : planRows.length === 0 ? (
                                    <div className="px-2.5 py-2 text-[11px] text-muted-foreground">
                                        This instance has no plans.
                                    </div>
                                ) : (
                                    <ul className="max-h-44 divide-y divide-border overflow-y-auto">
                                        {planRows.map((p) => {
                                            const checked = planIds.includes(p.id);
                                            return (
                                                <li key={p.id}>
                                                    <label className="flex cursor-pointer items-center gap-2.5 px-2.5 py-2 text-[12.5px] hover:bg-muted/50">
                                                        <input
                                                            type="checkbox"
                                                            checked={checked}
                                                            onChange={() =>
                                                                setPlanIds((prev) =>
                                                                    checked
                                                                        ? prev.filter((id) => id !== p.id)
                                                                        : [...prev, p.id],
                                                                )
                                                            }
                                                            className="size-3.5 accent-[var(--admin-accent)]"
                                                        />
                                                        <span className="flex-1 truncate">
                                                            {p.name ?? p.id}
                                                        </span>
                                                        {p.public === false && (
                                                            <span className="text-[10px] text-muted-foreground">
                                                                private
                                                            </span>
                                                        )}
                                                    </label>
                                                </li>
                                            );
                                        })}
                                    </ul>
                                )}
                            </div>
                        )}
                    </Section>

                    <Section title="Redemption limits">
                        <OptionalCap
                            enabled={capTotal}
                            onEnabledChange={setCapTotal}
                            label="Cap the total number of redemptions"
                            hint="Counted across every workspace on the instance, not per customer. Leave it off for an open launch offer."
                        >
                            <Input
                                inputMode="numeric"
                                value={maxRedemptions}
                                onChange={(e) => setMaxRedemptions(e.target.value)}
                                placeholder="100"
                                className="h-8 w-32 text-[12.5px] tabular-nums"
                            />
                        </OptionalCap>

                        {editing && existing && (
                            <p className="text-[11px] text-muted-foreground">
                                Redeemed {existing.times_redeemed.toLocaleString()} time
                                {existing.times_redeemed === 1 ? "" : "s"} so far. Lowering the cap below
                                that does not undo anything already granted.
                            </p>
                        )}

                        <FieldRow
                            label="Per workspace"
                            htmlFor="discount-per-account"
                            hint="How many times one workspace may redeem it. Pending checkouts count, so an abandoned session does not hand out a second use."
                        >
                            <Input
                                id="discount-per-account"
                                inputMode="numeric"
                                value={perAccount}
                                onChange={(e) => setPerAccount(e.target.value)}
                                className="h-8 w-32 text-[12.5px] tabular-nums"
                            />
                        </FieldRow>
                    </Section>

                    <Section
                        title="Schedule"
                        description="Both are optional. A start in the future makes the code scheduled; an expiry is the end of that day in your timezone."
                    >
                        <div className="flex gap-3">
                            <FieldRow label="Starts" htmlFor="discount-starts">
                                <Input
                                    id="discount-starts"
                                    type="date"
                                    value={startsOn}
                                    max={expiresOn || undefined}
                                    onChange={(e) => setStartsOn(e.target.value)}
                                    className="h-8 text-[12.5px]"
                                />
                            </FieldRow>
                            <FieldRow label="Expires" htmlFor="discount-expires">
                                <Input
                                    id="discount-expires"
                                    type="date"
                                    value={expiresOn}
                                    min={startsOn || undefined}
                                    onChange={(e) => setExpiresOn(e.target.value)}
                                    className="h-8 text-[12.5px]"
                                />
                            </FieldRow>
                        </div>

                        <FieldRow
                            label="Status"
                            hint={
                                status === "expired"
                                    ? "This code is recorded as expired and is refused at checkout. Bringing it back takes both a future expiry above and setting it Active here."
                                    : "A disabled code is refused at checkout without being deleted."
                            }
                        >
                            <Segmented
                                value={status}
                                onChange={setStatus}
                                options={statusOptions}
                            />
                        </FieldRow>
                    </Section>
                </div>

                <DialogFooter className="shrink-0 items-center gap-3 border-t border-border bg-muted/30 px-4 py-3 sm:justify-between">
                    <div className="min-w-0 text-[11px] leading-relaxed">
                        {problem ? (
                            <span className="text-amber-700">{problem}</span>
                        ) : (
                            <span className="text-muted-foreground">
                                <span className="font-mono text-foreground">
                                    {(code || "CODE").toUpperCase()}
                                </span>{" "}
                                gives {describeDiscount(shape)}
                                {allPlans ? " on every plan" : ` on ${planIds.length} plan${planIds.length === 1 ? "" : "s"}`}.
                            </span>
                        )}
                    </div>
                    <div className="flex shrink-0 gap-2">
                        <Button
                            size="sm"
                            variant="outline"
                            className="h-8"
                            onClick={() => onOpenChange(false)}
                            disabled={save.isPending}
                        >
                            Cancel
                        </Button>
                        <Button
                            size="sm"
                            className="h-8"
                            disabled={!!problem || save.isPending}
                            onClick={() => save.mutate()}
                        >
                            {save.isPending ? "Saving…" : editing ? "Save changes" : "Create code"}
                        </Button>
                    </div>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
