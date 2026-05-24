import Checkbox from "../../Checkbox";

export default function WeekdayBitmask({
    weekdays,
    value,
    setValue,
}: {
    weekdays: string[],
    value: number,
    setValue: (v: number) => void,
}) {
    const toggleDay = (index: number) => {
        const mask = 1 << index;
        setValue(value ^ mask); // XOR toggles the bit
    };

    return weekdays.map((day, index) => {
        const mask = 1 << index;
        const isChecked = (value & mask) !== 0;

        return (
            <label
                key={day}
                className="flex items-center cursor-pointer select-none text-slate-600 gap-3"
            >
                <input
                    type="checkbox"
                    checked={isChecked}
                    onChange={() => toggleDay(index)}
                    style={{ position: 'absolute', opacity: 0 }}
                />
                <Checkbox checked={isChecked} />
                <span>{day}</span>
            </label>
        );
    })
};
