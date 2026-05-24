import React from "react";
import Selector from "./Selector";
import SelectMenu from "./SelectMenu";
import SelectOption from "./SelectOption";
import { RiTimeLine } from "@remixicon/react";
import { timeOptions, to12Hour } from "@/lib/core/time";

export default function TimeSelector({
    value,
    onChange,
}:{
    value: string, 
    onChange: (v: string) => void, 
}) {
    const [show, setShow] = React.useState<boolean>(false);
    const popupRef = React.useRef<HTMLDivElement>(null);

    React.useEffect(() => {
        function handleClickOutside(event: MouseEvent) {
            if (
            show && 
            popupRef.current && 
            !popupRef.current.contains(event.target as Node)
            ) {
            setShow(false);
            }
        }
        document.addEventListener("mousedown", handleClickOutside);

        return () => {
            document.removeEventListener("mousedown", handleClickOutside);
        };
    }, [show]);

    return (
        <div className="relative" ref={popupRef}>
            <Selector show={show} setShow={setShow} caret>
                {to12Hour(value)}
            </Selector>
            <SelectMenu show={show}>
                {timeOptions.map((t) => {
                    const isSelected = to12Hour(value) === t.name;
                    return (
                        <SelectOption 
                         key={t.value} 
                         onClick={() => {
                            onChange(t.value)
                         }} 
                         selected={isSelected} 
                        >
                            <RiTimeLine className="w-4 shrink-0"/>
                            <span className="truncate">{t.name}</span>
                        </SelectOption>
                    )
                })}
            </SelectMenu>
        </div>
    )
}