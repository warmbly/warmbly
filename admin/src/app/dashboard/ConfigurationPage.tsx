// Configuration: everything an operator can read or change about this
// deployment, in four tabs. Settings and notifications write to the instance
// settings document; environment and limits are read only. ?tab= deep-links
// a tab, and a dirty form is confirmed before a tab switch or a navigation
// discards it.

import { useCallback, useEffect, useRef, useState } from "react";
import { useBlocker, useSearchParams } from "react-router-dom";
import { Bell, Gauge, Settings2, Terminal } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { PageTabs } from "@/components/layout/PageTabs";
import { useConfirm } from "@/components/ConfirmDialog";
import { EnvironmentTab } from "./configuration/EnvironmentTab";
import { LimitsTab } from "./configuration/LimitsTab";
import { NotificationsTab } from "./configuration/NotificationsTab";
import { SettingsTab } from "./configuration/SettingsTab";

const TAB_IDS = ["settings", "notifications", "environment", "limits"] as const;
type TabId = (typeof TAB_IDS)[number];

function isTabId(v: string | null): v is TabId {
    return TAB_IDS.includes(v as TabId);
}

const DISCARD_PROMPT = {
    title: "Discard unsaved changes?",
    description:
        "You have edits that have not been saved. Leaving now throws them away.",
    confirmLabel: "Discard",
    destructive: true,
};

export default function ConfigurationPage() {
    const [params, setParams] = useSearchParams();
    const raw = params.get("tab");
    const tab: TabId = isTabId(raw) ? raw : "settings";
    const confirm = useConfirm();

    // The active tab reports its dirty state; a ref keeps the blocker's
    // predicate current without re-registering it on every keystroke.
    const [dirty, setDirty] = useState(false);
    const dirtyRef = useRef(false);
    const onDirtyChange = useCallback((d: boolean) => {
        dirtyRef.current = d;
        setDirty(d);
    }, []);

    // Only a pathname change counts: ?tab= switches are handled by setTab.
    const blocker = useBlocker(
        ({ currentLocation, nextLocation }) =>
            dirtyRef.current && currentLocation.pathname !== nextLocation.pathname,
    );

    const promptingRef = useRef(false);
    useEffect(() => {
        if (blocker.state !== "blocked" || promptingRef.current) return;
        promptingRef.current = true;
        confirm(DISCARD_PROMPT).then((ok) => {
            promptingRef.current = false;
            if (ok) blocker.proceed();
            else blocker.reset();
        });
    }, [blocker, confirm]);

    // A reload or tab close cannot show our dialog; the browser's own prompt
    // is the only thing that stands between the operator and lost edits.
    useEffect(() => {
        if (!dirty) return;
        const onBeforeUnload = (e: BeforeUnloadEvent) => {
            e.preventDefault();
        };
        window.addEventListener("beforeunload", onBeforeUnload);
        return () => window.removeEventListener("beforeunload", onBeforeUnload);
    }, [dirty]);

    function writeTab(next: TabId) {
        setParams(
            (prev) => {
                const p = new URLSearchParams(prev);
                if (next === "settings") p.delete("tab");
                else p.set("tab", next);
                return p;
            },
            { replace: true },
        );
    }

    async function setTab(next: string) {
        if (!isTabId(next) || next === tab) return;
        if (dirtyRef.current && !(await confirm(DISCARD_PROMPT))) return;
        writeTab(next);
    }

    return (
        <div>
            <PageHeader
                title="Configuration"
                description="What this instance is set to: the settings and notification channels you can edit here, the environment the backend booted with, and the limits that result."
            />

            <PageTabs
                tabs={[
                    { id: "settings", label: "Settings", icon: Settings2 },
                    { id: "notifications", label: "Notifications", icon: Bell },
                    { id: "environment", label: "Environment", icon: Terminal },
                    { id: "limits", label: "Limits", icon: Gauge },
                ]}
                value={tab}
                onChange={(id) => void setTab(id)}
            />

            {tab === "settings" && (
                <SettingsTab onDirtyChange={onDirtyChange} onSwitchTab={(t) => void setTab(t)} />
            )}
            {tab === "notifications" && <NotificationsTab onDirtyChange={onDirtyChange} />}
            {tab === "environment" && <EnvironmentTab onSwitchTab={writeTab} />}
            {tab === "limits" && <LimitsTab onSwitchTab={writeTab} />}
        </div>
    );
}
