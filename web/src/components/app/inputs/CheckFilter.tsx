import CheckLine from "./CheckLine";

export default function CheckFilter({
    children,
    label,
    value,
    setValue
}: {
    children?: React.ReactNode,
    label: string,
    value: boolean,
    setValue: (v: boolean) => void,
}) {
    return <div>
        <CheckLine
            value={value}
            setValue={setValue}>
            {label}
        </CheckLine>
        {(value && children) && (
            <div className="flex">
                <div className="w-5 shrink-0 flex justify-center py-2">
                    <div className="w-px h-full bg-slate-300" />
                </div>
                <div className="flex-1 px-4 py-3">
                    {children}
                </div>
            </div>
        )}
    </div>
}
