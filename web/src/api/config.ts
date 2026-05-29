// Runtime configuration for the API client.
//
// The auth model (see _contracts.md §5) is a single optional bearer token: if the
// server has `[auth] token` set, every request must carry `Authorization: Bearer
// <token>`; otherwise the API is open (LAN use). Since the UI is embedded and served
// from the same origin, the token is supplied at runtime rather than baked into the
// build:
//   1. `window.__FSIM_CONFIG__.token` — the server may inject this into index.html.
//   2. `localStorage["fsim.token"]` — set via the in-app token field (phone/LAN).

export interface RuntimeConfig {
  /** API origin; empty string means same-origin (the embedded/proxied default). */
  baseUrl: string;
  /** Bearer token, or null when the API is open. */
  token: string | null;
}

declare global {
  interface Window {
    __FSIM_CONFIG__?: Partial<RuntimeConfig>;
  }
}

const TOKEN_STORAGE_KEY = 'fsim.token';

function readStoredToken(): string | null {
  try {
    return globalThis.localStorage?.getItem(TOKEN_STORAGE_KEY) ?? null;
  } catch {
    return null;
  }
}

export function getConfig(): RuntimeConfig {
  const injected = (typeof window !== 'undefined' && window.__FSIM_CONFIG__) || {};
  const token = injected.token ?? readStoredToken();
  return {
    baseUrl: injected.baseUrl ?? '',
    token: token && token.length > 0 ? token : null,
  };
}

export function setStoredToken(token: string | null): void {
  try {
    if (token && token.length > 0) {
      globalThis.localStorage?.setItem(TOKEN_STORAGE_KEY, token);
    } else {
      globalThis.localStorage?.removeItem(TOKEN_STORAGE_KEY);
    }
  } catch {
    // Storage unavailable (private mode / SSR) — token simply isn't persisted.
  }
}
