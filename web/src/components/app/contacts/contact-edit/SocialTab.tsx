// Social tab: the contact's handle on each channel with the consent basis recorded for outreach there, the social steps
// campaigns ran for them, and their social conversations.
import React from "react";
import { Link } from "@tanstack/react-router";
import toast from "react-hot-toast/headless";
import {
    AwardIcon,
    EyeIcon,
    HeartIcon,
    Loader2Icon,
    MailIcon,
    MessageCircleIcon,
    MessageSquareIcon,
    MicIcon,
    PencilIcon,
    PlusIcon,
    ShieldCheckIcon,
    Trash2Icon,
    UndoIcon,
    UserPlusIcon,
    UserRoundCheckIcon,
    type LucideIcon,
} from "lucide-react";
import { TextInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import ChannelMark from "@/components/app/social/ChannelMark";
import { SocialProfileLink } from "@/components/app/social/SocialLinks";
import { Avatar } from "@/components/app/social/parts";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import type { AppError } from "@/lib/api/client/normalizeError";
import useUpdateContact from "@/lib/api/hooks/app/contacts/useUpdateContact";
import { useSocialChannels, useSocialContactActivity } from "@/lib/api/hooks/app/social/useSocial";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type { ChannelSpec, SocialChannel, SocialContactAction } from "@/lib/api/models/app/social/Social";
import { CHANNELS, CONSENT_LABEL, HANDLE_HINT, HANDLE_PLACEHOLDER, channelLabel, consentLabel, handleProblem, orderedCatalog } from "@/lib/social/channels";
import { socialErrorCode, socialErrorMessage, socialOutcomeLabel } from "@/lib/social/errors";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { fmtAbsolute, fmtRelative } from "./format";

const LABEL = "mb-2 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400";
const NO_BASIS = "";

export default function SocialTab({ contact }: { contact: Contact }) {
    return (
        <div className="space-y-6">
            <Identities contact={contact} />
            <Activity contactId={contact.id} />
        </div>
    );
}

function Identities({ contact }: { contact: Contact }) {
    const catalog = useSocialChannels();
    const canEdit = usePermission("MANAGE_CONTACTS");
    const update = useUpdateContact(contact.id);
    const confirm = useConfirm();
    const [editing, setEditing] = React.useState<SocialChannel | null>(null);
    const social = contact.social ?? {};
    const specs = orderedCatalog(catalog.data);
    const channels: SocialChannel[] = specs.length ? specs.map((c) => c.channel) : CHANNELS;
    const present = channels.filter((c) => social[c]?.handle);
    const missing = channels.filter((c) => !social[c]?.handle);

    const save = async (channel: SocialChannel, handle: string, basis: string): Promise<string | null> => {
        try {
            await update.mutateAsync({ social: { [channel]: { handle: handle.trim(), consent_basis: basis || null } } });
            setEditing(null);
            return null;
        } catch (e) {
            const code = socialErrorCode(e);
            return code === "invalid_social_handle" || code === "consent_required" ? socialErrorMessage(e) : buildError(e as AppError);
        }
    };
    const remove = (channel: SocialChannel) =>
        confirm.show(
            `Remove this contact's ${channelLabel(channel, catalog.data)} handle? Campaign steps on ${channelLabel(channel, catalog.data)} pass them over until one is added again.`,
            async () => {
                try {
                    await update.mutateAsync({ social: { [channel]: null } });
                } catch (e) {
                    toast.error(buildError(e as AppError));
                }
            },
            undefined,
            { title: "Remove handle", confirmLabel: "Remove", tone: "danger" },
        );

    return (
        <section>
            <h3 className={LABEL}>Profiles</h3>
            <div className="divide-y divide-slate-100 rounded-md border border-slate-200">
                {present.map((c) =>
                    editing === c ? (
                        <IdentityEditor
                            key={c}
                            channel={c}
                            spec={specs.find((s) => s.channel === c)}
                            initialHandle={social[c]?.handle ?? ""}
                            initialBasis={social[c]?.consent_basis ?? NO_BASIS}
                            onCancel={() => setEditing(null)}
                            onSave={(h, b) => save(c, h, b)}
                        />
                    ) : (
                        <div key={c} className="group flex items-center gap-2.5 px-3 py-2.5">
                            <ChannelMark channel={c} className="size-4" />
                            <div className="min-w-0 flex-1">
                                <div className="flex min-w-0 items-center gap-1.5">
                                    <span className="truncate text-[12.5px] text-slate-900">{social[c]?.handle}</span>
                                    <SocialProfileLink channel={c} handle={social[c]?.handle} label={contact.first_name || contact.email} className="shrink-0" />
                                </div>
                                <ConsentLine spec={specs.find((s) => s.channel === c)} basis={social[c]?.consent_basis} recordedAt={social[c]?.consent_recorded_at} />
                            </div>
                            {canEdit && (
                                <div className="flex shrink-0 items-center gap-0.5 opacity-100 md:opacity-0 md:group-hover:opacity-100 focus-within:opacity-100 transition-opacity">
                                    <button
                                        type="button"
                                        onClick={() => setEditing(c)}
                                        aria-label={`Edit the ${channelLabel(c)} handle`}
                                        className="size-7 rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-800 inline-flex items-center justify-center"
                                    >
                                        <PencilIcon className="size-3.5" />
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() => remove(c)}
                                        aria-label={`Remove the ${channelLabel(c)} handle`}
                                        className="size-7 rounded-md text-slate-400 hover:bg-rose-50 hover:text-rose-600 inline-flex items-center justify-center"
                                    >
                                        <Trash2Icon className="size-3.5" />
                                    </button>
                                </div>
                            )}
                        </div>
                    ),
                )}
                {editing && !present.includes(editing) && (
                    <IdentityEditor
                        channel={editing}
                        spec={specs.find((s) => s.channel === editing)}
                        initialHandle=""
                        initialBasis={NO_BASIS}
                        onCancel={() => setEditing(null)}
                        onSave={(h, b) => save(editing, h, b)}
                    />
                )}
                {present.length === 0 && !editing && <p className="px-3 py-3 text-[12px] text-slate-500">No social profiles yet. Campaign steps on a channel pass over a contact with no handle there.</p>}
            </div>
            {canEdit && missing.length > 0 && !editing && (
                <div className="mt-2 flex flex-wrap items-center gap-1.5">
                    {missing.map((c) => (
                        <button
                            key={c}
                            type="button"
                            onClick={() => setEditing(c)}
                            className="h-7 rounded-md border border-dashed border-slate-200 px-2 text-[11.5px] text-slate-600 hover:border-slate-300 hover:text-slate-900 inline-flex items-center gap-1.5"
                        >
                            <PlusIcon className="size-3" />
                            <ChannelMark channel={c} className="size-3.5" />
                            {channelLabel(c, catalog.data)}
                        </button>
                    ))}
                </div>
            )}
        </section>
    );
}

function ConsentLine({ spec, basis, recordedAt }: { spec: ChannelSpec | undefined; basis: string | null | undefined; recordedAt?: Date | string | null }) {
    if (basis) {
        return (
            <p className="mt-0.5 flex items-center gap-1 text-[11px] text-emerald-700">
                <ShieldCheckIcon className="size-3" />
                {consentLabel(basis)}
                {recordedAt && <span className="text-slate-400"> · {fmtRelative(recordedAt)}</span>}
            </p>
        );
    }
    if (spec?.consent_required) return <p className="mt-0.5 text-[11px] text-amber-700">No consent recorded, so campaigns on {spec.label} pass this contact over.</p>;
    return null;
}

function IdentityEditor({
    channel,
    spec,
    initialHandle,
    initialBasis,
    onCancel,
    onSave,
}: {
    channel: SocialChannel;
    spec: ChannelSpec | undefined;
    initialHandle: string;
    initialBasis: string;
    onCancel: () => void;
    onSave: (handle: string, basis: string) => Promise<string | null>;
}) {
    const [handle, setHandle] = React.useState(initialHandle);
    const [basis, setBasis] = React.useState(initialBasis);
    const [error, setError] = React.useState<string | null>(null);
    const [saving, setSaving] = React.useState(false);
    const bases = spec?.consent_bases?.length ? spec.consent_bases : spec?.consent_required ? Object.keys(CONSENT_LABEL) : [];
    const options = [
        { value: NO_BASIS, label: spec?.consent_required ? "No consent recorded" : "None" },
        ...bases.map((b) => ({ value: b, label: consentLabel(b) })),
    ];
    const submit = async () => {
        if (!handle.trim() || saving) return;
        const problem = handleProblem(channel, handle);
        if (problem) return void setError(problem);
        setSaving(true);
        setError(await onSave(handle, basis));
        setSaving(false);
    };
    return (
        <form
            className="space-y-2 px-3 py-2.5 bg-slate-50/60"
            onSubmit={(e) => {
                e.preventDefault();
                void submit();
            }}
        >
            <div className="flex items-center gap-2">
                <ChannelMark channel={channel} className="size-4" />
                <TextInput
                    value={handle}
                    onChange={(v) => {
                        setHandle(v);
                        setError(null);
                    }}
                    placeholder={HANDLE_PLACEHOLDER[channel]}
                    aria-label={`${channelLabel(channel)} handle`}
                    className="w-full"
                    maxLength={300}
                    autoFocus
                />
            </div>
            {!error && <p className="pl-6 text-[11px] text-slate-500">{HANDLE_HINT[channel]}</p>}
            {bases.length > 0 && (
                <div className="flex items-center gap-2 pl-6">
                    <span className="shrink-0 text-[11.5px] text-slate-500">Consent</span>
                    <SelectMenu value={basis} onChange={setBasis} options={options} aria-label="Consent basis" fullWidth />
                </div>
            )}
            {basis && CONSENT_LABEL[basis as keyof typeof CONSENT_LABEL] && <p className="pl-6 text-[11px] text-slate-500">{CONSENT_LABEL[basis as keyof typeof CONSENT_LABEL].hint}</p>}
            {error && (
                <p role="alert" className="pl-6 text-[11.5px] text-rose-600">
                    {error}
                </p>
            )}
            <div className="flex justify-end gap-1.5">
                <button type="button" onClick={onCancel} className="h-7 rounded-md border border-slate-200 bg-white px-2.5 text-[12px] font-medium text-slate-700 hover:bg-slate-50">
                    Cancel
                </button>
                <button
                    type="submit"
                    disabled={!handle.trim() || saving}
                    className="h-7 rounded-md bg-sky-600 px-2.5 text-[12px] font-medium text-white hover:bg-sky-700 disabled:bg-slate-200 disabled:text-slate-400 inline-flex items-center gap-1.5"
                >
                    {saving && <Loader2Icon className="size-3 animate-spin" />}
                    Save
                </button>
            </div>
        </form>
    );
}

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
    voice_note: { label: "Voice note", icon: MicIcon },
    comment_post: { label: "Commented on their latest post", icon: MessageCircleIcon },
    endorse: { label: "Endorsed their skills", icon: AwardIcon },
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

const actionKey = (t: string) => t;

const humanize = (s: string) => s.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());

// The server's own message wins: capability and consent refusals say exactly what is missing.
function reasonOf(a: SocialContactAction): string | null {
    if (a.reason) return a.reason;
    return socialOutcomeLabel(a.skip_reason || a.error_code);
}

function Activity({ contactId }: { contactId: string }) {
    const q = useSocialContactActivity(contactId);
    if (q.isPending) {
        return (
            <div className="space-y-2 p-4">
                {[...Array(3)].map((_, i) => (
                    <div key={i} className="h-12 rounded-md bg-slate-50 animate-pulse" />
                ))}
            </div>
        );
    }
    if (q.isError) return <p className="p-4 text-[12px] text-slate-500">{socialErrorMessage(q.error)}</p>;
    const { actions, conversations } = q.data;
    return (
        <div className="space-y-5">
            <section>
                <h3 className={LABEL}>Conversations</h3>
                {conversations.length === 0 ? (
                    <p className="text-[12px] text-slate-500">No social messages with this contact yet.</p>
                ) : (
                    <ul className="divide-y divide-slate-100 rounded-md border border-slate-200">
                        {conversations.map((c) => (
                            <li key={c.id}>
                                <Link
                                    to="/app/unibox/{-$scope}/{-$threadId}"
                                    params={{ scope: "social", threadId: c.id }}
                                    search={{ ref: undefined }}
                                    className="flex items-start gap-2.5 px-3 py-2.5 hover:bg-slate-50"
                                >
                                    <span className="relative shrink-0">
                                        <Avatar src={c.participant.picture_url} name={c.participant.full_name || c.participant.handle || channelLabel(c.channel)} size={28} />
                                        <span className="absolute -bottom-0.5 -right-0.5 rounded-[4px] bg-white p-px">
                                            <ChannelMark channel={c.channel} className="size-3" />
                                        </span>
                                    </span>
                                    <span className="min-w-0 flex-1">
                                        <span className="flex items-center gap-2">
                                            <span className={cn("min-w-0 truncate text-[12.5px]", c.unread_count > 0 ? "font-semibold text-slate-900" : "text-slate-800")}>
                                                {c.participant.full_name || c.participant.handle || `${channelLabel(c.channel)} contact`}
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
                <h3 className={LABEL}>Campaign steps</h3>
                {actions.length === 0 ? (
                    <p className="text-[12px] text-slate-500">No campaign has run a social step for this contact.</p>
                ) : (
                    <ol className="space-y-1.5">
                        {actions.map((a) => {
                            const meta = ACTION[actionKey(a.type)] ?? { label: humanize(actionKey(a.type)), icon: MessageSquareIcon };
                            const status = STATUS[a.status] ?? { label: humanize(a.status), cls: "bg-slate-100 text-slate-600" };
                            const reason = a.status === "failed" || a.status === "skipped" ? reasonOf(a) : null;
                            const when = a.completed_at ?? a.queued_at;
                            return (
                                <li key={a.id} className="flex items-start gap-2.5 rounded-md border border-slate-200 px-3 py-2">
                                    <span className="relative mt-0.5 shrink-0">
                                        <meta.icon className="size-3.5 text-slate-400" />
                                        <ChannelMark channel={a.channel} className="absolute -bottom-1 -right-1 size-2.5" />
                                    </span>
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
