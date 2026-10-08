import { useContext, useEffect, useState } from "react";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";

export default function useBrowseDebouncedValue<T>(value: T, resource: string, delay = 300): T {
  const user = useContext(UserContext)?.user.id;
  const workspace = useAppStore((state) => state.currentOrganization?.id);
  const identity = JSON.stringify([user, workspace, resource]);
  const [memory, setMemory] = useState({ identity, value });
  let current = memory;
  if (memory.identity !== identity) {
    current = { identity, value };
    setMemory(current);
  }
  useEffect(() => {
    if (!value) {
      setMemory((previous) => previous.value === value && previous.identity === identity ? previous : { identity, value });
      return;
    }
    const timer = window.setTimeout(() => setMemory({ identity, value }), delay);
    return () => window.clearTimeout(timer);
  }, [value, identity, delay]);
  return current.value;
}
