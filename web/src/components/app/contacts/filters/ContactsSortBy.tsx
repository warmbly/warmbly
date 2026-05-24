import useClickOutside from "@/hooks/useClickOutside";
import React from "react";
import Selector from "../../popup/select/Selector";
import type { SearchContactsSortBy } from "@/lib/api/models/app/contacts/search-contacts.types";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import SelectMenu from "../../popup/select/SelectMenu";
import SelectOption from "../../popup/select/SelectOption";
import CheckLine from "../../inputs/CheckLine";

const SortByNames: Record<SearchContactsSortBy, string> = {
    'created_at': "Created At",
    'updated_at': "Updated At",
    'first_name': "First Name",
    'last_name': "Last Name",
    'email': "Email",
    'campaign_count': "Campaign Count"
}

export default function ContactsSortBy({
    search,
    setSearch
}: {
    search: SearchContacts,
    setSearch: React.Dispatch<React.SetStateAction<SearchContacts>>
}) {
    const [show, setShow] = React.useState<boolean>(false)
    const ref = React.useRef<HTMLDivElement>(null);

    useClickOutside(ref, () => setShow(false))

    return (<div className="space-y-4">
        <div className="relative" ref={ref}>
            <Selector caret show={show} setShow={setShow}>
                {SortByNames[search.sort_by]}
            </Selector>

            <SelectMenu show={show}>
                {Object.entries(SortByNames).map(([key, label]) => (
                    <SelectOption
                        key={key}
                        selected={search.sort_by === key}
                        onClick={() => {
                            setSearch(bef => ({
                                ...bef,
                                sort_by: key as SearchContactsSortBy,
                            }))
                            setShow(false);
                        }}
                    >
                        {label}
                    </SelectOption>
                ))}
            </SelectMenu>
        </div>
        <div className="flex justify-end px-2">
            <CheckLine
                value={search.reverse}
                setValue={(v) => setSearch(bef => ({
                    ...bef,
                    reverse: v,
                }))}>
                Reverse
            </CheckLine></div>
    </div>)
}
