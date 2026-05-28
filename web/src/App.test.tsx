import { describe, expect, it } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from './App';
import { renderWithQuery } from './test/render';

describe('App (integration over MSW)', () => {
  it('loads libraries, defaults to the first, and browses its assets', async () => {
    renderWithQuery(<App />);

    // Library switcher populated from /api/libraries.
    const switcher = await screen.findByTestId('library-switcher');
    expect(within(switcher).getByRole('button', { name: 'Pictures' })).toBeInTheDocument();
    expect(within(switcher).getByRole('button', { name: 'Videos' })).toBeInTheDocument();

    // Pictures (default) shows its two assets — RAW+JPG appears once.
    await waitFor(() => expect(screen.getAllByTestId('asset-tile')).toHaveLength(2));
    expect(screen.getByTestId('raw-badge')).toBeInTheDocument();
  });

  it('switches libraries and shows the video with a play badge', async () => {
    renderWithQuery(<App />);
    const switcher = await screen.findByTestId('library-switcher');
    await userEvent.click(within(switcher).getByRole('button', { name: 'Videos' }));
    await waitFor(() => expect(screen.getByTestId('video-badge')).toBeInTheDocument());
  });

  it('opens an asset in the lightbox', async () => {
    renderWithQuery(<App />);
    await waitFor(() => expect(screen.getAllByTestId('asset-tile').length).toBeGreaterThan(0));
    await userEvent.click(screen.getByRole('button', { name: /open img_1234/i }));
    expect(await screen.findByTestId('lightbox')).toBeInTheDocument();
    expect(screen.getByTestId('preview-image')).toBeInTheDocument();
  });

  it('searches and narrows the grid to matching assets', async () => {
    renderWithQuery(<App />);
    await waitFor(() => expect(screen.getAllByTestId('asset-tile').length).toBeGreaterThan(0));

    await userEvent.type(screen.getByLabelText('Search query'), 'MVI');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));

    await waitFor(() => expect(screen.getAllByTestId('asset-tile')).toHaveLength(1));
    expect(screen.getByTestId('video-badge')).toBeInTheDocument();
  });

  it('renders navigable subfolders and descends into one on click', async () => {
    renderWithQuery(<App />);

    // The browse response for the library root carries dirs:["album1","2021"].
    const folderBar = await screen.findByTestId('folder-bar');
    expect(within(folderBar).getByText('album1')).toBeInTheDocument();
    expect(within(folderBar).getByText('2021')).toBeInTheDocument();

    await userEvent.click(within(folderBar).getByText('album1'));

    // Descending updates the breadcrumb trail to the joined "pictures/album1" path.
    const crumbs = await screen.findByTestId('breadcrumbs');
    await waitFor(() => expect(within(crumbs).getByText('album1')).toBeInTheDocument());
    expect(within(crumbs).getByRole('button', { name: 'Pictures' })).toBeInTheDocument();
  });

  it('renders the People view with cluster naming', async () => {
    renderWithQuery(<App />);
    const peopleTab = await screen.findByRole('button', { name: 'People' });
    await userEvent.click(peopleTab);
    const peopleView = await screen.findByTestId('people-view');
    // Scope to the People view — "Alice" also appears in the search Person facet.
    expect(within(peopleView).getByText('Alice')).toBeInTheDocument();
    expect(within(peopleView).getByText('Name this person')).toBeInTheDocument();
  });

  it('shows the timeline built from /api/assets/timeline', async () => {
    renderWithQuery(<App />);
    await waitFor(() => expect(screen.getAllByTestId('timeline-bar').length).toBe(2));
  });

  it('reveals the upload drop-zone targeting the active library', async () => {
    renderWithQuery(<App />);
    const uploadTab = await screen.findByRole('button', { name: 'Upload' });
    await userEvent.click(uploadTab);
    const zone = await screen.findByTestId('upload-dropzone');
    expect(zone).toHaveTextContent('pictures');
  });
});
