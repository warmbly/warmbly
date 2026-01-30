'use client';

import { useState } from 'react';
import { ChevronDown, Terminal, Code2 } from 'lucide-react';

const languages = [
  { id: 'curl', name: 'cURL', icon: Terminal, available: true },
  { id: 'typescript', name: 'TypeScript', icon: Code2, available: false },
  { id: 'python', name: 'Python', icon: Code2, available: false },
  { id: 'go', name: 'Go', icon: Code2, available: false },
] as const;

type LanguageId = typeof languages[number]['id'];

export function LanguageSelector() {
  const [selected, setSelected] = useState<LanguageId>('curl');
  const [open, setOpen] = useState(false);

  const selectedLang = languages.find(l => l.id === selected)!;
  const Icon = selectedLang.icon;

  return (
    <div className="relative">
      <button
        onClick={() => setOpen(!open)}
        className="inline-flex items-center gap-1.5 rounded-md border border-fd-border bg-fd-background px-3 py-1.5 text-sm font-medium text-fd-foreground hover:bg-fd-accent transition-colors"
      >
        <Icon className="h-3.5 w-3.5" />
        <span className="hidden sm:inline">{selectedLang.name}</span>
        <ChevronDown className="h-3 w-3 opacity-50" />
      </button>

      {open && (
        <>
          <div
            className="fixed inset-0 z-40"
            onClick={() => setOpen(false)}
          />
          <div className="absolute right-0 top-full mt-1 z-50 w-48 rounded-md border border-fd-border bg-fd-popover p-1 shadow-lg">
            <div className="px-2 py-1.5 text-xs font-semibold text-fd-muted-foreground">
              Code Examples
            </div>
            <div className="h-px bg-fd-border my-1" />
            {languages.map((lang) => {
              const LangIcon = lang.icon;
              return (
                <button
                  key={lang.id}
                  onClick={() => {
                    if (lang.available) {
                      setSelected(lang.id);
                    }
                    setOpen(false);
                  }}
                  disabled={!lang.available}
                  className={`flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm ${
                    lang.available
                      ? 'hover:bg-fd-accent cursor-pointer'
                      : 'opacity-50 cursor-not-allowed'
                  }`}
                >
                  <LangIcon className="h-4 w-4" />
                  <span>{lang.name}</span>
                  {!lang.available && (
                    <span className="ml-auto text-xs text-fd-muted-foreground">Soon</span>
                  )}
                  {lang.id === selected && lang.available && (
                    <span className="ml-auto text-xs text-fd-primary">&#10003;</span>
                  )}
                </button>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}
