// Vendor import, step one: pick a saved vendor account, or connect one with
// its API key. The key is verified with the vendor before it is stored and is
// never shown again; updating it replaces it.
import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast/headless";
import {
    ChevronLeftIcon,
    ChevronRightIcon,
    ExternalLinkIcon,
    KeyRoundIcon,
    Loader2Icon,
    MoreHorizontalIcon,
    PlugIcon,
    PlusIcon,
    RefreshCwIcon,
    Trash2Icon,
} from "lucide-react";
import { Label, TextInput } from "@/components/ui/field";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { useGrantConfig } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import {
    useCreateVendorConnection,
    useDeleteVendorConnection,
    useStartVendorReconnect,
    useUpdateVendorConnection,
    useVendorReconnect,
} from "@/lib/api/hooks/app/emails/useMailboxVendors";
import type { VendorCatalogEntry, VendorConnection } from "@/lib/api/models/app/emails/MailboxSources";
import { reconnectOpen, vendorLabel } from "@/lib/api/models/app/emails/MailboxSources";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { cn } from "@/lib/utils";
import { plural } from "../importFields";
import { Banner, SecretInput, SectionLabel, SourceStatusPill } from "../parts";
import { SkeletonCards } from "../Discovering";

type Mode = { kind: "list" } | { kind: "vendors" } | { kind: "form"; vendor: VendorCatalogEntry; updating?: VendorConnection };

function hostOf(url: string): string {
    try {
        return new URL(url).host.replace(/^www\./, "");
    } catch {
        return url;
    }
}

export default function VendorAccountStep({
    catalog,
    catalogError,
    connections,
    loading,
    selectedId,
    onSelect,
    onDirtyChange,
}: {
    catalog: VendorCatalogEntry[];
    catalogError?: string | null;
    connections: VendorConnection[];
    loading: boolean;
    selectedId: string | null;
    /** A picked account; `advance` moves to its mailboxes. */
    onSelect: (conn: VendorConnection | null, advance: boolean) => void;
    onDirtyChange: (dirty: boolean) => void;
}) {
    const confirm = useConfirm();
    // A vendor reconnect ends in a Google sign-in, which needs the instance's Google client.
    const googleSignin = !!useGrantConfig(true).data?.google_mailbox_client_id;
    const [mode, setMode] = React.useState<Mode | null>(null);
    // With nothing saved yet, the vendor grid is the first screen.
    const current: Mode = mode ?? (connections.length > 0 ? { kind: "list" } : { kind: "vendors" });
    const [formDirty, setFormDirty] = React.useState(false);
    React.useEffect(() => {
        onDirtyChange(current.kind === "form" && formDirty);
    }, [current.kind, formDirty, onDirtyChange]);

    const del = useDeleteVendorConnection();
    const reconnect = useStartVendorReconnect();

    const leaveForm = (to: Mode) => {
        if (current.kind === "form" && formDirty) {
            confirm.show("Discard what you entered for this vendor?", async () => {
                setFormDirty(false);
                setMode(to);
            });
            return;
        }
        setFormDirty(false);
        setMode(to);
    };

    const openUpdate = (conn: VendorConnection) => {
        const vendor = catalog.find((v) => v.id === conn.vendor);
        if (!vendor) {
            toast.error("This vendor is no longer supported here.");
            return;
        }
        setMode({ kind: "form", vendor, updating: conn });
    };

    const remove = (conn: VendorConnection) => {
        const name = vendorLabel(conn.vendor);
        confirm.show(
            `Remove ${conn.label}? The ${plural(conn.mailboxes, "mailbox", "mailboxes")} it brought in stay connected. They just stop picking up new credentials from ${name} on their own, so a password changed at ${name} has to be updated here by hand.`,
            async () => {
                try {
                    await del.mutateAsync(conn.id);
                    if (selectedId === conn.id) onSelect(null, false);
                    toast.success(`${conn.label} removed`);
                } catch (e) {
                    toast.error(buildError(e as AppError));
                }
            },
        );
    };

    const reconnectAll = (conn: VendorConnection) => {
        const name = vendorLabel(conn.vendor);
        confirm.show(
            `Have ${name} sign in again as every Google mailbox imported from ${conn.label}? Each one is reconnected in place with full mail access, keeping its history, campaigns and warmup. A mailbox ${name} cannot sign in to keeps working as it is.`,
            async () => {
                try {
                    const out = await reconnect.mutateAsync({ id: conn.id });
                    toast.success(`${name} is reconnecting ${plural(out.data.length, "mailbox", "mailboxes")}`);
                } catch (e) {
                    toast.error(buildError(e as AppError));
                }
            },
        );
    };

    if (loading) {
        return (
            <div className="p-4" role="status" aria-label="Loading your vendor accounts">
                <SkeletonCards count={3} />
            </div>
        );
    }

    return (
        <div className="p-4">
            <AnimatePresence mode="wait" initial={false}>
                <motion.div
                    key={current.kind === "form" ? `form-${current.vendor.id}-${current.updating?.id ?? "new"}` : current.kind}
                    initial={{ opacity: 0, x: current.kind === "list" ? -16 : 16 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: current.kind === "list" ? 16 : -16 }}
                    transition={{ duration: 0.16, ease: [0.22, 1, 0.36, 1] }}
                >
                    {current.kind === "list" && (
                        <div className="space-y-3">
                            <div>
                                <SectionLabel className="mb-1.5">Your vendor accounts</SectionLabel>
                                <div className="rounded-md border border-slate-200 divide-y divide-slate-100">
                                    {connections.map((conn) => (
                                        <ConnectionRow
                                            key={conn.id}
                                            conn={conn}
                                            signsIn={googleSignin && !!catalog.find((v) => v.id === conn.vendor)?.signs_in}
                                            onReconnect={() => reconnectAll(conn)}
                                            active={conn.id === selectedId}
                                            onOpen={() => {
                                                if (conn.status === "invalid") {
                                                    openUpdate(conn);
                                                    return;
                                                }
                                                onSelect(conn, true);
                                            }}
                                            onUpdate={() => openUpdate(conn)}
                                            onRemove={() => remove(conn)}
                                        />
                                    ))}
                                </div>
                            </div>
                            <button
                                type="button"
                                onClick={() => setMode({ kind: "vendors" })}
                                className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5 transition-colors"
                            >
                                <PlusIcon className="w-3 h-3" />
                                Connect another vendor
                            </button>
                        </div>
                    )}

                    {current.kind === "vendors" && (
                        <div className="space-y-3">
                            {connections.length > 0 && (
                                <button
                                    type="button"
                                    onClick={() => setMode({ kind: "list" })}
                                    className="h-6 -ml-1 px-1.5 rounded-md text-[11.5px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                                >
                                    <ChevronLeftIcon className="w-3 h-3" />
                                    Your vendor accounts
                                </button>
                            )}
                            <div>
                                <p className="text-[13px] font-medium text-slate-900">Which vendor are your mailboxes with?</p>
                                <p className="text-[11.5px] text-slate-500 mt-0.5">
                                    Paste an API key from the vendor and pick the mailboxes to bring in. Their passwords come straight from
                                    the vendor.
                                </p>
                            </div>
                            {catalogError ? (
                                <Banner tone="red" title="Could not load the vendor list">
                                    {catalogError}
                                </Banner>
                            ) : (
                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                                    {catalog.map((v) => (
                                        <div
                                            key={v.id}
                                            className="group rounded-md border border-slate-200 hover:border-slate-300 hover:bg-slate-50/60 transition-colors flex items-center min-w-0"
                                        >
                                            <button
                                                type="button"
                                                onClick={() => setMode({ kind: "form", vendor: v })}
                                                className="flex-1 min-w-0 px-2.5 py-2 flex items-center gap-2.5 text-left"
                                            >
                                                <ProviderLogo id={v.id} size="lg" title={v.label} />
                                                <span className="min-w-0 flex-1">
                                                    <span className="block text-[12.5px] font-medium text-slate-900 truncate">{v.label}</span>
                                                    <span className="block text-[11px] text-slate-500 truncate">Connect with an API key</span>
                                                </span>
                                            </button>
                                            {v.website && (
                                                <a
                                                    href={v.website}
                                                    target="_blank"
                                                    rel="noreferrer"
                                                    title={`Open ${hostOf(v.website)}`}
                                                    className="shrink-0 mr-2 h-6 px-1.5 rounded text-[11px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                                                >
                                                    <span className="hidden sm:inline">{hostOf(v.website)}</span>
                                                    <ExternalLinkIcon className="w-2.5 h-2.5" />
                                                </a>
                                            )}
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>
                    )}

                    {current.kind === "form" && (
                        <VendorKeyForm
                            vendor={current.vendor}
                            updating={current.updating}
                            onDirty={setFormDirty}
                            onCancel={() => leaveForm(connections.length > 0 ? { kind: "list" } : { kind: "vendors" })}
                            onSaved={(conn, created) => {
                                setFormDirty(false);
                                setMode({ kind: "list" });
                                onSelect(conn, created || current.updating?.status === "invalid");
                            }}
                        />
                    )}
                </motion.div>
            </AnimatePresence>
        </div>
    );
}

function ConnectionRow({
    conn,
    signsIn,
    active,
    onOpen,
    onUpdate,
    onRemove,
    onReconnect,
}: {
    conn: VendorConnection;
    /** The vendor can reconnect its Google mailboxes in place. */
    signsIn: boolean;
    active: boolean;
    onOpen: () => void;
    onUpdate: () => void;
    onRemove: () => void;
    onReconnect: () => void;
}) {
    const invalid = conn.status === "invalid";
    const canReconnect = signsIn && !invalid && conn.mailboxes > 0;
    const progress = useVendorReconnect(conn.id, canReconnect).data?.data ?? [];
    const pending = progress.filter((m) => reconnectOpen(m.status)).length;
    const failed = progress.filter((m) => m.status === "failed").length;
    const done = progress.filter((m) => m.status === "connected").length;
    return (
        <div
            role="button"
            tabIndex={0}
            onClick={onOpen}
            onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onOpen();
                }
            }}
            className={cn(
                "group px-3 py-2.5 flex items-start gap-2.5 cursor-pointer transition-colors outline-none focus-visible:bg-slate-50",
                active ? "bg-sky-50/50" : "hover:bg-slate-50",
            )}
        >
            <ProviderLogo id={conn.vendor} size="lg" title={vendorLabel(conn.vendor) || conn.label} />
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 min-w-0">
                    <span className="text-[12.5px] font-medium text-slate-900 truncate">{conn.label}</span>
                    {conn.label !== vendorLabel(conn.vendor) && (
                        <span className="text-[11.5px] text-slate-500 truncate">{vendorLabel(conn.vendor)}</span>
                    )}
                    <SourceStatusPill status={conn.status} />
                </div>
                <div className="text-[11px] text-slate-500 mt-0.5 truncate">
                    {plural(conn.mailboxes, "mailbox", "mailboxes")} connected
                    {conn.last_used_at ? ` · used ${timeAgo(conn.last_used_at)}` : ` · added ${timeAgo(conn.created_at)}`}
                </div>
                {invalid && (
                    <p className="text-[11.5px] text-red-700 mt-1 leading-snug">
                        {conn.last_error || "The vendor no longer accepts this key."}
                    </p>
                )}
                {canReconnect && progress.length > 0 && (
                    <p role="status" className={cn("text-[11px] mt-1 leading-snug inline-flex items-center gap-1", pending > 0 ? "text-sky-700" : failed > 0 ? "text-rose-700" : "text-emerald-700")}>
                        {pending > 0 && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        {pending > 0
                            ? `Reconnecting ${plural(pending, "Google mailbox", "Google mailboxes")}`
                            : `Reconnected ${done} of ${progress.length}`}
                        {failed > 0 && `, ${failed} could not be. Their drawers say why.`}
                    </p>
                )}
            </div>
            <div className="shrink-0 flex items-center gap-1 self-center">
                {invalid && (
                    <button
                        type="button"
                        onClick={(e) => {
                            e.stopPropagation();
                            onUpdate();
                        }}
                        className="h-6 px-2 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[11.5px] font-medium inline-flex items-center gap-1 transition-colors"
                    >
                        <KeyRoundIcon className="w-3 h-3" />
                        Update key
                    </button>
                )}
                <div onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
                    <PopoverMenu align="end">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                aria-label={`More for ${conn.label}`}
                                className="size-6 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <MoreHorizontalIcon className="w-3.5 h-3.5" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={170}>
                            <PopoverMenuItem icon={<KeyRoundIcon className="w-3.5 h-3.5" />} onSelect={onUpdate}>
                                Update key
                            </PopoverMenuItem>
                            {canReconnect && (
                                <PopoverMenuItem icon={<RefreshCwIcon className="w-3.5 h-3.5" />} onSelect={onReconnect}>
                                    Reconnect all Google mailboxes
                                </PopoverMenuItem>
                            )}
                            <PopoverMenuItem danger icon={<Trash2Icon className="w-3.5 h-3.5" />} onSelect={onRemove}>
                                Remove
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                </div>
                <ChevronRightIcon className="w-3.5 h-3.5 text-slate-300 group-hover:text-slate-500 transition-colors" />
            </div>
        </div>
    );
}

function VendorKeyForm({
    vendor,
    updating,
    onDirty,
    onCancel,
    onSaved,
}: {
    vendor: VendorCatalogEntry;
    updating?: VendorConnection;
    onDirty: (dirty: boolean) => void;
    onCancel: () => void;
    onSaved: (conn: VendorConnection, created: boolean) => void;
}) {
    const create = useCreateVendorConnection();
    const update = useUpdateVendorConnection();
    const pending = create.isPending || update.isPending;
    const [values, setValues] = React.useState<Record<string, string>>({});
    const [label, setLabel] = React.useState("");
    const [error, setError] = React.useState<string | null>(null);
    const [tried, setTried] = React.useState(false);

    const dirty = label.trim() !== "" || Object.values(values).some((v) => v.trim() !== "");
    React.useEffect(() => {
        onDirty(dirty);
    }, [dirty, onDirty]);

    const missing = vendor.fields.filter((f) => f.required && !(values[f.key] ?? "").trim());

    async function submit(e: React.FormEvent) {
        e.preventDefault();
        setTried(true);
        if (missing.length > 0 || pending) return;
        setError(null);
        const fields: Record<string, string> = {};
        for (const f of vendor.fields) {
            const v = (values[f.key] ?? "").trim();
            if (v) fields[f.key] = v;
        }
        try {
            if (updating) {
                const conn = await update.mutateAsync({ id: updating.id, body: { fields, label: label.trim() || undefined } });
                toast.success(`Key updated for ${conn.label}`);
                onSaved(conn, false);
            } else {
                const conn = await create.mutateAsync({ vendor: vendor.id, label: label.trim(), fields });
                toast.success(`${vendor.label} connected`);
                onSaved(conn, true);
            }
        } catch (err) {
            const ae = err as AppError;
            setError(
                ae.code === "mailbox_vendor_rate_limited"
                    ? `${vendor.label} is limiting requests right now. Wait a minute and try again.`
                    : buildError(ae),
            );
        }
    }

    return (
        <form onSubmit={(e) => void submit(e)} className="space-y-3">
            <button
                type="button"
                onClick={onCancel}
                className="h-6 -ml-1 px-1.5 rounded-md text-[11.5px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
            >
                <ChevronLeftIcon className="w-3 h-3" />
                {updating ? "Your vendor accounts" : "All vendors"}
            </button>
            <div className="flex items-start gap-2.5">
                <ProviderLogo id={vendor.id} size="lg" title={vendor.label} />
                <div className="min-w-0 flex-1">
                    <p className="text-[13px] font-medium text-slate-900">
                        {updating ? `Update the key for ${updating.label}` : `Connect ${vendor.label}`}
                    </p>
                    <p className="text-[11.5px] text-slate-500 mt-0.5">
                        {updating
                            ? `The new key is checked with ${vendor.label} before it replaces the old one.`
                            : `Checked with ${vendor.label} before it is saved. Stored encrypted and never shown again.`}
                        {vendor.key_help_url && (
                            <>
                                {" "}
                                <a
                                    href={vendor.key_help_url}
                                    target="_blank"
                                    rel="noreferrer"
                                    className="inline-flex items-center gap-0.5 text-sky-700 underline decoration-sky-300 hover:decoration-sky-600"
                                >
                                    Where do I find this?
                                    <ExternalLinkIcon className="w-2.5 h-2.5" />
                                </a>
                            </>
                        )}
                    </p>
                </div>
            </div>

            {vendor.fields.map((f, i) => {
                const bad = tried && f.required && !(values[f.key] ?? "").trim();
                const set = (v: string) => setValues((prev) => ({ ...prev, [f.key]: v }));
                return (
                    <div key={f.key}>
                        <Label>
                            {f.label}
                            {!f.required && <span className="normal-case tracking-normal text-slate-400 font-normal"> (optional)</span>}
                        </Label>
                        {f.secret ? (
                            <SecretInput value={values[f.key] ?? ""} onChange={set} autoFocus={i === 0} invalid={bad} />
                        ) : (
                            <TextInput value={values[f.key] ?? ""} onChange={set} autoFocus={i === 0} invalid={bad} className="w-full" />
                        )}
                        {f.help && <p className="mt-1 text-[11px] text-slate-500">{f.help}</p>}
                    </div>
                );
            })}

            <div>
                <Label>
                    Name <span className="normal-case tracking-normal text-slate-400 font-normal">(optional)</span>
                </Label>
                <TextInput
                    value={label}
                    onChange={setLabel}
                    maxLength={80}
                    placeholder={updating ? updating.label : `${vendor.label}, e.g. the client or project`}
                    className="w-full"
                />
            </div>

            {error && (
                <Banner tone="red" title={updating ? "The key was not updated" : `${vendor.label} did not accept this`}>
                    {error}
                </Banner>
            )}

            <div className="flex items-center gap-2 pt-1">
                <button
                    type="submit"
                    aria-disabled={pending}
                    className={cn(
                        "h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors",
                        pending && "opacity-60",
                    )}
                >
                    {pending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <PlugIcon className="w-3 h-3" />}
                    {pending ? "Checking with the vendor…" : updating ? "Update key" : `Connect ${vendor.label}`}
                </button>
                {tried && missing.length > 0 && (
                    <span className="text-[11.5px] text-amber-700">Fill in {missing.map((f) => f.label).join(", ")}.</span>
                )}
            </div>
        </form>
    );
}
