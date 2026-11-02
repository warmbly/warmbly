import { WEEKDAY_LABELS } from "@/lib/api/models/app/emails/SendingBehavior";

export function warmupDaysLabel(days: number): string {
    if (days === 127) return "Every day";
    if (days === 31) return "Monday to Friday";
    if (days === 0) return "Follow sending plan";
    return WEEKDAY_LABELS.filter((_, index) => (days & (1 << index)) !== 0).join(", ");
}

