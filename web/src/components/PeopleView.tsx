import { useState } from 'react';
import type { Person } from '../api/types';
import { thumbUrl } from '../api/client';

interface PeopleViewProps {
  people: Person[];
  /** Persist a cluster's name. */
  onRename: (id: string, name: string) => void;
  /** Open the assets for a person (drives the grid via search ?person=). */
  onOpenPerson: (person: Person) => void;
  renamingId?: string | null;
}

/**
 * People (face clusters). Each cluster shows its cover face; an unnamed cluster reads
 * "Unknown" and can be named inline. Naming a cluster lets new faces auto-assign to the
 * person server-side (TECH_SPEC §9.2). The cover thumbnail reuses the asset thumb URL —
 * coverFaceId maps to an asset id in the catalog.
 */
export function PeopleView({ people, onRename, onOpenPerson, renamingId }: PeopleViewProps) {
  if (people.length === 0) {
    return (
      <div className="p-6 text-sm text-slate-500" data-testid="people-empty">
        No people detected yet. Faces appear here once enrichment has run.
      </div>
    );
  }
  return (
    <div
      data-testid="people-view"
      className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-4 p-4"
    >
      {people.map((person) => (
        <PersonCard
          key={person.id}
          person={person}
          onRename={onRename}
          onOpenPerson={onOpenPerson}
          busy={renamingId === person.id}
        />
      ))}
    </div>
  );
}

function PersonCard({
  person,
  onRename,
  onOpenPerson,
  busy,
}: {
  person: Person;
  onRename: (id: string, name: string) => void;
  onOpenPerson: (person: Person) => void;
  busy?: boolean;
}) {
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(person.name);
  const named = person.name.trim().length > 0;

  const commit = () => {
    const trimmed = name.trim();
    if (trimmed && trimmed !== person.name) onRename(person.id, trimmed);
    setEditing(false);
  };

  return (
    <div data-testid="person-card" className="flex flex-col gap-2">
      <button
        type="button"
        onClick={() => onOpenPerson(person)}
        aria-label={`Photos of ${named ? person.name : 'unknown person'}`}
        className="aspect-square overflow-hidden rounded-full bg-surface-raised ring-2 ring-border hover:ring-accent"
      >
        {person.coverFaceId ? (
          <img
            src={thumbUrl(person.coverFaceId, 160)}
            alt=""
            className="h-full w-full object-cover"
          />
        ) : (
          <span className="flex h-full w-full items-center justify-center text-2xl text-slate-600">
            ?
          </span>
        )}
      </button>

      {editing ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            commit();
          }}
          className="flex flex-col gap-1"
        >
          <input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={commit}
            aria-label="Person name"
            placeholder="Name this person"
            className="rounded-md border border-border bg-surface px-2 py-1 text-sm text-slate-100"
          />
        </form>
      ) : (
        <button
          type="button"
          onClick={() => {
            setName(person.name);
            setEditing(true);
          }}
          disabled={busy}
          className="truncate text-center text-sm text-slate-200 hover:text-accent"
        >
          {busy ? 'Saving…' : named ? person.name : 'Name this person'}
        </button>
      )}
    </div>
  );
}
