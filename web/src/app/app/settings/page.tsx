import { Separator } from "@/components/ui/separator";
import { Page, PageHeader } from "@/components/layout/Page";

export default function SettingsPage() {
    return (
        <Page width="default">
            <PageHeader
                title="Settings"
                subtitle="Manage your account."
            />

            <div className="rounded-md border border-slate-200 bg-white divide-y divide-slate-100">
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
                    <button className="h-7 px-2.5 rounded-md bg-slate-900 text-white hover:bg-slate-800 text-[12.5px] font-medium transition-colors">
                        Save changes
                    </button>
                </div>
            </div>
        </Page>
    );
}
