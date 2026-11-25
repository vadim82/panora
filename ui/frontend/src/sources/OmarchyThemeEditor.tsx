import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { bridge, usePanora } from '../client';
import { Button, FormActions, Notice } from '../components/ui';
import type { ConfiguredOmarchyTheme, OmarchyThemePreview } from '../types';
import type { EditorCallbacks } from './editorCallbacks';
import {
  SourceRules,
  sourceRuleDraft,
  sourceRuleError,
  sourceRulePatch,
  sourceRuleSummary,
  sourceRules,
} from './SourceRules';
import type { SourceRuleDraft } from './SourceRules';
import { useSourceDraft } from './useSourceDraft';

const thumbnailsPerPage = 12;

function useThemePreview(sourceID: string, page: number, limit: number) {
  const ui = usePanora();
  const [preview, setPreview] = useState<OmarchyThemePreview>();
  const [error, setError] = useState('');
  const connected = ui.connected && ui.page === 'providers';
  const instance = ui.snapshot?.status?.instance_id;
  const name = ui.snapshot?.status?.omarchy_theme_name;
  const revision = ui.configuration?.revision;
  useEffect(() => {
    if (!connected) return;
    let alive = true;
    let timer: number;
    let observedRevision: string | undefined;
    let currentPage = page;
    async function refresh(full: boolean) {
      try {
        let result = await bridge('OmarchyThemePreview', [
          full ? currentPage : 1,
          full ? limit : 0,
        ]);
        if (!alive) return;
        const next = result.theme;
        if (!full && next?.revision && next.revision !== observedRevision) {
          result = await bridge('OmarchyThemePreview', [currentPage, limit]);
        }
        if (!alive) return;
        if (full || result.theme?.revision !== observedRevision || result.error) {
          currentPage = result.theme?.page || 1;
          observedRevision = result.theme?.revision;
          setPreview(result.theme);
          setError(result.error || '');
        }
      } catch {
        if (alive) setError('Could not load the active theme preview.');
      } finally {
        if (alive) timer = window.setTimeout(() => void refresh(false), 2200);
      }
    }
    void refresh(true);
    return () => {
      alive = false;
      window.clearTimeout(timer);
    };
  }, [connected, instance, limit, name, page, revision, sourceID]);
  return { preview: name && preview?.theme !== name ? undefined : preview, error };
}

function ThemeGallery({
  preview,
}: {
  preview: ReturnType<typeof useThemePreview> & {
    showAll: boolean;
    setShowAll(value: boolean): void;
    setPage(value: number): void;
  };
}) {
  const { preview: data, error, showAll, setShowAll, setPage } = preview;
  const total = data?.total ?? 0;
  const page = data?.page || 1;
  const first = (page - 1) * thumbnailsPerPage + 1;
  const last = Math.min(page * thumbnailsPerPage, total);
  return (
    <>
      <div className="theme-editor-overview">
        <div>
          <p className="subtext">Active theme</p>
          <h3 id="theme-editor-title" tabIndex={-1}>
            {data?.theme || 'Omarchy theme'}
          </h3>
        </div>
        {data?.state === 'ready' && (
          <p className="subtext">
            {total} {total === 1 ? 'still' : 'stills'} · {data.eligible ?? '—'} eligible
          </p>
        )}
      </div>
      {error && <Notice>{error}</Notice>}
      {data?.warning && <Notice>{data.warning}</Notice>}
      {(!data || data.state === 'loading') && !error && (
        <p role="status" className="subtext">
          Checking active theme…
        </p>
      )}
      {Boolean(data?.palette.length) && (
        <section aria-labelledby="theme-palette-title">
          <h4 id="theme-palette-title">Theme palette</h4>
          <ul className="theme-swatches">
            {data?.palette.map(({ label, color }) => (
              <li key={label}>
                <span title={color} style={{ backgroundColor: color }} />
                {label}
              </li>
            ))}
          </ul>
        </section>
      )}
      {total > 0 && (
        <section aria-labelledby="theme-wallpapers-title">
          <div className="theme-gallery-controls">
            <h4 id="theme-wallpapers-title">Wallpapers</h4>
            {total > 4 && (
              <Button
                variant="secondary"
                className="outline"
                aria-expanded={showAll}
                aria-controls="theme-wallpapers"
                onClick={() => {
                  setPage(1);
                  setShowAll(!showAll);
                }}
              >
                {showAll ? 'Show preview' : `View all ${total}`}
              </Button>
            )}
          </div>
          <ul id="theme-wallpapers" className="theme-thumbnails">
            {data?.stills.slice(0, showAll ? thumbnailsPerPage : 4).map((still) => (
              <li key={still.id}>
                <img
                  src={`data:image/jpeg;base64,${still.jpeg}`}
                  alt={still.name}
                  title={`${still.name} · ${still.width} × ${still.height}`}
                />
              </li>
            ))}
          </ul>
          {showAll && total > thumbnailsPerPage && (
            <div className="theme-gallery-controls">
              <p className="subtext" role="status">
                Showing {first}–{last} of {total}
              </p>
              <FormActions>
                <Button
                  variant="secondary"
                  aria-disabled={page === 1}
                  onClick={() => {
                    if (page > 1) setPage(page - 1);
                  }}
                >
                  Previous
                </Button>
                <Button
                  variant="secondary"
                  aria-disabled={last === total}
                  onClick={() => {
                    if (last < total) setPage(page + 1);
                  }}
                >
                  Next
                </Button>
              </FormActions>
            </div>
          )}
        </section>
      )}
    </>
  );
}

export function OmarchyThemeEditor({
  theme,
  mutable,
  onDirty,
  onSaved,
  onCancel,
}: EditorCallbacks & { theme: ConfiguredOmarchyTheme; mutable: boolean }) {
  const ui = usePanora();
  const config = ui.configuration!;
  const live = config.omarchy_theme?.id === theme.id ? config.omarchy_theme : undefined;
  const [editor, setEditor] = useSourceDraft(
    config.revision,
    theme.id,
    () => sourceRuleDraft(theme, config.rules),
    live,
    config.rules,
    sourceRuleDraft,
  );
  const [showAll, setShowAll] = useState(false);
  const [page, setPage] = useState(1);
  const [working, setWorking] = useState(false);
  const [notice, setNotice] = useState('');
  const [validation, setValidation] = useState(false);
  const inFlight = useRef(false);
  const { draft, baseline, revision } = editor;
  const patch = sourceRulePatch(draft, baseline, true);
  const dirty = !!(patch.inherit_rules || patch.image_rules);
  const data = useThemePreview(theme.id, showAll ? page : 1, showAll ? thumbnailsPerPage : 4);
  useLayoutEffect(() => {
    onDirty(dirty);
  }, [dirty, onDirty]);
  function change(rules: Partial<SourceRuleDraft>) {
    setEditor((previous) => ({ ...previous, draft: { ...previous.draft, ...rules } }));
    setNotice('');
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!mutable || !live || !dirty || inFlight.current) return;
    const origin = document.activeElement;
    const error = sourceRuleError(draft, config.rules);
    setValidation(true);
    if (error) {
      const target = document.getElementById(
        `theme-${draft.rule_mode === 'inherit' ? 'rule-mode' : error.field}`,
      );
      target?.focus();
      setNotice(error.message);
      return;
    }
    inFlight.current = true;
    setWorking(true);
    const result = await ui.request(
      'EditOmarchyTheme',
      ['update', { id: live.id, revision, ...patch }],
      { mutation: true },
    );
    if (result.configuration?.omarchy_theme) {
      await ui.refresh();
      onSaved(live.id, 'Omarchy theme source saved.', origin);
    } else {
      setNotice(result.error || 'The source could not be saved.');
    }
    inFlight.current = false;
    setWorking(false);
  }
  const summary = sourceRuleSummary(
    { image_rules: draft.rule_mode === 'custom' ? sourceRules(draft) : null },
    config.rules,
  );
  return (
    <form className="source-editor theme-editor" onSubmit={(event) => void save(event)} noValidate>
      <ThemeGallery preview={{ ...data, showAll, setShowAll, setPage }} />
      <details className="theme-rules">
        <summary>
          <strong>Image rules</strong> <span className="source-filter-summary">{summary}</span>
        </summary>
        <SourceRules
          prefix="theme"
          provider="omarchy"
          scopeRevision={data.preview?.revision}
          inputID={theme.id}
          revision={revision}
          enrolled={Boolean(live?.enabled)}
          draft={draft}
          inherited={config.rules}
          disabled={!mutable || working || !live}
          validate={validation}
          onChange={change}
        />
      </details>
      {!live && <Notice>This Omarchy theme source was removed from saved settings.</Notice>}
      {notice && <Notice>{notice}</Notice>}
      <FormActions>
        <Button variant="secondary" disabled={working} onClick={onCancel}>
          Cancel
        </Button>
        <Button
          id="save-omarchy-theme"
          type="submit"
          variant="primary"
          disabled={!mutable || !live || !dirty}
          pending={working}
        >
          Save
        </Button>
      </FormActions>
    </form>
  );
}
