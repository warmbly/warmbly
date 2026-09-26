// The step preview's context controls: which contact the copy is rendered for,
// which mailbox signs it, and a "send test" action that mails the saved step
// to an address through that mailbox. Shared by the original step and its A/B
// variants so every arm previews against the same lead and sender.

import React from "react";
import { Loader2Icon, MailCheckIcon, MailIcon, SendIcon, UserRoundIcon } from "lucide-react";
import toast from "react-hot-toast";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import useSendTestEmail from "@/lib/api/hooks/app/campaigns/useSendTestEmail";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import { useUserProfile } from "@/hooks/context/user";
import { Label, SearchInput, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { usePermission, showPermissionDenied } from "@/hooks/usePermission";
import NewPlacementTestDialog from "@/components/app/placement/tests/NewPlacementTestDialog";
import { SAMPLE_CONTACT_LABEL, contactLabel } from "./previewContext";

// Picks the contact the preview renders for. Lists the campaign's own leads
// first (they are who the step goes to); typing searches every contact.
export function PreviewContactPicker({
    campaignId,
    value,
    onChange,
}: {
    campaignId: string;
    value: Contact | null;
    onChange: (c: Contact | null) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    const debounced = useDebouncedValue(query.trim(), 250);
    const searching = debounced.length > 0;

    const search = useSearchContacts({
        options: {
            query: debounced,
            custom_field_filters: [],
            campaign_ids: searching ? [] : [campaignId],
            sort_by: "updated_at",
            reverse: false,
        },
        limit: 8,
        enabled: open,
        keepPrevious: true,
    });
    const contacts = search.contacts ?? [];

    return (
        <PopoverMenu open={open} onOpenChange={setOpen}>
            <PopoverMenuTrigger asChild>
                <SelectButton
                    icon={<UserRoundIcon className="w-3.5 h-3.5" />}
                    label={value ? contactLabel(value) : SAMPLE_CONTACT_LABEL}
                    title="Contact the preview is rendered for"
                />
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={280} className="p-1">
                <div className="p-1.5">
                    <SearchInput value={query} onChange={setQuery} placeholder="Search contacts…" autoFocus />
                </div>
                <PopoverMenuItem
                    selected={value === null}
                    onSelect={() => onChange(null)}
                    icon={<UserRoundIcon className="w-3.5 h-3.5" />}
                >
                    {SAMPLE_CONTACT_LABEL}
                </PopoverMenuItem>
                <PopoverMenuSeparator />
                <PopoverMenuLabel>{searching ? "Matches" : "Leads in this campaign"}</PopoverMenuLabel>
                <div className="max-h-56 overflow-y-auto">
                    {search.isLoading && contacts.length === 0 ? (
                        <div className="px-3 py-2 text-[11.5px] text-slate-400 inline-flex items-center gap-1.5">
                            <Loader2Icon className="w-3 h-3 animate-spin" /> Loading…
                        </div>
                    ) : contacts.length === 0 ? (
                        <div className="px-3 py-2 text-[11.5px] text-slate-400">
                            {searching ? "No contact matches that." : "No leads yet. Type to search all contacts."}
                        </div>
                    ) : (
                        contacts.map((c) => (
                            <PopoverMenuItem key={c.id} selected={value?.id === c.id} onSelect={() => onChange(c)}>
                                <span className="text-slate-800">{contactLabel(c)}</span>
                                <span className="ml-1.5 text-[11px] text-slate-400">{c.email}</span>
                            </PopoverMenuItem>
                        ))
                    )}
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

export function PreviewMailboxPicker({
    inboxes,
    value,
    onChange,
}: {
    inboxes: Inbox[];
    value: Inbox | null;
    onChange: (i: Inbox) => void;
}) {
    return (
        <PopoverMenu>
            <PopoverMenuTrigger asChild>
                <SelectButton
                    icon={<MailIcon className="w-3.5 h-3.5" />}
                    label={value ? value.email : "No sending mailbox"}
                    title="Mailbox whose signature and name the preview uses"
                />
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={260} className="p-1">
                <PopoverMenuLabel>Sending mailboxes</PopoverMenuLabel>
                {inboxes.length === 0 ? (
                    <div className="px-3 py-2 text-[11.5px] text-slate-400">
                        Add a mailbox under Senders to preview its signature.
                    </div>
                ) : (
                    inboxes.map((i) => (
                        <PopoverMenuItem key={i.id} selected={value?.id === i.id} onSelect={() => onChange(i)}>
                            <span className="text-slate-800">{i.email}</span>
                            {i.name && <span className="ml-1.5 text-[11px] text-slate-400">{i.name}</span>}
                        </PopoverMenuItem>
                    ))
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

// Mails the saved step to an address. Disabled with the reason shown when it
// could not succeed (no mailbox in the pool, unsaved copy).
export function SendTestButton({
    campaignId,
    stepId,
    contact,
    mailbox,
    dirty,
}: {
    campaignId: string;
    stepId: string;
    contact: Contact | null;
    mailbox: Inbox | null;
    dirty: boolean;
}) {
    const { user } = useUserProfile();
    const [open, setOpen] = React.useState(false);
    const [recipient, setRecipient] = React.useState(user.email ?? "");
    const send = useSendTestEmail(campaignId);

    const valid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(recipient.trim());
    const blocked = !mailbox ? "Add a sending mailbox to the campaign first." : dirty ? "Save the step first so the test carries the latest copy." : null;

    async function submit() {
        if (!mailbox || blocked || !valid) return;
        await toast.promise(
            send.mutateAsync({
                account_id: mailbox.id,
                recipient: recipient.trim(),
                step_id: stepId,
                ...(contact ? { contact_id: contact.id } : {}),
            }),
            {
                loading: "Sending test…",
                success: `Test sent to ${recipient.trim()} from ${mailbox.email}.`,
                error: (e: AppError) => buildError(e),
            },
        );
        setOpen(false);
    }

    return (
        <PopoverMenu open={open} onOpenChange={setOpen}>
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    title="Send this step to yourself"
                    className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-900"
                >
                    <SendIcon className="w-3.5 h-3.5" />
                    Send test
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={300} className="p-2.5">
                <Label>Send to</Label>
                <TextInput value={recipient} onChange={setRecipient} placeholder="you@company.com" />
                <p className="mt-2 text-[11px] leading-relaxed text-slate-500">
                    Rendered for <span className="text-slate-700">{contact ? contactLabel(contact) : SAMPLE_CONTACT_LABEL}</span>
                    {mailbox && (
                        <>
                            , sent from <span className="text-slate-700">{mailbox.email}</span>
                        </>
                    )}
                    , with the campaign&apos;s attachments, the mailbox signature and the opt-out footer. Opens and clicks on
                    it are not tracked.
                </p>
                {blocked && <p className="mt-1.5 text-[11px] text-amber-600">{blocked}</p>}
                <div className="mt-2.5 flex justify-end">
                    <button
                        type="button"
                        onClick={submit}
                        disabled={!!blocked || !valid || send.isPending}
                        className="h-7 px-3 rounded-md bg-sky-600 text-[12px] font-medium text-white hover:bg-sky-700 inline-flex items-center gap-1.5 disabled:opacity-50"
                    >
                        {send.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <SendIcon className="w-3.5 h-3.5" />}
                        Send
                    </button>
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

// Opens a placement test of the saved step: the same copy sent to a panel of
// seed inboxes to see whether it reaches the inbox, a tab or spam.
export function PlacementTestButton({ campaignId, stepId, dirty }: { campaignId: string; stepId: string; dirty: boolean }) {
    const [open, setOpen] = React.useState(false);
    const canStart = usePermission("SEND_CAMPAIGNS");
    const onClick = () => {
        if (!canStart) {
            showPermissionDenied("SEND_CAMPAIGNS");
            return;
        }
        if (dirty) {
            toast.error("Save the step first so the test carries the latest copy.");
            return;
        }
        setOpen(true);
    };
    return (
        <>
            <button
                type="button"
                onClick={onClick}
                title={dirty ? "Save the step first so the test carries the latest copy." : "Send this step to seed inboxes and see where it lands"}
                className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-900"
            >
                <MailCheckIcon className="w-3.5 h-3.5" />
                <span className="hidden sm:inline">Test inbox placement</span>
                <span className="sm:hidden">Placement</span>
            </button>
            <NewPlacementTestDialog open={open} onClose={() => setOpen(false)} prefill={{ campaignId, stepId }} />
        </>
    );
}
