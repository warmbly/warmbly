import { useContext, useEffect, useState } from "react";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";

export default function useBrowseDebouncedValue(value: string, resource: string, delay = 200) {
    const userId = useContext(UserContext)?.user.id;
    const orgId = useAppStore((s) => s.currentOrganization?.id);
    const scope = `${userId}:${orgId}:${resource}`;
    const [memory, setMemory] = useState({ scope, value });
    if (memory.scope !== scope) setMemory({ scope, value });
    useEffect(() => {
        const timer = setTimeout(() => setMemory({ scope, value }), delay);
        return () => clearTimeout(timer);
    }, [scope, value, delay]);
    return memory.scope === scope ? memory.value : value;
}
