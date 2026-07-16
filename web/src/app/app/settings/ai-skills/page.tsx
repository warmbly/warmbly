// AI skills (org playbooks): the settings surface for the reusable instructions
// the AI assistant, research, and reply drafts follow. A list of skills opens a
// right-side drawer with a name, one-line description, enable toggle, and a
// markdown body. Requires Manage settings.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";
import {
    SparklesIcon,
    PlusIcon,
    XIcon,
    Trash2Icon,
    Loader2Icon,
    CheckIcon,
} from "lucide-react";
import {
    useSkills,
    useCreateSkill,
    useUpdateSkill,
    useDeleteSkill,
} from "@/lib/api/hooks/app/skills/useSkills";
import type { AISkill } from "@/lib/api/models/app/skills/Skill";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { usePermission } from "@/hooks/usePermission";
import { useConfirm } from "@/hooks/context/confirm";
import { TextInput } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import { Toggle } from "../_components/SectionShell";
import { SectionShell, Section } from "../_components/SectionShell";

type DraftSkill = { id?: string; name: string; description: string; content: string; enabled: boolean };

export default function SkillsSettingsPage() {
    const canManage = usePermission("MANAGE_SETTINGS");
    const skills = useSkills();
    const [editing, setEditing] = React.useState<DraftSkill | null>(null);

    const rows = skills.data?.data ?? [];

    return (
        <SectionShell
            title="AI skills"
            description="Reusable playbooks your AI features follow. Write down how your team qualifies leads, handles objections, or books meetings, and the assistant, research, and reply drafts use them."
            actions={
                canManage ? (
                    <button
                        type="button"
                        onClick={() => setEditing({ name: "", description: "", content: "", enabled: true })}
                        className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                    >
                        <PlusIcon className="w-3 h-3" />
                        New skill
                    </button>
                ) : undefined
            }
        >
            <Section eyebrow="Playbooks" description="Enabled skills are shown to the AI; the model reads a skill's full content on demand.">
                {skills.isPending ? (
                    <div className="h-16 rounded bg-slate-100 animate-pulse" />
                ) : rows.length === 0 ? (
                    <p className="text-[12px] text-slate-500 leading-relaxed">
                        No skills yet. Add one to teach your AI how your team works.
                    </p>
                ) : (
                    <div className="rounded-md border border-slate-200 overflow-hidden divide-y divide-slate-100">
                        {rows.map((s) => (
                            <SkillRow key={s.id} skill={s} onOpen={() => canManage && setEditing({ ...s })} />
                        ))}
                    </div>
                )}
            </Section>

            <SkillDrawer draft={editing} onClose={() => setEditing(null)} />
        </SectionShell>
    );
}

function SkillRow({ skill, onOpen }: { skill: AISkill; onOpen: () => void }) {
    return (
        <button
            type="button"
            onClick={onOpen}
            className="w-full text-left px-3 py-2.5 flex items-center gap-3 hover:bg-slate-50 transition-colors"
        >
            <div className="size-7 rounded-md bg-sky-50 border border-sky-100 text-sky-600 flex items-center justify-center shrink-0">
                <SparklesIcon className="w-3.5 h-3.5" />
            </div>
            <div className="min-w-0 flex-1">
                <div className="text-[12.5px] font-medium text-slate-900 truncate">{skill.name}</div>
                {skill.description && (
                    <div className="text-[11.5px] text-slate-500 truncate">{skill.description}</div>
                )}
            </div>
            {!skill.enabled && (
                <span className="text-[10px] uppercase tracking-[0.08em] text-slate-400 border border-slate-200 rounded-sm px-1 py-0.5 shrink-0">
                    Off
                </span>
            )}
        </button>
    );
}

function SkillDrawer({ draft, onClose }: { draft: DraftSkill | null; onClose: () => void }) {
    const create = useCreateSkill();
    const update = useUpdateSkill();
    const del = useDeleteSkill();
    const confirm = useConfirm();

    const [name, setName] = React.useState("");
    const [description, setDescription] = React.useState("");
    const [content, setContent] = React.useState("");
    const [enabled, setEnabled] = React.useState(true);

    React.useEffect(() => {
        if (!draft) return;
        setName(draft.name);
        setDescription(draft.description);
        setContent(draft.content);
        setEnabled(draft.enabled);
    }, [draft]);

    async function save() {
        if (!name.trim()) {
            toast.error("A name is required");
            return;
        }
        try {
            if (draft?.id) {
                await update.mutateAsync({ id: draft.id, data: { name, description, content, enabled } });
            } else {
                await create.mutateAsync({ name, description, content, enabled });
            }
            toast.success("Skill saved");
            onClose();
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    function remove() {
        if (!draft?.id) return;
        confirm.show("Delete this skill? The AI will stop using it.", async () => {
            await del.mutateAsync(draft.id!);
            toast.success("Skill deleted");
            onClose();
        });
    }

    const saving = create.isPending || update.isPending;

    return (
        <AnimatePresence>
            {draft && (
                <>
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        onClick={onClose}
                        className="fixed inset-0 z-40 bg-slate-900/30"
                    />
                    <motion.aside
                        initial={{ x: "100%" }}
                        animate={{ x: 0 }}
                        exit={{ x: "100%" }}
                        transition={{ type: "spring", stiffness: 380, damping: 40 }}
                        className="fixed right-0 top-0 z-50 h-full w-full sm:w-[520px] bg-white border-l border-slate-200 shadow-[0_0_60px_-12px_rgba(15,23,42,0.3)] flex flex-col"
                    >
                        <div className="shrink-0 px-5 h-14 flex items-center gap-3 border-b border-slate-200">
                            <div className="size-7 rounded-md bg-sky-50 border border-sky-100 text-sky-600 flex items-center justify-center">
                                <SparklesIcon className="w-4 h-4" />
                            </div>
                            <div className="text-[13px] font-semibold text-slate-900 flex-1">
                                {draft.id ? "Edit skill" : "New skill"}
                            </div>
                            <button onClick={onClose} className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors">
                                <XIcon className="w-4 h-4" />
                            </button>
                        </div>

                        <div className="flex-1 min-h-0 overflow-y-auto px-5 py-4 space-y-4">
                            <Field label="Name" hint="Short and memorable; the AI loads a skill by name.">
                                <TextInput value={name} onChange={setName} placeholder="Objection handling" className="w-full" />
                            </Field>
                            <Field label="Description" hint="One line so the AI knows when to use it.">
                                <TextInput value={description} onChange={setDescription} placeholder="How to respond to common pushback" className="w-full" />
                            </Field>
                            <Field label="Enabled" hint="Only enabled skills are shown to the AI.">
                                <Toggle on={enabled} onChange={setEnabled} />
                            </Field>
                            <Field label="Playbook" hint="Markdown. Plain instructions the AI should follow.">
                                <Textarea
                                    value={content}
                                    onChange={(e) => setContent(e.target.value)}
                                    rows={14}
                                    maxLength={32 * 1024}
                                    placeholder={"When a prospect says it's too expensive:\n- acknowledge the concern\n- ask what they are comparing to\n- ..."}
                                    className="w-full font-mono text-[12px]"
                                />
                            </Field>
                        </div>

                        <div className="shrink-0 h-14 px-5 flex items-center gap-2 border-t border-slate-200 bg-slate-50/60">
                            <button
                                type="button"
                                onClick={save}
                                disabled={saving}
                                className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                            >
                                {saving ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                                Save
                            </button>
                            {draft.id && (
                                <button
                                    type="button"
                                    onClick={remove}
                                    className="h-7 px-2.5 rounded-md text-[12px] text-red-600 hover:text-white hover:bg-red-600 font-medium inline-flex items-center gap-1.5 transition-colors ml-auto"
                                >
                                    <Trash2Icon className="w-3 h-3" />
                                    Delete
                                </button>
                            )}
                        </div>
                    </motion.aside>
                </>
            )}
        </AnimatePresence>
    );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
    return (
        <div>
            <div className="text-[12.5px] font-medium text-slate-900">{label}</div>
            {hint && <div className="text-[11px] text-slate-500 mb-1.5">{hint}</div>}
            {children}
        </div>
    );
}
