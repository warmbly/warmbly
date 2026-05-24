import React from "react"
import Search from "../Search";
import { Loading } from "@/components/loader";
import { SearchIcon } from "lucide-react";

export default function HeadSearch({
    loading,
    onSubmit,
}: {
    loading: boolean,
    onSubmit: (e: React.FormEvent<HTMLFormElement>, search: string) => Promise<void> | void,
}) {
    const [search, setSearch] = React.useState<string>("")

    return (
        <div>
            <form className="w-full flex gap-1.5 h-full" onSubmit={(e) => onSubmit(e, search)}>
                <Search
                    value={search}
                    onChange={(e) => setSearch(e)}
                />
                <button
                    type="submit"
                    className="flex items-center justify-center h-8 w-8 rounded-md border border-border text-muted-foreground hover:text-foreground transition-colors duration-150 ease-in-out cursor-pointer">
                    {loading ? <Loading className="h-3.5" /> : <SearchIcon className="w-4 h-4" />}
                </button>
            </form>
        </div>
    )
}
