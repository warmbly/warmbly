import { z } from "zod";

export const browseSearchSchema = z.string();
export const browseFlagSchema = z.boolean();
export const formsStatusSchema = z.enum(["all", "draft", "published", "archived"]);
export const formsSortSchema = z.strictObject({
    key: z.enum(["name", "views", "starts", "submissions", "conversion", "identified", "created"]),
    dir: z.union([z.literal(1), z.literal(-1)]),
});
export const submissionsStatusSchema = z.enum(["all", "completed", "in_progress", "junk"]);
export const formAnalyticsRangeSchema = z.enum(["7d", "30d", "90d"]);
export const auditActionSchema = z.enum([
    "create", "update", "delete", "invite", "remove", "transfer", "start", "stop", "pause", "resume", "send",
    "connect", "disconnect", "rotate", "revoke", "duplicate", "export", "import", "api_call",
]);
export const auditEntitySchema = z.enum([
    "campaign", "campaign_lead", "contact", "email_account", "step", "template", "email_image",
    "api_key", "webhook", "integration", "warmup_routing_rule", "organization", "organization_member", "invitation",
    "folder", "tag", "category", "subscription", "settings", "crm_pipeline", "crm_stage", "crm_deal", "crm_task", "crm_note",
    "unibox", "user",
]);
export const auditDateSchema = z.union([z.literal(""), z.iso.date()]);
export const billingIntervalSchema = z.enum(["monthly", "annual"]);
export const aiUsageRangeSchema = z.union([z.literal(7), z.literal(30), z.literal(90)]);
export const oauthAppsTabSchema = z.enum(["apps", "authorized"]);
export const webhookTabSchema = z.enum(["overview", "deliveries", "settings"]);
export const webhookStatusSchema = z.enum(["", "pending", "in_flight", "delivered", "failed", "abandoned"]);
export const slackTabSchema = z.enum(["overview", "assistant", "inbox", "notifications", "members"]);
