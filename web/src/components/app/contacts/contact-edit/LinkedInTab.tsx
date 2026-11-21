// LinkedIn tab: the LinkedIn steps campaigns ran for this contact, and their LinkedIn conversations.
import { Link } from "@tanstack/react-router";
import { EyeIcon, HeartIcon, MailIcon, MessageSquareIcon, UndoIcon, UserPlusIcon, UserRoundCheckIcon, type LucideIcon } from "lucide-react";
import { useLinkedInContactActivity } from "@/lib/api/hooks/app/linkedin/useLinkedIn";
import type { LinkedInContactAction } from "@/lib/api/models/app/linkedin/LinkedIn";
import { Avatar } from "@/components/app/linkedin/parts";
import { linkedInErrorMessage } from "@/lib/linkedin/errors";
import { cn } from "@/lib/utils";
import { fmtAbsolute, fmtRelative } from "./format";

const ACTION: Record<string, { label: string; icon: LucideIcon }> = {
    invite: { label: "Connection request", icon: UserPlusIcon },
    message: { label: "Message", icon: MessageSquareIcon },
    inmail: { label: "InMail", icon: MailIcon },
    profile_visit: { label: "Profile visit", icon: EyeIcon },
    visit: { label: "Profile visit", icon: EyeIcon },
    follow: { label: "Follow", icon: UserRoundCheckIcon },
    like_post: { label: "Liked their latest post", icon: HeartIcon },
    like: { label: "Liked their latest post", icon: HeartIcon },
    withdraw_invite: { label: "Withdrew the request", icon: UndoIcon },
    withdraw: { label: "Withdrew the request", icon: UndoIcon },
};

const STATUS: Record<string, { label: string; cls: string }> = {
    pending: { label: "Queued", cls: "bg-slate-100 text-slate-600" },
    queued: { label: "Queued", cls: "bg-slate-100 text-slate-600" },
    running: { label: "In progress", cls: "bg-sky-50 text-sky-700" },
    succeeded: { label: "Done", cls: "bg-emerald-50 text-emerald-700" },
    failed: { label: "Failed", cls: "bg-rose-50 text-rose-700" },
    skipped: { label: "Skipped", cls: "bg-amber-50 text-amber-700" },
    cancelled: { label: "Cancelled", cls: "bg-slate-100 text-slate-500" },
    expired: { label: "Expired", cls: "bg-slate-100 text-slate-500" },
};

// Stored by GraphKit action (invite, like_post); a sequence type (linkedin_invite) reads the same.
const actionKey = (t: string) => t.replace(/^linkedin_/, "");

const humanize = (s: string) => s.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());

function reasonOf(a: LinkedInContactAction): string | null {
    if (a.reason) return a.reason;
    const code = a.skip_reason || a.error_code;
    return code ? humanize(code) : null;
}

export default function LinkedInTab({ contactId }: { contactId: string }) {
    const q = useLinkedInContactActivity(contactId);
    if (q.isPending) {
        return (
            <div className="space-y-2 p-4">
                {[...Array(3)].map((_, i) => (
                    <div key={i} className="h-12 rounded-md bg-slate-50 animate-pulse" />
                ))}
            </div>
        );
    }
    if (q.isError) return <p className="p-4 text-[12px] text-slate-500">{linkedInErrorMessage(q.error)}</p>;
    const { actions, conversations } = q.data;
    return (
        <div className="space-y-5 p-4">
            <section>
                <h3 className="mb-2 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Conversations</h3>
                {conversations.length === 0 ? (
                    <p className="text-[12px] text-slate-500">No LinkedIn messages with this contact yet.</p>
                ) : (
                    <ul className="divide-y divide-slate-100 rounded-md border border-slate-200">
                        {conversations.map((c) => (
                            <li key={c.id}>
                                <Link
                                    to="/app/unibox/{-$scope}/{-$threadId}"
                                    params={{ scope: "linkedin", threadId: c.id }}
                                    search={{ ref: undefined }}
                                    className="flex items-start gap-2.5 px-3 py-2.5 hover:bg-slate-50"
                                >
                                    <Avatar src={c.participant.picture_url} name={c.participant.full_name || "LinkedIn member"} size={28} />
                                    <span className="min-w-0 flex-1">
                                        <span className="flex items-center gap-2">
                                            <span className={cn("min-w-0 truncate text-[12.5px]", c.unread_count > 0 ? "font-semibold text-slate-900" : "text-slate-800")}>
                                                {c.participant.full_name || "LinkedIn member"}
                                            </span>
                                            {c.unread_count > 0 && <span className="shrink-0 rounded-full bg-sky-500 px-1.5 text-[10px] font-medium text-white tabular-nums">{c.unread_count}</span>}
                                            <span className="ml-auto shrink-0 text-[11px] text-slate-400">{fmtRelative(c.last_message_at)}</span>
                                        </span>
                                        <span className="mt-0.5 block truncate text-[12px] text-slate-500">{c.last_message_preview || "No messages yet"}</span>
                                    </span>
                                </Link>
                            </li>
                        ))}
                    </ul>
                )}
            </section>
            <section>
                <h3 className="mb-2 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Campaign steps</h3>
                {actions.length === 0 ? (
                    <p className="text-[12px] text-slate-500">No campaign has run a LinkedIn step for this contact.</p>
                ) : (
                    <ol className="space-y-1.5">
                        {actions.map((a) => {
                            const meta = ACTION[actionKey(a.type)] ?? { label: humanize(actionKey(a.type)), icon: MessageSquareIcon };
                            const status = STATUS[a.status] ?? { label: humanize(a.status), cls: "bg-slate-100 text-slate-600" };
                            const reason = a.status === "failed" || a.status === "skipped" ? reasonOf(a) : null;
                            const when = a.completed_at ?? a.queued_at;
                            return (
                                <li key={a.id} className="flex items-start gap-2.5 rounded-md border border-slate-200 px-3 py-2">
                                    <meta.icon className="mt-0.5 size-3.5 shrink-0 text-slate-400" />
                                    <div className="min-w-0 flex-1">
                                        <div className="flex items-center gap-2">
                                            <span className="min-w-0 truncate text-[12.5px] text-slate-900">{meta.label}</span>
                                            <span className={cn("shrink-0 rounded px-1.5 py-px text-[10.5px] font-medium", status.cls)}>{status.label}</span>
                                            {when && (
                                                <span className="ml-auto shrink-0 text-[11px] text-slate-400" title={fmtAbsolute(when)}>
                                                    {fmtRelative(when)}
                                                </span>
                                            )}
                                        </div>
                                        {a.campaign_id && (
                                            <Link to="/app/campaigns/$id" params={{ id: a.campaign_id }} className="block truncate text-[11.5px] text-slate-500 hover:text-sky-700 hover:underline">
                                                {a.campaign_name || "Campaign"}
                                            </Link>
                                        )}
                                        {reason && <p className="mt-0.5 text-[11.5px] text-slate-500">{reason}</p>}
                                    </div>
                                </li>
                            );
                        })}
                    </ol>
                )}
            </section>
        </div>
    );
}
