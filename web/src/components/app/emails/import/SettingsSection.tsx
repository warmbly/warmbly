// "Settings for every mailbox" and "If a mailbox is already connected": the
// choices every import asks before it starts, whatever the rows came from.
import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { ChevronDownIcon } from "lucide-react";
import type { MailboxImportSettings, OnExisting } from "@/lib/api/models/app/emails/MailboxImport";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import TagSelector from "@/components/app/popup/select/TagSelector";
import { OptionSelect } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import useFeatureStatus from "@/lib/api/hooks/app/subscription/useFeatureStatus";
import { useUserProfile } from "@/hooks/context/user";
import { cn } from "@/lib/utils";
import { plural } from "./importFields";

// Product defaults for a new mailbox; mirrors internal/config/constants.go.
const DEFAULTS = {
    daily_limit: 50,
    min_wait: 600,
    warmup_start: 10,
    warmup_max: 40,
    warmup_increase: 1,
    warmup_reply_rate: 30,
};

type WarmupChoice = "rows" | "on" | "off";

// What "update" replaces depends on what the rows carry.
const UPDATE_COPY: Record<"file" | "vendor" | "grant", { label: string; hint: string }> = {
    file: { label: "Update password and settings", hint: "The row's password, servers and settings replace the stored ones." },
    vendor: { label: "Update credentials and settings", hint: "The vendor's current credentials and the settings below replace the stored ones." },
    grant: { label: "Update settings", hint: "The settings below replace the stored ones. The grant keeps signing it in." },
};

export function OnExistingChoice({
    source,
    value,
    onChange,
}: {
    source: "file" | "vendor" | "grant";
    value: OnExisting;
    onChange: (v: OnExisting) => void;
}) {
    const update = UPDATE_COPY[source];
    return (
        <div>
            <Label>If a mailbox is already connected</Label>
            <OptionSelect<OnExisting>
                value={value}
                onChange={onChange}
                cols={2}
                aria-label="If a mailbox is already connected"
                options={[
                    { value: "update", label: update.label, hint: update.hint },
                    { value: "skip", label: "Skip", hint: "Leave the connected mailbox exactly as it is." },
                ]}
            />
        </div>
    );
}

export function SettingsSection({
    settings,
    setSettings,
    note,
    fromFile = false,
}: {
    settings: MailboxImportSettings;
    setSettings: React.Dispatch<React.SetStateAction<MailboxImportSettings>>;
    /** A line under the header, e.g. what wins over these values. */
    note?: string;
    /** The rows come from a file whose columns can carry their own settings. */
    fromFile?: boolean;
}) {
    const [open, setOpen] = React.useState(false);
    const profile = useUserProfile();
    const canWarmup = useFeatureStatus().data?.can_use_warmup !== false;
    // An emptied value drops its key, so only real changes travel and count.
    const set = <K extends keyof MailboxImportSettings>(k: K, v: MailboxImportSettings[K]) =>
        setSettings((prev) => {
            const next = { ...prev };
            if (v === undefined || (Array.isArray(v) && v.length === 0)) delete next[k];
            else next[k] = v;
            return next;
        });

    const tagIds = settings.tag_ids ?? [];
    const dailyLimit = settings.daily_limit ?? DEFAULTS.daily_limit;
    const minutes = Math.round((settings.min_wait ?? DEFAULTS.min_wait) / 60);
    // Unset leaves each row's own warmup value (or the mailbox) as it is; an explicit off wins over every row.
    const warmupChoice: WarmupChoice = settings.warmup === undefined ? "rows" : settings.warmup ? "on" : "off";
    const warmup = canWarmup && warmupChoice !== "off";
    const changed = Object.keys(settings).length;

    const timezoneOptions = React.useMemo<SelectOption[]>(
        () => [
            { value: "", label: "Workspace default" },
            ...(profile?.timezones ?? []).map((tz) => ({ value: tz.name, label: tz.display_name })),
        ],
        [profile?.timezones],
    );

    return (
        <div className="rounded-md border border-slate-200">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                aria-expanded={open}
                className="w-full h-10 px-3 flex items-center gap-2 text-left hover:bg-slate-50/60 transition-colors"
            >
                <span className="text-[12.5px] font-medium text-slate-900">Settings for every mailbox</span>
                <span className="text-[11.5px] text-slate-500 truncate">
                    {changed > 0 ? `${plural(changed, "change", "changes")} from the defaults` : note ? "Defaults, unless a column says otherwise" : "Defaults"}
                </span>
                <ChevronDownIcon className={cn("ml-auto w-3.5 h-3.5 text-slate-400 transition-transform shrink-0", open && "rotate-180")} />
            </button>
            <AnimatePresence initial={false}>
                {open && (
                    <motion.div
                        key="settings"
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.2, ease: [0.32, 0.72, 0, 1] }}
                        className="overflow-hidden"
                    >
                        <div className="px-3 pb-3 pt-1 space-y-3 border-t border-slate-100">
                            {note && <p className="text-[11.5px] text-slate-500 pt-2">{note}</p>}
                            <div>
                                <Label>Tags</Label>
                                <TagSelector
                                    selected={tagIds}
                                    onAdd={(id) => set("tag_ids", [...tagIds, id])}
                                    onRemove={(id) => set("tag_ids", tagIds.filter((t) => t !== id))}
                                />
                            </div>
                            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                <div>
                                    <Label>Daily limit</Label>
                                    <NumberInput value={dailyLimit} min={1} max={5000} onChange={(v) => set("daily_limit", v)} suffix="/ day" className="w-full" />
                                    {dailyLimit > 100 && (
                                        <p className="mt-1 text-[11px] text-amber-700">Above 100 a day only suits a proven mailbox with low complaints.</p>
                                    )}
                                </div>
                                <div>
                                    <Label>Minutes between sends</Label>
                                    <NumberInput value={minutes} min={1} max={1440} onChange={(v) => set("min_wait", v * 60)} suffix="min" className="w-full" />
                                </div>
                            </div>
                            <div className="rounded-md border border-slate-200 p-2.5 space-y-2.5">
                                <div className="min-w-0">
                                    <div className="text-[12.5px] text-slate-900 font-medium">Warmup</div>
                                    {!canWarmup && <div className="text-[11.5px] text-slate-500">Warmup is available on paid plans.</div>}
                                </div>
                                {canWarmup && (
                                    <OptionSelect<WarmupChoice>
                                        value={warmupChoice}
                                        onChange={(v) => set("warmup", v === "rows" ? undefined : v === "on")}
                                        cols={3}
                                        aria-label="Warmup"
                                        options={[
                                            {
                                                value: "rows",
                                                label: fromFile ? "As each row says" : "Leave as it is",
                                                hint: fromFile ? "A row's warmup column decides. Without one, warmup is left as it is." : "New mailboxes start without warmup; connected ones keep theirs.",
                                            },
                                            { value: "on", label: "On", hint: fromFile ? "Starts and ramps up slowly, unless a row turns it off." : "Starts on every mailbox and ramps up slowly." },
                                            { value: "off", label: "Off", hint: fromFile ? "Off on every mailbox the import touches, whatever a row says." : "Off on every mailbox the import touches." },
                                        ]}
                                    />
                                )}
                                {warmup && (
                                    <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
                                        <div>
                                            <Label>Start</Label>
                                            <NumberInput value={settings.warmup_start ?? DEFAULTS.warmup_start} min={1} max={500} onChange={(v) => set("warmup_start", v)} suffix="/ day" className="w-full" />
                                        </div>
                                        <div>
                                            <Label>Max</Label>
                                            <NumberInput value={settings.warmup_max ?? DEFAULTS.warmup_max} min={1} max={500} onChange={(v) => set("warmup_max", v)} suffix="/ day" className="w-full" />
                                        </div>
                                        <div>
                                            <Label>Daily increase</Label>
                                            <NumberInput value={settings.warmup_increase ?? DEFAULTS.warmup_increase} min={0} max={100} onChange={(v) => set("warmup_increase", v)} className="w-full" />
                                        </div>
                                        <div>
                                            <Label>Reply rate</Label>
                                            <NumberInput value={settings.warmup_reply_rate ?? DEFAULTS.warmup_reply_rate} min={0} max={100} onChange={(v) => set("warmup_reply_rate", v)} suffix="%" className="w-full" />
                                        </div>
                                    </div>
                                )}
                            </div>
                            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                <div>
                                    <Label>Reply-to</Label>
                                    <TextInput
                                        value={settings.reply_to ?? ""}
                                        onChange={(v) => set("reply_to", v || undefined)}
                                        placeholder="Replies go to the mailbox itself"
                                        className="w-full"
                                    />
                                </div>
                                <div>
                                    <Label>Timezone</Label>
                                    <SelectMenu
                                        value={settings.timezone ?? ""}
                                        onChange={(v) => set("timezone", v || undefined)}
                                        options={timezoneOptions}
                                        fullWidth
                                        aria-label="Timezone"
                                    />
                                </div>
                            </div>
                            {changed > 0 && (
                                <button
                                    type="button"
                                    onClick={() => setSettings({})}
                                    className="text-[11.5px] text-slate-500 hover:text-slate-900 underline"
                                >
                                    Back to the defaults
                                </button>
                            )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
