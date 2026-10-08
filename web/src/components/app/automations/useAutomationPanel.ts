import { useCallback, useState } from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { browseFlagSchema } from "@/lib/browse-other-lists";

export default function useAutomationPanel(automationID: string) {
    const [historyOpen, setHistoryOpen] = useBrowseState(`automations.${automationID}.history.open`, false, browseFlagSchema);
    const [testOpen, setTestOpen] = useState(false);
    const closePanel = useCallback(() => {
        setHistoryOpen(false);
        setTestOpen(false);
    }, [setHistoryOpen]);
    const showTest = useCallback(() => {
        setHistoryOpen(false);
        setTestOpen(true);
    }, [setHistoryOpen]);
    const toggleHistory = useCallback(() => {
        setTestOpen(false);
        setHistoryOpen((open) => !open);
    }, [setHistoryOpen]);
    const toggleTest = useCallback(() => {
        setHistoryOpen(false);
        setTestOpen((open) => !open);
    }, [setHistoryOpen]);

    return { panel: testOpen ? "test" as const : historyOpen ? "history" as const : null, closePanel, showTest, toggleHistory, toggleTest };
}
