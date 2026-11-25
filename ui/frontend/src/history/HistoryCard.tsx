import { Button, FormActions } from '../components/ui';
import { imageLabel } from '../imageLabel';
import { providerLabel } from '../sources/SourceHealth';
import type { Exclusion, HistoryEntry } from '../types';

export type Thumbnail = { src: string } | 'busy' | null;

function date(value?: string): string {
  if (!value) return 'Time unavailable';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime())
    ? 'Time unavailable'
    : parsed.toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
}
function reason(value?: string): string {
  const reasons: Record<string, string> = {
    excluded: 'Excluded from rotation. Restore it in Excluded to display it again.',
    filter_mismatch: 'Does not match the current image rules.',
    outside_enabled_inputs: 'Its source is disabled, removed or changed.',
    outside_active_theme: 'This wallpaper belongs to a different or unavailable Omarchy theme.',
    original_unavailable: 'The original file is unavailable.',
    configuration_invalid: 'Fix the saved image settings before displaying again.',
    monitor_geometry_unavailable: 'Display size is unavailable. Review image settings.',
    monitor_geometry_ambiguous: 'More than one display was found. Review image settings.',
    monitor_geometry_unsupported:
      'This desktop cannot report the display size. Choose another image rule.',
    monitor_geometry_query_failed: 'The display size could not be checked. Review image settings.',
    backend_unavailable:
      'The configured desktop connection is unavailable. Review desktop settings.',
    backend_ambiguous: 'The desktop connection is ambiguous. Choose a backend in Settings.',
    desktop_capabilities_unavailable:
      'Desktop capabilities could not be checked. Review desktop settings.',
  };
  return (
    reasons[value || ''] ||
    (value?.startsWith('monitor_geometry_')
      ? 'The display could not be resolved. Review image settings.'
      : 'Unavailable with the current sources, files or image rules. Review Settings or Sources.')
  );
}
export function ExcludedCard({
  entry,
  disabled,
  onRestore,
}: {
  entry: Exclusion;
  disabled: boolean;
  onRestore(): void;
}) {
  const title = imageLabel(entry);
  const headingID = `excluded-image-${entry.id}-title`;
  return (
    <li
      data-history-id={entry.id}
      aria-labelledby={headingID}
      className="history-card excluded-card"
    >
      <div>
        <h2 id={headingID} tabIndex={-1}>
          {title}
        </h2>
        <p>
          {providerLabel(entry.provider)} · Excluded {date(entry.excluded_at)}
        </p>
      </div>
      <Button
        className="outline"
        data-history-action="restore"
        aria-label={`Restore to rotation: ${title}`}
        disabled={disabled}
        onClick={onRestore}
      >
        Restore to rotation
      </Button>
    </li>
  );
}

export function HistoryCard({
  entry,
  thumbnail,
  source,
  currentLabel,
  excluded,
  disabled,
  replayDisabled,
  onPreviewError,
  onReplay,
  onOpen,
  onManageExcluded,
  onReviewSources,
  onReviewRules,
}: {
  entry: HistoryEntry;
  thumbnail?: Thumbnail;
  source: string;
  currentLabel?: string;
  excluded: boolean;
  disabled: boolean;
  replayDisabled: boolean;
  onPreviewError(): void;
  onReplay(): void;
  onOpen(target: 'page' | 'folder'): void;
  onManageExcluded(): void;
  onReviewSources(): void;
  onReviewRules(): void;
}) {
  const title = imageLabel(entry);
  const id = `history-image-${encodeURIComponent(entry.id)}`;
  const unavailable = excluded || entry.availability === 'unavailable';
  const why = excluded ? 'excluded' : entry.reason;
  const sourceIssue =
    why === 'outside_enabled_inputs' ||
    why === 'original_unavailable' ||
    why === 'outside_active_theme';
  const reviewLabel = sourceIssue ? 'Review sources' : 'Review image settings';

  return (
    <li data-history-id={entry.id} className="history-card" aria-labelledby={`${id}-title`}>
      <div className="history-thumbnail">
        {thumbnail === null ? (
          'Preview unavailable'
        ) : thumbnail === 'busy' ? (
          'Preview busy. Reload history to try again.'
        ) : thumbnail ? (
          <img src={thumbnail.src} alt={`Preview of ${title}`} onError={onPreviewError} />
        ) : (
          'Loading preview…'
        )}
      </div>
      <div>
        <h2 id={`${id}-title`} tabIndex={-1}>
          {title}
        </h2>
        <p>
          {entry.dimensions ? `${entry.dimensions.width} × ${entry.dimensions.height} · ` : ''}
          {source} · {date(entry.accepted_at)}
        </p>
        {currentLabel && <p className="history-current">{currentLabel}</p>}
        <p id={`${id}-availability`}>
          {unavailable
            ? reason(why)
            : entry.availability === 'cached'
              ? 'Cached copy available · Checked again before display.'
              : 'Original will be checked when you display it again.'}
        </p>
        <FormActions>
          <Button
            className="primary"
            data-history-action="replay"
            aria-label={`Display again: ${title}`}
            aria-describedby={unavailable ? `${id}-availability` : undefined}
            disabled={disabled || replayDisabled || unavailable}
            onClick={onReplay}
          >
            Display again
          </Button>
          {entry.attribution?.page_url && (
            <Button
              className="outline"
              data-history-action="page"
              aria-label={`View on website: ${title}`}
              disabled={disabled}
              onClick={() => onOpen('page')}
            >
              View on website ↗
            </Button>
          )}
          {entry.original_path && (
            <Button
              className="outline"
              data-history-action="folder"
              aria-label={`Show in folder: ${title}`}
              disabled={disabled}
              onClick={() => onOpen('folder')}
            >
              Show in folder
            </Button>
          )}
          {unavailable &&
            (why === 'excluded' ? (
              <Button
                className="outline"
                aria-label={`Manage excluded: ${title}`}
                onClick={onManageExcluded}
              >
                Manage excluded
              </Button>
            ) : (
              <Button
                className="outline"
                aria-label={`${reviewLabel}: ${title}`}
                onClick={sourceIssue ? onReviewSources : onReviewRules}
              >
                {reviewLabel}
              </Button>
            ))}
        </FormActions>
      </div>
    </li>
  );
}
