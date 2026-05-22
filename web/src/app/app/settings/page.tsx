import { Separator } from "@/components/ui/separator";
import { Page, PageHeader } from "@/components/layout/Page";

export default function SettingsPage() {
    return (
        <Page width="default">
            <PageHeader
                title="Settings"
                subtitle="Manage your account."
            />

            <div className="rounded-xl border border-slate-200/80 bg-white divide-y divide-slate-100">
                <div className="p-5">
                    <h2 className="text-[13.5px] font-medium text-slate-900 mb-0.5">Profile</h2>
                    <p className="text-xs text-slate-400">Update your personal information.</p>
                </div>
                <div className="p-5 space-y-4">
                    <div>
                        <label className="text-[13px] text-slate-600 block mb-1.5">Email</label>
                        <input
                            type="email"
                            placeholder="your@email.com"
                            disabled
                            className="w-full h-9 px-3 rounded-lg border border-slate-200 bg-slate-50 text-[13px] text-slate-400"
                        />
                    </div>
                    <Separator />
                    <button className="bg-sky-600 text-white hover:bg-sky-700 rounded-lg px-3 py-1.5 text-[13px] font-medium transition-colors">
                        Save Changes
                    </button>
                </div>
            </div>
        </Page>
    );
}
