// Admin grant, the first step when the workspace already holds grants for the
// provider: pick one to connect its mailboxes, check or remove it, or (Microsoft
// only) start the setup of another (GrantSetupSteps).
import toast from "react-hot-toast/headless";
import { ChevronRightIcon, MoreHorizontalIcon, PlusIcon, RefreshCwIcon, SettingsIcon, Trash2Icon } from "lucide-react";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import { useConfirm } from "@/hooks/context/confirm";
import { useCheckGrant, useDeleteGrant } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import {
    GRANT_PROVIDER_LABELS,
    grantName,
    type DomainGrant,
    type GrantConfig,
    type GrantProvider,
} from "@/lib/api/models/app/emails/MailboxSources";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { cn } from "@/lib/utils";
import { plural } from "../importFields";
import { SectionLabel, SourceStatusPill } from "../parts";
import GrantSetupNote from "./GrantSetupNote";
import { SkeletonCards } from "../Discovering";

/** Admin connections this instance has no credentials for. */
export function GrantsNotSetUp({ provider, config }: { provider: GrantProvider; config: GrantConfig | undefined }) {
    return (
        <div className="rounded-md border border-amber-200 bg-amber-50 p-3 flex items-start gap-2.5">
            <SettingsIcon className="w-4 h-4 text-amber-600 mt-0.5 shrink-0" />
            <div className="min-w-0 text-[12px] text-amber-800">
                <p className="text-[12.5px] font-medium text-amber-900 mb-1">
                    {GRANT_PROVIDER_LABELS[provider]} admin connections are not set up on this instance
                </p>
                <GrantSetupNote provider={provider} config={config} tone="amber" />
            </div>
        </div>
    );
}

export default function GrantAdminStep({
    provider,
    config,
    grants,
    loading,
    selectedId,
    onSelect,
    onAddNew,
}: {
    provider: GrantProvider;
    config: GrantConfig | undefined;
    grants: DomainGrant[];
    loading: boolean;
    selectedId: string | null;
    /** A picked grant; `advance` moves to its users. */
    onSelect: (grant: DomainGrant | null, advance: boolean) => void;
    /** Starts the setup of another grant; absent for Google, whose new grants are retired. */
    onAddNew?: () => void;
}) {
    const enabled = provider === "google" ? config?.google_enabled === true : config?.microsoft_enabled === true;
    const noun = provider === "google" ? "domain" : "organization";

    if (loading) {
        return (
            <div className="p-4" role="status" aria-label="Loading admin connections">
                <SkeletonCards count={2} />
            </div>
        );
    }

    return (
        <div className="p-4 space-y-4">
            {!enabled && (provider === "microsoft" || grants.length > 0) && <GrantsNotSetUp provider={provider} config={config} />}

            {grants.length > 0 && (
                <div>
                    <SectionLabel className="mb-1.5">Connected {noun === "domain" ? "domains" : "organizations"}</SectionLabel>
                    <div className="rounded-md border border-slate-200 divide-y divide-slate-100">
                        {grants.map((g) => (
                            <GrantRow
                                key={g.id}
                                grant={g}
                                active={g.id === selectedId}
                                onOpen={() => onSelect(g, g.status === "active")}
                                onRemoved={() => {
                                    if (g.id === selectedId) onSelect(null, false);
                                }}
                            />
                        ))}
                    </div>
                </div>
            )}

            {provider === "google" && <GoogleDelegationRetired none={grants.length === 0} />}

            {enabled && onAddNew && (
                <button
                    type="button"
                    onClick={onAddNew}
                    className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5 transition-colors"
                >
                    <PlusIcon className="w-3 h-3" />
                    Connect another {noun}
                </button>
            )}
        </div>
    );
}

/** New Google domain-wide delegation grants are retired; grants already here keep working. */
function GoogleDelegationRetired({ none }: { none: boolean }) {
    return (
        <p className="text-[11.5px] text-slate-500 leading-relaxed">
            {none ? "This workspace holds no Google domain-wide delegation grant. " : ""}
            New Google Workspace domains are no longer connected with delegation: your Workspace admin marks Warmbly as a trusted
            app, then each mailbox signs in with Google, or connects with an app password.{" "}
            {none ? "" : "The grants above keep working, and you can connect more of their mailboxes."}
        </p>
    );
}

function GrantRow({
    grant: g,
    active,
    onOpen,
    onRemoved,
}: {
    grant: DomainGrant;
    active: boolean;
    onOpen: () => void;
    onRemoved: () => void;
}) {
    const confirm = useConfirm();
    const check = useCheckGrant();
    const del = useDeleteGrant();
    const checking = check.isPending && check.variables === g.id;
    const invalid = g.status === "invalid";

    const runCheck = async () => {
        try {
            const out = await check.mutateAsync(g.id);
            if (out.status === "active") toast.success(`${grantName(out)} is working`);
            else toast.error(out.last_error || "The grant still fails its check.");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const remove = () =>
        confirm.show(
            `Remove the admin grant for ${grantName(g)}? Its ${plural(g.mailboxes, "mailbox stops", "mailboxes stop")} sending and syncing right away, because they have no password of their own. To bring them back, connect the ${g.provider === "google" ? "domain" : "organization"} again or connect each mailbox another way.`,
            async () => {
                try {
                    await del.mutateAsync(g.id);
                    onRemoved();
                    toast.success("Admin grant removed");
                } catch (e) {
                    toast.error(buildError(e as AppError));
                }
            },
        );

    return (
        <div
            role="button"
            tabIndex={0}
            onClick={onOpen}
            onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onOpen();
                }
            }}
            className={cn(
                "group px-3 py-2.5 flex items-start gap-2.5 cursor-pointer transition-colors outline-none focus-visible:bg-slate-50",
                active ? "bg-sky-50/50" : "hover:bg-slate-50",
            )}
        >
            <ProviderLogo id={g.provider} size="lg" />
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 min-w-0">
                    <span className="text-[12.5px] font-medium text-slate-900 truncate">{grantName(g)}</span>
                    <SourceStatusPill status={g.status} />
                </div>
                <div className="text-[11px] text-slate-500 mt-0.5 truncate">
                    {plural(g.mailboxes, "mailbox", "mailboxes")} connected
                    {g.verified_at ? ` · checked ${timeAgo(g.verified_at)}` : ""}
                    {g.admin_email ? ` · as ${g.admin_email}` : ""}
                </div>
                {invalid && (
                    <p className="text-[11.5px] text-red-700 mt-1 leading-snug">
                        {g.last_error || "The grant failed its last check."} Its mailboxes are stopped until it works again.
                    </p>
                )}
            </div>
            <div className="shrink-0 flex items-center gap-1 self-center">
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        void runCheck();
                    }}
                    disabled={checking}
                    className={cn(
                        "h-6 px-2 rounded-md text-[11.5px] font-medium inline-flex items-center gap-1 transition-colors disabled:opacity-60",
                        invalid ? "bg-slate-900 hover:bg-slate-800 text-white" : "border border-slate-200 text-slate-700 hover:bg-white",
                    )}
                >
                    <RefreshCwIcon className={cn("w-3 h-3", checking && "animate-spin")} />
                    <span className="hidden sm:inline">Check again</span>
                </button>
                <div onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
                    <PopoverMenu align="end">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                aria-label={`More for ${grantName(g)}`}
                                className="size-6 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <MoreHorizontalIcon className="w-3.5 h-3.5" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={170}>
                            <PopoverMenuItem icon={<RefreshCwIcon className="w-3.5 h-3.5" />} onSelect={() => void runCheck()}>
                                Check again
                            </PopoverMenuItem>
                            <PopoverMenuItem danger icon={<Trash2Icon className="w-3.5 h-3.5" />} onSelect={remove}>
                                Remove grant
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                </div>
                <ChevronRightIcon className="w-3.5 h-3.5 text-slate-300 group-hover:text-slate-500 transition-colors" />
            </div>
        </div>
    );
}
