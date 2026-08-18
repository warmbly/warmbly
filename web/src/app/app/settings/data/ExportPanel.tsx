// Export panel — pick what travels, decide whether credentials come too.

import React from "react";
import toast from "react-hot-toast";
import { KeyRoundIcon, Loader2Icon } from "lucide-react";
import { TextInput } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";
import { useCreateOrgExport } from "@/lib/api/hooks/app/orgtransfer/useOrgTransfer";
import type { OrgDataGroup, OrgDataGroupInfo } from "@/lib/api/models/app/orgtransfer/OrgTransfer";
import { expandGroups } from "@/lib/api/models/app/orgtransfer/OrgTransfer";
import { Toggle } from "../_components/SectionShell";
import GroupPicker from "./GroupPicker";

export default function ExportPanel({
    groups,
    minPassphrase,
    loading,
    onStarted,
}: {
    groups: OrgDataGroupInfo[];
    minPassphrase: number;
    loading?: boolean;
    onStarted: () => void;
}) {
    const confirm = useConfirm();
    const createExport = useCreateOrgExport();

    // Everything is on by default: a migration that quietly leaves data behind
    // is worse than one that takes a while.
    const [selected, setSelected] = React.useState<Set<OrgDataGroup>>(new Set());
    const [includeSecrets, setIncludeSecrets] = React.useState(false);
    const [passphrase, setPassphrase] = React.useState("");
    const [repeat, setRepeat] = React.useState("");

    React.useEffect(() => {
        if (groups.length > 0 && selected.size === 0) {
            setSelected(new Set(groups.map((g) => g.key)));
        }
        // Only seeds the initial selection; later toggles are the user's.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [groups]);

    function toggle(key: OrgDataGroup) {
        setSelected((prev) => {
            const next = new Set(prev);
            if (next.has(key)) next.delete(key);
            else next.add(key);
            // Re-expand so ticking a group pulls in what it cannot travel
            // without, exactly as the server would.
            return expandGroups(next, groups);
        });
    }

    const passphraseTooShort = passphrase.length > 0 && passphrase.length < minPassphrase;
    const passphraseMismatch = repeat.length > 0 && passphrase !== repeat;
    const canSubmit =
        !createExport.isPending &&
        !loading &&
        (!includeSecrets ||
            (passphrase.length >= minPassphrase && passphrase === repeat));

    async function start() {
        const run = async () => {
            try {
                await createExport.mutateAsync({
                    groups: Array.from(selected),
                    include_secrets: includeSecrets,
                    passphrase: includeSecrets ? passphrase : undefined,
                });
                setPassphrase("");
                setRepeat("");
                onStarted();
            } catch (e) {
                toast.error((e as { message?: string })?.message ?? "Could not start the export.");
            }
        };

        if (includeSecrets) {
            confirm.show(
                "This archive will contain every mailbox password and access token in the workspace, protected only by the passphrase you just typed. Anyone with both the file and the passphrase can send mail as these mailboxes. Continue?",
                run,
            );
            return;
        }
        await run();
    }

    return (
        <div className="space-y-4">
            <GroupPicker
                groups={groups}
                selected={selected}
                onToggle={toggle}
                disabled={createExport.isPending}
            />

            <div className="rounded-md border border-slate-200 px-3 py-2.5">
                <div className="flex items-start gap-3">
                    <div className="min-w-0 flex-1">
                        <div className="text-[12.5px] font-medium text-slate-900 leading-tight inline-flex items-center gap-1.5">
                            <KeyRoundIcon className="w-3 h-3 text-slate-400" />
                            Include mailbox credentials
                        </div>
                        <div className="text-[11.5px] text-slate-500 leading-tight mt-0.5">
                            Mailbox passwords, OAuth tokens and integration keys are sealed into
                            the archive under a passphrase you choose. Import it with the same
                            passphrase and mailboxes come up connected. Leave this off and they
                            arrive needing a reconnect.
                        </div>
                    </div>
                    <Toggle
                        on={includeSecrets}
                        onChange={setIncludeSecrets}
                        disabled={createExport.isPending}
                    />
                </div>

                {includeSecrets && (
                    <div className="mt-3 pt-3 border-t border-slate-200/70 space-y-2">
                        <TextInput
                            value={passphrase}
                            onChange={setPassphrase}
                            type="password"
                            autoComplete="new-password"
                            placeholder={`Passphrase (at least ${minPassphrase} characters)`}
                            disabled={createExport.isPending}
                            className="w-full"
                        />
                        <TextInput
                            value={repeat}
                            onChange={setRepeat}
                            type="password"
                            autoComplete="new-password"
                            placeholder="Repeat the passphrase"
                            disabled={createExport.isPending}
                            className="w-full"
                        />
                        <p className="text-[11px] leading-relaxed text-amber-700">
                            {passphraseTooShort
                                ? `Use at least ${minPassphrase} characters.`
                                : passphraseMismatch
                                    ? "The two passphrases do not match."
                                    : "Warmbly never stores this passphrase. Lose it and the credentials in the archive cannot be recovered, so you would have to export again."}
                        </p>
                    </div>
                )}
            </div>

            <div className="flex items-center gap-2">
                <button
                    type="button"
                    onClick={start}
                    disabled={!canSubmit}
                    className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                    {createExport.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                    Start export
                </button>
                <span className="text-[11.5px] text-slate-500">
                    Large workspaces take a while. You can leave this page.
                </span>
            </div>
        </div>
    );
}
