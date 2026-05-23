// Billing — plan + usage + invoices preview.
//
// Distinct from Settings (form-heavy) and the empty-state pages —
// dominated by a plan card on the left + a usage strip on the right
// + a fake-invoices preview. Reads as "money", not "config".

import { ArrowUpRightIcon, FileTextIcon, SparklesIcon } from "lucide-react";
import { Link } from "react-router-dom";
import {
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
    TopbarAction,
} from "@/components/layout/Page";
import { comingSoon } from "@/lib/helper/comingSoon";

const SAMPLE_INVOICES = [
    { number: "INV-2026-001", amount: "$0.00", status: "Trial", date: "May 22" },
];

export default function BillingPage() {
    return (
        <Page>
            <PageTopbar eyebrow="Billing" subtitle="Plan · usage · invoices" />

            <StatStrip cols={4}>
                <Stat label="Mailboxes" value={0} sub="of unlimited" />
                <Stat label="Sends (mo)" value={0} sub="this month" />
                <Stat label="Warmup pool" value="free" sub="shared" />
                <Stat label="API calls (mo)" value={0} sub="free tier" last />
            </StatStrip>

            <SectionBar label="Current plan" />
            <div className="px-5 py-4 border-b border-slate-200/60">
                <div className="rounded-md border border-slate-200 bg-white p-4 flex items-start gap-4 max-w-2xl">
                    <div className="size-9 rounded-md bg-slate-100 text-slate-600 flex items-center justify-center shrink-0">
                        <SparklesIcon className="w-4 h-4" />
                    </div>
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                            <span className="text-[14px] font-semibold text-slate-900">Free</span>
                            <span className="inline-flex items-center text-[10px] rounded px-1.5 h-4 bg-slate-100 text-slate-500 uppercase tracking-[0.1em] font-medium">
                                current
                            </span>
                        </div>
                        <p className="text-[11.5px] text-slate-500 mt-0.5 leading-relaxed max-w-md">
                            For trying Warmbly out. Connect one mailbox, send up to
                            50 cold emails / day. Shared warmup pool. No team
                            seats.
                        </p>
                        <div className="mt-3 grid grid-cols-3 gap-x-4 gap-y-2 max-w-md text-[11.5px]">
                            <Bullet label="Mailboxes" value="1" />
                            <Bullet label="Sends / day" value="50" />
                            <Bullet label="Warmup" value="Shared free" />
                            <Bullet label="Team seats" value="—" />
                            <Bullet label="Realtime" value="✓" />
                            <Bullet label="Webhooks" value="—" />
                        </div>
                    </div>
                    <TopbarAction
                        icon={<SparklesIcon className="w-3 h-3" />}
                        onClick={() => comingSoon("Upgrade flow")}
                    >
                        Upgrade
                    </TopbarAction>
                </div>
                <Link
                    to="/#pricing"
                    className="inline-flex items-center gap-1 text-[11.5px] text-slate-500 hover:text-slate-900 transition-colors mt-3"
                >
                    <ArrowUpRightIcon className="w-3 h-3" />
                    Compare every plan
                </Link>
            </div>

            <SectionBar label="Payment method" />
            <div className="px-5 py-3 border-b border-slate-200/60 max-w-2xl">
                <div className="rounded-md border border-dashed border-slate-300 bg-slate-50/40 px-3 py-3 flex items-center gap-3">
                    <span className="size-7 rounded-md bg-slate-200 text-slate-500 flex items-center justify-center text-[10px] font-bold">
                        ••
                    </span>
                    <div className="min-w-0 flex-1">
                        <div className="text-[12px] text-slate-700">No card on file</div>
                        <div className="text-[11px] text-slate-400">
                            Add a card to unlock paid plans
                        </div>
                    </div>
                    <button
                        type="button"
                        onClick={() => comingSoon("Payment methods")}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors shrink-0"
                    >
                        Add card
                    </button>
                </div>
            </div>

            <SectionBar label="Invoices" count={SAMPLE_INVOICES.length} />
            <PageBody>
                <div className="divide-y divide-slate-200/60">
                    {SAMPLE_INVOICES.map((inv) => (
                        <div key={inv.number} className="h-11 px-5 flex items-center gap-3">
                            <FileTextIcon className="w-3.5 h-3.5 text-slate-400" />
                            <span className="font-mono text-[11.5px] text-slate-900">
                                {inv.number}
                            </span>
                            <span className="text-[11.5px] text-slate-500">{inv.status}</span>
                            <span className="ml-auto font-mono text-[11.5px] text-slate-900 tabular-nums">
                                {inv.amount}
                            </span>
                            <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                                {inv.date}
                            </span>
                        </div>
                    ))}
                </div>
            </PageBody>
        </Page>
    );
}

function Bullet({ label, value }: { label: string; value: string }) {
    return (
        <div className="flex items-baseline gap-1.5 min-w-0">
            <span className="text-slate-400 truncate">{label}</span>
            <span className="text-slate-900 font-medium ml-auto tabular-nums">{value}</span>
        </div>
    );
}
