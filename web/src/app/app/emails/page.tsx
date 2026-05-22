import { RiFireLine, RiMoreLine, RiPriceTag3Line, RiSoundModuleLine } from "@remixicon/react";
import React, { useMemo } from "react";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import { useUserProfile } from "@/hooks/context/user";
import InboxDetails from "@/components/app/emails/InboxDetails";
import HeadSelectMenu from "@/components/app/head/HeadSelectMenu";
import SelectOption from "@/components/app/popup/select/SelectOption";
import type Tag from "@/lib/api/models/app/Tag";
import {
    AlertCircleIcon,
    FilterIcon,
    FlameIcon,
    MailIcon,
    PlusIcon,
    ShieldCheckIcon,
} from "lucide-react";
import Search from "@/components/app/Search";
import { EmptyState, Page, PageHeader, StatCard, StatRow } from "@/components/layout/Page";

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
        const f = p.user.folders.find((f) => f.id === tag);
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
        <Page width="wide">
            <PageHeader
                title="Accounts"
                subtitle="One row per mailbox. Status, warmup, and recent sends at a glance."
            >
                <button
                    onClick={() => p?.setAddEmail(true)}
                    className="flex items-center gap-1.5 h-7 px-2.5 rounded-md bg-slate-900 text-white hover:bg-slate-800 text-[12.5px] font-medium transition-colors"
                >
                    <PlusIcon className="w-3.5 h-3.5" />
                    <span>Add account</span>
                </button>
            </PageHeader>

            <StatRow>
                <StatCard
                    icon={<MailIcon className="w-3 h-3" />}
                    label="Total"
                    value={stats.total}
                />
                <StatCard
                    icon={<ShieldCheckIcon className="w-3 h-3" />}
                    label="Healthy"
                    value={stats.healthy}
                />
                <StatCard
                    icon={<FlameIcon className="w-3 h-3" />}
                    label="Warming"
                    value={stats.warming}
                />
                <StatCard
                    icon={<AlertCircleIcon className="w-3 h-3" />}
                    label="Needs attention"
                    value={stats.issues}
                />
            </StatRow>

            <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
                <div className="flex items-center justify-between px-3 py-2 border-b border-slate-200">
                    <div className="flex items-baseline gap-2">
                        <h2 className="text-[12.5px] font-semibold text-slate-700">Accounts</h2>
                        {emailsData.emails && (
                            <span className="text-[11px] text-slate-400 tabular-nums">{emailsData.emails.length}</span>
                        )}
                    </div>
                    <div className="flex items-center gap-2">
                        <form
                            onSubmit={(e) => {
                                e.preventDefault();
                                setQuery((e.target as HTMLFormElement).querySelector("input")?.value || "");
                            }}
                            className="hidden sm:block"
                        >
                            <Search value={query} onChange={(v) => setQuery(v)} />
                        </form>
                        <HeadSelectMenu icon={<FilterIcon className="w-3.5 h-3.5" />} title={stag.title}>
                            {p?.user.folders.map((fo) => (
                                <SelectOption
                                    key={fo.id}
                                    onClick={async () => (tag !== fo.id ? setTag(fo.id) : setTag(""))}
                                    color={fo.color}
                                    selected={tag === fo.id}
                                >
                                    <RiPriceTag3Line className="w-3.5 h-3.5" />
                                    <span className="truncate">{fo.title}</span>
                                </SelectOption>
                            ))}
                            <SelectOption onClick={() => p?.setTagsEdit(true)}>
                                <RiSoundModuleLine className="w-3.5 h-3.5" />
                                <span className="truncate">Manage tags</span>
                            </SelectOption>
                        </HeadSelectMenu>
                    </div>
                </div>

                {emailsData.emails && emailsData.emails.length === 0 ? (
                    <div className="p-6">
                        <EmptyState
                            icon={<MailIcon className="w-5 h-5" />}
                            title="No email accounts yet"
                            description="Connect your first mailbox to start warming up and sending campaigns."
                        >
                            <button
                                onClick={() => p?.setAddEmail(true)}
                                className="flex items-center gap-1.5 h-7 px-2.5 rounded-md bg-slate-900 text-white hover:bg-slate-800 text-[12.5px] font-medium transition-colors"
                            >
                                <PlusIcon className="w-3.5 h-3.5" />
                                Add account
                            </button>
                        </EmptyState>
                    </div>
                ) : emailsData.isLoading ? (
                    <div className="p-4 space-y-2">
                        {[...Array(5)].map((_, i) => (
                            <div key={i} className="h-12 bg-slate-100 animate-pulse rounded-lg" />
                        ))}
                    </div>
                ) : (
                    <table className="w-full text-left">
                        <thead>
                            <tr className="border-b border-slate-100">
                                <th className="pl-4 pr-2 py-2.5 w-10">
                                    <input
                                        type="checkbox"
                                        className="w-3.5 h-3.5 rounded accent-slate-900"
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
                                <th className="px-3 py-2.5 text-[11px] font-medium text-slate-400 uppercase tracking-wider">Account</th>
                                <th className="px-3 py-2.5 text-[11px] font-medium text-slate-400 uppercase tracking-wider">Sent</th>
                                <th className="px-3 py-2.5 text-[11px] font-medium text-slate-400 uppercase tracking-wider">Warmup</th>
                                <th className="px-3 py-2.5 text-[11px] font-medium text-slate-400 uppercase tracking-wider">Status</th>
                                <th className="px-3 py-2.5 w-20"></th>
                            </tr>
                        </thead>
                        <tbody>
                            {emailsData.emails.map((box) => (
                                <tr
                                    key={box.id}
                                    className="border-b border-slate-50 last:border-0 hover:bg-slate-50/80 transition-colors group"
                                >
                                    <td className="pl-4 pr-2 py-3">
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
                                    <td className="px-3 py-3">
                                        <div className="flex items-center gap-2.5">
                                            <div className="w-6 h-6 rounded-md bg-slate-100 flex items-center justify-center shrink-0">
                                                <span className="text-[10px] font-medium text-slate-600">
                                                    {box.email.slice(0, 2).toUpperCase()}
                                                </span>
                                            </div>
                                            <span className="text-[12.5px] font-medium text-slate-900">{box.email}</span>
                                        </div>
                                    </td>
                                    <td className="px-3 py-3 text-[13px] text-slate-500 tabular-nums">0</td>
                                    <td className="px-3 py-3 text-[13px] text-slate-500 tabular-nums">{box.warmup_base}</td>
                                    <td className="px-3 py-3">
                                        <span
                                            className={`inline-flex items-center gap-1.5 text-xs font-medium ${
                                                box.status === "healthy"
                                                    ? "text-emerald-600"
                                                    : box.status === "warming"
                                                      ? "text-amber-600"
                                                      : "text-red-500"
                                            }`}
                                        >
                                            <span
                                                className={`w-1.5 h-1.5 rounded-full ${
                                                    box.status === "healthy"
                                                        ? "bg-emerald-500"
                                                        : box.status === "warming"
                                                          ? "bg-amber-500"
                                                          : "bg-red-500"
                                                }`}
                                            />
                                            {box.status}
                                        </span>
                                    </td>
                                    <td className="px-3 py-3">
                                        <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                                            <button className="w-7 h-7 flex items-center justify-center rounded-md hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-colors">
                                                <RiFireLine className="w-4 h-4" />
                                            </button>
                                            <button
                                                className="w-7 h-7 flex items-center justify-center rounded-md hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-colors cursor-pointer"
                                                onClick={() => setView(box.id)}
                                            >
                                                <RiMoreLine className="w-4 h-4" />
                                            </button>
                                        </div>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                )}
            </div>

            <InboxDetails emails={emailsData.emails} view={view} setView={setView} />
        </Page>
    );
}
