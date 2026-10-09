import { useEffect, useState } from "react";
import { motion } from "framer-motion";
import { CheckCircle2Icon, DownloadIcon, HourglassIcon, PlusIcon, RefreshCwIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { CheckSquare } from "@/components/ui/check-square";
import { TextInput } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import useSync from "@/lib/api/hooks/app/emails/useSync";
import useUpdateSyncSkipFolders from "@/lib/api/hooks/app/emails/useUpdateSyncSkipFolders";
import type { SyncFolder, SyncThrottleReason } from "@/lib/api/models/app/emails/SyncState";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

// Sync card in the mailbox drawer: what the initial import has done, whether
// fair use is holding new mail, and when the last pass ran. Copy stays
// concrete (numbers, times) because "syncing..." with no progress is what
// makes a fresh mailbox feel broken.

const REASON_COPY: Record<SyncThrottleReason, string> = {
    burst: "a lot of mail arrived at once",
    hourly: "the hourly limit for this mailbox was reached",
    daily: "the daily limit for this mailbox was reached",
    org_daily: "the workspace's daily limit was reached",
    priority_daily: "the daily limit for replies was reached",
};

function relative(iso: string | Date, now: number): string {
	const diff = now - new Date(iso).getTime();
    const m = Math.round(diff / 60_000);
    if (m < 1) return "just now";
    if (m < 60) return `${m} min ago`;
    const h = Math.round(m / 60);
    if (h < 24) return `${h} h ago`;
    return new Date(iso).toLocaleDateString();
}

function until(iso: string | Date): string {
    const d = new Date(iso);
    const sameDay = d.toDateString() === new Date().toDateString();
    return sameDay
        ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
        : d.toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" });
}

// The folders a client may offer to skip: everything the worker listed
// except INBOX and the special folders, which the sync always follows.
function skippable(folders: SyncFolder[]): string[] {
    return folders.filter((f) => f.folder === "inbox" && f.name.toUpperCase() !== "INBOX").map((f) => f.name);
}

// Folders the owner excluded from sync, with the ones the server lists as
// the choices. A skipped folder leaves the listing once the worker stops
// following it, so the rows are the union of both, and a name the listing
// does not have yet (a folder not yet seen, or one on a mailbox that has not
// synced) can be typed in.
function SkipFoldersSection({ mailboxId, listed, skipped }: { mailboxId: string; listed: string[]; skipped: string[] }) {
    const mutation = useUpdateSyncSkipFolders(mailboxId);
    const confirm = useConfirm();
    const [draft, setDraft] = useState("");

    const isSkipped = (name: string) => skipped.some((s) => s.toLowerCase() === name.toLowerCase());
    const rows = [...listed, ...skipped.filter((s) => !listed.some((l) => l.toLowerCase() === s.toLowerCase()))];

    const save = async (next: string[]) => {
        try {
            await mutation.mutateAsync(next);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };
    // Skipping removes what was imported and nothing brings it back, so it
    // asks first; following a folder again does not.
    const skip = (name: string) =>
        confirm.show(
            `Stop syncing "${name}"? Mail already imported from it is removed from Warmbly. It stays in your mailbox, and is not imported again if you turn the folder back on.`,
            () => save([...skipped, name]),
        );
    const toggle = (name: string) => {
        if (isSkipped(name)) {
            void save(skipped.filter((s) => s.toLowerCase() !== name.toLowerCase()));
        } else {
            skip(name);
        }
    };
    const add = () => {
        const name = draft.trim();
        if (!name) return;
        setDraft("");
        if (isSkipped(name)) return;
        skip(name);
    };

    return (
        <div className="mt-4">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Folders not synced</div>
            <p className="mt-1 text-[11.5px] leading-relaxed text-slate-500">
                Mail in a folder ticked here never reaches Warmbly, and what was already imported from it is removed. Use it
                for a folder another tool fills, such as a second warmup service. Inbox, sent, drafts, spam, trash and archive
                always sync.
            </p>
            {rows.length > 0 && (
                <ul className="mt-2 -mx-2.5">
                    {rows.map((name) => (
                        <li key={name}>
                            <button
                                type="button"
                                disabled={mutation.isPending}
                                onClick={() => void toggle(name)}
                                className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors disabled:opacity-60 rounded-md"
                            >
                                <CheckSquare checked={isSkipped(name)} />
                                <span className="truncate">{name}</span>
                                {!listed.some((l) => l.toLowerCase() === name.toLowerCase()) && (
                                    <span className="ml-auto text-[10.5px] text-slate-400 shrink-0">skipped</span>
                                )}
                            </button>
                        </li>
                    ))}
                </ul>
            )}
            <div className="mt-2 flex items-center gap-1.5">
                <TextInput
                    value={draft}
                    onChange={setDraft}
                    placeholder="Folder name as your mail server lists it"
                    disabled={mutation.isPending}
                    maxLength={255}
                    onKeyDown={(e) => {
                        if (e.key === "Enter") {
                            e.preventDefault();
                            add();
                        }
                    }}
                    className="flex-1"
                />
                <button
                    type="button"
                    onClick={add}
                    disabled={mutation.isPending || !draft.trim()}
                    className="h-7 px-2.5 inline-flex items-center gap-1 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:bg-slate-50 disabled:opacity-50 shrink-0"
                >
                    <PlusIcon className="w-3 h-3" /> Skip
                </button>
            </div>
        </div>
    );
}

export default function SyncStatusCard({ mailboxId, provider }: { mailboxId: string; provider?: string }) {
	const [now, setNow] = useState(Date.now);
	useEffect(() => {
		const timer = setInterval(() => setNow(Date.now()), 60_000);
		return () => clearInterval(timer);
	}, []);
    const sync = useSync(mailboxId);
    const state = sync.data?.state ?? null;
    const policy = sync.data?.policy;

    if (sync.isPending) {
        return (
            <div className="px-5 py-4">
                <div className="h-3 w-24 rounded bg-slate-100 animate-pulse" />
                <div className="mt-3 h-3 w-48 rounded bg-slate-100 animate-pulse" />
            </div>
        );
    }
    if (!sync.data) return null;

    const throttled = !!state?.throttled_until && new Date(state.throttled_until).getTime() > now;
    const lastSuccess = state?.last_synced_at && new Date(state.last_synced_at).getTime();
    // The worker relays successful checks at least every ten minutes.
    const stale = !!lastSuccess && now - lastSuccess > 20 * 60_000;
    const recovering = !!state?.backfill_cursor?.google_recovery;
    const status = state?.backfill_status ?? "pending";
    const cap = policy?.backfill_messages ?? 0;
    const synced = state?.backfill_synced ?? 0;
    const pct = cap > 0 ? Math.min(100, Math.round((synced / cap) * 100)) : 0;

    let headline: React.ReactNode;
    let Icon = RefreshCwIcon;
    let tone = "text-slate-500";
    if (throttled && state?.throttled_until) {
        Icon = HourglassIcon;
        tone = "text-amber-600";
        const reason = state.throttle_reason ? REASON_COPY[state.throttle_reason as SyncThrottleReason] : undefined;
        headline = (
            <>
                Waiting on the sync budget until {until(state.throttled_until)}
                {reason ? <span className="text-slate-500"> ({reason})</span> : null}
            </>
        );
    } else if (recovering) {
        tone = "text-sky-600";
        headline = "Recovering mailbox sync";
    } else if ((state?.deferred ?? 0) > 0) {
        Icon = HourglassIcon;
        tone = "text-amber-600";
        headline = "New mail was waiting on sync at the last check";
    } else if (stale) {
        tone = "text-amber-600";
        headline = "No recent successful sync check";
    } else if (status === "complete") {
        Icon = CheckCircle2Icon;
        headline = "Recent-mail import complete";
    } else if (status === "running") {
        Icon = DownloadIcon;
        tone = "text-sky-600";
        headline = `Importing recent mail: ${synced.toLocaleString()} message${synced === 1 ? "" : "s"} so far`;
    } else {
        Icon = RefreshCwIcon;
        tone = "text-sky-600";
        headline = "Import starts on the next pass";
    }

    return (
        <div className="px-5 py-4">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Sync</div>
            <div className={cn("mt-2 inline-flex items-start gap-1.5 text-[12.5px] font-medium", tone)}>
                <Icon className={cn("w-3.5 h-3.5 mt-0.5 shrink-0", status === "running" && !throttled && "animate-pulse")} />
                <span className="text-slate-900">{headline}</span>
            </div>

            {status === "running" && !throttled && (
                <div className="mt-2.5 h-1.5 w-full rounded-full bg-slate-100 overflow-hidden">
                    <motion.div
                        className="h-full rounded-full bg-sky-500"
                        initial={false}
                        animate={{ width: `${Math.max(pct, 3)}%` }}
                        transition={{ type: "spring", stiffness: 120, damping: 20 }}
                    />
                </div>
            )}

            <p className="mt-2 text-[11.5px] leading-relaxed text-slate-500">
                {status === "complete" && policy
                    ? `Imported ${synced.toLocaleString()} message${synced === 1 ? "" : "s"} from the configured ${policy.backfill_days}-day window. Import completion does not confirm live sync is current.`
                    : policy
                        ? `The last ${policy.backfill_days} days come in newest first, up to ${cap.toLocaleString()} messages. New mail syncs alongside.`
                        : null}
                {throttled ? " Mail is retried automatically within its sync budget." : null}
            </p>

            <p className="mt-1 text-[11.5px] text-slate-500">
                {state?.last_synced_at
                    ? `Last successful sync ${relative(state.last_synced_at, now)}.`
                    : "No successful sync check reported yet."}
                {stale ? " Recovery is not yet confirmed by a recent successful check." : null}
            </p>

            {(state?.deferred ?? 0) > 0 && (
                <p className="mt-1 text-[11.5px] text-amber-700">
                    Last observed backlog: {state!.deferred.toLocaleString()} message{state!.deferred === 1 ? "" : "s"} waiting on the server. The current total may have changed.
                </p>
            )}

            {(state?.folders_skipped_cap ?? 0) > 0 && (
                <p className="mt-1 text-[11.5px] text-amber-700">
                    {state!.folders_skipped_cap!.toLocaleString()} folder{state!.folders_skipped_cap === 1 ? " is" : "s are"} not synced: this mailbox has more
                    folders than Warmbly follows. Your inbox, sent, drafts, archive, spam and trash are always included.
                </p>
            )}

            {(state?.folders_skipped_conflict ?? 0) > 0 && (
                <p className="mt-1 text-[11.5px] text-amber-700">
                    Your mail server listed {state!.folders_skipped_conflict!.toLocaleString()} folder
                    {state!.folders_skipped_conflict === 1 ? " name" : " names"} more than once, so only the first of each is
                    synced. Renaming one of them on your mail server clears this.
                </p>
            )}

            {provider === "smtp_imap" && (
                <SkipFoldersSection
                    mailboxId={mailboxId}
                    listed={skippable(sync.data.folders ?? [])}
                    skipped={sync.data.skip_folders ?? []}
                />
            )}
        </div>
    );
}
