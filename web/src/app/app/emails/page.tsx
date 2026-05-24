import { RiFireLine, RiMoreLine } from "@remixicon/react";
import React, { useMemo } from "react";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import { useUserProfile } from "@/hooks/context/user";
import InboxDetails from "@/components/app/emails/InboxDetails";
import type Tag from "@/lib/api/models/app/Tag";
import {
    FilterIcon,
    MailIcon,
    PlusIcon,
    Settings2Icon,
} from "lucide-react";
import { SearchInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import {
    EmptyBlock,
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
    TopbarAction,
} from "@/components/layout/Page";

const DefaultFolder = {
    title: "All accounts",
    color: "#c4c8cf",
} as Tag;

export default function AddressesPage() {
    const p = useUserProfile();

    const [query, setQuery] = React.useState<string>("");
    const [tag, setTag] = React.useState<string>("");
    const emailsData = useEmails({ query, tag });
    const [selected, setSelected] = React.useState<string[]>([]);
    const [view, setView] = React.useState<string>("");

    const stag = useMemo(() => {
        if (!p) return DefaultFolder;
        const f = p.user.tags.find((t) => t.id === tag);
        if (!f) return DefaultFolder;
        return f;
    }, [tag, p]);

    const stats = useMemo(() => {
        if (!emailsData.emails) return { total: 0, healthy: 0, warming: 0, issues: 0 };
        return {
            total: emailsData.emails.length,
            healthy: emailsData.emails.filter((e) => e.status === "healthy").length,
            warming: emailsData.emails.filter((e) => e.status === "warming").length,
            issues: emailsData.emails.filter((e) => e.status !== "healthy" && e.status !== "warming").length,
        };
    }, [emailsData.emails]);

    function isSelectedAll(): boolean {
        return emailsData.emails
            ? emailsData.emails.length > 0 && selected.length === emailsData.emails.length
            : false;
    }

    return (
        <Page>
            <PageTopbar
                eyebrow="Accounts"
                subtitle={
                    emailsData.emails
                        ? `${stats.total} mailboxes`
                        : "Loading…"
                }
            >
                <TopbarAction
                    onClick={() => p?.setAddEmail(true)}
                    icon={<PlusIcon className="w-3 h-3" />}
                >
                    Add account
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Total" value={stats.total} sub="connected" />
                <Stat label="Healthy" value={stats.healthy} sub="sending now" accent={stats.healthy > 0} />
                <Stat label="Warming" value={stats.warming} sub="ramping up" />
                <Stat label="Needs attention" value={stats.issues} sub="paused or failing" last />
            </StatStrip>

            <SectionBar label="Mailboxes" count={emailsData.emails?.length ?? 0}>
                <SearchInput
                    value={query}
                    onChange={setQuery}
                    placeholder="Search by email…"
                    className="w-56"
                />
                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <SelectButton
                            icon={<FilterIcon className="w-3.5 h-3.5" />}
                            label={stag.title}
                        />
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={200}>
                        <PopoverMenuLabel>Tags</PopoverMenuLabel>
                        <PopoverMenuItem
                            onSelect={() => setTag("")}
                            selected={!tag}
                        >
                            All accounts
                        </PopoverMenuItem>
                        {(p?.user.tags ?? []).map((t) => (
                            <PopoverMenuItem
                                key={t.id}
                                onSelect={() => setTag(tag === t.id ? "" : t.id)}
                                icon={<span className="size-2 rounded-full" style={{ backgroundColor: t.color }} />}
                                selected={tag === t.id}
                            >
                                {t.title}
                            </PopoverMenuItem>
                        ))}
                        <PopoverMenuSeparator />
                        <PopoverMenuItem
                            onSelect={() => p?.setTagsEdit(true)}
                            icon={<Settings2Icon className="w-3 h-3" />}
                        >
                            Manage tags
                        </PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>
            </SectionBar>

            <PageBody>
                {emailsData.isLoading ? (
                    <div className="divide-y divide-slate-200/60">
                        {Array.from({ length: 6 }).map((_, i) => (
                            <div key={i} className="h-11 px-5 flex items-center gap-3">
                                <div className="w-3.5 h-3.5 bg-slate-100 rounded" />
                                <div className="w-6 h-6 rounded-full bg-slate-100 shrink-0" />
                                <div className="h-3 w-52 bg-slate-100 rounded animate-pulse" />
                                <div className="ml-auto h-3 w-16 bg-slate-100 rounded animate-pulse" />
                            </div>
                        ))}
                    </div>
                ) : !emailsData.emails || emailsData.emails.length === 0 ? (
                    <EmptyBlock
                        title="No email accounts yet"
                        body="Connect your first mailbox to start warming up and sending campaigns."
                        cta={
                            <TopbarAction
                                onClick={() => p?.setAddEmail(true)}
                                icon={<PlusIcon className="w-3 h-3" />}
                            >
                                Add account
                            </TopbarAction>
                        }
                    />
                ) : (
                    <table className="w-full text-left">
                        <thead className="sticky top-0 bg-white z-[1]">
                            <tr className="border-b border-slate-200">
                                <th className="pl-5 pr-2 py-2 w-9">
                                    <input
                                        type="checkbox"
                                        className="w-3.5 h-3.5 rounded accent-sky-600"
                                        checked={isSelectedAll()}
                                        onChange={() => {
                                            if (isSelectedAll()) {
                                                setSelected((bef) =>
                                                    bef.filter((e) => !emailsData.emails.map((em) => em.id).includes(e)),
                                                );
                                            } else {
                                                setSelected((bef) => [
                                                    ...bef,
                                                    ...emailsData.emails
                                                        .filter((em) => !selected.includes(em.id))
                                                        .map((em) => em.id),
                                                ]);
                                            }
                                        }}
                                    />
                                </th>
                                <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Account</th>
                                <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-20 text-right">Sent</th>
                                <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-24 text-right">Warmup</th>
                                <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-28">Status</th>
                                <th className="px-3 py-2 w-16"></th>
                            </tr>
                        </thead>
                        <tbody>
                            {emailsData.emails.map((box) => {
                                const dot =
                                    box.status === "healthy"
                                        ? "bg-emerald-500"
                                        : box.status === "warming"
                                            ? "bg-amber-500"
                                            : "bg-red-500";
                                const statusText =
                                    box.status === "healthy"
                                        ? "text-emerald-600"
                                        : box.status === "warming"
                                            ? "text-amber-600"
                                            : "text-red-500";
                                return (
                                    <tr
                                        key={box.id}
                                        className="border-b border-slate-200/60 hover:bg-slate-50/80 transition-colors group h-11"
                                    >
                                        <td className="pl-5 pr-2">
                                            <input
                                                type="checkbox"
                                                className="w-3.5 h-3.5 rounded accent-sky-600"
                                                checked={selected.includes(box.id)}
                                                onChange={() => {
                                                    selected.includes(box.id)
                                                        ? setSelected((bef) => bef.filter((i) => i !== box.id))
                                                        : setSelected((bef) => [...bef, box.id]);
                                                }}
                                            />
                                        </td>
                                        <td className="px-3">
                                            <div className="flex items-center gap-2.5">
                                                <div className="w-6 h-6 rounded-full bg-sky-100 flex items-center justify-center shrink-0">
                                                    <span className="text-[9.5px] font-semibold text-sky-700">
                                                        {box.email.slice(0, 2).toUpperCase()}
                                                    </span>
                                                </div>
                                                <span className="text-[12.5px] font-medium text-slate-900 truncate">{box.email}</span>
                                            </div>
                                        </td>
                                        <td className="px-3 text-[12px] text-slate-500 tabular-nums text-right font-mono">0</td>
                                        <td className="px-3 text-[12px] text-slate-500 tabular-nums text-right font-mono">{box.warmup_base}</td>
                                        <td className="px-3">
                                            <span className={`inline-flex items-center gap-1.5 text-[11px] font-medium ${statusText}`}>
                                                <span className={`w-1.5 h-1.5 rounded-full ${dot}`} />
                                                <span className="uppercase tracking-[0.08em]">{box.status}</span>
                                            </span>
                                        </td>
                                        <td className="px-3">
                                            <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                                                <button
                                                    type="button"
                                                    onClick={() => setView(box.id)}
                                                    aria-label="Warmup settings"
                                                    className="w-6 h-6 flex items-center justify-center rounded hover:bg-slate-100 text-slate-400 hover:text-slate-700 transition-colors cursor-pointer"
                                                >
                                                    <RiFireLine className="w-3.5 h-3.5" />
                                                </button>
                                                <button
                                                    type="button"
                                                    className="w-6 h-6 flex items-center justify-center rounded hover:bg-slate-100 text-slate-400 hover:text-slate-700 transition-colors cursor-pointer"
                                                    onClick={() => setView(box.id)}
                                                    aria-label="Account details"
                                                >
                                                    <RiMoreLine className="w-3.5 h-3.5" />
                                                </button>
                                            </div>
                                        </td>
                                    </tr>
                                );
                            })}
                        </tbody>
                    </table>
                )}
            </PageBody>

            <InboxDetails emails={emailsData.emails} view={view} setView={setView} />
        </Page>
    );
}
