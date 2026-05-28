import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Timeline } from './Timeline';
import { timeline } from '../test/fixtures';

describe('Timeline', () => {
  it('renders one bar per bucket', () => {
    render(<Timeline buckets={timeline} />);
    expect(screen.getAllByTestId('timeline-bar')).toHaveLength(2);
  });

  it('emits the bucket date when a bar is clicked', async () => {
    const onSelect = vi.fn();
    render(<Timeline buckets={timeline} onSelect={onSelect} />);
    await userEvent.click(screen.getByRole('button', { name: /2021-06-15/ }));
    expect(onSelect).toHaveBeenCalledWith('2021-06-15');
  });

  it('shows an empty state with no buckets', () => {
    render(<Timeline buckets={[]} />);
    expect(screen.getByTestId('timeline-empty')).toBeInTheDocument();
  });
});
