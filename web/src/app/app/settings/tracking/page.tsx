// Website tracking settings: the snippet a workspace installs on its own
// site, and the privacy posture it runs under. Off by default, and the
// consent mode, location precision and retention window are all the
// workspace's decision, enforced on the server rather than in the snippet.

import React from "react";
import { useAppStore } from "@/stores";
import { CheckIcon, CopyIcon } from "lucide-react";
import { Row, Section, SectionShell, Toggle } from "../_components/SectionShell";
import { NoAccess } from "@/components/layout/NoAccess";
import { usePermission } from "@/hooks/usePermission";
import { useConfirm } from "@/hooks/context/confirm";
import SaveStatus from "../_components/SaveStatus";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { NumberInput } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import { useAutosave } from "@/hooks/useAutosave";
import { useRegisterUnsaved } from "@/hooks/context/unsaved";
import {
    useRotateWebsiteTrackingKey,
    useUpdateWebsiteTrackingSettings,
    useWebsiteTrackingSettings,
} from "@/lib/api/hooks/app/websitetracking/useWebsiteTracking";
import {
    WEBSITE_RETENTION_MAX_DAYS,
    WEBSITE_RETENTION_MIN_DAYS,
    trackingSnippet,
    type UpdateWebsiteTrackingSettings,
    type WebsiteTrackingSettings,
} from "@/lib/api/models/app/websitetracking/WebsiteTrackingSettings";

const CONSENT_OPTIONS: SelectOption[] = [
    { value: "explicit", label: "Ask first (recommended)" },
    { value: "implicit", label: "Record on load" },
];

const LOCATION_OPTIONS: SelectOption[] = [
    { value: "none", label: "Do not keep" },
    { value: "country", label: "Country only" },
    { value: "city", label: "Country, region and city" },
];

type Draft = Pick<
    WebsiteTrackingSettings,
    "enabled" | "consent_mode" | "location_precision" | "retention_days"
> & { hosts: string };

function toDraft(s: WebsiteTrackingSettings): Draft {
    return {
        enabled: s.enabled,
        consent_mode: s.consent_mode,
        location_precision: s.location_precision,
        retention_days: s.retention_days,
        hosts: s.allowed_hosts.join("\n"),
    };
}

function toPatch(d: Draft): UpdateWebsiteTrackingSettings {
    return {
        enabled: d.enabled,
        consent_mode: d.consent_mode,
        location_precision: d.location_precision,
        retention_days: d.retention_days,
        allowed_hosts: d.hosts
            .split(/[\n,]/)
            .map((h) => h.trim())
            .filter(Boolean),
    };
}

export default function WebsiteTrackingSettingsPage() {
    const canManage = usePermission("MANAGE_SETTINGS");
    if (!canManage) return <NoAccess feature="Website tracking" permissionLabel="Manage settings" />;
    return <WebsiteTrackingSettingsView />;
}

function WebsiteTrackingSettingsView() {
    const { data, isLoading } = useWebsiteTrackingSettings();
    const update = useUpdateWebsiteTrackingSettings();
    const rotate = useRotateWebsiteTrackingKey();
    const confirm = useConfirm();
    const [draft, setDraft] = React.useState<Draft | null>(null);

    // One workspace's tracking settings, so the draft belongs to the workspace
    // it was hydrated from: switching re-hydrates it, and a save that would land
    // on a different workspace is dropped rather than written to it.
    const orgID = useAppStore((st) => st.currentOrganization?.id);
    const hydratedFor = React.useRef<string | undefined>(undefined);

    const autosave = useAutosave({
        value: draft,
        enabled: !!draft,
        debounceMs: 600,
        save: async (v) => {
            if (!v) return;
            if (hydratedFor.current !== useAppStore.getState().currentOrganization?.id) return;
            await update.mutateAsync(toPatch(v));
        },
    });
    useRegisterUnsaved(autosave, () => setDraft(autosave.savedValue));

    // Hydration is once per workspace, as on the other autosave settings pages:
    // the server seeds the draft and the save path owns the baseline after that.
    React.useEffect(() => {
        if (!data || hydratedFor.current === orgID) return;
        hydratedFor.current = orgID;
        const d = toDraft(data);
        setDraft(d);
        autosave.markSaved(d);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [data, orgID]);

    const patch = React.useCallback((next: Partial<Draft>) => {
        setDraft((prev) => (prev ? { ...prev, ...next } : prev));
    }, []);

    const hostCount = draft ? toPatch(draft).allowed_hosts?.length ?? 0 : 0;

    return (
        <SectionShell
            title="Website tracking"
            description="See which pages a contact visits on your own site, in their activity timeline."
            actions={<SaveStatus status={autosave.status} onRetry={autosave.retry} />}
        >
            <Section
                eyebrow="Collection"
                description="Nothing is recorded until this is on. Page views reach a contact only when they arrive from a link in an email you sent them; nobody can be attached to a visit by guessing."
            >
                {isLoading || !draft ? (
                    <div className="h-7 w-40 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <>
                        <Row
                            label="Record website visits"
                            description={
                                draft.enabled
                                    ? "On. The snippet below reports page views for this workspace."
                                    : "Off. Installed snippets send nothing that is kept."
                            }
                        >
                            <Toggle on={draft.enabled} onChange={(on) => patch({ enabled: on })} />
                        </Row>

                        <Row
                            label="Consent"
                            description={
                                draft.consent_mode === "explicit"
                                    ? "The snippet stores and sends nothing until your page calls warmbly('consent', 'granted'), typically from your cookie banner."
                                    : "Views are recorded as soon as the page loads. Choose this only where you have a lawful basis without a prior opt-in."
                            }
                        >
                            <SelectMenu
                                value={draft.consent_mode}
                                onChange={(v) => patch({ consent_mode: v as Draft["consent_mode"] })}
                                options={CONSENT_OPTIONS}
                                aria-label="Consent mode"
                                minWidth={220}
                                align="end"
                            />
                        </Row>

                        <Row
                            label="Location from IP address"
                            description="Worked out on the server from the visitor's address, which is never stored itself."
                        >
                            <SelectMenu
                                value={draft.location_precision}
                                onChange={(v) => patch({ location_precision: v as Draft["location_precision"] })}
                                options={LOCATION_OPTIONS}
                                aria-label="Location precision"
                                minWidth={220}
                                align="end"
                            />
                        </Row>

                        <Row
                            label="Keep visits for"
                            description={`Days. Older page views are deleted automatically (${WEBSITE_RETENTION_MIN_DAYS} to ${WEBSITE_RETENTION_MAX_DAYS}).`}
                        >
                            <NumberInput
                                min={WEBSITE_RETENTION_MIN_DAYS}
                                max={WEBSITE_RETENTION_MAX_DAYS}
                                value={draft.retention_days}
                                onChange={(n) =>
                                    patch({
                                        retention_days: Number.isFinite(n)
                                            ? Math.min(WEBSITE_RETENTION_MAX_DAYS, Math.max(WEBSITE_RETENTION_MIN_DAYS, n))
                                            : 90,
                                    })
                                }
                                className="w-24"
                            />
                        </Row>

                        <Row
                            label="Your website hosts"
                            description="One per line, for example example.com. Views from other hosts are ignored, and links in your emails only identify a visitor when they point at one of these."
                            align="start"
                        >
                            <div className="w-full sm:w-[320px]">
                                <Textarea
                                    value={draft.hosts}
                                    onChange={(e) => patch({ hosts: e.target.value })}
                                    placeholder={"example.com\napp.example.com"}
                                    rows={3}
                                    className="text-[12.5px] font-mono"
                                />
                                {draft.enabled && hostCount === 0 && (
                                    <p className="mt-1.5 text-[11.5px] text-amber-700">
                                        Add at least one host, or visits are recorded but never tied to a contact.
                                    </p>
                                )}
                            </div>
                        </Row>
                    </>
                )}
            </Section>

            <Section
                eyebrow="Install"
                description="Paste this before the closing </head> tag on every page. The first line lets you call warmbly() before the script has loaded."
            >
                {isLoading || !data ? (
                    <div className="h-16 rounded bg-slate-100 animate-pulse" />
                ) : (
                    <>
                        <Snippet code={trackingSnippet(data)} />
                        {!data.tracking_host && (
                            <p className="text-[11.5px] text-amber-700">
                                This install has no tracking host configured (TRACKING_DOMAIN), so the snippet cannot
                                load. Ask your operator to set one.
                            </p>
                        )}
                        <Row
                            label="Site key"
                            description="Public: it only says which workspace a view belongs to. Rotate it if a copy of the snippet ends up somewhere it should not be; the old key stops working at once."
                        >
                            <button
                                type="button"
                                onClick={() =>
                                    confirm.show(
                                        "Rotate the site key? Every installed snippet must be updated to the new one before it reports again.",
                                        async () => {
                                            await rotate.mutateAsync();
                                        },
                                    )
                                }
                                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors"
                            >
                                Rotate key
                            </button>
                        </Row>
                    </>
                )}
            </Section>

            <Section
                eyebrow="What is collected"
                description="Page URL and title, referrer, UTM parameters, language, timezone and screen size come from the browser. Device, operating system and browser are read on the server from the request. Visitors who send Global Privacy Control or Do Not Track are never recorded, and a contact's visits are deleted with the contact."
            >
                <p className="text-[11.5px] text-slate-500">
                    Call <code className="font-mono text-slate-700">warmbly(&apos;consent&apos;, &apos;denied&apos;)</code> to
                    clear the visitor id on this browser, or{" "}
                    <code className="font-mono text-slate-700">warmbly(&apos;reset&apos;)</code> when a shared device changes
                    hands.
                </p>
            </Section>
        </SectionShell>
    );
}

function Snippet({ code }: { code: string }) {
    const [copied, setCopied] = React.useState(false);
    return (
        <div className="relative rounded-md border border-slate-200 bg-slate-50">
            <pre className="overflow-x-auto px-3 py-2.5 pr-20 text-[11.5px] leading-relaxed font-mono text-slate-700 whitespace-pre">
                {code}
            </pre>
            <button
                type="button"
                onClick={async () => {
                    try {
                        await navigator.clipboard.writeText(code);
                        setCopied(true);
                        setTimeout(() => setCopied(false), 1500);
                    } catch {
                        /* clipboard blocked */
                    }
                }}
                className="absolute top-1.5 right-1.5 inline-flex items-center gap-1 rounded-md border border-slate-200 bg-white px-2 h-7 text-[11.5px] text-slate-600 hover:bg-slate-50"
            >
                {copied ? <CheckIcon className="w-3.5 h-3.5 text-emerald-600" /> : <CopyIcon className="w-3.5 h-3.5" />}
                {copied ? "Copied" : "Copy"}
            </button>
        </div>
    );
}
