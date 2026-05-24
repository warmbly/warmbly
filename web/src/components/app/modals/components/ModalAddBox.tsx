import React from "react";

export default function ModalAddBox({
    placeholder,
    onSubmit,
    children,
    load,
}: {
    placeholder: string,
    onSubmit: (title: string) => Promise<void>,
    children: React.ReactNode,
    load: boolean,
}) {
    const [title, setTitle] = React.useState<string>("");

    return (
        <div className='flex bg-slate-100 rounded-xl mt-2'>
            <input
                type="text"
                className='grow py-3 px-4 outline-none placeholder:text-slate-400 text-slate-600'
                placeholder={placeholder}
                value={title}
                onChange={(e) => setTitle(e.target.value)}
            />
            <button onClick={() => onSubmit(title)} className={`shrink-0 ripple w-32 my-1 mr-1 bg-slate-300/70 ${!load ? "cursor-pointer hover:bg-slate-300" : "cursor-not-allowed"} transition flex items-center justify-center gap-1 px-3 py-2 rounded-lg text-slate-600`}>
                {children}
            </button>
        </div>
    )
}
