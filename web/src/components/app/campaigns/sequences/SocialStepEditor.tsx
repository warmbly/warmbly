// The editor for a social step: the channel, what to do on it, and that action's copy, versions, reaction, comments,
// voice note or skills, plus what happens to a contact the channel cannot reach. Limits come from GET /social/channels.
import React from "react";
import { AlertTriangleIcon, Loader2Icon, MicIcon, PenLineIcon, PlusIcon, ShieldCheckIcon, Trash2Icon, UploadIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import ChannelMark from "@/components/app/social/ChannelMark";
import { SocialSetupCard } from "@/components/app/social/SocialSetup";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import { useCampaignSocialAccounts, usePutCampaignSocialAccounts, useSocialAccounts, useSocialChannels, useUploadSocialAudio } from "@/lib/api/hooks/app/social/useSocial";
import type { SequenceAction, SocialStepConfig, SocialVariant } from "@/lib/api/models/app/campaigns/sequences/Action";
import { SOCIAL_BASE_VARIANT, SOCIAL_MAX_COMMENTS, SOCIAL_MAX_VARIANTS } from "@/lib/api/models/app/campaigns/sequences/Action";
import type { ChannelSpec, SocialChannel } from "@/lib/api/models/app/social/Social";
import { ACTION_META, ACTION_ORDER, CHANNELS, actionLabel, channelLabel, channelList, channelsWith, orderedCatalog } from "@/lib/social/channels";
import { socialErrorMessage } from "@/lib/social/errors";
import { VARIABLES } from "@/lib/templateVars";
import { cn } from "@/lib/utils";
import { VariableMenu } from "./RichTextEditor";
import { hasVariants, renderedLength, socialIssues, socialLimits, switchSocialConfig, type SocialIssue } from "./socialStep";

type SetAction = React.Dispatch<React.SetStateAction<SequenceAction>>;

const REACTION_LABEL: Record<string, string> = { like: "Like", celebrate: "Celebrate", support: "Support", love: "Love", insightful: "Insightful", funny: "Funny" };

export default function SocialStepEditor({ campaignId, action, setAction }: { campaignId: string; action: SequenceAction; setAction: SetAction }) {
    const catalog = useSocialChannels();
    const specs = orderedCatalog(catalog.data);
    const cfg = action.social;
    const spec = specs.find((s) => s.channel === cfg?.channel);
    const issues = socialIssues(cfg, spec);
    const issueFor = (field: string) => issues.find((i) => i.field === field)?.message;
    const meta = cfg ? ACTION_META[cfg.action as keyof typeof ACTION_META] : undefined;

    const update = (patch: Partial<SocialStepConfig>) => setAction((a) => ({ ...a, social: { ...(a.social as SocialStepConfig), ...patch } }));
    const pick = (channel: SocialChannel, act: string) =>
        setAction((a) => ({ ...a, social: switchSocialConfig(a.social, channel, act, specs.find((s) => s.channel === channel)) }));

    if (catalog.isPending) {
        return (
            <div className="space-y-2">
                <div className="h-8 rounded-md skeleton-shimmer" />
                <div className="h-24 rounded-md skeleton-shimmer" />
            </div>
        );
    }
    if (catalog.isError || !cfg) {
        return (
            <p className="rounded-md border border-rose-200 bg-rose-50/70 px-2.5 py-2 text-[11.5px] text-rose-800">
                {catalog.isError ? socialErrorMessage(catalog.error) : "This step has no social settings yet. Pick a channel below."}
            </p>
        );
    }

    return (
        <div className="space-y-5">
            <ChannelPicker specs={specs} value={cfg.channel} onChange={(c) => pick(c, specs.find((s) => s.channel === c)?.actions.includes(cfg.action) ? cfg.action : (specs.find((s) => s.channel === c)?.actions[0] ?? "message"))} />
            <ActionGrid specs={specs} channel={cfg.channel} value={cfg.action} onChange={(a) => pick(cfg.channel, a)} />

            <Senders campaignId={campaignId} channel={cfg.channel} />

            {spec && <ActionFields campaignId={campaignId} cfg={cfg} spec={spec} update={update} issueFor={issueFor} />}

            {spec && hasVariants(cfg.action) && <Variants cfg={cfg} spec={spec} update={update} issueFor={issueFor} />}

            {spec?.consent_required && (
                <p className="flex items-start gap-2 rounded-md border border-sky-200 bg-sky-50/60 px-2.5 py-2 text-[11px] leading-relaxed text-sky-900">
                    <ShieldCheckIcon className="mt-px size-3.5 shrink-0" />
                    {spec.label} only reaches contacts with a consent basis recorded for it, on the contact&apos;s Social tab or in an import. A contact who replies STOP is opted out.
                </p>
            )}

            <div>
                <Label>A contact {spec?.consent_required ? "with no handle or consent" : "with no handle"} on {channelLabel(cfg.channel, specs)}</Label>
                <SelectMenu
                    value={cfg.if_missing ?? "skip"}
                    onChange={(v) => update({ if_missing: v as "skip" | "stop" })}
                    options={[
                        { value: "skip", label: "Skips this step and carries on" },
                        { value: "stop", label: "Leaves the campaign" },
                    ]}
                    fullWidth
                    aria-label="When the contact cannot be reached"
                />
            </div>

            <p className="rounded-md border border-slate-200 bg-slate-50 px-2.5 py-2 text-[11px] leading-relaxed text-slate-600">
                {meta?.description ?? ""}
                {cfg.channel === "linkedin" && meta?.linkedin ? ` ${meta.linkedin}` : ""} It runs from one of the campaign&apos;s {channelLabel(cfg.channel, specs)} accounts, within that account&apos;s
                daily limits and working hours; once it has talked to a contact, the same account keeps them.
            </p>

            {issues.length > 0 && (
                <ul className="space-y-1 rounded-md border border-amber-200 bg-amber-50/60 px-2.5 py-2" role="status">
                    {dedupe(issues).map((i) => (
                        <li key={i.field + i.message} className="flex items-start gap-1.5 text-[11px] leading-relaxed text-amber-900">
                            <AlertTriangleIcon className="mt-px size-3 shrink-0 text-amber-600" />
                            {i.message}
                        </li>
                    ))}
                </ul>
            )}
        </div>
    );
}

const dedupe = (issues: SocialIssue[]) => issues.filter((i, n) => issues.findIndex((j) => j.message === i.message) === n);

function ChannelPicker({ specs, value, onChange }: { specs: ChannelSpec[]; value: SocialChannel; onChange: (c: SocialChannel) => void }) {
    const list = specs.length ? specs.map((s) => s.channel) : CHANNELS;
    return (
        <div>
            <Label>Channel</Label>
            <div role="radiogroup" aria-label="Channel" className="grid grid-cols-5 gap-1 rounded-md border border-slate-200 bg-slate-50 p-0.5">
                {list.map((c) => {
                    const active = c === value;
                    return (
                        <button
                            key={c}
                            type="button"
                            role="radio"
                            aria-checked={active}
                            title={channelLabel(c, specs)}
                            onClick={() => onChange(c)}
                            className={cn(
                                "flex h-11 min-w-0 flex-col items-center justify-center gap-0.5 rounded px-1 text-[10.5px] transition-colors",
                                active ? "bg-white font-medium text-slate-900 shadow-sm ring-1 ring-slate-200" : "text-slate-500 hover:text-slate-800",
                            )}
                        >
                            <ChannelMark channel={c} className="size-4" />
                            <span className="max-w-full truncate">{channelLabel(c, specs)}</span>
                        </button>
                    );
                })}
            </div>
        </div>
    );
}

function ActionGrid({ specs, channel, value, onChange }: { specs: ChannelSpec[]; channel: SocialChannel; value: string; onChange: (a: string) => void }) {
    const spec = specs.find((s) => s.channel === channel);
    const known = ACTION_ORDER.filter((a) => specs.some((s) => s.actions.includes(a)));
    const extra = (spec?.actions ?? []).filter((a) => !(ACTION_ORDER as string[]).includes(a));
    return (
        <div>
            <Label>Action</Label>
            <div role="radiogroup" aria-label="Action" className="grid grid-cols-2 gap-1.5">
                {[...known, ...extra].map((a) => {
                    const offered = !!spec?.actions.includes(a);
                    const active = a === value;
                    const elsewhere = channelsWith(specs, a);
                    const hint = offered ? (ACTION_META[a as keyof typeof ACTION_META]?.hint ?? "") : elsewhere.length ? `Only on ${channelList(elsewhere, "and")}` : "Not offered";
                    return (
                        <button
                            key={a}
                            type="button"
                            role="radio"
                            aria-checked={active}
                            disabled={!offered}
                            title={offered ? undefined : `${channelLabel(channel, specs)} does not offer this. ${hint}.`}
                            onClick={() => onChange(a)}
                            className={cn(
                                "flex min-w-0 flex-col items-start rounded-md border px-2 py-1.5 text-left transition-colors",
                                active ? "border-sky-300 bg-sky-50" : "border-slate-200 bg-white hover:border-slate-300",
                                !offered && "cursor-not-allowed border-dashed bg-slate-50/60 opacity-60 hover:border-slate-200",
                            )}
                        >
                            <span className={cn("w-full truncate text-[12px]", active ? "font-medium text-sky-800" : "text-slate-800")}>{actionLabel(a)}</span>
                            <span className="w-full truncate text-[10.5px] text-slate-500">{hint}</span>
                        </button>
                    );
                })}
            </div>
        </div>
    );
}

// The campaign has to send from an account on this channel; say so, and offer one in two clicks.
function Senders({ campaignId, channel }: { campaignId: string; channel: SocialChannel }) {
    const accounts = useSocialAccounts();
    const senders = useCampaignSocialAccounts(campaignId);
    const put = usePutCampaignSocialAccounts(campaignId);
    const canEdit = usePermission("MANAGE_CAMPAIGNS");
    if (accounts.isPending || senders.isPending) return null;
    const onChannel = (accounts.data?.data ?? []).filter((a) => a.channel === channel && a.status !== "removed");
    const ids = senders.data ?? [];
    if (onChannel.some((a) => ids.includes(a.id))) return null;
    const name = channelLabel(channel);
    if (onChannel.length === 0) {
        return <SocialSetupCard channel={channel} description={`This step needs a ${name} account in the campaign to run from.`} />;
    }
    const first = onChannel[0];
    return (
        <SocialSetupCard
            channel={channel}
            title={`No ${name} account in this campaign yet`}
            description={`Add one of your ${name} accounts so this step has someone to run from.`}
            action={
                canEdit ? (
                    <button
                        type="button"
                        disabled={put.isPending}
                        onClick={() =>
                            put.mutate([...ids, first.id], {
                                onSuccess: () => toast.success(`${first.display_name || name} now runs this campaign's ${name} steps.`),
                                onError: (e) => toast.error(socialErrorMessage(e)),
                            })
                        }
                        className="inline-flex h-7 items-center gap-1.5 rounded-md bg-sky-600 px-2.5 text-[12px] font-medium text-white hover:bg-sky-700 disabled:opacity-60"
                    >
                        {put.isPending ? <Loader2Icon className="size-3 animate-spin" /> : <PlusIcon className="size-3" />}
                        Add {onChannel.length === 1 ? first.display_name || "it" : first.display_name || "one"}
                    </button>
                ) : undefined
            }
        />
    );
}

// A textarea with the email editor's variable menu, a live count against the channel's limit and an error line.
function CopyArea({
    label,
    value,
    onChange,
    max,
    rows,
    placeholder,
    error,
    hint,
    optional,
    rawCount,
}: {
    label: string;
    value: string;
    onChange: (v: string) => void;
    max?: number;
    /** Count the text as written instead of rendered, for limits the server checks before personalising. */
    rawCount?: boolean;
    rows: number;
    placeholder?: string;
    error?: string;
    hint?: string;
    optional?: boolean;
}) {
    const ref = React.useRef<HTMLTextAreaElement>(null);
    const id = React.useId();
    const length = rawCount ? value.length : renderedLength(value);
    const insert = (token: string) => {
        const el = ref.current;
        const at = el ? el.selectionStart : value.length;
        const end = el ? el.selectionEnd : value.length;
        onChange(value.slice(0, at) + token + value.slice(end));
        requestAnimationFrame(() => {
            el?.focus();
            el?.setSelectionRange(at + token.length, at + token.length);
        });
    };
    return (
        <div>
            <div className="flex items-baseline justify-between gap-2">
                <Label htmlFor={id}>
                    {label}
                    {optional && <span className="font-normal text-slate-400"> (optional)</span>}
                </Label>
                {max ? (
                    <span
                        className={cn("text-[11px] tabular-nums", length > max ? "font-medium text-rose-600" : length > max * 0.9 ? "text-amber-600" : "text-slate-400")}
                        title="Counted once the variables are filled in for a sample contact"
                    >
                        {length}/{max}
                    </span>
                ) : null}
            </div>
            <div className={cn("rounded-md border bg-white focus-within:ring-2", error ? "border-rose-300 focus-within:ring-rose-100" : "border-slate-200 focus-within:border-sky-400 focus-within:ring-sky-100")}>
                <textarea
                    id={id}
                    ref={ref}
                    value={value}
                    onChange={(e) => onChange(e.target.value)}
                    placeholder={placeholder}
                    rows={rows}
                    aria-invalid={!!error}
                    className="block w-full resize-y rounded-t-md bg-transparent px-2.5 py-2 text-[12.5px] leading-relaxed text-slate-800 placeholder:text-slate-400 focus:outline-none"
                />
                <div className="flex items-center border-t border-slate-100 px-1.5 py-1">
                    <VariableMenu onPick={insert} variables={VARIABLES} />
                </div>
            </div>
            {error ? <p className="mt-1 text-[11px] text-rose-600">{error}</p> : hint ? <p className="mt-1 text-[11px] text-slate-400">{hint}</p> : null}
        </div>
    );
}

const FALLBACK_HINT = "Sent instead to a contact missing one of the variables above. Leave it empty to skip them.";

function ActionFields({
    campaignId,
    cfg,
    spec,
    update,
    issueFor,
}: {
    campaignId: string;
    cfg: SocialStepConfig;
    spec: ChannelSpec;
    update: (p: Partial<SocialStepConfig>) => void;
    issueFor: (f: string) => string | undefined;
}) {
    const [showFallback, setShowFallback] = React.useState(!!(cfg.fallback_note || cfg.fallback_body || cfg.fallback_subject));
    const lim = socialLimits(spec, cfg.action);
    const fallbackToggle = (
        <button type="button" onClick={() => setShowFallback((v) => !v)} className="text-[11.5px] font-medium text-sky-700 hover:underline">
            {showFallback ? "Hide the fallback" : "Add a fallback for missing variables"}
        </button>
    );
    switch (cfg.action) {
        case "invite":
            return (
                <div className="space-y-3">
                    <CopyArea label="Note" optional value={cfg.note ?? ""} onChange={(v) => update({ note: v })} max={spec.invite_note_max_chars} rows={4} placeholder="Hi {{.FirstName}}, …" error={issueFor("note")} hint="Leave it empty to send the request with no note." />
                    {showFallback && <CopyArea label="Fallback note" value={cfg.fallback_note ?? ""} onChange={(v) => update({ fallback_note: v })} max={spec.invite_note_max_chars} rows={3} error={issueFor("fallback_note")} hint={FALLBACK_HINT} />}
                    {fallbackToggle}
                </div>
            );
        case "message":
        case "inmail":
            return (
                <div className="space-y-3">
                    {cfg.action === "inmail" && (
                        <SubjectField label="Subject" max={lim.subject} value={cfg.subject ?? ""} onChange={(v) => update({ subject: v })} error={issueFor("subject")} />
                    )}
                    <CopyArea label="Message" value={cfg.body ?? ""} onChange={(v) => update({ body: v })} max={lim.body} rows={6} placeholder="Hi {{.FirstName}}, …" error={issueFor("body")} />
                    {showFallback && (
                        <>
                            {cfg.action === "inmail" && <SubjectField label="Fallback subject" max={lim.subject} value={cfg.fallback_subject ?? ""} onChange={(v) => update({ fallback_subject: v })} error={issueFor("fallback_subject")} />}
                            <CopyArea label="Fallback message" value={cfg.fallback_body ?? ""} onChange={(v) => update({ fallback_body: v })} max={lim.body} rows={4} error={issueFor("fallback_body")} hint={FALLBACK_HINT} />
                        </>
                    )}
                    {fallbackToggle}
                </div>
            );
        case "like_post":
            return (
                <div className="space-y-3">
                    {spec.reactions.length > 0 && (
                        <div>
                            <Label>Reaction</Label>
                            <div role="radiogroup" aria-label="Reaction" className="flex flex-wrap gap-1.5">
                                {spec.reactions.map((r) => (
                                    <button
                                        key={r}
                                        type="button"
                                        role="radio"
                                        aria-checked={(cfg.reaction ?? spec.reactions[0]) === r}
                                        onClick={() => update({ reaction: r })}
                                        className={cn(
                                            "h-7 rounded-md border px-2.5 text-[12px] transition-colors",
                                            (cfg.reaction ?? spec.reactions[0]) === r ? "border-sky-300 bg-sky-50 font-medium text-sky-800" : "border-slate-200 bg-white text-slate-700 hover:border-slate-300",
                                        )}
                                    >
                                        {REACTION_LABEL[r] ?? r.replace(/_/g, " ")}
                                    </button>
                                ))}
                            </div>
                            {issueFor("reaction") && <p className="mt-1 text-[11px] text-rose-600">{issueFor("reaction")}</p>}
                        </div>
                    )}
                    <PostAge max={lim.postAge} value={cfg.post_age_days ?? 0} onChange={(v) => update({ post_age_days: v })} />
                </div>
            );
        case "comment_post":
            return (
                <div className="space-y-3">
                    <Comments cfg={cfg} spec={spec} update={update} issueFor={issueFor} />
                    <PostAge max={lim.postAge} value={cfg.post_age_days ?? 0} onChange={(v) => update({ post_age_days: v })} />
                </div>
            );
        case "voice_note":
            return <Voice campaignId={campaignId} cfg={cfg} spec={spec} scriptMax={lim.script} update={update} issueFor={issueFor} />;
        case "endorse":
            return (
                <div>
                    <Label>Skills to endorse</Label>
                    <div className="flex items-center gap-2">
                        <NumberInput value={cfg.max_skills ?? 1} onChange={(v) => update({ max_skills: Math.max(1, Math.min(3, Math.round(v) || 1)) })} min={1} max={3} className="w-20" align="center" />
                        <span className="text-[11.5px] text-slate-500">of their top skills, 1 to 3</span>
                    </div>
                    {issueFor("max_skills") && <p className="mt-1 text-[11px] text-rose-600">{issueFor("max_skills")}</p>}
                </div>
            );
        default:
            return null;
    }
}

function SubjectField({ label, max, value, onChange, error }: { label: string; max: number; value: string; onChange: (v: string) => void; error?: string }) {
    return (
        <div>
            <div className="flex items-baseline justify-between">
                <Label>{label}</Label>
                <span className={cn("text-[11px] tabular-nums", value.length > max ? "font-medium text-rose-600" : "text-slate-400")}>
                    {value.length}/{max}
                </span>
            </div>
            <TextInput value={value} onChange={onChange} placeholder="Quick question, {{.FirstName}}" className="w-full" />
            {error && <p className="mt-1 text-[11px] text-rose-600">{error}</p>}
        </div>
    );
}

function PostAge({ max, value, onChange }: { max: number; value: number; onChange: (v: number) => void }) {
    return (
        <div>
            <Label>Latest post no older than</Label>
            <div className="flex items-center gap-2">
                <NumberInput value={value} onChange={(v) => onChange(Math.max(0, Math.min(max, Math.round(v) || 0)))} min={0} max={max} className="w-20" align="center" />
                <span className="text-[11.5px] text-slate-500">days{value === 0 ? " (any age)" : ""}</span>
            </div>
            <p className="mt-1 text-[11px] text-slate-400">A contact with no post that recent skips this step.</p>
        </div>
    );
}

function Comments({ cfg, spec, update, issueFor }: { cfg: SocialStepConfig; spec: ChannelSpec; update: (p: Partial<SocialStepConfig>) => void; issueFor: (f: string) => string | undefined }) {
    const comments = cfg.comments?.length ? cfg.comments : [""];
    const set = (i: number, v: string) => update({ comments: comments.map((c, n) => (n === i ? v : c)) });
    return (
        <div className="space-y-2">
            <div className="flex items-baseline justify-between">
                <Label>Comments</Label>
                <span className="text-[11px] text-slate-400">One is picked at random for each contact</span>
            </div>
            {comments.map((c, i) => (
                <div key={i} className="flex items-start gap-1.5">
                    <div className="min-w-0 flex-1">
                        <CopyArea label={`Comment ${i + 1}`} value={c} onChange={(v) => set(i, v)} max={spec.comment_max_chars} rows={2} placeholder="Great point about…" error={issueFor(`comments.${i}`)} />
                    </div>
                    {comments.length > 1 && (
                        <button
                            type="button"
                            onClick={() => update({ comments: comments.filter((_, n) => n !== i) })}
                            aria-label={`Remove comment ${i + 1}`}
                            className="mt-6 size-7 shrink-0 rounded-md text-slate-400 hover:bg-rose-50 hover:text-rose-600 inline-flex items-center justify-center"
                        >
                            <Trash2Icon className="size-3.5" />
                        </button>
                    )}
                </div>
            ))}
            {comments.length < SOCIAL_MAX_COMMENTS && (
                <button type="button" onClick={() => update({ comments: [...comments, ""] })} className="inline-flex items-center gap-1 text-[11.5px] font-medium text-sky-700 hover:underline">
                    <PlusIcon className="size-3" /> Add a comment
                </button>
            )}
            {issueFor("comments") && <p className="text-[11px] text-rose-600">{issueFor("comments")}</p>}
        </div>
    );
}

// Measured here because the upload has to name it; 0 when the browser cannot read the file.
function audioDuration(file: File): Promise<number> {
    return new Promise((resolve) => {
        const url = URL.createObjectURL(file);
        const audio = new Audio();
        audio.preload = "metadata";
        audio.onloadedmetadata = () => {
            URL.revokeObjectURL(url);
            resolve(Number.isFinite(audio.duration) ? Math.ceil(audio.duration) : 0);
        };
        audio.onerror = () => {
            URL.revokeObjectURL(url);
            resolve(0);
        };
        audio.src = url;
    });
}

const AUDIO_MAX_BYTES = 16 * 1024 * 1024;

function Voice({
    campaignId,
    cfg,
    spec,
    scriptMax,
    update,
    issueFor,
}: {
    campaignId: string;
    cfg: SocialStepConfig;
    spec: ChannelSpec;
    scriptMax: number;
    update: (p: Partial<SocialStepConfig>) => void;
    issueFor: (f: string) => string | undefined;
}) {
    const upload = useUploadSocialAudio();
    const accounts = useSocialAccounts();
    const senders = useCampaignSocialAccounts(campaignId);
    // A written note is read in the sending account's own voice, which only GraphKit can set up.
    const chosen = (accounts.data?.data ?? []).filter((a) => a.channel === cfg.channel && (senders.data ?? []).includes(a.id));
    const writtenReason = chosen.length === 0
        ? `Add a ${spec.label} account to the campaign first.`
        : !chosen.every((a) => a.voice?.available)
          ? chosen.some((a) => a.voice?.available)
              ? `Not every ${spec.label} account on this campaign has a voice set up in GraphKit. Use a recording, or keep only accounts with a voice.`
              : `None of this campaign's ${spec.label} accounts has a voice set up in GraphKit. Use a recording.`
          : null;
    const confirm = useConfirm();
    const fileRef = React.useRef<HTMLInputElement>(null);
    const voice = cfg.voice ?? { mode: "recording" as const };
    const mode = voice.mode;
    const setVoice = (p: Partial<NonNullable<SocialStepConfig["voice"]>>) => update({ voice: { ...voice, ...p } });

    const onFile = async (file: File | undefined) => {
        if (!file) return;
        if (!file.type.startsWith("audio/")) return void toast.error("Pick an audio file.");
        if (file.size > AUDIO_MAX_BYTES) return void toast.error("A recording is at most 16 MB.");
        const seconds = await audioDuration(file);
        if (seconds <= 0) return void toast.error("This file's length could not be read. Try another format, such as MP3 or M4A.");
        if (seconds > spec.voice_note_max_seconds) return void toast.error(`A ${spec.label} voice note is at most ${spec.voice_note_max_seconds} seconds; this one is ${seconds}.`);
        upload.mutate(
            { file, durationSeconds: seconds, filename: file.name },
            { onSuccess: (a) => setVoice({ mode: "recording", audio_id: a.id, duration_seconds: a.duration_seconds || seconds }) },
        );
    };

    return (
        <div className="space-y-3">
            <div>
                <Label>Voice note</Label>
                <div role="radiogroup" aria-label="Voice note source" className="inline-flex rounded-md border border-slate-200 bg-slate-50 p-0.5">
                    {(
                        [
                            ["recording", "A recording", MicIcon],
                            ["written", "A written script", PenLineIcon],
                        ] as const
                    ).map(([m, label, Icon]) => {
                        const off = m === "written" && !!writtenReason && mode !== "written";
                        return (
                            <button
                                key={m}
                                type="button"
                                role="radio"
                                aria-checked={mode === m}
                                disabled={off}
                                title={off ? (writtenReason ?? undefined) : undefined}
                                onClick={() => setVoice({ mode: m })}
                                className={cn(
                                    "h-6 rounded px-2 text-[11.5px] inline-flex items-center gap-1.5 transition-colors disabled:cursor-not-allowed disabled:opacity-50",
                                    mode === m ? "bg-white font-medium text-slate-900 shadow-sm ring-1 ring-slate-200" : "text-slate-500 hover:text-slate-800 disabled:hover:text-slate-500",
                                )}
                            >
                                <Icon className="size-3" /> {label}
                            </button>
                        );
                    })}
                </div>
                {writtenReason && <p className="mt-1 text-[11px] text-slate-500">{mode === "written" ? <span className="text-amber-700">{writtenReason}</span> : `A written script is not available: ${writtenReason.charAt(0).toLowerCase()}${writtenReason.slice(1)}`}</p>}
            </div>
            {mode === "recording" ? (
                <div>
                    {voice.audio_id ? (
                        <div className="flex items-center gap-2.5 rounded-md border border-slate-200 px-3 py-2">
                            <MicIcon className="size-4 text-sky-600" />
                            <div className="min-w-0 flex-1">
                                <p className="text-[12.5px] text-slate-900">Recording uploaded</p>
                                <p className="text-[11px] text-slate-500">{voice.duration_seconds ?? 0} seconds · everyone hears the same note</p>
                            </div>
                            <button
                                type="button"
                                onClick={() => confirm.show("Remove this recording from the step?", async () => setVoice({ audio_id: undefined, duration_seconds: undefined }))}
                                aria-label="Remove the recording"
                                className="size-7 rounded-md text-slate-400 hover:bg-rose-50 hover:text-rose-600 inline-flex items-center justify-center"
                            >
                                <XIcon className="size-3.5" />
                            </button>
                        </div>
                    ) : (
                        <button
                            type="button"
                            disabled={upload.isPending}
                            onClick={() => fileRef.current?.click()}
                            className="flex w-full flex-col items-center gap-1 rounded-md border border-dashed border-slate-300 px-3 py-4 text-center hover:border-slate-400 disabled:opacity-60"
                        >
                            {upload.isPending ? <Loader2Icon className="size-4 animate-spin text-slate-400" /> : <UploadIcon className="size-4 text-slate-400" />}
                            <span className="text-[12px] font-medium text-slate-700">{upload.isPending ? "Uploading…" : "Upload a recording"}</span>
                            <span className="text-[11px] text-slate-500">Audio up to {spec.voice_note_max_seconds} seconds and 16 MB</span>
                        </button>
                    )}
                    <input
                        ref={fileRef}
                        type="file"
                        accept="audio/*"
                        className="sr-only"
                        tabIndex={-1}
                        onChange={(e) => {
                            void onFile(e.target.files?.[0]);
                            e.target.value = "";
                        }}
                    />
                </div>
            ) : (
                <div className="space-y-3">
                    <CopyArea label="Script" value={voice.script ?? ""} onChange={(v) => setVoice({ script: v })} max={scriptMax} rawCount rows={4} placeholder="Hi {{.FirstName}}, it's …" error={issueFor("voice.script")} hint="Read in the account's own voice for each contact. Needs a voice set up for the account in GraphKit." />
                    <CopyArea label="Fallback script" optional value={voice.fallback_script ?? ""} onChange={(v) => setVoice({ fallback_script: v })} max={scriptMax} rawCount rows={3} error={issueFor("voice.fallback_script")} hint={FALLBACK_HINT} />
                </div>
            )}
            {issueFor("voice") && <p className="text-[11px] text-rose-600">{issueFor("voice")}</p>}
        </div>
    );
}

function Variants({ cfg, spec, update, issueFor }: { cfg: SocialStepConfig; spec: ChannelSpec; update: (p: Partial<SocialStepConfig>) => void; issueFor: (f: string) => string | undefined }) {
    const variants = cfg.variants ?? [];
    const used = variants.reduce((n, v) => n + (v.weight || 0), 0);
    const baseShare = Math.max(0, 100 - used);
    const nextId = () => "bcdefghij".split("").find((c) => !variants.some((v) => v.id === c)) ?? String(variants.length + 2);
    const set = (i: number, p: Partial<SocialVariant>) => update({ variants: variants.map((v, n) => (n === i ? { ...v, ...p } : v)) });
    const add = () => {
        const share = Math.floor(100 / (variants.length + 2));
        update({ variants: [...variants.map((v) => ({ ...v, weight: share })), { id: nextId(), weight: share }] });
    };
    const remove = (i: number) => {
        const gone = variants[i];
        update({ variants: variants.filter((_, n) => n !== i), winner: cfg.winner === gone.id ? "" : cfg.winner });
    };
    const field = cfg.action === "invite" ? "note" : "body";
    return (
        <div className="space-y-2">
            <div className="flex items-baseline justify-between">
                <Label>Versions</Label>
                <span className="text-[11px] text-slate-400">Each contact gets one, by share; parts left empty use the main copy</span>
            </div>
            {variants.length > 0 && (
                <div className="flex items-center gap-2 rounded-md bg-slate-50 px-2.5 py-1.5 text-[11.5px] text-slate-600">
                    <span className="font-medium text-slate-800">Version {SOCIAL_BASE_VARIANT.toUpperCase()}</span>
                    <span>the main copy above</span>
                    <span className="ml-auto tabular-nums">{baseShare}%</span>
                </div>
            )}
            {variants.map((v, i) => (
                <div key={v.id + i} className="space-y-2 rounded-md border border-slate-200 p-2.5">
                    <div className="flex items-center gap-2">
                        <span className="text-[12px] font-medium text-slate-800">Version {v.id.toUpperCase()}</span>
                        <span className="ml-auto text-[11px] text-slate-500">Share</span>
                        <NumberInput value={v.weight} onChange={(w) => set(i, { weight: Math.max(0, Math.min(100, Math.round(w) || 0)) })} min={0} max={100} suffix="%" className="w-20" align="center" />
                        <button type="button" onClick={() => remove(i)} aria-label={`Remove version ${v.id.toUpperCase()}`} className="size-7 rounded-md text-slate-400 hover:bg-rose-50 hover:text-rose-600 inline-flex items-center justify-center">
                            <Trash2Icon className="size-3.5" />
                        </button>
                    </div>
                    {cfg.action === "inmail" && <SubjectField label="Subject" max={socialLimits(spec, cfg.action).subject} value={v.subject ?? ""} onChange={(s) => set(i, { subject: s })} error={issueFor(`variants.${i}.subject`)} />}
                    <CopyArea
                        label={cfg.action === "invite" ? "Note" : "Message"}
                        value={(field === "note" ? v.note : v.body) ?? ""}
                        onChange={(t) => set(i, field === "note" ? { note: t } : { body: t })}
                        max={cfg.action === "invite" ? spec.invite_note_max_chars : socialLimits(spec, cfg.action).body}
                        rows={3}
                        error={issueFor(`variants.${i}.${field}`) ?? issueFor(`variants.${i}.weight`) ?? issueFor(`variants.${i}`)}
                    />
                </div>
            ))}
            <div className="flex flex-wrap items-center gap-3">
                {variants.length < SOCIAL_MAX_VARIANTS && (
                    <button type="button" onClick={add} className="inline-flex items-center gap-1 text-[11.5px] font-medium text-sky-700 hover:underline">
                        <PlusIcon className="size-3" /> Add a version to test
                    </button>
                )}
                {variants.length > 0 && (
                    <div className="ml-auto flex items-center gap-2">
                        <span className="text-[11.5px] text-slate-500">Winner</span>
                        <SelectMenu
                            value={cfg.winner ?? ""}
                            onChange={(w) => update({ winner: w })}
                            options={[
                                { value: "", label: "Split by share" },
                                { value: SOCIAL_BASE_VARIANT, label: `Version ${SOCIAL_BASE_VARIANT.toUpperCase()} for everyone` },
                                ...variants.map((v) => ({ value: v.id, label: `Version ${v.id.toUpperCase()} for everyone` })),
                            ]}
                            minWidth={200}
                            align="end"
                            aria-label="Winner"
                        />
                    </div>
                )}
            </div>
            {(issueFor("variants") || issueFor("winner")) && <p className="text-[11px] text-rose-600">{issueFor("variants") ?? issueFor("winner")}</p>}
        </div>
    );
}
