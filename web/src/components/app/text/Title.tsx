export default function Title({ children }: { children: React.ReactNode }) {
    return <h1 className='text-gray-800 font-medium mb-1 text-[18px] font-inter'>
        {children}
    </h1>
}
