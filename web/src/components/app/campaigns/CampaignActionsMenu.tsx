// The campaign "⋯" menu: Edit, Duplicate, Start/Pause, Delete. Used by the
// campaigns list row (compact, hover-revealed trigger) and the campaign
// detail header. Destructive and copy actions are permission-gated: a member
// without manage_campaigns gets the standard explanation instead of a request.

import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import {
    CopyIcon,
    MoreHorizontalIcon,
    PauseIcon,
    PencilIcon,
    PlayIcon,
    TrashIcon,
} from "lucide-react";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { cn } from "@/lib/utils";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import { canStartCampaign, gate, useCampaignActions } from "./useCampaignActions";
import { useAccessRestricted } from "@/hooks/usePermission";

interface Props {
    campaign: Campaign;
    variant: "row" | "header";
    /** Start or pause handler. Omit to hide the item. */
    onToggle?: () => void;
    /** Runs after a successful delete (the detail header navigates away). */
    afterDelete?: () => void;
}

export default function CampaignActionsMenu({ campaign, variant, onToggle, afterDelete }: Props) {
    const [open, setOpen] = useState(false);
    const navigate = useNavigate();
    const actions = useCampaignActions();

    const status = campaign.status ?? "draft";
    const isActive = status === "active";
    const showToggle = onToggle !== undefined && (isActive || canStartCampaign(status));
    // Settings, copies and deletion reach past a restricted member's grants; they keep start and pause.
    const restricted = useAccessRestricted();
    if (restricted && !showToggle) return null;

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    aria-label={`Actions for ${campaign.name}`}
                    title="More actions"
                    className={cn(
                        "inline-flex items-center justify-center shrink-0 transition-colors",
                        variant === "row"
                            ? "size-6 rounded text-slate-400 hover:text-slate-900 hover:bg-slate-100 opacity-100 md:opacity-0 md:group-hover:opacity-100 aria-expanded:opacity-100 transition-opacity"
                            : "h-7 w-7 rounded-md border border-slate-200 text-slate-500 hover:text-slate-900 hover:bg-slate-50",
                    )}
                >
                    <MoreHorizontalIcon className="w-3.5 h-3.5" />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={184}>
                {!restricted && (
                    <>
                        <PopoverMenuItem
                            onSelect={() => navigate({ to: "/app/campaigns/$id/preferences", params: { id: campaign.id } })}
                            icon={<PencilIcon className="w-3 h-3" />}
                        >
                            {variant === "row" ? "Edit" : "Edit settings"}
                        </PopoverMenuItem>
                        <PopoverMenuItem
                            onSelect={gate("MANAGE_CAMPAIGNS", () => actions.duplicateAndOpen(campaign))}
                            disabled={actions.duplicating}
                            icon={<CopyIcon className="w-3 h-3" />}
                        >
                            Duplicate
                        </PopoverMenuItem>
                    </>
                )}
                {showToggle && (
                    <PopoverMenuItem
                        onSelect={gate("SEND_CAMPAIGNS", () => onToggle?.())}
                        icon={
                            isActive ? (
                                <PauseIcon className="w-3 h-3" />
                            ) : (
                                <PlayIcon className="w-3 h-3" />
                            )
                        }
                    >
                        {isActive ? "Pause" : "Start"}
                    </PopoverMenuItem>
                )}
                {!restricted && (
                    <>
                        <PopoverMenuSeparator />
                        <PopoverMenuItem
                            danger
                            onSelect={gate("MANAGE_CAMPAIGNS", () => actions.requestDelete(campaign, { afterDelete }))}
                            disabled={actions.deleting}
                            icon={<TrashIcon className="w-3 h-3" />}
                        >
                            Delete
                        </PopoverMenuItem>
                    </>
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
