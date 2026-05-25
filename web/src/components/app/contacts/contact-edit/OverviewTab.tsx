// Overview tab — at-a-glance contact 360.
//
// Composition:
//   - Suppression card (only when suppressed)
//   - Engagement: six flat stat tiles with a thin ratio bar where
//     a ratio over Sent makes sense
//   - Latest activity rail
//   - Profile rows for the fields not already in the panel header

import {
    AlertOctagonIcon,
    BanIcon,
    MailIcon,
    MailOpenIcon,
    MailWarningIcon,
    MousePointerClickIcon,
    ReplyIcon,
} from "lucide-react";
import type ContactDetail from "@/lib/api/models/app/contacts/ContactDetail";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import { fmtAbsolute, fmtRelative } from "./format";

export default function OverviewTab({
    contact,
    detail,
    detailLoading,
}: {
    contact: Contact;
    detail?: ContactDetail;
    detailLoading: boolean;
}) {
    const eng = detail?.engagement;
    const supp = detail?.suppression;
    const sent = eng?.total_sent ?? 0;

    return (
        <div className="space-y-5">
            {supp && (
                <div className="rounded-md border border-red-200 bg-red-50/60 px-3 py-2.5 flex items-start gap-2">
                    <BanIcon className="w-3.5 h-3.5 text-red-600 mt-px shrink-0" />
                    <div className="min-w-0 flex-1">
                        <div className="text-[12px] font-medium text-red-900 leading-tight">
                            Suppressed ({supp.source})
                        </div>
                        <div className="text-[11px] text-red-700/90 mt-0.5">
                            {supp.reason || "No reason given"} · since{" "}
                            {fmtAbsolute(supp.created_at)}
                        </div>
                    </div>
                </div>
            )}

            <Section title="Engagement">
                <div className="grid grid-cols-3 gap-1.5">
                    <StatTile
                        icon={<MailIcon className="w-3 h-3" />}
                        label="Sent"
                        value={sent}
                        loading={detailLoading}
                    />
                    <StatTile
                        icon={<MailOpenIcon className="w-3 h-3" />}
                        label="Opened"
                        value={eng?.total_opened ?? 0}
                        loading={detailLoading}
                        ratioOf={sent}
                    />
                    <StatTile
                        icon={<MousePointerClickIcon className="w-3 h-3" />}
                        label="Clicked"
                        value={eng?.total_clicked ?? 0}
                        loading={detailLoading}
                        ratioOf={sent}
                    />
                    <StatTile
                        icon={<ReplyIcon className="w-3 h-3" />}
                        label="Replied"
                        value={eng?.total_replied ?? 0}
                        loading={detailLoading}
                        ratioOf={sent}
                        accent={
                            eng && eng.total_replied > 0 ? "positive" : undefined
                        }
                    />
                    <StatTile
                        icon={<MailWarningIcon className="w-3 h-3" />}
                        label="Bounced"
                        value={eng?.total_bounced ?? 0}
                        loading={detailLoading}
                        ratioOf={sent}
                        accent={
                            eng && eng.total_bounced > 0 ? "negative" : undefined
                        }
                    />
                    <StatTile
                        icon={<AlertOctagonIcon className="w-3 h-3" />}
                        label="Complained"
                        value={eng?.total_complained ?? 0}
                        loading={detailLoading}
                        ratioOf={sent}
                        accent={
                            eng && eng.total_complained > 0
                                ? "negative"
                                : undefined
                        }
                    />
                </div>
            </Section>

            <Section title="Latest activity">
                <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
                    <LatestRow
                        label="Last sent"
                        ts={eng?.last_sent_at}
                        icon={<MailIcon className="w-3 h-3" />}
                    />
                    <LatestRow
                        label="Last opened"
                        ts={eng?.last_opened_at}
                        icon={<MailOpenIcon className="w-3 h-3" />}
                    />
                    <LatestRow
                        label="Last clicked"
                        ts={eng?.last_clicked_at}
                        icon={<MousePointerClickIcon className="w-3 h-3" />}
                    />
                    <LatestRow
                        label="Last replied"
                        ts={eng?.last_replied_at}
                        icon={<ReplyIcon className="w-3 h-3" />}
                    />
                    <LatestRow
                        label="Last bounced"
                        ts={eng?.last_bounced_at}
                        icon={<MailWarningIcon className="w-3 h-3" />}
                    />
                </div>
            </Section>

            <Section title="Profile">
                <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
                    <ProfileRow
                        label="Company"
                        value={contact.company || "—"}
                    />
                    <ProfileRow label="Phone" value={contact.phone || "—"} />
                    <ProfileRow
                        label="Categories"
                        value={
                            contact.categories.length > 0 ? (
                                <span className="flex flex-wrap gap-1 justify-end">
                                    {contact.categories.map((c) => (
                                        <span
                                            key={c.id}
                                            className="inline-flex h-4 items-center px-1.5 rounded text-[10.5px] font-medium"
                                            style={{
                                                backgroundColor: `${c.color}1a`,
                                                color: c.color,
                                            }}
                                        >
                                            {c.title}
                                        </span>
                                    ))}
                                </span>
                            ) : (
                                "None"
                            )
                        }
                    />
                    <ProfileRow
                        label="Campaigns"
                        value={
                            contact.campaigns.length > 0
                                ? `${contact.campaigns.length} active`
                                : "None"
                        }
                    />
                </div>
            </Section>

            {Object.keys(contact.custom_fields || {}).length > 0 && (
                <Section title="Custom fields">
                    <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
                        {Object.entries(contact.custom_fields).map(([k, v]) => (
                            <ProfileRow key={k} label={k} value={v} mono />
                        ))}
                    </div>
                </Section>
            )}
        </div>
    );
}

function Section({
    title,
    children,
}: {
    title: string;
    children: React.ReactNode;
}) {
    return (
        <section>
            <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500 mb-2">
                {title}
            </h2>
            {children}
        </section>
    );
}

function StatTile({
    icon,
    label,
    value,
    loading,
    ratioOf,
    accent,
}: {
    icon: React.ReactNode;
    label: string;
    value: number;
    loading: boolean;
    ratioOf?: number;
    accent?: "positive" | "negative";
}) {
    const pct =
        ratioOf && ratioOf > 0 ? Math.round((value / ratioOf) * 100) : null;
    const valueTone =
        accent === "negative"
            ? "text-red-700"
            : accent === "positive"
              ? "text-emerald-700"
              : "text-slate-900";
    const barTone =
        accent === "negative"
            ? "bg-red-500/80"
            : accent === "positive"
              ? "bg-emerald-500/80"
              : "bg-slate-900/70";

    return (
        <div className="rounded-md border border-slate-200 bg-white px-2.5 py-2">
            <div className="text-[10px] uppercase tracking-[0.12em] text-slate-500 font-medium flex items-center gap-1">
                <span className="text-slate-400">{icon}</span>
                {label}
            </div>
            <div className="mt-1 flex items-baseline justify-between gap-1.5">
                <span
                    className={`text-[15px] font-semibold tabular-nums leading-none ${valueTone}`}
                >
                    {loading ? (
                        <span className="inline-block w-6 h-3.5 rounded bg-slate-100 animate-pulse align-middle" />
                    ) : (
                        value.toLocaleString()
                    )}
                </span>
                {pct !== null && (
                    <span className="text-[10px] text-slate-400 tabular-nums">
                        {pct}%
                    </span>
                )}
            </div>
            {pct !== null && (
                <div className="mt-1.5 h-0.5 rounded-full bg-slate-100 overflow-hidden">
                    <div
                        className={`h-full ${barTone} transition-all`}
                        style={{ width: `${Math.min(100, pct)}%` }}
                    />
                </div>
            )}
        </div>
    );
}

function LatestRow({
    label,
    ts,
    icon,
}: {
    label: string;
    ts?: string | null;
    icon: React.ReactNode;
}) {
    return (
        <div className="flex items-center gap-2 px-3 py-1.5 border-b last:border-b-0 border-slate-100">
            <span className={ts ? "text-slate-500" : "text-slate-300"}>
                {icon}
            </span>
            <div className="text-[11.5px] text-slate-700 flex-1">{label}</div>
            <div
                className={`text-[11.5px] tabular-nums ${
                    ts ? "text-slate-600" : "text-slate-300"
                }`}
            >
                {ts ? fmtRelative(ts) : "never"}
            </div>
        </div>
    );
}

function ProfileRow({
    label,
    value,
    mono,
}: {
    label: string;
    value: React.ReactNode;
    mono?: boolean;
}) {
    return (
        <div className="flex items-start gap-2 px-3 py-1.5 border-b last:border-b-0 border-slate-100">
            <div className="text-[11px] text-slate-500 w-24 shrink-0">
                {label}
            </div>
            <div
                className={`text-[12px] flex-1 text-right break-words text-slate-900 ${
                    mono ? "font-mono" : ""
                }`}
            >
                {value}
            </div>
        </div>
    );
}
