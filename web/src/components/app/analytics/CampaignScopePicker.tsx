import React from "react";
import { MegaphoneIcon } from "lucide-react";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import { CheckSquare } from "@/components/ui/check-square";
import { useUserProfile } from "@/hooks/context/user";
import useDebouncedValue from "@/hooks/useDebouncedValue";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import type { DashboardCampaignFilter, DashboardScope } from "@/lib/api/models/app/analytics/DashboardOverview";
import { ALL_CAMPAIGNS, isScoped, scopeLabel } from "./campaignScope";

// The Analytics page's campaign filter: every campaign, or any mix of
// folders and campaigns, which the server unions and counts once each.
export default function CampaignScopePicker({
    value,
    onChange,
    scope,
}: {
    value: DashboardCampaignFilter;
    onChange: (next: DashboardCampaignFilter) => void;
    scope?: DashboardScope;
}) {
    const { user } = useUserProfile();
    const folders = user.folders ?? [];
    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");
    const search = useDebouncedValue(query.trim());
    // Only listed while the menu is open; the trigger reads names from the server's echo.
    const list = useCampaigns({ query: search, folder: "", enabled: open });

    const needle = query.trim().toLowerCase();
    const shownFolders = needle ? folders.filter((f) => f.title.toLowerCase().includes(needle)) : folders;
    const label = scopeLabel(
        value,
        scope,
        (id) => folders.find((f) => f.id === id)?.title,
        (id) => list.campaigns.find((c) => c.id === id)?.name,
    );

    const toggle = (key: keyof DashboardCampaignFilter, id: string) => {
        const current = value[key];
        onChange({ ...value, [key]: current.includes(id) ? current.filter((x) => x !== id) : [...current, id] });
    };

    return (
        <PopoverMenu
            align="end"
            open={open}
            onOpenChange={(o) => {
                setOpen(o);
                if (!o) setQuery("");
            }}
        >
            <PopoverMenuTrigger asChild>
                <SelectButton
                    icon={<MegaphoneIcon className="w-3 h-3" />}
                    label={label}
                    title={label}
                    aria-label={`Campaign filter: ${label}`}
                    className={isScoped(value) ? "border-sky-300 bg-sky-50 text-sky-800 hover:border-sky-400" : undefined}
                />
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={300} className="max-h-[min(440px,calc(100dvh-16px))]">
                <div className="px-2 py-1.5 border-b border-slate-200">
                    <input
                        value={query}
                        onChange={(e) => setQuery(e.target.value)}
                        placeholder="Search campaigns and folders…"
                        aria-label="Search campaigns and folders"
                        className="w-full h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                    />
                </div>
                <PopoverMenuItem selected={!isScoped(value)} onSelect={() => onChange(ALL_CAMPAIGNS)}>
                    All campaigns
                </PopoverMenuItem>

                {shownFolders.length > 0 && (
                    <>
                        <PopoverMenuSeparator />
                        <PopoverMenuLabel>Folders</PopoverMenuLabel>
                        {shownFolders.map((f) => (
                            <PopoverMenuItem
                                key={f.id}
                                closeOnSelect={false}
                                icon={<CheckSquare checked={value.folders.includes(f.id)} />}
                                trailing={<span className="block size-2 rounded-full" style={{ backgroundColor: f.color }} />}
                                onSelect={() => toggle("folders", f.id)}
                            >
                                {f.title}
                            </PopoverMenuItem>
                        ))}
                    </>
                )}

                <PopoverMenuSeparator />
                <PopoverMenuLabel>Campaigns</PopoverMenuLabel>
                {list.campaigns.map((c) => (
                    <PopoverMenuItem
                        key={c.id}
                        closeOnSelect={false}
                        icon={<CheckSquare checked={value.campaigns.includes(c.id)} />}
                        trailing={
                            <span
                                className={`block size-1.5 rounded-full ${
                                    c.status === "active" ? "bg-emerald-500" : c.status === "paused" ? "bg-amber-500" : "bg-slate-300"
                                }`}
                            />
                        }
                        onSelect={() => toggle("campaigns", c.id)}
                    >
                        {c.name}
                    </PopoverMenuItem>
                ))}
                {list.campaigns.length === 0 && (
                    <div className="px-3 py-2 text-[11.5px] text-slate-400">
                        {list.isPending ? "Loading…" : list.isError ? "Couldn't load campaigns." : "No campaigns found."}
                    </div>
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
