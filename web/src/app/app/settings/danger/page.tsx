import { TrashIcon } from "lucide-react";
import { comingSoon } from "@/lib/helper/comingSoon";
import { Section, SectionShell } from "../_components/SectionShell";

export default function DangerSettingsPage() {
    return (
        <SectionShell title="Danger zone" description="Irreversible actions. Read carefully.">
            <Section eyebrow="Account" description="Affects you across every workspace.">
                <DangerRow
                    title="Delete account"
                    body="Permanently delete your account and every workspace you own. This can't be undone."
                    cta="Delete account…"
                    onClick={() => comingSoon("Account deletion")}
                />
            </Section>

            <Section eyebrow="Workspace" description="Affects only the workspace you're viewing.">
                <DangerRow
                    title="Leave workspace"
                    body="Remove yourself from this workspace. You'll lose access to its data immediately."
                    cta="Leave workspace…"
                    onClick={() => comingSoon("Leave workspace")}
                />
                <DangerRow
                    title="Transfer ownership"
                    body="Hand ownership of this workspace to another member. You'll become an admin."
                    cta="Transfer…"
                    onClick={() => comingSoon("Ownership transfer")}
                />
                <DangerRow
                    title="Delete workspace"
                    body="Permanently delete this workspace and every campaign, contact and mailbox it contains."
                    cta="Delete workspace…"
                    onClick={() => comingSoon("Workspace deletion")}
                />
            </Section>
        </SectionShell>
    );
}

function DangerRow({
    title,
    body,
    cta,
    onClick,
}: {
    title: string;
    body: string;
    cta: string;
    onClick: () => void;
}) {
    return (
        <div className="flex flex-col sm:flex-row gap-2 sm:gap-4 sm:items-center border-l-2 border-red-200 pl-3">
            <div className="min-w-0 flex-1">
                <div className="text-[12.5px] font-medium text-red-700 leading-tight flex items-center gap-1.5">
                    <TrashIcon className="w-3 h-3" />
                    {title}
                </div>
                <div className="text-[11.5px] text-red-700/70 leading-tight mt-0.5">{body}</div>
            </div>
            <button
                type="button"
                onClick={onClick}
                className="self-start sm:ml-auto h-7 px-2.5 rounded-md border border-red-300 hover:border-red-400 text-red-700 hover:text-red-800 hover:bg-red-100/60 text-[12px] font-medium transition-colors shrink-0"
            >
                {cta}
            </button>
        </div>
    );
}
