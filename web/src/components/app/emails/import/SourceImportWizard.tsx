// The shell the vendor and admin-grant imports share. Source (a vendor account
// or a grant, owned by the caller) -> Mailboxes (the picker) -> Settings ->
// Import, which is the file import's RunStep: both sources end in the same
// MailboxImport job, so the result screen, retries and the Imports menu are one.
import React from "react";
import toast from "react-hot-toast";
import { CheckIcon, ChevronRightIcon, Loader2Icon, UploadIcon, XIcon } from "lucide-react";
import type { MailboxImport, MailboxImportSettings, OnExisting } from "@/lib/api/models/app/emails/MailboxImport";
import type { SourceImportOptions } from "@/lib/api/models/app/emails/MailboxSources";
import useMailboxAllowance from "@/lib/api/hooks/app/emails/useMailboxAllowance";
import { useSendingDomains, useTrackingSuggestions } from "@/lib/api/hooks/app/emails/useSendingDomains";
import { noDnsControl } from "@/components/app/emails/domains/rules";
import { isAllowanceError } from "@/hooks/useMailboxOAuth";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { plural } from "./importFields";
import { Banner, SectionLabel, StatCard } from "./parts";
import PickTable, { type PickItem } from "./PickTable";
import RunStep from "./RunStep";
import { OnExistingChoice, SettingsSection } from "./SettingsSection";
import { DomainChoicesSection } from "./DomainChoices";
import { dnsFollowUps, domainChoiceIssue, domainOptions, emptyPicks, picksTouched, type DomainInfo, type DomainPicks } from "./domainChoiceRules";
import { PrimaryButton, Stepper, WizardFooter, WizardPanes, type WizardStep } from "./wizard";

type StepKey = string;

/** A step before the picker: choosing a source, or setting one up. */
export interface SetupStep {
    key: string;
    label: string;
    /** Why this step cannot be left, or null. */
    issue: string | null;
    /** The footer's primary label here; "Continue" by default. */
    cta?: string;
    /** `next` moves to the following step, `goTo` to any step before it. */
    render: (next: () => void, goTo: (key: string) => void) => React.ReactNode;
}

export interface SourceImportWizardProps {
    /** The steps before the picker; when set, the single-source props below are ignored. */
    setupSteps?: SetupStep[];
    /** The first step's label ("Account", "Admin"). */
    sourceLabel?: string;
    /** The first step; `next` moves on once a source is chosen. */
    renderSource?: (next: () => void) => React.ReactNode;
    /** Why the first step cannot be left, or null. */
    sourceIssue?: string | null;
    /** Set to the wizard's step navigation, for a caller that advances from outside (a popup finishing). */
    goRef?: React.MutableRefObject<((key: string) => void) | null>;
    /** "Connect more" after an import, before the wizard returns to its first step. */
    onStartOver?: () => void;
    /** Changing it clears the picked mailboxes. */
    sourceKey: string | null;
    /** The first step holds typed input nobody has submitted. */
    sourceDirty?: boolean;
    /** Above the picker, e.g. how Microsoft mailboxes sign in. */
    pickNote?: (selected: Set<string>) => React.ReactNode;
    items: PickItem[] | undefined;
    /** The logo and the steps shown while the mailboxes are listed. */
    itemsLoadingLogo?: string;
    itemsLoadingSteps?: string[];
    itemsLoading: boolean;
    itemsFetching?: boolean;
    itemsError?: string | null;
    onRefresh?: () => void;
    source: "vendor" | "grant";
    submit: (ids: string[], options: SourceImportOptions) => Promise<MailboxImport>;
    submitting: boolean;
    anotherLabel: string;
    onDone: () => void;
    onAllowance?: () => void;
    onDirtyChange?: (dirty: boolean) => void;
}

const NOUN = { one: "mailbox", many: "mailboxes" };
// Each suggestion probes DNS, so only this many domains get per-domain choices here.
const MAX_CHOICE_DOMAINS = 50;

export default function SourceImportWizard(props: SourceImportWizardProps) {
    const {
        setupSteps,
        sourceLabel = "Source",
        renderSource,
        sourceIssue = null,
        sourceKey,
        goRef,
        onStartOver,
        sourceDirty,
        pickNote,
        items,
        itemsLoadingLogo,
        itemsLoadingSteps,
        itemsLoading,
        itemsFetching,
        itemsError,
        onRefresh,
        source,
        submit,
        submitting,
        anotherLabel,
        onDone,
        onAllowance,
        onDirtyChange,
    } = props;

    const setup: SetupStep[] = setupSteps ?? [
        { key: "source", label: sourceLabel, issue: sourceIssue, render: (next) => renderSource?.(next) ?? null },
    ];
    const steps: WizardStep<StepKey>[] = [
        ...setup.map((s) => ({ key: s.key, label: s.label })),
        { key: "pick", label: "Mailboxes" },
        { key: "settings", label: "Settings" },
        { key: "run", label: "Import" },
    ];
    const firstKey = setup[0]?.key ?? "pick";

    const [step, setStep] = React.useState<StepKey>(firstKey);
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [nudged, setNudged] = React.useState(false);
    const [selected, setSelected] = React.useState<Set<string>>(() => new Set());
    const [onExisting, setOnExisting] = React.useState<OnExisting>("update");
    const [settings, setSettings] = React.useState<MailboxImportSettings>({});
    const [domainPicks, setDomainPicks] = React.useState<DomainPicks>(emptyPicks);
    const [dnsLeft, setDnsLeft] = React.useState(0);
    const [importId, setImportId] = React.useState<string | null>(null);
    const allowance = useMailboxAllowance(true);

    // Another account's mailboxes are another list.
    React.useEffect(() => {
        setSelected(new Set());
    }, [sourceKey]);

    const dirty =
        !importId && (selected.size > 0 || Object.keys(settings).length > 0 || picksTouched(domainPicks) || !!sourceDirty);
    React.useEffect(() => {
        onDirtyChange?.(dirty);
    }, [dirty, onDirtyChange]);

    // Only what is still listed counts; a refetch can drop a picked row.
    const picked = React.useMemo(() => (items ?? []).filter((i) => selected.has(i.id)), [items, selected]);
    const pickedNew = picked.filter((i) => !i.connected).length;
    const pickedUpgrade = picked.filter((i) => i.connected && i.upgrade).length;
    const pickedExisting = picked.length - pickedNew;
    const importCount = onExisting === "skip" ? pickedNew : picked.length;

    // The picked mailboxes' domains, each with its tracking suggestion and any redirect it has.
    const pickedDomains = React.useMemo(
        () => [...new Set(picked.map((i) => (i.domain ?? "").toLowerCase()).filter((d) => d && !noDnsControl(d)))].sort(),
        [picked],
    );
    const choiceDomains = React.useMemo(
        () => (step === "settings" ? pickedDomains.slice(0, MAX_CHOICE_DOMAINS) : []),
        [step, pickedDomains],
    );
    const suggestions = useTrackingSuggestions(choiceDomains);
    const sendingDomains = useSendingDomains(step === "settings");
    const domainInfos: DomainInfo[] = choiceDomains.map((d, i) => ({
        domain: d,
        tracking: suggestions[i]?.data ?? null,
        trackingLoading: suggestions[i]?.isLoading ?? false,
        redirect: sendingDomains.data?.data.find((x) => x.domain === d)?.redirect ?? null,
        vendor_domain: suggestions[i]?.data?.vendor_domain ?? sendingDomains.data?.data.find((x) => x.domain === d)?.vendor_domain ?? null,
    }));
    const a = allowance.data;
    const remaining = a && a.allowance != null ? (a.remaining ?? 0) : null;
    const allowanceFull = remaining !== null && remaining <= 0 && pickedNew > 0;
    const overBy = remaining !== null && pickedNew > remaining ? pickedNew - remaining : 0;

    const order = steps.map((s) => s.key);
    // A setup step that went away (a mode switch) lands on the first one.
    const at = Math.max(0, order.indexOf(step));
    const current = order[at];
    const setupStep = setup.find((s) => s.key === current);

    function stepIssue(key: StepKey): string | null {
        const own = setup.find((s) => s.key === key);
        if (own) return own.issue;
        if (key === "pick") {
            if (itemsLoading) return "Still loading the mailboxes.";
            if (picked.length === 0) return "Pick at least one mailbox.";
            return null;
        }
        if (key === "settings") {
            if (importCount === 0) return "Every picked mailbox is already connected. Choose Update, or pick others.";
            if (allowanceFull && onExisting === "skip") return "Your mailbox allowance is full. Request more first.";
            return domainChoiceIssue(domainInfos, domainPicks);
        }
        return null;
    }
    const issue = stepIssue(current);

    const reachable = (target: StepKey) => {
        if (importId) return target === "run";
        if (target === "run") return false;
        const t = order.indexOf(target);
        for (let i = 0; i < t; i++) if (stepIssue(order[i])) return false;
        return true;
    };

    function go(target: StepKey) {
        setDirection(order.indexOf(target) >= at ? 1 : -1);
        setNudged(false);
        setStep(target);
    }

    React.useEffect(() => {
        if (!goRef) return;
        goRef.current = go;
        return () => {
            goRef.current = null;
        };
    });

    function next() {
        if (issue) {
            setNudged(true);
            return;
        }
        if (current !== "settings" && current !== "run") go(order[at + 1]);
    }

    function back() {
        if (at > 0) go(order[at - 1]);
    }

    async function run() {
        if (issue) {
            setNudged(true);
            return;
        }
        if (submitting) return;
        try {
            const job = await submit(
                picked.map((i) => i.id),
                { on_existing: onExisting, settings, ...domainOptions(domainInfos, domainPicks) },
            );
            setDnsLeft(dnsFollowUps(domainInfos, domainPicks));
            setImportId(job.id);
            go("run");
        } catch (e) {
            if (isAllowanceError(e) && onAllowance) {
                onAllowance();
                return;
            }
            toast.error(buildError(e as AppError));
        }
    }

    function startOver() {
        setSelected(new Set());
        setOnExisting("update");
        setSettings({});
        setDomainPicks(emptyPicks());
        setDnsLeft(0);
        setImportId(null);
        onStartOver?.();
        go(firstKey);
    }

    const pickable = (items ?? []).filter((i) => !i.disabledReason);
    // What "select all" means: every mailbox not here yet, and every one here that moves onto this source.
    const toConnect = pickable.filter((i) => !i.connected || i.upgrade);
    const selectionBar =
        current === "pick" && picked.length > 0 ? (
            <div className="flex items-center gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5 whitespace-nowrap">
                <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                    <CheckIcon className="w-3 h-3" />
                    <span>{picked.length.toLocaleString()} selected</span>
                </div>
                <button
                    type="button"
                    onClick={() => setSelected(new Set(toConnect.map((i) => i.id)))}
                    className="h-7 px-2.5 rounded text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 font-medium transition-colors"
                >
                    <span className="hidden sm:inline">{toConnect.some((i) => i.upgrade) ? "Only new and moving" : "Only not connected"}</span>
                    <span className="sm:hidden">{toConnect.some((i) => i.upgrade) ? "New and moving" : "Not connected"}</span>
                </button>
                <button
                    type="button"
                    onClick={() => setSelected(new Set())}
                    aria-label="Clear selection"
                    className="h-7 px-2 rounded text-[12px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1 transition-colors"
                >
                    <XIcon className="w-3 h-3" />
                    <span className="hidden sm:inline">Clear</span>
                </button>
            </div>
        ) : null;

    return (
        <div className="flex flex-col">
            <Stepper
                steps={steps}
                at={at}
                canClick={(key, i) => !importId && key !== "run" && (i < at || reachable(key))}
                goTo={go}
            />

            <WizardPanes stepKey={current} direction={direction}>
                {setupStep?.render(() => go(order[at + 1]), (key) => go(key))}
                {current === "pick" && (
                    <div className="p-4 space-y-3 pb-16">
                        {pickNote?.(selected)}
                        {!itemsLoading && !itemsError && toConnect.length > 0 && picked.length === 0 && (
                            <button
                                type="button"
                                onClick={() => setSelected(new Set(toConnect.map((i) => i.id)))}
                                className="text-[11.5px] text-sky-700 hover:text-sky-900 underline decoration-sky-300"
                            >
                                {toConnect.some((i) => i.upgrade)
                                    ? `Select every mailbox to connect or move (${toConnect.length.toLocaleString()})`
                                    : `Select every mailbox not connected yet (${toConnect.length.toLocaleString()})`}
                            </button>
                        )}
                        <PickTable
                            items={items}
                            loading={itemsLoading}
                            loadingLogo={itemsLoadingLogo}
                            loadingSteps={itemsLoadingSteps}
                            fetching={itemsFetching}
                            error={itemsError}
                            onRefresh={onRefresh}
                            selected={selected}
                            setSelected={setSelected}
                            noun={NOUN}
                        />
                    </div>
                )}
                {current === "settings" && (
                    <div className="p-4 space-y-4">
                        <div className={cn("grid grid-cols-2 gap-2", pickedUpgrade > 0 ? "sm:grid-cols-4" : "sm:grid-cols-3")}>
                            <StatCard label="Picked" value={picked.length} accent="sky" />
                            <StatCard label="New" value={pickedNew} accent="emerald" />
                            {pickedUpgrade > 0 && <StatCard label="Moving here" value={pickedUpgrade} accent="amber" />}
                            <StatCard label="Already here" value={pickedExisting - pickedUpgrade} accent="slate" />
                        </div>

                        {pickedUpgrade > 0 && (
                            <Banner tone="sky" title={`${plural(pickedUpgrade, "mailbox moves", "mailboxes move")} onto this grant`}>
                                {pickedUpgrade === 1 ? "It signs in on its own today. It" : "They sign in on their own today. They"} keep their history,
                                campaigns and warmup, and the stored sign-in is dropped.
                                {onExisting === "skip" && " Skip leaves them as they are; choose Update to move them."}
                            </Banner>
                        )}

                        {allowanceFull ? (
                            <Banner tone="red" title="Your mailbox allowance is full">
                                New mailboxes cannot be connected until the allowance is raised.{" "}
                                {onAllowance && (
                                    <button type="button" onClick={onAllowance} className="underline font-medium">
                                        Request more
                                    </button>
                                )}
                            </Banner>
                        ) : overBy > 0 && remaining !== null ? (
                            <Banner tone="amber" title={`Room for ${remaining.toLocaleString()} more mailboxes, ${pickedNew.toLocaleString()} new picked`}>
                                The first {remaining.toLocaleString()} connect and the other {overBy.toLocaleString()} are reported as failed, so
                                you can request more and retry only those.
                            </Banner>
                        ) : null}

                        {pickedExisting > 0 ? (
                            <OnExistingChoice source={source} value={onExisting} onChange={setOnExisting} />
                        ) : (
                            <div>
                                <SectionLabel className="mb-1">What happens</SectionLabel>
                                <p className="text-[11.5px] text-slate-600 leading-relaxed">
                                    {plural(pickedNew, "mailbox is", "mailboxes are")} connected in the background, a few seconds each. You can
                                    close the window while it runs.
                                </p>
                            </div>
                        )}

                        <DomainChoicesSection
                            infos={domainInfos}
                            picks={domainPicks}
                            setPicks={setDomainPicks}
                            capped={Math.max(0, pickedDomains.length - MAX_CHOICE_DOMAINS)}
                        />

                        <SettingsSection settings={settings} setSettings={setSettings} />
                    </div>
                )}
                {current === "run" && importId && (
                    <RunStep
                        importId={importId}
                        onDone={onDone}
                        onAllowance={onAllowance}
                        onImportAnother={startOver}
                        anotherLabel={anotherLabel}
                        dnsFollowUps={dnsLeft}
                    />
                )}
            </WizardPanes>

            {current !== "run" && (
                <WizardFooter
                    onBack={at > 0 ? back : undefined}
                    backDisabled={submitting}
                    note="Credentials stay on the server, encrypted."
                    issue={issue}
                    nudged={nudged}
                    floating={selectionBar}
                    primary={
                        current === "settings" ? (
                            <PrimaryButton blocked={!!issue} pending={submitting} onClick={() => void run()}>
                                {submitting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <UploadIcon className="w-3 h-3" />}
                                Import {plural(importCount, "mailbox", "mailboxes")}
                            </PrimaryButton>
                        ) : (
                            <PrimaryButton blocked={!!issue} onClick={next}>
                                {current === "pick" && itemsFetching && !itemsLoading ? <Loader2Icon className="w-3 h-3 animate-spin" /> : null}
                                {setupStep?.cta ?? "Continue"}
                                <ChevronRightIcon className="w-3 h-3" />
                            </PrimaryButton>
                        )
                    }
                />
            )}
        </div>
    );
}
