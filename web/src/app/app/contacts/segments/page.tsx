// Segments: saved contact audiences (issue #266). Each row is one segment
// with its live member count; clicking opens the segment's contact list.

import React from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { browseText } from "@/lib/browse-contacts-campaigns";
import { useNavigate } from "@tanstack/react-router";
import { MoreHorizontalIcon, PlusIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { EmptyBlock, Page, PageBody, PageTopbar, SectionBar, StatStrip, Stat, TopbarAction } from "@/components/layout/Page";
import { NoAccess } from "@/components/layout/NoAccess";
import { SearchInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import SegmentEditor from "@/components/app/segments/SegmentEditor";
import AddSegmentToCampaignDialog from "@/components/app/segments/AddSegmentToCampaignDialog";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission, useWriteGuard } from "@/hooks/usePermission";
import { useCreateSegment, useDeleteSegment, useSegments } from "@/lib/api/hooks/app/segments";
import type Segment from "@/lib/api/models/app/segments/Segment";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export default function SegmentsPage() {
    const canView = usePermission("VIEW_CONTACTS");
    if (!canView) return <NoAccess feature="segments" permissionLabel="View contacts" />;
    return <SegmentsList />;
}

function SegmentsList() {
    const navigate = useNavigate();
    const confirm = useConfirm();
    const write = useWriteGuard("MANAGE_CONTACTS");
    // Menu items and topbar actions take a bare () => void, so give the
    // permission guard an empty event to swallow.
    const guarded = (fn: () => void) => () => write.guard(fn)({});
    const campaigns = useWriteGuard("MANAGE_CAMPAIGNS");
    const campaignGuarded = (fn: () => void) => () => campaigns.guard(fn)({});
    const segments = useSegments();
    const remove = useDeleteSegment();
    const create = useCreateSegment();

    async function duplicate(s: Segment) {
        try {
            const copy = await create.mutateAsync({
                name: `${s.name} (copy)`.slice(0, 120),
                description: s.description,
                color: s.color,
                match: s.match,
                conditions: s.conditions,
            });
            toast.success(`Duplicated as ${copy.name}`);
            navigate({ to: "/app/contacts/segments/$id", params: { id: copy.id } });
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    const [query, setQuery] = useBrowseState("contacts:segments:query", "", browseText);
    const [editorOpen, setEditorOpen] = React.useState(false);
    const [editing, setEditing] = React.useState<Segment | null>(null);
    const [campaignFor, setCampaignFor] = React.useState<Segment | null>(null);

    const list = React.useMemo(() => {
        const all = segments.data ?? [];
        const q = query.trim().toLowerCase();
        if (!q) return all;
        return all.filter((s) => s.name.toLowerCase().includes(q) || s.description.toLowerCase().includes(q));
    }, [segments.data, query]);

    const totals = React.useMemo(() => {
        const all = segments.data ?? [];
        return {
            segments: all.length,
            contacts: all.reduce((n, s) => n + s.contact_count, 0),
            manual: all.reduce((n, s) => n + s.included_count + s.excluded_count, 0),
            largest: all.reduce<Segment | null>((best, s) => (!best || s.contact_count > best.contact_count ? s : best), null),
        };
    }, [segments.data]);

    function openNew() {
        setEditing(null);
        setEditorOpen(true);
    }

    function openEdit(s: Segment) {
        setEditing(s);
        setEditorOpen(true);
    }

    // The ID is what the segments API takes; copying needs no write permission.
    async function copyId(s: Segment) {
        try {
            await navigator.clipboard.writeText(s.id);
            toast.success("Segment ID copied");
        } catch {
            toast.error("Could not copy");
        }
    }

    function askDelete(s: Segment) {
        confirm.show(`Delete the segment "${s.name}"? Contacts themselves are kept.`, async () => {
            try {
                await remove.mutateAsync(s.id);
                toast.success("Segment deleted");
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
    }

    return (
        <Page>
            <PageTopbar eyebrow="Segments" subtitle="Reusable audiences built from contact data">
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={guarded(openNew)}>
                    New segment
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={3}>
                <Stat label="Segments" value={totals.segments} sub="saved audiences" />
                <Stat label="Memberships" value={totals.contacts.toLocaleString()} sub="across all segments" accent={totals.contacts > 0} />
                <Stat
                    label="Largest"
                    value={totals.largest ? totals.largest.contact_count.toLocaleString() : "0"}
                    sub={totals.largest ? totals.largest.name : "no segments yet"}
                    last
                />
            </StatStrip>

            <SectionBar label="All segments" count={list.length}>
                <SearchInput value={query} onChange={setQuery} placeholder="Search segments…" className="w-full sm:w-64" />
            </SectionBar>

            <PageBody>
                {segments.isPending ? (
                    <div className="p-3 space-y-1.5">
                        {[...Array(5)].map((_, i) => (
                            <div key={i} className="h-11 rounded-md bg-slate-100 animate-pulse" />
                        ))}
                    </div>
                ) : segments.isError ? (
                    <EmptyBlock title="Couldn't load segments" body="Try again in a moment." />
                ) : list.length === 0 ? (
                    <EmptyBlock
                        title={query ? "No segments match" : "No segments yet"}
                        body={
                            query
                                ? "Try a different search."
                                : "Build an audience from contact fields, labels, campaign activity and engagement, then add it to a campaign in one step."
                        }
                        cta={
                            query ? undefined : (
                                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={guarded(openNew)}>
                                    New segment
                                </TopbarAction>
                            )
                        }
                    />
                ) : (
                    <div>
                        {list.map((s) => (
                            <div
                                key={s.id}
                                role="link"
                                tabIndex={0}
                                onClick={() => navigate({ to: "/app/contacts/segments/$id", params: { id: s.id } })}
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") navigate({ to: "/app/contacts/segments/$id", params: { id: s.id } });
                                }}
                                className="group h-11 px-5 flex items-center gap-3 border-b border-slate-200/60 transition-colors hover:bg-slate-50/80 cursor-pointer"
                            >
                                <span className="size-2.5 rounded-full shrink-0" style={{ backgroundColor: s.color }} />
                                <div className="min-w-0 flex-1">
                                    <div className="flex items-center gap-2 min-w-0">
                                        <span className="text-[12.5px] font-medium text-slate-900 truncate">{s.name}</span>
                                        {s.conditions.length === 0 && (
                                            <span className="inline-flex items-center h-4 px-1 rounded bg-slate-100 text-slate-500 text-[10px] font-medium">
                                                manual
                                            </span>
                                        )}
                                    </div>
                                    {s.description && <div className="text-[11px] text-slate-500 truncate">{s.description}</div>}
                                </div>
                                <span className="hidden md:inline text-[11px] text-slate-400 tabular-nums shrink-0">
                                    {s.conditions.length} condition{s.conditions.length === 1 ? "" : "s"}
                                    {s.match === "any" && s.conditions.length > 1 ? " · any" : ""}
                                </span>
                                <span className="font-mono text-[12px] text-slate-900 tabular-nums w-20 text-right shrink-0">
                                    {s.contact_count.toLocaleString()}
                                </span>
                                <span className="text-[10.5px] uppercase tracking-[0.1em] text-slate-400 w-16 shrink-0 hidden sm:inline">
                                    contacts
                                </span>
                                <PopoverMenu align="end">
                                    <PopoverMenuTrigger asChild>
                                        <button
                                            type="button"
                                            onClick={(e) => e.stopPropagation()}
                                            aria-label="More"
                                            className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors shrink-0"
                                        >
                                            <MoreHorizontalIcon className="w-3.5 h-3.5" />
                                        </button>
                                    </PopoverMenuTrigger>
                                    <PopoverMenuContent minWidth={180}>
                                        <PopoverMenuItem onSelect={() => navigate({ to: "/app/contacts/segments/$id", params: { id: s.id } })}>View contacts</PopoverMenuItem>
                                        <PopoverMenuItem onSelect={guarded(() => openEdit(s))}>Edit conditions</PopoverMenuItem>
                                        <PopoverMenuItem onSelect={campaignGuarded(() => setCampaignFor(s))}>Add to campaign</PopoverMenuItem>
                                        <PopoverMenuItem onSelect={guarded(() => duplicate(s))}>Duplicate</PopoverMenuItem>
                                        <PopoverMenuItem onSelect={() => copyId(s)}>Copy segment ID</PopoverMenuItem>
                                        <PopoverMenuSeparator />
                                        <PopoverMenuItem onSelect={guarded(() => askDelete(s))}>Delete</PopoverMenuItem>
                                    </PopoverMenuContent>
                                </PopoverMenu>
                            </div>
                        ))}
                    </div>
                )}
            </PageBody>

            <SegmentEditor
                open={editorOpen}
                onClose={() => setEditorOpen(false)}
                segment={editing}
                onSaved={(saved) => {
                    if (!editing) navigate({ to: "/app/contacts/segments/$id", params: { id: saved.id } });
                }}
            />
            {campaignFor && (
                <AddSegmentToCampaignDialog open={!!campaignFor} onClose={() => setCampaignFor(null)} segment={campaignFor} />
            )}
        </Page>
    );
}
