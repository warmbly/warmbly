import { ClockIcon } from "lucide-react";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import { timeOptions, to12Hour } from "@/lib/core/time";
import { cn } from "@/lib/utils";

// Themed 30-minute time picker. Portal-based (PopoverMenu) so it renders above
// drawers/modals. Shared by the campaign schedule and the warmup settings.
export default function TimeSelect({
    value,
    onChange,
    className,
}: {
    value: string;
    onChange: (v: string) => void;
    className?: string;
}) {
    return (
        <PopoverMenu>
            <PopoverMenuTrigger asChild>
                <SelectButton
                    icon={<ClockIcon className="w-3.5 h-3.5" />}
                    label={to12Hour(value)}
                    className={cn("w-full justify-between", className)}
                />
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={160} className="max-h-64 overflow-y-auto">
                {timeOptions.map((t) => (
                    <PopoverMenuItem
                        key={t.value}
                        selected={t.value === value}
                        onSelect={() => onChange(t.value)}
                    >
                        {t.name}
                    </PopoverMenuItem>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
