// Unibox page — three-pane mail browser.
//
//   ┌────────────────┬──────────────────────────────────────┐
//   │ ConversationList │ ThreadView                          │
//   │  (340px)         │  (fills remainder)                  │
//   └────────────────┴──────────────────────────────────────┘

import { ConversationList } from "@/components/app/unibox/ConversationList";
import { ThreadView } from "@/components/app/unibox/ThreadView";
import { useAppStore } from "@/stores";
import { InboxIcon } from "lucide-react";

export default function UniboxPage() {
    const selectedThreadId = useAppStore((s) => s.selectedThreadId);

    return (
        <div className="flex h-full bg-white">
            <div className="w-[340px] shrink-0 border-r border-slate-200 overflow-hidden flex flex-col">
                <ConversationList />
            </div>
            <div className="flex-1 min-w-0 overflow-hidden flex flex-col">
                {selectedThreadId ? (
                    <ThreadView threadId={selectedThreadId} />
                ) : (
                    <div className="flex-1 flex items-center justify-center">
                        <div className="text-center px-5">
                            <div className="w-8 h-8 rounded-md bg-slate-100 flex items-center justify-center mx-auto mb-3 text-slate-400">
                                <InboxIcon className="w-4 h-4" />
                            </div>
                            <p className="text-[12.5px] font-medium text-slate-700">
                                Select a conversation
                            </p>
                            <p className="text-[11.5px] text-slate-400 mt-1 max-w-[34ch] leading-relaxed">
                                Pick a thread from the list to read and reply.
                            </p>
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
}
