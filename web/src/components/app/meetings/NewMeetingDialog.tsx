// NewMeetingDialog — create a meeting/call natively, with no redirect to an
// external scheduler. Reused anywhere a contact is in view (Meetings page,
// inbox contact rail, contact detail). When opened with a prefill it locks the
// meeting to that contact (contact_id) so attribution is exact.

import React from "react";
import { Loader2Icon, XIcon } from "lucide-react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import useCreateMeeting from "@/lib/api/hooks/app/meetings/useCreateMeeting";

export interface MeetingPrefill {
    title?: string;
    name?: string;
    email?: string;
    contactId?: string;
}

// toLocalInputValue renders a Date as the value a <input type="datetime-local">
// expects (local time, no timezone suffix).
function toLocalInputValue(d: Date): string {
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export default function NewMeetingDialog({
    open,
    onClose,
    prefill,
}: {
    open: boolean;
    onClose: () => void;
    prefill?: MeetingPrefill;
}) {
    const create = useCreateMeeting();
    const [title, setTitle] = React.useState("");
    const [name, setName] = React.useState("");
    const [email, setEmail] = React.useState("");
    const [when, setWhen] = React.useState("");
    const [duration, setDuration] = React.useState(30);
    const [location, setLocation] = React.useState("");
    const [joinURL, setJoinURL] = React.useState("");

    // Seed fields (and default the time to the next hour) each time it opens.
    React.useEffect(() => {
        if (!open) return;
        const d = new Date(Date.now() + 60 * 60_000);
        d.setMinutes(0, 0, 0);
        setWhen(toLocalInputValue(d));
        setTitle(prefill?.title ?? "");
        setName(prefill?.name ?? "");
        setEmail(prefill?.email ?? "");
        setDuration(30);
        setLocation("");
        setJoinURL("");
    }, [open, prefill?.title, prefill?.name, prefill?.email]);

    const lockedContact = !!prefill?.contactId;
    const canSubmit = !!when && (!!name.trim() || !!email.trim()) && !create.isPending;

    const submit = async () => {
        if (!canSubmit) return;
        const dt = new Date(when);
        if (isNaN(dt.getTime())) {
            toast.error("Pick a valid date and time");
            return;
        }
        try {
            await create.mutateAsync({
                title: title.trim() || "Call",
                invitee_name: name.trim(),
                invitee_email: email.trim(),
                scheduled_for: dt.toISOString(),
                duration_minutes: duration > 0 ? duration : undefined,
                location: location.trim() || undefined,
                join_url: joinURL.trim() || undefined,
                contact_id: prefill?.contactId,
            });
            toast.success("Meeting created");
            onClose();
        } catch {
            toast.error("Could not create meeting");
        }
    };

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    onMouseDown={onClose}
                    className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        initial={{ opacity: 0, scale: 0.97, y: 8 }}
                        animate={{ opacity: 1, scale: 1, y: 0 }}
                        exit={{ opacity: 0, scale: 0.97, y: 8 }}
                        transition={{ duration: 0.15 }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-md bg-white rounded-xl border border-slate-200 shadow-xl overflow-hidden"
                    >
                        <div className="h-11 px-4 flex items-center border-b border-slate-200">
                            <span className="text-[13px] font-medium text-slate-900">New meeting</span>
                            <button
                                type="button"
                                onClick={onClose}
                                className="ml-auto h-7 w-7 rounded-md inline-flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-100"
                            >
                                <XIcon className="w-4 h-4" />
                            </button>
                        </div>

                        <div className="p-4 space-y-3">
                            <div>
                                <Label>Title</Label>
                                <TextInput value={title} onChange={setTitle} placeholder="Discovery call" />
                            </div>
                            <div className="grid grid-cols-2 gap-3">
                                <div>
                                    <Label>Contact name</Label>
                                    <TextInput value={name} onChange={setName} placeholder="Jane Doe" />
                                </div>
                                <div>
                                    <Label>Contact email</Label>
                                    <TextInput value={email} onChange={setEmail} placeholder="jane@acme.com" disabled={lockedContact} />
                                </div>
                            </div>
                            {!lockedContact && (
                                <p className="text-[11px] text-slate-400 -mt-1.5">
                                    We link the meeting to a contact by email when one matches.
                                </p>
                            )}
                            <div className="grid grid-cols-2 gap-3">
                                <div>
                                    <Label>When</Label>
                                    <input
                                        type="datetime-local"
                                        value={when}
                                        onChange={(e) => setWhen(e.target.value)}
                                        className="w-full h-7 px-2 rounded-md border border-slate-200 text-[12.5px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 tabular-nums"
                                    />
                                </div>
                                <div>
                                    <Label>Duration</Label>
                                    <NumberInput value={duration} onChange={setDuration} min={5} max={480} step={5} suffix="min" />
                                </div>
                            </div>
                            <div>
                                <Label>Location (optional)</Label>
                                <TextInput value={location} onChange={setLocation} placeholder="Office, phone, etc." />
                            </div>
                            <div>
                                <Label>Meeting link (optional)</Label>
                                <TextInput value={joinURL} onChange={setJoinURL} placeholder="https://meet.google.com/…" />
                            </div>
                        </div>

                        <div className="h-12 px-4 flex items-center justify-end gap-2 border-t border-slate-200 bg-slate-50/60">
                            <button
                                type="button"
                                onClick={onClose}
                                className="h-7 px-3 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={!canSubmit}
                                className="h-7 px-3 rounded-md text-[12px] font-medium bg-sky-600 hover:bg-sky-700 text-white inline-flex items-center gap-1.5 disabled:opacity-60 disabled:cursor-not-allowed"
                            >
                                {create.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                                Create meeting
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
