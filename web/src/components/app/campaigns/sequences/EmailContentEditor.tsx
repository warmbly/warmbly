// Shared email composer used by BOTH the step's original email and each A/B
// variant, so every arm has the identical toolset: a template picker, save as
// template, Write with AI, a subject with variable insert, a body with an
// Edit/Preview toggle that renders through the real send engine, template
// validity warnings, and the advisory content score.

import React from "react";
import {
    AlertCircleIcon,
    BookmarkPlusIcon,
    EyeIcon,
    Loader2Icon,
    PencilLineIcon,
    SparklesIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import RichTextEditor, { VariableMenu } from "./RichTextEditor";
import { useTemplatePreview } from "@/lib/api/hooks/app/campaigns/useTemplatePreview";
import type { TemplatePreview } from "@/lib/api/client/app/campaigns/previewTemplate";
import { Label, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import WriteWithAI from "./WriteWithAI";
import ContentScore from "../ContentScore";
import useTemplates from "@/lib/api/hooks/app/templates/useTemplates";
import useCreateTemplate from "@/lib/api/hooks/app/templates/useCreateTemplate";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { VARIABLES, SAMPLE, htmlToPlain, renderPreview, templateIssue } from "./emailPreview";

export default function EmailContentEditor({
    subject,
    onSubjectChange,
    bodyHtml,
    onBodyChange,
    subjectPlaceholder = "Quick question, {{.FirstName}}",
    bodyPlaceholder = "Hi {{.FirstName}}, …",
}: {
    subject: string;
    onSubjectChange: (value: string) => void;
    bodyHtml: string;
    onBodyChange: (html: string, plain: string) => void;
    subjectPlaceholder?: string;
    bodyPlaceholder?: string;
}) {
    const [tab, setTab] = React.useState<"edit" | "preview">("edit");

    const previewMut = useTemplatePreview();
    const runPreview = previewMut.mutateAsync;
    const [serverPreview, setServerPreview] = React.useState<TemplatePreview | null>(null);
    React.useEffect(() => {
        if (tab !== "preview") return;
        const t = setTimeout(() => {
            runPreview({ subject, body_html: bodyHtml, body_plain: htmlToPlain(bodyHtml) })
                .then(setServerPreview)
                .catch(() => setServerPreview(null));
        }, 250);
        return () => clearTimeout(t);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [tab, subject, bodyHtml]);

    const tplIssue = templateIssue(subject) || templateIssue(bodyHtml) || templateIssue(htmlToPlain(bodyHtml));

    // Toolbar: template library, save as template, write with AI.
    const { data: templates } = useTemplates("");
    const createTemplate = useCreateTemplate();
    const confirm = useConfirm();
    const [saveTplOpen, setSaveTplOpen] = React.useState(false);
    const [tplName, setTplName] = React.useState("");

    function applyTemplate(t: { name: string; subject: string; body_html: string; body_plain: string }) {
        const apply = () => {
            const html = t.body_html || (t.body_plain ? `<p>${t.body_plain.replace(/\n/g, "</p><p>")}</p>` : "");
            onSubjectChange(t.subject || subject);
            onBodyChange(html, t.body_plain || htmlToPlain(html));
            toast.success(`Applied "${t.name}"`);
        };
        if (subject.trim() || htmlToPlain(bodyHtml).trim()) {
            confirm.show(`Replace this content with the "${t.name}" template?`, apply);
        } else {
            apply();
        }
    }

    async function saveAsTemplate() {
        const name = tplName.trim();
        if (!name) return;
        await toast.promise(
            createTemplate.mutateAsync({ name, subject, body_html: bodyHtml, body_plain: htmlToPlain(bodyHtml) }),
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

                <WriteWithAI
                    onInsert={(text) => {
                        const html = `<p>${text
                            .replace(/&/g, "&amp;")
                            .replace(/</g, "&lt;")
                            .replace(/>/g, "&gt;")
                            .replace(/\n{2,}/g, "</p><p>")
                            .replace(/\n/g, "<br />")}</p>`;
                        const next = bodyHtml ? `${bodyHtml}${html}` : html;
                        onBodyChange(next, htmlToPlain(next));
                    }}
                />
            </div>

            <div>
                <div className="flex items-center justify-between gap-2 mb-1.5">
                    <Label className="mb-0">Subject</Label>
                    <VariableMenu variables={VARIABLES} onPick={(v) => onSubjectChange(subject + v)} />
                </div>
                <TextInput value={subject} onChange={onSubjectChange} placeholder={subjectPlaceholder} />
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
                    <RichTextEditor
                        html={bodyHtml}
                        onChange={(html) => onBodyChange(html, htmlToPlain(html))}
                        variables={VARIABLES}
                        placeholder={bodyPlaceholder}
                    />
                ) : (
                    <div className="rounded-md border border-slate-200 bg-white">
                        <div className="border-b border-slate-200/70 px-3 py-2 text-[12.5px]">
                            <span className="text-slate-400">Subject: </span>
                            <span className="text-slate-800">{(serverPreview?.subject ?? renderPreview(subject)) || "—"}</span>
                        </div>
                        <div
                            className="tiptap-body min-h-[200px] px-3 py-2.5 text-[13px] leading-relaxed text-slate-800"
                            dangerouslySetInnerHTML={{
                                __html: (serverPreview?.body_html ?? renderPreview(bodyHtml)) || '<p class="text-slate-300">Nothing to preview yet.</p>',
                            }}
                        />
                        <p className="border-t border-slate-200/70 px-3 py-1.5 text-[10.5px] text-slate-400">
                            Rendered with the real send engine against sample data ({SAMPLE.FirstName} at {SAMPLE.Company}) — conditionals, functions, and spintax all run.
                        </p>
                    </div>
                )}
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

            <ContentScore subject={subject} bodyHtml={bodyHtml} bodyPlain={htmlToPlain(bodyHtml)} />
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
