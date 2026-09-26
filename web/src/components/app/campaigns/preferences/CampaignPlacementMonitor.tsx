// A campaign's scheduled placement test: every few days its first email step
// is sent to a seed panel from one of its mailboxes, and a low inbox rate
// alerts the team (and can pause the campaign). Saves as it changes, through
// its own endpoint, apart from the campaign's save bar.

import React from "react";
import { Link } from "react-router-dom";
import { AlertTriangleIcon, ArrowUpRightIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast";
import { Label, NumberInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import {
    useDeletePlacementMonitor,
    usePlacementMonitor,
    usePlacementOverview,
    usePutPlacementMonitor,
} from "@/lib/api/hooks/app/placement/usePlacement";
import {
    PANEL_LABEL,
    PLACEMENT_MONITOR_INTERVAL_MAX,
    PLACEMENT_MONITOR_INTERVAL_MIN,
    type PlacementMonitorInput,
    type PlacementPanel,
} from "@/lib/api/models/app/placement/Placement";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { fmtDate } from "@/components/app/placement/tests/placementTests";
import { SettingRow, Toggle } from "./components/CampaignPreferenceBoolBox";

// Defaults a new monitor starts from, matching the backend's.
const DEFAULT_INTERVAL = 7;
const DEFAULT_ALERT_BELOW = 70;

export function PlacementMonitorSection({ campaignId }: { campaignId: string }) {
    const monitor = usePlacementMonitor(campaignId);
    const overview = usePlacementOverview();
    const put = usePutPlacementMonitor(campaignId);
    const remove = useDeletePlacementMonitor(campaignId);
    const confirm = useConfirm();
    const canEdit = usePermission("SEND_CAMPAIGNS");

    const m = monitor.data ?? null;
    const [intervalDays, setIntervalDays] = React.useState(m?.interval_days ?? DEFAULT_INTERVAL);
    const [alertBelow, setAlertBelow] = React.useState(m?.alert_below ?? DEFAULT_ALERT_BELOW);
    const [panel, setPanel] = React.useState<PlacementPanel>(m?.panel ?? "instance");
    const [pauseOnAlert, setPauseOnAlert] = React.useState(m?.pause_on_alert ?? false);

    // A teammate's edit lands live, so the controls follow the stored monitor.
    React.useEffect(() => {
        if (!m) return;
        setIntervalDays(m.interval_days);
        setAlertBelow(m.alert_below);
        setPanel(m.panel);
        setPauseOnAlert(m.pause_on_alert);
    }, [m]);

    const save = async (input: PlacementMonitorInput) => {
        try {
            await put.mutateAsync(input);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    // Only an existing monitor saves field by field; a new one is created with
    // every value at once when it is switched on.
    const change = (input: PlacementMonitorInput) => {
        if (m) void save(input);
    };

    const enabled = !!m?.enabled;
    const toggle = (on: boolean) => {
        if (!m) {
            if (on) void save({ enabled: true, interval_days: intervalDays, alert_below: alertBelow, panel, pause_on_alert: pauseOnAlert });
            return;
        }
        void save({ enabled: on });
    };

    const onRemove = () =>
        confirm.show("Remove this campaign's placement monitor? Its past tests stay in Placement tests.", async () => {
            try {
                await remove.mutateAsync();
                toast.success("Placement monitor removed.");
            } catch (e) {
                toast.error(buildError(e as AppError));
            }
        });

    const panels = overview.data?.panels ?? [];
    const panelOptions = (panels.length > 0 ? panels : [{ panel: "instance" as const, available: true, reason: "" }]).map((p) => ({
        value: p.panel,
        label: p.available ? PANEL_LABEL[p.panel] : `${PANEL_LABEL[p.panel]} (unavailable)`,
        disabled: !p.available && p.panel !== panel,
    }));
    const selectedPanel = panels.find((p) => p.panel === panel);

    if (monitor.isLoading) {
        return (
            <div className="h-10 flex items-center">
                <Loader2Icon className="w-4 h-4 animate-spin text-slate-300" />
            </div>
        );
    }

    const disabled = !canEdit || put.isPending;

    return (
        <div className="space-y-5">
            {m?.last_error && (
                <div className="rounded-md border border-amber-100 bg-amber-50/70 px-3 py-2.5 flex gap-2.5">
                    <AlertTriangleIcon className="w-4 h-4 text-amber-600 shrink-0 mt-0.5" />
                    <p className="text-[11.5px] text-amber-900/90 leading-relaxed">The last scheduled test did not run: {m.last_error}</p>
                </div>
            )}

            <SettingRow
                title="Placement monitor"
                description="Re-tests this campaign's first email every few days from one of its mailboxes, taking turns, and tells your team when too little of it reaches the inbox."
                control={
                    <span title={!canEdit ? "You need the Send campaigns permission" : undefined} className="inline-flex items-center gap-2">
                        {put.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin text-slate-400" />}
                        <Toggle id="campaign-pref-placement-monitor" value={enabled} onChange={toggle} disabled={disabled} />
                    </span>
                }
            />

            <div className="rounded-md border border-slate-200 bg-slate-50/40 p-3.5 space-y-4">
                <div className="flex flex-wrap items-end gap-4">
                    <div>
                        <Label>Test every</Label>
                        <NumberInput
                            value={intervalDays}
                            min={PLACEMENT_MONITOR_INTERVAL_MIN}
                            max={PLACEMENT_MONITOR_INTERVAL_MAX}
                            onChange={setIntervalDays}
                            onCommit={(v) => {
                                if (v !== m?.interval_days) change({ interval_days: v });
                            }}
                            suffix="days"
                            disabled={!canEdit}
                            className="w-36"
                        />
                    </div>
                    <div>
                        <Label>Alert below inbox rate</Label>
                        <NumberInput
                            value={alertBelow}
                            min={0}
                            max={100}
                            onChange={setAlertBelow}
                            onCommit={(v) => {
                                if (v !== m?.alert_below) change({ alert_below: v });
                            }}
                            suffix="%"
                            disabled={!canEdit}
                            className="w-36"
                        />
                    </div>
                    <div className="min-w-[200px]">
                        <Label>Seed panel</Label>
                        <SelectMenu
                            value={panel}
                            onChange={(v) => {
                                setPanel(v as PlacementPanel);
                                change({ panel: v as PlacementPanel });
                            }}
                            options={panelOptions}
                            disabled={!canEdit}
                            fullWidth
                            aria-label="Seed panel"
                        />
                    </div>
                </div>
                {selectedPanel && !selectedPanel.available && (
                    <p className="text-[11px] text-amber-600">{selectedPanel.reason || "This panel cannot run a test right now."}</p>
                )}
                {selectedPanel?.metered && (
                    <p className="text-[11px] text-slate-500">Each scheduled test counts toward your monthly placement tests.</p>
                )}

                <SettingRow
                    title="Pause the campaign on an alert"
                    description="Stops sending when a scheduled test falls below the alert rate. Starting the campaign again clears it."
                    control={
                        <Toggle
                            value={pauseOnAlert}
                            onChange={(v) => {
                                setPauseOnAlert(v);
                                change({ pause_on_alert: v });
                            }}
                            disabled={!canEdit}
                        />
                    }
                />

                {m && (
                    <dl className="grid grid-cols-1 sm:grid-cols-3 gap-3 pt-1">
                        <div>
                            <dt className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Last run</dt>
                            <dd className="mt-0.5 text-[12px] text-slate-700">
                                {m.last_run_at ? fmtDate(m.last_run_at) : "Not yet"}
                                {m.last_test_id && (
                                    <Link
                                        to={`/app/placement/${m.last_test_id}`}
                                        className="ml-1.5 inline-flex items-center gap-0.5 text-sky-700 hover:text-sky-800"
                                    >
                                        View
                                        <ArrowUpRightIcon className="w-3 h-3" />
                                    </Link>
                                )}
                            </dd>
                        </div>
                        <div>
                            <dt className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Next run</dt>
                            <dd className="mt-0.5 text-[12px] text-slate-700">{m.enabled ? fmtDate(m.next_run_at) : "Off"}</dd>
                        </div>
                        <div>
                            <dt className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Last alert</dt>
                            <dd className="mt-0.5 text-[12px] text-slate-700">{m.last_alert_at ? fmtDate(m.last_alert_at) : "None"}</dd>
                        </div>
                    </dl>
                )}

                <p className="text-[11px] text-slate-500 leading-relaxed">
                    {m
                        ? "Changes save as you make them."
                        : "Switch it on to start. The first test runs within a few minutes."}{" "}
                    Every copy counts against the sending mailbox&apos;s daily limit.
                </p>
            </div>

            <div className="flex flex-wrap items-center gap-3">
                <Link
                    to={`/app/placement?campaign_id=${campaignId}`}
                    className="inline-flex items-center gap-1 text-[12px] text-sky-700 hover:text-sky-800"
                >
                    This campaign&apos;s placement tests
                    <ArrowUpRightIcon className="w-3 h-3" />
                </Link>
                {m && canEdit && (
                    <button
                        type="button"
                        onClick={onRemove}
                        disabled={remove.isPending}
                        className="text-[12px] text-slate-500 hover:text-rose-600 transition-colors disabled:opacity-60"
                    >
                        Remove monitor
                    </button>
                )}
            </div>
        </div>
    );
}
