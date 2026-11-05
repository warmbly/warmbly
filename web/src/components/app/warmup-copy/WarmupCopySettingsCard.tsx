// The workspace's own-copy mix: how much of warmup carries it, whether running
// campaigns are warmed, and whether warmup sends only own copy.
import React from "react";
import toast from "react-hot-toast/headless";
import { NumberInput } from "@/components/ui/field";
import { SettingRow, Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { useUpdateWarmupCopySettings, useWarmupCopySettings } from "@/lib/api/hooks/app/warmup/useWarmupCopy";
import type { WarmupCopySettings } from "@/lib/api/models/app/warmup/WarmupCopy";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export default function WarmupCopySettingsCard() {
    const settings = useWarmupCopySettings();
    const update = useUpdateWarmupCopySettings();
    const saved = settings.data;
    // The share field's text while typing; saved on blur, Enter or a stepper click.
    const [share, setShare] = React.useState<number | null>(null);

    const save = (patch: Partial<WarmupCopySettings>) => {
        if (!saved) return;
        const next = { ...saved, ...patch };
        if (next.template_share === saved.template_share && next.templates_only === saved.templates_only && next.campaign_copy === saved.campaign_copy) {
            return;
        }
        update.mutate(next, {
            onError: (err) => toast.error(buildError(err as unknown as AppError)),
        });
    };

    if (settings.isLoading) {
        return (
            <div className="px-5 py-4 space-y-3 border-b border-slate-200">
                {[0, 1, 2].map((i) => (
                    <div key={i} className="h-9 rounded bg-slate-50 animate-pulse" />
                ))}
            </div>
        );
    }
    if (!saved) {
        return (
            <div className="px-5 py-4 border-b border-slate-200 text-[12px] text-rose-600">
                {settings.error ? buildError(settings.error as unknown as AppError) : "Settings could not be loaded."}
            </div>
        );
    }

    return (
        <div className="px-5 py-4 border-b border-slate-200 space-y-4">
            <SettingRow
                title="Share of new warmup threads"
                description={
                    saved.templates_only
                        ? "Every new thread uses your own copy while only your own copy is on."
                        : "Running campaign copy is used first. Templates fill whatever share it leaves."
                }
                control={
                    <NumberInput
                        value={share ?? saved.template_share}
                        onChange={setShare}
                        onCommit={(v) => {
                            setShare(null);
                            save({ template_share: v });
                        }}
                        min={0}
                        max={100}
                        suffix="%"
                        align="right"
                        disabled={saved.templates_only}
                        className="w-24"
                    />
                }
            />
            <SettingRow
                title="Warm up running campaigns"
                description="While a campaign is active, warmup sends its opening steps as the campaign renders them."
                control={
                    <Toggle
                        value={saved.campaign_copy}
                        onChange={(v) => save({ campaign_copy: v })}
                        ariaLabel="Warm up running campaigns"
                    />
                }
            />
            <SettingRow
                title="Only send my own copy"
                description="Every new thread uses a campaign step or template, repeating the one sent longest ago when needed. Ordinary conversations are used only if there is no eligible copy at all."
                control={
                    <Toggle
                        value={saved.templates_only}
                        onChange={(v) => save({ templates_only: v })}
                        ariaLabel="Only send my own copy"
                    />
                }
            />
            <p className="text-[11px] text-slate-400 leading-relaxed">
                A mailbox carries your own copy only after its first 7 days of warmup. Until then it warms on ordinary
                conversations, whatever is set here.
            </p>
        </div>
    );
}
