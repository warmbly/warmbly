// MailboxImportsMenu: the mailboxes page's way back into a running or recent
// import. Hidden until the workspace has one. Each entry says what the import
// is doing or waiting on, and can be hidden from the list.
import React from "react";
import toast from "react-hot-toast";
import { FileSpreadsheetIcon, Loader2Icon, XIcon } from "lucide-react";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuLabel,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import useMailboxImports from "@/lib/api/hooks/app/emails/useMailboxImports";
import { useDismissMailboxImport } from "@/lib/api/hooks/app/emails/useMailboxImportActions";
import { type MailboxImport } from "@/lib/api/models/app/emails/MailboxImport";
import { vendorLabel } from "@/lib/api/models/app/emails/MailboxSources";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { cn } from "@/lib/utils";
import { VENDOR_AUTHORIZING, importSourceName, plural } from "./importFields";
import MailboxImportDialog from "./MailboxImportDialog";
import ProviderLogo from "@/components/app/emails/ProviderLogo";

type Tone = "working" | "waiting" | "action" | "error" | "done" | "muted";

interface JobState {
    text: string;
    hint?: string;
    tone: Tone;
}

// jobState says, in words, what an import is doing now and what it waits on.
function jobState(job: MailboxImport): JobState {
    const c = job.counts;
    const authorizing = job.causes?.find((x) => x.cause === VENDOR_AUTHORIZING)?.count ?? 0;
    const signin = Math.max(0, c.needs_signin - authorizing);
    const inFlight = c.queued + c.running;
    const settled = job.total - inFlight;

    if (job.status === "cancelled") return { text: "Stopped", tone: "muted" };
    if (job.status === "running" && inFlight > 0) {
        return {
            text: `Connecting ${settled.toLocaleString()} of ${job.total.toLocaleString()}`,
            hint: "Each mailbox is checked against its mail server, a few seconds each.",
            tone: "working",
        };
    }
    // What needs the person comes first; an authorization still running is mentioned alongside.
    const alsoAuthorizing = authorizing > 0 ? ` ${plural(authorizing, "more is", "more are")} still being authorized.` : "";
    if (c.failed > 0) {
        return { text: `${c.failed.toLocaleString()} failed`, hint: `Open to see why and retry.${alsoAuthorizing}`, tone: "error" };
    }
    if (signin > 0) {
        return {
            text: `${plural(signin, "mailbox needs", "mailboxes need")} sign-in`,
            hint: `Open to sign in to each one.${alsoAuthorizing}`,
            tone: "action",
        };
    }
    if (authorizing > 0) {
        return {
            text: `Authorizing ${plural(authorizing, "mailbox", "mailboxes")}`,
            hint: `${vendorLabel(job.vendor) || "The inbox vendor"} is approving Warmbly. This can take up to an hour and they connect on their own; open it to connect them sooner.`,
            tone: "waiting",
        };
    }
    const ok = c.connected + c.updated;
    return { text: ok > 0 ? `${ok.toLocaleString()} connected` : "Done", tone: "done" };
}

const TONE: Record<Tone, string> = {
    working: "text-sky-700",
    waiting: "text-sky-700",
    action: "text-amber-700",
    error: "text-red-600",
    done: "text-emerald-700",
    muted: "text-slate-500",
};

export default function MailboxImportsMenu() {
    const imports = useMailboxImports();
    const [openId, setOpenId] = React.useState<string | null>(null);
    const [menuOpen, setMenuOpen] = React.useState(false);
    const jobs = imports.data?.data ?? [];
    const running = jobs.filter((j) => j.status === "running").length;
    const needsYou = jobs.filter((j) => {
        const t = jobState(j).tone;
        return t === "action" || t === "error";
    }).length;

    return (
        <>
            {jobs.length > 0 && (
                <PopoverMenu align="end" open={menuOpen} onOpenChange={setMenuOpen}>
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            className="h-7 px-2.5 rounded-md inline-flex items-center gap-1.5 text-[12px] font-medium transition-colors border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 bg-white"
                        >
                            {running > 0 ? (
                                <Loader2Icon className="w-3 h-3 text-sky-600 animate-spin" />
                            ) : (
                                <FileSpreadsheetIcon className="w-3 h-3" />
                            )}
                            Imports
                            {running > 0 && <span className="text-sky-600 tabular-nums">{running}</span>}
                            {needsYou > 0 && (
                                <span
                                    className="min-w-4 h-4 px-1 rounded-full bg-amber-100 text-amber-800 text-[10.5px] tabular-nums inline-flex items-center justify-center"
                                    title={`${plural(needsYou, "import needs", "imports need")} you`}
                                >
                                    {needsYou}
                                </span>
                            )}
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={340}>
                        <PopoverMenuLabel>Recent imports</PopoverMenuLabel>
                        <div className="max-h-[60vh] overflow-y-auto">
                            {jobs.map((job) => (
                                <ImportEntry
                                    key={job.id}
                                    job={job}
                                    onOpen={() => {
                                        setMenuOpen(false);
                                        setOpenId(job.id);
                                    }}
                                />
                            ))}
                        </div>
                    </PopoverMenuContent>
                </PopoverMenu>
            )}
            <MailboxImportDialog importId={openId} onClose={() => setOpenId(null)} />
        </>
    );
}

function ImportEntry({ job, onOpen }: { job: MailboxImport; onOpen: () => void }) {
    const confirm = useConfirm();
    const dismiss = useDismissMailboxImport();
    const st = jobState(job);
    const live = job.status === "running";

    const hide = (e: React.MouseEvent) => {
        e.stopPropagation();
        const run = async () => {
            try {
                await dismiss.mutateAsync(job.id);
                toast.success(live ? "Import stopped and hidden" : "Import hidden");
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        };
        if (live) {
            confirm.show("Stop this import and hide it? Mailboxes it already connected stay connected.", run);
            return;
        }
        void run();
    };

    return (
        <div className="group relative flex items-start gap-2 px-3 py-2 hover:bg-slate-50 transition-colors">
            <button
                type="button"
                role="menuitem"
                onClick={onOpen}
                className="min-w-0 flex-1 flex items-start gap-2 text-left"
            >
                <span
                    className={cn(
                        "mt-1.5 block size-1.5 shrink-0 rounded-full",
                        live ? "bg-sky-500 animate-pulse" : st.tone === "error" ? "bg-red-400" : st.tone === "action" ? "bg-amber-400" : "bg-slate-300",
                    )}
                />
                <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-1.5 min-w-0 text-[12.5px] text-slate-800">
                        {job.vendor && <ProviderLogo id={job.vendor} size="xs" framed={false} />}
                        <span className="truncate">
                            {importSourceName(job)} · {plural(job.total, "mailbox", "mailboxes")}
                        </span>
                    </span>
                    <span className="mt-0.5 flex items-center gap-1.5 text-[11.5px]">
                        {st.tone === "working" || st.tone === "waiting" ? <Loader2Icon className="w-3 h-3 text-sky-600 animate-spin shrink-0" /> : null}
                        <span className={cn("tabular-nums", TONE[st.tone])}>{st.text}</span>
                        <span className="text-slate-400">· {timeAgo(job.created_at)}</span>
                    </span>
                    {st.hint && <span className="mt-0.5 block text-[11px] leading-snug text-slate-500">{st.hint}</span>}
                </span>
            </button>
            <button
                type="button"
                onClick={hide}
                disabled={dismiss.isPending}
                aria-label={live ? "Stop and hide this import" : "Hide this import"}
                title={live ? "Stop and hide" : "Hide"}
                className="shrink-0 mt-0.5 size-5 rounded inline-flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-200/60 opacity-100 md:opacity-0 md:group-hover:opacity-100 focus-visible:opacity-100 transition-opacity disabled:opacity-50"
            >
                <XIcon className="w-3 h-3" />
            </button>
        </div>
    );
}
