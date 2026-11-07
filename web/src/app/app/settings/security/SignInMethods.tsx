import toast from "react-hot-toast/headless";
import { Link2, Trash2 } from "lucide-react";
import { Section } from "../_components/SectionShell";
import { Loading } from "@/components/loader";
import { useConfirm } from "@/hooks/context/confirm";
import useLinkedIdentities from "@/lib/api/hooks/auth/useLinkedIdentities";
import useUnlinkIdentity from "@/lib/api/hooks/auth/useUnlinkIdentity";
import type LinkedIdentity from "@/lib/api/models/auth/LinkedIdentity";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

const fmt = (d: string) =>
    new Date(d).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });

// Google, Apple and single sign-on linked to this account. Hidden when none are.
export default function SignInMethods() {
    const { data: identities, isLoading } = useLinkedIdentities();
    const unlink = useUnlinkIdentity();
    const confirm = useConfirm();

    if (!isLoading && !identities?.length) return null;

    const remove = (id: LinkedIdentity) => {
        confirm.show(
            `Stop signing in with ${id.label}? Other devices that signed in with ${id.label} are signed out. Your workspaces and connected mailboxes are not affected.`,
            async () => {
                try {
                    await unlink.mutateAsync(id.provider);
                    toast.success(`${id.label} removed`);
                } catch (err) {
                    toast.error(buildError(err as AppError));
                }
            },
        );
    };

    return (
        <Section
            eyebrow="Sign-in methods"
            description="Providers you can sign in with. Removing one does not change your email address or touch mailboxes you send from."
        >
            {isLoading ? (
                <div className="flex items-center gap-2 text-[12px] text-slate-400 py-2">
                    <Loading className="!w-4 h-4 text-slate-400" /> Loading sign-in methods…
                </div>
            ) : (
                <div className="rounded-md border border-slate-200 divide-y divide-slate-200 bg-white">
                    {identities?.map((id) => (
                        <div key={id.provider} className="flex items-center gap-3 px-3 py-2.5">
                            <div className="w-8 h-8 rounded-lg bg-sky-50 flex items-center justify-center shrink-0">
                                <Link2 className="w-4 h-4 text-sky-500" />
                            </div>
                            <div className="min-w-0 flex-1">
                                <div className="text-[12.5px] font-medium text-slate-900 truncate">{id.label}</div>
                                <div className="text-[11px] text-slate-500 truncate mt-0.5">
                                    {id.email ? `${id.email} · ` : ""}Linked {fmt(id.created_at)}
                                    {id.last_login_at ? ` · Last used ${fmt(id.last_login_at)}` : ""}
                                </div>
                            </div>
                            <button
                                type="button"
                                onClick={() => remove(id)}
                                disabled={unlink.isPending}
                                className="h-7 px-2 inline-flex items-center gap-1.5 rounded-md text-[12px] text-slate-500 hover:text-red-600 hover:bg-red-50 transition-colors disabled:opacity-50"
                                aria-label={`Remove ${id.label}`}
                            >
                                <Trash2 className="w-3.5 h-3.5" /> Remove
                            </button>
                        </div>
                    ))}
                </div>
            )}
        </Section>
    );
}
