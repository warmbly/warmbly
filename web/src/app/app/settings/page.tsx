// Settings — sectioned account/notification/security/danger page.
//
// Settings used to be a single Profile form. Different from every
// other tab because each row is a labelled form field grouped by
// SectionBar, and the page reads like an actual settings sheet —
// inline edits, hairline rows, slate-900 save buttons.

import React from "react";
import { Page, PageBody, PageTopbar, SectionBar, TopbarAction } from "@/components/layout/Page";
import { Label, TextInput } from "@/components/ui/field";
import { useUserProfile } from "@/hooks/context/user";
import { comingSoon } from "@/lib/helper/comingSoon";

export default function SettingsPage() {
    const { user } = useUserProfile();
    const [firstName, setFirstName] = React.useState(user.first_name ?? "");
    const [lastName, setLastName] = React.useState(user.last_name ?? "");

    return (
        <Page>
            <PageTopbar eyebrow="Settings" subtitle="Account · notifications · security" />

            <SectionBar label="Profile" />
            <div className="px-5 py-4 border-b border-slate-200/60 space-y-3 max-w-xl">
                <div className="grid grid-cols-2 gap-2">
                    <div>
                        <Label>First name</Label>
                        <TextInput value={firstName} onChange={setFirstName} className="w-full" />
                    </div>
                    <div>
                        <Label>Last name</Label>
                        <TextInput value={lastName} onChange={setLastName} className="w-full" />
                    </div>
                </div>
                <div>
                    <Label>Email</Label>
                    <input
                        type="email"
                        value={user.email}
                        disabled
                        className="w-full h-7 px-2.5 rounded-md border border-slate-200 bg-slate-50 text-[12.5px] text-slate-500"
                    />
                    <p className="text-[11px] text-slate-400 mt-1">
                        Email changes are coming soon — contact support to update for now.
                    </p>
                </div>
                <div className="pt-1">
                    <TopbarAction onClick={() => comingSoon("Profile editing")}>
                        Save profile
                    </TopbarAction>
                </div>
            </div>

            <SectionBar label="Notifications" />
            <div className="px-5 py-4 border-b border-slate-200/60 space-y-1 max-w-xl">
                <ToggleRow
                    label="Reply received"
                    description="Email + push when a recipient replies to a campaign"
                />
                <ToggleRow
                    label="Bounce detected"
                    description="Notify when a mailbox starts bouncing hard"
                />
                <ToggleRow
                    label="Spam complaint"
                    description="Immediate alert on any complaint event"
                    defaultOn
                />
                <ToggleRow
                    label="Weekly digest"
                    description="Monday summary of last week's send volume and replies"
                    defaultOn
                />
            </div>

            <SectionBar label="Security" />
            <div className="px-5 py-4 border-b border-slate-200/60 space-y-2 max-w-xl">
                <RowLink
                    title="Active sessions"
                    description="Devices currently signed in to your account"
                    cta="View sessions"
                />
                <RowLink
                    title="Two-factor authentication"
                    description="Add a one-time code to every sign-in"
                    cta="Enable 2FA"
                />
                <RowLink
                    title="Change password"
                    description="Use 12+ characters with mixed case and a number"
                    cta="Change"
                />
            </div>

            <SectionBar label="Danger zone" />
            <PageBody>
                <div className="px-5 py-4 max-w-xl space-y-2">
                    <div className="rounded-md border border-red-200 bg-red-50/40 p-3">
                        <div className="text-[12.5px] font-semibold text-red-700 mb-0.5">
                            Delete account
                        </div>
                        <p className="text-[11.5px] text-red-700/80 mb-2 leading-relaxed">
                            Permanently delete your account and every workspace you own.
                            This can't be undone.
                        </p>
                        <button
                            type="button"
                            onClick={() => comingSoon("Account deletion")}
                            className="h-7 px-2.5 rounded-md border border-red-300 hover:border-red-400 text-red-700 hover:text-red-800 hover:bg-red-100/60 text-[12px] font-medium transition-colors"
                        >
                            Delete account…
                        </button>
                    </div>
                </div>
            </PageBody>
        </Page>
    );
}

function ToggleRow({
    label,
    description,
    defaultOn,
}: {
    label: string;
    description: string;
    defaultOn?: boolean;
}) {
    const [on, setOn] = React.useState(!!defaultOn);
    return (
        <div className="flex items-center gap-3 py-2 group">
            <div className="min-w-0 flex-1">
                <div className="text-[12.5px] text-slate-900 font-medium leading-tight">
                    {label}
                </div>
                <div className="text-[11.5px] text-slate-500 leading-tight mt-0.5">
                    {description}
                </div>
            </div>
            <button
                type="button"
                onClick={() => setOn(!on)}
                role="switch"
                aria-checked={on}
                className={`relative h-4 w-7 rounded-full transition-colors shrink-0 ${
                    on ? "bg-slate-900" : "bg-slate-200"
                }`}
            >
                <span
                    className={`absolute top-0.5 left-0.5 size-3 rounded-full bg-white transition-transform ${
                        on ? "translate-x-3" : "translate-x-0"
                    }`}
                />
            </button>
        </div>
    );
}

function RowLink({
    title,
    description,
    cta,
}: {
    title: string;
    description: string;
    cta: string;
}) {
    return (
        <div className="flex items-center gap-3 py-2">
            <div className="min-w-0 flex-1">
                <div className="text-[12.5px] text-slate-900 font-medium leading-tight">
                    {title}
                </div>
                <div className="text-[11.5px] text-slate-500 leading-tight mt-0.5">
                    {description}
                </div>
            </div>
            <button
                type="button"
                onClick={() => comingSoon(title)}
                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors shrink-0"
            >
                {cta}
            </button>
        </div>
    );
}
