"use client";

interface HexProps {
    color: string;
}

const Hex = ({color}: HexProps) => {
    return (
        <div className="flex justify-center items-center w-28 text-xs rounded-md py-1.5 px-2 border border-gray-300 bg-gray-100">
            <span className="text-slate-500 mr-2 ">HEX</span>
            <span className="w-20 text-center">{color}</span>
        </div>
    )
}
export default Hex;