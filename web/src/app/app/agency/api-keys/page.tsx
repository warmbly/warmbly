import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, CopyIcon, KeyIcon, Loader2Icon, MoreHorizontalIcon, PlusIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { AgencyDialog } from "@/components/app/agency/AgencyDialog";
import { AgencyPage } from "@/components/app/agency/AgencyPage";
import { ghostBtn, primaryBtn } from "@/components/app/agency/styles";
import { EmptyBlock, PageBody, SectionBar, TopbarAction } from "@/components/layout/Page";
import { Checkbox } from "@/components/ui/checkbox";
import { Label, TextInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import useBrand from "@/hooks/useBrand";
import { useCurrentAgency } from "@/lib/api/hooks/app/agency/useAgency";
import {
    useAgencyAPIKeyPermissions,
    useAgencyAPIKeys,
    useCreateAgencyAPIKey,
    useRevokeAgencyAPIKey,
    useUpdateAgencyAPIKey,
} from "@/lib/api/hooks/app/agency/useAgencyAPIKeys";
import type { AppError } from "@/lib/api/client/normalizeError";
import { type AgencyAPIKey, type AgencyAPIKeyWithSecret, type AgencyAPIPermission, agencyKeyLive } from "@/lib/api/models/app/agency/AgencyAPIKey";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

const ORG_HEADER = "Warmbly-Organization";

function parseIPs(raw: string): string[] {
    return raw
        .split(/[\s,]+/)
        .map((s) => s.trim())
        .filter(Boolean);
}

function fmtRelative(iso?: string): string {
    if (!iso) return "never used";
    const s = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000));
    if (s < 60) return "used just now";
    if (s < 3600) return `used ${Math.floor(s / 60)}m ago`;
    if (s < 86400) return `used ${Math.floor(s / 3600)}h ago`;
    return `used ${Math.floor(s / 86400)}d ago`;
}

function CopyBlock({ text, label }: { text: string; label: string }) {
    const [copied, setCopied] = React.useState(false);
    const copy = () =>
        navigator.clipboard.writeText(text).then(
            () => {
                setCopied(true);
                setTimeout(() => setCopied(false), 1500);
            },
            () => toast.error("Could not copy"),
        );
    return (
        <div className="rounded-md border border-slate-200 bg-slate-950 overflow-hidden">
            <div className="h-8 px-3 flex items-center border-b border-slate-800/60">
                <span className="text-[11px] text-slate-400 font-mono">{label}</span>
                <button
                    type="button"
                    onClick={() => void copy()}
                    className="ml-auto inline-flex items-center gap-1 h-6 px-2 rounded text-slate-400 hover:text-white hover:bg-slate-800 transition-colors text-[11px]"
                >
                    {copied ? <CheckIcon className="w-3 h-3" /> : <CopyIcon className="w-3 h-3" />}
                    {copied ? "Copied" : "Copy"}
                </button>
            </div>
            <pre className="px-4 py-3 text-[12px] leading-relaxed text-slate-200 font-mono whitespace-pre-wrap break-all">{text}</pre>
        </div>
    );
}

function snippet(apiURL: string, secret: string) {
    return `curl ${apiURL}/v1/campaigns \\
  -H "Authorization: Bearer ${secret}" \\
  -H "${ORG_HEADER}: <client workspace id>"`;
}

type Preset = "read_only" | "full_access" | "custom";

function PermissionGroup({ title, perms, value, onChange }: { title: string; perms: AgencyAPIPermission[]; value: number; onChange: (v: number) => void }) {
    return (
        <div>
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1">{title}</div>
            <div className="grid sm:grid-cols-2 gap-x-3 gap-y-1">
                {perms.map((p) => (
                    <label key={p.name} className="flex items-start gap-2 py-0.5 cursor-pointer">
                        <Checkbox
                            tone="slate"
                            className="mt-0.5"
                            checked={(value & p.value) === p.value}
                            onChange={(e) => onChange(e.target.checked ? value | p.value : value & ~p.value)}
                        />
                        <span className="min-w-0">
                            <span className="block text-[12px] text-slate-800 font-mono">{p.name}</span>
                            <span className="block text-[11px] text-slate-500 leading-snug">{p.description}</span>
                        </span>
                    </label>
                ))}
            </div>
        </div>
    );
}

function CreateKeyDialog({ open, onClose, agencyId }: { open: boolean; onClose: () => void; agencyId: string }) {
    const perms = useAgencyAPIKeyPermissions(agencyId, open);
    const create = useCreateAgencyAPIKey(agencyId);
    const confirm = useConfirm();
    const { apiURL } = useBrand();
    const [name, setName] = React.useState("");
    const [preset, setPreset] = React.useState<Preset>("read_only");
    const [custom, setCustom] = React.useState(0);
    const [ips, setIPs] = React.useState("");
    const [created, setCreated] = React.useState<AgencyAPIKeyWithSecret | null>(null);

    React.useEffect(() => {
        if (open) return;
        setName("");
        setPreset("read_only");
        setCustom(0);
        setIPs("");
        setCreated(null);
    }, [open]);

    const mask = preset === "custom" ? custom : (perms.data?.presets[preset] ?? 0);
    const reason = !name.trim() ? "Name the key first." : mask === 0 ? "Pick at least one permission." : null;

    async function submit() {
        if (reason) return;
        try {
            const k = await create.mutateAsync({ name: name.trim(), permissions: mask, allowed_ips: parseIPs(ips) });
            setCreated(k);
        } catch (e) {
            toast.error(buildError(e as unknown as AppError));
        }
    }

    // The secret is shown once, so leaving before copying it is confirmed.
    const requestClose = () => {
        if (created) {
            confirm.show("Close without copying the key? It is not shown again; you would have to create a new one.", async () => onClose(), undefined, {
                title: "Close",
                confirmLabel: "Close",
            });
            return;
        }
        onClose();
    };

    const options: { id: Preset; label: string; hint: string }[] = [
        { id: "read_only", label: "Read only", hint: "Read every client workspace, the agency, its usage and AI balance." },
        { id: "full_access", label: "Full access", hint: "Everything an agency key can do, including sending and moving AI balance." },
        { id: "custom", label: "Custom", hint: "Pick each permission." },
    ];

    return (
        <AgencyDialog
            open={open}
            onClose={requestClose}
            eyebrow={created ? "Copy" : "Create"}
            title={created ? "Your new key" : "Agency API key"}
            icon={<KeyIcon className="w-3 h-3" />}
            footer={
                created ? (
                    <button type="button" onClick={onClose} className={cn("ml-auto", primaryBtn)}>
                        I copied it
                    </button>
                ) : (
                    <>
                        {reason && <span className="text-[11px] text-slate-400 truncate">{reason}</span>}
                        <button type="button" onClick={onClose} className={cn("ml-auto", ghostBtn)}>
                            Cancel
                        </button>
                        <button type="button" onClick={() => void submit()} disabled={create.isPending || !!reason} className={primaryBtn}>
                            {create.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            Create key
                        </button>
                    </>
                )
            }
        >
            <div className="relative overflow-hidden">
                <AnimatePresence mode="wait" initial={false}>
                    {created ? (
                        <motion.div key="secret" initial={{ x: 24, opacity: 0 }} animate={{ x: 0, opacity: 1 }} exit={{ x: -24, opacity: 0 }} transition={{ duration: 0.16 }} className="px-4 py-4 space-y-3">
                            <p className="text-[12px] text-slate-600 leading-relaxed">
                                This is the only time the key is shown. Store it somewhere safe; Warmbly keeps only a hash of it.
                            </p>
                            <CopyBlock label="key" text={created.secret} />
                            <p className="text-[11.5px] text-slate-500 leading-relaxed">
                                Name the client workspace in the <span className="font-mono">{ORG_HEADER}</span> header. Agency endpoints under{" "}
                                <span className="font-mono">/v1/agency/{agencyId}</span> need no header.
                            </p>
                            <CopyBlock label="curl" text={snippet(apiURL, created.secret)} />
                        </motion.div>
                    ) : (
                        <motion.form
                            key="form"
                            initial={{ x: -24, opacity: 0 }}
                            animate={{ x: 0, opacity: 1 }}
                            exit={{ x: 24, opacity: 0 }}
                            transition={{ duration: 0.16 }}
                            className="px-4 py-4 space-y-3 max-h-[60vh] overflow-y-auto"
                            onSubmit={(e) => {
                                e.preventDefault();
                                void submit();
                            }}
                        >
                            <div>
                                <Label>Name</Label>
                                <TextInput value={name} onChange={setName} placeholder="Reporting sync" autoFocus maxLength={255} className="w-full" />
                            </div>
                            <div>
                                <Label>Permissions</Label>
                                <div className="space-y-1">
                                    {options.map((o) => (
                                        <button
                                            key={o.id}
                                            type="button"
                                            role="radio"
                                            aria-checked={preset === o.id}
                                            onClick={() => setPreset(o.id)}
                                            className={cn(
                                                "w-full px-2.5 py-2 rounded-md border text-left transition-colors",
                                                preset === o.id ? "border-sky-400 bg-sky-50" : "border-slate-200 hover:border-slate-300",
                                            )}
                                        >
                                            <div className="text-[12.5px] font-medium text-slate-900">{o.label}</div>
                                            <div className="text-[11px] text-slate-500">{o.hint}</div>
                                        </button>
                                    ))}
                                </div>
                            </div>
                            {preset === "custom" && perms.data && (
                                <div className="space-y-3 rounded-md border border-slate-200 p-3">
                                    <PermissionGroup title="Agency" perms={perms.data.agency} value={custom} onChange={setCustom} />
                                    <PermissionGroup title="Client workspaces" perms={perms.data.workspace} value={custom} onChange={setCustom} />
                                </div>
                            )}
                            <div>
                                <Label>Allowed IP addresses</Label>
                                <TextInput value={ips} onChange={setIPs} placeholder="Any address, or 203.0.113.4, 10.0.0.0/8" className="w-full" />
                            </div>
                            <p className="text-[11px] text-slate-400 leading-relaxed">
                                The key acts for you in every client workspace and stops working if you stop being an owner or admin of the agency.
                            </p>
                        </motion.form>
                    )}
                </AnimatePresence>
            </div>
        </AgencyDialog>
    );
}

function EditKeyDialog({ apiKey, onClose, agencyId, onRevoke }: { apiKey: AgencyAPIKey | null; onClose: () => void; agencyId: string; onRevoke: (k: AgencyAPIKey) => void }) {
    const update = useUpdateAgencyAPIKey(agencyId);
    const [name, setName] = React.useState("");
    const [ips, setIPs] = React.useState("");
    React.useEffect(() => {
        if (!apiKey) return;
        setName(apiKey.name);
        setIPs(apiKey.allowed_ips.join(", "));
    }, [apiKey]);
    const live = apiKey ? agencyKeyLive(apiKey) : false;

    async function save() {
        if (!apiKey || !name.trim()) return;
        try {
            await update.mutateAsync({ keyId: apiKey.id, data: { name: name.trim(), allowed_ips: parseIPs(ips) } });
            onClose();
        } catch (e) {
            toast.error(buildError(e as unknown as AppError));
        }
    }

    return (
        <AgencyDialog
            open={!!apiKey}
            onClose={onClose}
            eyebrow="Key"
            title={apiKey?.name ?? ""}
            icon={<KeyIcon className="w-3 h-3" />}
            footer={
                live ? (
                    <>
                        <button type="button" onClick={() => apiKey && onRevoke(apiKey)} className="h-7 px-2.5 rounded-md text-[12px] text-red-600 hover:bg-red-50 transition-colors">
                            Revoke
                        </button>
                        <button type="button" onClick={onClose} className={cn("ml-auto", ghostBtn)}>
                            Cancel
                        </button>
                        <button type="button" onClick={() => void save()} disabled={update.isPending || !name.trim()} className={primaryBtn}>
                            {update.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            Save
                        </button>
                    </>
                ) : (
                    <button type="button" onClick={onClose} className={cn("ml-auto", ghostBtn)}>
                        Close
                    </button>
                )
            }
        >
            {apiKey && (
                <form
                    className="px-4 py-4 space-y-3"
                    onSubmit={(e) => {
                        e.preventDefault();
                        void save();
                    }}
                >
                    <div className="font-mono text-[11.5px] text-slate-500">
                        {apiKey.key_prefix}…{apiKey.key_suffix} · {apiKey.rate_limit_per_minute}/min · {fmtRelative(apiKey.last_used_at)}
                    </div>
                    <div>
                        <Label>Name</Label>
                        <TextInput value={name} onChange={setName} disabled={!live} maxLength={255} className="w-full" />
                    </div>
                    <div>
                        <Label>Allowed IP addresses</Label>
                        <TextInput value={ips} onChange={setIPs} disabled={!live} placeholder="Any address" className="w-full" />
                    </div>
                    {!live && <p className="text-[11.5px] text-slate-500">This key no longer works. Create a new one to replace it.</p>}
                </form>
            )}
        </AgencyDialog>
    );
}

function KeyStatus({ apiKey }: { apiKey: AgencyAPIKey }) {
    const live = agencyKeyLive(apiKey);
    const label = live ? "active" : apiKey.status === "revoked" ? "revoked" : "expired";
    return (
        <span
            className={cn(
                "inline-flex items-center gap-1 h-5 px-1.5 rounded text-[10px] uppercase tracking-[0.08em] font-medium",
                live ? "bg-emerald-50 text-emerald-700" : label === "revoked" ? "bg-rose-50 text-rose-700" : "bg-slate-100 text-slate-600",
            )}
        >
            {label}
        </span>
    );
}

function AgencyAPIKeys() {
    const { agency } = useCurrentAgency();
    const agencyId = agency?.id ?? "";
    const canManage = agency?.role === "owner" || agency?.role === "admin";
    const keys = useAgencyAPIKeys(agency?.id, canManage);
    const revoke = useRevokeAgencyAPIKey(agencyId);
    const confirm = useConfirm();
    const [creating, setCreating] = React.useState(false);
    const [selected, setSelected] = React.useState<AgencyAPIKey | null>(null);

    if (!agency) return null;
    if (!canManage) {
        return (
            <PageBody>
                <EmptyBlock title="Owners and admins manage API keys" body="Ask an owner or admin of the agency for a key, or for access to this page." />
            </PageBody>
        );
    }

    const list = keys.data?.pages.flatMap((p) => p.data) ?? [];
    const askRevoke = (k: AgencyAPIKey) =>
        confirm.show(
            `Revoke ${k.name}? Anything using it stops working on its next request, in every client workspace.`,
            async () => {
                try {
                    await revoke.mutateAsync(k.id);
                    setSelected(null);
                } catch (e) {
                    toast.error(buildError(e as unknown as AppError));
                }
            },
            undefined,
            { title: "Revoke key", confirmLabel: "Revoke" },
        );

    return (
        <PageBody>
            <SectionBar label="Keys" count={list.length}>
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={() => setCreating(true)}>
                    Create key
                </TopbarAction>
            </SectionBar>
            <p className="px-5 py-2.5 text-[11.5px] text-slate-500 border-b border-slate-200/60">
                One key reaches every client workspace and this agency&apos;s own endpoints. Each request names its client in the{" "}
                <span className="font-mono">{ORG_HEADER}</span> header, and a workspace stops being reachable the moment it leaves the agency.
            </p>
            {list.map((k) => {
                const live = agencyKeyLive(k);
                return (
                    <div
                        key={k.id}
                        role="button"
                        tabIndex={0}
                        onClick={() => setSelected(k)}
                        onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === " ") {
                                e.preventDefault();
                                setSelected(k);
                            }
                        }}
                        className="group min-h-11 px-5 py-1.5 flex items-center gap-3 border-b border-slate-200/60 hover:bg-slate-50/80 cursor-pointer"
                    >
                        <KeyIcon className={cn("w-3.5 h-3.5 shrink-0", live ? "text-slate-500" : "text-slate-300")} />
                        <div className="flex-1 min-w-0">
                            <div className="text-[12.5px] text-slate-900 font-medium truncate">{k.name}</div>
                            <div className="font-mono text-[10.5px] text-slate-400 truncate">
                                {k.key_prefix}…{k.key_suffix}
                                {k.allowed_ips.length > 0 && ` · ${k.allowed_ips.length} IP rule${k.allowed_ips.length === 1 ? "" : "s"}`}
                            </div>
                        </div>
                        <KeyStatus apiKey={k} />
                        <span className="hidden md:inline text-[10.5px] text-slate-400 w-28 text-right">{fmtRelative(k.last_used_at)}</span>
                        {live && (
                            <PopoverMenu align="end">
                                <PopoverMenuTrigger asChild>
                                    <button
                                        type="button"
                                        aria-label={`Actions for ${k.name}`}
                                        onClick={(e) => e.stopPropagation()}
                                        className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 focus:opacity-100"
                                    >
                                        <MoreHorizontalIcon className="w-3.5 h-3.5" />
                                    </button>
                                </PopoverMenuTrigger>
                                <PopoverMenuContent minWidth={180}>
                                    <PopoverMenuItem onSelect={() => setSelected(k)}>Rename or restrict</PopoverMenuItem>
                                    <PopoverMenuItem danger onSelect={() => askRevoke(k)}>
                                        Revoke
                                    </PopoverMenuItem>
                                </PopoverMenuContent>
                            </PopoverMenu>
                        )}
                    </div>
                );
            })}
            {keys.hasNextPage && (
                <div className="px-5 py-3">
                    <button type="button" onClick={() => void keys.fetchNextPage()} disabled={keys.isFetchingNextPage} className={ghostBtn}>
                        {keys.isFetchingNextPage ? "Loading…" : "Show more"}
                    </button>
                </div>
            )}
            {!keys.isLoading && list.length === 0 && (
                <EmptyBlock
                    title="No agency API keys yet"
                    body="Create a key to call Warmbly for every client from one integration."
                    cta={
                        <button type="button" onClick={() => setCreating(true)} className={primaryBtn}>
                            <PlusIcon className="w-3 h-3" />
                            Create key
                        </button>
                    }
                />
            )}
            <CreateKeyDialog open={creating} onClose={() => setCreating(false)} agencyId={agencyId} />
            <EditKeyDialog apiKey={selected} onClose={() => setSelected(null)} agencyId={agencyId} onRevoke={askRevoke} />
        </PageBody>
    );
}

export default function AgencyAPIKeysPage() {
    return (
        <AgencyPage title="API keys">
            <AgencyAPIKeys />
        </AgencyPage>
    );
}
