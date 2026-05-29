import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SearchBar } from './SearchBar';
import { libraries, people } from '../test/fixtures';

describe('SearchBar', () => {
  it('submits text + facet values as a single SearchParams object', async () => {
    const onSearch = vi.fn();
    render(
      <SearchBar libraries={libraries} people={people} onSearch={onSearch} onClear={vi.fn()} />,
    );

    await userEvent.type(screen.getByLabelText('Search query'), 'beach');
    await userEvent.type(screen.getByLabelText('Camera'), 'Canon');
    await userEvent.selectOptions(screen.getByLabelText('Library'), 'pictures');
    await userEvent.selectOptions(screen.getByLabelText('Person'), 'person-alice');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));

    expect(onSearch).toHaveBeenCalledWith(
      expect.objectContaining({
        q: 'beach',
        camera: 'Canon',
        alias: 'pictures',
        person: 'person-alice',
      }),
    );
  });

  it('clears all fields and notifies onClear', async () => {
    const onClear = vi.fn();
    render(
      <SearchBar libraries={libraries} people={people} onClear={onClear} onSearch={vi.fn()} />,
    );
    const q = screen.getByLabelText('Search query');
    await userEvent.type(q, 'beach');
    await userEvent.click(screen.getByRole('button', { name: 'Clear' }));
    expect(onClear).toHaveBeenCalled();
    expect(q).toHaveValue('');
  });

  it('lists configured libraries and people as facet options', () => {
    render(
      <SearchBar libraries={libraries} people={people} onSearch={vi.fn()} onClear={vi.fn()} />,
    );
    expect(screen.getByRole('option', { name: 'Pictures' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Videos' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Alice' })).toBeInTheDocument();
  });
});
