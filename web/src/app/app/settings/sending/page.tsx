// Sending settings — when campaign mail leaves, in the RECIPIENT's day rather
// than the sending mailbox's. The scheduler treats the chosen hours as a real
// constraint (it delays a send to reach them), so the summary line spells out
// what the current selection means before anyone saves it.

import React from "react";
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
import {
    DEFAULT_PREFERRED_HOURS,
    describeHours,
    formatHour,
    type OutreachSettings,
} from "@/lib/api/models/app/outreach/OutreachSettings";

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

    const autosave = useAutosave({
        value: draft,
        enabled: !!draft,
        save: async (v) => {
            if (v) await update.mutateAsync(v);
        },
    });
    useRegisterUnsaved(autosave, () => setDraft(autosave.savedValue));

    // One-shot hydration: the server value seeds the draft once, then the save
    // path owns the baseline so a refetch can't stomp an in-flight edit.
    const hydrated = React.useRef(false);
    React.useEffect(() => {
        if (!data || hydrated.current) return;
        hydrated.current = true;
        setDraft(data);
        autosave.markSaved(data);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [data]);

    const sto = draft?.send_time_optimization;

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
                                        <div className="grid grid-cols-6 gap-1">
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
                                    min={0}
                                    max={100}
                                    value={draft.preflight?.min_content_score ?? 60}
                                    onChange={(n) =>
                                        patchPreflight({
                                            min_content_score: Number.isFinite(n) ? Math.min(100, Math.max(0, n)) : 60,
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
