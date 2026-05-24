export default function MiniTitle({ children }: { children: React.ReactNode }) {
    return (
        <h1 className="text-slate-500 font-semibold font-inter mb-3 text-sm uppercase tracking-wider flex items-center gap-2">
            {children}
        </h1>
    )
}
