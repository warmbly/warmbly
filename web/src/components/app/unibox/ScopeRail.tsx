// Left-rail navigator for the unibox.
//
// Reads /unibox/overview so every count is server-truth. One visual
// language for every row: a quiet icon, the label, a bare tabular count.
//
//   Compose
//   All mail / Inbox / Unread / Awaiting reply / Agent drafts / Snoozed
//   Drafts / Sent / Scheduled / Archive / Spam / Trash
//   Mailboxes / Labels / Tags   (collapsible, searchable past 8 items)
//
// Today and This week are not rows: the filter sheet's date range covers
// them, and the rail is for the places mail lives, not for every slice of it.

import React from "react";
import {
  ArchiveIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  FileTextIcon,
  InboxIcon,
  LayersIcon,
  MailIcon,
  MailOpenIcon,
  MoonIcon,
  MoreHorizontalIcon,
  OctagonAlertIcon,
  PenLineIcon,
  ReplyIcon,
  SearchIcon,
  SendIcon,
  SparklesIcon,
  Trash2Icon,
  ClockIcon,
} from "lucide-react";
import useUniboxOverview from "@/lib/api/hooks/app/unibox/useUniboxOverview";
import useMarkSeen from "@/lib/api/hooks/app/unibox/useMarkSeen";
import ShortcutTooltip from "@/components/ui/shortcut-tooltip";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import ComposeDraftsItem from "@/components/app/unibox/compose/ComposeDraftsItem";
import { useComposeStore } from "@/hooks/useComposeStore";
import { cn } from "@/lib/utils";
import { DitherMeter } from "@/components/ui/dither";
import {
  PopoverMenu,
  PopoverMenuContent,
  PopoverMenuItem,
  PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import type { UniboxFolder } from "@/lib/api/models/app/unibox/UniboxSearch";

export type UniboxScope =
  | { kind: "all" }
  | { kind: "unread" }
  | { kind: "today" }
  | { kind: "week" }
  | { kind: "awaiting" }
  | { kind: "agent_drafts" }
  | { kind: "snoozed" }
  | { kind: "scheduled" }
  | { kind: "folder"; folder: UniboxFolder }
  | { kind: "mailbox"; mailboxId: string }
  | { kind: "tag"; tagId: string }
  | { kind: "category"; categoryId: string };

export function scopeKey(s: UniboxScope): string {
  switch (s.kind) {
    case "folder":
      return `folder:${s.folder}`;
    case "mailbox":
      return `mailbox:${s.mailboxId}`;
    case "tag":
      return `tag:${s.tagId}`;
    case "category":
      return `category:${s.categoryId}`;
    default:
      return s.kind;
  }
}

const ICON = "w-[15px] h-[15px]";

const MAIL_FOLDERS: {
  folder: UniboxFolder;
  label: string;
  icon: React.ReactNode;
}[] = [
  { folder: "drafts", label: "Drafts", icon: <FileTextIcon className={ICON} /> },
  { folder: "sent", label: "Sent", icon: <SendIcon className={ICON} /> },
  { folder: "archive", label: "Archive", icon: <ArchiveIcon className={ICON} /> },
  { folder: "spam", label: "Spam", icon: <OctagonAlertIcon className={ICON} /> },
  { folder: "trash", label: "Trash", icon: <Trash2Icon className={ICON} /> },
];

const COLLAPSE_THRESHOLD = 8;
const COLLAPSED_VISIBLE = 6;

interface ScopeRailProps {
  scope: UniboxScope;
  onChange: (s: UniboxScope) => void;
}

export function ScopeRail({ scope, onChange }: ScopeRailProps) {
  const overview = useUniboxOverview();
  const data = overview.data;
  const markSeen = useMarkSeen();

  const active = scopeKey(scope);
  const folderCounts = React.useMemo(() => {
    const m = new Map<string, { unread: number; total: number }>();
    for (const f of data?.folders ?? []) {
      m.set(f.folder, { unread: f.unread, total: f.total });
    }
    return m;
  }, [data?.folders]);

  const folderRow = (
    folder: UniboxFolder,
    label: string,
    icon: React.ReactNode,
  ) => {
    const counts = folderCounts.get(folder);
    // Drafts reads better as a total; everywhere else the number is unread.
    const count = folder === "drafts" ? counts?.total : counts?.unread;
    return (
      <FolderItem
        key={folder}
        icon={icon}
        label={label}
        count={count || undefined}
        accent={folder !== "drafts" && !!count}
        active={active === `folder:${folder}`}
        onOpen={() => onChange({ kind: "folder", folder })}
        onMarkAllRead={() => markSeen.mutate({ folder, seen: true })}
      />
    );
  };

  return (
    <nav className="h-full w-full bg-white border-r border-slate-200 overflow-y-auto py-3">
      <div className="px-3 pb-3">
        <ShortcutTooltip label="New email" combo="n" side="bottom">
          <button
            type="button"
            onClick={() => useComposeStore.getState().openCompose()}
            className="w-full h-8 rounded-md bg-sky-600 text-white text-[12.5px] font-medium inline-flex items-center justify-center gap-1.5 hover:bg-sky-700 active:bg-sky-800 transition-colors"
          >
            <PenLineIcon className="w-3.5 h-3.5" />
            Compose
          </button>
        </ShortcutTooltip>
        <ComposeDraftsItem />
      </div>

      <Group>
        <Item
          icon={<LayersIcon className={ICON} />}
          label="All mail"
          count={data?.total}
          active={active === "all"}
          onClick={() => onChange({ kind: "all" })}
        />
        {folderRow("inbox", "Inbox", <InboxIcon className={ICON} />)}
        <Item
          icon={<MailIcon className={ICON} />}
          label="Unread"
          count={data?.unread}
          accent={!!data?.unread}
          active={active === "unread"}
          onClick={() => onChange({ kind: "unread" })}
        />
        <Item
          icon={<ReplyIcon className={ICON} />}
          label="Awaiting reply"
          count={data?.awaiting_reply}
          accent={!!data?.awaiting_reply}
          active={active === "awaiting"}
          onClick={() => onChange({ kind: "awaiting" })}
        />
        <Item
          icon={<SparklesIcon className={ICON} />}
          label="Agent drafts"
          count={data?.awaiting_agent_draft}
          accent={!!data?.awaiting_agent_draft}
          active={active === "agent_drafts"}
          onClick={() => onChange({ kind: "agent_drafts" })}
        />
        <Item
          icon={<MoonIcon className={ICON} />}
          label="Snoozed"
          count={data?.snoozed}
          active={active === "snoozed"}
          onClick={() => onChange({ kind: "snoozed" })}
        />
      </Group>

      <Group>
        {MAIL_FOLDERS.slice(0, 2).map((f) => folderRow(f.folder, f.label, f.icon))}
        <Item
          icon={<ClockIcon className={ICON} />}
          label="Scheduled"
          count={data?.scheduled_pending}
          accent={!!data?.scheduled_pending}
          active={active === "scheduled"}
          onClick={() => onChange({ kind: "scheduled" })}
        />
        {MAIL_FOLDERS.slice(2).map((f) => folderRow(f.folder, f.label, f.icon))}
        {/* Cap meter, only once the user is materially through the allowance,
            so the rail stays calm for the 99% case. */}
        {data &&
          data.scheduled_pending_max > 0 &&
          data.scheduled_pending / data.scheduled_pending_max >= 0.7 && (
            <div className="px-2 pt-1.5 pb-1">
              <ScheduledMeter
                used={data.scheduled_pending}
                cap={data.scheduled_pending_max}
              />
            </div>
          )}
      </Group>

      <CollapsibleSection
        label="Mailboxes"
        items={data?.mailboxes ?? []}
        emptyText={overview.isPending ? "Loading…" : "No mailboxes connected."}
        searchPlaceholder="Filter mailboxes"
        getSearchKey={(m) => `${m.email} ${m.name}`}
        renderItem={(m) => (
          <Item
            key={m.id}
            icon={<MailOpenIcon className={ICON} />}
            label={m.email}
            count={m.unread || undefined}
            accent={m.unread > 0}
            active={active === `mailbox:${m.id}`}
            onClick={() => onChange({ kind: "mailbox", mailboxId: m.id })}
          />
        )}
      />

      {data && data.categories && data.categories.length > 0 && (
        <CollapsibleSection
          label="Labels"
          items={data.categories}
          emptyText="No labels yet."
          searchPlaceholder="Filter labels"
          getSearchKey={(c) => c.title}
          renderItem={(c) => (
            <Item
              key={c.id}
              icon={<Dot color={c.color} />}
              label={c.title}
              count={c.unread || c.total || undefined}
              accent={c.unread > 0}
              active={active === `category:${c.id}`}
              onClick={() => onChange({ kind: "category", categoryId: c.id })}
            />
          )}
        />
      )}

      {data && data.tags.length > 0 && (
        <CollapsibleSection
          label="Tags"
          items={data.tags}
          emptyText="No tags yet."
          searchPlaceholder="Filter tags"
          getSearchKey={(t) => t.title}
          renderItem={(t) => (
            <Item
              key={t.id}
              icon={<Dot color={t.color} />}
              label={t.title}
              count={t.unread || t.total || undefined}
              accent={t.unread > 0}
              active={active === `tag:${t.id}`}
              onClick={() => onChange({ kind: "tag", tagId: t.id })}
            />
          )}
        />
      )}
    </nav>
  );
}

function Dot({ color }: { color?: string }) {
  return (
    <span className="inline-flex w-[15px] h-[15px] items-center justify-center">
      <span
        aria-hidden
        className="block size-2 rounded-full"
        style={{ backgroundColor: color || "#94a3b8" }}
      />
    </span>
  );
}

function Group({ children }: { children: React.ReactNode }) {
  return <div className="px-2 pb-3 space-y-px">{children}</div>;
}

function CollapsibleSection<T extends { id: string }>({
  label,
  items,
  emptyText,
  searchPlaceholder,
  getSearchKey,
  renderItem,
}: {
  label: string;
  items: T[];
  emptyText: string;
  searchPlaceholder: string;
  getSearchKey: (item: T) => string;
  renderItem: (item: T) => React.ReactNode;
}) {
  const [search, setSearch] = React.useState("");
  const [expanded, setExpanded] = React.useState(false);
  const [sectionOpen, setSectionOpen] = React.useState(true);

  const filtered = React.useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return items;
    return items.filter((it) => getSearchKey(it).toLowerCase().includes(q));
  }, [items, search, getSearchKey]);

  const showSearch = items.length > COLLAPSE_THRESHOLD;
  const showCollapse = filtered.length > COLLAPSE_THRESHOLD;
  const visible =
    showCollapse && !expanded ? filtered.slice(0, COLLAPSED_VISIBLE) : filtered;
  const hidden = filtered.length - visible.length;

  return (
    <div className="pb-2">
      <button
        type="button"
        onClick={() => setSectionOpen((v) => !v)}
        className="group/section w-full h-7 px-4 flex items-center gap-1.5 text-left"
        aria-expanded={sectionOpen}
      >
        <span className="text-[10.5px] uppercase tracking-[0.12em] text-slate-400 font-medium group-hover/section:text-slate-600 transition-colors">
          {label}
        </span>
        {sectionOpen ? (
          <ChevronDownIcon className="w-3 h-3 text-slate-300 group-hover/section:text-slate-500 transition-colors" />
        ) : (
          <ChevronRightIcon className="w-3 h-3 text-slate-300 group-hover/section:text-slate-500 transition-colors" />
        )}
        <span className="ml-auto text-[10.5px] text-slate-300 tabular-nums">
          {items.length}
        </span>
      </button>

      {sectionOpen && (
        <div className="px-2">
          {showSearch && (
            <div className="flex items-center gap-1.5 px-2 h-7 mb-1 rounded-md border border-slate-200 bg-white focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 transition-colors">
              <SearchIcon className="w-3 h-3 text-slate-400 shrink-0" />
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={searchPlaceholder}
                className="flex-1 min-w-0 h-5 bg-transparent text-[11.5px] text-slate-900 placeholder:text-slate-400 outline-none"
              />
              {search && (
                <button
                  type="button"
                  onClick={() => setSearch("")}
                  className="text-[10px] text-slate-400 hover:text-slate-600 shrink-0"
                  aria-label="Clear filter"
                >
                  clear
                </button>
              )}
            </div>
          )}

          {items.length === 0 ? (
            <div className="px-2 py-1.5 text-[11.5px] text-slate-400">
              {emptyText}
            </div>
          ) : filtered.length === 0 ? (
            <div className="px-2 py-1.5 text-[11.5px] text-slate-400">
              No matches.
            </div>
          ) : (
            <div className="space-y-px">{visible.map(renderItem)}</div>
          )}

          {hidden > 0 && (
            <button
              type="button"
              onClick={() => setExpanded(true)}
              className="w-full h-7 px-2 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-50 transition-colors text-left"
            >
              Show all ({hidden} more)
            </button>
          )}
          {expanded && filtered.length > COLLAPSED_VISIBLE && (
            <button
              type="button"
              onClick={() => setExpanded(false)}
              className="w-full h-7 px-2 rounded-md text-[11.5px] text-slate-400 hover:text-slate-700 hover:bg-slate-50 transition-colors text-left"
            >
              Show less
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// ScheduledMeter: slim usage bar for the pending-send cap. Hidden until 70%
// so it never clutters the rail; amber and rose as the cap gets close.
function ScheduledMeter({ used, cap }: { used: number; cap: number }) {
  const ratio = Math.min(1, used / cap);
  const tone = ratio >= 0.95 ? "rose" : ratio >= 0.85 ? "amber" : "sky";

  const textClasses =
    tone === "rose"
      ? "text-rose-700"
      : tone === "amber"
        ? "text-amber-700"
        : "text-slate-500";

  return (
    <div className="px-1">
      <div
        className={cn(
          "flex items-center justify-between text-[10px] mb-1",
          textClasses,
        )}
      >
        <span className="uppercase tracking-[0.12em] font-medium">Queue</span>
        <span className="tabular-nums">
          {used}/{cap}
        </span>
      </div>
      <DitherMeter frac={ratio} tone={tone} height={4} />
      {ratio >= 0.95 && (
        <p className="mt-1 text-[10px] text-rose-600 leading-snug">
          Near the limit. Cancel a few sends to free up space.
        </p>
      )}
    </div>
  );
}

const ROW =
  "w-full h-7 pl-2 pr-2 rounded-md flex items-center gap-2.5 transition-colors text-left";
const ROW_ACTIVE = "bg-slate-100 text-slate-900";
const ROW_IDLE = "text-slate-600 hover:bg-slate-50 hover:text-slate-900";

function Count({ value, accent, active }: { value: number; accent?: boolean; active?: boolean }) {
  return (
    <span
      className={cn(
        "shrink-0 tabular-nums text-[11px]",
        accent && !active
          ? "text-sky-700 font-semibold"
          : active
            ? "text-slate-600"
            : "text-slate-400",
      )}
    >
      <AnimatedNumber value={value} duration={0.4} />
    </span>
  );
}

// FolderItem: a mail folder row with a three-dot menu on the right. The row
// is a div, not a button, because the menu trigger nests inside it and nested
// buttons are invalid HTML; the trigger stops propagation so opening the menu
// never also switches folders.
function FolderItem({
  icon,
  label,
  count,
  accent,
  active,
  onOpen,
  onMarkAllRead,
}: {
  icon: React.ReactNode;
  label: string;
  count?: number;
  accent?: boolean;
  active?: boolean;
  onOpen: () => void;
  onMarkAllRead: () => void;
}) {
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        // Only the row itself activates: keydown from the nested menu trigger
        // bubbles here, and Enter on the three-dot button must open its menu.
        if (e.target !== e.currentTarget) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      className={cn("group/folder relative cursor-pointer pr-7 md:pr-2", ROW, active ? ROW_ACTIVE : ROW_IDLE)}
      title={label}
    >
      <span className={cn("shrink-0", active ? "text-slate-800" : "text-slate-400 group-hover/folder:text-slate-600")}>
        {icon}
      </span>
      <span className={cn("truncate min-w-0 flex-1 text-[12.5px]", active && "font-medium")}>
        {label}
      </span>
      {count !== undefined && count !== null && (
        <span className="md:group-hover/folder:invisible md:group-focus-within/folder:invisible">
          <Count value={count} accent={accent} active={active} />
        </span>
      )}
      <PopoverMenu align="end">
        {/* asChild: the trigger's own onClick already stops propagation, so
            opening the menu never also fires the row's onOpen. */}
        <PopoverMenuTrigger asChild>
          <button
            type="button"
            aria-label={`${label} folder actions`}
            className="absolute right-1 top-1 size-5 rounded inline-flex items-center justify-center text-slate-500 hover:text-slate-900 hover:bg-slate-200/70 transition-colors opacity-100 md:opacity-0 md:group-hover/folder:opacity-100 md:group-focus-within/folder:opacity-100"
          >
            <MoreHorizontalIcon className="w-3.5 h-3.5" />
          </button>
        </PopoverMenuTrigger>
        <PopoverMenuContent>
          <PopoverMenuItem
            icon={<MailOpenIcon className="w-3 h-3" />}
            onSelect={onMarkAllRead}
          >
            Mark all as read
          </PopoverMenuItem>
        </PopoverMenuContent>
      </PopoverMenu>
    </div>
  );
}

function Item({
  icon,
  label,
  count,
  accent,
  active,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  count?: number;
  accent?: boolean;
  active?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn("group/item", ROW, active ? ROW_ACTIVE : ROW_IDLE)}
      title={label}
    >
      <span className={cn("shrink-0", active ? "text-slate-800" : "text-slate-400 group-hover/item:text-slate-600")}>
        {icon}
      </span>
      <span className={cn("truncate min-w-0 flex-1 text-[12.5px]", active && "font-medium")}>
        {label}
      </span>
      {count !== undefined && count !== null && (
        <Count value={count} accent={accent} active={active} />
      )}
    </button>
  );
}
