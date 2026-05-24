import { useContext, createContext } from "react";

interface ConfirmContextValue {
    show: (text: string, onSubmit: () => void | Promise<void>) => void,
    setLoading: React.Dispatch<React.SetStateAction<boolean>>,
    setShow: React.Dispatch<React.SetStateAction<boolean>>,
}

export const ConfirmContext = createContext<ConfirmContextValue | undefined>(undefined);

export function useConfirm() {
    const c = useContext(ConfirmContext);
    if (!c) {
        throw Error("ConfirmProvider not found")
    }
    return c
}
