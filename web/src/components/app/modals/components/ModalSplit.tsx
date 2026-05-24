import type React from "react";

export default function ModalSplit({
    children,
    icon,
}: {
    children: React.ReactNode,
    icon: React.ReactNode,
}) {
    return (

        <div className='grid h-full lg:grid-cols-2 gap-10 overflow-y-scroll md:overflow-y-auto'>
            <div className='md:overflow-y-scroll'>
                {children}
            </div>
            <div className='flex justify-center items-center'>
                <div>
                    {icon}
                </div>
            </div>
        </div>
    )
}
