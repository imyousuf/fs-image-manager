import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { FolderBar } from './FolderBar';

describe('FolderBar', () => {
  it('renders one entry per subfolder', () => {
    render(<FolderBar dirs={['album1', '2021']} currentPath="pictures" onOpen={vi.fn()} />);
    const entries = screen.getAllByTestId('folder-entry');
    expect(entries).toHaveLength(2);
    expect(screen.getByText('album1')).toBeInTheDocument();
    expect(screen.getByText('2021')).toBeInTheDocument();
  });

  it('renders nothing when there are no subfolders', () => {
    const { container } = render(<FolderBar dirs={[]} currentPath="pictures" onOpen={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
    expect(screen.queryByTestId('folder-bar')).not.toBeInTheDocument();
  });

  it('opens a subfolder with the joined "<currentPath>/<dir>" path', async () => {
    const onOpen = vi.fn();
    render(<FolderBar dirs={['album1']} currentPath="pictures" onOpen={onOpen} />);
    await userEvent.click(screen.getByText('album1'));
    expect(onOpen).toHaveBeenCalledWith('pictures/album1');
  });

  it('joins onto a nested path without doubling slashes', async () => {
    const onOpen = vi.fn();
    render(<FolderBar dirs={['raw']} currentPath="pictures/2021/" onOpen={onOpen} />);
    await userEvent.click(screen.getByText('raw'));
    expect(onOpen).toHaveBeenCalledWith('pictures/2021/raw');
  });
});
