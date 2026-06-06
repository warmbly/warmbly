// Notes tab — CRM-style internal notes attached to a contact.
//
// The compose row is sticky-ish at the top. Existing notes render
// newest-first as small cards with edit / delete affordances. Author
// is shown as the bare user id for now — the backend doesn't yet
// expand it to a display name.

import React from "react";
import { CheckIcon, Loader2Icon, PencilIcon, PlusIcon, TrashIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast";
import useContactNotes from "@/lib/api/hooks/app/contacts/useContactNotes";
import useCreateContactNote from "@/lib/api/hooks/app/contacts/useCreateContactNote";
import useUpdateContactNote from "@/lib/api/hooks/app/contacts/useUpdateContactNote";
import useDeleteContactNote from "@/lib/api/hooks/app/contacts/useDeleteContactNote";
import { useConfirm } from "@/hooks/context/confirm";
import type ContactNote from "@/lib/api/models/app/crm/ContactNote";
import { fmtRelative, fmtAbsolute } from "./format";

// The notes endpoint returns either a bare array or a paginated
// {data, pagination} envelope depending on the historical client
// path. Coerce both shapes so the tab works against either.
type MaybeEnveloped = ContactNote[] | { data: ContactNote[] };

function asList(raw: MaybeEnveloped | undefined): ContactNote[] {
    if (!raw) return [];
    if (Array.isArray(raw)) return raw;
    return Array.isArray(raw.data) ? raw.data : [];
}

export default function NotesTab({ contactId }: { contactId: string }) {
    const notes = useContactNotes(contactId);
    const create = useCreateContactNote();
    const remove = useDeleteContactNote();
    const confirm = useConfirm();

    const [draft, setDraft] = React.useState("");

    const items = asList(notes.data as MaybeEnveloped | undefined);

    async function submit() {
        const content = draft.trim();
        if (!content) return;
        try {
            await toast.promise(
                create.mutateAsync({ contactId, data: { content } }),
                {
                    loading: "Adding note…",
                    success: "Note added",
                    error: "Could not add note",
                },
            );
            setDraft("");
        } catch {
            /* toast surfaced */
        }
    }

    function onDelete(noteId: string) {
        confirm.show("Delete this note? This cannot be undone.", async () => {
            try {
                await toast.promise(
                    remove.mutateAsync({ contactId, noteId }),
                    {
                        loading: "Deleting…",
                        success: "Note deleted",
                        error: "Could not delete note",
                    },
                );
            } catch {
                /* toast surfaced */
            }
        });
    }

    return (
        <div className="space-y-4">
            <div className="rounded-md border border-slate-200 bg-white focus-within:border-slate-400 transition-colors">
                <textarea
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    placeholder="Add a note about this contact — context for the next person who looks them up."
                    rows={3}
                    className="w-full resize-none px-3 py-2 text-[12px] text-slate-900 placeholder:text-slate-400 outline-none bg-transparent"
                />
                <div className="flex items-center justify-between border-t border-slate-100 px-2 py-1.5">
                    <span className="text-[10.5px] text-slate-400">
                        {draft.length}/10000
                    </span>
                    <button
                        type="button"
                        disabled={!draft.trim() || create.isPending}
                        onClick={submit}
                        className="h-6 px-2.5 rounded text-[11px] font-medium bg-slate-900 text-white hover:bg-slate-800 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                    >
                        {create.isPending ? (
                            <Loader2Icon className="w-3 h-3 animate-spin" />
                        ) : (
                            <PlusIcon className="w-3 h-3" />
                        )}
                        Add note
                    </button>
                </div>
            </div>

            {notes.isLoading ? (
                <div className="flex items-center justify-center py-8 text-slate-400">
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                </div>
            ) : notes.isError ? (
                <div className="rounded-md border border-red-200 bg-red-50/50 px-3 py-2.5 text-[11.5px] text-red-700">
                    Failed to load notes.
                </div>
            ) : items.length === 0 ? (
                <div className="rounded-md border border-dashed border-slate-200 px-3 py-8 text-[11.5px] text-slate-400 text-center">
                    No notes yet. The first one usually saves the next person
                    an hour.
                </div>
            ) : (
                <div className="space-y-1.5">
                    {items.map((n) => (
                        <NoteRow
                            key={n.id}
                            contactId={contactId}
                            note={n}
                            onDelete={() => onDelete(n.id)}
                        />
                    ))}
                </div>
            )}
        </div>
    );
}

function NoteRow({
    contactId,
    note,
    onDelete,
}: {
    contactId: string;
    note: ContactNote;
    onDelete: () => void;
}) {
    const update = useUpdateContactNote();
    const [editing, setEditing] = React.useState(false);
    const [draft, setDraft] = React.useState(note.content);

    async function save() {
        const content = draft.trim();
        if (!content || content === note.content) {
            setEditing(false);
            return;
        }
        try {
            await toast.promise(
                update.mutateAsync({ contactId, noteId: note.id, data: { content } }),
                { loading: "Saving…", success: "Note updated", error: "Could not save" },
            );
            setEditing(false);
        } catch {
            /* toast surfaced */
        }
    }

    return (
        <div className="rounded-md border border-slate-200 bg-white px-3 py-2 group">
            <div className="flex items-center justify-between gap-2">
                <span
                    className="text-[10.5px] text-slate-400 tabular-nums"
                    title={fmtAbsolute(note.created_at)}
                >
                    {fmtRelative(note.created_at)}
                </span>
                <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                    {!editing && (
                        <button
                            type="button"
                            onClick={() => {
                                setDraft(note.content);
                                setEditing(true);
                            }}
                            className="size-6 rounded text-slate-400 hover:text-slate-700 hover:bg-slate-100 inline-flex items-center justify-center"
                            aria-label="Edit note"
                        >
                            <PencilIcon className="w-3 h-3" />
                        </button>
                    )}
                    <button
                        type="button"
                        onClick={onDelete}
                        className="size-6 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center"
                        aria-label="Delete note"
                    >
                        <TrashIcon className="w-3 h-3" />
                    </button>
                </div>
            </div>
            {editing ? (
                <div className="mt-1.5">
                    <textarea
                        value={draft}
                        onChange={(e) => setDraft(e.target.value)}
                        rows={3}
                        className="w-full resize-none px-2 py-1.5 text-[12px] text-slate-900 border border-slate-200 rounded outline-none focus:border-slate-400"
                        autoFocus
                    />
                    <div className="flex items-center gap-1 mt-1.5">
                        <button
                            type="button"
                            onClick={save}
                            disabled={update.isPending}
                            className="h-6 px-2 rounded text-[11px] font-medium bg-slate-900 text-white hover:bg-slate-800 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                        >
                            {update.isPending ? (
                                <Loader2Icon className="w-3 h-3 animate-spin" />
                            ) : (
                                <CheckIcon className="w-3 h-3" />
                            )}
                            Save
                        </button>
                        <button
                            type="button"
                            onClick={() => setEditing(false)}
                            className="h-6 px-2 rounded text-[11px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                        >
                            <XIcon className="w-3 h-3" />
                            Cancel
                        </button>
                    </div>
                </div>
            ) : (
                <div className="mt-1 text-[12px] text-slate-700 whitespace-pre-wrap break-words">
                    {note.content}
                </div>
            )}
        </div>
    );
}
