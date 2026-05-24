import Checkbox from "../Checkbox";

export default function CheckLine({
    children,
    value,
    setValue,
}: {
    children: React.ReactNode,
    value: boolean,
    setValue: (v: boolean) => void,
}) {
    return (
        <div className="flex">
            <label
                className="flex items-center cursor-pointer select-none text-slate-600 gap-4"
            >
                <input
                    type="checkbox"
                    checked={value}
                    onChange={() => setValue(!value)}
                    style={{ position: 'absolute', opacity: 0 }}
                />
                <Checkbox checked={value} />
                <span className="text-lg">{children}</span>
            </label>
        </div>
    )
}
