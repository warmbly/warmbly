// App-wide banner for a workspace its reseller paused (unpaid, or cancelled). Not dismissible:
// the workspace stays read-only, with sending and warmup stopped, until the reseller resumes it.

import { PauseCircleIcon } from "lucide-react";
import usePartnerManaged from "@/hooks/usePartnerManaged";
import { partnerPaused, partnerPauseDetail, partnerPauseTitle } from "@/lib/partnerPause";

export default function PartnerPausedBar() {
    const partner = usePartnerManaged();
    if (!partnerPaused(partner)) return null;

    return (
        <div role="status" className="flex items-start gap-2 px-4 py-2.5 border-b bg-amber-50 border-amber-200 text-amber-900">
            <PauseCircleIcon className="w-4 h-4 mt-px shrink-0 text-amber-600" />
            <div className="min-w-0 text-[12.5px] leading-relaxed">
                <span className="font-medium">{partnerPauseTitle(partner)}</span> {partnerPauseDetail(partner)}
            </div>
        </div>
    );
}
