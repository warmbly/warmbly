// /placement — seed inbox-placement testing.
//
// Send a tokenized copy of a real template through a real sender to the panel
// of Warmbly-controlled SEED mailboxes, then watch where each landed (Inbox /
// Spam / Promotions / other), rolled up per provider. A backend poller fills in
// results as the probes sync into each seed's inbox, so a test starts "pending"
// and resolves over the next few minutes.

import { useMemo, useState } from "react";
import {
    keepPreviousData,
    useMutation,
    useQuery,
    useQueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";
import { Inbox, Send, Sparkles, Tag, TriangleAlert } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/ErrorState";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import {
    createPlacementTest,
    getPlacementTest,
    listPlacementTests,
    listSeedCandidates,
    listSeedMailboxes,
    setSeedMailbox,
    type PlacementFolder,
    type PlacementTestRow,
    type SeedAccount,
} from "@/lib/api/client/admin/placement";
import { fmtDate } from "./warmup-content/shared";

const STATUS_TONE: Record<string, string> = {
    pending: "border-amber-300 bg-amber-50 text-amber-700",
    completed: "border-emerald-300 bg-emerald-50 text-emerald-700",
};

const FOLDER_TONE: Record<PlacementFolder, string> = {
    inbox: "border-emerald-300 bg-emerald-50 text-emerald-700",
    promotions: "border-sky-300 bg-sky-50 text-sky-700",
    spam: "border-red-300 bg-red-50 text-red-700",
    other: "border-zinc-300 bg-zinc-50 text-zinc-600",
    pending: "border-amber-300 bg-amber-50 text-amber-700",
};

const PROVIDER_LABEL: Record<string, string> = {
    gmail: "Gmail / Workspace",
    outlook: "Outlook / M365",
    smtp_imap: "SMTP / IMAP",
    unknown: "Unknown",
};

function providerLabel(p: string): string {
    return PROVIDER_LABEL[p] ?? p;
}

export default function PlacementPage() {
    return (
        <div>
            <PageHeader
                title="Inbox placement"
                description="Send a tokenized copy of a template through a real sender to the seed panel, then classify where it landed per provider. Results fill in as the probes sync into each seed's inbox."
            />

            <OperationalNotice />

            <section className="mt-6">
                <h2 className="text-sm font-semibold mb-2">Run a placement test</h2>
                <CreateTestForm />
            </section>

            <section className="mt-8">
                <h2 className="text-sm font-semibold mb-2">Tests</h2>
                <TestsTable />
            </section>

            <section className="mt-8">
                <h2 className="text-sm font-semibold mb-2">Seed mailboxes</h2>
                <SeedsSection />
            </section>
        </div>
    );
}

function OperationalNotice() {
    return (
        <div className="mt-4 flex gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2.5 text-[12.5px] text-amber-800">
            <TriangleAlert className="size-4 shrink-0 mt-0.5" />
            <div>
                Placement classification only works for mailboxes Warmbly owns and
                syncs. Connect real seed mailboxes across providers (Gmail/Workspace,
                Outlook/M365, Yahoo, AOL, iCloud, corporate) and flag them below.
                Gmail Promotions-tab detection additionally needs category-label sync
                (a worker follow-up); until then a Gmail tab reads as Inbox.
            </div>
        </div>
    );
}

// --- Create -------------------------------------------------------------

function CreateTestForm() {
    const qc = useQueryClient();
    const [senderId, setSenderId] = useState("");
    const [subject, setSubject] = useState("");
    const [bodyPlain, setBodyPlain] = useState("");

    const create = useMutation({
        mutationFn: () =>
            createPlacementTest({
                sender_account_id: senderId.trim(),
                subject: subject.trim(),
                body_plain: bodyPlain,
                body_html: "",
            }),
        onSuccess: () => {
            toast.success("Placement test sent to the seed panel");
            setSubject("");
            setBodyPlain("");
            qc.invalidateQueries({ queryKey: ["admin", "placement", "tests"] });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to send test"),
    });

    const canSubmit =
        senderId.trim() !== "" && subject.trim() !== "" && bodyPlain.trim() !== "";

    return (
        <div className="rounded-md border border-zinc-200 bg-white p-4 space-y-3">
            <div className="grid gap-3 md:grid-cols-2">
                <div className="space-y-1.5">
                    <Label htmlFor="pl-sender">Sender account ID</Label>
                    <Input
                        id="pl-sender"
                        placeholder="email_account UUID to send from"
                        value={senderId}
                        onChange={(e) => setSenderId(e.target.value)}
                    />
                    <p className="text-[11px] text-muted-foreground">
                        The real mailbox whose deliverability you are testing. Copy its
                        ID from the Mailboxes explorer.
                    </p>
                </div>
                <div className="space-y-1.5">
                    <Label htmlFor="pl-subject">Subject</Label>
                    <Input
                        id="pl-subject"
                        placeholder="Template subject line"
                        value={subject}
                        onChange={(e) => setSubject(e.target.value)}
                    />
                    <p className="text-[11px] text-muted-foreground">
                        A hidden placement token is appended so we can find the copy in
                        each seed inbox.
                    </p>
                </div>
            </div>
            <div className="space-y-1.5">
                <Label htmlFor="pl-body">Body (plain text)</Label>
                <Textarea
                    id="pl-body"
                    rows={6}
                    placeholder="The template body to test."
                    value={bodyPlain}
                    onChange={(e) => setBodyPlain(e.target.value)}
                />
            </div>
            <div className="flex justify-end">
                <Button
                    onClick={() => create.mutate()}
                    disabled={!canSubmit || create.isPending}
                >
                    <Send className="size-4" />
                    {create.isPending ? "Sending…" : "Send placement test"}
                </Button>
            </div>
        </div>
    );
}

// --- Tests --------------------------------------------------------------

function TestsTable() {
    const pager = useCursorPager();
    const [openId, setOpenId] = useState<string | null>(null);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "placement", "tests", pager.cursor],
        queryFn: () => listPlacementTests({ cursor: pager.cursor, limit: 25 }),
        staleTime: 15_000,
        refetchInterval: 30_000,
        placeholderData: keepPreviousData,
    });

    const rows = data?.data ?? [];

    const columns: Column<PlacementTestRow>[] = useMemo(
        () => [
            {
                id: "subject",
                header: "Subject",
                cell: (r) => (
                    <span className="font-medium text-zinc-800">{r.subject}</span>
                ),
            },
            {
                id: "status",
                header: "Status",
                cell: (r) => (
                    <Badge
                        variant="outline"
                        className={STATUS_TONE[r.status] ?? FOLDER_TONE.other}
                    >
                        {r.status}
                    </Badge>
                ),
            },
            {
                id: "created_at",
                header: "Created",
                cell: (r) => (
                    <span className="text-zinc-500">{fmtDate(r.created_at)}</span>
                ),
            },
            {
                id: "finished_at",
                header: "Finished",
                cell: (r) => (
                    <span className="text-zinc-500">
                        {r.finished_at ? fmtDate(r.finished_at) : "—"}
                    </span>
                ),
            },
        ],
        [],
    );

    return (
        <>
            <DataTable<PlacementTestRow>
                columns={columns}
                rows={rows}
                getRowId={(r) => r.id}
                loading={isLoading}
                error={error}
                onRetry={() => refetch()}
                onRowClick={(r) => setOpenId(r.id)}
                emptyTitle="No placement tests yet"
                emptyHint="Send one above to see where it lands."
                noun="tests"
                pager={{
                    canPrev: pager.canPrev,
                    canNext: !!data?.pagination.has_more,
                    onPrev: pager.prev,
                    onNext: () => pager.next(data?.pagination.next_cursor ?? null),
                    page: pager.page,
                    shown: rows.length,
                    total: data?.pagination.total,
                }}
            />
            <TestDetailDialog
                id={openId}
                onClose={() => setOpenId(null)}
            />
        </>
    );
}

function TestDetailDialog({
    id,
    onClose,
}: {
    id: string | null;
    onClose: () => void;
}) {
    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "placement", "test", id],
        queryFn: () => getPlacementTest(id as string),
        enabled: !!id,
        refetchInterval: 15_000,
    });

    const detail = data?.data;

    return (
        <Dialog open={!!id} onOpenChange={(o) => !o && onClose()}>
            <DialogContent className="max-w-2xl">
                <DialogHeader>
                    <DialogTitle>Placement result</DialogTitle>
                    <DialogDescription>
                        {detail?.test.subject ?? "Per-provider folder rollup."}
                    </DialogDescription>
                </DialogHeader>

                {isLoading && <Skeleton className="h-40" />}
                {error && (
                    <ErrorState
                        error={error}
                        title="Failed to load test"
                        onRetry={() => refetch()}
                    />
                )}

                {detail && (
                    <div className="space-y-5">
                        <div className="space-y-2">
                            <div className="text-[10px] uppercase tracking-wider text-muted-foreground">
                                Per-provider rollup
                            </div>
                            {detail.rollup.length === 0 ? (
                                <p className="text-[12.5px] text-zinc-500">
                                    No seeds resolved yet.
                                </p>
                            ) : (
                                <div className="space-y-2">
                                    {detail.rollup.map((r) => (
                                        <div
                                            key={r.provider}
                                            className="rounded-md border border-zinc-200 p-3"
                                        >
                                            <div className="mb-2 flex items-center gap-1.5 text-[12.5px] font-medium text-zinc-800">
                                                <Sparkles className="size-3.5 text-zinc-400" />
                                                {providerLabel(r.provider)}
                                                <span className="text-zinc-400 font-normal">
                                                    · {r.total} seed{r.total === 1 ? "" : "s"}
                                                </span>
                                            </div>
                                            <div className="flex flex-wrap gap-1.5">
                                                <Count tone={FOLDER_TONE.inbox} icon={<Inbox className="size-3" />} label="Inbox" n={r.inbox} />
                                                <Count tone={FOLDER_TONE.promotions} icon={<Tag className="size-3" />} label="Promotions" n={r.promotions} />
                                                <Count tone={FOLDER_TONE.spam} icon={<TriangleAlert className="size-3" />} label="Spam" n={r.spam} />
                                                <Count tone={FOLDER_TONE.other} label="Other" n={r.other} />
                                                <Count tone={FOLDER_TONE.pending} label="Pending" n={r.pending} />
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>

                        <div className="space-y-2">
                            <div className="text-[10px] uppercase tracking-wider text-muted-foreground">
                                Per-seed detail
                            </div>
                            <div className="overflow-hidden rounded-md border border-zinc-200">
                                <table className="w-full text-[12.5px]">
                                    <thead className="bg-zinc-50 text-left text-zinc-500">
                                        <tr>
                                            <th className="px-3 py-1.5 font-medium">Seed</th>
                                            <th className="px-3 py-1.5 font-medium">Provider</th>
                                            <th className="px-3 py-1.5 font-medium">Folder</th>
                                            <th className="px-3 py-1.5 font-medium">Detected</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {detail.results.map((res) => (
                                            <tr
                                                key={res.seed_account_id}
                                                className="border-t border-zinc-100"
                                            >
                                                <td className="px-3 py-1.5 font-mono text-[11px] text-zinc-600">
                                                    {res.seed_account_id.slice(0, 8)}
                                                </td>
                                                <td className="px-3 py-1.5 text-zinc-600">
                                                    {providerLabel(res.provider || "unknown")}
                                                </td>
                                                <td className="px-3 py-1.5">
                                                    <Badge
                                                        variant="outline"
                                                        className={FOLDER_TONE[res.folder]}
                                                    >
                                                        {res.folder}
                                                    </Badge>
                                                </td>
                                                <td className="px-3 py-1.5 text-zinc-500">
                                                    {res.detected_at
                                                        ? fmtDate(res.detected_at)
                                                        : "—"}
                                                </td>
                                            </tr>
                                        ))}
                                    </tbody>
                                </table>
                            </div>
                        </div>
                    </div>
                )}
            </DialogContent>
        </Dialog>
    );
}

function Count({
    tone,
    icon,
    label,
    n,
}: {
    tone: string;
    icon?: React.ReactNode;
    label: string;
    n: number;
}) {
    return (
        <Badge variant="outline" className={tone}>
            {icon}
            <span className="ml-0.5">
                {label} {n}
            </span>
        </Badge>
    );
}

// --- Seeds --------------------------------------------------------------

function SeedsSection() {
    const qc = useQueryClient();
    const [search, setSearch] = useState("");

    const seeds = useQuery({
        queryKey: ["admin", "placement", "seeds"],
        queryFn: () => listSeedMailboxes(),
        staleTime: 15_000,
    });

    const candidates = useQuery({
        queryKey: ["admin", "placement", "seed-candidates", search],
        queryFn: () => listSeedCandidates(search || undefined),
        enabled: search.trim().length >= 2,
        placeholderData: keepPreviousData,
    });

    const toggle = useMutation({
        mutationFn: ({ id, isSeed }: { id: string; isSeed: boolean }) =>
            setSeedMailbox(id, isSeed),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["admin", "placement", "seeds"] });
            qc.invalidateQueries({
                queryKey: ["admin", "placement", "seed-candidates"],
            });
        },
        onError: (err: Error) => toast.error(err.message || "Failed to update seed"),
    });

    return (
        <div className="grid gap-4 lg:grid-cols-2">
            <div className="rounded-md border border-zinc-200 bg-white p-4">
                <div className="mb-2 text-[12.5px] font-medium text-zinc-800">
                    Active seed panel
                </div>
                {seeds.isLoading && <Skeleton className="h-24" />}
                {seeds.error && (
                    <ErrorState
                        error={seeds.error}
                        title="Failed to load seeds"
                        onRetry={() => seeds.refetch()}
                    />
                )}
                {seeds.data && seeds.data.data.length === 0 && (
                    <p className="text-[12.5px] text-zinc-500">
                        No seed mailboxes yet. Search on the right to flag one.
                    </p>
                )}
                <div className="space-y-1.5">
                    {seeds.data?.data.map((s) => (
                        <SeedRow
                            key={s.id}
                            seed={s}
                            disabled={toggle.isPending}
                            onToggle={(isSeed) => toggle.mutate({ id: s.id, isSeed })}
                        />
                    ))}
                </div>
            </div>

            <div className="rounded-md border border-zinc-200 bg-white p-4">
                <div className="mb-2 text-[12.5px] font-medium text-zinc-800">
                    Add a seed
                </div>
                <Input
                    placeholder="Search connected mailboxes by email…"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                />
                <p className="mt-1 mb-2 text-[11px] text-muted-foreground">
                    Type at least 2 characters. Flagging a mailbox as a seed makes it a
                    placement recipient — it should be a mailbox Warmbly owns and syncs.
                </p>
                {candidates.isLoading && search.trim().length >= 2 && (
                    <Skeleton className="h-20" />
                )}
                <div className="space-y-1.5">
                    {candidates.data?.data.map((s) => (
                        <SeedRow
                            key={s.id}
                            seed={s}
                            disabled={toggle.isPending}
                            onToggle={(isSeed) => toggle.mutate({ id: s.id, isSeed })}
                        />
                    ))}
                </div>
            </div>
        </div>
    );
}

function SeedRow({
    seed,
    disabled,
    onToggle,
}: {
    seed: SeedAccount;
    disabled: boolean;
    onToggle: (isSeed: boolean) => void;
}) {
    return (
        <div className="flex items-center justify-between gap-2 rounded-md border border-zinc-100 px-2.5 py-1.5">
            <div className="min-w-0">
                <div className="truncate text-[12.5px] text-zinc-800">{seed.email}</div>
                <div className="flex items-center gap-1.5 text-[11px] text-zinc-500">
                    <span>{providerLabel(seed.provider)}</span>
                    <span>·</span>
                    <span>{seed.status}</span>
                </div>
            </div>
            <Switch
                checked={seed.is_seed}
                disabled={disabled}
                onCheckedChange={(v) => onToggle(v)}
            />
        </div>
    );
}
