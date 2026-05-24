export default interface WebSocketProviderProps {
    children: React.ReactNode;

    /** Optional global callbacks (rarely needed) */
    onOpen?: (ev: Event) => void;
    onClose?: (ev: CloseEvent) => void;
    onError?: (ev: Event) => void;
}
