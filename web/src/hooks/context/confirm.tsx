import { useContext, createContext } from "react";

/** How the dialog reads; the default is the red "Are you sure?" of a destructive action. */
export interface ConfirmOptions {
    title?: string;
    confirmLabel?: string;
    tone?: "danger" | "primary";
}

interface ConfirmContextValue {
    /** onCancel runs when the dialog is dismissed without confirming. */
    show: (text: string, onSubmit: () => void | Promise<void>, onCancel?: () => void, options?: ConfirmOptions) => void,
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
