import { useEffect, useEffectEvent, useRef, useState } from 'react';
import { bridge, usePanora } from '../client';
import type { ClientContext } from '../client';
import type { Bridge, ImageRules, Result } from '../types';

type SearchCount = Awaited<ReturnType<Bridge['CountSearchCandidates']>>;
export type SearchDraftStatus = { id?: string; revision: string } & (
  | { reason: 'unenrolled' | 'disabled' | 'criteria_changed' | 'invalid_rules' }
  | { rules: ImageRules }
);

function currentSearchRead(
  getState: ClientContext['getState'],
  revision: string,
  canceled: () => boolean,
) {
  const instance = getState().snapshot?.status?.instance_id;
  return () => {
    const state = getState();
    return (
      !canceled() &&
      state.connected &&
      !state.blocked &&
      state.page === 'providers' &&
      state.snapshot?.status?.instance_id === instance &&
      state.configuration?.revision === revision &&
      state.liveConfiguration?.revision === revision
    );
  };
}

type SearchCheck = { id: string; lastSuccess?: string };

export function useSearchCounts(
  checks: SearchCheck[],
  revision: string | undefined,
  connection: string | null,
  refresh: number,
) {
  const { getState } = usePanora();
  const scope = connection && revision ? JSON.stringify([connection, revision, refresh]) : null;
  const checkKey = JSON.stringify(checks);
  const lastScope = useRef<string | null>(null);
  const [reading, setReading] = useState<{
    scope: string;
    results: ReadonlyMap<string, { lastSuccess?: string; result: SearchCount }>;
  }>();
  const read = useEffectEvent(async (scope: string, reload: boolean, canceled: () => boolean) => {
    if (!revision) return;
    const current = currentSearchRead(getState, revision, canceled);
    // A status disclosure may contain many searches. Keep catalog reads serial,
    // and revisit only searches whose latest successful check changed.
    const previousResults = !reload && reading?.scope === scope ? reading.results : undefined;
    for (const { id, lastSuccess } of checks) {
      if (!current()) return;
      const previous = previousResults?.get(id);
      if (previous && previous.lastSuccess === lastSuccess) continue;
      let result: SearchCount;
      try {
        result = await bridge('CountSearchCandidates', [id, revision]);
      } catch {
        result = {};
      }
      if (!current()) return;
      setReading((previous) => ({
        scope,
        results: new Map(previous?.scope === scope ? previous.results : []).set(id, {
          lastSuccess,
          result,
        }),
      }));
    }
  });
  useEffect(() => {
    const reload = lastScope.current !== scope;
    lastScope.current = scope;
    if (!scope) return;
    let canceled = false;
    void read(scope, reload, () => canceled);
    return () => {
      canceled = true;
    };
  }, [scope, checkKey]);
  return reading && reading.scope === scope ? reading.results : undefined;
}

export function SavedSearchCount({
  result,
  available,
}: {
  result?: SearchCount;
  available: boolean;
}) {
  const count = result?.counts;
  return (
    <p className="source-counts">
      {!available
        ? 'Catalog totals unavailable. Reconnect or reload settings to check the saved search.'
        : count
          ? `${count.discovered} discovered in the catalog · ${count.eligible == null ? 'Eligible count unavailable because the screen shape could not be resolved' : `${count.eligible} eligible under saved rules and exclusions`}`
          : result
            ? result.error || 'The saved catalog count is unavailable. Refresh status to try again.'
            : 'Counting saved candidates…'}
    </p>
  );
}

const draftReasons = {
  unenrolled: 'Save and enable this search to estimate matches from its catalog.',
  disabled: 'Enable this search to estimate matches from its catalog.',
  criteria_changed: 'Save the changed search criteria before estimating matches.',
  invalid_rules: 'Correct the image rules to estimate matches.',
};
const staleDraft = 'Reconnect or reload settings to estimate matches.';

export function DraftSearchMatches({
  draft,
  connection,
  revision,
}: {
  draft: SearchDraftStatus;
  connection: string | null;
  revision: string | undefined;
}) {
  const message =
    !connection || draft.revision !== revision
      ? staleDraft
      : 'reason' in draft
        ? draftReasons[draft.reason]
        : null;
  return (
    <p className="source-counts" role="status">
      <strong>With your unsaved rules</strong>
      {' · '}
      {message ||
        ('rules' in draft && draft.id && (
          <DraftCount
            key={JSON.stringify([connection, draft.id, draft.revision, draft.rules])}
            id={draft.id}
            revision={draft.revision}
            rules={draft.rules}
          />
        ))}
    </p>
  );
}

function DraftCount({ id, revision, rules }: { id: string; revision: string; rules: ImageRules }) {
  const { getState } = usePanora();
  const [result, setResult] = useState<Result>();
  // The component key ties the timer and its result to one exact draft.
  const estimate = useEffectEvent(async (canceled: () => boolean) => {
    const current = currentSearchRead(getState, revision, canceled);
    if (!current()) return;
    let response: Result;
    try {
      response = await bridge('RulePreview', [
        { provider_id: 'wallhaven', input_id: id, revision, rules },
      ]);
    } catch {
      response = { code: 'request_failed' };
    }
    if (current()) setResult(response);
  });
  useEffect(() => {
    let canceled = false;
    const timer = window.setTimeout(() => void estimate(() => canceled), 350);
    return () => {
      canceled = true;
      window.clearTimeout(timer);
    };
  }, []);
  if (!result) return 'Checking catalog images…';
  const preview = result.preview;
  if (!preview) {
    return ['config_conflict', 'request_failed', 'service_unavailable'].includes(result.code || '')
      ? staleDraft
      : result.error || 'The estimate is unavailable.';
  }
  if (preview.reason === 'source_not_enabled_or_enrolled') return draftReasons.unenrolled;
  if (preview.rule_resolution && preview.rule_resolution.state !== 'resolved') {
    return 'The estimate is unavailable because the screen shape could not be resolved.';
  }
  return (
    <>
      {preview.matched} matching · {preview.rejected} rejected · {preview.unknown} unknown ·{' '}
      {preview.inspected} inspected
      {!preview.complete && ' · Partial catalog check'}
      {!preview.exclusions_applied && ' · Exclusions not applied to the estimate'}
    </>
  );
}
