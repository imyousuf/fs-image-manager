import { useState } from 'react';
import type { Library, Person, SearchParams } from '../api/types';

interface SearchBarProps {
  libraries: Library[];
  people: Person[];
  initial?: SearchParams;
  onSearch: (params: SearchParams) => void;
  onClear: () => void;
}

/**
 * Full-text search plus facets (date range, camera, alias, person). Controlled
 * locally and submitted as a single SearchParams object so the container can drive the
 * `useSearch` infinite query. GPS/map is intentionally omitted — date/camera/person are
 * the priority facets for a DSLR library (TECH_SPEC §8).
 */
export function SearchBar({ libraries, people, initial, onSearch, onClear }: SearchBarProps) {
  const [q, setQ] = useState(initial?.q ?? '');
  const [from, setFrom] = useState(initial?.from ?? '');
  const [to, setTo] = useState(initial?.to ?? '');
  const [alias, setAlias] = useState(initial?.alias ?? '');
  const [camera, setCamera] = useState(initial?.camera ?? '');
  const [person, setPerson] = useState(initial?.person ?? '');

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    onSearch({
      q: q || undefined,
      from: from || undefined,
      to: to || undefined,
      alias: alias || undefined,
      camera: camera || undefined,
      person: person || undefined,
    });
  };

  const reset = () => {
    setQ('');
    setFrom('');
    setTo('');
    setAlias('');
    setCamera('');
    setPerson('');
    onClear();
  };

  return (
    <form
      data-testid="search-bar"
      onSubmit={submit}
      className="flex flex-wrap items-end gap-2 rounded-lg border border-border bg-surface-raised p-3"
    >
      <label className="flex min-w-48 flex-1 flex-col gap-1 text-xs text-slate-400">
        Search
        <input
          type="search"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="filename, tags, caption…"
          aria-label="Search query"
          className="rounded-md border border-border bg-surface px-2 py-1.5 text-sm text-slate-100"
        />
      </label>

      <label className="flex flex-col gap-1 text-xs text-slate-400">
        From
        <input
          type="date"
          value={from}
          onChange={(e) => setFrom(e.target.value)}
          aria-label="From date"
          className="rounded-md border border-border bg-surface px-2 py-1.5 text-sm text-slate-100"
        />
      </label>

      <label className="flex flex-col gap-1 text-xs text-slate-400">
        To
        <input
          type="date"
          value={to}
          onChange={(e) => setTo(e.target.value)}
          aria-label="To date"
          className="rounded-md border border-border bg-surface px-2 py-1.5 text-sm text-slate-100"
        />
      </label>

      <label className="flex flex-col gap-1 text-xs text-slate-400">
        Camera
        <input
          type="text"
          value={camera}
          onChange={(e) => setCamera(e.target.value)}
          placeholder="e.g. Canon"
          aria-label="Camera"
          className="rounded-md border border-border bg-surface px-2 py-1.5 text-sm text-slate-100"
        />
      </label>

      <label className="flex flex-col gap-1 text-xs text-slate-400">
        Library
        <select
          value={alias}
          onChange={(e) => setAlias(e.target.value)}
          aria-label="Library"
          className="rounded-md border border-border bg-surface px-2 py-1.5 text-sm text-slate-100"
        >
          <option value="">All</option>
          {libraries.map((lib) => (
            <option key={lib.alias} value={lib.alias}>
              {lib.name}
            </option>
          ))}
        </select>
      </label>

      <label className="flex flex-col gap-1 text-xs text-slate-400">
        Person
        <select
          value={person}
          onChange={(e) => setPerson(e.target.value)}
          aria-label="Person"
          className="rounded-md border border-border bg-surface px-2 py-1.5 text-sm text-slate-100"
        >
          <option value="">Anyone</option>
          {people.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name || 'Unknown'}
            </option>
          ))}
        </select>
      </label>

      <div className="flex gap-2">
        <button
          type="submit"
          className="rounded-md bg-accent px-4 py-1.5 text-sm font-medium text-slate-900 hover:bg-accent-strong"
        >
          Search
        </button>
        <button
          type="button"
          onClick={reset}
          className="rounded-md border border-border px-3 py-1.5 text-sm text-slate-300 hover:bg-surface-hover"
        >
          Clear
        </button>
      </div>
    </form>
  );
}
