// Shared email composer used by BOTH the step's original email and each A/B
// variant, so every arm has the identical toolset: a template picker, save as
// template, Write with AI, a subject with variable insert, a body with an
// Edit/Preview toggle that renders through the real send engine (for a chosen
// contact and mailbox, with signature, opt-out footer and attachments when the
// campaign is known), a "send test" action, template validity warnings, and
// the advisory content score.

import React from "react";
import {
    AlertCircleIcon,
    BookmarkPlusIcon,
    EyeIcon,
    Loader2Icon,
    PaperclipIcon,
    PencilLineIcon,
    SparklesIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import RichTextEditor, { VariableMenu } from "./RichTextEditor";
import EmailBody from "@/components/app/unibox/EmailBody";
import { useTemplatePreview } from "@/lib/api/hooks/app/campaigns/useTemplatePreview";
import type { TemplatePreview } from "@/lib/api/client/app/campaigns/previewTemplate";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import formatBytes from "@/lib/helper/formatBytes";
import { PreviewContactPicker, PreviewMailboxPicker, SendTestButton } from "./PreviewControls";
import { SAMPLE_CONTACT_LABEL, contactLabel, useCampaignSenderInboxes } from "./previewContext";
import { Label, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import ContentScore from "../ContentScore";
import HtmlFindings from "./HtmlFindings";
import useTemplates from "@/lib/api/hooks/app/templates/useTemplates";
import useCreateTemplate from "@/lib/api/hooks/app/templates/useCreateTemplate";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { VARIABLES, htmlToPlain, linkifyUnsubscribe, promptToHtml, renderPreview, templateIssue } from "./emailPreview";
import { LINK_VARIABLES, UNSUBSCRIBE_TOKEN } from "@/lib/templateVars";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import { isDocumentBody } from "@/lib/email/pastedEmail";

export default function EmailContentEditor({
    subject,
    onSubjectChange,
    bodyHtml,
    onBodyChange,
    bodyCode = false,
    onBodyCodeChange,
    subjectPlaceholder = "Quick question, {{.FirstName}}",
    subjectLocked,
    bodyPlaceholder = "Hi {{.FirstName}}, …",
    campaignId,
    stepId,
    canSendTest = false,
    dirty = false,
}: {
    subject: string;
    onSubjectChange: (value: string) => void;
    bodyHtml: string;
    onBodyChange: (html: string, plain: string) => void;
    // HTML mode for this arm, persisted on the step as body_code. Without the
    // setter the mode is local to the session and inferred from the body: an
    // A/B variant has no column to record it in.
    bodyCode?: boolean;
    onBodyCodeChange?: (code: boolean) => void;
    subjectPlaceholder?: string;
    // A step that replies in the contact's thread has no subject of its own:
    // a reply carries the conversation's. Pass the conversation's subject to
    // show it read-only in place of the field.
    subjectLocked?: { subject: string; note: string };
    bodyPlaceholder?: string;
    // When set, the preview renders for a chosen lead and mailbox and shows the
    // campaign's opt-out footer, signature and attachments.
    campaignId?: string;
    // The saved step this copy is sent by, so the preview lists the files that
    // step attaches.
    stepId?: string;
    // Offer "Send test" for that step. Off for a variant arm: the test send
    // mails the step's saved copy, which is not what this arm holds.
    canSendTest?: boolean;
    // Unsaved edits: the test send mails the saved step, so it waits for a save.
    dirty?: boolean;
}) {
    const [tab, setTab] = React.useState<"edit" | "preview">("edit");

    // An A/B arm has nowhere to persist the mode, so it infers one from the
    // body it opens with: markup no schema can hold faithfully must not be
    // parsed just because the arm was reopened. This component is keyed on the
    // step or the variant, so the seed is re-read whenever either changes.
    const [localCode, setLocalCode] = React.useState(() => isDocumentBody(bodyHtml));
    const code = onBodyCodeChange ? bodyCode : localCode;
    const setCode = onBodyCodeChange ?? setLocalCode;

    // A body can also become a document after mount: applying a template
    // replaces it wholesale. Whichever mode the step is in, it has to switch
    // before the editor parses that markup through its schema, or the next
    // visual edit saves the gutted version. The visual editor cannot produce
    // document markup itself, so this only ever fires on a body from outside.
    React.useEffect(() => {
        if (!code && isDocumentBody(bodyHtml)) setCode(true);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [bodyHtml, code]);

    // Preview context: null contact = the built-in sample; the mailbox defaults
    // to the campaign's first enabled sender once the pool has loaded.
    const [previewContact, setPreviewContact] = React.useState<Contact | null>(null);
    const [previewMailbox, setPreviewMailbox] = React.useState<Inbox | null>(null);
    const [serverPreview, setServerPreview] = React.useState<TemplatePreview | null>(null);
    const senders = useCampaignSenderInboxes(campaignId ?? "");
    // Both picks belong to one campaign, so drop them when this editor is
    // pointed at a different one, or at none, without remounting. The rendered
    // panel goes with them: it names a sender and files of the old campaign.
    React.useEffect(() => {
        setPreviewContact(null);
        setPreviewMailbox(null);
        setServerPreview(null);
    }, [campaignId]);
    React.useEffect(() => {
        if (!campaignId) return;
        if (previewMailbox && senders.inboxes.some((i) => i.id === previewMailbox.id)) return;
        setPreviewMailbox(senders.inboxes[0] ?? null);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [campaignId, senders.inboxes.map((i) => i.id).join(",")]);

    // A threading step's subject belongs to the conversation, not to the step,
    // so that is the one the preview, the content score and the template check
    // must read. The step's own stored subject is not what gets sent.
    const shownSubject = subjectLocked ? subjectLocked.subject : subject;

    const previewMut = useTemplatePreview();
    const runPreview = previewMut.mutateAsync;
    React.useEffect(() => {
        // HTML mode keeps the preview running on the Edit tab: the client
        // notes below the editor come from it, and an author writing markup
        // is exactly who needs them while they write.
        if (tab !== "preview" && !code) return;
        // Switching to Preview is a deliberate act and should feel immediate.
        // Writing markup is not: the request carries the whole body, which for
        // a designed email is tens of kilobytes, so it waits for a real pause.
        const settle = tab === "preview" ? 250 : 800;
        // Responses can land out of order; only the newest request may paint.
        let active = true;
        const t = setTimeout(() => {
            runPreview({
                subject: shownSubject,
                body_html: bodyHtml,
                // Matches what the step stores, so the preview shows the text
                // part the recipient gets rather than a second derivation.
                body_plain: code ? "" : htmlToPlain(bodyHtml),
                ...(campaignId && previewContact ? { contact_id: previewContact.id } : {}),
                ...(campaignId ? { campaign_id: campaignId } : {}),
                ...(campaignId && previewMailbox ? { account_id: previewMailbox.id } : {}),
                ...(campaignId && stepId ? { step_id: stepId } : {}),
            })
                .then((p) => {
                    if (active) setServerPreview(p);
                })
                .catch(() => {
                    if (active) setServerPreview(null);
                });
        }, settle);
        return () => {
            active = false;
            clearTimeout(t);
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [tab, code, shownSubject, bodyHtml, previewContact?.id, campaignId, previewMailbox?.id, stepId]);

    const tplIssue = templateIssue(shownSubject) || templateIssue(bodyHtml) || templateIssue(htmlToPlain(bodyHtml));

    // A plain-text campaign ships no HTML, so the send path cannot give the
    // unsubscribe variable an anchor: the recipient reads the whole signed
    // address. Preflight says the same at launch; say it here, while it is
    // still one keystroke to fix.
    const { data: previewCampaign } = useCampaign(campaignId ?? "");
    const plainTextUnsubLink =
        !!previewCampaign?.text_only && (bodyHtml.includes(UNSUBSCRIBE_TOKEN) || shownSubject.includes(UNSUBSCRIBE_TOKEN));

    // Toolbar: template library, save as template, write with AI.
    const { data: templates } = useTemplates("");
    const createTemplate = useCreateTemplate();
    const confirm = useConfirm();
    const [saveTplOpen, setSaveTplOpen] = React.useState(false);
    const [tplName, setTplName] = React.useState("");

    function applyTemplate(t: { name: string; subject: string; body_html: string; body_plain: string }) {
        const apply = () => {
            // promptToHtml escapes as it paragraph-wraps: a template body with
            // "&" or "<" in it must not reach the editor as raw markup.
            const html = t.body_html || promptToHtml(t.body_plain ?? "");
            // A threading step sends the conversation's subject, so a template
            // must not quietly rewrite one nobody will see.
            if (!subjectLocked) onSubjectChange(t.subject || subject);
            onBodyChange(html, t.body_plain || htmlToPlain(html));
            toast.success(`Applied "${t.name}"`);
        };
        if (shownSubject.trim() || htmlToPlain(bodyHtml).trim()) {
            confirm.show(`Replace this content with the "${t.name}" template?`, apply);
        } else {
            apply();
        }
    }

    async function saveAsTemplate() {
        const name = tplName.trim();
        if (!name) return;
        await toast.promise(
            createTemplate.mutateAsync({ name, subject: shownSubject, body_html: bodyHtml, body_plain: htmlToPlain(bodyHtml) }),
            { loading: "Saving template…", success: "Saved to template library.", error: (e: AppError) => buildError(e) },
        );
        setSaveTplOpen(false);
        setTplName("");
    }

    return (
        <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
                <PopoverMenu>
                    <PopoverMenuTrigger asChild>
                        <SelectButton icon={<SparklesIcon className="w-3.5 h-3.5" />} label="Templates" />
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={260} className="max-h-72 overflow-y-auto p-1">
                        {(templates ?? []).length === 0 ? (
                            <div className="px-2.5 py-3 text-center text-[11.5px] text-slate-400">
                                No templates yet. Save one below.
                            </div>
                        ) : (
                            (templates ?? []).map((t) => (
                                <button
                                    key={t.id}
                                    type="button"
                                    onClick={() => applyTemplate(t)}
                                    className="block w-full rounded px-2.5 py-1.5 text-left hover:bg-slate-100"
                                >
                                    <div className="truncate text-[12.5px] font-medium text-slate-800">{t.name}</div>
                                    <div className="truncate text-[11px] text-slate-400">{t.subject || "No subject"}</div>
                                </button>
                            ))
                        )}
                    </PopoverMenuContent>
                </PopoverMenu>

                <PopoverMenu open={saveTplOpen} onOpenChange={setSaveTplOpen}>
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            title="Save this content as a reusable template"
                            className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-900"
                        >
                            <BookmarkPlusIcon className="w-3.5 h-3.5" />
                            Save as template
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={240} className="p-2">
                        <Label>Template name</Label>
                        <div className="flex items-center gap-1.5">
                            <TextInput value={tplName} onChange={setTplName} placeholder="e.g. Cold intro v1" className="flex-1" />
                            <button
                                type="button"
                                onClick={saveAsTemplate}
                                disabled={!tplName.trim() || createTemplate.isPending}
                                className="h-7 px-2.5 rounded-md bg-sky-600 text-[12px] font-medium text-white hover:bg-sky-700 disabled:opacity-50"
                            >
                                {createTemplate.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : "Save"}
                            </button>
                        </div>
                    </PopoverMenuContent>
                </PopoverMenu>
            </div>

            <div>
                <div className="flex items-center justify-between gap-2 mb-1.5">
                    <Label className="mb-0">Subject</Label>
                    {!subjectLocked && (
                        <VariableMenu variables={VARIABLES} onPick={(v) => onSubjectChange(subject + v)} />
                    )}
                </div>
                {subjectLocked ? (
                    <>
                        <div className="h-7 px-2.5 flex items-center rounded-md border border-slate-200 bg-slate-50 text-[12.5px] text-slate-500">
                            <span className="truncate">{subjectLocked.subject || "No subject"}</span>
                        </div>
                        <p className="mt-1.5 text-[10.5px] text-slate-400">{subjectLocked.note}</p>
                    </>
                ) : (
                    <TextInput value={subject} onChange={onSubjectChange} placeholder={subjectPlaceholder} />
                )}
            </div>

            <div>
                <div className="flex items-center justify-between gap-2 mb-1.5">
                    <Label className="mb-0">Body</Label>
                    <div className="inline-flex items-center gap-0.5 rounded-md bg-slate-100 p-0.5">
                        <TabBtn active={tab === "edit"} onClick={() => setTab("edit")} icon={<PencilLineIcon className="w-3 h-3" />}>
                            Edit
                        </TabBtn>
                        <TabBtn active={tab === "preview"} onClick={() => setTab("preview")} icon={<EyeIcon className="w-3 h-3" />}>
                            Preview
                        </TabBtn>
                    </div>
                </div>
                {tab === "edit" ? (
                    <>
                        <RichTextEditor
                            html={bodyHtml}
                            // In HTML mode the plain-text half is left to the
                            // send path, which renders it from the finished
                            // message (signature and footer included) with
                            // list bullets, table rows and link destinations.
                            // Deriving a weaker one here would only mask it.
                            onChange={(html) => onBodyChange(html, code ? "" : htmlToPlain(html))}
                            code={code}
                            onCodeChange={setCode}
                            variables={VARIABLES}
                            links={LINK_VARIABLES}
                            placeholder={bodyPlaceholder}
                        />
                        {/* What clients will do to this markup. Live while
                            writing HTML, where it is most needed. */}
                        <HtmlFindings findings={serverPreview?.html_findings ?? []} />
                    </>
                ) : (
                    <div className="rounded-md border border-slate-200 bg-white">
                        {campaignId && (
                            <div className="flex flex-wrap items-center gap-1.5 border-b border-slate-200/70 px-2 py-1.5">
                                <span className="px-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Preview as</span>
                                <PreviewContactPicker campaignId={campaignId} value={previewContact} onChange={setPreviewContact} />
                                <PreviewMailboxPicker inboxes={senders.inboxes} value={previewMailbox} onChange={setPreviewMailbox} />
                                {stepId && canSendTest && (
                                    <div className="ml-auto">
                                        <SendTestButton
                                            campaignId={campaignId}
                                            stepId={stepId}
                                            contact={previewContact}
                                            mailbox={previewMailbox}
                                            dirty={dirty}
                                        />
                                    </div>
                                )}
                            </div>
                        )}
                        {serverPreview?.from && (
                            <div className="border-b border-slate-200/70 px-3 py-2 text-[12.5px]">
                                <span className="text-slate-400">From: </span>
                                <span className="text-slate-800">
                                    {serverPreview.from.name ? `${serverPreview.from.name} <${serverPreview.from.email}>` : serverPreview.from.email}
                                </span>
                            </div>
                        )}
                        <div className="border-b border-slate-200/70 px-3 py-2 text-[12.5px]">
                            <span className="text-slate-400">Subject: </span>
                            <span className="text-slate-800">{(serverPreview?.subject ?? renderPreview(shownSubject)) || "—"}</span>
                        </div>
                        {/* The body renders in the same sandboxed frame the
                            inbox uses. The HTML source view lets an author put
                            anything in a body, so dropping it into the
                            dashboard DOM would let one member's markup restyle
                            the app, or run, for every teammate who opens the
                            preview. The frame carries no allow-scripts, and it
                            also makes the preview read the way a mail client
                            renders it rather than the way our editor does. */}
                        <div className="min-h-[200px] px-3 py-2.5">
                            <EmailBody
                                html={serverPreview?.body_html ?? linkifyUnsubscribe(renderPreview(bodyHtml))}
                                plain={serverPreview?.body_plain}
                            />
                        </div>
                        {(serverPreview?.attachments?.length ?? 0) > 0 && (
                            <ul className="flex flex-wrap gap-1.5 border-t border-slate-200/70 px-3 py-2">
                                {serverPreview!.attachments!.map((a) => (
                                    <li
                                        key={a.id}
                                        title={a.mime_type}
                                        className="inline-flex items-center gap-1 rounded-md border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] text-slate-700"
                                    >
                                        <PaperclipIcon className="w-3 h-3 text-slate-400" />
                                        <span className="max-w-[200px] truncate">{a.filename}</span>
                                        <span className="text-slate-400">{formatBytes(a.size)}</span>
                                    </li>
                                ))}
                            </ul>
                        )}
                        <p className="border-t border-slate-200/70 px-3 py-1.5 text-[10.5px] text-slate-400">
                            {campaignId
                                ? `Rendered with the real send engine for ${previewContact ? contactLabel(previewContact) : SAMPLE_CONTACT_LABEL}${
                                      previewMailbox ? `, with ${previewMailbox.email}'s signature` : ""
                                  } and the campaign's opt-out footer. Tracking links are not rewritten here.`
                                : `Rendered with the real send engine for ${SAMPLE_CONTACT_LABEL}. Conditionals, functions and spintax all run.`}
                        </p>
                    </div>
                )}
                {tab === "preview" && <HtmlFindings findings={serverPreview?.html_findings ?? []} />}
                {(serverPreview?.errors?.length ?? 0) > 0 ? (
                    <p className="mt-1.5 flex items-start gap-1.5 text-[11px] text-rose-600">
                        <AlertCircleIcon className="mt-px w-3.5 h-3.5 shrink-0" />
                        <span>{serverPreview!.errors!.join(" · ")} — fix before sending.</span>
                    </p>
                ) : tplIssue ? (
                    <p className="mt-1.5 flex items-center gap-1.5 text-[11px] text-amber-600">
                        <AlertCircleIcon className="w-3.5 h-3.5 shrink-0" />
                        {tplIssue} It&apos;ll fall back to plain text — fix it before sending.
                    </p>
                ) : null}
                {plainTextUnsubLink && (
                    <p className="mt-1.5 flex items-start gap-1.5 text-[11px] text-amber-600">
                        <AlertCircleIcon className="mt-px w-3.5 h-3.5 shrink-0" />
                        <span>
                            This campaign sends plain text only, so the unsubscribe link cannot render as a word and the
                            recipient reads its whole address. Leave the unsubscribe header on and invite a reply
                            instead, or turn plain text off in Preferences.
                        </span>
                    </p>
                )}
                {(serverPreview?.unresolved?.length ?? 0) > 0 && (
                    <p className="mt-1.5 flex items-start gap-1.5 text-[11px] text-amber-600">
                        <AlertCircleIcon className="mt-px w-3.5 h-3.5 shrink-0" />
                        <span>
                            These won&apos;t resolve and would send literally:{" "}
                            <span className="font-mono">{serverPreview!.unresolved!.join(", ")}</span>
                        </span>
                    </p>
                )}
            </div>

            <ContentScore
                subject={shownSubject}
                bodyHtml={bodyHtml}
                bodyPlain={htmlToPlain(bodyHtml)}
                onApplySubject={subjectLocked ? undefined : onSubjectChange}
            />
        </div>
    );
}

function TabBtn({
    active,
    onClick,
    icon,
    children,
}: {
    active: boolean;
    onClick: () => void;
    icon: React.ReactNode;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            className={`h-6 px-2 inline-flex items-center gap-1 rounded text-[11.5px] font-medium transition-colors ${
                active ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-900"
            }`}
        >
            {icon}
            {children}
        </button>
    );
}
