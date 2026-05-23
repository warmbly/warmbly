import { Page, PageBody, PageTopbar, SectionBar, TopbarAction } from "@/components/layout/Page";

export default function SettingsPage() {
    return (
        <Page>
            <PageTopbar eyebrow="Settings" subtitle="Account" />

            <SectionBar label="Profile" />
            <PageBody>
                <div className="px-5 py-5 max-w-lg space-y-4">
                    <div>
                        <label className="text-[11px] uppercase tracking-[0.14em] text-slate-400 font-medium block mb-1.5">
                            Email
                        </label>
                        <input
                            type="email"
                            placeholder="your@email.com"
                            disabled
                            className="w-full h-8 px-2.5 rounded-md border border-slate-200 bg-slate-50 text-[12.5px] text-slate-500"
                        />
                    </div>
                    <div className="pt-2">
                        <TopbarAction>Save changes</TopbarAction>
                    </div>
                </div>
            </PageBody>
        </Page>
    );
}
