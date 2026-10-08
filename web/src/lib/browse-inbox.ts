import { z } from "zod";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";

export const inboxSearchSchema = z.string();
export const inboxBooleanSchema = z.boolean();
export const inboxNullableIdSchema = z.string().min(1).nullable();
export const inboxHistoryTabSchema = z.enum(["all", "sent"]);
export const inboxContactSortSchema = z.enum(["updated_at", "first_name", "email"]);
export const inboxCategorySchema = z.object({ id: z.string().min(1), title: z.string(), color: z.string() }).strict().nullable();

const idsSchema = z.array(z.string().min(1)).transform((ids) => [...new Set(ids)]);
const dateSchema = z.union([z.date(), z.iso.datetime({ offset: true }).transform((value) => new Date(value))]).refine((value) => Number.isFinite(value.getTime()));
const filtersSchema = z.object({
  from: z.string().optional(),
  unseen: z.boolean().optional(),
  since: dateSchema.optional(),
  until: dateSchema.optional(),
  datePreset: z.enum(["today", "week", "month", "custom"]).optional(),
  categoryIds: idsSchema.optional(),
  accountIds: idsSchema.optional(),
  tagId: z.string().min(1).optional(),
}).strict();

export const inboxListSchema = z.object({
  scope: z.string().min(1),
  search: z.string(),
  sortBy: z.enum(["newest", "oldest"]),
  filters: filtersSchema,
}).strict();
export type InboxListState = z.infer<typeof inboxListSchema>;
type InboxFilters = InboxListState["filters"];

export function inboxPresetDate(preset: UniboxSearchParams["datePreset"], now = new Date()): Date | undefined {
  if (!preset || preset === "custom") return undefined;
  const date = new Date(now);
  date.setHours(0, 0, 0, 0);
  date.setDate(date.getDate() - (preset === "week" ? 6 : preset === "month" ? 29 : 0));
  return date;
}

export function composeInboxParams(base: UniboxSearchParams, filters: InboxFilters, accounts: { id: string; tags?: string[] }[]): UniboxSearchParams {
  const next = { ...base, from: filters.from };
  if (base.unseen === undefined) next.unseen = filters.unseen;
  if (!base.since && !base.until) {
    next.datePreset = filters.datePreset;
    next.since = inboxPresetDate(filters.datePreset) ?? filters.since;
    next.until = filters.until;
  }
  if (!base.categoryIds) next.categoryIds = filters.categoryIds;
  if (!base.accountIds) {
    next.tagId = filters.tagId;
    next.accountIds = filters.tagId
      ? accounts.filter((account) => account.tags?.includes(filters.tagId!)).map((account) => account.id)
      : filters.accountIds;
  }
  return next;
}

export function inboxUserOverrides(params: UniboxSearchParams, base: UniboxSearchParams): InboxFilters {
  const filters: InboxFilters = {};
  if (params.from) filters.from = params.from;
  if (base.unseen === undefined && params.unseen !== undefined) filters.unseen = params.unseen;
  if (!base.since && !base.until) {
    if (params.datePreset) filters.datePreset = params.datePreset;
    if (!params.datePreset || params.datePreset === "custom") {
      if (params.since) filters.since = params.since;
      if (params.until) filters.until = params.until;
    }
  }
  if (!base.categoryIds && params.categoryIds?.length) filters.categoryIds = params.categoryIds;
  if (!base.accountIds) {
    if (params.tagId) filters.tagId = params.tagId;
    else if (params.accountIds?.length) filters.accountIds = params.accountIds;
  }
  return filters;
}
