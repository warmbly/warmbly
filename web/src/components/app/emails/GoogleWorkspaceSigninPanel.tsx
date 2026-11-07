// GoogleWorkspaceSigninPanel: one Google Workspace mailbox through Google sign-in.
//
// The domain's administrator marks Warmbly's OAuth client Trusted once, which is
// what lets mailboxes on that domain sign in to it; then each mailbox signs in
// on its own. A personal Gmail address goes to the app-password walkthrough.

import React from "react";
import { motion } from "framer-motion";
import { CheckIcon, ExternalLinkIcon, KeyRoundIcon, Loader2Icon, ShieldAlertIcon, ShieldCheckIcon } from "lucide-react";

import { Google } from "@/components/svg";
import { TextInput } from "@/components/ui/field";
import { CopyValue } from "@/components/app/emails/import/parts";
import { Crumbs, Instruction } from "@/components/app/emails/import/grants/GrantSetupSteps";
import { isPersonalGmail } from "@/lib/mailHost";

/** Admin console > Security > Access and data control > API controls > Manage Third-Party App Access. */
export const GOOGLE_THIRD_PARTY_APPS_URL = "https://admin.google.com/ac/owl/list?tab=configuredApps";

const THIRD_PARTY_PATH = ["Security", "Access and data control", "API controls", "Manage Third-Party App Access"];

const ADDRESS_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export default function GoogleWorkspaceSigninPanel({
    busy,
    viaCloud,
    clientId,
    unfinished,
    onConnect,
    onPersonalGmail,
    onUseAppPassword,
}: {
    busy: boolean;
    viaCloud: boolean;
    /** The OAuth client a Workspace admin trusts; absent when the instance did not report one. */
    clientId?: string;
    /** The last sign-in window closed or was denied without connecting. */
    unfinished: boolean;
    onConnect: (address: string) => void;
    onPersonalGmail: (address: string) => void;
    onUseAppPassword: (address: string) => void;
}) {
    const [address, setAddress] = React.useState("");
    const trimmed = address.trim();
    const personal = isPersonalGmail(trimmed);
    const valid = ADDRESS_RE.test(trimmed);
    // Brokered through Warmbly Cloud, the consent runs on the cloud's client, not this one.
    const showAdmin = !viaCloud;

    function submit() {
        if (busy || !valid) return;
        if (personal) onPersonalGmail(trimmed);
        else onConnect(trimmed);
    }

    return (
        <div className="px-5 py-5 space-y-4">
            <div className="flex items-center gap-3">
                <div className="size-11 rounded-md border border-slate-200 bg-white flex items-center justify-center shrink-0">
                    <Google className="w-6 h-6" />
                </div>
                <div className="min-w-0">
                    <div className="text-[13.5px] font-medium text-slate-900">Sign in a Google Workspace mailbox</div>
                    <div className="text-[11.5px] text-slate-500">
                        {viaCloud
                            ? "Warmbly Cloud opens the Google window on its own app. Approve and you're done."
                            : "Your Workspace admin trusts Warmbly once, then each mailbox signs in with Google."}
                    </div>
                </div>
            </div>

            {showAdmin && (
                <div className="rounded-md border border-slate-200 bg-white px-3 py-3 space-y-3">
                    <div className="flex items-center gap-2 min-w-0">
                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium shrink-0">Step 1</span>
                        <span className="text-[12.5px] font-medium text-slate-900 min-w-0">Your Workspace admin trusts Warmbly, once per domain</span>
                    </div>
                    <p className="text-[11.5px] text-slate-500 leading-relaxed">
                        Marking Warmbly as a trusted app is what lets mailboxes on your domain connect. Without it, Google can show
                        &ldquo;Access blocked&rdquo; when a mailbox signs in. Already done for this domain? Go on to step 2.
                    </p>
                    <TrustedAppSteps clientId={clientId} />
                </div>
            )}

            <div className="space-y-2">
                {showAdmin && (
                    <div className="flex items-center gap-2">
                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Step 2</span>
                        <span className="text-[12.5px] font-medium text-slate-900">Sign in the mailbox</span>
                    </div>
                )}
                <TextInput
                    value={address}
                    onChange={setAddress}
                    placeholder="alex@yourcompany.com"
                    type="email"
                    autoComplete="email"
                    className="w-full"
                    onKeyDown={(e) => {
                        if (e.key === "Enter") submit();
                    }}
                />
                {personal && (
                    <div className="rounded-md border border-sky-200 bg-sky-50 px-3 py-2 flex items-start gap-2">
                        <KeyRoundIcon className="w-3.5 h-3.5 text-sky-600 mt-0.5 shrink-0" />
                        <p className="text-[11.5px] text-sky-800 leading-relaxed">
                            This is a personal Gmail address. Personal Gmail connects with an app password over IMAP and SMTP, not with Google
                            sign-in. Continue and Warmbly walks you through it.
                        </p>
                    </div>
                )}
            </div>

            <ul className="text-[11.5px] text-slate-600 space-y-1.5 px-1">
                <Scope>Send and read mail on the mailbox's behalf</Scope>
                <Scope>Track replies and deliveries</Scope>
                <Scope>Refresh tokens are stored encrypted; revoke any time</Scope>
            </ul>

            <div className="space-y-1.5">
                <motion.button
                    type="button"
                    onClick={submit}
                    disabled={busy || !valid}
                    whileTap={busy || !valid ? undefined : { scale: 0.985 }}
                    className="w-full h-9 rounded-md text-[12.5px] font-medium inline-flex items-center justify-center gap-2 transition-colors disabled:opacity-60 disabled:cursor-not-allowed bg-slate-900 hover:bg-slate-800 text-white"
                >
                    {busy ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : personal ? <KeyRoundIcon className="w-3.5 h-3.5" /> : <ShieldCheckIcon className="w-3.5 h-3.5" />}
                    {busy ? "Waiting for authorization…" : personal ? "Continue with an app password" : "Continue with Google"}
                </motion.button>
                {!valid && !busy && <p className="text-[11px] text-slate-500 text-center">Enter the mailbox's address to continue.</p>}
            </div>

            {unfinished && !viaCloud && <BlockedByAdminHint clientId={clientId} />}

            <p className="text-[11.5px] text-slate-500 text-center">
                Prefer not to use Google sign-in?{" "}
                <button
                    type="button"
                    onClick={() => onUseAppPassword(trimmed)}
                    className="text-sky-700 underline decoration-sky-300 hover:decoration-sky-600 transition-colors"
                >
                    Connect it with an app password instead
                </button>
            </p>
        </div>
    );
}

/** The Admin console steps that mark Warmbly's OAuth client Trusted for a Workspace domain. */
export function TrustedAppSteps({ clientId }: { clientId?: string }) {
    return (
        <ol className="space-y-3">
            <Instruction n={1}>
                <p>
                    Sign in to <span className="font-medium text-slate-900">admin.google.com</span> as an administrator.
                </p>
                <a
                    href={GOOGLE_THIRD_PARTY_APPS_URL}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="h-7 px-2.5 rounded-md border border-slate-200 bg-white hover:bg-slate-50 text-slate-700 text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    <ExternalLinkIcon className="w-3 h-3" />
                    Open Google Admin
                </a>
            </Instruction>
            <Instruction n={2}>
                <p>Go to</p>
                <Crumbs items={THIRD_PARTY_PATH} />
                <p>
                    and click <span className="font-medium text-slate-900">Configure new app</span> (or{" "}
                    <span className="font-medium text-slate-900">Add app &gt; OAuth App Name or Client ID</span>).
                </p>
            </Instruction>
            <Instruction n={3}>
                <p>Search by this client ID and select Warmbly.</p>
                {clientId ? (
                    <CopyValue label="Client ID" value={clientId} />
                ) : (
                    <p className="text-amber-700">This instance did not report its client ID. Ask the operator for it.</p>
                )}
            </Instruction>
            <Instruction n={4}>
                <p>
                    Choose <span className="font-medium text-slate-900">All users</span> (or the organizational unit that holds the
                    mailboxes), then <span className="font-medium text-slate-900">Trusted</span>, and finish.
                </p>
            </Instruction>
        </ol>
    );
}

/** Shown after a sign-in window closed without connecting: the usual cause on Workspace is an untrusted app. */
export function BlockedByAdminHint({ clientId }: { clientId?: string }) {
    return (
        <motion.div
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.18, ease: "easeOut" }}
            className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2.5 space-y-2"
        >
            <div className="flex items-start gap-2">
                <ShieldAlertIcon className="w-3.5 h-3.5 text-amber-600 mt-0.5 shrink-0" />
                <div className="min-w-0 space-y-1">
                    <div className="text-[12px] font-medium text-amber-900">Google said the app is blocked by your admin?</div>
                    <p className="text-[11.5px] text-amber-800 leading-relaxed">
                        Your Workspace admin has to add Warmbly&apos;s client ID as a trusted app under Manage Third-Party App Access, for All
                        users, then the mailbox can sign in again.
                    </p>
                </div>
            </div>
            {clientId && <CopyValue label="Client ID for your admin" value={clientId} />}
        </motion.div>
    );
}

function Scope({ children }: { children: React.ReactNode }) {
    return (
        <li className="flex items-start gap-2">
            <CheckIcon className="w-3 h-3 text-slate-400 mt-1 shrink-0" />
            <span>{children}</span>
        </li>
    );
}
