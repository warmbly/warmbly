import { RiCheckLine } from "@remixicon/react";

interface SaveButtonProps {
    color: string;
    submit: (color: string) => void;
}

const SaveButton = ({color, submit}: SaveButtonProps) => {
    return (
        <div>
            <button
                disabled={color === ""}
                className="ripple rounded-full px-2 h-full items-center transition-colors duration-75 cursor-pointer hover:brightness-95"
                style={{
                    background: color === "" ? "#1e293b":"#22c55e",
                    color: color === "" ? "#64748b":"white"
                }}
                onClick={() => submit(color)}
            >   
                <RiCheckLine className="w-4"/>
            </button>
        </div>
    )
}
export default SaveButton;