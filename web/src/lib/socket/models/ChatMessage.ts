export default interface ChatMessage {
    type: 'chat';
    id: string;
    user: string;
    text: string;
    ts: number;
}
