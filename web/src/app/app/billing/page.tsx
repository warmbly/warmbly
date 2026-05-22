import { SparklesIcon, ArrowUpRightIcon } from "lucide-react";
import { Page, PageHeader } from "@/components/layout/Page";

export default function BillingPage() {
    return (
        <Page width="default">
            <PageHeader
                title="Billing"
                subtitle="Manage your subscription and invoices."
            />

            <div className="rounded-xl border border-slate-200/80 bg-white">
                <div className="p-5 flex items-start justify-between">
                    <div>
                        <div className="flex items-center gap-2 mb-1">
                            <h2 className="text-[13.5px] font-medium text-slate-900">Free Plan</h2>
                            <span className="inline-flex items-center text-[11px] rounded-full px-1.5 py-0.5 bg-slate-100 text-slate-500">
                                Current
                            </span>
                        </div>
                        <p className="text-xs text-slate-400">Basic features for getting started.</p>
                    </div>
                    <button className="bg-sky-600 text-white hover:bg-sky-700 rounded-lg px-3 py-1.5 text-[13px] font-medium transition-colors flex items-center gap-1.5">
                        <SparklesIcon className="w-3.5 h-3.5" />
                        Upgrade
                    </button>
                </div>
                <div className="border-t border-slate-100 p-4">
                    <a className="flex items-center gap-1 text-xs text-slate-400 hover:text-slate-600 transition-colors">
                        <ArrowUpRightIcon className="w-3.5 h-3.5" />
                        <span>View all plans and pricing</span>
                    </a>
                </div>
            </div>
        </Page>
    );
}
