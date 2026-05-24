import MiniInput from "@/components/app/popup/MiniInput";
import React from "react";
import { LinkContext } from "./context/link";

function Title({ children }: { children: React.ReactNode }) {
    return <h1 className="text-lg font-medium font-sans">
        {children}
    </h1>
}

export default function LinkProvider({ children }: { children: React.ReactNode }) {
    const [visible, setVisible] = React.useState<boolean>(false);
    const [displayName, setDisplayName] = React.useState<string>("");
    const [url, setUrl] = React.useState<string>("")
    const s = React.useRef<(displayName: string, url: string) => void>(null)

    const show = (dname: string, onSubmit: (displayName: string, url: string) => void) => {
        setDisplayName(dname);
        setUrl("");
        setVisible(true);
        s.current = onSubmit;
    }


    const [mouseDownOnButton, setMouseDownOnButton] = React.useState(false);
    const handleMouseDown = () => setMouseDownOnButton(true);
    const handleMouseUp = () => {
        if (mouseDownOnButton) {
            setVisible(false)
        }
        setMouseDownOnButton(false);
    };

    return <>
        <LinkContext.Provider value={{ show }}>
            {children}
            <div className={`bg-black/30 fixed flex inset-0 z-101 items-center justify-center p-1 transition ${visible ? "opacity-100 visible" : "opacity-0 invisible"}`} onMouseDown={handleMouseDown} onMouseUp={handleMouseUp}>
                <div className={`bg-white max-w-xl w-full flex flex-col gap-2 p-5 rounded-md transition ease-bezier duration-300 ${visible ? "scale-100" : "scale-90"}`} onMouseDown={(e) => e.stopPropagation()} onMouseUp={(e) => e.stopPropagation()}>
                    <Title>Display Name</Title>
                    <MiniInput placeholder="Display Name" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
                    <Title>URL</Title>
                    <MiniInput placeholder="https://example.com" value={url} onChange={(e) => setUrl(e.target.value)} />
                    <div className="flex justify-end mt-2 gap-2">
                        <button onClick={() => setVisible(false)} className="ripple bg-gray-200 text-gray-500 py-2.5 p-8 rounded-md cursor-pointer">
                            Cancel
                        </button>
                        <button onClick={() => {
                            if (s.current) s.current(displayName, url)
                            setVisible(false)
                        }} className="ripple bg-blue-100 text-blue-500 py-2.5 p-8 rounded-md cursor-pointer">
                            Submit
                        </button>
                    </div>
                </div>
            </div>
        </LinkContext.Provider>
    </>
}

