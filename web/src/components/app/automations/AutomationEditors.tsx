// Field editors for the automation builder's side panel: the trigger's inbound
// URL, a condition's test, and every action's settings. The canvas decides
// which one to show; these only edit the step they are given.

import React from "react";
import { motion } from "framer-motion";
import {
    BracesIcon,
    CheckIcon,
    CopyIcon,
    Link2Icon,
    PlusIcon,
    SparklesIcon,
    TriangleAlertIcon,
    XIcon,
    ZapIcon,
} from "lucide-react";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import type { AutomationCondition, AITagRef } from "@/lib/api/models/app/automations/Automation";
import type { IntegrationConnection } from "@/lib/api/models/app/integrations/Integration";
import {
    triggerLabel,
    actionLabel,
    actionNeedsChannel,
    actionNeedsURL,
    actionTakesClayURL,
    actionSupportsTemplate,
    triggerConditionFields,
    triggerFieldDef,
    conditionFieldKey,
    conditionFromFieldKey,
    operatorsForType,
    isConfidenceKey,
    triggerVariables,
    NATIVE_CONNECTION,
    NATIVE_ACTIONS,
    AI_ALLOWLIST_ACTIONS,
    isNativeAction,
    nativeActionNeeds,
    triggerCarriesThread,
    type TriggerFieldDef,
} from "@/lib/api/models/app/automations/meta";
import { API_URL } from "@/lib/information";
import CategoryPicker from "@/components/app/contacts/CategoryPicker";
import CampaignPicker from "@/components/app/campaigns/CampaignPicker";
import { ExpressionReference } from "@/components/app/automations/ExpressionReference";
import DealStagePicker from "@/components/app/crm/DealStagePicker";
import TaskTypePicker from "@/components/app/crm/TaskTypePicker";
import AssigneeTeamPicker, { type AssigneeValue } from "@/components/app/crm/AssigneeTeamPicker";
import { CrmDealNote, CrmTaskNote, CrmUpsertNote } from "@/components/app/crm/crmMode";
import { useAutomations } from "@/lib/api/hooks/app/automations/useAutomations";
import { useUserProfile } from "@/hooks/context/user";
import { actionGlyph, defaultConfigForAction } from "./automationActions";
import { cn } from "@/lib/utils";
import { fmtUsdAbout } from "@/lib/money";
import { AI_ESTIMATE_USD } from "@/lib/aiEstimates";

// InboundUrlField shows the automation's unique inbound-webhook URL (read-only)
// with a copy button. The stored value is a path; we prefix the API origin so
// the copied value is the full URL an external system POSTs to.
export function InboundUrlField({ inboundUrl }: { inboundUrl?: string }) {
    const [copied, setCopied] = React.useState(false);
    if (!inboundUrl) {
        return (
            <p className="text-[11.5px] text-slate-400 leading-relaxed">
                Save this automation to generate its unique webhook URL. An external system POSTs JSON to that URL and
                the body becomes the event payload (reference any field with <code>{"{{.field}}"}</code>).
            </p>
        );
    }
    const full = `${API_URL}${inboundUrl}`;
    const copy = async () => {
        try {
            await navigator.clipboard.writeText(full);
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
        } catch {
            /* clipboard blocked — the value is still selectable in the field */
        }
    };
    return (
        <div>
            <Label>Your webhook URL</Label>
            <div className="flex items-center gap-1.5">
                <input
                    readOnly
                    value={full}
                    onFocus={(e) => e.currentTarget.select()}
                    className="flex-1 h-7 rounded-md border border-slate-200 bg-slate-50 px-2 text-[11.5px] font-mono text-slate-700 focus:border-sky-400 focus:ring-2 focus:ring-sky-100 focus:outline-none"
                />
                <button
                    type="button"
                    onClick={copy}
                    className="h-7 px-2 rounded-md border border-slate-200 inline-flex items-center gap-1 text-[11.5px] text-slate-600 hover:bg-slate-50 shrink-0"
                >
                    {copied ? <CheckIcon className="w-3.5 h-3.5 text-emerald-600" /> : <CopyIcon className="w-3.5 h-3.5" />}
                    {copied ? "Copied" : "Copy"}
                </button>
            </div>
            <p className="text-[11.5px] text-slate-400 mt-1.5 leading-relaxed">
                POST JSON here to run this automation. The body becomes the event payload, so reference its keys with{" "}
                <code>{"{{.field}}"}</code> in actions and conditions.
            </p>
        </div>
    );
}


export function ConditionEditor({
    trigger,
    condition,
    onChange,
}: {
    trigger: string;
    condition: AutomationCondition;
    onChange: (c: AutomationCondition) => void;
}) {
    const fieldOptions: SelectOption[] = triggerConditionFields(trigger).map((f) => ({ value: f.key, label: f.label }));
    const selectedKey = conditionFieldKey(condition);
    const def: TriggerFieldDef | undefined = triggerFieldDef(trigger, selectedKey);
    const set = (patch: Partial<AutomationCondition>) => onChange({ ...condition, ...patch });
    const pickField = (key: string) => onChange(conditionFromFieldKey(trigger, key));

    const isRandom = condition.field === "random";
    const isExpression = condition.field === "expression";
    const isAI = condition.field === "ai";
    const op = condition.operator;
    const needsValue = !isRandom && !isAI && op !== "exists" && op !== "is_true";
    const isConfidence = isConfidenceKey(selectedKey);
    const vars = triggerVariables(trigger);

    return (
        <div className="space-y-3">
            <div>
                <Label>If</Label>
                <SelectMenu value={selectedKey} onChange={pickField} options={fieldOptions} className="w-full" fullWidth />
            </div>

            {/* Operator — data fields only (random / expression / AI don't use one). */}
            {!isRandom && !isExpression && !isAI && def && (
                <div>
                    <Label>Condition</Label>
                    <SelectMenu
                        value={op}
                        onChange={(v) => set({ operator: v, value: v === "exists" || v === "is_true" ? undefined : condition.value })}
                        options={operatorsForType(def.type)}
                        className="w-full"
                        fullWidth
                    />
                </div>
            )}

            {/* Value editor, by field type + operator. */}
            {isAI ? (
                <div className="space-y-2">
                    <Label>Ask AI</Label>
                    <textarea
                        value={String(condition.prompt ?? "")}
                        onChange={(e) => set({ prompt: e.target.value })}
                        rows={3}
                        maxLength={2000}
                        placeholder="Is this reply asking about pricing?"
                        className="w-full px-2.5 py-1.5 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 resize-y leading-relaxed"
                    />
                    {vars.length > 0 && (
                        <div className="flex flex-wrap gap-1">
                            {vars.map((v) => (
                                <button
                                    key={v}
                                    type="button"
                                    onClick={() => set({ prompt: `${condition.prompt ?? ""}{{.${v}}}` })}
                                    className="px-1.5 py-0.5 rounded border border-slate-200 bg-slate-50 font-mono text-[10.5px] text-slate-600 hover:border-sky-300 hover:text-sky-700"
                                >
                                    {`{{.${v}}}`}
                                </button>
                            ))}
                        </div>
                    )}
                    <p className="text-[10.5px] text-slate-400 leading-relaxed">
                        AI reads the event data and answers your question: yes follows the Yes branch, no follows No. If AI
                        can't answer, the No branch runs. Costs {fmtUsdAbout(AI_ESTIMATE_USD.aiNode)} each time this branch is evaluated.
                    </p>
                </div>
            ) : isExpression ? (
                <div className="space-y-2">
                    <div className="flex items-center justify-between gap-2">
                        <Label className="mb-0">Expression</Label>
                        <ExpressionReference />
                    </div>
                    <textarea
                        value={String(condition.expression ?? "")}
                        onChange={(e) => set({ expression: e.target.value })}
                        rows={3}
                        placeholder={`and (gtf .confidence 0.8) (eq .intent "positive")`}
                        className="w-full px-2.5 py-1.5 rounded-md border border-slate-200 bg-white font-mono text-[12px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 resize-y leading-relaxed"
                    />
                    {vars.length > 0 && (
                        <div className="flex flex-wrap gap-1">
                            {vars.map((v) => (
                                <button
                                    key={v}
                                    type="button"
                                    onClick={() => set({ expression: `${condition.expression ?? ""} .${v}`.replace(/^\s+/, "") })}
                                    className="px-1.5 py-0.5 rounded border border-slate-200 bg-slate-50 font-mono text-[10.5px] text-slate-600 hover:border-sky-300 hover:text-sky-700"
                                >
                                    .{v}
                                </button>
                            ))}
                        </div>
                    )}
                    <p className="text-[10.5px] text-slate-400 leading-relaxed">
                        Passes the “yes” branch when truthy. Reference fields as <code className="font-mono">.field</code>; use{" "}
                        <code className="font-mono">eq gt lt and or not</code>, numeric{" "}
                        <code className="font-mono">gtf ltf add sub mul div</code>, and{" "}
                        <code className="font-mono">contains lower has join</code>. Example:{" "}
                        <code className="font-mono">{`and (gtf .confidence 0.8) (eq .intent "positive")`}</code>.
                    </p>
                </div>
            ) : isRandom ? (
                <div>
                    <Label>Take the “yes” path</Label>
                    <NumberInput
                        value={Number(condition.value ?? 50)}
                        onChange={(v) => set({ value: Math.max(1, Math.min(99, v)) })}
                        min={1}
                        max={99}
                        step={5}
                        suffix="% of the time"
                        className="w-full"
                    />
                </div>
            ) : needsValue && def?.type === "enum" ? (
                <div>
                    <Label>Value</Label>
                    <SelectMenu
                        value={String(condition.value ?? "")}
                        onChange={(v) => set({ value: v })}
                        options={def.options ?? []}
                        className="w-full"
                        fullWidth
                    />
                </div>
            ) : needsValue && def?.type === "list" ? (
                <div className="space-y-2">
                    <Label>Value</Label>
                    <TextInput value={String(condition.value ?? "")} onChange={(v) => set({ value: v })} placeholder="Label" className="w-full" />
                    {(def.suggestions?.length ?? 0) > 0 && (
                        <div className="flex flex-wrap gap-1">
                            {def.suggestions!.map((v) => (
                                <button
                                    key={v}
                                    type="button"
                                    onClick={() => set({ value: v })}
                                    className={`px-1.5 py-0.5 rounded border text-[11px] ${
                                        condition.value === v
                                            ? "border-sky-300 bg-sky-50 text-sky-700"
                                            : "border-slate-200 bg-slate-50 text-slate-600 hover:border-sky-300 hover:text-sky-700"
                                    }`}
                                >
                                    {v}
                                </button>
                            ))}
                        </div>
                    )}
                    <p className="text-[10.5px] text-slate-400 leading-relaxed">
                        Matches a whole label, so “Interested” does not match “Not interested”. Type a label from your own tagging
                        questions to test it too.
                    </p>
                </div>
            ) : needsValue && isConfidence ? (
                <div>
                    <Label>{op === "gte" ? "At least" : op === "lte" ? "At most" : "Value"}</Label>
                    <NumberInput
                        value={Math.round(Number(condition.value ?? 0) * 100)}
                        onChange={(v) => set({ value: Math.max(0, Math.min(100, v)) / 100 })}
                        min={0}
                        max={100}
                        step={5}
                        suffix="%"
                        className="w-full"
                    />
                </div>
            ) : needsValue && def?.type === "number" ? (
                <div>
                    <Label>Value</Label>
                    <NumberInput value={Number(condition.value ?? 0)} onChange={(v) => set({ value: v })} className="w-full" />
                </div>
            ) : needsValue ? (
                <div>
                    <Label>Value</Label>
                    <TextInput value={String(condition.value ?? "")} onChange={(v) => set({ value: v })} placeholder="value" className="w-full" />
                </div>
            ) : null}

            <p className="text-[11px] text-slate-400 leading-relaxed">
                Steps under Yes run when this passes. Steps under No run when it does not.
            </p>
        </div>
    );
}


export function ActionEditor({
    trigger,
    selfId,
    data,
    targets,
    connLabel,
    actionsForProvider,
    providerOf,
    onAction,
}: {
    trigger: string;
    selfId: string;
    data: { action?: string; connection_id?: string; config?: Record<string, unknown> };
    targets: IntegrationConnection[];
    connLabel: (id?: string) => string;
    actionsForProvider: (provider?: string) => string[];
    providerOf: (id?: string) => string;
    onAction: (patch: Record<string, unknown>) => void;
}) {
    const config = data.config ?? {};
    const vars = triggerVariables(trigger);
    const setConfig = (k: string, v: unknown) => onAction({ config: { ...config, [k]: v } });
    const patchConfig = (p: Record<string, unknown>) => onAction({ config: { ...config, ...p } });
    const insertInto = (k: string, token: string) => setConfig(k, `${String(config[k] ?? "")}{{.${token}}}`);

    const isNative = isNativeAction(data.action ?? "");
    const selectedConn = isNative ? NATIVE_CONNECTION : (data.connection_id ?? "");
    const connOptions: SelectOption[] = [
        { value: NATIVE_CONNECTION, label: "Warmbly (built-in)", icon: <ZapIcon className="size-3.5 shrink-0 text-indigo-600" /> },
        ...targets.map((c) => ({
            value: c.id,
            label: connLabel(c.id),
            icon: <Link2Icon className="size-3.5 shrink-0 text-slate-400" />,
        })),
    ];
    const actionOptions: SelectOption[] = (isNative ? NATIVE_ACTIONS : actionsForProvider(providerOf(data.connection_id))).map(
        (a) => ({ value: a, label: actionLabel(a), icon: actionGlyph(a) }),
    );

    const pickConnection = (connId: string) => {
        if (connId === NATIVE_CONNECTION) {
            const first = NATIVE_ACTIONS[0];
            onAction({ connection_id: undefined, action: first, config: {}, sub: "Built-in action", provider: "", native: true, title: actionLabel(first) });
            return;
        }
        const acts = actionsForProvider(providerOf(connId));
        onAction({
            connection_id: connId,
            action: acts[0] ?? "",
            config: {},
            sub: connLabel(connId),
            provider: providerOf(connId),
            native: false,
            title: acts[0] ? actionLabel(acts[0]) : "Choose an action",
        });
    };
    const pickAction = (action: string) =>
        onAction({
            action,
            config: defaultConfigForAction(action, trigger),
            title: actionLabel(action),
            native: isNativeAction(action),
            ...(isNativeAction(action) ? { connection_id: undefined } : {}),
        });

    return (
        <div className="space-y-3">
            <div>
                <Label>Run</Label>
                <SelectMenu value={selectedConn} onChange={pickConnection} options={connOptions} className="w-full" fullWidth />
            </div>
            <div>
                <Label>Action</Label>
                <SelectMenu value={data.action ?? ""} onChange={pickAction} options={actionOptions} className="w-full" fullWidth />
            </div>

            {/* Config fields swap when the action/target changes; the keyed
                fade keeps that swap from feeling like a hard cut. */}
            <motion.div
                key={`${selectedConn}:${data.action ?? ""}`}
                initial={{ opacity: 0, y: 4 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.15 }}
                className="space-y-3"
            >
            {isNative ? (
                <NativeActionConfig action={data.action ?? ""} trigger={trigger} config={config} patchConfig={patchConfig} selfId={selfId} />
            ) : (
                <>
                    <CrmUpsertNote action={data.action ?? ""} />
                    {actionNeedsChannel(data.action ?? "") && (
                        <div>
                            <Label>Channel</Label>
                            <TextInput value={String(config.channel ?? "")} onChange={(v) => setConfig("channel", v)} placeholder="#sales" className="w-full" />
                        </div>
                    )}
                    {actionNeedsURL(data.action ?? "") && (
                        <div>
                            <Label>Webhook URL</Label>
                            <TextInput value={String(config.url ?? "")} onChange={(v) => setConfig("url", v)} placeholder="https://hooks.zapier.com/…" className="w-full" />
                            <VarChips vars={vars} onPick={(t) => insertInto("url", t)} />
                        </div>
                    )}
                    {actionTakesClayURL(data.action ?? "") && (
                        <div>
                            <Label>Clay table webhook (optional)</Label>
                            <TextInput value={String(config.url ?? "")} onChange={(v) => setConfig("url", v)} placeholder="Uses the webhook saved on the Clay connection" className="w-full" />
                            <p className="text-[11px] text-slate-400 mt-1">Sends to a different Clay table than the connection default. The connection's auth token is not sent to it.</p>
                        </div>
                    )}
                    {actionSupportsTemplate(data.action ?? "") && (
                        <div>
                            <Label>Message (optional)</Label>
                            <TextInput
                                value={String(config.message_template ?? "")}
                                onChange={(v) => setConfig("message_template", v)}
                                placeholder="New reply from {{.contact_email}}"
                                className="w-full"
                            />
                            <VarChips vars={vars} onPick={(t) => insertInto("message_template", t)} />
                        </div>
                    )}
                    <p className="text-[11px] text-slate-400 leading-relaxed">
                        Values are full Go templates: <span className="font-mono text-slate-500">{"{{.variable}}"}</span> fields plus{" "}
                        <span className="font-mono text-slate-500">{"{{if}}"}</span>, helpers, and pipelines, rendered against the trigger data when the automation runs.
                    </p>
                </>
            )}
            </motion.div>
        </div>
    );
}

const NATIVE_PRIORITIES = ["low", "medium", "high", "urgent"] as const;
function PrioritySegment({ value, onChange }: { value: string; onChange: (p: string) => void }) {
    const current = value || "medium";
    return (
        <div className="inline-flex rounded-md border border-slate-200 bg-white p-0.5">
            {NATIVE_PRIORITIES.map((p) => (
                <button
                    key={p}
                    type="button"
                    onClick={() => onChange(p)}
                    className={cn(
                        "h-7 px-2.5 rounded text-[11px] font-medium capitalize transition-colors",
                        current === p ? "bg-sky-600 text-white shadow-sm" : "text-slate-500 hover:bg-slate-50 hover:text-slate-700",
                    )}
                >
                    {p}
                </button>
            ))}
        </div>
    );
}

const NATIVE_CURRENCIES: SelectOption[] = ["USD", "EUR", "GBP", "CAD", "AUD"].map((c) => ({ value: c, label: c }));

// NativeActionConfig renders the right editor for a built-in (Warmbly) action.
function NativeActionConfig({
    action,
    trigger,
    config,
    patchConfig,
    selfId,
}: {
    action: string;
    trigger: string;
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
    selfId: string;
}) {
    const need = nativeActionNeeds(action);
    return (
        <div className="space-y-3">
            {need === "tag" && (
                <div>
                    <Label>{action === "warmbly.add_tag" ? "Label to add" : "Label to remove"}</Label>
                    <CategoryPicker
                        value={config.category_id ? [String(config.category_id)] : []}
                        onChange={(ids) => patchConfig({ category_id: ids.length ? ids[ids.length - 1] : "" })}
                        placeholder="Pick a label…"
                    />
                </div>
            )}

            {need === "label" && (
                <div className="space-y-2">
                    <div>
                        <Label>Labels to apply</Label>
                        <CategoryPicker
                            value={Array.isArray(config.label_ids) ? (config.label_ids as string[]) : []}
                            onChange={(ids) => patchConfig({ label_ids: ids })}
                            placeholder="Pick one or more labels…"
                        />
                    </div>
                    {triggerCarriesThread(trigger) ? (
                        <p className="text-[11px] leading-relaxed text-slate-400">
                            Labels the conversation the contact replied on (the same labels you set by hand in the unibox).
                        </p>
                    ) : (
                        <p className="inline-flex items-start gap-1.5 rounded-md border border-amber-200 bg-amber-50 px-2 py-1.5 text-[11px] leading-relaxed text-amber-700">
                            <TriangleAlertIcon className="mt-px w-3.5 h-3.5 shrink-0" /> This labels the email a contact
                            replied on, so it only runs on a &quot;Reply received&quot; automation. This trigger has no
                            inbox thread to label.
                        </p>
                    )}
                </div>
            )}

            {need === "deal" && (
                <>
                    <CrmDealNote creates={action === "warmbly.create_deal"} />
                    <div>
                        <Label>{action === "warmbly.create_deal" ? "Create the deal in" : "Move the deal to"}</Label>
                        <DealStagePicker
                            pipelineId={config.deal_pipeline_id ? String(config.deal_pipeline_id) : undefined}
                            stageId={config.deal_stage_id ? String(config.deal_stage_id) : undefined}
                            onChange={({ pipelineId, stageId }) => patchConfig({ deal_pipeline_id: pipelineId, deal_stage_id: stageId })}
                        />
                    </div>
                    {action === "warmbly.create_deal" && (
                        <>
                            <div>
                                <Label>Deal name</Label>
                                <TextInput
                                    value={String(config.deal_name ?? "")}
                                    onChange={(v) => patchConfig({ deal_name: v })}
                                    placeholder="{{.company}} ({{.contact_email}})"
                                    className="w-full"
                                />
                                <p className="mt-1.5 text-[11px] text-slate-400">
                                    Full Go template: {"{{.variable}}"} fields plus {"{{if}}"}, helpers, and pipelines.
                                </p>
                            </div>
                            <div className="flex items-end gap-3">
                                <div className="flex-1">
                                    <Label>Value (optional)</Label>
                                    <NumberInput
                                        value={Number(config.deal_value ?? 0)}
                                        onChange={(n) => patchConfig({ deal_value: n > 0 ? n : undefined })}
                                        min={0}
                                        max={1_000_000_000}
                                        className="w-full"
                                    />
                                </div>
                                <div className="w-32">
                                    <Label>Currency</Label>
                                    <SelectMenu
                                        value={String(config.deal_currency ?? "USD")}
                                        onChange={(c) => patchConfig({ deal_currency: c })}
                                        options={NATIVE_CURRENCIES}
                                        className="w-full"
                                        fullWidth
                                    />
                                </div>
                            </div>
                        </>
                    )}
                    {action === "warmbly.move_deal_stage" && (
                        <p className="text-[11px] text-slate-400 leading-relaxed">
                            Moves the contact's most recent open deal in this pipeline. If they have no open deal here, nothing happens.
                        </p>
                    )}
                </>
            )}

            {need === "task" && (
                <>
                    <CrmTaskNote />
                    <div>
                        <Label>Task title</Label>
                        <TextInput
                            value={String(config.task_title ?? "")}
                            onChange={(v) => patchConfig({ task_title: v })}
                            placeholder="Follow up with {{.contact_email}}"
                            className="w-full"
                        />
                        <p className="mt-1.5 text-[11px] text-slate-400">
                            Full Go template: {"{{.variable}}"} fields plus {"{{if}}"}, helpers, and pipelines.
                        </p>
                    </div>
                    <div>
                        <Label>Task type</Label>
                        <TaskTypePicker
                            value={String(config.task_type ?? "")}
                            onChange={(name) => patchConfig({ task_type: name })}
                            className="w-full"
                        />
                    </div>
                    <div className="flex flex-wrap items-end gap-4">
                        <div>
                            <Label>Priority</Label>
                            <PrioritySegment value={String(config.task_priority ?? "")} onChange={(p) => patchConfig({ task_priority: p })} />
                        </div>
                        <div>
                            <Label>Due in (days)</Label>
                            <NumberInput
                                value={Number(config.task_due_offset_days ?? 1)}
                                onChange={(n) => patchConfig({ task_due_offset_days: n })}
                                min={0}
                                max={365}
                                className="w-28"
                            />
                        </div>
                    </div>
                    <div>
                        <Label>Assign to</Label>
                        <AssigneeTeamPicker
                            className="w-full"
                            value={{
                                userId: config.task_assigned_to ? String(config.task_assigned_to) : null,
                                teamId: config.task_assigned_team_id ? String(config.task_assigned_team_id) : null,
                            }}
                            onChange={(v: AssigneeValue) => patchConfig({ task_assigned_to: v.userId ?? null, task_assigned_team_id: v.teamId ?? null })}
                        />
                        <p className="mt-1.5 text-[11px] text-slate-400">
                            Assign to a teammate or a whole team. Unassigned falls back to the workspace owner.
                        </p>
                    </div>
                </>
            )}

            {need === "automation" && <RunAnotherAutomationFields config={config} patchConfig={patchConfig} selfId={selfId} />}

            {need === "contact" && <UpsertContactFields trigger={trigger} config={config} patchConfig={patchConfig} />}

            {need === "campaign" && (
                <div className="space-y-2">
                    <div>
                        <Label>Campaign</Label>
                        <CampaignPicker
                            campaignId={config.campaign_id ? String(config.campaign_id) : null}
                            campaignName={String(config.campaign_name ?? "")}
                            onChange={(id, name) => patchConfig({ campaign_id: id ?? "", campaign_name: name })}
                            noneLabel="Pick a campaign…"
                        />
                    </div>
                    <p className="text-[11px] text-slate-400 leading-relaxed">
                        Enrols the contact this event is about (by <code>contact_id</code> or <code>contact_email</code>). Sending still runs through the campaign&apos;s mailboxes, daily caps and spacing, and the campaign keeps running for new leads instead of finishing between runs.
                    </p>
                </div>
            )}

            {need === "event" && <FireEventFields config={config} patchConfig={patchConfig} />}

            {need === "vars" && <SetVariablesFields config={config} patchConfig={patchConfig} />}

            {need === "ai_step" && <AIStepFields config={config} patchConfig={patchConfig} />}

            {need === "ai_switch" && <AISwitchFields config={config} patchConfig={patchConfig} />}

            {need === "none" && (
                <p className="text-[11px] text-slate-400 leading-relaxed">
                    Works when the event carries a campaign (reply / bounce / unsubscribe triggers).
                </p>
            )}
        </div>
    );
}

type SetVarRow = { key: string; value: string };

const IF_EXISTS_OPTIONS: SelectOption[] = [
    { value: "update", label: "Update it (fill blanks, add labels and campaign)" },
    { value: "skip", label: "Leave it alone" },
];

// The contact columns a lead-intake action fills, each a template rendered
// against the event data. Email is the identity; the rest enrich.
const UPSERT_FIELDS: { key: string; label: string; placeholder: string }[] = [
    { key: "first_name", label: "First name", placeholder: "{{.first_name}}" },
    { key: "last_name", label: "Last name", placeholder: "{{.last_name}}" },
    { key: "company", label: "Company", placeholder: "{{.company}}" },
    { key: "phone", label: "Phone", placeholder: "{{.phone}}" },
];

// UpsertContactFields edits the lead-intake action: which event fields become
// the contact, where it lands (tags, campaign), and what to do when the email
// already exists. Every value is a Go template against the trigger data, so an
// inbound webhook's own JSON keys map straight onto contact columns.
function UpsertContactFields({
    trigger,
    config,
    patchConfig,
}: {
    trigger: string;
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    const vars = triggerVariables(trigger);
    const insertInto = (k: string, token: string) => patchConfig({ [k]: `${String(config[k] ?? "")}{{.${token}}}` });
    const rows: SetVarRow[] = Array.isArray(config.custom_fields)
        ? (config.custom_fields as SetVarRow[]).map((v) => ({ key: String(v?.key ?? ""), value: String(v?.value ?? "") }))
        : [];
    const update = (next: SetVarRow[]) => patchConfig({ custom_fields: next });
    const setRow = (i: number, patch: Partial<SetVarRow>) => update(rows.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
    const addRow = () => update([...rows, { key: "", value: "" }]);
    const removeRow = (i: number) => update(rows.filter((_, idx) => idx !== i));

    return (
        <div className="space-y-3">
            <div>
                <Label>Email</Label>
                <TextInput
                    value={String(config.email ?? "")}
                    onChange={(v) => patchConfig({ email: v })}
                    placeholder="{{.email}}"
                    className="w-full font-mono"
                />
                <VarChips vars={vars} onPick={(t) => insertInto("email", t)} />
                <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                    The contact&apos;s identity. An existing contact with this address is matched instead of duplicated.
                </p>
            </div>
            <div className="grid grid-cols-2 gap-2">
                {UPSERT_FIELDS.map((f) => (
                    <div key={f.key}>
                        <Label>{f.label}</Label>
                        <TextInput
                            value={String(config[f.key] ?? "")}
                            onChange={(v) => patchConfig({ [f.key]: v })}
                            placeholder={f.placeholder}
                            className="w-full font-mono"
                        />
                    </div>
                ))}
            </div>
            <div>
                <Label>Custom fields</Label>
                <div className="space-y-2">
                    {rows.map((row, i) => (
                        <div key={i} className="flex items-center gap-2">
                            <TextInput
                                value={row.key}
                                onChange={(v) => setRow(i, { key: v })}
                                placeholder="field"
                                className="w-28 shrink-0 font-mono"
                            />
                            <span className="text-[12.5px] text-slate-400">=</span>
                            <TextInput
                                value={row.value}
                                onChange={(v) => setRow(i, { value: v })}
                                placeholder="{{.team_size}}"
                                className="flex-1 min-w-0 font-mono"
                            />
                            <button
                                type="button"
                                onClick={() => removeRow(i)}
                                className="shrink-0 text-slate-400 hover:text-rose-500"
                                aria-label="Remove custom field"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                    ))}
                </div>
                <button
                    type="button"
                    onClick={addRow}
                    className="mt-2 inline-flex items-center gap-1 text-[12px] text-sky-600 hover:text-sky-700"
                >
                    <PlusIcon className="w-3.5 h-3.5" /> Add custom field
                </button>
            </div>
            <div>
                <Label>Labels</Label>
                <CategoryPicker
                    value={Array.isArray(config.category_ids) ? (config.category_ids as string[]) : []}
                    onChange={(ids) => patchConfig({ category_ids: ids })}
                    placeholder="Pick labels…"
                />
            </div>
            <div>
                <Label>Add to campaign</Label>
                <CampaignPicker
                    campaignId={config.campaign_id ? String(config.campaign_id) : null}
                    campaignName={String(config.campaign_name ?? "")}
                    onChange={(id, name) => patchConfig({ campaign_id: id ?? "", campaign_name: name })}
                />
            </div>
            <div>
                <Label>If the contact already exists</Label>
                <SelectMenu
                    value={String(config.if_exists ?? "update")}
                    onChange={(v) => patchConfig({ if_exists: v })}
                    options={IF_EXISTS_OPTIONS}
                    className="w-full"
                    fullWidth
                />
            </div>
            <p className="text-[11px] text-slate-400 leading-relaxed">
                A blank value never erases what the contact already has. The written contact becomes this event&apos;s contact, so the
                steps after it (label, task, deal) act on it. A campaign picked here keeps running for new leads instead of finishing between runs.
            </p>
        </div>
    );
}

// SetVariablesFields edits a list of named template values written back into the
// event data for later steps to reuse (the safe "transform" node).
function SetVariablesFields({
    config,
    patchConfig,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    const rows: SetVarRow[] = Array.isArray(config.set_vars)
        ? (config.set_vars as SetVarRow[]).map((v) => ({ key: String(v?.key ?? ""), value: String(v?.value ?? "") }))
        : [];
    const display = rows.length ? rows : [{ key: "", value: "" }];

    // Keep blank rows while editing (filtering here would delete a just-added row
    // before it can be typed in). Save-time validation requires one named var, and
    // the backend ignores any row whose key is empty.
    const update = (next: SetVarRow[]) => patchConfig({ set_vars: next });
    const setRow = (i: number, patch: Partial<SetVarRow>) => update(display.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
    const addRow = () => update([...display, { key: "", value: "" }]);
    const removeRow = (i: number) => update(display.filter((_, idx) => idx !== i));

    return (
        <div className="space-y-2">
            {display.map((row, i) => (
                <div key={i} className="flex items-center gap-2">
                    <TextInput
                        value={row.key}
                        onChange={(v) => setRow(i, { key: v })}
                        placeholder="name"
                        className="w-28 shrink-0 font-mono"
                    />
                    <span className="text-[12.5px] text-slate-400">=</span>
                    <TextInput
                        value={row.value}
                        onChange={(v) => setRow(i, { value: v })}
                        placeholder="{{.first_name}} at {{.company}}"
                        className="flex-1 min-w-0 font-mono"
                    />
                    <button
                        type="button"
                        onClick={() => removeRow(i)}
                        className="shrink-0 text-slate-400 hover:text-rose-500"
                        aria-label="Remove variable"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>
            ))}
            <button
                type="button"
                onClick={addRow}
                className="inline-flex items-center gap-1 text-[12px] text-sky-600 hover:text-sky-700"
            >
                <PlusIcon className="w-3.5 h-3.5" /> Add variable
            </button>
            <p className="text-[11px] text-slate-400 leading-relaxed">
                Each value is a Go template. Later steps reference it as <code>{`{{.name}}`}</code>.
            </p>
        </div>
    );
}

// FireEventFields configures a custom "fire event": an event name + a list of
// templated key/value fields that become the event payload. The event is
// published to the realtime gateway, so a developer's app receives it over the
// API websocket (API key + REALTIME_SUBSCRIBE) without hosting a webhook URL.
function FireEventFields({
    config,
    patchConfig,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    const rows: SetVarRow[] = Array.isArray(config.event_fields)
        ? (config.event_fields as SetVarRow[]).map((v) => ({ key: String(v?.key ?? ""), value: String(v?.value ?? "") }))
        : [];
    const display = rows.length ? rows : [{ key: "", value: "" }];
    const update = (next: SetVarRow[]) => patchConfig({ event_fields: next });
    const setRow = (i: number, patch: Partial<SetVarRow>) => update(display.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
    const addRow = () => update([...display, { key: "", value: "" }]);
    const removeRow = (i: number) => update(display.filter((_, idx) => idx !== i));

    return (
        <div className="space-y-3">
            <div>
                <Label>Event name</Label>
                <TextInput
                    value={String(config.event_name ?? "")}
                    onChange={(v) => patchConfig({ event_name: v })}
                    placeholder="lead.replied"
                    className="w-full font-mono"
                />
                <p className="mt-1 text-[11px] text-slate-400">What your app subscribes to. Lowercase dotted names work well.</p>
            </div>
            <div>
                <Label>Payload</Label>
                <div className="space-y-2">
                    {display.map((row, i) => (
                        <div key={i} className="flex items-center gap-2">
                            <TextInput
                                value={row.key}
                                onChange={(v) => setRow(i, { key: v })}
                                placeholder="field"
                                className="w-28 shrink-0 font-mono"
                            />
                            <span className="text-[12.5px] text-slate-400">=</span>
                            <TextInput
                                value={row.value}
                                onChange={(v) => setRow(i, { value: v })}
                                placeholder="{{.contact_email}}"
                                className="flex-1 min-w-0 font-mono"
                            />
                            <button
                                type="button"
                                onClick={() => removeRow(i)}
                                className="shrink-0 text-slate-400 hover:text-rose-500"
                                aria-label="Remove field"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>
                    ))}
                </div>
                <button
                    type="button"
                    onClick={addRow}
                    className="mt-2 inline-flex items-center gap-1 text-[12px] text-sky-600 hover:text-sky-700"
                >
                    <PlusIcon className="w-3.5 h-3.5" /> Add field
                </button>
                <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                    Each value is a Go template against the event data. Your app receives <code>{`{ name, payload }`}</code> over the websocket.
                </p>
            </div>
        </div>
    );
}

// AIInstruction is the shared instruction textarea for every AI node: plain-
// language guidance the model follows, templated against the event data.
function AIInstruction({
    value,
    onChange,
    placeholder,
}: {
    value: string;
    onChange: (v: string) => void;
    placeholder: string;
}) {
    return (
        <div>
            <Label>Instruction</Label>
            <textarea
                value={value}
                onChange={(e) => onChange(e.target.value)}
                rows={3}
                placeholder={placeholder}
                className="w-full px-2.5 py-1.5 rounded-md border border-slate-200 bg-white text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 resize-y leading-relaxed"
            />
            <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                Reference event fields with <code>{`{{.field}}`}</code>. The model also sees the rest of the event data.
            </p>
        </div>
    );
}

// AIStringList edits a plain list of strings (labels or output keys) with the
// same add/remove pattern as the set-variables rows, minus the value column.
function AIStringList({
    label,
    values,
    onChange,
    placeholder,
    addLabel,
}: {
    label: string;
    values: string[];
    onChange: (next: string[]) => void;
    placeholder: string;
    addLabel: string;
}) {
    // Keep blank rows while editing; the backend + save-time check drop empties.
    const display = values.length ? values : [""];
    const setRow = (i: number, v: string) => onChange(display.map((r, idx) => (idx === i ? v : r)));
    const addRow = () => onChange([...display, ""]);
    const removeRow = (i: number) => onChange(display.filter((_, idx) => idx !== i));
    return (
        <div>
            <Label>{label}</Label>
            <div className="space-y-2">
                {display.map((row, i) => (
                    <div key={i} className="flex items-center gap-2">
                        <TextInput
                            value={row}
                            onChange={(v) => setRow(i, v)}
                            placeholder={placeholder}
                            className="flex-1 min-w-0 font-mono"
                        />
                        <button
                            type="button"
                            onClick={() => removeRow(i)}
                            className="shrink-0 text-slate-400 hover:text-rose-500"
                            aria-label={`Remove ${label}`}
                        >
                            <XIcon className="w-3.5 h-3.5" />
                        </button>
                    </div>
                ))}
            </div>
            <button
                type="button"
                onClick={addRow}
                className="mt-2 inline-flex items-center gap-1 text-[12px] text-sky-600 hover:text-sky-700"
            >
                <PlusIcon className="w-3.5 h-3.5" /> {addLabel}
            </button>
        </div>
    );
}

// AIOutputVariable is the optional target-variable override for classify/generate
// (so two AI nodes don't both write ai_class / ai_text).
function AIOutputVariable({
    value,
    onChange,
    defaultName,
}: {
    value: string;
    onChange: (v: string) => void;
    defaultName: string;
}) {
    return (
        <div>
            <Label>Output variable (optional)</Label>
            <TextInput value={value} onChange={onChange} placeholder={defaultName} className="w-40 font-mono" />
            <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                Where the result is stored. Defaults to <code>{`{{.${defaultName}}}`}</code>; set a name if another AI step already uses it.
            </p>
        </div>
    );
}

// AIClassifyFields: instruction + a closed label set + optional output var. The
// model returns one label, stored in ai_class (or the override) for downstream IFs.
function AIClassifyFields({
    config,
    patchConfig,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    const labels = Array.isArray(config.labels) ? (config.labels as unknown[]).map(String) : [];
    return (
        <div className="space-y-3">
            <AIInstruction
                value={String(config.instruction ?? "")}
                onChange={(v) => patchConfig({ instruction: v })}
                placeholder="Classify this reply by how interested the sender is."
            />
            <AIStringList
                label="Labels"
                values={labels}
                onChange={(next) => patchConfig({ labels: next })}
                placeholder="interested"
                addLabel="Add label"
            />
            <AIOutputVariable
                value={String(config.output_key ?? "")}
                onChange={(v) => patchConfig({ output_key: v })}
                defaultName="ai_class"
            />
            <p className="text-[11px] text-slate-400 leading-relaxed">
                The model picks exactly one label. Add at least two. Costs {fmtUsdAbout(AI_ESTIMATE_USD.aiNode)} each time it runs.
            </p>
        </div>
    );
}

// AIExtractFields: instruction + the field names to pull. Each output key becomes
// a variable of that name for later steps to read.
function AIExtractFields({
    config,
    patchConfig,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    const keys = Array.isArray(config.output_keys) ? (config.output_keys as unknown[]).map(String) : [];
    return (
        <div className="space-y-3">
            <AIInstruction
                value={String(config.instruction ?? "")}
                onChange={(v) => patchConfig({ instruction: v })}
                placeholder="Pull the company size, budget, and timeline from this message."
            />
            <AIStringList
                label="Output keys"
                values={keys}
                onChange={(next) => patchConfig({ output_keys: next })}
                placeholder="company_size"
                addLabel="Add key"
            />
            <p className="text-[11px] text-slate-400 leading-relaxed">
                Each key becomes a variable ( <code>{`{{.company_size}}`}</code> ). A field the model can't find is empty. Costs {fmtUsdAbout(AI_ESTIMATE_USD.aiNode)}.
            </p>
        </div>
    );
}

// AIGenerateFields: instruction + optional output var. The model writes text into
// ai_text (or the override) for a later step to send. It never sends on its own.
function AIGenerateFields({
    config,
    patchConfig,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    return (
        <div className="space-y-3">
            <AIInstruction
                value={String(config.instruction ?? "")}
                onChange={(v) => patchConfig({ instruction: v })}
                placeholder="Write a one-line summary of this reply for a Slack alert."
            />
            <AIOutputVariable
                value={String(config.output_key ?? "")}
                onChange={(v) => patchConfig({ output_key: v })}
                defaultName="ai_text"
            />
            <p className="text-[11px] text-slate-400 leading-relaxed">
                The result is stored, not sent. Use it in a later step (a Slack message, a task). Costs {fmtUsdAbout(AI_ESTIMATE_USD.aiNode)}.
            </p>
        </div>
    );
}

// The AI step mirrors the campaign agent plus single-shot transforms. Routing is
// the AI switch's job, so "decide" is deliberately not a step mode.
const AI_STEP_MODE_OPTIONS: SelectOption[] = [
    { value: "agent", label: "Agent (decide + act)" },
    { value: "generate", label: "Generate text" },
    { value: "classify", label: "Classify" },
    { value: "extract", label: "Extract fields" },
];

// AIToggle — one capability row (extended thinking, web search) matching the
// campaign switch's toggle. Purple accent when on.
function AIToggle({
    label,
    detail,
    on,
    onToggle,
}: {
    label: string;
    detail: string;
    on: boolean;
    onToggle: () => void;
}) {
    return (
        <div
            className={cn(
                "flex items-center gap-2 rounded-md px-2 py-1.5 ring-1 transition-colors",
                on ? "bg-purple-50 ring-purple-200" : "bg-slate-50 ring-slate-200",
            )}
        >
            <div className="min-w-0 flex-1">
                <div className={cn("text-[11.5px] font-medium", on ? "text-purple-700" : "text-slate-500")}>{label}</div>
                <div className="text-[10.5px] text-slate-400">{detail}</div>
            </div>
            <button
                type="button"
                role="switch"
                aria-checked={on}
                aria-label={label}
                onClick={onToggle}
                className={cn(
                    "relative inline-flex h-[18px] w-8 shrink-0 items-center rounded-full transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-purple-300",
                    on ? "bg-purple-600" : "bg-slate-300",
                )}
            >
                <span
                    className={cn(
                        "inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow-sm transition-transform duration-200",
                        on ? "translate-x-[15px]" : "translate-x-[2px]",
                    )}
                />
            </button>
        </div>
    );
}

// AIThinkingToggle — the "extended thinking" capability shown on every AI node
// (step + switch): route to the stronger model tier.
function AIThinkingToggle({
    config,
    patchConfig,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    return (
        <AIToggle
            label="Extended thinking"
            detail="Uses the stronger model with a bigger reasoning budget. Costs more through usage metering"
            on={!!config.thinking}
            onToggle={() => patchConfig({ thinking: !config.thinking })}
        />
    );
}

// AIStepFields is the unified AI step editor: a mode selector swaps between the
// single-shot transforms (classify/extract/generate) and the agent, and every
// mode can route to the stronger model tier. All write one shared config blob.
function AIStepFields({
    config,
    patchConfig,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    const mode = String(config.mode ?? "agent");
    return (
        <div className="space-y-3">
            <div>
                <Label>Mode</Label>
                <SelectMenu
                    value={mode}
                    onChange={(m) => patchConfig({ mode: m })}
                    options={AI_STEP_MODE_OPTIONS}
                    className="w-full"
                    fullWidth
                />
            </div>
            {mode === "classify" && <AIClassifyFields config={config} patchConfig={patchConfig} />}
            {mode === "extract" && <AIExtractFields config={config} patchConfig={patchConfig} />}
            {mode === "generate" && <AIGenerateFields config={config} patchConfig={patchConfig} />}
            {mode === "agent" && <AIAgentFields config={config} patchConfig={patchConfig} />}
            <div>
                <Label>Capabilities</Label>
                <AIThinkingToggle config={config} patchConfig={patchConfig} />
            </div>
        </div>
    );
}

// The allowlist ids whose agent tool draws from an optional tag/label pool.
const AI_POOL_KEY: Record<string, "ai_add_tags" | "ai_remove_tags" | "ai_labels"> = {
    "warmbly.add_tag": "ai_add_tags",
    "warmbly.remove_tag": "ai_remove_tags",
    "warmbly.label_email": "ai_labels",
};

// AITagPoolField — a multi-select pool of tags/labels the agent may use. The
// display name is stored alongside the id so the backend can offer the tag to
// the model and resolve its pick without a category lookup. Empty = any.
function AITagPoolField({
    label,
    value,
    onChange,
}: {
    label: string;
    value: AITagRef[];
    onChange: (refs: AITagRef[]) => void;
}) {
    const { user } = useUserProfile();
    const titleById = React.useMemo(() => {
        const m = new Map<string, string>();
        for (const c of user.categories ?? []) m.set(c.id, c.title);
        return m;
    }, [user.categories]);
    const knownName = React.useMemo(() => new Map(value.map((r) => [r.id, r.name])), [value]);
    return (
        <div>
            <Label>{label}</Label>
            <CategoryPicker
                value={value.map((r) => r.id)}
                onChange={(ids) => onChange(ids.map((id) => ({ id, name: titleById.get(id) ?? knownName.get(id) ?? id })))}
                placeholder="Any — leave empty to let the agent choose"
            />
            <p className="mt-1.5 text-[11px] text-slate-400">
                {value.length
                    ? "The agent chooses among these for each event."
                    : "Empty, so the agent may use any of your labels for each event."}
            </p>
        </div>
    );
}

// AIAgentFields — the agentic AI step: an instruction plus the guarded reversible
// actions the model may call, choosing which per event and writing task/deal
// details itself. Tag/label actions carry an optional pool (empty = any of your
// tags). It never sends or replies.
function AIAgentFields({
    config,
    patchConfig,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    const enabled = Array.isArray(config.allowed_actions) ? (config.allowed_actions as unknown[]).map(String) : [];
    const toggle = (id: string) =>
        patchConfig({
            allowed_actions: enabled.includes(id) ? enabled.filter((a) => a !== id) : [...enabled, id],
        });
    const poolFor = (id: string): AITagRef[] => {
        const key = AI_POOL_KEY[id];
        const v = key ? config[key] : undefined;
        return Array.isArray(v) ? (v as AITagRef[]) : [];
    };
    const anyOpenPool = Object.keys(AI_POOL_KEY).some((id) => enabled.includes(id) && poolFor(id).length === 0);
    return (
        <div className="space-y-3">
            <AIInstruction
                value={String(config.instruction ?? "")}
                onChange={(v) => patchConfig({ instruction: v })}
                placeholder="Read the reply. If they ask about pricing, label them 'pricing' and create a follow-up task."
            />
            <div>
                <Label>Actions the agent may take</Label>
                <div className="divide-y divide-slate-100 rounded-md border border-slate-200 p-1">
                    {AI_ALLOWLIST_ACTIONS.map((id) => {
                        const on = enabled.includes(id);
                        const poolKey = AI_POOL_KEY[id];
                        return (
                            <div key={id} className="py-0.5">
                                <button
                                    type="button"
                                    onClick={() => toggle(id)}
                                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[12.5px] text-slate-700 transition-colors hover:bg-slate-100"
                                >
                                    <span
                                        className={cn(
                                            "inline-flex size-4 shrink-0 items-center justify-center rounded border",
                                            on ? "border-sky-500 bg-sky-500 text-white" : "border-slate-300 bg-white",
                                        )}
                                    >
                                        {on && <CheckIcon className="w-3 h-3" />}
                                    </span>
                                    {actionLabel(id)}
                                </button>
                                {on && poolKey && (
                                    <div className="ml-[1.35rem] mt-1 space-y-3 border-l border-slate-200 pl-3 pb-1.5">
                                        <AITagPoolField
                                            label={
                                                id === "warmbly.add_tag"
                                                    ? "Labels the agent can add"
                                                    : id === "warmbly.remove_tag"
                                                      ? "Labels the agent can remove"
                                                      : "Conversation labels the agent can apply"
                                            }
                                            value={poolFor(id)}
                                            onChange={(refs) => patchConfig({ [poolKey]: refs })}
                                        />
                                    </div>
                                )}
                            </div>
                        );
                    })}
                </div>
                {anyOpenPool && (
                    <button
                        type="button"
                        onClick={() => patchConfig({ ai_allow_create_tags: !config.ai_allow_create_tags })}
                        className="mt-2 flex w-full items-center gap-2 rounded px-1.5 py-1.5 text-left text-[12px] text-slate-600 transition-colors hover:bg-slate-50"
                    >
                        <span
                            className={cn(
                                "inline-flex size-4 shrink-0 items-center justify-center rounded border",
                                config.ai_allow_create_tags ? "border-sky-500 bg-sky-500 text-white" : "border-slate-300 bg-white",
                            )}
                        >
                            {!!config.ai_allow_create_tags && <CheckIcon className="w-3 h-3" />}
                        </span>
                        Let the agent create a new label when none fits
                    </button>
                )}
                <p className="mt-1.5 text-[11px] text-slate-400 leading-relaxed">
                    The agent decides which of these to use for each event, writes any task or deal details itself, and
                    can chain several. It only takes these reversible actions and never sends or replies. Each step it
                    takes is charged to the workspace balance by what it uses, usually {fmtUsdAbout(AI_ESTIMATE_USD.agentIteration)}.
                </p>
            </div>
        </div>
    );
}

// AISwitchFields — the AI switch router, mirroring the campaign switch: pick the
// decider (an AI prompt over the event, or a value template matched to the case
// names). Each case is its own branch on the canvas.
function AISwitchFields({
    config,
    patchConfig,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
}) {
    const cases = Array.isArray(config.cases) ? (config.cases as unknown[]).map(String) : [];
    const aiMode = String(config.switch_on ?? "ai") !== "value";
    return (
        <div className="space-y-3">
            <div>
                <Label>Decided by</Label>
                <div className="grid grid-cols-2 gap-1.5">
                    {(
                        [
                            { ai: true, Icon: SparklesIcon, title: "AI prompt", detail: `A model reads the event and picks a case. ${fmtUsdAbout(AI_ESTIMATE_USD.aiNode)}.` },
                            { ai: false, Icon: BracesIcon, title: "Value", detail: "A field or template is matched to the cases. Free." },
                        ] as const
                    ).map(({ ai, Icon, title, detail }) => {
                        const active = aiMode === ai;
                        return (
                            <button
                                key={title}
                                type="button"
                                onClick={() => patchConfig({ switch_on: ai ? "ai" : "value" })}
                                className={cn(
                                    "flex items-start gap-1.5 rounded-md border px-2 py-1.5 text-left transition-colors",
                                    active ? "border-purple-300 bg-purple-50" : "border-slate-200 bg-white hover:border-slate-300",
                                )}
                            >
                                <Icon className={cn("mt-0.5 w-3.5 h-3.5 shrink-0", active ? "text-purple-600" : "text-slate-400")} />
                                <span className="min-w-0">
                                    <span className={cn("block text-[11.5px] font-medium", active ? "text-purple-700" : "text-slate-700")}>
                                        {title}
                                    </span>
                                    <span className="block text-[10.5px] leading-snug text-slate-400">{detail}</span>
                                </span>
                            </button>
                        );
                    })}
                </div>
            </div>

            {aiMode ? (
                <>
                    <AIInstruction
                        value={String(config.instruction ?? "")}
                        onChange={(v) => patchConfig({ instruction: v })}
                        placeholder="Decide which path fits this event."
                    />
                    <div>
                        <Label>Capabilities</Label>
                        <div className="space-y-1">
                            <AIToggle
                                label="Web search"
                                detail={`Looks up the event's company on the web before deciding. Adds ${fmtUsdAbout(AI_ESTIMATE_USD.webSearch)} when results are found`}
                                on={!!config.web_search}
                                onToggle={() => patchConfig({ web_search: !config.web_search })}
                            />
                            <AIThinkingToggle config={config} patchConfig={patchConfig} />
                        </div>
                    </div>
                </>
            ) : (
                <div>
                    <Label>Value to match</Label>
                    <TextInput
                        value={String(config.switch_value ?? "")}
                        onChange={(v) => patchConfig({ switch_value: v.slice(0, 500) })}
                        placeholder="e.g. {{.intent}}"
                        className="w-full font-mono"
                    />
                    <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                        Rendered against the event and matched to the case names. Matching ignores casing and extra
                        spaces; wrap a case in slashes for a regex, e.g. <code className="font-mono">/^(vip|enterprise)/</code>.
                        First match wins. No model call, no charge.
                    </p>
                </div>
            )}

            <AIStringList
                label="Cases"
                values={cases}
                onChange={(next) => patchConfig({ cases: next })}
                placeholder="interested"
                addLabel="Add case"
            />
            <p className="text-[11px] text-slate-400 leading-relaxed">
                Add at least two cases. Each case gets its own branch on the canvas; anything under Always runs whichever case is picked.
            </p>
        </div>
    );
}

// RunAnotherAutomationFields picks the automation to launch. It excludes self and
// flags a disabled / non-campaign-trigger target. Recursion + compute are bounded
// server-side by the chain-depth guard, so this stays safe even if chains nest.
function RunAnotherAutomationFields({
    config,
    patchConfig,
    selfId,
}: {
    config: Record<string, unknown>;
    patchConfig: (p: Record<string, unknown>) => void;
    selfId: string;
}) {
    const { data } = useAutomations();
    const all = (data?.automations ?? []).filter((a) => a.id !== selfId);
    const options: SelectOption[] = all.map((a) => ({
        value: a.id,
        label: (a.name || "Untitled automation") + (a.enabled ? "" : " · disabled"),
    }));
    const selected = all.find((a) => a.id === String(config.automation_id ?? ""));
    return (
        <div className="space-y-2">
            <div>
                <Label>Automation to run</Label>
                <SelectMenu
                    value={String(config.automation_id ?? "")}
                    onChange={(id) => patchConfig({ automation_id: id })}
                    options={options}
                    placeholder={options.length ? "Choose an automation…" : "No other automations yet"}
                    className="w-full"
                    fullWidth
                />
            </div>
            {selected && !selected.enabled && (
                <p className="inline-flex items-start gap-1.5 rounded-md border border-amber-200 bg-amber-50 px-2 py-1.5 text-[11px] leading-relaxed text-amber-700">
                    <TriangleAlertIcon className="mt-px w-3.5 h-3.5 shrink-0" /> This automation is disabled, so nothing runs until you enable it.
                </p>
            )}
            {selected && selected.enabled && selected.trigger_event !== "campaign.action" && (
                <p className="rounded-md border border-sky-200 bg-sky-50 px-2 py-1.5 text-[11px] leading-relaxed text-sky-700">
                    Built for the &quot;{triggerLabel(selected.trigger_event)}&quot; trigger. It still runs here, but only the variables present in this event are passed through.
                </p>
            )}
            <p className="text-[11px] leading-relaxed text-slate-400">
                The launched automation receives this event&apos;s data. Chains are depth-limited, so automations can&apos;t loop forever.
            </p>
        </div>
    );
}

// VarChips — clickable {{.variable}} fields that insert into a templatable field.
function VarChips({ vars, onPick }: { vars: string[]; onPick: (v: string) => void }) {
    if (!vars.length) return null;
    return (
        <div className="mt-1.5 flex flex-wrap gap-1">
            {vars.map((v) => (
                <button
                    key={v}
                    type="button"
                    onClick={() => onPick(v)}
                    title={`Insert {{.${v}}}`}
                    className="h-5 rounded border border-slate-200 bg-slate-50 px-1.5 font-mono text-[10.5px] text-slate-500 transition-colors hover:border-sky-300 hover:bg-sky-50 hover:text-sky-700"
                >
                    {`{{.${v}}}`}
                </button>
            ))}
        </div>
    );
}
