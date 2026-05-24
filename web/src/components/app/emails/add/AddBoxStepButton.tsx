import { RiArrowLeftSLine, RiArrowRightSLine } from "@remixicon/react"
import { Loading } from "@/components/loader"

export default function AddBoxStepButton({ next = false, tt, loading = false, onClick }: { next?: boolean, tt?: string, loading?: boolean, onClick?: () => Promise<void> | void }) {
    return <button onClick={onClick} className={`border ${next && "bg-blue-500 text-white"} border-gray-200 justify-center transition-transform hover:scale-98 cursor-pointer shadow-sm px-3 py-2 rounded-lg flex items-center gap-2`}>
        {!loading ? <>
            {!next && <RiArrowLeftSLine className="w-4.5" />}
            <div>{!tt ? next ? "Next" : "Back" : tt}</div>
            {next && <RiArrowRightSLine className="w-4.5" />}
        </> : <Loading className={`h-6 ${next ? "text-white" : "text-black"}`} />}
    </button>
}
