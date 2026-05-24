import { RiArrowDropLeftLine } from "@remixicon/react"
import React from "react"

export default function AddBoxTopBack({ onClick, children }: { onClick: () => void, children: React.ReactNode }) {
    return <>
        <div className="flex flex-col sm:flex-row gap-2 sm:gap-20">
            <div onClick={() => onClick()} className="flex gap-2 text-gray-500 hover:text-gray-700 cursor-pointer transition items-center">
                <RiArrowDropLeftLine className="w-8 -mt-0.5" />
                <p>Go Back</p>
            </div>
            <div className="w-0.5 bg-gray-400 opacity-20"></div>
            <h1 className="font-poppins">{children}</h1>
        </div>
        <hr className="my-5 opacity-10" />
    </>
}
