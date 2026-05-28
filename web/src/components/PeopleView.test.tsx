import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { PeopleView } from './PeopleView';
import { people } from '../test/fixtures';

describe('PeopleView', () => {
  it('renders a card per cluster; unnamed clusters prompt for a name', () => {
    render(<PeopleView people={people} onRename={vi.fn()} onOpenPerson={vi.fn()} />);
    expect(screen.getAllByTestId('person-card')).toHaveLength(2);
    expect(screen.getByText('Alice')).toBeInTheDocument();
    expect(screen.getByText('Name this person')).toBeInTheDocument();
  });

  it('names an unknown cluster and persists via onRename', async () => {
    const onRename = vi.fn();
    render(<PeopleView people={people} onRename={onRename} onOpenPerson={vi.fn()} />);
    await userEvent.click(screen.getByRole('button', { name: 'Name this person' }));
    const input = screen.getByLabelText('Person name');
    await userEvent.type(input, 'Bob');
    await userEvent.keyboard('{Enter}');
    expect(onRename).toHaveBeenCalledWith('person-unknown', 'Bob');
  });

  it('opens a person to browse their photos', async () => {
    const onOpenPerson = vi.fn();
    render(<PeopleView people={people} onRename={vi.fn()} onOpenPerson={onOpenPerson} />);
    await userEvent.click(screen.getByRole('button', { name: /photos of alice/i }));
    expect(onOpenPerson).toHaveBeenCalledWith(people[0]);
  });

  it('shows an empty state when no people are detected', () => {
    render(<PeopleView people={[]} onRename={vi.fn()} onOpenPerson={vi.fn()} />);
    expect(screen.getByTestId('people-empty')).toBeInTheDocument();
  });
});
