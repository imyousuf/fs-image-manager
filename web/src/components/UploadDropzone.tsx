import { useId, useRef, useState } from 'react';
import clsx from 'clsx';

interface UploadDropzoneProps {
  alias: string;
  dir: string;
  /** Uploads one file; resolves on success, rejects on error. */
  onUpload: (file: File) => Promise<void>;
  disabled?: boolean;
}

interface FileState {
  name: string;
  status: 'pending' | 'done' | 'error';
  error?: string;
}

/**
 * Drag-and-drop (or click-to-pick) upload into the current library folder
 * (POST /api/upload?alias=&dir=). Uploads sequentially and reports per-file status; the
 * server triggers ingestion. Presentational w.r.t. the upload itself — `onUpload` is
 * injected so it can be mocked in tests.
 */
export function UploadDropzone({ alias, dir, onUpload, disabled }: UploadDropzoneProps) {
  const inputId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const [files, setFiles] = useState<FileState[]>([]);

  const handleFiles = async (list: FileList | File[]) => {
    const arr = Array.from(list);
    if (arr.length === 0) return;
    setFiles((prev) => [...prev, ...arr.map((f) => ({ name: f.name, status: 'pending' as const }))]);
    for (const file of arr) {
      try {
        await onUpload(file);
        setFiles((prev) =>
          prev.map((f) => (f.name === file.name ? { ...f, status: 'done' } : f)),
        );
      } catch (err) {
        setFiles((prev) =>
          prev.map((f) =>
            f.name === file.name
              ? { ...f, status: 'error', error: err instanceof Error ? err.message : 'failed' }
              : f,
          ),
        );
      }
    }
  };

  return (
    <div data-testid="upload-dropzone">
      <label
        htmlFor={inputId}
        onDragOver={(e) => {
          e.preventDefault();
          if (!disabled) setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          if (!disabled) void handleFiles(e.dataTransfer.files);
        }}
        className={clsx(
          'flex cursor-pointer flex-col items-center justify-center gap-1 rounded-lg border-2 border-dashed p-6 text-center text-sm transition',
          dragging ? 'border-accent bg-accent/10' : 'border-border',
          disabled && 'cursor-not-allowed opacity-50',
        )}
      >
        <span className="font-medium text-slate-200">Drop files to upload</span>
        <span className="text-xs text-slate-400">
          into <code className="text-accent">{alias}</code>
          {dir ? `/${dir}` : ' (root)'}
        </span>
        <input
          id={inputId}
          ref={inputRef}
          type="file"
          multiple
          disabled={disabled}
          aria-label="Choose files to upload"
          className="sr-only"
          onChange={(e) => {
            if (e.target.files) void handleFiles(e.target.files);
            e.target.value = '';
          }}
        />
      </label>

      {files.length > 0 && (
        <ul data-testid="upload-list" className="mt-3 space-y-1 text-xs">
          {files.map((f, i) => (
            <li key={`${f.name}-${i}`} className="flex items-center justify-between gap-2">
              <span className="truncate text-slate-300">{f.name}</span>
              <span
                className={clsx(
                  f.status === 'done' && 'text-emerald-400',
                  f.status === 'error' && 'text-red-400',
                  f.status === 'pending' && 'text-slate-500',
                )}
              >
                {f.status === 'done' && 'Uploaded'}
                {f.status === 'pending' && 'Uploading…'}
                {f.status === 'error' && (f.error ?? 'Failed')}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
