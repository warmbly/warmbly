// Adding funds: the preset amounts, the custom amount's bounds and steps, and how long a balance lasts.
import type { TopUpBounds } from "@/lib/api/models/app/subscription/Credits";
import { DEFAULT_TOP_UP_USD, MAX_TOP_UP_USD, MIN_TOP_UP_USD, TOP_UP_PRESETS } from "@/lib/money";

/** The store's presets and default, used only where the store is on. */
export const FUND_PRESETS_USD = [100, 250, 500, 1000, 2500, 5000] as const;
export const DEFAULT_FUND_USD = 100;
export const FUND_STEP_USD = 10;
/** The least a card is charged when the store does not say. */
export const FALLBACK_MIN_FUND_USD = 10;
/** The lowest balance automatic reload can top up at, as the server takes it (credits.ReloadThresholdMin). */
export const RELOAD_THRESHOLD_MIN_USD = 0.01;

/** What the funds dialog offers: the server's top-up bounds, or the store's where the store is on. */
export interface FundsPolicy {
    minUsd: number;
    presetsUsd: readonly number[];
    defaultUsd: number;
    /** Whether the defaults for a first automatic top-up follow the store's larger amounts. */
    store: boolean;
}

export function fundsPolicy(topUp: TopUpBounds | undefined, store: { enabled: boolean; minCardChargeMicros?: number | null }): FundsPolicy {
    if (store.enabled) {
        return { minUsd: Math.max(topUp?.min_usd ?? MIN_TOP_UP_USD, minFundUsd(store.minCardChargeMicros)), presetsUsd: FUND_PRESETS_USD, defaultUsd: DEFAULT_FUND_USD, store: true };
    }
    const presets = topUp?.presets_usd?.length ? topUp.presets_usd : [...TOP_UP_PRESETS];
    return { minUsd: topUp?.min_usd ?? MIN_TOP_UP_USD, presetsUsd: presets, defaultUsd: topUp?.default_usd ?? DEFAULT_TOP_UP_USD, store: false };
}

/** The smallest amount to add: the store's minimum card charge, never below what a top-up takes. */
export function minFundUsd(minCardChargeMicros?: number | null): number {
    const store = minCardChargeMicros && minCardChargeMicros > 0 ? minCardChargeMicros / 1_000_000 : FALLBACK_MIN_FUND_USD;
    return Math.max(MIN_TOP_UP_USD, Math.ceil(store));
}

/** A typed or stepped amount held to whole dollars between the minimum and the top-up ceiling. */
export function clampFund(usd: number, min: number): number {
    if (!Number.isFinite(usd)) return min;
    return Math.min(MAX_TOP_UP_USD, Math.max(min, Math.round(usd)));
}

/** One step up or down, landing on the step grid ($10, $20, ...) and never past the bounds. */
export function stepFund(usd: number, dir: 1 | -1, min: number): number {
    const next = dir > 0 ? Math.floor(usd / FUND_STEP_USD) * FUND_STEP_USD + FUND_STEP_USD : Math.ceil(usd / FUND_STEP_USD) * FUND_STEP_USD - FUND_STEP_USD;
    return clampFund(next, min);
}

/** The first preset above an amount, offered as a rounder top-up at checkout; null past the largest. */
export function nextPresetAbove(usd: number): number | null {
    return FUND_PRESETS_USD.find((p) => p > usd) ?? null;
}

/** Whole days a balance lasts at a daily spend; null when nothing is spent, so it lasts. */
export function lastsDays(balanceUsd: number, perDayUsd: number): number | null {
    if (!(perDayUsd > 0)) return null;
    return Math.max(0, Math.floor(balanceUsd / perDayUsd));
}

/** "Lasts ~9 days", "Lasts over a year", or what is said when nothing is being spent. */
export function lastsLabel(days: number | null): string {
    if (days === null) return "Nothing is drawing on it yet";
    if (days === 0) return "Runs out today";
    if (days > 365) return "Lasts over a year";
    return `Lasts ~${days} ${days === 1 ? "day" : "days"}`;
}

/** 1st, 2nd, 3rd, 4th ... 21st, 22nd, 23rd. */
export function ordinal(n: number): string {
    const tens = n % 100;
    if (tens >= 11 && tens <= 13) return `${n}th`;
    return `${n}${["th", "st", "nd", "rd"][n % 10] ?? "th"}`;
}
