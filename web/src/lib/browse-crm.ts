import { z } from "zod";
import { EMPTY_TASK_SEARCH } from "@/lib/api/models/app/crm/SearchTasks";
import { EMPTY_DEAL_SEARCH } from "@/lib/api/models/app/crm/SearchDeals";

const isoDate = z.union([z.iso.datetime({ offset: true }), z.iso.date()]);

export const taskBrowseFiltersSchema = z.object({
    query: z.string(),
    statuses: z.array(z.enum(["pending", "in_progress", "completed", "cancelled"])),
    priorities: z.array(z.enum(["low", "medium", "high", "urgent"])),
    types: z.array(z.string()),
    assigned_to: z.array(z.string()),
    team_ids: z.array(z.string()),
    due_after: isoDate,
    due_before: isoDate,
    overdue: z.boolean(),
    sort_by: z.enum(["created_at", "updated_at", "due_date", "priority", "title"]),
    reverse: z.boolean(),
}).partial().transform((filters) => ({ ...EMPTY_TASK_SEARCH, ...filters }));

export const taskBrowseViewSchema = z.enum(["flat", "grouped"]);
export const dealBrowseViewSchema = z.enum(["table", "board"]);
export const dealBoardPipelineSchema = z.string().min(1).optional();
export const crmBrowseSearchSchema = z.string();
export const meetingBrowseTimeframeSchema = z.enum(["upcoming", "past", "all"]);

export const dealBrowseFiltersSchema = z.object({
    query: z.string(),
    statuses: z.array(z.enum(["open", "won", "lost"])),
    pipeline_ids: z.array(z.string()),
    min_value: z.number().finite(),
    max_value: z.number().finite(),
    close_after: isoDate,
    close_before: isoDate,
    sort_by: z.enum(["created_at", "updated_at", "value", "expected_close_date", "name"]),
    reverse: z.boolean(),
}).partial().transform((filters) => ({ ...EMPTY_DEAL_SEARCH, ...filters }));
