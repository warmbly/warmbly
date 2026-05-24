import CheckFilter from "./CheckFilter"
import MiniDate from "../popup/MiniDate"

export default function CheckFilterTime({
    label,
    value,
    setValue
}: {
    label: string,
    value: Date | undefined,
    setValue: (v: Date | undefined) => void,
}) {
    return <CheckFilter
        value={value !== null}
        setValue={(v) => {
            if (v) {
                setValue(new Date())
            } else {
                setValue(undefined)
            }
        }}
        label={label}
    >
        <MiniDate
            onChange={setValue}
            placeholder="Null"
            value={value}
        />
    </CheckFilter>
}
