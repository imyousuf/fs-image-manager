import { describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { UploadDropzone } from './UploadDropzone';

describe('UploadDropzone', () => {
  it('shows the target alias and directory', () => {
    render(<UploadDropzone alias="pictures" dir="2021" onUpload={vi.fn().mockResolvedValue(undefined)} />);
    const zone = screen.getByTestId('upload-dropzone');
    expect(zone).toHaveTextContent('pictures');
    expect(zone).toHaveTextContent('2021');
  });

  it('uploads picked files and marks each done', async () => {
    const onUpload = vi.fn().mockResolvedValue(undefined);
    render(<UploadDropzone alias="pictures" dir="" onUpload={onUpload} />);
    const file = new File(['data'], 'IMG_9999.JPG', { type: 'image/jpeg' });
    await userEvent.upload(screen.getByLabelText('Choose files to upload'), file);

    await waitFor(() => expect(onUpload).toHaveBeenCalledWith(file));
    await waitFor(() => expect(screen.getByText('Uploaded')).toBeInTheDocument());
  });

  it('reports a per-file error when an upload fails', async () => {
    const onUpload = vi.fn().mockRejectedValue(new Error('disk full'));
    render(<UploadDropzone alias="pictures" dir="" onUpload={onUpload} />);
    const file = new File(['data'], 'bad.JPG', { type: 'image/jpeg' });
    await userEvent.upload(screen.getByLabelText('Choose files to upload'), file);
    await waitFor(() => expect(screen.getByText('disk full')).toBeInTheDocument());
  });
});
