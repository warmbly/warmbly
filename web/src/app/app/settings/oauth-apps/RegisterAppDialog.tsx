// Register an OAuth app in three steps: what it is, what it can do and where
// it sends people back, and (optionally) the events it receives. Then the
// one-time credentials, with what to do next. Leaving a half-filled form, or
// the credentials before the secret was copied, asks first.

import React from "react";
import { createPortal } from "react-dom";
import toast from "react-hot-toast/headless";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowLeftIcon, CheckIcon, Loader2Icon, PlusIcon, XIcon } from "lucide-react";

import { AvatarUploader } from "@/components/app/avatar/AvatarUploader";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { CheckSquare } from "@/components/ui/check-square";
import { Label, TextInput } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";
import useAPIPermissions from "@/lib/api/hooks/app/api-keys/useAPIPermissions";
import { appNameError } from "@/lib/displayName";
import { useCreateOAuthApp, useSetOAuthAppLogo } from "@/lib/api/hooks/app/oauth/useOAuthApps";
import { useWebhookEventCatalog } from "@/lib/api/hooks/app/webhooks/useWebhooks";
import type APIPermission from "@/lib/api/models/app/apikeys/APIPermission";
import type { OAuthApplicationWithSecret } from "@/lib/api/models/app/oauth/OAuthApp";
import { APP_URL } from "@/lib/information";
import { cn } from "@/lib/utils";

import { CopyButton, EventPicker } from "./parts";

const STEPS = ["App", "Access", "Events"] as const;
type Preset = "read_only" | "full_access" | "custom";

function redirectProblem(raw: string): string | null {
    const v = raw.trim();
    if (!v) return null;
    let u: URL;
    try {
        u = new URL(v);
    } catch {
        return "Not a full address. Start with https://";
    }
    const loopback = u.hostname === "localhost" || u.hostname === "127.0.0.1" || u.hostname === "[::1]";
    if (u.protocol !== "https:" && !(u.protocol === "http:" && loopback)) return "Use https (http only for localhost)";
    if (u.hash) return "Remove the # part; it is never sent back";
    return null;
}

function hostOf(url: string): string {
    try {
        return new URL(url).hostname;
    } catch {
        return "";
    }
}

export default function RegisterAppDialog({ onClose }: { onClose: () => void }) {
    const create = useCreateOAuthApp();
    const setLogo = useSetOAuthAppLogo();
    const perms = useAPIPermissions();
    const catalog = useWebhookEventCatalog();
    const confirm = useConfirm();

    const [step, setStep] = React.useState(0);
    const [dir, setDir] = React.useState(1);
    const [tried, setTried] = React.useState<Record<number, boolean>>({});

    const [name, setName] = React.useState("");
    const [description, setDescription] = React.useState("");
    const [website, setWebsite] = React.useState("");
    const [logo, setLogoBlob] = React.useState<{ blob: Blob; url: string } | null>(null);
    const [preset, setPreset] = React.useState<Preset>("read_only");
    const [scopes, setScopes] = React.useState(0);
    const [redirects, setRedirects] = React.useState<string[]>([""]);
    const [webhooksOn, setWebhooksOn] = React.useState(false);
    const [webhookUrl, setWebhookUrl] = React.useState("");
    const [domainsEdited, setDomainsEdited] = React.useState<string | null>(null);
    const [webhookEvents, setWebhookEvents] = React.useState<string[]>([]);

    const [created, setCreated] = React.useState<OAuthApplicationWithSecret | null>(null);
    const [secretCopied, setSecretCopied] = React.useState(false);

    const presets = perms.data?.presets;
    // Key management is never offered to an app, so only the scopes an app may hold are listed.
    const appScopes = perms.data?.app_scopes ?? 0;
    const appPerms = React.useMemo(
        () => (perms.data?.permissions ?? []).filter((p) => (appScopes & p.value) === p.value),
        [perms.data, appScopes],
    );
    React.useEffect(() => {
        if (presets && preset !== "custom" && scopes === 0) setScopes(presets[preset] & appScopes);
    }, [presets, preset, scopes, appScopes]);

    const redirectList = redirects.map((r) => r.trim()).filter(Boolean);
    const redirectErrors = redirects.map(redirectProblem);
    const nameProblem = appNameError("Name", name);
    const websiteProblem = website.trim() && !/^https?:\/\/[^\s/]+/i.test(website.trim()) ? "Start with https://" : null;
    const webhookProblem =
        webhooksOn && webhookUrl.trim() && !/^https:\/\/[^\s/]+/i.test(webhookUrl.trim()) ? "Use an https address" : null;
    const webhookHost = hostOf(webhookUrl.trim());
    const domains = (domainsEdited ?? webhookHost)
        .split(/[\s,]+/)
        .map((d) => d.trim())
        .filter(Boolean);

    // Why a step cannot be left, shown beside the button rather than a disabled one.
    const problems: string[][] = [
        [nameProblem ?? "", websiteProblem ? "Fix the website address" : ""].filter(Boolean),
        [
            scopes === 0 ? "Choose at least one permission" : "",
            redirectList.length === 0 ? "Add the address people return to" : "",
            redirectErrors.some(Boolean) ? "Fix the return address" : "",
        ].filter(Boolean),
        [
            webhooksOn && !webhookUrl.trim() ? "Add the webhook address, or turn events off" : "",
            webhookProblem ? "Fix the webhook address" : "",
        ].filter(Boolean),
    ];

    const dirty =
        !created &&
        (name.trim() !== "" || description.trim() !== "" || website.trim() !== "" || !!logo || redirectList.length > 0 || webhookUrl.trim() !== "");

    const requestClose = React.useCallback(() => {
        if (created && !secretCopied) {
            confirm.show("You haven’t copied the client secret. It won’t be shown again; you would have to rotate it.", async () => onClose());
            return;
        }
        if (dirty) {
            confirm.show("Discard this app? Nothing has been registered yet.", async () => onClose());
            return;
        }
        onClose();
    }, [created, secretCopied, dirty, confirm, onClose]);

    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key !== "Escape") return;
            if (document.querySelector('[data-floating], [role="alertdialog"]')) return;
            requestClose();
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [requestClose]);

    function go(next: number) {
        if (next > step) {
            for (let i = step; i < next; i++) {
                if (problems[i].length) {
                    setTried((t) => ({ ...t, [i]: true }));
                    setStep(i);
                    return;
                }
            }
        }
        setDir(next > step ? 1 : -1);
        setStep(next);
    }

    async function submit() {
        for (let i = 0; i < STEPS.length; i++) {
            if (problems[i].length) {
                setTried((t) => ({ ...t, [i]: true }));
                setDir(i > step ? 1 : -1);
                setStep(i);
                return;
            }
        }
        try {
            const app = await create.mutateAsync({
                name: name.trim(),
                description: description.trim(),
                website_url: website.trim(),
                redirect_uris: redirectList,
                allowed_webhook_domains: webhooksOn ? domains : [],
                webhook_url: webhooksOn ? webhookUrl.trim() : undefined,
                webhook_events: webhooksOn ? webhookEvents : undefined,
                scopes,
            });
            setCreated(app);
            if (logo) {
                try {
                    await setLogo.mutateAsync({ id: app.id, blob: logo.blob });
                } catch {
                    toast.error("The app is registered, but the logo didn’t upload. Add it from the app’s settings.");
                }
            }
        } catch (e) {
            toast.error((e as { message?: string })?.message ?? "Could not register the app");
        }
    }

    const showErrors = !!tried[step];
    const last = step === STEPS.length - 1;

    return createPortal(
        <div className="fixed inset-0 z-[60] flex items-start justify-center p-4 pt-[8vh]">
            <motion.div
                className="absolute inset-0 bg-slate-900/40"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.12 }}
                onMouseDown={requestClose}
            />
            <motion.div
                role="dialog"
                aria-modal="true"
                aria-label="Register an OAuth app"
                onMouseDown={(e) => e.stopPropagation()}
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: 8 }}
                transition={{ duration: 0.16, ease: "easeOut" }}
                className="relative w-full max-w-[560px] max-h-[84vh] flex flex-col overflow-hidden rounded-lg border border-slate-200 bg-white shadow-[0_24px_60px_-12px_rgba(15,23,42,0.25)]"
            >
                <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-3 shrink-0">
                    <span className="text-[13px] font-semibold text-slate-900">{created ? "App registered" : "Register an OAuth app"}</span>
                    {!created && (
                        <ol className="ml-auto flex items-center gap-1">
                            {STEPS.map((label, i) => (
                                <li key={label} className="flex items-center gap-1">
                                    {i > 0 && <span className="w-3 h-px bg-slate-200" />}
                                    <button
                                        type="button"
                                        onClick={() => go(i)}
                                        className={cn(
                                            "h-6 px-2 rounded-md text-[11.5px] inline-flex items-center gap-1 transition-colors",
                                            i === step ? "bg-slate-100 text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
                                        )}
                                    >
                                        {i < step && !problems[i].length ? <CheckIcon className="w-3 h-3 text-emerald-600" /> : <span className="tabular-nums">{i + 1}</span>}
                                        {label}
                                    </button>
                                </li>
                            ))}
                        </ol>
                    )}
                    <button
                        type="button"
                        onClick={requestClose}
                        aria-label="Close"
                        className={cn("h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100", created && "ml-auto")}
                    >
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

                {created ? (
                    <Credentials app={created} redirect={redirectList[0]} perms={perms.data?.permissions ?? []} onCopied={() => setSecretCopied(true)} onDone={requestClose} />
                ) : (
                    <>
                        <div className="relative flex-1 overflow-y-auto overflow-x-hidden">
                            <AnimatePresence mode="wait" initial={false} custom={dir}>
                                <motion.div
                                    key={step}
                                    custom={dir}
                                    initial={{ opacity: 0, x: dir * 24 }}
                                    animate={{ opacity: 1, x: 0 }}
                                    exit={{ opacity: 0, x: dir * -24 }}
                                    transition={{ duration: 0.16, ease: "easeOut" }}
                                    className="p-4 flex flex-col gap-4"
                                >
                                    {step === 0 && (
                                        <>
                                            <div className="flex items-start gap-4">
                                                <AvatarUploader
                                                    shape="square"
                                                    current={logo?.url ?? null}
                                                    fallbackInitials={(name.trim()[0] ?? "?").toUpperCase()}
                                                    onUpload={async (blob) => {
                                                        if (logo) URL.revokeObjectURL(logo.url);
                                                        setLogoBlob({ blob, url: URL.createObjectURL(blob) });
                                                    }}
                                                    onRemove={async () => {
                                                        if (logo) URL.revokeObjectURL(logo.url);
                                                        setLogoBlob(null);
                                                    }}
                                                />
                                            </div>
                                            <div>
                                                <Label>Name</Label>
                                                <TextInput value={name} onChange={setName} placeholder="Acme Sync" autoFocus maxLength={80} className="w-full" invalid={showErrors && !!nameProblem} />
                                                <p className={cn("mt-1 text-[11.5px]", showErrors && nameProblem ? "text-rose-600" : "text-slate-400")}>
                                                    {(showErrors && nameProblem) || "People see it on the consent screen when they connect your app."}
                                                </p>
                                            </div>
                                            <div>
                                                <Label>Description</Label>
                                                <TextInput value={description} onChange={setDescription} placeholder="Pushes positive replies into Acme as deals" maxLength={200} className="w-full" />
                                            </div>
                                            <div>
                                                <Label>Website (optional)</Label>
                                                <TextInput value={website} onChange={setWebsite} placeholder="https://acme.com" className="w-full" invalid={!!websiteProblem && (showErrors || website.length > 8)} />
                                                {websiteProblem && (showErrors || website.length > 8) && <p className="mt-1 text-[11.5px] text-rose-600">{websiteProblem}</p>}
                                            </div>
                                        </>
                                    )}

                                    {step === 1 && (
                                        <>
                                            <div>
                                                <Label>What it can do</Label>
                                                <div className="grid grid-cols-3 gap-1.5">
                                                    {(
                                                        [
                                                            ["read_only", "Read only", "View, never change"],
                                                            ["full_access", "Read and write", "Everything the API allows"],
                                                            ["custom", "Custom", "Pick each permission"],
                                                        ] as const
                                                    ).map(([key, label, sub]) => (
                                                        <button
                                                            key={key}
                                                            type="button"
                                                            onClick={() => {
                                                                setPreset(key);
                                                                if (key !== "custom" && presets) setScopes(presets[key] & appScopes);
                                                            }}
                                                            className={cn(
                                                                "rounded-md border px-2.5 py-2 text-left transition-colors",
                                                                preset === key ? "border-sky-400 bg-sky-50" : "border-slate-200 hover:border-slate-300",
                                                            )}
                                                        >
                                                            <span className="block text-[12.5px] font-medium text-slate-900">{label}</span>
                                                            <span className="block text-[11px] text-slate-500">{sub}</span>
                                                        </button>
                                                    ))}
                                                </div>
                                                {preset === "custom" ? (
                                                    <PermissionList
                                                        permissions={appPerms}
                                                        mask={scopes}
                                                        onToggle={(v) => setScopes((m) => (m & v ? m & ~v : m | v))}
                                                    />
                                                ) : (
                                                    <p className="mt-2 text-[11.5px] text-slate-500">
                                                        {summarize(appPerms, scopes)}
                                                    </p>
                                                )}
                                                {showErrors && scopes === 0 && <p className="mt-1 text-[11.5px] text-rose-600">Choose at least one permission.</p>}
                                            </div>
                                            <div>
                                                <Label>Return address</Label>
                                                <p className="-mt-0.5 mb-1.5 text-[11.5px] text-slate-400">
                                                    Where Warmbly sends people after they approve. Matched exactly, so add each one you use.
                                                </p>
                                                <div className="flex flex-col gap-1.5">
                                                    {redirects.map((r, i) => (
                                                        <div key={i}>
                                                            <div className="flex items-center gap-1.5">
                                                                <TextInput
                                                                    value={r}
                                                                    onChange={(v) => setRedirects((list) => list.map((x, j) => (j === i ? v : x)))}
                                                                    placeholder="https://acme.com/oauth/callback"
                                                                    className="w-full font-mono"
                                                                    invalid={!!redirectErrors[i] || (showErrors && redirectList.length === 0 && i === 0)}
                                                                />
                                                                {redirects.length > 1 && (
                                                                    <button
                                                                        type="button"
                                                                        aria-label="Remove address"
                                                                        onClick={() => setRedirects((list) => list.filter((_, j) => j !== i))}
                                                                        className="h-7 w-7 shrink-0 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                                                                    >
                                                                        <XIcon className="w-3.5 h-3.5" />
                                                                    </button>
                                                                )}
                                                            </div>
                                                            {redirectErrors[i] && <p className="mt-1 text-[11.5px] text-rose-600">{redirectErrors[i]}</p>}
                                                        </div>
                                                    ))}
                                                </div>
                                                <button
                                                    type="button"
                                                    onClick={() => setRedirects((list) => [...list, ""])}
                                                    className="mt-1.5 inline-flex items-center gap-1 text-[12px] text-sky-700 hover:text-sky-800"
                                                >
                                                    <PlusIcon className="w-3.5 h-3.5" />
                                                    Add another
                                                </button>
                                            </div>
                                        </>
                                    )}

                                    {step === 2 && (
                                        <>
                                            <div className="flex items-start gap-3">
                                                <div className="min-w-0 flex-1">
                                                    <div className="text-[12.5px] font-medium text-slate-900">Send Warmbly events to your app</div>
                                                    <p className="text-[11.5px] text-slate-500 leading-relaxed">
                                                        Each workspace that connects gets its own signed stream, limited to what it approved. You can turn this on later.
                                                    </p>
                                                </div>
                                                <Toggle value={webhooksOn} onChange={setWebhooksOn} ariaLabel="Send events" />
                                            </div>
                                            {webhooksOn && (
                                                <>
                                                    <div>
                                                        <Label>Webhook address</Label>
                                                        <TextInput
                                                            value={webhookUrl}
                                                            onChange={setWebhookUrl}
                                                            placeholder="https://hooks.acme.com/warmbly"
                                                            className="w-full font-mono"
                                                            invalid={!!webhookProblem || (showErrors && !webhookUrl.trim())}
                                                        />
                                                        {webhookProblem && <p className="mt-1 text-[11.5px] text-rose-600">{webhookProblem}</p>}
                                                    </div>
                                                    <div>
                                                        <Label>Allowed domains</Label>
                                                        {domainsEdited === null ? (
                                                            <p className="text-[11.5px] text-slate-500">
                                                                {webhookHost ? (
                                                                    <>
                                                                        Events can only be sent to <span className="font-mono text-slate-700">{webhookHost}</span>.{" "}
                                                                    </>
                                                                ) : (
                                                                    "Taken from the webhook address. "
                                                                )}
                                                                <button type="button" onClick={() => setDomainsEdited(webhookHost)} className="text-sky-700 hover:text-sky-800">
                                                                    Change
                                                                </button>
                                                            </p>
                                                        ) : (
                                                            <>
                                                                <TextInput value={domainsEdited} onChange={setDomainsEdited} placeholder=".acme.com" className="w-full font-mono" />
                                                                <p className="mt-1 text-[11.5px] text-slate-400">
                                                                    Separate with spaces. A leading dot covers subdomains (.acme.com matches hooks.acme.com).
                                                                </p>
                                                            </>
                                                        )}
                                                    </div>
                                                    <div>
                                                        <Label>Events</Label>
                                                        {catalog.isPending ? (
                                                            <div className="h-24 rounded-md bg-slate-50 animate-pulse" />
                                                        ) : (
                                                            <EventPicker catalog={catalog.data?.event_types ?? []} value={webhookEvents} onChange={setWebhookEvents} />
                                                        )}
                                                    </div>
                                                </>
                                            )}
                                        </>
                                    )}
                                </motion.div>
                            </AnimatePresence>
                        </div>

                        <div className="h-12 px-4 border-t border-slate-200 flex items-center gap-2 shrink-0">
                            {step > 0 ? (
                                <button
                                    type="button"
                                    onClick={() => go(step - 1)}
                                    className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300 inline-flex items-center gap-1"
                                >
                                    <ArrowLeftIcon className="w-3.5 h-3.5" />
                                    Back
                                </button>
                            ) : (
                                <button type="button" onClick={requestClose} className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300">
                                    Cancel
                                </button>
                            )}
                            <span className={cn("min-w-0 truncate text-[11.5px]", showErrors ? "text-rose-600" : "text-slate-400")}>
                                {problems[step][0] ?? ""}
                            </span>
                            <div className="ml-auto flex items-center gap-2">
                                {last && !webhooksOn && <span className="text-[11.5px] text-slate-400">Optional</span>}
                                <button
                                    type="button"
                                    onClick={() => (last ? void submit() : go(step + 1))}
                                    disabled={create.isPending}
                                    className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                >
                                    {create.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                                    {last ? "Register app" : "Continue"}
                                </button>
                            </div>
                        </div>
                    </>
                )}
            </motion.div>
        </div>,
        document.body,
    );
}

function summarize(permissions: APIPermission[], mask: number): string {
    const on = permissions.filter((p) => (mask & p.value) === p.value);
    if (on.length === 0) return "";
    const reads = on.filter((p) => p.category === "read").length;
    const writes = on.length - reads;
    return `${on.length} permissions: ${reads} to read${writes ? `, ${writes} to change data` : ""}. People approve these on the consent screen.`;
}

function PermissionList({ permissions, mask, onToggle }: { permissions: APIPermission[]; mask: number; onToggle: (v: number) => void }) {
    const groups = React.useMemo(() => {
        const g: Record<string, APIPermission[]> = {};
        for (const p of permissions) (g[p.category] ??= []).push(p);
        return Object.entries(g);
    }, [permissions]);
    return (
        <div className="mt-2 max-h-56 overflow-y-auto rounded-md border border-slate-200 divide-y divide-slate-200/70">
            {groups.map(([cat, list]) => (
                <div key={cat}>
                    <div className="px-2.5 py-1.5 bg-slate-50/60 text-[10px] uppercase tracking-[0.14em] text-slate-500 font-medium">{cat}</div>
                    {list.map((p) => {
                        const on = (mask & p.value) === p.value;
                        return (
                            <button
                                key={p.name}
                                type="button"
                                onClick={() => onToggle(p.value)}
                                className="w-full px-2.5 py-1.5 flex items-center gap-2 text-left hover:bg-slate-50 transition-colors"
                            >
                                <CheckSquare checked={on} tone="sky" />
                                <span className="text-[12px] text-slate-700 truncate">{p.description}</span>
                                <code className="ml-auto text-[10.5px] text-slate-400 font-mono shrink-0">{p.name.toLowerCase()}</code>
                            </button>
                        );
                    })}
                </div>
            ))}
        </div>
    );
}

function Credentials({
    app,
    redirect,
    perms,
    onCopied,
    onDone,
}: {
    app: OAuthApplicationWithSecret;
    redirect?: string;
    perms: APIPermission[];
    onCopied: () => void;
    onDone: () => void;
}) {
    const scope = perms
        .filter((p) => (app.scopes & p.value) === p.value)
        .map((p) => p.name.toLowerCase())
        .join(" ");
    const authorizeUrl = `${APP_URL}/oauth/authorize?response_type=code&client_id=${encodeURIComponent(app.client_id)}&redirect_uri=${encodeURIComponent(redirect ?? "")}&scope=${encodeURIComponent(scope)}&state=YOUR_STATE`;
    const env = `WARMBLY_CLIENT_ID=${app.client_id}\nWARMBLY_CLIENT_SECRET=${app.client_secret ?? ""}`;
    return (
        <>
            <div className="flex-1 overflow-y-auto p-4 flex flex-col gap-4">
                <p className="text-[12.5px] text-slate-600 leading-relaxed">
                    <span className="font-medium text-slate-900">{app.name}</span> is registered. Copy the client secret now: it is shown once,
                    and you can only rotate it if it is lost.
                </p>
                <div>
                    <Label>Client ID</Label>
                    <div className="flex items-center gap-1.5">
                        <code className="flex-1 min-w-0 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-slate-700">{app.client_id}</code>
                        <CopyButton value={app.client_id} />
                    </div>
                </div>
                <div>
                    <Label>Client secret</Label>
                    <div className="flex items-center gap-1.5">
                        <code className="flex-1 min-w-0 truncate rounded-md border border-amber-200 bg-amber-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-amber-800" data-ph-mask="">
                            {app.client_secret}
                        </code>
                        <CopyButton value={app.client_secret ?? ""} onCopied={onCopied} />
                    </div>
                </div>
                <div className="flex items-center gap-2">
                    <CopyButton value={env} label="Copy both as .env" onCopied={onCopied} />
                </div>
                <div>
                    <Label>Start the sign-in</Label>
                    <p className="-mt-0.5 mb-1.5 text-[11.5px] text-slate-400">
                        Send people here from your app. Replace YOUR_STATE with a random value you check when they come back.
                    </p>
                    <div className="flex items-start gap-1.5">
                        <code className="flex-1 min-w-0 rounded-md border border-slate-200 bg-slate-50 px-2 py-1.5 text-[11px] font-mono text-slate-600 break-all">{authorizeUrl}</code>
                        <CopyButton value={authorizeUrl} />
                    </div>
                </div>
                <a href="https://docs.warmbly.com/api/oauth/" target="_blank" rel="noopener noreferrer" className="self-start text-[12px] text-sky-700 hover:text-sky-800">
                    Read the OAuth guide
                </a>
            </div>
            <div className="h-12 px-4 border-t border-slate-200 flex items-center justify-end shrink-0">
                <button type="button" onClick={onDone} className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium">
                    Done
                </button>
            </div>
        </>
    );
}
