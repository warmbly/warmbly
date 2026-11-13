// "Migrate from another tool": pick the tool, paste its key, preview what the
// scan found, (agency) choose a client for each source workspace, then watch it
// run. The server holds the migration from the moment the key is accepted; this
// component only holds the choices until it starts.

import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowRightLeftIcon, ChevronRightIcon, Loader2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { useConfirm } from "@/hooks/context/confirm";
import { useAgencyClients } from "@/lib/api/hooks/app/agency/useAgency";
import {
    useCreateToolMigration,
    useDismissToolMigration,
    useStartToolMigration,
    useToolMigration,
    useToolMigrationProviders,
} from "@/lib/api/hooks/app/toolmigrate/useToolMigrations";
import type { ToolCredentials, ToolMigration, ToolMigrationScope } from "@/lib/api/models/app/toolmigrate/ToolMigration";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { PrimaryButton, Stepper, WizardFooter, WizardPanes, type WizardStep } from "@/components/app/emails/import/wizard";
import {
    choicesIssue,
    credentialsIssue,
    credentialsTyped,
    defaultChoices,
    defaultMapping,
    emptyCredentials,
    mappingIssue,
    toOptions,
    toTargets,
    type Choices,
    type Mapping,
} from "./choices";
import PickStep from "./PickStep";
import ConnectStep from "./ConnectStep";
import PreviewStep from "./PreviewStep";
import MapStep from "./MapStep";
import RunStep from "./RunStep";

type StepKey = "pick" | "connect" | "preview" | "map" | "run";

const WORKSPACE_STEPS: WizardStep<StepKey>[] = [
    { key: "pick", label: "Tool" },
    { key: "connect", label: "Connect" },
    { key: "preview", label: "Preview" },
    { key: "run", label: "Migrate" },
];
const AGENCY_STEPS: WizardStep<StepKey>[] = [
    { key: "pick", label: "Tool" },
    { key: "connect", label: "Connect" },
    { key: "preview", label: "Preview" },
    { key: "map", label: "Clients" },
    { key: "run", label: "Migrate" },
];

/** A migration past its preview: started, or ended after starting. */
const started = (m: ToolMigration) =>
    m.status === "running" || m.status === "completed" || m.status === "canceled" || (m.status === "failed" && (m.targets?.length ?? 0) > 0);

export default function ToolMigrationWizard({
    scope,
    open,
    onClose,
    migrationId,
    provider,
}: {
    scope: ToolMigrationScope;
    open: boolean;
    onClose: () => void;
    /** Opens an existing migration at its preview or its progress. */
    migrationId?: string | null;
    /** Starts a new migration at the key step for this tool. */
    provider?: string | null;
}) {
    const [dirty, setDirty] = React.useState<string | null>(null);
    const [busy, setBusy] = React.useState(false);
    const confirm = useConfirm();

    React.useEffect(() => {
        if (!open) {
            setDirty(null);
            setBusy(false);
        }
    }, [open]);

    const requestClose = React.useCallback(() => {
        if (busy) return;
        if (dirty) {
            confirm.show(dirty, async () => onClose());
            return;
        }
        onClose();
    }, [busy, dirty, confirm, onClose]);

    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, requestClose]);

    return createPortal(
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onMouseDown={requestClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-2 sm:px-4"
                >
                    <motion.div
                        key="card"
                        role="dialog"
                        aria-modal="true"
                        aria-label="Migrate from another tool"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="w-full max-w-[820px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18),0_8px_16px_-8px_rgba(15,23,42,0.1)] overflow-hidden flex flex-col max-h-[92dvh] sm:max-h-[88dvh]"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <ArrowRightLeftIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Migrate</span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12px] text-slate-600 truncate">From another tool</span>
                            <button
                                type="button"
                                onClick={requestClose}
                                disabled={busy}
                                aria-label="Close"
                                title={busy ? "Wait for this to finish" : undefined}
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors disabled:opacity-40 disabled:hover:bg-transparent disabled:cursor-not-allowed"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden">
                            <WizardBody
                                key={migrationId ?? provider ?? "new"}
                                scope={scope}
                                initialId={migrationId ?? null}
                                initialProvider={provider ?? null}
                                onDone={onClose}
                                setDirty={setDirty}
                                setBusy={setBusy}
                            />
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>,
        document.body,
    );
}

function WizardBody({
    scope,
    initialId,
    initialProvider,
    onDone,
    setDirty,
    setBusy,
}: {
    scope: ToolMigrationScope;
    initialId: string | null;
    initialProvider: string | null;
    onDone: () => void;
    /** The text asking to discard what would be lost, or null when closing loses nothing. */
    setDirty: (text: string | null) => void;
    setBusy: (busy: boolean) => void;
}) {
    const agency = scope.kind === "agency";
    const steps = agency ? AGENCY_STEPS : WORKSPACE_STEPS;
    const confirm = useConfirm();

    const [step, setStep] = React.useState<StepKey>(initialId ? "preview" : initialProvider ? "connect" : "pick");
    const [direction, setDirection] = React.useState<1 | -1>(1);
    const [nudged, setNudged] = React.useState(false);

    const [providerId, setProviderId] = React.useState<string | null>(initialProvider);
    const [creds, setCreds] = React.useState<ToolCredentials[]>([{}]);
    const [connectError, setConnectError] = React.useState<string | null>(null);
    const [id, setId] = React.useState<string | null>(initialId);
    const [choices, setChoices] = React.useState<Choices | null>(null);
    const [touched, setTouched] = React.useState(false);
    const [mapping, setMapping] = React.useState<Mapping | null>(null);

    // One key per submission: a retry after a lost answer replays it, a changed key mints a new one.
    const createKey = React.useRef(crypto.randomUUID());
    const startKey = React.useRef(crypto.randomUUID());

    const providers = useToolMigrationProviders(scope);
    const migration = useToolMigration(scope, id);
    const clients = useAgencyClients(agency ? scope.agencyId : undefined);
    const create = useCreateToolMigration(scope);
    const start = useStartToolMigration(scope);
    const dismiss = useDismissToolMigration(scope);

    const m = migration.data;
    const list = providers.data?.data ?? [];
    const info = list.find((p) => p.id === (m?.provider ?? providerId));
    const label = info?.label ?? m?.label ?? "the other tool";

    // A tool chosen before the list loaded gets its field defaults once it has.
    React.useEffect(() => {
        if (info && creds.length === 1 && Object.keys(creds[0]).length === 0 && info.fields.length > 0) setCreds([emptyCredentials(info)]);
    }, [info, creds]);

    const at = steps.findIndex((s) => s.key === step);

    const go = React.useCallback(
        (target: StepKey) => {
            const from = steps.findIndex((s) => s.key === step);
            const to = steps.findIndex((s) => s.key === target);
            setDirection(to >= from ? 1 : -1);
            setNudged(false);
            setStep(target);
        },
        [step, steps],
    );

    // A migration that has started (here, by a teammate, or before this was opened) shows its progress.
    React.useEffect(() => {
        if (m && started(m) && step !== "run") go("run");
    }, [m, step, go]);

    // The preview's choices start from what the provider can bring, once the scan is in.
    React.useEffect(() => {
        if (m?.status === "ready" && info && !choices) setChoices(defaultChoices(info));
    }, [m?.status, info, choices]);

    React.useEffect(() => {
        if (agency && m?.status === "ready" && !mapping && clients.data) setMapping(defaultMapping(m, clients.data, label));
    }, [agency, m, mapping, clients.data, label]);

    const typed = !id && credentialsTyped(creds, info);
    React.useEffect(() => {
        if (typed) setDirty(`Discard these keys? Nothing has been read from ${label} yet.`);
        else if (m?.status === "ready" && touched) setDirty("Close without starting? The scan stays in your list, but the choices made here are lost.");
        else setDirty(null);
    }, [typed, m?.status, touched, label, setDirty]);

    const pending = create.isPending || start.isPending || dismiss.isPending;
    React.useEffect(() => setBusy(pending), [pending, setBusy]);

    const pick = (pid: string) => {
        if (pid === providerId) return go("connect");
        const apply = () => {
            const next = list.find((p) => p.id === pid);
            setProviderId(pid);
            setCreds([emptyCredentials(next)]);
            setConnectError(null);
            createKey.current = crypto.randomUUID();
            go("connect");
        };
        if (typed) confirm.show(`Switch tools? The keys typed for ${label} are cleared.`, async () => apply());
        else apply();
    };

    const editCreds = (fn: (prev: ToolCredentials[]) => ToolCredentials[]) => {
        setCreds(fn);
        setConnectError(null);
        createKey.current = crypto.randomUUID();
    };

    // Changing the keys after a scan reads the account again from scratch.
    const backToConnect = () => {
        if (!id) return go("connect");
        confirm.show(`Change the keys? This scan is discarded and ${label} is read again.`, async () => {
            try {
                await dismiss.mutateAsync(id);
            } catch (e) {
                toast.error(buildError(e as AppError));
                return;
            }
            setId(null);
            setChoices(null);
            setMapping(null);
            setTouched(false);
            if (!providerId && m) {
                setProviderId(m.provider);
                setCreds([emptyCredentials(info)]);
            }
            createKey.current = crypto.randomUUID();
            go("connect");
        });
    };

    const updateChoices = (c: Choices) => {
        setChoices(c);
        setTouched(true);
    };
    const updateMapping = (next: Mapping) => {
        setMapping(next);
        setTouched(true);
    };

    function issue(key: StepKey): string | null {
        switch (key) {
            case "pick":
                return providerId ? null : "Choose the tool you are moving from.";
            case "connect":
                if (!info) return "Choose the tool you are moving from.";
                return credentialsIssue(info, creds);
            case "preview":
                if (!m) return "Still loading the scan.";
                if (m.status === "scanning") return `Still reading your ${label} account.`;
                if (m.status === "failed") return "The scan did not finish. Go back and check the key.";
                if ((m.inventory ?? []).every((w) => w.error)) return "No workspace could be read. Go back and check the key.";
                if (!choices) return "Still loading the scan.";
                return choicesIssue(m, choices);
            case "map":
                if (!m || !mapping) return "Still loading your clients.";
                return mappingIssue(m, mapping, label);
            case "run":
                return null;
        }
    }

    async function submitConnect() {
        if (!info || create.isPending) return;
        setConnectError(null);
        try {
            const created = await create.mutateAsync({
                data: {
                    provider: info.id,
                    credentials: creds.map((c) => Object.fromEntries(Object.entries(c).map(([k, v]) => [k, v.trim()]).filter(([, v]) => v !== ""))),
                },
                idempotencyKey: createKey.current,
            });
            setId(created.id);
            setChoices(null);
            setMapping(null);
            setTouched(false);
            startKey.current = crypto.randomUUID();
            go("preview");
        } catch (e) {
            // An answered refusal created nothing, so the next try is a new submission; a lost answer replays this one.
            if ((e as AppError).status) createKey.current = crypto.randomUUID();
            setConnectError(buildError(e as AppError));
        }
    }

    async function submitStart() {
        if (!m || !choices || start.isPending) return;
        try {
            await start.mutateAsync({
                id: m.id,
                data: { options: toOptions(m, choices), targets: agency && mapping ? toTargets(m, mapping) : undefined },
                idempotencyKey: startKey.current,
            });
            setTouched(false);
            go("run");
        } catch (e) {
            if ((e as AppError).status) startKey.current = crypto.randomUUID();
            toast.error(buildError(e as AppError));
        }
    }

    const current = issue(step);
    const attempt = (fn: () => void) => {
        if (current) {
            setNudged(true);
            return;
        }
        fn();
    };

    // Before a scan exists the first two steps are open; after it, they are reached through "Change the keys".
    const canClick = (key: StepKey, i: number) => {
        if (i >= at || step === "run") return false;
        if (key === "pick" || key === "connect") return !id;
        return true;
    };

    let body: React.ReactNode;
    let primary: React.ReactNode;
    let onBack: (() => void) | undefined;

    switch (step) {
        case "pick":
            body = (
                <PickStep
                    providers={list}
                    loading={providers.isLoading}
                    error={providers.error ? buildError(providers.error as unknown as AppError) : null}
                    selected={providerId}
                    onPick={pick}
                />
            );
            primary = (
                <PrimaryButton blocked={!!current} onClick={() => attempt(() => go("connect"))}>
                    Next
                    <ChevronRightIcon className="w-3 h-3" />
                </PrimaryButton>
            );
            break;
        case "connect":
            body = info ? (
                <ConnectStep
                    info={info}
                    creds={creds}
                    agency={agency}
                    error={connectError}
                    onChange={(i, k, v) => editCreds((prev) => prev.map((c, j) => (j === i ? { ...c, [k]: v } : c)))}
                    onAdd={() => editCreds((prev) => [...prev, emptyCredentials(info)])}
                    onRemove={(i) => editCreds((prev) => prev.filter((_, j) => j !== i))}
                />
            ) : (
                <Loading />
            );
            onBack = () => go("pick");
            primary = (
                <PrimaryButton blocked={!!current} pending={create.isPending} onClick={() => attempt(() => void submitConnect())}>
                    {create.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                    Connect and scan
                </PrimaryButton>
            );
            break;
        case "preview":
            body = m && choices ? (
                <PreviewStep migration={m} info={info} choices={choices} onChoices={updateChoices} />
            ) : m && m.status !== "ready" ? (
                <PreviewStep migration={m} info={info} choices={defaultChoices(info)} onChoices={updateChoices} />
            ) : (
                <Loading />
            );
            onBack = m && m.status !== "scanning" ? backToConnect : undefined;
            primary = agency ? (
                <PrimaryButton blocked={!!current} onClick={() => attempt(() => go("map"))}>
                    Next: choose clients
                    <ChevronRightIcon className="w-3 h-3" />
                </PrimaryButton>
            ) : (
                <PrimaryButton blocked={!!current} pending={start.isPending} onClick={() => attempt(() => void submitStart())}>
                    {start.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                    Start migration
                </PrimaryButton>
            );
            break;
        case "map":
            body = m && mapping ? <MapStep migration={m} label={label} clients={clients.data ?? []} mapping={mapping} onMapping={updateMapping} /> : <Loading />;
            onBack = () => go("preview");
            primary = (
                <PrimaryButton blocked={!!current} pending={start.isPending} onClick={() => attempt(() => void submitStart())}>
                    {start.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                    Start migration
                </PrimaryButton>
            );
            break;
        case "run":
            body = m ? <RunStep scope={scope} migration={m} label={label} onLeave={onDone} /> : <Loading />;
            primary = <PrimaryButton blocked={false} onClick={onDone}>Done</PrimaryButton>;
            break;
    }

    return (
        <div className="flex flex-col min-h-full">
            <Stepper steps={steps} at={at} canClick={canClick} goTo={go} />
            <div className="flex-1">
                {migration.isError && !m ? (
                    <p className="p-5 text-[12px] text-red-600">{buildError(migration.error as unknown as AppError)}</p>
                ) : (
                    <WizardPanes stepKey={step} direction={direction}>
                        {body}
                    </WizardPanes>
                )}
            </div>
            <WizardFooter
                onBack={onBack}
                backDisabled={pending}
                note={step === "run" ? undefined : "Nothing changes here until you start."}
                issue={current}
                nudged={nudged}
                primary={primary}
            />
        </div>
    );
}

function Loading() {
    return (
        <div className="h-48 flex items-center justify-center gap-2 text-[12px] text-slate-400">
            <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
            Loading
        </div>
    );
}
