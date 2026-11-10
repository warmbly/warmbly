// Move money from the agency's AI balance into one client workspace's own balance.

import React from "react";
import { Loader2Icon, WalletIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { Label, NumberInput } from "@/components/ui/field";
import { useTransferAgencyAI } from "@/lib/api/hooks/app/agency/useAgency";
import type { AgencyClient } from "@/lib/api/models/app/agency/Agency";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { fmtUsd, roundCents } from "@/lib/money";
import { cn } from "@/lib/utils";
import { AgencyDialog } from "./AgencyDialog";
import { ghostBtn, primaryBtn } from "./styles";

const MIN_MOVE_USD = 1;
const MAX_MOVE_USD = 5000;

export function AddBalanceDialog({
    agencyId,
    client,
    available,
    onClose,
}: {
    agencyId: string;
    client: AgencyClient | null;
    available: number;
    onClose: () => void;
}) {
    const transfer = useTransferAgencyAI(agencyId);
    const [amount, setAmount] = React.useState(10);
    // One key per opening, so a retried submit moves the money once.
    const [key, setKey] = React.useState(() => crypto.randomUUID());
    const [openedFor, setOpenedFor] = React.useState<string | null>(null);

    // Reset once per opening; a balance refetched while open leaves the typed amount alone.
    const opening = client?.organization_id ?? null;
    if (opening !== openedFor) {
        setOpenedFor(opening);
        if (opening) {
            setAmount(Math.max(MIN_MOVE_USD, Math.min(10, Math.floor(available))));
            setKey(crypto.randomUUID());
        }
    }

    const usd = roundCents(amount);
    const error =
        usd < MIN_MOVE_USD || usd > MAX_MOVE_USD
            ? `Enter an amount from ${fmtUsd(MIN_MOVE_USD)} to ${fmtUsd(MAX_MOVE_USD)}.`
            : usd > available
              ? `The agency balance holds ${fmtUsd(available)}. Buy more under Billing or move less.`
              : null;

    async function submit() {
        if (!client || error) return;
        try {
            await transfer.mutateAsync({ orgId: client.organization_id, amountUsd: usd, key });
            toast.success(`Moved ${fmtUsd(usd)} to ${client.name}`);
            onClose();
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    return (
        <AgencyDialog
            open={!!client}
            onClose={onClose}
            eyebrow="Add balance"
            title={client?.name ?? "Client"}
            icon={<WalletIcon className="w-3 h-3" />}
            footer={
                <>
                    <button type="button" onClick={onClose} className={cn("ml-auto", ghostBtn)}>
                        Cancel
                    </button>
                    <button type="button" onClick={() => void submit()} disabled={transfer.isPending || !!error} className={primaryBtn}>
                        {transfer.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        Move {fmtUsd(usd)}
                    </button>
                </>
            }
        >
            <form
                className="px-4 py-4 space-y-2"
                onSubmit={(e) => {
                    e.preventDefault();
                    void submit();
                }}
            >
                <Label htmlFor="agency-move-amount">Amount</Label>
                <NumberInput
                    id="agency-move-amount"
                    value={amount}
                    onChange={setAmount}
                    min={0}
                    max={MAX_MOVE_USD}
                    step={5}
                    suffix="USD"
                    className="w-40"
                />
                <p className={cn("text-[11.5px]", error ? "text-red-600" : "text-slate-500")} aria-live="polite">
                    {error ??
                        `Taken from the agency balance (${fmtUsd(available)} left) and added to ${client?.name ?? "the workspace"}'s own balance, where it stays and does not expire.`}
                </p>
            </form>
        </AgencyDialog>
    );
}
