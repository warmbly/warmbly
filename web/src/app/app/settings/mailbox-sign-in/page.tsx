// Mailbox sign-in: the Partnership plan's own Google and Microsoft OAuth apps,
// which every Google or Microsoft mailbox sign-in in the workspace goes through.

import React from "react";
import toast from "react-hot-toast/headless";
import { CheckIcon, CopyIcon, ExternalLinkIcon } from "lucide-react";
import { Card, Section, SectionShell } from "../_components/SectionShell";
import { NoAccess } from "@/components/layout/NoAccess";
import { Label, TextInput } from "@/components/ui/field";
import { usePermission } from "@/hooks/usePermission";
import { useConfirm } from "@/hooks/context/confirm";
import { useRegisterUnsaved } from "@/hooks/context/unsaved";
import type { AutosaveHandle } from "@/hooks/useAutosave";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import { errorMessage } from "@/lib/errors/message";
import {
    useDeleteMailboxOAuthApp,
    useMailboxOAuthApps,
    useSaveMailboxOAuthApp,
} from "@/lib/api/hooks/app/emails/useMailboxOAuthApps";
import type { MailboxOAuthApp, MailboxOAuthProvider } from "@/lib/api/models/app/emails/MailboxOAuthApp";

import HelpLink from "@/components/shared/HelpLink";
const GUIDE_PAGE = "https://docs.warmbly.com/guides/partnership/";
const GUIDE = GUIDE_PAGE + "#your-own-oauth-app";

const PROVIDERS: { id: MailboxOAuthProvider; label: string; clientHint: string; anchor: string }[] = [
    { id: "gmail", label: "Google", clientHint: "1234567890-abc.apps.googleusercontent.com", anchor: "#google" },
    { id: "outlook", label: "Microsoft", clientHint: "Application (client) ID, a GUID", anchor: "#microsoft" },
];

export default function MailboxSignInPage() {
    const canManage = usePermission("MANAGE_EMAILS");
    const access = useFeatureAccess();
    const partner = access.partnership;
    // A workspace off the plan still maintains an app it saved, since that app refreshes its mailboxes.
    const apps = useMailboxOAuthApps(canManage && !partner);
    const keeps = (apps.data?.data.length ?? 0) > 0;
    if (!canManage) return <NoAccess feature="Mailbox sign-in" permissionLabel="Manage mailboxes" />;
    if (!access.loading && !partner && apps.isSuccess && !keeps) {
        return (
            <SectionShell title="Mailbox sign-in" description="Your own Google and Microsoft OAuth apps.">
                <Section eyebrow="Partnership plan" description="Signing mailboxes in through your own OAuth app is part of the Partnership plan. Ask Warmbly to offer it to this workspace.">
                    <span />
                </Section>
            </SectionShell>
        );
    }
    return <MailboxSignInView partner={partner} />;
}

function MailboxSignInView({ partner }: { partner: boolean }) {
    const { data, isLoading, isError, refetch } = useMailboxOAuthApps();
    return (
        <SectionShell
            title="Mailbox sign-in"
            description="Google and Microsoft mailboxes sign in through your own OAuth app, so the consent screen shows your name and logo."
            actions={
                <HelpLink
                    href={GUIDE}
                    className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-slate-50"
                    portalLabel={<>Get help <ExternalLinkIcon className="w-3.5 h-3.5" /></>}
                >
                    Setup guide <ExternalLinkIcon className="w-3.5 h-3.5" />
                </HelpLink>
            }
        >
            <Section
                eyebrow="Apps"
                description="On the Partnership plan these are the only way to sign in a Google or Microsoft mailbox. Mailboxes added with an app password or SMTP/IMAP need neither."
            >
                {isLoading ? (
                    <div className="h-40 rounded-md bg-slate-100 animate-pulse" />
                ) : isError || !data ? (
                    <div className="text-[12.5px] text-slate-600">
                        Your apps could not be loaded.{" "}
                        <button type="button" onClick={() => void refetch()} className="text-sky-700 hover:underline">Try again</button>
                    </div>
                ) : (
                    <div className="grid gap-3 lg:grid-cols-2">
                        {PROVIDERS.filter((p) => partner || data.data.some((a) => a.provider === p.id)).map((p) => (
                            <ProviderCard
                                key={p.id}
                                provider={p}
                                app={data.data.find((a) => a.provider === p.id)}
                                callback={data.callbacks[p.id] ?? ""}
                            />
                        ))}
                    </div>
                )}
            </Section>
        </SectionShell>
    );
}

function CopyValue({ value }: { value: string }) {
    const [copied, setCopied] = React.useState(false);
    return (
        <div className="flex items-center gap-1.5 min-w-0">
            <code className="min-w-0 flex-1 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 leading-7 text-[11.5px] text-slate-700" title={value}>
                {value || "Not configured on this deployment"}
            </code>
            <button
                type="button"
                disabled={!value}
                aria-label="Copy"
                onClick={async () => {
                    try {
                        await navigator.clipboard.writeText(value);
                        setCopied(true);
                        setTimeout(() => setCopied(false), 1500);
                    } catch {
                        toast.error("Could not copy");
                    }
                }}
                className="h-7 w-7 shrink-0 inline-flex items-center justify-center rounded-md border border-slate-200 text-slate-500 hover:bg-slate-50 disabled:opacity-40"
            >
                {copied ? <CheckIcon className="w-3.5 h-3.5 text-emerald-600" /> : <CopyIcon className="w-3.5 h-3.5" />}
            </button>
        </div>
    );
}

function ProviderCard({
    provider,
    app,
    callback,
}: {
    provider: (typeof PROVIDERS)[number];
    app?: MailboxOAuthApp;
    callback: string;
}) {
    const save = useSaveMailboxOAuthApp();
    const remove = useDeleteMailboxOAuthApp();
    const confirm = useConfirm();
    const [appName, setAppName] = React.useState(app?.app_name ?? "");
    const [clientId, setClientId] = React.useState(app?.client_id ?? "");
    const [secret, setSecret] = React.useState("");
    const [redirect, setRedirect] = React.useState(app?.custom_redirect ? app.redirect_url : "");

    React.useEffect(() => {
        setAppName(app?.app_name ?? "");
        setClientId(app?.client_id ?? "");
        setRedirect(app?.custom_redirect ? app.redirect_url : "");
        setSecret("");
    }, [app?.id, app?.updated_at]); // eslint-disable-line react-hooks/exhaustive-deps

    const sameClient = !!app && clientId.trim() === app.client_id;
    const keepsSecret = sameClient && !!app?.has_secret;
    const needsSecret = !keepsSecret && !secret.trim();
    const missing = !appName.trim() ? "Give the app a name." : !clientId.trim() ? "Paste the client ID." : needsSecret ? "Paste the client secret." : "";
    const lockedClient = !!app && app.mailboxes > 0 && !sameClient;

    const savedRedirect = app?.custom_redirect ? app.redirect_url : "";
    const dirty = appName.trim() !== (app?.app_name ?? "") || clientId.trim() !== (app?.client_id ?? "") ||
        secret.trim() !== "" || redirect.trim() !== savedRedirect;

    // Throws on a refused or failed save, so leaving the page through the unsaved guard keeps the draft.
    async function persist() {
        if (missing) {
            toast.error(missing);
            throw new Error(missing);
        }
        try {
            await save.mutateAsync({
                provider: provider.id,
                data: { app_name: appName.trim(), client_id: clientId.trim(), client_secret: secret.trim(), redirect_url: redirect.trim() },
            });
        } catch (e) {
            toast.error(errorMessage(e, `Could not save the ${provider.label} app`));
            throw e;
        }
        setSecret("");
        toast.success(`${provider.label} app saved`);
    }

    function discard() {
        setAppName(app?.app_name ?? "");
        setClientId(app?.client_id ?? "");
        setRedirect(savedRedirect);
        setSecret("");
    }

    const draft: AutosaveHandle<null> = {
        status: save.isPending ? "saving" : "idle",
        dirty,
        flush: persist,
        retry: () => void persist().catch(() => undefined),
        savedValue: null,
        markSaved: () => undefined,
    };
    useRegisterUnsaved(draft, discard);

    function onSave() {
        void persist().catch(() => undefined);
    }

    function onRemove() {
        if (!app) return;
        confirm.show(`Remove your ${provider.label} app? New ${provider.label} sign-ins will need another app.`, async () => {
            try {
                await remove.mutateAsync(provider.id);
                toast.success(`${provider.label} app removed`);
            } catch (e) {
                toast.error(errorMessage(e, `Could not remove the ${provider.label} app`));
            }
        });
    }

    const signedIn = !app ? "" : app.mailboxes === 1 ? "1 mailbox signed in" : `${app.mailboxes} mailboxes signed in`;
    const status = !app ? "Not set up" : app.has_secret ? signedIn : `${signedIn}. Save the client secret again: it did not come with the workspace import.`;

    return (
        <Card
            title={provider.label}
            description={status}
            footer={
                <>
                    <button
                        type="button"
                        onClick={onSave}
                        disabled={save.isPending || lockedClient}
                        title={lockedClient ? "Mailboxes are signed in through the current client ID. Save a new secret for it, or remove those mailboxes first." : missing || undefined}
                        className="h-7 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700 disabled:opacity-50"
                    >
                        {save.isPending ? "Checking…" : app ? "Save" : "Add app"}
                    </button>
                    {app && (
                        <button
                            type="button"
                            onClick={onRemove}
                            disabled={app.mailboxes > 0}
                            title={app.mailboxes > 0 ? "Mailboxes are signed in through this app. Remove them or sign them in through another app first." : undefined}
                            className="ml-auto h-7 px-3 rounded-md border border-slate-200 text-[12.5px] text-red-600 hover:bg-red-50 disabled:opacity-50 disabled:hover:bg-transparent"
                        >
                            Remove
                        </button>
                    )}
                </>
            }
        >
            <div className="space-y-3">
                <div>
                    <Label>Register this redirect URI</Label>
                    <CopyValue value={app?.custom_redirect ? app.redirect_url : callback} />
                    <p className="mt-1 text-[11.5px] text-slate-500">
                        Add it to the app in {provider.id === "gmail" ? "Google Cloud" : "Microsoft Entra"} exactly as shown.{" "}
                        <HelpLink href={GUIDE_PAGE + provider.anchor} className="text-sky-700 hover:underline" portalLabel="Get help">How</HelpLink>
                    </p>
                </div>
                <div>
                    <Label>App name</Label>
                    <TextInput value={appName} onChange={setAppName} placeholder="Shown to mailbox owners on the sign-in link page" maxLength={64} />
                </div>
                <div>
                    <Label>Client ID</Label>
                    <TextInput value={clientId} onChange={setClientId} placeholder={provider.clientHint} autoComplete="off" invalid={lockedClient} title={lockedClient ? "Mailboxes are signed in through the current client ID." : undefined} />
                </div>
                <div>
                    <Label>Client secret</Label>
                    <TextInput type="password" value={secret} onChange={setSecret} placeholder={keepsSecret ? "Saved. Paste a new one to replace it." : "Paste the secret"} autoComplete="new-password" />
                </div>
                <div>
                    <Label>Your own redirect URI (optional)</Label>
                    <TextInput value={redirect} onChange={setRedirect} placeholder="https://yourdomain.com/oauth/callback" autoComplete="off" />
                    <p className="mt-1 text-[11.5px] text-slate-500 break-all">
                        Only when the provider needs the callback on your verified domain. That page must forward its whole query string to {callback || "this deployment's callback"}.
                    </p>
                </div>
            </div>
        </Card>
    );
}
