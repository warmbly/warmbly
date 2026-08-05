// Admin outreach composer + audit log. Send platform email from
// noreply@warmbly.com with a configurable Reply-To so customer replies route
// to a real inbox. Every send is recorded in admin_outreach_messages; the log
// below the composer is a faceted, server-paged Explorer.

import { useEffect, useState } from "react";
import {
    keepPreviousData,
    useMutation,
    useQuery,
    useQueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";
import { Send } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
    Explorer,
    FilterGroup,
    SearchFilter,
    SegmentedFilter,
    SelectFilter,
    ToggleFilter,
    DateRangeFilter,
} from "@/components/data/Explorer";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import {
    emptyRange,
    rangeActive,
    rangeWithin,
    rangeAfter,
    rangeBefore,
    type DateRange,
} from "@/lib/dateRange";
import { listOutreach, sendOutreach } from "@/lib/api/client/admin/outreach";
import type {
    AdminOutreachMessage,
    AdminOutreachSearch,
    AdminOutreachStatus,
    SendAdminOutreachRequest,
} from "@/lib/api/models/admin";

const STATUS_TONE: Record<AdminOutreachStatus, string> = {
    queued: "border-amber-300 text-amber-700 bg-amber-50",
    sent: "border-emerald-300 text-emerald-700 bg-emerald-50",
    failed: "border-red-300 text-red-700 bg-red-50",
};

type Mode = "user_id" | "org_id" | "email";

export default function OutreachPage() {
    const qc = useQueryClient();
    const [mode, setMode] = useState<Mode>("email");
    const [target, setTarget] = useState("");
    const [replyTo, setReplyTo] = useState("team@warmbly.com");
    const [subject, setSubject] = useState("");
    const [body, setBody] = useState("");

    // Log facets.
    const [query, setQuery] = useState("");
    const [status, setStatus] = useState<"any" | AdminOutreachStatus>("any");
    const [recipientType, setRecipientType] = useState("");
    const [sentByQ, setSentByQ] = useState("");
    const [hasReplyTo, setHasReplyTo] = useState(false);
    const [hasError, setHasError] = useState(false);
    const [hasUser, setHasUser] = useState(false);
    const [hasOrg, setHasOrg] = useState(false);
    const [created, setCreated] = useState<DateRange>(emptyRange);
    const [sentAt, setSentAt] = useState<DateRange>(emptyRange);
    const [sort, setSort] = useState<{ by: string; desc: boolean }>({ by: "", desc: true });
    const pager = useCursorPager();
    const { reset } = pager;

    const filterKey = JSON.stringify({
        query, status, recipientType, sentByQ, hasReplyTo, hasError, hasUser, hasOrg, created, sentAt, sort,
    });

    useEffect(() => {
        reset();
    }, [filterKey, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "outreach", filterKey, pager.cursor],
        queryFn: () =>
            listOutreach({
                q: query.trim() || undefined,
                status: status === "any" ? undefined : status,
                recipient_type: (recipientType || undefined) as AdminOutreachSearch["recipient_type"],
                sent_by_q: sentByQ.trim() || undefined,
                has_reply_to: hasReplyTo || undefined,
                has_error: hasError || undefined,
                has_user: hasUser || undefined,
                has_org: hasOrg || undefined,
                created_within: rangeWithin(created),
                created_after: rangeAfter(created),
                created_before: rangeBefore(created),
                sent_at_after: rangeAfter(sentAt),
                sent_at_before: rangeBefore(sentAt),
                limit: 50,
                cursor: pager.cursor,
                sort_by: sort.by ? (sort.by as AdminOutreachSearch["sort_by"]) : undefined,
                sort_desc: sort.by ? sort.desc : undefined,
            }),
        staleTime: 30_000,
        refetchInterval: 30_000,
        placeholderData: keepPreviousData,
    });

    const send = useMutation({
        mutationFn: () => {
            const req: SendAdminOutreachRequest = { subject, body };
            if (replyTo.trim()) req.reply_to = replyTo.trim();
            if (mode === "email") req.to_email = target.trim();
            if (mode === "user_id") req.to_user_id = target.trim();
            if (mode === "org_id") req.to_org_id = target.trim();
            return sendOutreach(req);
        },
        onSuccess: () => {
            toast.success("Outreach sent");
            qc.invalidateQueries({ queryKey: ["admin", "outreach"] });
            setSubject("");
            setBody("");
            setTarget("");
        },
        onError: (err: Error) => toast.error(err.message || "Send failed"),
    });

    function submit(e: React.FormEvent) {
        e.preventDefault();
        if (!target.trim()) {
            toast.error("Recipient is required");
            return;
        }
        if (!subject.trim()) {
            toast.error("Subject is required");
            return;
        }
        if (!body.trim()) {
            toast.error("Body is required");
            return;
        }
        send.mutate();
    }

    const rows = data?.data ?? [];

    const bools = [hasReplyTo, hasError, hasUser, hasOrg];
    const activeCount =
        (query ? 1 : 0) +
        (status !== "any" ? 1 : 0) +
        (recipientType ? 1 : 0) +
        (sentByQ ? 1 : 0) +
        bools.filter(Boolean).length +
        [created, sentAt].filter(rangeActive).length +
        (sort.by ? 1 : 0);

    function resetAll() {
        setQuery("");
        setStatus("any");
        setRecipientType("");
        setSentByQ("");
        setHasReplyTo(false);
        setHasError(false);
        setHasUser(false);
        setHasOrg(false);
        setCreated(emptyRange);
        setSentAt(emptyRange);
        setSort({ by: "", desc: true });
    }

    const recipientLabel = (m: AdminOutreachMessage) => {
        if (m.to_user) return m.to_user.email;
        if (m.to_org_id) return "org owner";
        return "raw email";
    };

    const columns: Column<AdminOutreachMessage>[] = [
        {
            id: "when",
            header: "When",
            sortable: true,
            sortKey: "created_at",
            cell: (m) => (
                <span className="text-xs text-muted-foreground whitespace-nowrap">{new Date(m.created_at).toLocaleString()}</span>
            ),
            csv: (m) => m.created_at,
        },
        {
            id: "status",
            header: "Status",
            sortable: true,
            sortKey: "status",
            cell: (m) => (
                <div>
                    <Badge variant="outline" className={`text-[10px] ${STATUS_TONE[m.status]}`}>
                        {m.status}
                    </Badge>
                    {m.error && (
                        <div className="text-[10px] text-red-600 mt-1 max-w-xs truncate" title={m.error}>
                            {m.error}
                        </div>
                    )}
                </div>
            ),
            csv: (m) => m.status,
        },
        {
            id: "to",
            header: "To",
            sortable: true,
            sortKey: "to_email",
            cell: (m) => (
                <div className="text-xs">
                    <div className="font-mono">{m.to_email}</div>
                    {m.reply_to && <div className="text-[10px] text-muted-foreground">reply-to: {m.reply_to}</div>}
                </div>
            ),
            csv: (m) => m.to_email,
        },
        {
            id: "recipient",
            header: "Recipient",
            cell: (m) => <span className="text-xs text-muted-foreground">{recipientLabel(m)}</span>,
            csv: (m) => recipientLabel(m),
        },
        {
            id: "subject",
            header: "Subject",
            sortable: true,
            sortKey: "subject",
            cell: (m) => (
                <span className="text-xs max-w-md truncate block" title={m.subject}>
                    {m.subject}
                </span>
            ),
            csv: (m) => m.subject,
        },
        {
            id: "sender",
            header: "Sender",
            cell: (m) => <span className="text-xs">{m.sent_by_user?.email ?? m.sent_by.slice(0, 8)}</span>,
            csv: (m) => m.sent_by_user?.email ?? m.sent_by,
        },
        {
            id: "delivered",
            header: "Delivered",
            sortable: true,
            sortKey: "sent_at",
            defaultHidden: true,
            cell: (m) => (
                <span className="text-xs text-muted-foreground whitespace-nowrap">
                    {m.sent_at ? new Date(m.sent_at).toLocaleString() : "—"}
                </span>
            ),
            csv: (m) => m.sent_at ?? "",
        },
    ];

    return (
        <div>
            <PageHeader
                title="Outreach"
                description="Send platform email from the Warmbly noreply address with a configurable Reply-To. Every message is audit-logged below."
            />

            <section className="border border-border rounded-lg bg-card p-4 mb-6">
                <form onSubmit={submit} className="space-y-3">
                    <div>
                        <Label className="text-xs font-medium">Recipient</Label>
                        <div className="flex items-center gap-2 mt-1">
                            <select
                                value={mode}
                                onChange={(e) => setMode(e.target.value as Mode)}
                                className="text-sm px-2 py-1.5 rounded-md border border-border bg-background"
                            >
                                <option value="email">Email address</option>
                                <option value="user_id">User ID</option>
                                <option value="org_id">Org ID (sends to owner)</option>
                            </select>
                            <Input
                                value={target}
                                onChange={(e) => setTarget(e.target.value)}
                                placeholder={mode === "email" ? "support-customer@example.com" : "uuid"}
                                className="font-mono text-sm"
                            />
                        </div>
                    </div>

                    <div>
                        <Label htmlFor="reply_to" className="text-xs font-medium">
                            Reply-To
                        </Label>
                        <Input
                            id="reply_to"
                            value={replyTo}
                            onChange={(e) => setReplyTo(e.target.value)}
                            placeholder="team@warmbly.com (replies will land here)"
                            className="font-mono text-sm"
                        />
                        <p className="text-[10px] text-muted-foreground mt-0.5">
                            From: defaults to the platform noreply address. Leave blank to
                            also have replies bounce against noreply.
                        </p>
                    </div>

                    <div>
                        <Label htmlFor="subject" className="text-xs font-medium">
                            Subject
                        </Label>
                        <Input
                            id="subject"
                            value={subject}
                            onChange={(e) => setSubject(e.target.value)}
                            placeholder="One-line subject"
                        />
                    </div>

                    <div>
                        <Label htmlFor="body" className="text-xs font-medium">
                            Body (HTML)
                        </Label>
                        <textarea
                            id="body"
                            value={body}
                            onChange={(e) => setBody(e.target.value)}
                            rows={10}
                            className="mt-1 block w-full rounded-md border border-border bg-background px-3 py-2 text-sm font-mono"
                            placeholder="<p>Hi…</p>"
                        />
                    </div>

                    <Button
                        type="submit"
                        disabled={send.isPending}
                        className="bg-[var(--admin-accent)] hover:bg-[var(--admin-accent-strong)] text-white"
                    >
                        <Send className="size-3.5" />
                        {send.isPending ? "Sending…" : "Send"}
                    </Button>
                </form>
            </section>

            <h2 className="text-sm font-semibold mb-2">Outreach log</h2>
            <Explorer
                activeCount={activeCount}
                onReset={resetAll}
                filters={
                    <>
                        <FilterGroup label="Search">
                            <SearchFilter value={query} onChange={setQuery} placeholder="Email, subject, or reply-to…" />
                        </FilterGroup>
                        <FilterGroup label="Status">
                            <SegmentedFilter
                                value={status}
                                onChange={setStatus}
                                options={[
                                    { value: "any", label: "All" },
                                    { value: "queued", label: "Queued" },
                                    { value: "sent", label: "Sent" },
                                    { value: "failed", label: "Failed" },
                                ]}
                            />
                        </FilterGroup>
                        <FilterGroup label="Recipient type">
                            <SelectFilter
                                value={recipientType || "any"}
                                onChange={(v) => setRecipientType(v === "any" ? "" : v)}
                                options={[
                                    { value: "any", label: "Any recipient" },
                                    { value: "user", label: "User" },
                                    { value: "org", label: "Org owner" },
                                    { value: "email", label: "Raw email" },
                                ]}
                                placeholder="Any recipient"
                            />
                        </FilterGroup>
                        <FilterGroup label="Sender">
                            <SearchFilter value={sentByQ} onChange={setSentByQ} placeholder="Admin name or email…" />
                        </FilterGroup>
                        <FilterGroup label="Sent">
                            <DateRangeFilter value={created} onChange={setCreated} />
                        </FilterGroup>
                        <FilterGroup label="Delivered">
                            <DateRangeFilter value={sentAt} onChange={setSentAt} mode="custom" />
                        </FilterGroup>
                        <FilterGroup label="Flags">
                            <div className="flex flex-col gap-2">
                                <ToggleFilter checked={hasError} onChange={setHasError} label="Has error" />
                                <ToggleFilter checked={hasReplyTo} onChange={setHasReplyTo} label="Has reply-to" />
                                <ToggleFilter checked={hasUser} onChange={setHasUser} label="Linked to user" />
                                <ToggleFilter checked={hasOrg} onChange={setHasOrg} label="Linked to org" />
                            </div>
                        </FilterGroup>
                    </>
                }
            >
                <DataTable
                    columns={columns}
                    rows={rows}
                    getRowId={(m) => m.id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load outreach log"
                    sort={sort.by ? sort : undefined}
                    onSortChange={setSort}
                    storageKey="admin.outreach"
                    csvName="warmbly-outreach"
                    noun="messages"
                    emptyTitle="No outreach"
                    emptyHint="No messages match these filters."
                    pager={{
                        canPrev: pager.canPrev,
                        canNext: !!data?.pagination?.has_more,
                        onPrev: pager.prev,
                        onNext: () => pager.next(data?.pagination?.next_cursor),
                        page: pager.page,
                        shown: rows.length,
                        total: data?.pagination?.total ?? null,
                    }}
                />
            </Explorer>
        </div>
    );
}
