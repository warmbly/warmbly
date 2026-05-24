import useClickOutside from "@/hooks/useClickOutside";
import React from "react";
import SelectMenu from "../popup/select/SelectMenu";
import { ChevronDownIcon } from "lucide-react";

export default function HeadSelectMenu({
    children,
    icon,
    title,
}: {
    children: React.ReactNode,
    icon: React.ReactNode,
    title: string,
}) {
    const [show, setShow] = React.useState<boolean>(false);
    const ref = React.useRef<HTMLDivElement>(null);

    useClickOutside(ref, () => setShow(false))

    React.useEffect(() => {
        setShow(false);
    }, [title])

    return (
        <div className="relative" ref={ref}>
            <button onClickCapture={() => setShow(true)} className="px-2 py-1 cursor-pointer rounded-md text-sm text-muted-foreground transition-colors duration-150 ease-in-out border border-border flex items-center gap-1.5">
                {icon}
                <span className="max-w-[120px] truncate">{title}</span>
                <ChevronDownIcon className="w-4 h-4 text-muted-foreground" />
            </button>
            <SelectMenu show={show}>
                {children}
            </SelectMenu>
        </div>
    )
}
