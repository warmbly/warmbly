// VendorImportWizard: the "Inbox vendor" route of the connect modal. Account
// (a saved vendor key, or a new one) -> Mailboxes -> Settings -> Import, the
// last being the file import's result screen.
import React from "react";
import { InfoIcon } from "lucide-react";
import {
    useImportVendorMailboxes,
    useVendorCatalog,
    useVendorConnections,
    useVendorMailboxes,
} from "@/lib/api/hooks/app/emails/useMailboxVendors";
import { useGrants } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import { grantFor, vendorLabel, type VendorMailbox } from "@/lib/api/models/app/emails/MailboxSources";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { plural } from "../importFields";
import type { PickItem } from "../PickTable";
import SourceImportWizard from "../SourceImportWizard";
import VendorAccountStep from "./VendorAccountStep";

const QUIET_STATUS = /^(active|ok|ready|live|healthy|connected|)$/i;
const BAD_STATUS = /(suspend|disabl|inactive|error|fail|block|delet|cancel|expired)/i;

function statusPill(status: string): PickItem["status"] {
    const s = status.trim();
    if (QUIET_STATUS.test(s)) return undefined;
    const label = s.charAt(0).toUpperCase() + s.slice(1).toLowerCase().replace(/_/g, " ");
    return { label, cls: BAD_STATUS.test(s) ? "text-amber-700 bg-amber-50" : "text-slate-600 bg-slate-100" };
}

function toItem(m: VendorMailbox, labelWorkspace: boolean): PickItem {
    return {
        id: m.id,
        email: m.email,
        name: m.name,
        domain: m.domain,
        group: labelWorkspace ? m.workspace : undefined,
        provider: m.provider,
        status: statusPill(m.status),
        connected: m.connected,
    };
}

export default function VendorImportWizard({
    onDone,
    onAllowance,
    onDirtyChange,
}: {
    onDone: () => void;
    onAllowance?: () => void;
    onDirtyChange?: (dirty: boolean) => void;
}) {
    const catalog = useVendorCatalog();
    const conns = useVendorConnections();
    const grants = useGrants();
    const [connId, setConnId] = React.useState<string | null>(null);
    const [formDirty, setFormDirty] = React.useState(false);
    const boxes = useVendorMailboxes(connId);
    const imp = useImportVendorMailboxes();

    const connections = React.useMemo(() => conns.data?.data ?? [], [conns.data]);
    const conn = connections.find((c) => c.id === connId) ?? null;
    const mailboxes = boxes.data?.data;
    // The workspace is only worth a line when the key reaches more than one.
    const items = React.useMemo(() => {
        const several = new Set((mailboxes ?? []).map((m) => m.workspace ?? "")).size > 1;
        return mailboxes?.map((m) => toItem(m, several));
    }, [mailboxes]);

    const sourceIssue = !conn
        ? "Pick a vendor account, or connect one."
        : conn.status === "invalid"
          ? "The vendor refuses this account's key. Update it first."
          : null;

    const microsoft = (mailboxes ?? []).filter((m) => m.provider === "microsoft");
    const granted = microsoft.filter((m) => grantFor(grants.data?.data.filter((g) => g.provider === "microsoft" && g.status === "active"), m.email));

    return (
        <SourceImportWizard
            sourceLabel="Account"
            sourceKey={connId}
            sourceIssue={sourceIssue}
            sourceDirty={formDirty}
            renderSource={(next) => (
                <VendorAccountStep
                    catalog={catalog.data?.data ?? []}
                    catalogError={catalog.error ? buildError(catalog.error as unknown as AppError) : null}
                    connections={connections}
                    loading={conns.isLoading || catalog.isLoading}
                    selectedId={connId}
                    onSelect={(c, advance) => {
                        setConnId(c?.id ?? null);
                        if (c && advance) next();
                    }}
                    onDirtyChange={setFormDirty}
                />
            )}
            pickNote={() => (
                <>
                    {conn && (
                        <p className="text-[11.5px] text-slate-500">
                            Mailboxes in <span className="text-slate-700 font-medium">{conn.label}</span>
                            {conn.label !== vendorLabel(conn.vendor) ? ` at ${vendorLabel(conn.vendor)}` : ""}. Each one's credentials are
                            read from the vendor while the import runs, never before.
                        </p>
                    )}
                    {microsoft.length > 0 && (
                        <div className="rounded-md border border-sky-200 bg-sky-50/60 px-3 py-2.5 flex items-start gap-2">
                            <InfoIcon className="w-3.5 h-3.5 text-sky-600 mt-0.5 shrink-0" />
                            <div className="min-w-0 text-[11.5px] text-sky-900/90 leading-relaxed">
                                <p className="text-[12.5px] font-medium text-sky-900">
                                    {plural(microsoft.length, "Microsoft mailbox connects", "Microsoft mailboxes connect")} with Microsoft
                                    sign-in
                                </p>
                                Microsoft 365 takes no passwords over IMAP, so after the import each one gets a Sign in button, unless an
                                admin grant covers its domain: then it connects through the grant with no sign-in.
                                {granted.length > 0 && ` ${plural(granted.length, "of them is", "of them are")} on a domain your grant covers.`}
                            </div>
                        </div>
                    )}
                </>
            )}
            items={items}
            itemsLoading={!!connId && boxes.isLoading}
            itemsLoadingLogo={conn?.vendor}
            itemsLoadingSteps={
                conn
                    ? [
                          `Connecting to ${vendorLabel(conn.vendor)}…`,
                          `Reading the mailboxes in ${conn.label}…`,
                          "Checking which are already in this workspace…",
                      ]
                    : undefined
            }
            itemsFetching={boxes.isFetching}
            itemsError={boxes.error ? buildError(boxes.error as unknown as AppError) : null}
            onRefresh={() => void boxes.refetch()}
            source="vendor"
            submit={(ids, options) => imp.mutateAsync({ id: connId!, body: { mailbox_ids: ids, options } })}
            submitting={imp.isPending}
            anotherLabel="Import more from a vendor"
            onDone={onDone}
            onAllowance={onAllowance}
            onDirtyChange={onDirtyChange}
        />
    );
}
