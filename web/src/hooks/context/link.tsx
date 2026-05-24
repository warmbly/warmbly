import { createContext, useContext } from "react";

interface LinkContext {
    show: (displayName: string, onSubmit: (displayName: string, url: string) => void) => void,
}

export const LinkContext = createContext<LinkContext | undefined>(undefined);

export function useLink() {
    return useContext(LinkContext);
}
