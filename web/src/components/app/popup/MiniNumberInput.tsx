import React from "react";
import { NumberInput } from "@/components/ui/field";

// Thin adapter over the shared themed NumberInput (no native spinner)
// that preserves the legacy event-style onChange contract callers rely
// on (they read e.target.valueAsNumber).
export default function MiniNumberInput({onChange, placeholder, value, id}: {onChange: (e: React.ChangeEvent<HTMLInputElement>) => void, placeholder?: string, value?: number, id?: string, name?: string}){
    return <div id={id}>
        <NumberInput
            value={value ?? Number.NaN}
            min={0}
            placeholder={placeholder}
            onChange={(n) => {
                onChange({
                    target: { value: String(n), valueAsNumber: n },
                } as unknown as React.ChangeEvent<HTMLInputElement>);
            }}
            className="w-full"
        />
    </div>
}
