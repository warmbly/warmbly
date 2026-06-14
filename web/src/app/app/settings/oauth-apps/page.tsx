// OAuth apps — a Settings section. Developers register third-party OAuth clients
// here (client id + one-time secret, redirect URIs, requested scopes) and review
// the apps the workspace's members have authorized. Every app is issued a client
// secret; PKCE is an optional extra layer the developer can add. The flow itself
// (consent + token exchange) lives on the standalone /oauth/authorize page + API.

import React from "react";
import { createPortal } from "react-dom";
import toast from "react-hot-toast";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowLeftIcon,
    ArrowRightIcon,
    CheckCircle2Icon,
    CheckIcon,
    CopyIcon,
    ImageIcon,
    Loader2Icon,
    PlusIcon,
    RefreshCwIcon,
    Trash2Icon,
    XIcon,
} from "lucide-react";

import { NoAccess } from "@/components/layout/NoAccess";
import { usePermission } from "@/hooks/usePermission";
import { EmptyBlock } from "@/components/layout/Page";
import { Label, TextInput } from "@/components/ui/field";
import { cn } from "@/lib/utils";
import { useConfirm } from "@/hooks/context/confirm";
import useAPIPermissions from "@/lib/api/hooks/app/api-keys/useAPIPermissions";
import type APIPermission from "@/lib/api/models/app/apikeys/APIPermission";
import {
    useOAuthApps,
    useCreateOAuthApp,
    useDeleteOAuthApp,
    useRotateOAuthAppSecret,
    useUploadOAuthAppLogo,
} from "@/lib/api/hooks/app/oauth/useOAuthApps";
import { useAuthorizedApps, useRevokeAuthorizedApp } from "@/lib/api/hooks/app/oauth/useAuthorizedApps";
import type { OAuthApplication, OAuthApplicationWithSecret } from "@/lib/api/models/app/oauth/OAuthApp";
import { SectionShell } from "../_components/SectionShell";

function CopyButton({ value, label }: { value: string; label?: string }) {
    const [copied, setCopied] = React.useState(false);
    return (
        <button
            type="button"
            onClick={async () => {
                try {
                    await navigator.clipboard.writeText(value);
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1500);
                } catch {
                    /* clipboard blocked */
                }
            }}
            className="inline-flex items-center gap-1 rounded-md border border-slate-200 px-2 h-7 text-[11.5px] text-slate-600 hover:bg-slate-50"
        >
            {copied ? <CheckIcon className="w-3.5 h-3.5 text-emerald-600" /> : <CopyIcon className="w-3.5 h-3.5" />}
            {copied ? "Copied" : (label ?? "Copy")}
        </button>
    );
}

// ScopePicker is a checkbox grid over the API permissions, toggling bits in the
// scope bitmask. Reuses the same permission catalogue as API keys.
function ScopePicker({ value, onChange }: { value: number; onChange: (v: number) => void }) {
    const perms = useAPIPermissions();
    const grouped = React.useMemo(() => {
        const g: Record<string, APIPermission[]> = {};
        for (const p of perms.data?.permissions ?? []) {
            (g[p.category] ??= []).push(p);
        }
        return g;
    }, [perms.data]);
    const toggle = (bit: number) => onChange(value & bit ? value & ~bit : value | bit);
    return (
        <div className="space-y-3">
            {Object.entries(grouped).map(([cat, list]) => (
                <div key={cat}>
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-1">{cat}</div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-1">
                        {list.map((p) => {
                            const on = (value & p.value) === p.value;
                            return (
                                <button
                                    key={p.name}
                                    type="button"
                                    onClick={() => toggle(p.value)}
                                    className={`flex items-start gap-2 rounded-md border px-2 py-1.5 text-left ${on ? "border-sky-400 bg-sky-50" : "border-slate-200 hover:bg-slate-50"}`}
                                >
                                    <span
                                        className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded ${on ? "bg-sky-600 text-white" : "border border-slate-300"}`}
                                    >
                                        {on && <CheckIcon className="w-3 h-3" />}
                                    </span>
                                    <span className="min-w-0">
                                        <span className="block text-[12px] font-medium text-slate-700">
                                            {p.name.toLowerCase()}
                                        </span>
                                        <span className="block text-[11px] text-slate-400 leading-tight">{p.description}</span>
                                    </span>
                                </button>
                            );
                        })}
                    </div>
                </div>
            ))}
        </div>
    );
}

const TILE_COLORS = ["bg-sky-600", "bg-indigo-600", "bg-emerald-600", "bg-rose-600", "bg-amber-600", "bg-fuchsia-600"];

// AppLogo renders an app's uploaded logo, or a colored letter tile as a fallback.
function AppLogo({ name, url, size = "md" }: { name: string; url?: string | null; size?: "sm" | "md" | "lg" }) {
    const dim =
        size === "lg"
            ? "w-16 h-16 text-[22px] rounded-2xl"
            : size === "sm"
              ? "w-8 h-8 text-[12px] rounded-md"
              : "w-9 h-9 text-[13px] rounded-lg";
    if (url) return <img src={url} alt={name} className={cn(dim, "object-cover border border-slate-200 shrink-0")} />;
    const letter = (name.trim()[0] ?? "?").toUpperCase();
    const color = TILE_COLORS[letter.charCodeAt(0) % TILE_COLORS.length];
    return <div className={cn(dim, color, "flex items-center justify-center text-white font-semibold shrink-0")}>{letter}</div>;
}

const WIZARD_STEPS = ["Basics", "Branding", "Redirects", "Scopes"] as const;

// Stepper — the horizontal progress indicator at the top of the register wizard.
function Stepper({ step }: { step: number }) {
    return (
        <div className="px-4 pt-3 pb-1 shrink-0">
            <div className="flex items-center">
                {WIZARD_STEPS.map((label, i) => {
                    const done = i < step;
                    const active = i === step;
                    return (
                        <React.Fragment key={label}>
                            <div className="flex items-center gap-1.5">
                                <span
                                    className={cn(
                                        "flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold transition-colors",
                                        active ? "bg-sky-600 text-white" : done ? "bg-sky-100 text-sky-700" : "bg-slate-100 text-slate-400",
                                    )}
                                >
                                    {done ? <CheckIcon className="w-3 h-3" /> : i + 1}
                                </span>
                                <span
                                    className={cn(
                                        "text-[11px] font-medium hidden sm:inline transition-colors",
                                        active ? "text-slate-900" : done ? "text-slate-500" : "text-slate-400",
                                    )}
                                >
                                    {label}
                                </span>
                            </div>
                            {i < WIZARD_STEPS.length - 1 && (
                                <span className={cn("mx-2 h-px flex-1 transition-colors", i < step ? "bg-sky-200" : "bg-slate-200")} />
                            )}
                        </React.Fragment>
                    );
                })}
            </div>
        </div>
    );
}

// RegisterModal — a multi-step onboarding wizard for creating an OAuth app:
// Basics -> Branding (logo) -> Redirects -> Scopes -> reveal credentials. The
// modal animates in, steps slide, and a stepper shows progress.
function RegisterModal({ onClose }: { onClose: () => void }) {
    const create = useCreateOAuthApp();
    const uploadLogo = useUploadOAuthAppLogo();
    const fileRef = React.useRef<HTMLInputElement>(null);

    const [step, setStep] = React.useState(0);
    const [name, setName] = React.useState("");
    const [description, setDescription] = React.useState("");
    const [website, setWebsite] = React.useState("");
    const [logoUrl, setLogoUrl] = React.useState("");
    const [redirects, setRedirects] = React.useState("");
    const [scopes, setScopes] = React.useState(0);
    const [created, setCreated] = React.useState<OAuthApplicationWithSecret | null>(null);

    const redirectList = redirects.split("\n").map((s) => s.trim()).filter(Boolean);

    const stepValid = (i: number): boolean => {
        if (i === 0) return name.trim().length > 0;
        if (i === 2) return redirectList.length > 0;
        if (i === 3) return scopes !== 0;
        return true; // branding (logo) is optional
    };

    const goNext = () => {
        if (!stepValid(step)) {
            toast.error(step === 0 ? "Give the app a name" : step === 2 ? "Add at least one redirect URI" : "Select at least one scope");
            return;
        }
        setStep((s) => Math.min(s + 1, WIZARD_STEPS.length - 1));
    };
    const goBack = () => setStep((s) => Math.max(s - 1, 0));

    const onPickLogo = async (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0];
        e.target.value = "";
        if (!file) return;
        try {
            const { logo_url } = await uploadLogo.mutateAsync(file);
            setLogoUrl(logo_url);
        } catch (err) {
            toast.error((err as { message?: string })?.message ?? "Could not upload the logo");
        }
    };

    const submit = async () => {
        if (!stepValid(0) || !stepValid(2) || !stepValid(3)) {
            toast.error("Fill in the required fields");
            return;
        }
        try {
            const app = await create.mutateAsync({
                name: name.trim(),
                description: description.trim(),
                website_url: website.trim(),
                logo_url: logoUrl || undefined,
                redirect_uris: redirectList,
                scopes,
            });
            setCreated(app);
            toast.success("App registered");
        } catch (e) {
            toast.error((e as { message?: string })?.message ?? "Could not register the app");
        }
    };

    // Rendered through a portal to document.body so the modal mounts as a clean
    // top-level subtree (outside the Settings content's AnimatePresence) and its
    // entrance animation always plays.
    return createPortal(
        <div className="fixed inset-0 z-[60] flex items-center justify-center p-4">
            <motion.div
                className="absolute inset-0 bg-slate-900/40"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ duration: 0.2, ease: "easeOut" }}
            />
            <motion.div
                className="relative w-full max-w-lg max-h-[90vh] flex flex-col overflow-hidden rounded-xl bg-white shadow-xl ring-1 ring-slate-200"
                initial={{ opacity: 0, y: 24, scale: 0.94 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                transition={{ type: "spring", stiffness: 280, damping: 22, mass: 0.9 }}
            >
                <div className="flex items-center border-b border-slate-200 px-4 h-11 shrink-0">
                    <span className="text-[12.5px] font-medium text-slate-900">
                        {created ? "App created" : "Register an OAuth app"}
                    </span>
                    <button onClick={onClose} className="ml-auto h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100">
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

                {created ? (
                    <RevealStep app={created} onDone={onClose} />
                ) : (
                    <>
                        <Stepper step={step} />
                        <div className="relative flex-1 overflow-y-auto">
                            <AnimatePresence mode="wait" initial={false}>
                                <motion.div
                                    key={step}
                                    initial={{ opacity: 0, x: 24 }}
                                    animate={{ opacity: 1, x: 0 }}
                                    exit={{ opacity: 0, x: -24 }}
                                    transition={{ duration: 0.18, ease: "easeOut" }}
                                    className="p-4 space-y-3"
                                >
                                    {step === 0 && (
                                        <>
                                            <div>
                                                <Label>Name</Label>
                                                <TextInput value={name} onChange={setName} placeholder="Acme Integration" className="w-full" />
                                            </div>
                                            <div>
                                                <Label>Description</Label>
                                                <TextInput value={description} onChange={setDescription} placeholder="What the app does" className="w-full" />
                                            </div>
                                            <div>
                                                <Label>Website</Label>
                                                <TextInput value={website} onChange={setWebsite} placeholder="https://acme.com" className="w-full" />
                                            </div>
                                        </>
                                    )}
                                    {step === 1 && (
                                        <>
                                            <p className="text-[12px] text-slate-500 leading-relaxed">
                                                Add a logo so people recognize your app on the consent screen when they connect it.
                                            </p>
                                            <div className="flex items-center gap-3">
                                                <AppLogo name={name || "?"} url={logoUrl} size="lg" />
                                                <div>
                                                    <input ref={fileRef} type="file" accept="image/png,image/jpeg" className="hidden" onChange={onPickLogo} />
                                                    <div className="flex items-center gap-2">
                                                        <button
                                                            type="button"
                                                            onClick={() => fileRef.current?.click()}
                                                            disabled={uploadLogo.isPending}
                                                            className="h-8 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5 disabled:opacity-60"
                                                        >
                                                            {uploadLogo.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <ImageIcon className="w-3.5 h-3.5" />}
                                                            {logoUrl ? "Replace logo" : "Upload logo"}
                                                        </button>
                                                        {logoUrl && (
                                                            <button type="button" onClick={() => setLogoUrl("")} className="text-[11.5px] text-slate-400 hover:text-rose-600">
                                                                Remove
                                                            </button>
                                                        )}
                                                    </div>
                                                    <p className="mt-1.5 text-[10.5px] text-slate-400">PNG or JPG, up to 2MB. Optional.</p>
                                                </div>
                                            </div>
                                        </>
                                    )}
                                    {step === 2 && (
                                        <div>
                                            <Label>Redirect URIs</Label>
                                            <textarea
                                                value={redirects}
                                                onChange={(e) => setRedirects(e.target.value)}
                                                placeholder={"https://acme.com/oauth/callback"}
                                                rows={3}
                                                className="w-full rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[12px] font-mono text-slate-900 placeholder:text-slate-400 outline-none resize-y focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                                            />
                                            <p className="mt-1 text-[11px] text-slate-400">One per line. Must be HTTPS (or a loopback URL), matched exactly.</p>
                                        </div>
                                    )}
                                    {step === 3 && (
                                        <div>
                                            <Label>Scopes</Label>
                                            <ScopePicker value={scopes} onChange={setScopes} />
                                        </div>
                                    )}
                                </motion.div>
                            </AnimatePresence>
                        </div>

                        <div className="px-4 py-2.5 border-t border-slate-200 flex items-center gap-2 shrink-0">
                            {step > 0 ? (
                                <button onClick={goBack} className="h-8 px-3 rounded-md border border-slate-200 text-[12.5px] text-slate-600 hover:bg-slate-50 inline-flex items-center gap-1.5">
                                    <ArrowLeftIcon className="w-3.5 h-3.5" /> Back
                                </button>
                            ) : (
                                <button onClick={onClose} className="h-8 px-3 rounded-md border border-slate-200 text-[12.5px] text-slate-600 hover:bg-slate-50">
                                    Cancel
                                </button>
                            )}
                            <span className="ml-auto text-[11px] text-slate-400">
                                Step {step + 1} of {WIZARD_STEPS.length}
                            </span>
                            {step < WIZARD_STEPS.length - 1 ? (
                                <button onClick={goNext} className="h-8 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700 inline-flex items-center gap-1.5">
                                    Next <ArrowRightIcon className="w-3.5 h-3.5" />
                                </button>
                            ) : (
                                <button
                                    onClick={submit}
                                    disabled={create.isPending}
                                    className="h-8 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700 disabled:opacity-60 inline-flex items-center gap-1.5"
                                >
                                    {create.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CheckIcon className="w-3.5 h-3.5" />}
                                    Create app
                                </button>
                            )}
                        </div>
                    </>
                )}
            </motion.div>
        </div>,
        document.body,
    );
}

// RevealStep shows the one-time client credentials after the app is created.
function RevealStep({ app, onDone }: { app: OAuthApplicationWithSecret; onDone: () => void }) {
    return (
        <div className="flex-1 overflow-y-auto p-4 space-y-3">
            <div className="flex items-center gap-2 text-emerald-600">
                <CheckCircle2Icon className="w-4 h-4" />
                <span className="text-[12.5px] font-medium">{app.name} is ready</span>
            </div>
            <p className="text-[12px] text-slate-500 leading-relaxed">
                Save the client secret now. For security it is shown only once and cannot be retrieved later (you can rotate it if you lose it).
            </p>
            <div>
                <Label>Client ID</Label>
                <div className="flex items-center gap-1.5">
                    <code className="flex-1 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-slate-700">
                        {app.client_id}
                    </code>
                    <CopyButton value={app.client_id} />
                </div>
            </div>
            <div>
                <Label>Client secret</Label>
                <div className="flex items-center gap-1.5">
                    <code className="flex-1 truncate rounded-md border border-amber-200 bg-amber-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-amber-800">
                        {app.client_secret}
                    </code>
                    <CopyButton value={app.client_secret ?? ""} />
                </div>
            </div>
            <div className="pt-1">
                <button onClick={onDone} className="h-8 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700">
                    Done
                </button>
            </div>
        </div>
    );
}

function AppRow({ app }: { app: OAuthApplication }) {
    const del = useDeleteOAuthApp();
    const rotate = useRotateOAuthAppSecret();
    const confirm = useConfirm();
    const [secret, setSecret] = React.useState<string | null>(null);

    const onRotate = () =>
        confirm.show("Rotate this app's client secret? The current secret stops working immediately.", async () => {
            const res = await rotate.mutateAsync(app.id);
            setSecret(res.client_secret);
        });
    const onDelete = () =>
        confirm.show(`Delete "${app.name}"? Every token issued to it is revoked.`, async () => {
            await del.mutateAsync(app.id);
        });

    return (
        <div className="rounded-lg border border-slate-200 p-3">
            <div className="flex items-start gap-3">
                <AppLogo name={app.name} url={app.logo_url} size="md" />
                <div className="min-w-0 flex-1">
                    <div className="text-[13px] font-semibold text-slate-800 truncate">{app.name}</div>
                    {app.description && <div className="text-[11.5px] text-slate-400 truncate">{app.description}</div>}
                    <div className="mt-1.5 flex items-center gap-1.5">
                        <code className="truncate rounded border border-slate-200 bg-slate-50 px-1.5 py-0.5 text-[11px] font-mono text-slate-600">
                            {app.client_id}
                        </code>
                        <CopyButton value={app.client_id} label="ID" />
                    </div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                    <button
                        onClick={onRotate}
                        title="Rotate secret"
                        className="h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                    >
                        <RefreshCwIcon className="w-3.5 h-3.5" />
                    </button>
                    <button
                        onClick={onDelete}
                        title="Delete app"
                        className="h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-rose-50 hover:text-rose-600"
                    >
                        <Trash2Icon className="w-3.5 h-3.5" />
                    </button>
                </div>
            </div>
            <div className="mt-2 flex flex-wrap gap-1">
                {app.redirect_uris.map((u) => (
                    <span key={u} className="rounded bg-slate-100 px-1.5 py-0.5 text-[10.5px] font-mono text-slate-500">
                        {u}
                    </span>
                ))}
            </div>
            {secret && (
                <div className="mt-2 rounded-md border border-amber-200 bg-amber-50 p-2">
                    <div className="text-[10.5px] uppercase tracking-[0.12em] text-amber-700 mb-1">New client secret (shown once)</div>
                    <div className="flex items-center gap-1.5">
                        <code className="flex-1 truncate text-[11.5px] font-mono text-amber-800">{secret}</code>
                        <CopyButton value={secret} />
                    </div>
                </div>
            )}
        </div>
    );
}

function AuthorizedTab() {
    const authorized = useAuthorizedApps();
    const revoke = useRevokeAuthorizedApp();
    const confirm = useConfirm();
    const apps = authorized.data?.authorized_apps ?? [];

    if (apps.length === 0) {
        return <EmptyBlock title="No authorized apps" body="Apps your workspace connects to via OAuth will appear here." />;
    }
    return (
        <div className="space-y-2">
            {apps.map((a) => (
                <div key={a.application_id} className="flex items-center gap-3 rounded-lg border border-slate-200 p-3">
                    <AppLogo name={a.name} url={a.logo_url} size="md" />
                    <div className="min-w-0 flex-1">
                        <div className="text-[13px] font-semibold text-slate-800 truncate">{a.name}</div>
                        <div className="text-[11.5px] text-slate-400">
                            Authorized {new Date(a.authorized_at).toLocaleDateString()}
                        </div>
                    </div>
                    <button
                        onClick={() =>
                            confirm.show(`Revoke "${a.name}"? Its tokens stop working immediately.`, async () => {
                                await revoke.mutateAsync(a.application_id);
                            })
                        }
                        className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-rose-50 hover:text-rose-600 hover:border-rose-200"
                    >
                        Revoke
                    </button>
                </div>
            ))}
        </div>
    );
}

export default function OAuthAppsPage() {
    const canManage = usePermission("MANAGE_API_KEYS");
    const apps = useOAuthApps();
    const [tab, setTab] = React.useState<"apps" | "authorized">("apps");
    const [createOpen, setCreateOpen] = React.useState(false);

    if (!canManage) return <NoAccess feature="OAuth apps" permissionLabel="Manage API keys" />;

    const list = apps.data?.applications ?? [];

    return (
        <SectionShell
            title="OAuth apps"
            description="Apps that connect to Warmbly on a user's behalf via OAuth2."
            actions={
                tab === "apps" ? (
                    <button
                        onClick={() => setCreateOpen(true)}
                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5"
                    >
                        <PlusIcon className="w-3.5 h-3.5" /> Register app
                    </button>
                ) : null
            }
        >
            <div className="px-4 py-5 md:px-8 md:py-6">
                <div className="mb-3 flex items-center gap-1 border-b border-slate-200">
                    {(
                        [
                            ["apps", "Your apps"],
                            ["authorized", "Authorized apps"],
                        ] as const
                    ).map(([k, label]) => (
                        <button
                            key={k}
                            onClick={() => setTab(k)}
                            className={`relative h-9 px-2.5 text-[12.5px] transition-colors ${tab === k ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-700"}`}
                        >
                            {label}
                            {tab === k && (
                                <motion.span
                                    layoutId="oauth-apps-tab"
                                    className="absolute inset-x-1 -bottom-px h-0.5 rounded bg-sky-600"
                                    transition={{ type: "spring", stiffness: 520, damping: 40 }}
                                />
                            )}
                        </button>
                    ))}
                </div>

                <AnimatePresence mode="wait" initial={false}>
                    <motion.div
                        key={tab}
                        initial={{ opacity: 0, x: 8 }}
                        animate={{ opacity: 1, x: 0 }}
                        exit={{ opacity: 0, x: -8 }}
                        transition={{ duration: 0.15, ease: "easeOut" }}
                    >
                        {tab === "apps" ? (
                            list.length === 0 ? (
                                <EmptyBlock
                                    title="No OAuth apps yet"
                                    body="Register an app to let it request scoped access to Warmbly accounts via OAuth2."
                                />
                            ) : (
                                <div className="space-y-2">
                                    {list.map((app) => (
                                        <AppRow key={app.id} app={app} />
                                    ))}
                                </div>
                            )
                        ) : (
                            <AuthorizedTab />
                        )}
                    </motion.div>
                </AnimatePresence>
            </div>

            {createOpen && <RegisterModal onClose={() => setCreateOpen(false)} />}
        </SectionShell>
    );
}
