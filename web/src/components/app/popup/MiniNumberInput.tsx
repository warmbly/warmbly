import React from "react";

export default function MiniNumberInput({onChange, placeholder, value, id, name}: {onChange: (e: React.ChangeEvent<HTMLInputElement>) => void, placeholder?: string, value?: number, id?: string, name?: string}){
    const [isNull, setIsNull] = React.useState<boolean>(false);
    return <div>
        <input 
         type="number"  
         value={isNull ? "":value}
         onChange={(e) => {
            if (e.target.value !== ""){
                onChange(e)
                setIsNull(false)
            } else {
                setIsNull(true)
            }
         }}
         id={id}
         name={name}
         className="border font-sans shadow-sm border-gray-200 placeholder:text-slate-400 text-slate-700 text-sm rounded-md ring-0 outline-none focus:border-gray-300 block w-full p-2.5" 
         placeholder={placeholder} 
         required />
    </div>
}