// New contact dialog — brae-density modal mirroring NewCampaignDialog.
//
// Required: email. Everything else optional. Submits via useAddContacts
// which posts an array (the endpoint is bulk-shaped); we send a single
// item.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Loader2Icon, UserPlusIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast";
import useAddContacts from "@/lib/api/hooks/app/contacts/useAddContacts";
import type { AddContact } from "@/components/app/AddContacts";
import { Label, TextInput } from "@/components/ui/field";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

interface Props {
    open: boolean;
    onClose: () => void;
}

export function NewContactDialog({ open, onClose }: Props) {
    const [email, setEmail] = React.useState("");
    const [firstName, setFirstName] = React.useState("");
    const [lastName, setLastName] = React.useState("");
    const [company, setCompany] = React.useState("");
    const [phone, setPhone] = React.useState("");
    const add = useAddContacts();

    React.useEffect(() => {
        if (!open) {
            setEmail("");
            setFirstName("");
            setLastName("");
            setCompany("");
            setPhone("");
        }
    }, [open]);

    function isValidEmail(s: string) {
        return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s);
    }

    async function submit() {
        const e = email.trim();
        if (!isValidEmail(e)) {
            toast.error("Enter a valid email");
            return;
        }
        const contact: AddContact = {
            email: e,
            first_name: firstName.trim(),
            last_name: lastName.trim(),
            company: company.trim(),
            phone: phone.trim(),
            campaigns: [],
            custom_fields: {},
        };
        try {
            await toast.promise(add.mutateAsync([contact]), {
                loading: "Adding contact…",
                success: "Contact added",
                error: (err: AppError) => buildError(err),
            });
            onClose();
        } catch {
            /* toast.promise already surfaced */
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
                        className="w-full max-w-[460px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <UserPlusIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                New
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">
                                Contact
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
                                <Label>Email</Label>
                                <TextInput
                                    value={email}
                                    onChange={setEmail}
                                    placeholder="name@company.com"
                                    type="email"
                                    autoFocus
                                    className="w-full"
                                />
                            </div>
                            <div className="grid grid-cols-2 gap-2">
                                <div>
                                    <Label>First name</Label>
                                    <TextInput value={firstName} onChange={setFirstName} className="w-full" />
                                </div>
                                <div>
                                    <Label>Last name</Label>
                                    <TextInput value={lastName} onChange={setLastName} className="w-full" />
                                </div>
                            </div>
                            <div>
                                <Label>Company</Label>
                                <TextInput value={company} onChange={setCompany} className="w-full" />
                            </div>
                            <div>
                                <Label>Phone</Label>
                                <TextInput value={phone} onChange={setPhone} className="w-full" />
                            </div>
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
                                disabled={add.isPending}
                                className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {add.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                                Add
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
