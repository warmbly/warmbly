// "Use your own GraphKit workspace": link it (GraphKit's consent page returns to /app/settings/social?link_session=…), or unlink it.
import { ExternalLinkIcon, Link2Icon, Loader2Icon, UnlinkIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import { useStartSocialLink, useUnlinkSocial } from "@/lib/api/hooks/app/social/useSocial";
import type { SocialStatus } from "@/lib/api/models/app/social/Social";
import { socialErrorMessage } from "@/lib/social/errors";
import { cn } from "@/lib/utils";
import { fmtDay } from "./parts";

export default function GraphKitLinkCard({ status, completing, className }: { status: SocialStatus; completing: boolean; className?: string }) {
    const confirm = useConfirm();
    const canBill = usePermission("MANAGE_BILLING");
    const start = useStartSocialLink();
    const unlink = useUnlinkSocial();
    const linked = status.mode === "linked";
    const busy = start.isPending || completing;

    const link = () =>
        start.mutate(undefined, {
            onSuccess: (r) => {
                if (r.url.startsWith("https://") || r.url.startsWith("http://localhost")) window.location.assign(r.url);
            },
        });

    const doUnlink = () =>
        confirm.show(
            `Unlink ${status.linked_workspace?.name ?? "your GraphKit workspace"}? Its social accounts leave this workspace and stop running here. They stay in GraphKit.`,
            async () => {
                try {
                    await unlink.mutateAsync();
                    toast.success("GraphKit workspace unlinked.");
                } catch (e) {
                    toast.error(socialErrorMessage(e));
                }
            },
            undefined,
            { title: "Unlink GraphKit", confirmLabel: "Unlink", tone: "danger" },
        );

    return (
        <section className={cn("rounded-lg border border-slate-200 bg-white px-4 py-3.5", className)} aria-label="Your own GraphKit workspace">
            <div className="flex items-start gap-3">
                <span className="size-8 rounded-md bg-slate-100 text-slate-600 inline-flex items-center justify-center shrink-0">
                    <Link2Icon className="w-4 h-4" />
                </span>
                <div className="min-w-0 flex-1">
                    {linked ? (
                        <>
                            <p className="text-[12.5px] font-semibold text-slate-900">
                                Linked to {status.linked_workspace?.name ?? "your GraphKit workspace"}
                            </p>
                            <p className="mt-0.5 text-[11.5px] text-slate-500 leading-relaxed">
                                Social accounts are billed by your GraphKit plan, never by Warmbly
                                {status.linked_at ? `. Linked on ${fmtDay(status.linked_at)}.` : "."}
                            </p>
                        </>
                    ) : (
                        <>
                            <p className="text-[12.5px] font-semibold text-slate-900">Use your own GraphKit workspace</p>
                            <p className="mt-0.5 text-[11.5px] text-slate-500 leading-relaxed">
                                Already pay for social accounts in GraphKit? Link the workspace and run them from Warmbly with nothing charged here. Works on any Warmbly plan.
                            </p>
                        </>
                    )}
                </div>
                {linked ? (
                    <button
                        type="button"
                        onClick={doUnlink}
                        disabled={!canBill || unlink.isPending}
                        title={canBill ? undefined : "Unlinking needs the Manage billing permission."}
                        className="h-7 px-2.5 rounded-md border border-slate-200 bg-white hover:bg-slate-50 disabled:opacity-60 text-slate-700 text-[12px] font-medium inline-flex items-center gap-1.5 shrink-0"
                    >
                        {unlink.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <UnlinkIcon className="w-3 h-3" />}
                        Unlink
                    </button>
                ) : (
                    <button
                        type="button"
                        onClick={link}
                        disabled={!canBill || busy}
                        title={canBill ? undefined : "Linking needs the Manage billing permission."}
                        className="h-7 px-2.5 rounded-md border border-slate-200 bg-white hover:bg-slate-50 disabled:opacity-60 text-slate-700 text-[12px] font-medium inline-flex items-center gap-1.5 shrink-0"
                    >
                        {busy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ExternalLinkIcon className="w-3 h-3" />}
                        {completing ? "Linking" : "Link GraphKit"}
                    </button>
                )}
            </div>
            {!canBill && <p className="mt-2 text-[11px] text-slate-400">Ask someone with the Manage billing permission to {linked ? "unlink" : "link"} it.</p>}
        </section>
    );
}
