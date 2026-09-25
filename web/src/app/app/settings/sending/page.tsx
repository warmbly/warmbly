// Sending settings — when campaign mail leaves, in the RECIPIENT's day rather
// than the sending mailbox's. The scheduler treats the chosen hours as a real
// constraint (it delays a send to reach them), so the summary line spells out
// what the current selection means before anyone saves it.

import React from "react";
import { useAppStore } from "@/stores";
import { ClockIcon } from "lucide-react";
import { Row, Section, SectionShell, Toggle } from "../_components/SectionShell";
import { NoAccess } from "@/components/layout/NoAccess";
import { usePermission } from "@/hooks/usePermission";
import SaveStatus from "../_components/SaveStatus";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { NumberInput } from "@/components/ui/field";
import { useAutosave } from "@/hooks/useAutosave";
import { useRegisterUnsaved } from "@/hooks/context/unsaved";
import useTimezones from "@/lib/api/hooks/app/useTimezones";
import {
    useOutreachSettings,
    useUpdateOutreachSettings,
} from "@/lib/api/hooks/app/outreach/useOutreachSettings";
import VerificationSettings from "@/components/app/contacts/VerificationSettings";
import {
    AUTOMATED_INTENTS,
    DEFAULT_INBOX_TAGGING,
    DEFAULT_PREFERRED_HOURS,
    DEFAULT_UNSUBSCRIBE,
    REPLY_INTENT_CHOICES,
    describeHours,
    formatHour,
    taskIntents,
    type InboxTaggingSettings,
    type OutreachSettings,
    type ReplyIntent,
    type ReplyIntentSettings,
    type UnsubscribeMode,
    type UnsubscribeSettings,
} from "@/lib/api/models/app/outreach/OutreachSettings";
import { TextInput } from "@/components/ui/field";
import { Link } from "react-router-dom";
import TaggingQuestions from "./TaggingQuestions";
import LanguagePicker from "./LanguagePicker";

const UNSUB_MODES: SelectOption[] = [
    { value: "text", label: "Reply to opt out (text line)" },
    { value: "link", label: "Unsubscribe link" },
    { value: "off", label: "Nothing" },
];

const HOURS = Array.from({ length: 24 }, (_, i) => i);

export default function SendingSettingsPage() {
    const canManage = usePermission("MANAGE_SETTINGS");
    if (!canManage) return <NoAccess feature="Sending" permissionLabel="Manage settings" />;
    return <SendingSettings />;
}

function SendingSettings() {
    const { data, isLoading } = useOutreachSettings();
    const update = useUpdateOutreachSettings();
    const timezones = useTimezones();
    const [draft, setDraft] = React.useState<OutreachSettings | null>(null);

    // These are one workspace's settings, so the draft belongs to the workspace
    // it was hydrated from. Switching workspaces re-hydrates it, and a save that
    // would land on a different workspace than the draft came from is dropped:
    // otherwise the next edit after a switch wrote the previous workspace's
    // whole settings object onto the new one.
    const orgID = useAppStore((st) => st.currentOrganization?.id);
    const hydratedFor = React.useRef<string | undefined>(undefined);

    const autosave = useAutosave({
        value: draft,
        enabled: !!draft,
        save: async (v) => {
            if (!v) return;
            if (hydratedFor.current !== useAppStore.getState().currentOrganization?.id) return;
            await update.mutateAsync(v);
        },
    });
    useRegisterUnsaved(autosave, () => setDraft(autosave.savedValue));

    // Hydration is once per workspace: the server value seeds the draft, then
    // the save path owns the baseline so a refetch can't stomp an in-flight edit.
    React.useEffect(() => {
        if (!data || hydratedFor.current === orgID) return;
        hydratedFor.current = orgID;
        setDraft(data);
        autosave.markSaved(data);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [data, orgID]);

    const sto = draft?.send_time_optimization;

    const patchUnsubscribe = React.useCallback(
        (next: Partial<UnsubscribeSettings>) => {
            setDraft((prev) =>
                prev ? { ...prev, unsubscribe: { ...(prev.unsubscribe ?? DEFAULT_UNSUBSCRIBE), ...next } } : prev,
            );
        },
        [],
    );

    const patchReplyIntent = React.useCallback(
        (next: Partial<ReplyIntentSettings>) => {
            setDraft((prev) => (prev ? { ...prev, reply_intent: { ...prev.reply_intent, ...next } } : prev));
        },
        [],
    );

    const patchInboxTagging = React.useCallback(
        (next: Partial<InboxTaggingSettings>) => {
            setDraft((prev) =>
                prev
                    ? { ...prev, inbox_tagging: { ...(prev.inbox_tagging ?? DEFAULT_INBOX_TAGGING), ...next } }
                    : prev,
            );
        },
        [],
    );
    const tagging = draft?.inbox_tagging ?? DEFAULT_INBOX_TAGGING;

    const patchPreflight = React.useCallback(
        (next: Partial<OutreachSettings["preflight"]>) => {
            setDraft((prev) => (prev ? { ...prev, preflight: { ...prev.preflight, ...next } } : prev));
        },
        [],
    );

    const patch = React.useCallback(
        (next: Partial<NonNullable<typeof sto>>) => {
            setDraft((prev) =>
                prev
                    ? { ...prev, send_time_optimization: { ...prev.send_time_optimization, ...next } }
                    : prev,
            );
        },
        [],
    );

    const toggleHour = React.useCallback(
        (h: number) => {
            if (!sto) return;
            const set = new Set(sto.preferred_hours ?? []);
            if (set.has(h)) set.delete(h);
            else set.add(h);
            // Never persist an empty list: the backend would fall back to
            // business hours anyway, and an empty grid reads as "never send".
            const next = [...set].sort((a, b) => a - b);
            patch({ preferred_hours: next.length ? next : DEFAULT_PREFERRED_HOURS });
        },
        [sto, patch],
    );

    const tzOptions = React.useMemo<SelectOption[]>(
        () => (timezones.data ?? []).map((t) => ({ value: t.name, label: t.display_name })),
        [timezones.data],
    );

    const enabled = !!sto?.enabled;
    const hours = sto?.preferred_hours?.length ? sto.preferred_hours : DEFAULT_PREFERRED_HOURS;

    return (
        <SectionShell
            title="Sending"
            description="When campaign mail goes out, measured in your recipient's day."
            actions={<SaveStatus status={autosave.status} onRetry={autosave.retry} />}
        >
            <VerificationSettings />
            <Section
                eyebrow="Send-time optimization"
                description="Hold each campaign email until it lands inside the hours you pick, in the recipient's own timezone. It only ever delays a send, never brings one forward, and it still obeys the campaign schedule and each mailbox's working hours."
            >
                {isLoading || !sto ? (
                    <div className="h-7 w-40 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <>
                        <Row
                            label="Use the recipient's local hours"
                            description={
                                enabled
                                    ? "On. Sends aim for the hours below."
                                    : "Off. Sends follow the campaign schedule and the sending mailbox's hours only."
                            }
                        >
                            <Toggle on={enabled} onChange={(on) => patch({ enabled: on })} />
                        </Row>

                        {enabled && (
                            <>
                                <Row
                                    label="Read each contact's timezone"
                                    description="Uses the contact's timezone field, then the country its email domain points at. Falls back to the timezone below."
                                >
                                    <Toggle
                                        on={!!sto.use_contact_timezone}
                                        onChange={(on) => patch({ use_contact_timezone: on })}
                                    />
                                </Row>

                                <Row
                                    label="Fallback timezone"
                                    description="Used when a contact's timezone cannot be worked out."
                                >
                                    <SelectMenu
                                        value={sto.default_contact_timezone || "UTC"}
                                        onChange={(v) => patch({ default_contact_timezone: v })}
                                        options={tzOptions}
                                        aria-label="Fallback timezone"
                                        minWidth={240}
                                        align="end"
                                    />
                                </Row>

                                <Row
                                    label="Skip weekends"
                                    description="Push a send that would land on Saturday or Sunday to the next weekday."
                                >
                                    <Toggle
                                        on={(sto.weekend_weight_multiplier ?? 1) < 1}
                                        onChange={(on) => patch({ weekend_weight_multiplier: on ? 0.5 : 1 })}
                                    />
                                </Row>

                                <Row label="Delivery hours" align="start">
                                    <div className="w-full sm:w-[320px]">
                                        <div className="grid grid-cols-4 sm:grid-cols-6 gap-1">
                                            {HOURS.map((h) => {
                                                const on = hours.includes(h);
                                                return (
                                                    <button
                                                        key={h}
                                                        type="button"
                                                        onClick={() => toggleHour(h)}
                                                        aria-pressed={on}
                                                        className={`h-7 rounded-md border text-[11.5px] transition-colors ${
                                                            on
                                                                ? "bg-sky-50 text-sky-700 border-sky-200"
                                                                : "bg-white text-slate-500 border-slate-200 hover:border-slate-300"
                                                        }`}
                                                    >
                                                        {formatHour(h)}
                                                    </button>
                                                );
                                            })}
                                        </div>
                                        <p className="mt-2 text-[11.5px] text-slate-500 leading-relaxed inline-flex items-start gap-1.5">
                                            <ClockIcon className="w-3.5 h-3.5 mt-px shrink-0 text-slate-400" />
                                            <span>
                                                Mail arrives around {describeHours(hours)} for each recipient
                                                {sto.use_contact_timezone ? "" : ` (${sto.default_contact_timezone || "UTC"})`}.
                                            </span>
                                        </p>
                                    </div>
                                </Row>
                            </>
                        )}
                    </>
                )}
            </Section>

            <Section
                eyebrow="Unsubscribe"
                description="The opt-out every campaign email carries, added after the signature. A reply that asks to stop, a click on the link, and the mail client's own unsubscribe button all put the recipient on the suppression list, and no campaign emails them again. For cold outreach the List-Unsubscribe header (per campaign, on by default) is what satisfies the bulk-sender rules, so this line can stay a plain sentence. A campaign can override this in its preferences."
            >
                {isLoading || !draft ? (
                    <div className="h-7 w-40 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <>
                        <UnsubscribeRows value={draft.unsubscribe ?? DEFAULT_UNSUBSCRIBE} onChange={patchUnsubscribe} />
                        <Row
                            label="Honour replies that ask to stop"
                            description="A reply saying unsubscribe, remove me, stop emailing me and the like puts the sender on the suppression list at once. This is what makes the reply-to-opt-out line a real mechanism."
                        >
                            <Toggle
                                on={draft.reply_intent?.auto_suppress_on_unsubscribe_keyword !== false}
                                onChange={(on) => patchReplyIntent({ auto_suppress_on_unsubscribe_keyword: on })}
                            />
                        </Row>
                    </>
                )}
            </Section>

            <Section
                eyebrow="Out of office"
                description="When a recipient's mailbox answers with an away message, hold their next step until they are back instead of sending it to an empty desk. The hold covers every campaign that contact is in, not only the one they answered. The return date in the auto-reply is used when it can be read (English, German, French, Spanish, Portuguese, Italian and Dutch), plus a working day so the follow-up does not land in their first-morning backlog. The lead keeps its place in the sequence and the time it spent held does not count against the step's wait. You can resume or stop a held lead at any time from the campaign's Leads tab."
            >
                {isLoading || !draft ? (
                    <div className="h-7 w-40 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <>
                        <Row
                            label="Hold a contact who is away"
                            description="An auto-reply is never treated as a human reply, so without this the follow-up goes out on schedule and the sequence is over before they are back."
                        >
                            <Toggle
                                on={draft.reply_intent?.hold_on_out_of_office !== false}
                                onChange={(on) => patchReplyIntent({ hold_on_out_of_office: on })}
                            />
                        </Row>
                        {draft.reply_intent?.hold_on_out_of_office !== false && (
                            <Row
                                label="Hold for"
                                description="Used when the away message names no return date we can read. Between 1 and 90 days."
                            >
                                <div className="flex items-center gap-1.5">
                                    <NumberInput
                                        min={1}
                                        max={90}
                                        value={draft.reply_intent?.out_of_office_hold_days ?? 7}
                                        onChange={(n) =>
                                            patchReplyIntent({
                                                out_of_office_hold_days: Number.isFinite(n)
                                                    ? Math.min(90, Math.max(1, n))
                                                    : 7,
                                            })
                                        }
                                        className="w-20"
                                    />
                                    <span className="text-[11.5px] text-slate-500">days</span>
                                </div>
                            </Row>
                        )}
                    </>
                )}
            </Section>

            <Section
                eyebrow="Reply follow-ups"
                description="Open a CRM task when a reply lands, so a prospect who answers ends up on the Tasks page instead of only in the inbox. The task is assigned to the mailbox owner and due in 24 hours."
            >
                {isLoading || !draft ? (
                    <div className="h-7 w-40 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <>
                        <Row
                            label="Open a task on a reply"
                            description="One task per classified reply, titled with the intent and the sender."
                        >
                            <Toggle
                                on={draft.reply_intent?.auto_create_crm_task !== false}
                                onChange={(on) => patchReplyIntent({ auto_create_crm_task: on })}
                            />
                        </Row>
                        {draft.reply_intent?.auto_create_crm_task !== false && (
                            <Row
                                label="Which replies"
                                description="Automated replies are off by default: a vacation notice is not follow-up work, and a week of sending makes enough of them to bury the real ones."
                                align="start"
                            >
                                <IntentPicker
                                    value={taskIntents(draft.reply_intent)}
                                    onChange={(next) => patchReplyIntent({ crm_task_intents: next })}
                                />
                            </Row>
                        )}
                    </>
                )}
            </Section>

            <Section
                eyebrow="Classified replies"
                description="What a reply may do once automatic inbox tagging has read it. The hold, the stop and the task are on from the start and every one of them is visible: holds and stops show on the campaign's Leads tab, where you can lift them, and a task is an ordinary CRM task. The suppression is off until you turn it on, because it is the one the system cannot undo; watch the review page under Settings first."
            >
                {isLoading || !draft ? (
                    <div className="h-7 w-40 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <>
                        <Row
                            label="Hold a contact who says not now"
                            description="A reply read as open in principle but wrong on timing parks the contact's sequences, in every campaign they are in, and the follow-up lands after the hold instead of three days later."
                        >
                            <Toggle
                                on={tagging.hold_on_not_now}
                                onChange={(on) => patchInboxTagging({ hold_on_not_now: on })}
                            />
                        </Row>
                        {tagging.hold_on_not_now && (
                            <Row label="Hold for" description="Between 1 and 90 days.">
                                <div className="flex items-center gap-1.5">
                                    <NumberInput
                                        min={1}
                                        max={90}
                                        value={tagging.not_now_hold_days}
                                        onChange={(n) =>
                                            patchInboxTagging({
                                                not_now_hold_days: Number.isFinite(n)
                                                    ? Math.min(90, Math.max(1, n))
                                                    : 30,
                                            })
                                        }
                                        className="w-20"
                                    />
                                    <span className="text-[11.5px] text-slate-500">days</span>
                                </div>
                            </Row>
                        )}
                        <Row
                            label="Stop a contact who declines"
                            description="Not interested, or not the right person: the contact's sequences are parked for a year. Nothing is unsubscribed and nothing is deleted; a member resumes the lead if the reply was misread."
                        >
                            <Toggle
                                on={tagging.stop_on_declined}
                                onChange={(on) => patchInboxTagging({ stop_on_declined: on })}
                            />
                        </Row>
                        <Row
                            label="Open a task when they ask for a call"
                            description="A reply that asks for a call or proposes a time opens a high-priority task for the mailbox owner, due in 24 hours."
                        >
                            <Toggle
                                on={tagging.task_on_call_request}
                                onChange={(on) => patchInboxTagging({ task_on_call_request: on })}
                            />
                        </Row>
                        <Row
                            label="Suppress a contact who asks to be removed"
                            description="Adds the sender to the suppression list when the classifier is at least 80% sure the reply asks to stop receiving email. The keyword rule above catches the plain phrasings; this catches the rest. Suppression is permanent until a member removes the entry."
                        >
                            <Toggle
                                on={tagging.suppress_on_removal_request}
                                onChange={(on) => patchInboxTagging({ suppress_on_removal_request: on })}
                            />
                        </Row>
                    </>
                )}
            </Section>

            <Section
                eyebrow="Tagging languages and questions"
                description="Tune automatic inbox tagging to your own mail. Languages and questions only change how tagging reads a message; they never change a message's kind, intent or relevance on their own."
            >
                {isLoading || !draft ? (
                    <div className="h-7 w-40 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <>
                        <Row
                            label="Tagging languages"
                            description="Tagging reads English, German, French, Spanish, Italian and Dutch replies out of the box. Add the languages your mail arrives in and tagging also cuts their quoted history and recognises their away messages and bounce notices before asking, and tells the classifier what to expect. Nothing about a language is used until you add it."
                            align="start"
                        >
                            <LanguagePicker
                                value={tagging.languages ?? []}
                                onChange={(languages) => patchInboxTagging({ languages })}
                            />
                        </Row>
                        <Row
                            label="Your questions"
                            description="Ask something the built-in labels do not. Each question files a label of its own and can hold, stop or open a task, like the switches above, and rides in the same call as the built-in questions."
                        />
                        <TaggingQuestions
                            value={tagging.questions ?? []}
                            onChange={(questions) => patchInboxTagging({ questions })}
                        />
                    </>
                )}
            </Section>

            <Section
                eyebrow="Content checks"
                description="Score each step's copy for the signals spam filters weight: trigger wording, stacked punctuation, link and image counts, attachments. Checked when you launch, and again per send against the copy the recipient actually receives once merge fields and spintax have resolved."
            >
                {isLoading || !draft ? (
                    <div className="h-7 w-40 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <>
                        <Row
                            label="Flag risky copy"
                            description="Advisory only. It warns in the launch dialog and the campaign activity feed, and never blocks or delays a send."
                        >
                            <Toggle
                                on={!!draft.preflight?.check_content_score}
                                onChange={(on) => patchPreflight({ check_content_score: on })}
                            />
                        </Row>
                        {draft.preflight?.check_content_score && (
                            <Row
                                label="Minimum score"
                                description="Copy scoring below this out of 100 is flagged. Higher is stricter."
                            >
                                <NumberInput
                                    min={1}
                                    max={100}
                                    value={draft.preflight?.min_content_score ?? 60}
                                    onChange={(n) =>
                                        patchPreflight({
                                            min_content_score: Number.isFinite(n) ? Math.min(100, Math.max(1, n)) : 60,
                                        })
                                    }
                                    className="w-20"
                                />
                            </Row>
                        )}
                    </>
                )}
            </Section>
        </SectionShell>
    );
}

// The intents that open a follow-up task. Chips rather than checkboxes, to
// match the delivery-hours grid above; the two automated classes move together
// because they are one thing to the person reading the list.
function IntentPicker({
    value,
    onChange,
}: {
    value: ReplyIntent[];
    onChange: (next: ReplyIntent[]) => void;
}) {
    const has = (id: ReplyIntent) =>
        id === "out_of_office" ? AUTOMATED_INTENTS.some((i) => value.includes(i)) : value.includes(id);

    function toggle(id: ReplyIntent) {
        const group = id === "out_of_office" ? AUTOMATED_INTENTS : [id];
        const on = has(id);
        const next = on
            ? value.filter((v) => !group.includes(v))
            : [...value.filter((v) => !group.includes(v)), ...group];
        onChange(next);
    }

    return (
        <div className="w-full sm:w-[320px]">
            <div className="flex flex-wrap gap-1">
                {REPLY_INTENT_CHOICES.map((c) => {
                    const on = has(c.id);
                    return (
                        <button
                            key={c.id}
                            type="button"
                            onClick={() => toggle(c.id)}
                            aria-pressed={on}
                            title={c.hint}
                            className={`h-7 px-2.5 rounded-md border text-[11.5px] transition-colors ${
                                on
                                    ? "bg-sky-50 text-sky-700 border-sky-200"
                                    : "bg-white text-slate-500 border-slate-200 hover:border-slate-300"
                            }`}
                        >
                            {c.label}
                        </button>
                    );
                })}
            </div>
            <p className="mt-2 text-[11.5px] text-slate-500 leading-relaxed">
                {value.length === 0
                    ? "Nothing opens a task. Same as turning the switch off."
                    : `A reply classified ${REPLY_INTENT_CHOICES.filter((c) => has(c.id))
                          .map((c) => c.label.toLowerCase())
                          .join(", ")} opens a task.`}
            </p>
        </div>
    );
}

function UnsubscribeRows({
    value,
    onChange,
}: {
    value: UnsubscribeSettings;
    onChange: (next: Partial<UnsubscribeSettings>) => void;
}) {
    const mode = (value.mode || "text") as UnsubscribeMode;
    const text = value.text || DEFAULT_UNSUBSCRIBE.text;
    const intro = value.link_intro || DEFAULT_UNSUBSCRIBE.link_intro;
    const linkText = value.link_text || DEFAULT_UNSUBSCRIBE.link_text;
    return (
        <>
            <Row
                label="Opt-out line"
                description={
                    mode === "text"
                        ? "A plain sentence inviting a reply. Reads like a personal email; the reply is detected and honoured automatically."
                        : mode === "link"
                          ? "A sentence with a real unsubscribe link. One click on a confirmation page; the mail client may also show its own Unsubscribe button. Reads as bulk mail where a reply reads as a person, so keep it for lists that need a link. On a plain-text campaign it prints the full address in the copy."
                          : "No opt-out in the body. Keep the unsubscribe header on in each campaign, or you are relying on recipients replying."
                }
            >
                <SelectMenu
                    value={mode}
                    onChange={(v) => onChange({ mode: v as UnsubscribeMode })}
                    options={UNSUB_MODES}
                    aria-label="Opt-out line"
                    minWidth={240}
                    align="end"
                />
            </Row>
            {mode === "text" && (
                <Row label="Wording" description="One sentence, appended after the signature." align="start">
                    <TextInput
                        value={value.text}
                        onChange={(v) => onChange({ text: v })}
                        placeholder={DEFAULT_UNSUBSCRIBE.text}
                        className="w-full sm:w-[420px]"
                    />
                </Row>
            )}
            {mode === "link" && (
                <>
                    <Row label="Wording" description="The sentence before the link." align="start">
                        <TextInput
                            value={value.link_intro}
                            onChange={(v) => onChange({ link_intro: v })}
                            placeholder={DEFAULT_UNSUBSCRIBE.link_intro}
                            className="w-full sm:w-[420px]"
                        />
                    </Row>
                    <Row label="Link text" description="What the link itself says.">
                        <TextInput
                            value={value.link_text}
                            onChange={(v) => onChange({ link_text: v })}
                            placeholder={DEFAULT_UNSUBSCRIBE.link_text}
                            className="w-full sm:w-[240px]"
                        />
                    </Row>
                </>
            )}
            {mode !== "off" && (
                <Row label="Preview" align="start">
                    <p className="w-full sm:w-[420px] rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2 text-[12px] text-slate-500">
                        {mode === "text" ? (
                            text
                        ) : (
                            <>
                                {intro} <span className="underline text-slate-600">{linkText}</span>
                            </>
                        )}
                    </p>
                </Row>
            )}
            <Row
                label="Suppression list"
                description="Everyone who opted out, bounced or complained, plus anything added by hand. No campaign emails an entry on it."
            >
                <Link to="/app/contacts/suppressions" className="text-[12px] text-sky-700 hover:text-sky-800 font-medium">
                    Open the list
                </Link>
            </Row>
        </>
    );
}
