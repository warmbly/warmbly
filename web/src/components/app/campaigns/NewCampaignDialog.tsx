// New campaign dialog — brae-density modal.
//
// Slim center-aligned card with the same chrome vocabulary as the rest
// of the dashboard: 48px header band, hairline dividers, 28px form
// inputs, slate-900 primary, ghost secondary. Submits via
// useCreateCampaign — react-query invalidates the list query so the new
// row appears without a refetch.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2Icon, MegaphoneIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast";
import { useNavigate } from "react-router-dom";
import useCreateCampaign from "@/lib/api/hooks/app/campaigns/useCreateCampaign";
import { Label, TextInput } from "@/components/ui/field";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

interface Props {
    open: boolean;
    onClose: () => void;
}

export function NewCampaignDialog({ open, onClose }: Props) {
    const navigate = useNavigate();
    const [name, setName] = React.useState("");
    const [description, setDescription] = React.useState("");
    const create = useCreateCampaign();

    React.useEffect(() => {
        if (!open) {
            setName("");
            setDescription("");
        }
    }, [open]);

    async function submit() {
        const trimmedName = name.trim();
        if (trimmedName.length < 3) {
            toast.error("Name must be at least 3 characters");
            return;
        }
        try {
            const created = await create.mutateAsync({
                name: trimmedName,
                description: description.trim(),
            });
            toast.success("Campaign created");
            onClose();
            if (created?.id) navigate(`/app/campaigns/${created.id}`);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={onClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onClick={(e) => e.stopPropagation()}
                        className="w-full max-w-[440px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <MegaphoneIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                New
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">
                                Campaign
                            </span>
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <form
                            onSubmit={(e) => {
                                e.preventDefault();
                                submit();
                            }}
                            className="px-4 py-4 space-y-3"
                        >
                            <div>
                                <Label>Name</Label>
                                <TextInput
                                    value={name}
                                    onChange={setName}
                                    placeholder="Q1 outbound"
                                    autoFocus
                                    className="w-full"
                                />
                            </div>
                            <div>
                                <Label>Description</Label>
                                <TextInput
                                    value={description}
                                    onChange={setDescription}
                                    placeholder="Optional — what this campaign targets"
                                    className="w-full"
                                />
                            </div>
                            <p className="text-[11px] text-slate-400 leading-relaxed pt-1">
                                You'll add sequences, scheduling and contacts after creation.
                            </p>
                        </form>

                        <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5">
                            <button
                                type="button"
                                onClick={onClose}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={create.isPending}
                                className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {create.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                Create
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
