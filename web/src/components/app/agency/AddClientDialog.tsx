// Add a client: open a new workspace for it, or bring in one you own.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { BuildingIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { FieldError, Label, TextInput } from "@/components/ui/field";
import { useAppStore } from "@/stores";
import { useAttachAgencyClient, useCreateAgencyClient } from "@/lib/api/hooks/app/agency/useAgency";
import type { AgencyClient } from "@/lib/api/models/app/agency/Agency";
import { WORKSPACE_NAME_MAX, nameError, normalizeName } from "@/lib/displayName";
import { browserTimezone } from "@/lib/timezone";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { AgencyDialog } from "./AgencyDialog";
import { ghostBtn, primaryBtn } from "./styles";

type Mode = "new" | "existing";

export function AddClientDialog({
    open,
    onClose,
    agencyId,
    clients,
}: {
    open: boolean;
    onClose: () => void;
    agencyId: string;
    clients: AgencyClient[];
}) {
    const [mode, setMode] = React.useState<Mode>("new");
    const [name, setName] = React.useState("");
    const [orgId, setOrgId] = React.useState<string | null>(null);
    const organizations = useAppStore((s) => s.organizations);
    const create = useCreateAgencyClient(agencyId);
    const attach = useAttachAgencyClient(agencyId);

    React.useEffect(() => {
        if (!open) {
            setMode("new");
            setName("");
            setOrgId(null);
        }
    }, [open]);

    const taken = new Set(clients.map((c) => c.organization_id));
    const owned = organizations.filter((o) => o.role === "owner" && !taken.has(o.id));
    const error = mode === "new" ? nameError("Client name", name, "workspace") : orgId ? null : "Pick a workspace you own.";
    const pending = create.isPending || attach.isPending;

    async function submit() {
        if (error) {
            toast.error(error);
            return;
        }
        try {
            if (mode === "new") {
                await toast.promise(create.mutateAsync({ name: normalizeName(name), timezone: browserTimezone() || undefined }), {
                    loading: "Creating client workspace…",
                    success: "Client added",
                    error: (e: AppError) => buildError(e),
                });
            } else if (orgId) {
                await toast.promise(attach.mutateAsync(orgId), {
                    loading: "Adding workspace…",
                    success: "Client added",
                    error: (e: AppError) => buildError(e),
                });
            }
            onClose();
        } catch {
            /* surfaced by toast.promise */
        }
    }

    return (
        <AgencyDialog
            open={open}
            onClose={onClose}
            eyebrow="Add"
            title="Client"
            icon={<BuildingIcon className="w-3 h-3" />}
            footer={
                <>
                    <button type="button" onClick={onClose} className={cn("ml-auto", ghostBtn)}>
                        Cancel
                    </button>
                    <button type="button" onClick={() => void submit()} disabled={pending || !!error} className={primaryBtn}>
                        {pending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        {mode === "new" ? "Create client" : "Add workspace"}
                    </button>
                </>
            }
        >
            <div className="px-4 pt-3">
                <div className="inline-flex rounded-md border border-slate-200 p-0.5 bg-slate-50">
                    {(["new", "existing"] as const).map((m) => (
                        <button
                            key={m}
                            type="button"
                            onClick={() => setMode(m)}
                            className={cn(
                                "h-6 px-2.5 rounded text-[12px] transition-colors",
                                mode === m ? "bg-white text-slate-900 shadow-sm font-medium" : "text-slate-500 hover:text-slate-800",
                            )}
                        >
                            {m === "new" ? "New workspace" : "Existing workspace"}
                        </button>
                    ))}
                </div>
            </div>
            <AnimatePresence mode="wait" initial={false}>
                <motion.form
                    key={mode}
                    initial={{ opacity: 0, x: mode === "new" ? -12 : 12 }}
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: mode === "new" ? 12 : -12 }}
                    transition={{ duration: 0.16 }}
                    onSubmit={(e) => {
                        e.preventDefault();
                        void submit();
                    }}
                    className="px-4 py-4 space-y-3"
                >
                    {mode === "new" ? (
                        <div>
                            <Label>Client name</Label>
                            <TextInput
                                value={name}
                                onChange={setName}
                                placeholder="Acme Inc."
                                autoFocus
                                maxLength={WORKSPACE_NAME_MAX}
                                invalid={!!name.trim() && !!error}
                                className="w-full"
                            />
                            <FieldError message={name.trim() ? error : null} />
                            <p className="text-[11px] text-slate-400 leading-relaxed pt-2">
                                A new workspace owned by the agency owner, with your staff already in it. It does not count
                                toward anyone's workspace limit.
                            </p>
                        </div>
                    ) : owned.length === 0 ? (
                        <p className="text-[12px] text-slate-500 leading-relaxed">
                            You do not own a workspace that could join. Only a workspace's owner can bring it into an agency.
                        </p>
                    ) : (
                        <div>
                            <Label>Workspace</Label>
                            <div role="radiogroup" className="max-h-56 overflow-auto rounded-md border border-slate-200 divide-y divide-slate-100">
                                {owned.map((o) => (
                                    <button
                                        key={o.id}
                                        type="button"
                                        role="radio"
                                        aria-checked={orgId === o.id}
                                        onClick={() => setOrgId(o.id)}
                                        className={cn(
                                            "w-full h-8 px-2.5 flex items-center gap-2 text-left text-[12.5px] transition-colors",
                                            orgId === o.id ? "bg-sky-50 text-sky-700" : "text-slate-700 hover:bg-slate-50",
                                        )}
                                    >
                                        <span className={cn("size-3 rounded-full border", orgId === o.id ? "border-sky-600 bg-sky-600" : "border-slate-300")} />
                                        <span className="truncate">{o.name}</span>
                                    </button>
                                ))}
                            </div>
                            <p className="text-[11px] text-slate-400 leading-relaxed pt-2">
                                The workspace keeps its data and members. Its plan comes from the agency from now on, so a
                                workspace paying for its own plan has to cancel it first.
                            </p>
                        </div>
                    )}
                </motion.form>
            </AnimatePresence>
        </AgencyDialog>
    );
}
