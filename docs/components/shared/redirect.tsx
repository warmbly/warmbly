'use client';
import { useEffect } from 'react';

// Client half of the root redirect; the page's meta refresh covers loads without JS.
export function Redirect({ to }: { to: string }) {
  useEffect(() => {
    location.replace(to);
  }, [to]);
  return null;
}
