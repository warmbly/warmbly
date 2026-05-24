export default function Switch({id, value, onChange}:{id: string, value: boolean, onChange: (v: boolean) => void}) {
    return (
        <label htmlFor={id} className="relative select-none inline-block w-11 h-6 cursor-pointer p-2 scale-95 shrink-0">
            <input type="checkbox" checked={value} onChange={() => onChange(!value)} id={id} className="peer sr-only"/>
            <span className="absolute inset-0 bg-gray-200 rounded-full transition-colors duration-200 ease-in-out peer-checked:bg-blue-500 peer-disabled:opacity-50 peer-disabled:pointer-events-none"></span>
            <span className="absolute top-1/2 start-0.5 -translate-y-1/2 size-5 bg-white rounded-full shadow-xs transition-transform duration-200 ease-in-out peer-checked:translate-x-full"></span>
        </label>
    )
}