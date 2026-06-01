import { useState } from "react";
import toast from "react-hot-toast";
import { KeyRound, Trash2, Plus, Check, Pencil } from "lucide-react";
import { Section } from "../_components/SectionShell";
import { Loading } from "@/components/loader";
import { useConfirm } from "@/hooks/context/confirm";
import usePasskeys from "@/lib/api/hooks/auth/usePasskeys";
import useDeletePasskey from "@/lib/api/hooks/auth/useDeletePasskey";
import useRenamePasskey from "@/lib/api/hooks/auth/useRenamePasskey";
import { registerPasskey, passkeySupported, PasskeyCancelled } from "@/lib/passkey";
import type Passkey from "@/lib/api/models/auth/Passkey";

const fmt = (d: Date | string) =>
    new Date(d).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });

export default function PasskeyManager() {
    const { data: passkeys, isLoading, refetch } = usePasskeys();
    const del = useDeletePasskey();
    const rename = useRenamePasskey();
    const confirm = useConfirm();

    const [adding, setAdding] = useState(false);
    const [editingId, setEditingId] = useState<string | null>(null);
    const [draftName, setDraftName] = useState("");

    const supported = passkeySupported();

    const handleAdd = async () => {
        setAdding(true);
        try {
            const created = await registerPasskey();
            toast.success("Passkey added");
            await refetch();
            // Drop straight into renaming so the user can give it a label.
            setEditingId(created.id);
            setDraftName(created.name);
        } catch (e) {
            if (!(e instanceof PasskeyCancelled)) {
                toast.error((e as Error)?.message || "Couldn't create a passkey.");
            }
        } finally {
            setAdding(false);
        }
    };

    const submitRename = async (id: string) => {
        const name = draftName.trim();
        if (!name) {
            setEditingId(null);
            return;
        }
        try {
            await rename.mutateAsync({ id, name });
        } catch {
            toast.error("Couldn't rename passkey.");
        } finally {
            setEditingId(null);
        }
    };

    const handleDelete = (p: Passkey) => {
        confirm?.show(`Remove the passkey "${p.name}"? You won't be able to use it to sign in.`, async () => {
            try {
                await del.mutateAsync(p.id);
                toast.success("Passkey removed");
            } catch {
                toast.error("Couldn't remove passkey.");
            }
        });
    };

    return (
        <Section
            eyebrow="Passkeys"
            description="Sign in with Touch ID, Face ID, Windows Hello, or a security key — no password or email code."
        >
            {!supported ? (
                <p className="text-[12px] text-slate-500 leading-relaxed">
                    This browser doesn't support passkeys. Try a recent version of Chrome, Safari, Edge, or
                    Firefox.
                </p>
            ) : (
                <div className="space-y-3">
                    {isLoading ? (
                        <div className="flex items-center gap-2 text-[12px] text-slate-400 py-2">
                            <Loading className="!w-4 h-4 text-slate-400" /> Loading passkeys…
                        </div>
                    ) : passkeys && passkeys.length > 0 ? (
                        <div className="rounded-md border border-slate-200 divide-y divide-slate-200 bg-white">
                            {passkeys.map((p) => (
                                <div key={p.id} className="flex items-center gap-3 px-3 py-2.5">
                                    <div className="w-8 h-8 rounded-lg bg-sky-50 flex items-center justify-center shrink-0">
                                        <KeyRound className="w-4 h-4 text-sky-500" />
                                    </div>
                                    <div className="min-w-0 flex-1">
                                        {editingId === p.id ? (
                                            <input
                                                autoFocus
                                                value={draftName}
                                                maxLength={60}
                                                onChange={(e) => setDraftName(e.target.value)}
                                                onBlur={() => submitRename(p.id)}
                                                onKeyDown={(e) => {
                                                    if (e.key === "Enter") submitRename(p.id);
                                                    if (e.key === "Escape") setEditingId(null);
                                                }}
                                                className="w-full h-7 rounded-md border border-slate-200 px-2 text-[12.5px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                                            />
                                        ) : (
                                            <div className="flex items-center gap-2">
                                                <span className="text-[12.5px] font-medium text-slate-900 truncate">
                                                    {p.name}
                                                </span>
                                                {p.backup_state && (
                                                    <span className="text-[10px] uppercase tracking-[0.08em] font-medium rounded-sm px-1 bg-emerald-50 text-emerald-700">
                                                        Synced
                                                    </span>
                                                )}
                                            </div>
                                        )}
                                        <div className="text-[11px] text-slate-500 truncate mt-0.5">
                                            {p.provider ? `${p.provider} · ` : ""}Added {fmt(p.created_at)}
                                            {p.last_used_at ? ` · Last used ${fmt(p.last_used_at)}` : ""}
                                        </div>
                                    </div>
                                    {editingId === p.id ? (
                                        <button
                                            type="button"
                                            onClick={() => submitRename(p.id)}
                                            className="h-7 w-7 inline-flex items-center justify-center rounded-md text-sky-600 hover:bg-sky-50"
                                            aria-label="Save name"
                                        >
                                            <Check className="w-4 h-4" />
                                        </button>
                                    ) : (
                                        <div className="flex items-center gap-1 shrink-0">
                                            <button
                                                type="button"
                                                onClick={() => {
                                                    setEditingId(p.id);
                                                    setDraftName(p.name);
                                                }}
                                                className="h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:text-slate-700 hover:bg-slate-100 transition-colors"
                                                aria-label="Rename passkey"
                                            >
                                                <Pencil className="w-3.5 h-3.5" />
                                            </button>
                                            <button
                                                type="button"
                                                onClick={() => handleDelete(p)}
                                                disabled={del.isPending}
                                                className="h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 transition-colors disabled:opacity-50"
                                                aria-label="Remove passkey"
                                            >
                                                <Trash2 className="w-3.5 h-3.5" />
                                            </button>
                                        </div>
                                    )}
                                </div>
                            ))}
                        </div>
                    ) : (
                        <p className="text-[12px] text-slate-500 leading-relaxed">
                            You don't have any passkeys yet. Add one to sign in without a password.
                        </p>
                    )}

                    <button
                        type="button"
                        onClick={handleAdd}
                        disabled={adding}
                        className="h-8 px-3 rounded-md border border-slate-200 hover:border-sky-300 hover:bg-sky-50/50 text-[12.5px] font-medium text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 disabled:pointer-events-none"
                    >
                        {adding ? (
                            <Loading className="!w-3.5 h-3.5 text-slate-500" />
                        ) : (
                            <Plus className="w-3.5 h-3.5" />
                        )}
                        Add a passkey
                    </button>
                </div>
            )}
        </Section>
    );
}
