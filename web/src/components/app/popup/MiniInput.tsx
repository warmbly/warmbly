const MiniInput = ({onChange, placeholder, value, id, name}: {onChange: (e: React.ChangeEvent<HTMLInputElement>) => void, placeholder?: string, value?: string, id?: string, name?: string}) => {
    return <>
        <input className="w-full font-sans text-[15px] bg-transparent placeholder:text-slate-400 text-slate-700 border border-slate-200 rounded-md px-3 py-2.5 transition duration-300 ease focus:outline-none focus:border-slate-300 hover:border-slate-300 shadow-sm focus:shadow" type="text" onChange={onChange} placeholder={placeholder} value={value} id={id} name={name}/>
    </>
}

export default MiniInput;