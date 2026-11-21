// The social accounts that run a campaign's social steps, grouped by channel.
import { Checkbox } from "@/components/ui/checkbox";
import { Avatar, StatusPill } from "@/components/app/social/parts";
import ChannelMark from "@/components/app/social/ChannelMark";
import { SocialSetupCard, SocialSetupLink } from "@/components/app/social/SocialSetup";
import { useSocialAccounts, useSocialChannels } from "@/lib/api/hooks/app/social/useSocial";
import type { SocialAccount, SocialChannel } from "@/lib/api/models/app/social/Social";
import { CHANNELS, channelLabel } from "@/lib/social/channels";

export function SocialAccountsSection({ value, onChange, stepChannels = [] }: { value: string[]; onChange: (ids: string[]) => void; stepChannels?: SocialChannel[] }) {
    const { data: accounts, isPending, isError } = useSocialAccounts();
    const catalog = useSocialChannels();
    const picked = new Set(value);
    // An account removed since it was picked still shows, so it can be taken off.
    const listed = (accounts?.data ?? []).filter((a) => a.status !== "removed" || picked.has(a.id));
    const toggle = (id: string) => onChange(picked.has(id) ? value.filter((x) => x !== id) : [...value, id]);

    if (isPending) return <div className="h-16 rounded-md bg-slate-50 animate-pulse" />;
    if (isError) return <p className="text-[12px] text-rose-600">The social accounts could not be loaded. Refresh to try again.</p>;

    const byChannel = new Map<SocialChannel, SocialAccount[]>();
    for (const a of listed) byChannel.set(a.channel, [...(byChannel.get(a.channel) ?? []), a]);
    // Channels the steps use come first, so a missing sender is the first thing on the page.
    const channels = CHANNELS.filter((c) => byChannel.has(c) || stepChannels.includes(c)).sort((a, b) => Number(stepChannels.includes(b)) - Number(stepChannels.includes(a)));

    if (channels.length === 0) {
        return <SocialSetupCard description="No social account is connected yet. Social steps need one on their channel to run." />;
    }
    return (
        <div className="space-y-4">
            {channels.map((ch) => {
                const rows = byChannel.get(ch) ?? [];
                const name = channelLabel(ch, catalog.data);
                const used = stepChannels.includes(ch);
                const chosen = rows.filter((a) => picked.has(a.id)).length;
                return (
                    <section key={ch} aria-label={name}>
                        <div className="mb-1.5 flex items-center gap-2">
                            <ChannelMark channel={ch} className="size-3.5" />
                            <h4 className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-500">{name}</h4>
                            {used && chosen === 0 && rows.length > 0 && <span className="ml-auto text-[11px] text-rose-600">Pick one to run the {name} steps</span>}
                        </div>
                        {rows.length === 0 ? (
                            <div className="flex items-center gap-3 rounded-md border border-dashed border-slate-200 px-3 py-2.5">
                                <p className="min-w-0 flex-1 text-[12px] text-slate-600">
                                    {used ? `This campaign has ${name} steps and no ${name} account to run them.` : `No ${name} account is connected.`}
                                </p>
                                <SocialSetupLink channel={ch} className="shrink-0 text-[12px] font-medium text-sky-700 hover:underline" />
                            </div>
                        ) : (
                            <div className="divide-y divide-slate-100 rounded-md border border-slate-200">
                                {rows.map((a) => (
                                    <label key={a.id} className="flex cursor-pointer items-center gap-3 px-3 py-2 hover:bg-slate-50/60">
                                        <Checkbox tone="slate" checked={picked.has(a.id)} onChange={() => toggle(a.id)} aria-label={`Use ${a.display_name}`} />
                                        <Avatar src={a.avatar_url} name={a.display_name} size={28} />
                                        <span className="min-w-0 flex-1">
                                            <span className="block truncate text-[12.5px] font-medium text-slate-900">{a.display_name}</span>
                                            {(a.headline || a.username) && <span className="block truncate text-[11.5px] text-slate-500">{a.headline || a.username}</span>}
                                        </span>
                                        <StatusPill status={a.status} className="shrink-0" />
                                    </label>
                                ))}
                            </div>
                        )}
                    </section>
                );
            })}
            <p className="text-[11.5px] leading-relaxed text-slate-500">
                Each step runs from an account on its own channel. A contact stays with the first account that reached them on that channel; new contacts go to the account with the most room left today.
            </p>
        </div>
    );
}
