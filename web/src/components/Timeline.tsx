import { useMemo } from 'react';
import type { TimelineBucket } from '../api/types';

interface TimelineProps {
  buckets: TimelineBucket[];
  onSelect?: (date: string) => void;
}

/**
 * Capture-date histogram. Bars scale to the busiest bucket; clicking a bar narrows the
 * search to that date. The timeline is the primary temporal navigation for a DSLR
 * library where GPS is sparse (TECH_SPEC §8, §3).
 */
export function Timeline({ buckets, onSelect }: TimelineProps) {
  const max = useMemo(
    () => buckets.reduce((m, b) => Math.max(m, b.count), 0) || 1,
    [buckets],
  );

  if (buckets.length === 0) {
    return (
      <div className="text-xs text-slate-500" data-testid="timeline-empty">
        No timeline data.
      </div>
    );
  }

  return (
    <div data-testid="timeline" className="scroll-area flex items-end gap-1 overflow-x-auto py-2">
      {buckets.map((bucket) => {
        const heightPct = Math.max(6, Math.round((bucket.count / max) * 100));
        return (
          <button
            key={bucket.date}
            type="button"
            title={`${bucket.date}: ${bucket.count}`}
            aria-label={`${bucket.date}, ${bucket.count} assets`}
            onClick={() => onSelect?.(bucket.date)}
            className="flex w-6 flex-col items-center gap-1"
            data-testid="timeline-bar"
            data-date={bucket.date}
          >
            <span
              className="w-full rounded-sm bg-accent/70 transition hover:bg-accent"
              style={{ height: `${heightPct}px` }}
            />
            <span className="text-[9px] text-slate-500">{bucket.count}</span>
          </button>
        );
      })}
    </div>
  );
}
