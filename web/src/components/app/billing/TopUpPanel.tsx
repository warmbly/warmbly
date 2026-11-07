// Add money: a few presets or a typed amount. With a saved card it is paid in
// place after one confirm; without one, or when the bank wants to confirm, it
// carries on in Stripe Checkout.

import React from "react";
import { CreditCardIcon, Loader2Icon } from "lucide-react";
import { useConfirm } from "@/hooks/context/confirm";
import { TextInput } from "@/components/ui/field";
import { useBalanceCheckout, useTopUp } from "@/lib/api/hooks/app/subscription/useBalance";
import type { Balance } from "@/lib/api/models/app/subscription/Credits";
import { DEFAULT_TOP_UP_USD, MAX_TOP_UP_USD, MIN_TOP_UP_USD, TOP_UP_PRESETS, fmtUsd, fmtUsdTrim, parseUsd, validTopUp } from "@/lib/money";
import { cn } from "@/lib/utils";
import { paymentMethodLabel } from "./balanceModel";

type Choice = number | "custom";

export default function TopUpPanel({ balance, canBuy }: { balance: Balance; canBuy: boolean }) {
    const confirm = useConfirm();
    const pay = useTopUp();
    const checkout = useBalanceCheckout();
    const presets = balance.top_up?.presets_usd?.length ? balance.top_up.presets_usd : [...TOP_UP_PRESETS];
    const initial = balance.top_up?.default_usd ?? DEFAULT_TOP_UP_USD;
    const [choice, setChoice] = React.useState<Choice>(presets.includes(initial) ? initial : "custom");
    const [custom, setCustom] = React.useState(presets.includes(initial) ? "" : String(initial));

    const pm = balance.payment_method ?? null;
    const usd = choice === "custom" ? parseUsd(custom) : choice;
    const valid = validTopUp(usd);
    const typedWrong = choice === "custom" && custom.trim() !== "" && !valid;
    const inFlight = !!balance.charge_in_progress;
    const busy = pay.isPending || checkout.isPending;
    const disabled = !canBuy || !valid || busy || inFlight;

    const buy = () => {
        if (disabled) return;
        if (!pm) {
            checkout.mutate(usd);
            return;
        }
        confirm.show(
            `Charge ${fmtUsd(usd)} to ${paymentMethodLabel(pm)} now? It goes on the workspace balance and never expires.`,
            // The hook reports a failure itself; the dialog just closes.
            () => pay.mutateAsync(usd).then(() => undefined, () => undefined),
            undefined,
            { title: `Add ${fmtUsd(usd)} to the balance?`, confirmLabel: `Pay ${fmtUsd(usd)}`, tone: "primary" },
        );
    };

    const summary = !canBuy
        ? "Adding money needs an active paid plan."
        : inFlight
          ? "A payment is still going through. This updates when it lands."
          : typedWrong
            ? `Enter an amount from ${fmtUsdTrim(MIN_TOP_UP_USD)} to ${fmtUsdTrim(MAX_TOP_UP_USD)}.`
            : valid
              ? `Adds ${fmtUsd(usd)} to the balance.`
              : "Enter an amount.";

    return (
        <div className="rounded-md border border-slate-200 bg-slate-50/60 px-3 py-3 md:px-4">
            <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Add money</div>
                <div className="text-[11px] text-slate-400">Dollar for dollar. Never expires.</div>
            </div>
            <div className="mt-2.5 flex flex-wrap items-center gap-1.5" role="radiogroup" aria-label="Amount">
                {presets.map((v) => (
                    <Chip key={v} active={choice === v} onClick={() => setChoice(v)}>
                        {fmtUsdTrim(v)}
                    </Chip>
                ))}
                <Chip active={choice === "custom"} onClick={() => setChoice("custom")}>
                    Other
                </Chip>
                {choice === "custom" && (
                    <div className="relative">
                        <span className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-[12.5px] text-slate-400">$</span>
                        <TextInput
                            autoFocus
                            value={custom}
                            onChange={(v) => setCustom(v.replace(/[^\d.,$\s]/g, ""))}
                            onKeyDown={(e) => {
                                if (e.key === "Enter") buy();
                            }}
                            placeholder={String(DEFAULT_TOP_UP_USD)}
                            invalid={typedWrong}
                            title="Amount in US dollars"
                            className="w-[112px] pl-5 tabular-nums"
                        />
                    </div>
                )}
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-2">
                <p
                    aria-live="polite"
                    className={cn("min-w-0 flex-1 basis-[200px] text-[11.5px]", typedWrong ? "text-red-600" : "text-slate-500")}
                >
                    {summary}
                </p>
                <div className="flex items-center gap-1.5 ml-auto">
                    {pm && (
                        <button
                            type="button"
                            onClick={() => valid && checkout.mutate(usd)}
                            disabled={disabled}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors inline-flex items-center gap-1.5 disabled:opacity-50"
                        >
                            {checkout.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            Pay another way
                        </button>
                    )}
                    <button
                        type="button"
                        onClick={buy}
                        disabled={disabled}
                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                    >
                        {pay.isPending || (!pm && checkout.isPending) ? (
                            <Loader2Icon className="w-3 h-3 animate-spin" />
                        ) : pm ? (
                            <CreditCardIcon className="w-3 h-3" />
                        ) : null}
                        {valid ? (pm ? `Pay ${fmtUsd(usd)}` : `Continue to pay ${fmtUsd(usd)}`) : "Pay"}
                    </button>
                </div>
            </div>
            {pm && (
                <p className="mt-1.5 text-right text-[11px] text-slate-400 font-mono">Charged to {paymentMethodLabel(pm)}</p>
            )}
        </div>
    );
}

function Chip({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
    return (
        <button
            type="button"
            role="radio"
            aria-checked={active}
            onClick={onClick}
            className={cn(
                "h-7 px-2.5 rounded-md border text-[12px] tabular-nums transition-colors",
                active
                    ? "border-sky-400 bg-sky-50 text-sky-700 font-medium"
                    : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-900",
            )}
        >
            {children}
        </button>
    );
}
