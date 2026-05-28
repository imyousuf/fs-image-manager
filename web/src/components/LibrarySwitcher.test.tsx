import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { LibrarySwitcher } from './LibrarySwitcher';
import { Breadcrumbs } from './Breadcrumbs';
import { libraries } from '../test/fixtures';

describe('LibrarySwitcher', () => {
  it('marks the active library and switches on click', async () => {
    const onSelect = vi.fn();
    render(
      <LibrarySwitcher libraries={libraries} activeAlias="pictures" onSelect={onSelect} />,
    );
    expect(screen.getByRole('button', { name: 'Pictures' })).toHaveAttribute(
      'aria-current',
      'page',
    );
    await userEvent.click(screen.getByRole('button', { name: 'Videos' }));
    expect(onSelect).toHaveBeenCalledWith('videos');
  });
});

describe('Breadcrumbs', () => {
  it('builds crumbs from an alias-prefixed path using the library name for the root', async () => {
    const onNavigate = vi.fn();
    render(
      <Breadcrumbs path="pictures/2021/trip" libraryName="Pictures" onNavigate={onNavigate} />,
    );
    expect(screen.getByRole('button', { name: 'Pictures' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '2021' })).toBeInTheDocument();
    // The last crumb is current and disabled.
    expect(screen.getByRole('button', { name: 'trip' })).toBeDisabled();

    await userEvent.click(screen.getByRole('button', { name: '2021' }));
    expect(onNavigate).toHaveBeenCalledWith('pictures/2021');
  });
});
