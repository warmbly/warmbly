// Filters for the conversation list: a compact popover under the filter
// button, applied on the spot, and a row of chips under the search box that
// shows what is active and removes any of it with one click.
//
// The scope already fixes some of these (the Unread view sets unseen, a
// mailbox view sets accountIds, a label view sets categoryIds), so a control
// the scope owns is not offered: the popover only shows what the user can add
// on top, and the chips only show what the user added. `base` is what the
// scope alone would query; anything different from it is the user's.

import React from "react";
import { ListFilterIcon, XIcon } from "lucide-react";
import { DatePicker } from "@/components/ui/DatePicker";
import { TextInput } from "@/components/ui/field";
import {
  PopoverMenu,
  PopoverMenuContent,
  PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { useAppStore } from "@/stores";
import { useUserProfile } from "@/hooks/context/user";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import { cn } from "@/lib/utils";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";

type Setter = React.Dispatch<React.SetStateAction<UniboxSearchParams>>;

// ── Dates ─────────────────────────────────────────────────────────

type DatePreset = "any" | "today" | "week" | "month" | "custom";

function startOfDay(d = new Date()): Date {
  const x = new Date(d);
  x.setHours(0, 0, 0, 0);
  return x;
}
function daysAgo(n: number): Date {
  const d = startOfDay();
  d.setDate(d.getDate() - n);
  return d;
}
function sameDay(a?: Date, b?: Date): boolean {
  return !!a && !!b && startOfDay(a).getTime() === startOfDay(b).getTime();
}
function presetOf(since?: Date, until?: Date): DatePreset {
  if (!since && !until) return "any";
  if (until) return "custom";
  if (sameDay(since, daysAgo(0))) return "today";
  if (sameDay(since, daysAgo(6))) return "week";
  if (sameDay(since, daysAgo(29))) return "month";
  return "custom";
}
function sinceForPreset(p: DatePreset): Date | undefined {
  switch (p) {
    case "today":
      return daysAgo(0);
    case "week":
      return daysAgo(6);
    case "month":
      return daysAgo(29);
    default:
      return undefined;
  }
}
function toIso(d?: Date): string {
  if (!d) return "";
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}
function fromIso(s: string): Date | undefined {
  return s ? new Date(`${s}T00:00:00`) : undefined;
}
function shortDate(d: Date): string {
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

// ── What the user added on top of the scope ────────────────────────

function sameIds(a?: string[], b?: string[]): boolean {
  const x = [...(a ?? [])].sort().join(",");
  const y = [...(b ?? [])].sort().join(",");
  return x === y;
}

export interface UserFilters {
  unseen?: boolean;
  since?: Date;
  until?: Date;
  from?: string;
  categoryIds?: string[];
  accountIds?: string[];
  tagId?: string;
  oldest: boolean;
}

/** The fields where `params` departs from what the scope alone would query. */
export function userFilters(
  params: UniboxSearchParams,
  base: UniboxSearchParams,
): UserFilters {
  const out: UserFilters = { oldest: params.sortBy === "oldest" };
  if (params.unseen !== base.unseen) out.unseen = params.unseen;
  if (!sameDay(params.since, base.since)) out.since = params.since;
  if (!sameDay(params.until, base.until)) out.until = params.until;
  if ((params.from ?? "") !== (base.from ?? "")) out.from = params.from;
  if (!sameIds(params.categoryIds, base.categoryIds)) out.categoryIds = params.categoryIds;
  if (!sameIds(params.accountIds, base.accountIds)) {
    out.accountIds = params.accountIds;
    out.tagId = params.tagId;
  }
  return out;
}

export function countUserFilters(
  params: UniboxSearchParams,
  base: UniboxSearchParams,
): number {
  const u = userFilters(params, base);
  let n = 0;
  if (u.unseen !== undefined) n++;
  if (u.since || u.until) n++;
  if (u.from) n++;
  if (u.categoryIds && u.categoryIds.length > 0) n++;
  if (u.accountIds && u.accountIds.length > 0) n++;
  return n;
}

// ── The button and its popover ─────────────────────────────────────

export function UniboxFilterButton({
  params,
  base,
  setParams,
  open,
  onOpenChange,
}: {
  params: UniboxSearchParams;
  base: UniboxSearchParams;
  setParams: Setter;
  open: boolean;
  onOpenChange: (o: boolean) => void;
}) {
  const active = countUserFilters(params, base);
  return (
    <PopoverMenu align="end" open={open} onOpenChange={onOpenChange}>
      <PopoverMenuTrigger asChild>
        <button
          type="button"
          className={cn(
            "relative size-7 rounded-md inline-flex items-center justify-center transition-colors shrink-0",
            open || active > 0
              ? "text-sky-700 bg-sky-50 hover:bg-sky-100"
              : "text-slate-500 hover:text-slate-900 hover:bg-slate-100",
          )}
          aria-label={active > 0 ? `Filters (${active} active)` : "Filters"}
        >
          <ListFilterIcon className="w-4 h-4" />
          {active > 0 && (
            <span
              aria-hidden
              className="absolute top-1 right-1 size-1.5 rounded-full bg-sky-500 ring-2 ring-white"
            />
          )}
        </button>
      </PopoverMenuTrigger>
      <PopoverMenuContent minWidth={300} className="p-0 w-[300px]">
        <FilterPanel params={params} base={base} setParams={setParams} />
      </PopoverMenuContent>
    </PopoverMenu>
  );
}

function FilterPanel({
  params,
  base,
  setParams,
}: {
  params: UniboxSearchParams;
  base: UniboxSearchParams;
  setParams: Setter;
}) {
  const accounts = useAppStore((s) => s.emails);
  const { user } = useUserProfile();
  const categories = React.useMemo(
    () => [...(user.categories ?? [])].sort((a, b) => a.position - b.position),
    [user.categories],
  );
  const tags = user.tags ?? [];
  const active = countUserFilters(params, base);

  // What the scope owns is not offered here.
  const canReadState = base.unseen === undefined;
  const canDates = !base.since && !base.until;
  const canLabels = !base.categoryIds;
  const canMailboxes = !base.accountIds && accounts.length > 1;

  // The sender box commits on a pause, not on every keystroke: the value is
  // part of the query key, so a raw binding would fire a request per letter.
  const [from, setFrom] = React.useState(params.from ?? "");
  const debouncedFrom = useDebouncedValue(from, 300);
  React.useEffect(() => {
    const next = debouncedFrom.trim() || undefined;
    setParams((s) => (s.from === next ? s : { ...s, from: next }));
  }, [debouncedFrom, setParams]);
  React.useEffect(() => {
    setFrom(params.from ?? "");
  }, [params.from]);

  const preset = presetOf(params.since, params.until);
  const [customOpen, setCustomOpen] = React.useState(preset === "custom");
  const showCustom = customOpen || preset === "custom";

  const pickPreset = (p: DatePreset) => {
    if (p === "custom") {
      setCustomOpen(true);
      return;
    }
    setCustomOpen(false);
    setParams((s) => ({ ...s, since: sinceForPreset(p), until: undefined }));
  };

  const selectedCategories = new Set(params.categoryIds ?? []);
  const toggleCategory = (id: string) =>
    setParams((s) => {
      const next = new Set(s.categoryIds ?? []);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return { ...s, categoryIds: next.size ? Array.from(next) : undefined };
    });

  const selectedAccounts = new Set(params.accountIds ?? []);
  const toggleAccount = (id: string) =>
    setParams((s) => {
      const next = new Set(s.accountIds ?? []);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      // A hand-picked set is no longer "the tag".
      return { ...s, accountIds: next.size ? Array.from(next) : undefined, tagId: undefined };
    });
  const pickTag = (tagId: string) =>
    setParams((s) => {
      if (s.tagId === tagId) return { ...s, accountIds: undefined, tagId: undefined };
      const ids = accounts.filter((a) => (a.tags ?? []).includes(tagId)).map((a) => a.id);
      return { ...s, accountIds: ids, tagId };
    });

  const clear = () => {
    setCustomOpen(false);
    setFrom("");
    setParams((s) => ({ ...base, sortBy: s.sortBy }));
  };

  return (
    <div className="text-[12.5px]">
      <div className="h-10 px-3.5 flex items-center border-b border-slate-100">
        <span className="font-semibold text-slate-900">Filters</span>
        {active > 0 && (
          <button
            type="button"
            onClick={clear}
            className="ml-auto h-6 px-1.5 -mr-1.5 rounded text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
          >
            Clear all
          </button>
        )}
      </div>

      <div className="px-3.5 py-2.5 space-y-3">
        {canReadState && (
          <Row label="Show">
            <Segmented<boolean | undefined>
              value={params.unseen}
              onChange={(v) => setParams((s) => ({ ...s, unseen: v }))}
              options={[
                { id: undefined, label: "All" },
                { id: true, label: "Unread" },
                { id: false, label: "Read" },
              ]}
            />
          </Row>
        )}

        {canDates && (
          <Row label="Date">
            <div className="space-y-1.5">
              <Segmented<DatePreset>
                value={showCustom ? "custom" : preset}
                onChange={pickPreset}
                options={[
                  { id: "any", label: "Any" },
                  { id: "today", label: "Today" },
                  { id: "week", label: "7d" },
                  { id: "month", label: "30d" },
                  { id: "custom", label: "Custom" },
                ]}
              />
              {showCustom && (
                <div className="flex items-center gap-1.5">
                  <DatePicker
                    value={toIso(params.since)}
                    onChange={(v) => setParams((s) => ({ ...s, since: fromIso(v) }))}
                    placeholder="From"
                    className="flex-1 min-w-0"
                  />
                  <span className="text-slate-300">to</span>
                  <DatePicker
                    value={toIso(params.until)}
                    onChange={(v) => setParams((s) => ({ ...s, until: fromIso(v) }))}
                    placeholder="Until"
                    className="flex-1 min-w-0"
                  />
                </div>
              )}
            </div>
          </Row>
        )}

        <Row label="From">
          <TextInput
            value={from}
            onChange={setFrom}
            placeholder="Name or address"
            className="w-full"
          />
        </Row>

        {canLabels && categories.length > 0 && (
          <Row label="Labels" top>
            <div className="flex flex-wrap gap-1">
              {categories.map((c) => (
                <Chip
                  key={c.id}
                  label={c.title}
                  color={c.color}
                  selected={selectedCategories.has(c.id)}
                  onClick={() => toggleCategory(c.id)}
                />
              ))}
            </div>
          </Row>
        )}

        {canMailboxes && (
          <Row label="Mailbox" top>
            <div className="space-y-1.5">
              {tags.length > 0 && (
                <div className="flex flex-wrap gap-1">
                  {tags.map((t) => (
                    <Chip
                      key={t.id}
                      label={t.title}
                      color={t.color}
                      selected={params.tagId === t.id}
                      onClick={() => pickTag(t.id)}
                    />
                  ))}
                </div>
              )}
              <div className="max-h-40 overflow-y-auto -mx-1">
                {accounts.map((a) => {
                  const on = selectedAccounts.has(a.id);
                  return (
                    <button
                      key={a.id}
                      type="button"
                      onClick={() => toggleAccount(a.id)}
                      className="w-full h-7 px-1 rounded-md flex items-center gap-2 text-left hover:bg-slate-50 transition-colors"
                    >
                      <span
                        className={cn(
                          "size-3.5 rounded border flex items-center justify-center shrink-0 transition-colors",
                          on ? "bg-sky-600 border-sky-600" : "bg-white border-slate-300",
                        )}
                      >
                        {on && <CheckMark />}
                      </span>
                      <span className={cn("truncate text-[12px]", on ? "text-slate-900" : "text-slate-600")}>
                        {a.email}
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>
          </Row>
        )}

        <Row label="Sort">
          <Segmented<"newest" | "oldest">
            value={params.sortBy ?? "newest"}
            onChange={(v) => setParams((s) => ({ ...s, sortBy: v }))}
            options={[
              { id: "newest", label: "Newest first" },
              { id: "oldest", label: "Oldest first" },
            ]}
          />
        </Row>
      </div>
    </div>
  );
}

// ── Active chips under the search box ──────────────────────────────

export function UniboxFilterChips({
  params,
  base,
  setParams,
  onOpen,
}: {
  params: UniboxSearchParams;
  base: UniboxSearchParams;
  setParams: Setter;
  onOpen: () => void;
}) {
  const accounts = useAppStore((s) => s.emails);
  const { user } = useUserProfile();
  const u = userFilters(params, base);

  const chips: { key: string; label: string; remove: () => void }[] = [];

  if (u.unseen !== undefined) {
    chips.push({
      key: "unseen",
      label: u.unseen ? "Unread" : "Read",
      remove: () => setParams((s) => ({ ...s, unseen: base.unseen })),
    });
  }
  if (u.since || u.until) {
    const p = presetOf(u.since, u.until);
    const label =
      p === "today"
        ? "Today"
        : p === "week"
          ? "Last 7 days"
          : p === "month"
            ? "Last 30 days"
            : u.since && u.until
              ? `${shortDate(u.since)} to ${shortDate(u.until)}`
              : u.since
                ? `Since ${shortDate(u.since)}`
                : `Until ${shortDate(u.until!)}`;
    chips.push({
      key: "date",
      label,
      remove: () => setParams((s) => ({ ...s, since: base.since, until: base.until })),
    });
  }
  if (u.from) {
    chips.push({
      key: "from",
      label: `From ${u.from}`,
      remove: () => setParams((s) => ({ ...s, from: base.from })),
    });
  }
  if (u.categoryIds && u.categoryIds.length > 0) {
    const titles = u.categoryIds
      .map((id) => (user.categories ?? []).find((c) => c.id === id)?.title)
      .filter((t): t is string => !!t);
    chips.push({
      key: "labels",
      label: titles.length <= 2 ? titles.join(", ") : `${titles.slice(0, 2).join(", ")} +${titles.length - 2}`,
      remove: () => setParams((s) => ({ ...s, categoryIds: base.categoryIds })),
    });
  }
  if (u.accountIds && u.accountIds.length > 0) {
    const tag = u.tagId ? (user.tags ?? []).find((t) => t.id === u.tagId) : undefined;
    const label = tag
      ? `Tag ${tag.title}`
      : u.accountIds.length === 1
        ? (accounts.find((a) => a.id === u.accountIds![0])?.email ?? "1 mailbox")
        : `${u.accountIds.length} mailboxes`;
    chips.push({
      key: "accounts",
      label,
      remove: () =>
        setParams((s) => ({ ...s, accountIds: base.accountIds, tagId: base.tagId })),
    });
  }

  if (chips.length === 0) return null;

  return (
    <div className="flex flex-wrap items-center gap-1 pt-1.5">
      {chips.map((c) => (
        <span
          key={c.key}
          className="inline-flex items-center h-6 pl-2 pr-0.5 rounded-md bg-sky-50 text-sky-800 text-[11.5px] font-medium max-w-full"
        >
          <button
            type="button"
            onClick={onOpen}
            className="truncate max-w-[180px] hover:text-sky-900"
            title="Edit filters"
          >
            {c.label}
          </button>
          <button
            type="button"
            onClick={c.remove}
            aria-label={`Remove filter ${c.label}`}
            className="ml-0.5 size-5 rounded inline-flex items-center justify-center text-sky-500 hover:text-sky-900 hover:bg-sky-100 transition-colors"
          >
            <XIcon className="w-3 h-3" />
          </button>
        </span>
      ))}
      {chips.length > 1 && (
        <button
          type="button"
          onClick={() => setParams((s) => ({ ...base, sortBy: s.sortBy }))}
          className="h-6 px-1.5 rounded text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
        >
          Clear
        </button>
      )}
    </div>
  );
}

// ── Bits ───────────────────────────────────────────────────────────

function Row({
  label,
  top,
  children,
}: {
  label: string;
  top?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div className={cn("flex gap-3", top ? "items-start" : "items-center")}>
      <span className={cn("w-14 shrink-0 text-[11.5px] text-slate-400", top && "pt-1")}>
        {label}
      </span>
      <div className="flex-1 min-w-0">{children}</div>
    </div>
  );
}

function Segmented<T extends string | boolean | undefined>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { id: T; label: string }[];
}) {
  return (
    <div className="inline-flex w-full items-stretch rounded-md bg-slate-100 p-0.5">
      {options.map((o) => {
        const on = value === o.id;
        return (
          <button
            key={String(o.id)}
            type="button"
            onClick={() => onChange(o.id)}
            aria-pressed={on}
            className={cn(
              "flex-1 h-6 px-2 rounded text-[11.5px] font-medium transition-colors truncate",
              on
                ? "bg-white text-slate-900 shadow-[0_1px_2px_rgba(15,23,42,0.08)]"
                : "text-slate-500 hover:text-slate-800",
            )}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}

function Chip({
  label,
  color,
  selected,
  onClick,
}: {
  label: string;
  color?: string;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={selected}
      className={cn(
        "h-6 pl-1.5 pr-2 rounded-md inline-flex items-center gap-1.5 text-[11.5px] font-medium border transition-colors max-w-full",
        selected
          ? "bg-sky-50 border-sky-200 text-sky-800"
          : "bg-white border-slate-200 text-slate-600 hover:border-slate-300 hover:text-slate-900",
      )}
    >
      <span
        aria-hidden
        className="size-2 rounded-full shrink-0"
        style={{ backgroundColor: color || "#94a3b8" }}
      />
      <span className="truncate">{label}</span>
    </button>
  );
}

function CheckMark() {
  return (
    <svg width="9" height="9" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="3.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M5 12l5 5L20 7" />
    </svg>
  );
}
