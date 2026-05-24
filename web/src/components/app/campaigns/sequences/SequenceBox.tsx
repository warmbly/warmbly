export default function SequenceBox({
    children,
    next,
    active,
    def_wait,
    wait,
    setWait,
    onClick,
}: {
    children: React.ReactNode,
    next: boolean,
    active: boolean,
    def_wait: number,
    wait: number,
    setWait: (v: number) => void,
    onClick: () => void,
}) {
    return (
        <>
            <div onClick={onClick} className={`w-full select-none p-2.5 font-inter cursor-pointer transition bg-white border-gray-200 rounded-lg ${active ? "shadow-md text-slate-800" : "shadow-sm text-slate-400"}`}>
                {children}
            </div>
            {next && (<div className="px-2.5 relative text-slate-600 flex items-center gap-3 font-sans">
                <span>Wait</span>
                <MiniNumberInput
                    value={wait}
                    placeholder={`${def_wait} days`}
                    onChange={(e) => setWait(e.target.valueAsNumber)}
                />
                <span>Day(s)</span>
                <div className={`transition absolute inset-0 bg-gray-50 ${active ? "opacity-0 invisible" : "opacity-60 visible"}`}></div>
            </div>)}
        </>
    )
}
