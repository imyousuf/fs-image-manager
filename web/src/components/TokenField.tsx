import { useState } from 'react';
import { getConfig, setStoredToken } from '../api/config';

/**
 * Lets a user paste the bearer token for a token-protected server (LAN/phone use).
 * Stored in localStorage; the API client reads it per request. No-op visually when the
 * server is open, but always available behind the gear.
 */
export function TokenField() {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState(getConfig().token ?? '');
  const [saved, setSaved] = useState(false);

  const save = () => {
    setStoredToken(value.trim() || null);
    setSaved(true);
    setTimeout(() => setSaved(false), 1500);
  };

  return (
    <div className="relative">
      <button
        type="button"
        aria-label="API token settings"
        onClick={() => setOpen((o) => !o)}
        className="rounded-md p-1.5 text-slate-400 hover:bg-surface-hover hover:text-slate-200"
      >
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2">
          <circle cx="12" cy="12" r="3" />
          <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
        </svg>
      </button>
      {open && (
        <div
          data-testid="token-field"
          className="absolute right-0 z-30 mt-2 w-64 rounded-md border border-border bg-surface-raised p-3 shadow-xl"
        >
          <label className="flex flex-col gap-1 text-xs text-slate-400">
            API token
            <input
              type="password"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder="Bearer token (optional)"
              aria-label="API token"
              className="rounded-md border border-border bg-surface px-2 py-1.5 text-sm text-slate-100"
            />
          </label>
          <button
            type="button"
            onClick={save}
            className="mt-2 w-full rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-slate-900 hover:bg-accent-strong"
          >
            {saved ? 'Saved' : 'Save'}
          </button>
        </div>
      )}
    </div>
  );
}
