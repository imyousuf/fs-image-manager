import '@testing-library/jest-dom/vitest';
import { afterAll, afterEach, beforeAll, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { server } from './src/test/server';
import { setRequiredToken } from './src/test/handlers';

// jsdom lacks a working ResizeObserver; the virtualized grid observes its container.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;

// jsdom reports 0 for every layout dimension, which would make @tanstack/react-virtual
// render an empty viewport. Give elements a deterministic 1000x800 box so the grid
// virtualizer mounts a realistic (and bounded) subset of rows in tests.
Object.defineProperty(HTMLElement.prototype, 'clientWidth', {
  configurable: true,
  get() {
    return 1000;
  },
});
Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
  configurable: true,
  get() {
    return 800;
  },
});
Object.defineProperty(HTMLElement.prototype, 'offsetWidth', {
  configurable: true,
  get() {
    return 1000;
  },
});
Object.defineProperty(HTMLElement.prototype, 'offsetHeight', {
  configurable: true,
  get() {
    return 800;
  },
});

// jsdom does not implement matchMedia; provide a non-matching stub.
window.matchMedia = ((query: string) => ({
  matches: false,
  media: query,
  onchange: null,
  addListener: vi.fn(),
  removeListener: vi.fn(),
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
  dispatchEvent: vi.fn(),
})) as typeof window.matchMedia;

// scrollIntoView / scrollTo are used by some interactions; jsdom stubs them out.
Element.prototype.scrollIntoView = vi.fn();

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));

afterEach(() => {
  cleanup();
  server.resetHandlers();
  setRequiredToken(null);
  localStorage.clear();
});

afterAll(() => server.close());
