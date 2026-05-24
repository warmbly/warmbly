import React from "react";
import ColorBox from "../../colors/ColorBox";
import ColorPanel from "../../colors/ColorPanel";
import useClickOutside from "@/hooks/useClickOutside";

export default function ModalColorInput({
    color,
    setColor
}: {
    color: string,
    setColor: React.Dispatch<React.SetStateAction<string>>
}) {
    const [show, setShow] = React.useState<boolean>(false);
    const popupRef = React.useRef<HTMLDivElement>(null);

    useClickOutside(popupRef, () => setShow(false))

    return (
        <div className='relative z-2' ref={popupRef}>
            <div className='w-5 h-5 rounded-full border border-slate-300' onClick={() => setShow(!show)} style={{ backgroundColor: `${color}` }} />
            <ColorBox show={show}>
                <ColorPanel color={color} submitColor={(c) => { setColor(c); setShow(false) }} />
            </ColorBox>
        </div>
    )
}
