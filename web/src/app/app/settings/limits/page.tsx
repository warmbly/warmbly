// Customer-facing limit-increase request flow. Lists past requests
// and offers a form to submit a new one. Approval surfaces as the
// effective limit going up on the org; rejection surfaces with the
// admin's notes attached to the row.

import { useMemo, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import toast from "react-hot-toast";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { NumberInput } from "@/components/ui/field";
import { Section, SectionShell } from "../_components/SectionShell";
import getCurrentOrganization from "@/lib/api/client/app/organizations/getCurrentOrganization";
import listLimitRequests from "@/lib/api/client/app/organizations/listLimitRequests";
import submitLimitRequest from "@/lib/api/client/app/organizations/submitLimitRequest";
import cancelLimitRequest from "@/lib/api/client/app/organizations/cancelLimitRequest";
import type {
    LimitField,
    LimitRequestStatus,
} from "@/lib/api/models/app/organizations/LimitIncreaseRequest";

const FIELD_OPTIONS: { value: LimitField; label: string; hint: string }[] = [
    { value: "max_email_accounts", label: "Mailboxes", hint: "More connected sending mailboxes" },
    { value: "max_campaigns", label: "Campaigns (lifetime)", hint: "Higher cap on total campaigns created" },
    { value: "max_active_campaigns", label: "Active campaigns", hint: "More campaigns running at the same time" },
    { value: "max_team_members", label: "Team members", hint: "More seats on this workspace" },
    { value: "max_contacts", label: "Contacts", hint: "Store more recipient records" },
    { value: "daily_campaign_limit", label: "Daily sends", hint: "Send more campaign emails per day" },
];

const STATUS_TONE: Record<LimitRequestStatus, string> = {
    pending: "bg-amber-50 text-amber-700 border-amber-200",
    approved: "bg-emerald-50 text-emerald-700 border-emerald-200",
    rejected: "bg-red-50 text-red-700 border-red-200",
    cancelled: "bg-slate-50 text-slate-600 border-slate-200",
};

export default function LimitsSettingsPage() {
    const qc = useQueryClient();

    const orgQuery = useQuery({
        queryKey: ["app", "organizations", "current"],
        queryFn: getCurrentOrganization,
    });
    const orgId = orgQuery.data?.id;

    const requestsQuery = useQuery({
        queryKey: ["app", "organizations", orgId, "limit-requests"],
        queryFn: () => listLimitRequests(orgId!),
        enabled: !!orgId,
    });

    const [field, setField] = useState<LimitField>("max_email_accounts");
    const [requested, setRequested] = useState<number>(Number.NaN);
    const [reason, setReason] = useState<string>("");

    const fieldSelectOptions = useMemo<SelectOption[]>(
        () => FIELD_OPTIONS.map((opt) => ({ value: opt.value, label: opt.label })),
        [],
    );

    const submit = useMutation({
        mutationFn: () =>
            submitLimitRequest(orgId!, {
                field,
                requested,
                reason,
            }),
        onSuccess: () => {
            toast.success("Request submitted — an admin will review shortly.");
            qc.invalidateQueries({ queryKey: ["app", "organizations", orgId, "limit-requests"] });
            setRequested(Number.NaN);
            setReason("");
        },
        onError: (err: Error) => {
            toast.error(err.message || "Could not submit — please try again.");
        },
    });

    const cancel = useMutation({
        mutationFn: (id: string) => cancelLimitRequest(id),
        onSuccess: () => {
            toast.success("Request cancelled");
            qc.invalidateQueries({ queryKey: ["app", "organizations", orgId, "limit-requests"] });
        },
        onError: (err: Error) => {
            toast.error(err.message || "Cancel failed");
        },
    });

    const rows = requestsQuery.data?.data ?? [];

    function onSubmit(e: React.FormEvent) {
        e.preventDefault();
        const n = requested;
        if (!Number.isInteger(n) || n <= 0) {
            toast.error("Requested value must be a positive integer");
            return;
        }
        if (reason.trim().length < 10) {
            toast.error("Please include a reason (at least a sentence)");
            return;
        }
        submit.mutate();
    }

    return (
        <SectionShell
            title="Limits"
            description="Ask for more capacity than your plan or our product-level cap allows. Increases are reviewed and may be refused per our terms of service."
        >
            <Section
                eyebrow="Request an increase"
                description="Tell us what you need and why. We aim to respond within one business day."
            >
                <form onSubmit={onSubmit} className="space-y-3">
                    <div>
                        <label className="text-[12px] font-medium text-slate-700">Resource</label>
                        <SelectMenu
                            value={field}
                            onChange={(v) => setField(v as LimitField)}
                            options={fieldSelectOptions}
                            className="mt-1 w-full"
                            aria-label="Resource"
                        />
                        <p className="text-[11px] text-slate-500 mt-1">
                            {FIELD_OPTIONS.find((o) => o.value === field)?.hint}
                        </p>
                    </div>
                    <div>
                        <label className="text-[12px] font-medium text-slate-700">
                            Requested value
                        </label>
                        <NumberInput
                            min={1}
                            value={requested}
                            onChange={setRequested}
                            className="mt-1 flex w-full"
                            placeholder="e.g. 50"
                        />
                    </div>
                    <div>
                        <label className="text-[12px] font-medium text-slate-700">Reason</label>
                        <textarea
                            value={reason}
                            onChange={(e) => setReason(e.target.value)}
                            rows={3}
                            className="mt-1 block w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm"
                            placeholder="Why does this matter for your team? Volume, customer commitments, ramp plans, etc."
                        />
                    </div>
                    <div className="flex items-center gap-3">
                        <button
                            type="submit"
                            disabled={submit.isPending || !orgId}
                            className="rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50"
                        >
                            {submit.isPending ? "Submitting…" : "Submit request"}
                        </button>
                        <p className="text-[11px] text-slate-500">
                            Subject to review per our{" "}
                            <a
                                href="https://warmbly.com/terms"
                                target="_blank"
                                rel="noreferrer"
                                className="underline"
                            >
                                terms of service
                            </a>
                            .
                        </p>
                    </div>
                </form>
            </Section>

            <Section eyebrow="Your requests" description="Pending, approved, and historical decisions.">
                {requestsQuery.isLoading ? (
                    <p className="text-[12px] text-slate-500">Loading…</p>
                ) : rows.length === 0 ? (
                    <p className="text-[12px] text-slate-500">No requests yet.</p>
                ) : (
                    <ul className="space-y-2">
                        {rows.map((r) => {
                            const fieldLabel =
                                FIELD_OPTIONS.find((o) => o.value === r.field)?.label ?? r.field;
                            return (
                                <li
                                    key={r.id}
                                    className="rounded-md border border-slate-200 p-3 bg-white"
                                >
                                    <div className="flex items-start justify-between gap-3">
                                        <div className="min-w-0">
                                            <div className="text-sm font-medium">
                                                {fieldLabel}: {r.current_effective.toLocaleString()}
                                                {" → "}
                                                {r.requested.toLocaleString()}
                                            </div>
                                            <div className="text-[11px] text-slate-500 mt-1 break-words">
                                                {new Date(r.submitted_at).toLocaleDateString()} · "{r.reason}"
                                            </div>
                                            {r.review_notes && r.status !== "pending" && (
                                                <div className="text-[11px] text-slate-600 mt-1 italic break-words">
                                                    Reviewer: "{r.review_notes}"
                                                </div>
                                            )}
                                        </div>
                                        <div className="flex flex-col items-end gap-1.5 shrink-0">
                                            <span
                                                className={`text-[10px] px-1.5 py-0.5 rounded border ${STATUS_TONE[r.status]}`}
                                            >
                                                {r.status}
                                            </span>
                                            {r.status === "pending" && (
                                                <button
                                                    onClick={() => cancel.mutate(r.id)}
                                                    disabled={cancel.isPending}
                                                    className="text-[11px] text-slate-500 hover:text-slate-800 underline"
                                                >
                                                    Cancel
                                                </button>
                                            )}
                                        </div>
                                    </div>
                                </li>
                            );
                        })}
                    </ul>
                )}
            </Section>
        </SectionShell>
    );
}
