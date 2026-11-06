// GrantImportWizard: the whole-domain (Google) and whole-organization
// (Microsoft) routes of the connect modal, and the grant view the mailbox
// drawer opens. A new Microsoft grant is one admin approval; new Google
// delegation grants are retired, so Google only lists the grants already held.
// Then Mailboxes (the granted directory) -> Settings -> Import, the last being
// the file import's result screen.
import React from "react";
import { useConnectGrantUsers, useGrantConfig, useGrantUsers, useGrants } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import { domainOf, grantName, type DirectoryUser, type DomainGrant, type GrantProvider } from "@/lib/api/models/app/emails/MailboxSources";
import useMicrosoftAdminConsent from "@/hooks/useMicrosoftAdminConsent";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import type { PickItem } from "../PickTable";
import SourceImportWizard, { type SetupStep } from "../SourceImportWizard";
import GrantAdminStep from "./GrantAdminStep";
import { MicrosoftConsentStep } from "./GrantSetupSteps";

function toItem(u: DirectoryUser): PickItem {
    return {
        id: u.id,
        email: u.email,
        name: u.name,
        domain: domainOf(u.email),
        status: u.enabled ? undefined : { label: "Suspended", cls: "text-amber-700 bg-amber-50" },
        connected: u.connected,
        upgrade: u.upgrade === true,
        disabledReason: u.enabled ? undefined : "Suspended or disabled in the directory, so it cannot sign in.",
    };
}

export default function GrantImportWizard({
    provider,
    initialGrantId,
    onDone,
    onAllowance,
    onDirtyChange,
}: {
    provider: GrantProvider;
    /** Opens with this grant picked, e.g. from a mailbox connected through it. */
    initialGrantId?: string;
    onDone: () => void;
    onAllowance?: () => void;
    onDirtyChange?: (dirty: boolean) => void;
}) {
    const config = useGrantConfig();
    const list = useGrants();
    const [grantId, setGrantId] = React.useState<string | null>(initialGrantId ?? null);
    const users = useGrantUsers(grantId);
    const connect = useConnectGrantUsers();
    const goRef = React.useRef<((key: string) => void) | null>(null);

    const grants = React.useMemo(() => (list.data?.data ?? []).filter((g) => g.provider === provider), [list.data, provider]);
    const grant = grants.find((g) => g.id === grantId) ?? null;
    const directory = users.data?.data;
    const items = React.useMemo(() => directory?.map(toItem), [directory]);

    const enabled = provider === "google" ? config.data?.google_enabled === true : config.data?.microsoft_enabled === true;
    const loading = list.isLoading || config.isLoading;
    // Only Microsoft takes a new grant; with grants already here the list comes first.
    const canAdd = provider === "microsoft";
    const [adding, setAdding] = React.useState(false);
    const noGrants = canAdd && enabled && !loading && grants.length === 0;
    React.useEffect(() => {
        if (noGrants) setAdding(true);
    }, [noGrants]);
    const setupMode = canAdd && enabled && adding;
    const cancelSetup = grants.length > 0 ? () => setAdding(false) : undefined;

    const [msError, setMsError] = React.useState<string | null>(null);

    const granted = React.useCallback((g: DomainGrant) => {
        setGrantId(g.id);
        if (g.status === "active") goRef.current?.("pick");
    }, []);
    const msConsent = useMicrosoftAdminConsent((g) => {
        setMsError(null);
        granted(g);
    }, setMsError);

    const noun = provider === "google" ? "domain" : "organization";
    const grantIssue = !grant
        ? null
        : grant.status === "invalid"
          ? "This grant failed its last check. Check it again first."
          : null;

    let setupSteps: SetupStep[];
    if (setupMode) {
        setupSteps = [
            {
                key: "consent",
                label: "Admin approval",
                issue: grant ? grantIssue : "A Global Administrator has to approve Warmbly first.",
                render: () => <MicrosoftConsentStep consent={msConsent} error={msError} granted={grant} onCancel={cancelSetup} />,
            },
        ];
    } else {
        setupSteps = [
            {
                key: "source",
                label: provider === "google" ? "Domain" : "Organization",
                issue: grant ? grantIssue : canAdd ? `Connect a ${noun} first, or pick one.` : `Pick a ${noun} first.`,
                render: (next) => (
                    <GrantAdminStep
                        provider={provider}
                        config={config.data}
                        grants={grants}
                        loading={loading}
                        selectedId={grantId}
                        onSelect={(g, advance) => {
                            setGrantId(g?.id ?? null);
                            if (g && advance) next();
                        }}
                        onAddNew={
                            canAdd
                                ? () => {
                                      setGrantId(null);
                                      setAdding(true);
                                  }
                                : undefined
                        }
                    />
                ),
            },
        ];
    }

    return (
        <SourceImportWizard
            setupSteps={setupSteps}
            goRef={goRef}
            onStartOver={() => {
                if (grants.length > 0) setAdding(false);
            }}
            sourceKey={grantId}
            pickNote={() =>
                grant ? (
                    <p className="text-[11.5px] text-slate-500">
                        Accounts on <span className="text-slate-700 font-medium">{grantName(grant)}</span>. They connect through the
                        grant: no passwords, and nothing for each person to approve.
                        {items?.some((i) => i.upgrade) &&
                            " A mailbox marked Moves here signs in on its own today; it keeps its history, campaigns and warmup when it moves."}
                    </p>
                ) : null
            }
            items={items}
            itemsLoading={!!grantId && users.isLoading}
            itemsLoadingLogo={provider}
            itemsLoadingSteps={
                grant
                    ? [
                          provider === "google" ? "Asking Google for your directory…" : "Asking Microsoft for your organization's users…",
                          `Listing everyone on ${grantName(grant)}…`,
                          "Checking which are already in this workspace…",
                      ]
                    : undefined
            }
            itemsFetching={users.isFetching}
            itemsError={users.error ? buildError(users.error as unknown as AppError) : null}
            onRefresh={() => void users.refetch()}
            source="grant"
            submit={(ids, options) => connect.mutateAsync({ id: grantId!, body: { user_ids: ids, options } })}
            submitting={connect.isPending}
            anotherLabel="Connect more mailboxes"
            onDone={onDone}
            onAllowance={onAllowance}
            onDirtyChange={onDirtyChange}
        />
    );
}
