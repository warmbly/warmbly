import Checkbox from "../Checkbox";

export default function BoolField({
    children,
    name,
    value,
    onChange,
}: {
    name: string,
    value: boolean,
    onChange: () => void,
    children: React.ReactNode,
}) {
    return (<div>
        <label htmlFor={`edit-contact-${name}`} className="cursor-pointer select-none flex items-center gap-3 jusitfy-center">
            <input
                type="checkbox"
                checked={value}
                onChange={onChange}
                className="hidden"
                id={`edit-contact-${name}`} />
            <Checkbox
                checked={value} />
            {children}
        </label>
    </div>)
}
