import useClickOutside from "@/hooks/useClickOutside";
import React from "react";
import MiniInput from "../../popup/MiniInput";
import Selector from "../../popup/select/Selector";
import SelectMenu from "../../popup/select/SelectMenu";
import type SearchContactsFilter from "@/lib/api/models/app/contacts/SearchContactsFilter";
import SelectOption from "../../popup/select/SelectOption";
import { RiCloseCircleLine } from "@remixicon/react";
import type { SearchContactsFilterType } from "@/lib/api/models/app/contacts/search-contacts.types";

const CustomFieldFilterTypeNames: Record<SearchContactsFilterType, string> = {
    'equal': "Equal",
    'starts_with': "Starts With",
    'ends_with': "Ends With",
    'contains': "Contains"
}

export default function ContactsFilterField({
    field,
    onChange,
    onDelete,
}: {
    field: SearchContactsFilter;
    onChange: (f: SearchContactsFilter) => void;
    onDelete: () => void;
}) {
    const [show, setShow] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);

    useClickOutside(ref, () => setShow(false));

    return (
        <div className="space-y-2">
            <div className="grid grid-cols-2 gap-2">
                <MiniInput
                    value={field.name}
                    placeholder="my_custom_field"
                    onChange={(e) => onChange({ ...field, name: e.target.value })}
                />

                <div ref={ref} className="relative">
                    <Selector caret show={show} setShow={setShow}>
                        {CustomFieldFilterTypeNames[field.type]}
                    </Selector>

                    <SelectMenu show={show}>
                        {Object.entries(CustomFieldFilterTypeNames).map(([key, label]) => (
                            <SelectOption
                                key={key}
                                selected={field.type === key}
                                onClick={() => {
                                    onChange({ ...field, type: key as SearchContactsFilterType });
                                    setShow(false);
                                }}
                            >
                                {label}
                            </SelectOption>
                        ))}
                    </SelectMenu>
                </div>
            </div>

            <div className="flex gap-2">
                <div className="grow">
                    <MiniInput
                        value={field.value}
                        placeholder="my text"
                        onChange={(e) => onChange({ ...field, value: e.target.value })}
                    />
                </div>
                <button
                    type="button"
                    className="ripple shrink-0 px-3 cursor-pointer text-red-600 transition bg-red-100 hover:bg-red-200 rounded-lg"
                    onClick={onDelete}
                >
                    <RiCloseCircleLine className="w-5 h-5" />
                </button>
            </div>
        </div>
    );
}
