// Connections: external MCP servers that add tools to the AI assistant. Admin
// connects a server, reviews the tools Warmbly discovered, then enables it. Once
// enabled, the assistant can use those tools, always asking before it runs one.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import {
    PlugIcon,
    PlusIcon,
    XIcon,
    Trash2Icon,
    Loader2Icon,
    RefreshCwIcon,
    AlertTriangleIcon,
    WrenchIcon,
} from "lucide-react";
import {
    useMCPServers,
    useCreateMCPServer,
    useUpdateMCPServer,
    useDeleteMCPServer,
    useRefreshMCPServer,
} from "@/lib/api/hooks/app/mcp/useMCPServers";
import type { MCPServer } from "@/lib/api/models/app/mcp/MCPServer";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { usePermission } from "@/hooks/usePermission";
import { useConfirm } from "@/hooks/context/confirm";
import { TextInput } from "@/components/ui/field";
import { SectionShell, Section, Toggle } from "../_components/SectionShell";

export default function ConnectionsSettingsPage() {
    const canManage = usePermission("MANAGE_SETTINGS");
    const servers = useMCPServers();
    const [addOpen, setAddOpen] = React.useState(false);

    const rows = servers.data?.data ?? [];

    return (
        <SectionShell
            title="Connections"
            description="Connect external MCP servers to give the AI assistant extra tools. Warmbly discovers each server's tools; you review and enable them. The assistant always asks before running an external tool."
            actions={
                canManage ? (
                    <button
                        type="button"
                        onClick={() => setAddOpen(true)}
                        className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                    >
                        <PlusIcon className="w-3 h-3" />
                        Add server
                    </button>
                ) : undefined
            }
        >
            <Section eyebrow="MCP servers" description="Only enabled servers' tools are available to the AI.">
                {servers.isPending ? (
                    <div className="h-16 rounded bg-slate-100 animate-pulse" />
                ) : rows.length === 0 ? (
                    <p className="text-[12px] text-slate-500 leading-relaxed">
                        No connections yet. Add an MCP server to extend what the assistant can do.
                    </p>
                ) : (
                    <div className="space-y-3">
                        {rows.map((s) => (
                            <ServerCard key={s.id} server={s} canManage={canManage} />
                        ))}
                    </div>
                )}
            </Section>

            <AddServerDrawer open={addOpen} onClose={() => setAddOpen(false)} />
        </SectionShell>
    );
}

function ServerCard({ server, canManage }: { server: MCPServer; canManage: boolean }) {
    const update = useUpdateMCPServer();
    const del = useDeleteMCPServer();
    const refresh = useRefreshMCPServer();
    const confirm = useConfirm();

    async function toggle(next: boolean) {
        try {
            await update.mutateAsync({ id: server.id, data: { enabled: next } });
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }
    function remove() {
        confirm.show(`Disconnect "${server.name}"? Its tools will be removed from the assistant.`, async () => {
            await del.mutateAsync(server.id);
            toast.success("Disconnected");
        });
    }
    async function reDiscover() {
        try {
            await toast.promise(refresh.mutateAsync(server.id), {
                loading: "Refreshing tools…",
                success: "Tools refreshed",
                error: (e: AppError) => buildError(e),
            });
        } catch {
            /* surfaced */
        }
    }

    return (
        <div className="rounded-md border border-slate-200 p-3">
            <div className="flex items-start gap-3">
                <div className="size-8 rounded-md bg-slate-100 text-slate-500 flex items-center justify-center shrink-0">
                    <PlugIcon className="w-4 h-4" />
                </div>
                <div className="min-w-0 flex-1">
                    <div className="text-[12.5px] font-medium text-slate-900">{server.name}</div>
                    <div className="text-[11px] text-slate-400 font-mono truncate">{server.url}</div>
                </div>
                {canManage && (
                    <div className="flex items-center gap-2 shrink-0">
                        <Toggle on={server.enabled} onChange={toggle} disabled={update.isPending || server.discovered_tools.length === 0} />
                        <button type="button" onClick={reDiscover} title="Refresh tools" className="size-6 rounded-md text-slate-400 hover:text-slate-700 hover:bg-slate-100 inline-flex items-center justify-center transition-colors">
                            {refresh.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RefreshCwIcon className="w-3 h-3" />}
                        </button>
                        <button type="button" onClick={remove} title="Disconnect" className="size-6 rounded-md text-slate-400 hover:text-white hover:bg-red-600 inline-flex items-center justify-center transition-colors">
                            <Trash2Icon className="w-3 h-3" />
                        </button>
                    </div>
                )}
            </div>

            {server.last_error ? (
                <div className="mt-2 flex items-start gap-1.5 rounded-md bg-red-50 border border-red-100 px-2 py-1.5 text-[11px] text-red-700">
                    <AlertTriangleIcon className="w-3 h-3 mt-0.5 shrink-0" />
                    <span className="min-w-0 break-words">Could not reach the server: {server.last_error}</span>
                </div>
            ) : server.discovered_tools.length > 0 ? (
                <div className="mt-2">
                    <div className="text-[10px] uppercase tracking-[0.12em] text-slate-400 mb-1">
                        {server.discovered_tools.length} tool{server.discovered_tools.length === 1 ? "" : "s"}
                    </div>
                    <div className="flex flex-wrap gap-1.5">
                        {server.discovered_tools.map((t) => (
                            <span key={t.name} title={t.description} className="inline-flex items-center gap-1 text-[10.5px] text-slate-600 bg-slate-50 border border-slate-200 rounded px-1.5 py-0.5">
                                <WrenchIcon className="w-2.5 h-2.5 text-slate-400" />
                                {t.name}
                            </span>
                        ))}
                    </div>
                </div>
            ) : (
                <p className="mt-2 text-[11px] text-slate-400">No tools discovered yet.</p>
            )}
        </div>
    );
}

function AddServerDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
    const create = useCreateMCPServer();
    const [name, setName] = React.useState("");
    const [url, setUrl] = React.useState("");
    const [auth, setAuth] = React.useState<"none" | "bearer">("none");
    const [token, setToken] = React.useState("");

    React.useEffect(() => {
        if (open) {
            setName("");
            setUrl("");
            setAuth("none");
            setToken("");
        }
    }, [open]);

    async function submit() {
        if (!name.trim() || !url.trim()) {
            toast.error("Name and URL are required");
            return;
        }
        try {
            await create.mutateAsync({ name, url, auth_type: auth, token: auth === "bearer" ? token : undefined });
            toast.success("Server connected. Review its tools, then enable it.");
            onClose();
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    return (
        <AnimatePresence>
            {open && (
                <>
                    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={onClose} className="fixed inset-0 z-40 bg-slate-900/30" />
                    <motion.aside
                        initial={{ x: "100%" }}
                        animate={{ x: 0 }}
                        exit={{ x: "100%" }}
                        transition={{ type: "spring", stiffness: 380, damping: 40 }}
                        className="fixed right-0 top-0 z-50 h-full w-full sm:w-[460px] bg-white border-l border-slate-200 shadow-[0_0_60px_-12px_rgba(15,23,42,0.3)] flex flex-col"
                    >
                        <div className="shrink-0 px-5 h-14 flex items-center gap-3 border-b border-slate-200">
                            <div className="text-[13px] font-semibold text-slate-900 flex-1">Connect MCP server</div>
                            <button onClick={onClose} className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors">
                                <XIcon className="w-4 h-4" />
                            </button>
                        </div>
                        <div className="flex-1 min-h-0 overflow-y-auto px-5 py-4 space-y-4">
                            <Field label="Name">
                                <TextInput value={name} onChange={setName} placeholder="Company docs" className="w-full" />
                            </Field>
                            <Field label="Server URL" hint="Must be an https URL to a streamable-HTTP MCP endpoint.">
                                <TextInput value={url} onChange={setUrl} placeholder="https://mcp.example.com/mcp" className="w-full font-mono" />
                            </Field>
                            <Field label="Authentication">
                                <div className="inline-flex rounded-md border border-slate-200 bg-slate-50 p-0.5 text-[12px]">
                                    {(["none", "bearer"] as const).map((a) => (
                                        <button
                                            key={a}
                                            type="button"
                                            onClick={() => setAuth(a)}
                                            className={`h-6 px-3 rounded font-medium transition-colors ${auth === a ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-700"}`}
                                        >
                                            {a === "none" ? "None" : "Bearer token"}
                                        </button>
                                    ))}
                                </div>
                            </Field>
                            {auth === "bearer" && (
                                <Field label="Token" hint="Stored encrypted. Never shown again.">
                                    <TextInput value={token} onChange={setToken} placeholder="secret token" type="password" className="w-full font-mono" />
                                </Field>
                            )}
                        </div>
                        <div className="shrink-0 h-14 px-5 flex items-center border-t border-slate-200 bg-slate-50/60">
                            <button
                                type="button"
                                onClick={submit}
                                disabled={create.isPending}
                                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {create.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <PlugIcon className="w-3 h-3" />}
                                Connect
                            </button>
                        </div>
                    </motion.aside>
                </>
            )}
        </AnimatePresence>
    );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
    return (
        <div>
            <div className="text-[12.5px] font-medium text-slate-900">{label}</div>
            {hint && <div className="text-[11px] text-slate-500 mb-1.5">{hint}</div>}
            {children}
        </div>
    );
}
