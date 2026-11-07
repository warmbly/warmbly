// Warmbly for Slack: the Slack-only tabs of the connection drawer. The status
// endpoint drives all of it: whether the operator set the app up, whether the
// assistant can run, which channels notifications go to, and who is linked.

"use client";

import React from "react";
import { Link } from "@tanstack/react-router";
import { AnimatePresence, motion } from "framer-motion";
import {
    AlertTriangleIcon,
    AtSignIcon,
    BellIcon,
    BotIcon,
    CheckCircle2Icon,
    CheckIcon,
    ChevronDownIcon,
    HashIcon,
    InboxIcon,
    LayoutGridIcon,
    LinkIcon,
    Loader2Icon,
    LockIcon,
    MessageSquareIcon,
    ReplyIcon,
    SparklesIcon,
    ThumbsUpIcon,
    UserPlusIcon,
    ExternalLinkIcon,
    PanelRightIcon,
    RefreshCwIcon,
    QuoteIcon,
    Trash2Icon,
    UnlinkIcon,
    UsersIcon,
    type LucideIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";

import ScrollStrip from "@/components/ui/scroll-strip";
import { OptionSelect, Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { useConfirm } from "@/hooks/context/confirm";
import useClickOutside from "@/hooks/useClickOutside";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import { usePermission } from "@/hooks/usePermission";
import {
    useDeleteMySlackLink,
    useDeleteSlackLink,
    useSlackChannels,
    useSlackStatus,
    useUpdateMySlackLink,
    useUpdateSlackSettings,
} from "@/lib/api/hooks/app/integrations/useSlack";
import type {
    SlackChannel,
    SlackInboxScope,
    SlackSettings,
    SlackStatus,
    SlackUserLink,
} from "@/lib/api/models/app/integrations/Slack";
import { NOTIFICATION_CATEGORY_GROUPS } from "@/lib/api/models/app/notifications/Notification";
import { errorMessage } from "@/lib/errors/message";
import { cn } from "@/lib/utils";

import { SectionTitle } from "./IntegrationDialog";

export type SlackTab = "overview" | "assistant" | "inbox" | "notifications" | "members";

const TABS: { key: SlackTab; label: string; icon: LucideIcon }[] = [
    { key: "overview", label: "Overview", icon: LayoutGridIcon },
    { key: "assistant", label: "Assistant", icon: BotIcon },
    { key: "inbox", label: "Inbox", icon: InboxIcon },
    { key: "notifications", label: "Notifications", icon: BellIcon },
    { key: "members", label: "Members", icon: UsersIcon },
];

export function SlackTabBar({ tab, onTab }: { tab: SlackTab; onTab: (t: SlackTab) => void }) {
    return (
        <ScrollStrip activeKey={tab} className="shrink-0 border-b border-slate-200" innerClassName="px-3 gap-1">
            {TABS.map((t) => {
                const active = tab === t.key;
                return (
                    <button
                        key={t.key}
                        type="button"
                        data-active={active}
                        onClick={() => onTab(t.key)}
                        className={cn(
                            "relative h-10 px-2.5 inline-flex shrink-0 items-center gap-1.5 text-[12.5px] transition-colors",
                            active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
                        )}
                    >
                        <t.icon className="w-3.5 h-3.5" />
                        {t.label}
                        {active && (
                            <motion.span
                                layoutId="slack-tab-underline"
                                className="absolute left-1.5 right-1.5 bottom-0 h-0.5 rounded-full bg-sky-600"
                                transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                            />
                        )}
                    </button>
                );
            })}
        </ScrollStrip>
    );
}

// One banner for the state that limits what Slack can do right now.
export function SlackStatusBanner({ onReconnect, reconnecting }: { onReconnect: () => void; reconnecting: boolean }) {
    const status = useSlackStatus();
    const s = status.data;
    if (status.isPending) {
        return (
            <div className="px-5 py-3 border-b border-slate-200 text-[11.5px] text-slate-400 inline-flex items-center gap-1.5">
                <Loader2Icon className="w-3 h-3 animate-spin" /> Checking Slack
            </div>
        );
    }
    if (!s) return null;

    const missing = s.missing_scopes ?? [];
    if (!s.app_configured) {
        return (
            <Banner tone="rose" icon={AlertTriangleIcon} title="Slack is not set up on this instance">
                The operator has to add the Slack app credentials before Slack can be connected or reconnected.
                Notifications and the assistant are off until then.
            </Banner>
        );
    }
    if (missing.length > 0) {
        return (
            <Banner tone="amber" icon={RefreshCwIcon} title="Reconnect to turn on the newest features">
                <span className="block">
                    This install predates some permissions Warmbly now uses. Reconnecting grants them.
                </span>
                <span className="mt-1.5 flex flex-wrap gap-1">
                    {missing.map((m) => (
                        <span key={m} className="px-1.5 h-5 inline-flex items-center rounded bg-white/70 border border-amber-200 text-[10px] font-mono text-amber-800">
                            {m}
                        </span>
                    ))}
                </span>
                <button
                    type="button"
                    onClick={onReconnect}
                    disabled={reconnecting}
                    className="mt-2.5 h-7 px-2.5 rounded-md bg-amber-500 hover:bg-amber-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    {reconnecting ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <RefreshCwIcon className="w-3.5 h-3.5" />}
                    Reconnect Slack
                </button>
            </Banner>
        );
    }
    if (!s.interactive_configured) {
        return (
            <Banner tone="amber" icon={BellIcon} title="Notifications only">
                Slack posts notifications, but the assistant and buttons need the instance operator to
                set <span className="font-mono">SLACK_SIGNING_SECRET</span>.
            </Banner>
        );
    }
    return (
        <Banner tone="emerald" icon={CheckCircle2Icon} title="Slack is ready">
            {s.settings.assistant_disabled
                ? "Notifications are on. The assistant is turned off for this Slack workspace."
                : "Notifications and the assistant are on."}
        </Banner>
    );
}

const BANNER_TONES = {
    rose: "border-rose-200 bg-rose-50 text-rose-800 [&_svg.banner-icon]:text-rose-500",
    amber: "border-amber-200 bg-amber-50 text-amber-900 [&_svg.banner-icon]:text-amber-500",
    emerald: "border-emerald-200 bg-emerald-50 text-emerald-900 [&_svg.banner-icon]:text-emerald-500",
} as const;

function Banner({
    tone,
    icon: Icon,
    title,
    children,
}: {
    tone: keyof typeof BANNER_TONES;
    icon: LucideIcon;
    title: string;
    children: React.ReactNode;
}) {
    return (
        <div className="px-5 py-3 border-b border-slate-200">
            <div className={cn("rounded-md border px-3 py-2.5 flex items-start gap-2", BANNER_TONES[tone])}>
                <Icon className="banner-icon w-3.5 h-3.5 mt-0.5 shrink-0" />
                <div className="min-w-0 text-[11.5px] leading-relaxed">
                    <div className="text-[12px] font-medium">{title}</div>
                    <div className="opacity-90">{children}</div>
                </div>
            </div>
        </div>
    );
}

export function SlackTabContent({ tab }: { tab: Exclude<SlackTab, "overview"> }) {
    const status = useSlackStatus();
    const canManage = usePermission("MANAGE_SETTINGS");

    if (status.isPending) {
        return (
            <p className="px-5 py-6 text-[11.5px] text-slate-400 inline-flex items-center gap-1.5">
                <Loader2Icon className="w-3 h-3 animate-spin" /> Loading
            </p>
        );
    }
    if (status.isError || !status.data) {
        return (
            <div className="px-5 py-6 space-y-2">
                <p className="text-[12px] text-slate-700">Could not load the Slack settings.</p>
                <button
                    type="button"
                    onClick={() => status.refetch()}
                    className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 inline-flex items-center gap-1.5"
                >
                    <RefreshCwIcon className="w-3.5 h-3.5" /> Try again
                </button>
            </div>
        );
    }
    const s = status.data;
    if (tab === "assistant") return <AssistantTab status={s} canManage={canManage} />;
    if (tab === "inbox") return <InboxTab status={s} canManage={canManage} />;
    if (tab === "notifications") return <NotificationsTab status={s} canManage={canManage} />;
    return <MembersTab status={s} canManage={canManage} />;
}

// useSaveSettings writes the whole settings object with one field changed.
function useSaveSettings(current: SlackSettings) {
    const update = useUpdateSlackSettings();
    const save = React.useCallback(
        async (patch: Partial<SlackSettings>, success?: string) => {
            try {
                await update.mutateAsync({ ...current, ...patch });
                if (success) toast.success(success);
            } catch (err) {
                toast.error(errorMessage(err, "Could not save the Slack settings"));
            }
        },
        [current, update],
    );
    return { save, saving: update.isPending };
}

const CAPABILITIES: { icon: LucideIcon; title: string; body: string }[] = [
    { icon: MessageSquareIcon, title: "Direct message", body: "Message the Warmbly app and ask anything about your workspace." },
    { icon: AtSignIcon, title: "Mention in a channel", body: "Mention @Warmbly in a thread and it answers there, with the thread as context." },
    { icon: PanelRightIcon, title: "Assistant pane", body: "Open Warmbly from Slack's assistant side panel, with suggested prompts." },
    { icon: QuoteIcon, title: "Ask about a message", body: "Pick Ask Warmbly about this on any message to start a thread about it." },
];

function AssistantTab({ status, canManage }: { status: SlackStatus; canManage: boolean }) {
    const settings = status.settings ?? {};
    const { save, saving } = useSaveSettings(settings);
    const enabled = !settings.assistant_disabled;
    const locked = !canManage || saving;

    return (
        <>
            <div className="px-5 py-4 border-b border-slate-200 space-y-3">
                <SectionTitle>What it can do</SectionTitle>
                <div className="space-y-2.5">
                    {CAPABILITIES.map((c) => (
                        <div key={c.title} className="flex items-start gap-2.5">
                            <span className="size-6 rounded-md bg-slate-100 text-slate-600 flex items-center justify-center shrink-0">
                                <c.icon className="w-3.5 h-3.5" />
                            </span>
                            <div className="min-w-0">
                                <div className="text-[12px] font-medium text-slate-900">{c.title}</div>
                                <p className="text-[11.5px] text-slate-500 leading-relaxed">{c.body}</p>
                            </div>
                        </div>
                    ))}
                </div>
                <p className="text-[11px] text-slate-400 leading-relaxed">
                    The assistant acts as the Warmbly member linked to the Slack account asking, with that member's
                    permissions. Anything that sends or changes data waits for an Approve click, and AI use is
                    charged to the workspace balance as in the dashboard. It never answers in channels shared with other companies.
                </p>
            </div>

            <div className="px-5 py-4 border-b border-slate-200 space-y-3">
                <div className="flex items-center justify-between gap-2">
                    <SectionTitle>Settings</SectionTitle>
                    {saving && <Loader2Icon className="w-3 h-3 animate-spin text-slate-400" />}
                </div>
                <SettingRow
                    title="Assistant enabled"
                    body="Answer questions in this Slack workspace. Off keeps Slack to notifications."
                >
                    <Toggle
                        value={enabled}
                        disabled={locked}
                        ariaLabel="Assistant enabled"
                        onChange={(v) => void save({ assistant_disabled: !v }, v ? "Assistant turned on" : "Assistant turned off")}
                    />
                </SettingRow>
                <SettingRow
                    title="Only in direct messages"
                    body="Answer in DMs and the assistant pane only, never in channels."
                >
                    <Toggle
                        value={!!settings.assistant_dm_only}
                        disabled={locked || !enabled}
                        ariaLabel="Only in direct messages"
                        onChange={(v) => void save({ assistant_dm_only: v }, v ? "Assistant limited to DMs" : "Assistant allowed in channels")}
                    />
                </SettingRow>
                {!canManage && <ReadOnlyNote />}
                {!status.interactive_configured && (
                    <p className="text-[11px] text-amber-700 leading-relaxed">
                        The assistant stays off on this instance until the operator sets the Slack signing secret.
                    </p>
                )}
            </div>
        </>
    );
}

const INBOX_ACTIONS: { icon: LucideIcon; label: string }[] = [
    { icon: ReplyIcon, label: "Reply from Slack, sent from the mailbox" },
    { icon: SparklesIcon, label: "Draft a reply with AI" },
    { icon: ThumbsUpIcon, label: "Mark the lead interested or not interested" },
    { icon: UserPlusIcon, label: "Assign the conversation to a teammate" },
    { icon: ExternalLinkIcon, label: "Open it in Warmbly" },
];

const INBOX_SCOPES: { value: SlackInboxScope; label: string; hint: string }[] = [
    { value: "replies", label: "Human replies only", hint: "Skips auto-replies, out-of-office and bounces." },
    { value: "all", label: "Every inbound message", hint: "Anything that lands in the unified inbox." },
];

function InboxTab({ status, canManage }: { status: SlackStatus; canManage: boolean }) {
    const settings = status.settings ?? {};
    const { save, saving } = useSaveSettings(settings);
    const on = !!settings.inbox_channel;
    const scope: SlackInboxScope = settings.inbox_scope === "all" ? "all" : "replies";
    const locked = !canManage || saving;

    return (
        <>
            <div className="px-5 py-4 border-b border-slate-200 space-y-2.5">
                <div className="flex items-center justify-between gap-2">
                    <SectionTitle>Inbox in Slack</SectionTitle>
                    {saving ? (
                        <Loader2Icon className="w-3 h-3 animate-spin text-slate-400" />
                    ) : (
                        <span className={cn("text-[10.5px] font-medium", on ? "text-emerald-600" : "text-slate-400")}>
                            {on ? "On" : "Off"}
                        </span>
                    )}
                </div>
                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                    Each new reply in the unified inbox starts a thread in the channel you pick. The thread follows the
                    conversation, including replies your team sends.
                </p>
                <ChannelPicker
                    value={settings.inbox_channel ?? ""}
                    onChange={(v) => void save({ inbox_channel: v }, v ? "Inbox in Slack turned on" : "Inbox in Slack turned off")}
                    emptyLabel="Off"
                    disabled={locked}
                />
                <p className="text-[11px] text-amber-700 leading-relaxed">
                    Everyone in the channel can read these emails, so pick one only your team can see.
                </p>
            </div>

            <div className="px-5 py-4 border-b border-slate-200 space-y-2">
                <SectionTitle>What to post</SectionTitle>
                <div
                    aria-disabled={locked || !on || undefined}
                    className={cn((locked || !on) && "pointer-events-none opacity-60")}
                >
                    <OptionSelect
                        aria-label="What to post"
                        cols={2}
                        value={scope}
                        onChange={(v) => void save({ inbox_scope: v }, "Saved")}
                        options={INBOX_SCOPES}
                    />
                </div>
                {!on && <p className="text-[11px] text-slate-400">Pick a channel first.</p>}
            </div>

            <div className="px-5 py-4 border-b border-slate-200 space-y-2.5">
                <SectionTitle>From the thread, teammates can</SectionTitle>
                <div className="space-y-1.5">
                    {INBOX_ACTIONS.map((a) => (
                        <div key={a.label} className="flex items-center gap-2 text-[12px] text-slate-700">
                            <a.icon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                            {a.label}
                        </div>
                    ))}
                </div>
                <p className="text-[11px] text-slate-400 leading-relaxed inline-flex items-start gap-1.5">
                    <LinkIcon className="w-3 h-3 mt-0.5 shrink-0" />
                    Actions need a linked Slack account and run with that member's permissions.
                </p>
                {!status.interactive_configured && (
                    <p className="text-[11px] text-amber-700 leading-relaxed">
                        Threads post on this instance, but the buttons stay off until the operator sets the Slack
                        signing secret.
                    </p>
                )}
                {!canManage && <ReadOnlyNote />}
            </div>
        </>
    );
}

function NotificationsTab({ status, canManage }: { status: SlackStatus; canManage: boolean }) {
    const settings = status.settings ?? {};
    const { save, saving } = useSaveSettings(settings);
    const routes = settings.routes ?? {};
    // Each save writes the whole object, so pickers wait for the previous one.
    const locked = !canManage || saving;

    function setRoute(category: string, channel: string) {
        const next = { ...routes };
        if (channel) next[category] = channel;
        else delete next[category];
        void save({ routes: next }, "Routing saved");
    }

    return (
        <>
            <div className="px-5 py-4 border-b border-slate-200 space-y-2">
                <div className="flex items-center justify-between gap-2">
                    <SectionTitle>Default channel</SectionTitle>
                    {saving && <Loader2Icon className="w-3 h-3 animate-spin text-slate-400" />}
                </div>
                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                    Workspace notifications post here unless a category below has its own channel. To use a private
                    channel, invite @Warmbly to it first.
                </p>
                <ChannelPicker
                    value={settings.channel ?? ""}
                    onChange={(v) => void save({ channel: v }, "Default channel saved")}
                    emptyLabel="No default channel"
                    disabled={locked}
                />
            </div>

            <div className="px-5 py-4 border-b border-slate-200 space-y-3">
                <SectionTitle>Per category</SectionTitle>
                {NOTIFICATION_CATEGORY_GROUPS.map((g) => (
                    <div key={g.id} className="space-y-1.5">
                        <div className="text-[11px] font-medium text-slate-500">{g.label}</div>
                        {g.categories.map((c) => (
                            <div key={c.key} className="flex items-center gap-3">
                                <span className="flex-1 min-w-0 text-[12px] text-slate-700 truncate" title={c.hint}>
                                    {c.label}
                                </span>
                                <ChannelPicker
                                    value={routes[c.key] ?? ""}
                                    onChange={(v) => setRoute(c.key, v)}
                                    emptyLabel="Use default"
                                    disabled={locked}
                                    className="w-44 shrink-0"
                                />
                            </div>
                        ))}
                    </div>
                ))}
                {!canManage && <ReadOnlyNote />}
                <p className="text-[11px] text-slate-400 leading-relaxed">
                    Each member still chooses which categories reach Slack in{" "}
                    <Link to="/app/settings/notifications" className="text-sky-700 hover:underline">
                        notification settings
                    </Link>
                    . Members who link their Slack account can also get their own notifications as DMs.
                </p>
            </div>
        </>
    );
}

function MembersTab({ status, canManage }: { status: SlackStatus; canManage: boolean }) {
    const confirm = useConfirm();
    const updateMine = useUpdateMySlackLink();
    const unlinkMine = useDeleteMySlackLink();
    const removeLink = useDeleteSlackLink();
    const mine = status.my_link ?? null;
    const links = status.links ?? [];

    async function setDM(on: boolean) {
        try {
            await updateMine.mutateAsync({ dm_notifications: on });
            toast.success(on ? "Notifications will arrive as Slack DMs" : "Slack DMs turned off");
        } catch (err) {
            toast.error(errorMessage(err, "Could not update your Slack link"));
        }
    }

    function unlink() {
        confirm.show(
            "Unlink your Slack account? The assistant stops answering you in Slack until you link again.",
            async () => {
                try {
                    await unlinkMine.mutateAsync(undefined);
                    toast.success("Slack account unlinked");
                } catch (err) {
                    toast.error(errorMessage(err, "Could not unlink your Slack account"));
                }
            },
        );
    }

    function remove(link: SlackUserLink) {
        const who = link.user_name || link.user_email || "this member";
        confirm.show(`Remove the Slack link for ${who}? They can link again from Slack.`, async () => {
            try {
                await removeLink.mutateAsync(link.id);
                toast.success("Link removed");
            } catch (err) {
                toast.error(errorMessage(err, "Could not remove the link"));
            }
        });
    }

    return (
        <>
            <div className="px-5 py-4 border-b border-slate-200 space-y-3">
                <SectionTitle>My Slack account</SectionTitle>
                {mine ? (
                    <>
                        <div className="flex items-center gap-2.5">
                            <span className="size-7 rounded-md bg-emerald-50 text-emerald-600 flex items-center justify-center shrink-0">
                                <LinkIcon className="w-3.5 h-3.5" />
                            </span>
                            <div className="min-w-0 flex-1">
                                <div className="text-[12px] font-medium text-slate-900">Linked</div>
                                <div className="text-[11px] text-slate-500 truncate">
                                    Slack member <span className="font-mono">{mine.slack_user_id}</span>, since{" "}
                                    {new Date(mine.created_at).toLocaleDateString()}
                                </div>
                            </div>
                        </div>
                        <SettingRow
                            title="Send my notifications as Slack DMs"
                            body="Categories you have Slack turned on for in notification settings also arrive in your DMs."
                        >
                            <Toggle
                                value={mine.dm_notifications}
                                disabled={updateMine.isPending}
                                ariaLabel="Send my notifications as Slack DMs"
                                onChange={(v) => void setDM(v)}
                            />
                        </SettingRow>
                        <button
                            type="button"
                            onClick={unlink}
                            className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-rose-200 hover:bg-rose-50 text-[12px] text-rose-600 inline-flex items-center gap-1.5 transition-colors"
                        >
                            <UnlinkIcon className="w-3.5 h-3.5" />
                            Unlink
                        </button>
                    </>
                ) : (
                    <div className="rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5 space-y-1">
                        <div className="text-[12px] font-medium text-slate-900">Not linked</div>
                        <p className="text-[11.5px] text-slate-500 leading-relaxed">
                            {status.interactive_configured ? (
                                <>
                                    Mention @Warmbly or message it in Slack, and it sends you a link. Linking lets the
                                    assistant act as you and lets you get notifications as DMs.
                                </>
                            ) : (
                                "Linking needs the instance operator to finish the Slack app setup first."
                            )}
                        </p>
                    </div>
                )}
            </div>

            <div className="px-5 py-4 border-b border-slate-200 space-y-2">
                <SectionTitle>Linked members</SectionTitle>
                {!canManage ? (
                    <p className="text-[11.5px] text-slate-400">Members who manage settings can see and remove links.</p>
                ) : links.length === 0 ? (
                    <p className="text-[11.5px] text-slate-400">Nobody has linked a Slack account yet.</p>
                ) : (
                    <div className="divide-y divide-slate-200/70 rounded-md border border-slate-200">
                        {links.map((l) => (
                            <div key={l.id} className="group px-3 h-11 flex items-center gap-2.5">
                                <div className="min-w-0 flex-1">
                                    <div className="text-[12px] text-slate-900 truncate">{l.user_name || l.user_email || l.user_id}</div>
                                    <div className="text-[10.5px] text-slate-400 truncate">
                                        <span className="font-mono">{l.slack_user_id}</span>
                                        {l.dm_notifications && " · DMs on"}
                                    </div>
                                </div>
                                <button
                                    type="button"
                                    onClick={() => remove(l)}
                                    aria-label={`Remove the Slack link for ${l.user_name || l.user_email || "this member"}`}
                                    className="size-7 rounded-md text-slate-400 hover:text-rose-600 hover:bg-rose-50 flex items-center justify-center shrink-0 opacity-100 md:opacity-0 md:group-hover:opacity-100 focus-visible:opacity-100 transition-opacity"
                                >
                                    <Trash2Icon className="w-3.5 h-3.5" />
                                </button>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </>
    );
}

function SettingRow({ title, body, children }: { title: string; body: string; children: React.ReactNode }) {
    return (
        <div className="flex items-start justify-between gap-4">
            <div className="min-w-0">
                <div className="text-[12px] font-medium text-slate-900">{title}</div>
                <p className="text-[11.5px] text-slate-500 leading-relaxed">{body}</p>
            </div>
            <div className="pt-0.5">{children}</div>
        </div>
    );
}

function ReadOnlyNote() {
    return (
        <p className="text-[11px] text-slate-400 inline-flex items-center gap-1.5">
            <LockIcon className="w-3 h-3" />
            Only members who manage settings can change these.
        </p>
    );
}

// ChannelPicker picks one Slack channel, searched on the server. An empty
// value is the "none" / "use default" choice named by emptyLabel.
function ChannelPicker({
    value,
    onChange,
    emptyLabel,
    disabled,
    className,
}: {
    value: string;
    onChange: (v: string) => void;
    emptyLabel: string;
    disabled?: boolean;
    className?: string;
}) {
    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    const q = useDebouncedValue(query.trim(), 250);
    const ref = React.useRef<HTMLDivElement>(null);
    const triggerRef = React.useRef<HTMLButtonElement>(null);
    useClickOutside(open, () => setOpen(false), ref);
    const placement = useFlipPlacement(triggerRef, open, 280);

    // The unfiltered list names the stored id; react-query shares it across pickers.
    const all = useSlackChannels("", true);
    const results = useSlackChannels(q, open);
    const named = React.useMemo(() => {
        const m = new Map<string, SlackChannel>();
        for (const c of all.data?.data ?? []) m.set(c.id, c);
        for (const c of results.data?.data ?? []) m.set(c.id, c);
        return m;
    }, [all.data, results.data]);

    const current = value ? named.get(value) : undefined;
    const label = !value ? emptyLabel : current ? current.name : value.replace(/^#/, "");
    const channels = results.data?.data ?? [];

    function pick(v: string) {
        setOpen(false);
        setQuery("");
        if (v !== value) onChange(v);
    }

    return (
        <div ref={ref} className={cn("relative", className)}>
            <button
                ref={triggerRef}
                type="button"
                disabled={disabled}
                onClick={() => setOpen((o) => !o)}
                className="w-full h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] text-slate-700 flex items-center gap-1.5 transition-colors disabled:opacity-60 disabled:cursor-not-allowed"
            >
                {value ? (
                    current?.is_private ? (
                        <LockIcon className="w-3 h-3 text-slate-400 shrink-0" />
                    ) : (
                        <HashIcon className="w-3 h-3 text-slate-400 shrink-0" />
                    )
                ) : null}
                <span className={cn("truncate flex-1 text-left", !value && "text-slate-400")}>{label}</span>
                <ChevronDownIcon className="w-3 h-3 text-slate-400 shrink-0" />
            </button>

            <AnimatePresence>
                {open && (
                    <motion.div
                        data-floating
                        initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        className={cn(
                            "absolute right-0 z-30 w-60 max-w-[80vw] rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden",
                            placement === "top" ? "bottom-full mb-1" : "top-full mt-1",
                        )}
                    >
                        <div className="px-2 py-1.5 border-b border-slate-200 flex items-center gap-1.5">
                            <input
                                value={query}
                                onChange={(e) => setQuery(e.target.value)}
                                placeholder="Search channels"
                                autoFocus
                                className="flex-1 min-w-0 h-5 bg-transparent text-[16px] md:text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                            />
                            {results.isFetching && <Loader2Icon className="w-3 h-3 animate-spin text-slate-400 shrink-0" />}
                        </div>
                        <div className="max-h-56 overflow-y-auto py-1">
                            <PickerRow selected={!value} onClick={() => pick("")}>
                                <span className="truncate text-slate-500">{emptyLabel}</span>
                            </PickerRow>
                            {results.isError ? (
                                <div className="px-3 py-3 text-[11.5px] text-rose-600 text-center">
                                    {errorMessage(results.error, "Could not load channels")}
                                </div>
                            ) : channels.length === 0 && !results.isFetching ? (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                                    {q ? "No channel matches." : "No channels visible to Warmbly."}
                                </div>
                            ) : (
                                channels.map((c) => (
                                    <PickerRow key={c.id} selected={c.id === value} onClick={() => pick(c.id)}>
                                        {c.is_private ? (
                                            <LockIcon className="w-3 h-3 text-slate-400 shrink-0" />
                                        ) : (
                                            <HashIcon className="w-3 h-3 text-slate-400 shrink-0" />
                                        )}
                                        <span className="truncate">{c.name}</span>
                                    </PickerRow>
                                ))
                            )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function PickerRow({
    selected,
    onClick,
    children,
}: {
    selected: boolean;
    onClick: () => void;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            className={cn(
                "w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors",
                selected && "bg-sky-50 text-sky-700 hover:bg-sky-50",
            )}
        >
            {children}
            {selected && <CheckIcon className="w-3 h-3 ml-auto shrink-0 text-sky-600" />}
        </button>
    );
}
