// The contacts table's column registry. Every column the standalone list and
// the campaign Leads view can show is defined once here: its header, width,
// breakpoint, sort key and cell. The table renders whatever ordered subset the
// member's saved view names, so the header and the rows can never drift apart,
// and a custom field becomes a column by naming it ("custom:Industry").

import React from "react";
import {
    Building2Icon,
    CornerUpLeftIcon,
    MailIcon,
    MailOpenIcon,
    MousePointerClickIcon,
    PhoneIcon,
    type LucideIcon,
} from "lucide-react";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import clippedTitle from "@/lib/helper/clippedTitle";
import { mailHostLabel } from "@/lib/mailHost";
import type { ContactCampaignProgress, VerificationSource, VerificationStatus } from "@/lib/api/models/app/contacts/Contact";
import type { SearchContactsSortBy } from "@/lib/api/models/app/contacts/search-contacts.types";
import type { ViewName } from "@/lib/api/models/app/views/ViewPreferences";
import { CategoryChip } from "./CategoryPicker";
import VerificationBadge from "./VerificationBadge";
import { Dash, EngagementValue, InfoHeader, LeadStatusPill, StatusPill } from "./cells";

// The fields a row needs to render any column. Structural rather than the full
// Contact so the campaign pickers' lighter rows still fit.
export interface ContactRow {
    id: string;
    first_name: string;
    last_name: string;
    email: string;
    company: string;
    phone: string;
    subscribed: boolean;
    campaigns: { id: string }[];
    categories?: { id: string; title: string; color: string }[];
    campaign_lead?: ContactCampaignProgress | null;
    custom_fields?: Record<string, string>;
    verification_status?: VerificationStatus;
    verification_sub_status?: string;
    verification_source?: VerificationSource;
    verification_provider?: string;
    verification_checked_at?: string | null;
    verification_confidence?: number;
    verification_requested_at?: string | null;
    mail_host?: string;
    created_at: Date;
    updated_at?: Date;
}

export interface CellContext {
    c: ContactRow;
    lead: ContactCampaignProgress | null | undefined;
    // A terminal lead in the Leads view renders muted.
    processed: boolean;
    embedded: boolean;
}

export type Breakpoint = "sm" | "md" | "lg" | "xl" | "2xl";

export interface ContactColumn {
    id: string;
    label: string;
    // Richer header content (an info tooltip); the label stays the plain name.
    header?: React.ReactNode;
    // A Tailwind width class. Every column but Name carries one: the table is
    // table-fixed (issue #461), and Name takes whatever the sized ones leave.
    width: string;
    // The breakpoint the column appears at. Below it the column is hidden and
    // the drawer still has the value.
    hideBelow?: Breakpoint;
    align?: "right";
    sortKey?: SearchContactsSortBy;
    // Whether the first click on the header sorts ascending (text) rather than
    // descending (dates and counts, where newest or most is what people want).
    sortAsc?: boolean;
    cellClassName?: string;
    cell: (ctx: CellContext) => React.ReactNode;
    // Name: always shown, always first, never offered in the chooser.
    locked?: boolean;
    // The custom-field key this column shows, for the custom columns.
    custom?: string;
}

const HIDE: Record<Breakpoint, string> = {
    sm: "hidden sm:table-cell",
    md: "hidden md:table-cell",
    lg: "hidden lg:table-cell",
    xl: "hidden xl:table-cell",
    "2xl": "hidden 2xl:table-cell",
};

// The classes a column's header and cells share: width, breakpoint, alignment.
export function columnClass(col: ContactColumn): string {
    return [col.width, col.hideBelow ? HIDE[col.hideBelow] : "", col.align === "right" ? "text-right" : ""]
        .filter(Boolean)
        .join(" ");
}

export const CUSTOM_COLUMN_PREFIX = "custom:";
export const customColumnId = (key: string) => `${CUSTOM_COLUMN_PREFIX}${key}`;
export const isCustomColumnId = (id: string) => id.startsWith(CUSTOM_COLUMN_PREFIX);
export const customKeyOf = (id: string) => id.slice(CUSTOM_COLUMN_PREFIX.length);

function shortDate(d: Date | string | null | undefined): string {
    if (!d) return "—";
    return new Date(d).toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

const nameColumn: ContactColumn = {
    id: "name",
    label: "Name",
    width: "",
    sortKey: "first_name",
    sortAsc: true,
    locked: true,
    cell: ({ c, processed }) => {
        const name =
            (c.first_name || c.last_name)
                ? `${c.first_name ?? ""} ${c.last_name ?? ""}`.trim()
                : c.email;
        return (
            <div className="flex items-center gap-2.5 min-w-0">
                <div className="w-6 h-6 rounded-full bg-slate-100 flex items-center justify-center shrink-0">
                    <span className="text-[9.5px] font-semibold text-slate-600">
                        {(c.first_name || c.email)?.slice(0, 2).toUpperCase()}
                    </span>
                </div>
                {/* flex-1, not shrink-to-fit: the chip cap below is a
                    percentage, so this has to be the column's width and
                    not the name's. */}
                <div className="flex-1 min-w-0">
                    <div className={`text-[12.5px] font-medium truncate leading-tight flex items-center gap-1.5 ${processed ? "text-slate-400" : "text-slate-900"}`}>
                        <span className="truncate" {...clippedTitle}>{name}</span>
                        {/* One tag, then a count. The Name column is a fixed width
                            now, and two tags sharing it with a name left each of them
                            about three legible characters. The tags take at most 45%
                            of the line, and the +N tooltip names the rest in full. */}
                        {c.categories && c.categories.length > 0 && (
                            <span className="inline-flex items-center gap-0.5 min-w-0 max-w-[45%]">
                                <CategoryChip category={c.categories[0]} compact />
                                {c.categories.length > 1 && (
                                    <span
                                        className="inline-flex items-center h-4 px-1 shrink-0 rounded text-[10px] font-medium bg-slate-100 text-slate-500"
                                        title={c.categories.slice(1).map((x) => x.title).join(", ")}
                                    >
                                        +{c.categories.length - 1}
                                    </span>
                                )}
                            </span>
                        )}
                    </div>
                    <div className="text-[10.5px] text-slate-400 truncate font-mono leading-tight flex items-center gap-1">
                        <MailIcon className="w-2.5 h-2.5 shrink-0" />
                        <span className="truncate" {...clippedTitle}>{c.email}</span>
                        <VerificationBadge contact={c} />
                    </div>
                </div>
            </div>
        );
    },
};

// Name is the only auto-width column, so it takes every pixel the sized columns
// leave. Which breakpoint each sized column appears at is then just "does Name
// still clear ~170px": Leads carries five campaign columns Contacts does not, so
// company waits longer for room there.
function companyColumn(view: ViewName): ContactColumn {
    return {
        id: "company",
        label: "Company",
        width: view === "campaign_leads" ? "w-40" : "w-36",
        hideBelow: view === "campaign_leads" ? "xl" : "lg",
        sortKey: "company",
        sortAsc: true,
        cellClassName: "text-[12px] text-slate-600",
        cell: ({ c }) =>
            c.company ? (
                <div className="flex items-center gap-1.5 min-w-0">
                    <Building2Icon className="w-3 h-3 shrink-0 text-slate-400" />
                    <span className="truncate" {...clippedTitle}>{c.company}</span>
                </div>
            ) : (
                <Dash />
            ),
    };
}

// Who hosts the contact's inbox. Empty until the backend's DNS check reaches
// the contact, a minute or so after it is added.
function mailHostColumn(view: ViewName): ContactColumn {
    return {
        id: "mail_host",
        label: "Email provider",
        width: "w-40",
        hideBelow: view === "campaign_leads" ? "2xl" : "xl",
        sortKey: "mail_host",
        sortAsc: true,
        cellClassName: "text-[12px] text-slate-600",
        cell: ({ c }) =>
            c.mail_host ? (
                <div className="flex items-center gap-1.5 min-w-0">
                    <ProviderLogo id={c.mail_host} size="xs" framed={false} />
                    <span className="truncate" {...clippedTitle}>{mailHostLabel(c.mail_host)}</span>
                </div>
            ) : (
                <Dash />
            ),
    };
}

const phoneColumn: ContactColumn = {
    id: "phone",
    label: "Phone",
    width: "w-36",
    hideBelow: "xl",
    sortKey: "phone",
    sortAsc: true,
    cellClassName: "text-[12px] text-slate-600 font-mono",
    cell: ({ c }) =>
        c.phone ? (
            <div className="flex items-center gap-1.5 min-w-0">
                <PhoneIcon className="w-3 h-3 shrink-0 text-slate-400" />
                <span className="truncate" {...clippedTitle}>{c.phone}</span>
            </div>
        ) : (
            <Dash />
        ),
};

// Below sm the pill is its icon alone, so the column narrows to it and the
// label waits for the room, but never leaves the accessibility tree.
function statusHeader(label: string) {
    return (
        <>
            <span className="sr-only">{label}</span>
            <span aria-hidden className="hidden sm:inline">{label}</span>
        </>
    );
}

const statusColumn: ContactColumn = {
    id: "status",
    label: "Status",
    header: statusHeader("Status"),
    width: "w-12 sm:w-32",
    cell: ({ c }) => <StatusPill subscribed={c.subscribed} />,
};

const progressColumn: ContactColumn = {
    id: "progress",
    label: "Progress",
    header: statusHeader("Progress"),
    width: "w-12 sm:w-32",
    cell: ({ lead }) => <LeadStatusPill lead={lead} />,
};

const engagement = (
    id: "opened" | "clicked" | "replied",
    label: string,
    Icon: LucideIcon,
    header?: React.ReactNode,
): ContactColumn => ({
    id,
    label,
    header,
    width: "w-24",
    hideBelow: "lg",
    cell: ({ lead }) => (
        <EngagementValue
            n={lead?.[id] ?? 0}
            sent={(lead?.sent ?? 0) > 0}
            Icon={Icon}
            label={id}
            auto={id === "opened" && (lead?.machine_opened ?? 0) > 0}
        />
    ),
});

const currentStepColumn: ContactColumn = {
    id: "current_step",
    label: "Current step",
    width: "w-32",
    hideBelow: "xl",
    cell: ({ lead, processed }) =>
        lead?.current_step ? (
            <span
                title={lead.current_step}
                className={`inline-flex items-center h-5 px-1.5 rounded text-[11px] font-medium max-w-full ${
                    processed ? "bg-slate-100 text-slate-400" : "bg-sky-100 text-sky-700"
                }`}
            >
                <span className="truncate">{lead.current_step}</span>
            </span>
        ) : (
            <span className="text-[11px] text-slate-300">Not started</span>
        ),
};

const senderColumn: ContactColumn = {
    id: "sender",
    label: "Sender",
    header: (
        <InfoHeader
            label="Sender"
            title="The mailbox this lead's whole sequence sends from. It is picked when the first email goes out and every follow-up keeps it, so the contact always hears from one address."
            aria="How the sender is chosen"
        />
    ),
    width: "w-36",
    hideBelow: "2xl",
    cell: ({ lead }) =>
        lead?.sender ? (
            <span
                title={`Every step of this lead's sequence sends from ${lead.sender}`}
                className="block truncate text-[11.5px] text-slate-600"
            >
                {lead.sender}
            </span>
        ) : (
            <span className="text-[11px] text-slate-300">Not assigned</span>
        ),
};

const campaignsColumn: ContactColumn = {
    id: "campaigns",
    label: "Campaigns",
    width: "w-28",
    hideBelow: "lg",
    align: "right",
    sortKey: "campaign_count",
    cellClassName: "font-mono text-[12px] text-slate-600 tabular-nums",
    cell: ({ c }) => c.campaigns?.length ?? 0,
};

const dateCell = "font-mono text-[11px] text-slate-500 tabular-nums";

const addedColumn = (view: ViewName): ContactColumn => ({
    id: "created_at",
    label: "Added",
    width: view === "campaign_leads" ? "w-32" : "w-24",
    hideBelow: view === "campaign_leads" ? "2xl" : "md",
    align: "right",
    sortKey: "created_at",
    cellClassName: dateCell,
    cell: ({ c }) => shortDate(c.created_at),
});

const updatedColumn: ContactColumn = {
    id: "updated_at",
    label: "Updated",
    width: "w-24",
    hideBelow: "md",
    align: "right",
    sortKey: "updated_at",
    cellClassName: dateCell,
    cell: ({ c }) => shortDate(c.updated_at),
};

const lastActivityColumn: ContactColumn = {
    id: "last_activity",
    label: "Last activity",
    width: "w-32",
    hideBelow: "2xl",
    align: "right",
    cellClassName: dateCell,
    cell: ({ lead }) => shortDate(lead?.last_activity_at),
};

// A contact custom field as a column. Text, sortable, and hidden below md so
// the phone layout stays a name-and-status list.
export function customColumn(key: string): ContactColumn {
    return {
        id: customColumnId(key),
        label: key,
        width: "w-36",
        hideBelow: "md",
        sortKey: customColumnId(key) as SearchContactsSortBy,
        sortAsc: true,
        custom: key,
        cellClassName: "text-[12px] text-slate-600",
        cell: ({ c }) => {
            const v = c.custom_fields?.[key];
            return v ? <span className="block truncate" {...clippedTitle}>{v}</span> : <Dash />;
        },
    };
}

// Every built-in column a view can show, in its natural order. The chooser
// lists them in this order under "Available".
export function builtinColumns(view: ViewName): ContactColumn[] {
    if (view === "campaign_leads") {
        return [
            nameColumn,
            companyColumn(view),
            mailHostColumn(view),
            phoneColumn,
            progressColumn,
            engagement(
                "opened",
                "Opened",
                MailOpenIcon,
                <InfoHeader
                    label="Opened"
                    title="Opens rely on the mail client loading images. Clients that block images show no open even when the email was read. A click by the person always counts as an open."
                    aria="How opens are counted"
                />,
            ),
            engagement("clicked", "Clicked", MousePointerClickIcon),
            engagement("replied", "Replied", CornerUpLeftIcon),
            currentStepColumn,
            senderColumn,
            lastActivityColumn,
            addedColumn(view),
            updatedColumn,
        ];
    }
    return [nameColumn, companyColumn(view), mailHostColumn(view), phoneColumn, statusColumn, campaignsColumn, addedColumn(view), updatedColumn];
}

// The layout a member sees before choosing anything.
export const DEFAULT_COLUMNS: Record<ViewName, string[]> = {
    contacts: ["name", "company", "mail_host", "phone", "status", "campaigns", "created_at"],
    campaign_leads: ["name", "company", "progress", "opened", "clicked", "replied", "current_step", "sender", "last_activity"],
};

// The columns to render for a saved layout, and the ones the chooser can still
// add. A saved layout always names "name", so a layout of Name alone is
// `["name"]` and never the empty list, which means "the default". Unknown
// built-in ids (a column an older build offered) are dropped; a custom field
// is kept even when no contact currently carries it, so the column the member
// picked does not vanish because an import replaced the field's name.
export function resolveColumns(
    view: ViewName,
    saved: string[] | undefined,
    customKeys: string[],
): { visible: ContactColumn[]; available: ContactColumn[] } {
    const catalogue = builtinColumns(view);
    const byId = new Map(catalogue.map((c) => [c.id, c]));
    const chosen = saved && saved.length > 0 ? saved : DEFAULT_COLUMNS[view];
    const visible: ContactColumn[] = [];
    const seen = new Set<string>();
    for (const id of chosen) {
        if (seen.has(id)) continue;
        const col = byId.get(id) ?? (isCustomColumnId(id) ? customColumn(customKeyOf(id)) : undefined);
        if (!col || col.locked) continue;
        visible.push(col);
        seen.add(id);
    }
    visible.unshift(nameColumn);
    const available = [
        ...catalogue.filter((c) => !c.locked && !seen.has(c.id)),
        ...customKeys.filter((k) => !seen.has(customColumnId(k))).map(customColumn),
    ];
    return { visible, available };
}

export interface SortOption {
    key: SearchContactsSortBy;
    label: string;
    // Whether picking it starts ascending, as a header click on the same
    // column would (text does, dates and counts do not).
    asc: boolean;
}

// The sort choices the toolbar menu offers, beyond what a header click reaches.
export function sortOptions(view: ViewName): SortOption[] {
    const base: SortOption[] = [
        { key: "created_at", label: "Date added", asc: false },
        { key: "updated_at", label: "Last updated", asc: false },
        { key: "first_name", label: "First name", asc: true },
        { key: "last_name", label: "Last name", asc: true },
        { key: "email", label: "Email", asc: true },
        { key: "company", label: "Company", asc: true },
        { key: "phone", label: "Phone", asc: true },
        { key: "mail_host", label: "Email provider", asc: true },
    ];
    if (view === "contacts") base.push({ key: "campaign_count", label: "Campaigns", asc: false });
    return base;
}
