"use client";

import Close from '@/components/icons/Close';
import Warning from '@/components/icons/Warning';
import React, { createContext, useContext } from 'react';

export const ErrorContext = createContext({
    showError: (title: string, description: string, action?: () => Promise<void>) => {},
});

export const ErrorProvider = ({ children }: { children: React.ReactNode }) => {
    const [errorTitle, setErrorTitle] = React.useState<string>("");
    const [errorText, setErrorText] = React.useState<string>("");
    const [visible, setVisible] = React.useState<boolean>(false);
    const funcRef = React.useRef<() => Promise<void>>(async () => {});

    const showError = (tt: string, msg: string, action?: () => Promise<void>) => {
        setErrorTitle(tt)
        setErrorText(msg)
        setVisible(true);
        funcRef.current = action ?? (async () => {});
    };

    return (
        <ErrorContext.Provider value={{ showError }}>
            {children}
            <div className={`fixed flex inset-0 z-100 bg-slate-950/45 justify-center items-center transition ${visible ? "opacity-100 visible":"opacity-0 invisible"}`}>
                <div className={`bg-gray-100 flex flex-col gap-4 items-center max-w-xl w-full mx-6 p-10 relative transition ease-bezier duration-500 ${visible ? "scale-100":"scale-90"}`}>
                    <div className='flex justify-center rounded-full text-red-600'>
                        <Warning className='w-10'/>
                    </div>
                    <h1 className="font-bold text-xl text-gray-800 text-center">{errorTitle}</h1>
                    <p className="text-red-400 text-center text-lg">{errorText}</p>
                    <div onClick={async () => {
                        await funcRef.current();
                        setVisible(false);
                    }} className='absolute top-4 right-4 cursor-pointer'>
                        <p className='text-lg transition text-gray-400 hover:text-gray-500'>
                            <Close className='w-6'/>
                        </p>
                    </div>
                </div>
            </div>
        </ErrorContext.Provider>
    );
};

export function useError() {
  return useContext(ErrorContext);
}