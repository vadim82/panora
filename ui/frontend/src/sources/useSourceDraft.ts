import { useEffect, useState } from 'react';
import type { ImageRules } from '../types';
import { preserveEdits } from './drafts';
import { preserveSourceRuleEdits } from './SourceRules';
import type { SourceRuleDraft } from './SourceRules';

// The editor is keyed by its selected source. Rebase only when settings are reviewed.
export function useSourceDraft<Source, Draft extends SourceRuleDraft>(
  revision: string,
  id: string | undefined,
  initial: () => Draft,
  saved: Source | undefined,
  inherited: ImageRules,
  values: (source: Source, inherited: ImageRules) => Draft,
) {
  const [editor, setEditor] = useState(() => {
    const baseline = initial();
    return { revision, baseline, draft: baseline, missing: false };
  });
  useEffect(() => {
    setEditor((current) => {
      if (current.revision === revision) return current;
      if (!id || !saved) return { ...current, revision, missing: Boolean(id) };
      const baseline = values(saved, inherited);
      return {
        revision,
        baseline,
        draft: {
          ...preserveEdits(current.draft, current.baseline, baseline),
          ...preserveSourceRuleEdits(current.draft, current.baseline, baseline),
        },
        missing: false,
      };
    });
  }, [id, inherited, revision, saved, values]);
  return [editor, setEditor] as const;
}
