import React, { useRef, useState } from "react";

interface OTPInputProps {
    value: string[];
    setValue: (v: string[]) => void;
    cellClassName?: string;
}

export default function OTPInput({ value, setValue, cellClassName }: OTPInputProps) {
    const inputs = useRef<(HTMLInputElement | null)[]>([]);
    const [selections, setSelections] = useState(
        Array(value.length).fill({ start: 0, end: 0 })
    );

    const focusInput = (index: number) => inputs.current[index]?.focus();

    const replaceOtp = (newArr: string[]) => setValue(newArr);

    const handleChange = (text: string, index: number) => {
        const digits = text.replace(/[^0-9]/g, "");

        if (digits.length === 1) {
            const newOtp = [...value];
            newOtp[index] = digits;
            setValue(newOtp);
            if (index < value.length - 1) focusInput(index + 1);
        } else if (digits.length > 1) {
            const newOtp = [...value];
            digits.split("").forEach((d, i) => {
                const pos = index + i;
                if (pos < newOtp.length) newOtp[pos] = d;
            });
            setValue(newOtp);
            const lastFilled = index + digits.length - 1;
            if (lastFilled < value.length - 1) focusInput(lastFilled + 1);
        }
    };

    const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>, index: number) => {
        if (e.key === "Backspace" || e.key === "Delete") {
            if (!value[index] && index > 0) {
                const newOtp = [...value];
                newOtp[index - 1] = "";
                replaceOtp(newOtp);
                focusInput(index - 1);
            } else {
                const newOtp = [...value];
                newOtp[index] = "";
                replaceOtp(newOtp);
            }
        }
    };

    const handleFocus = (index: number) => {
        const next = [...selections];
        next[index] = { start: 0, end: 1 };
        setSelections(next);
    };

    const defaultCell = "flex-1 min-w-0 h-14 sm:h-16 rounded-lg border border-sky-200 bg-white text-slate-800 text-center text-2xl sm:text-3xl font-semibold outline-none transition-all duration-200 focus:border-sky-400 focus:ring-4 focus:ring-sky-400/15 placeholder:text-slate-200";

    return (
        <div className="flex gap-2 sm:gap-3">
            {value.map((digit, i) => (
                <input
                    key={i}
                    ref={(r) => { inputs.current[i] = r }}
                    className={cellClassName || defaultCell}
                    type="text"
                    inputMode="numeric"
                    maxLength={6}
                    value={digit}
                    placeholder="0"
                    onChange={(e) => handleChange(e.target.value, i)}
                    onKeyDown={(e) => handleKeyDown(e, i)}
                    onFocus={() => handleFocus(i)}
                    autoComplete="one-time-code"
                />
            ))}
        </div>
    );
}
