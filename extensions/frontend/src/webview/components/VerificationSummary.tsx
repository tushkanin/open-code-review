// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

import { parseVerificationCompletion } from '../../shared/verification';
import { useT } from '../I18nProvider';

export function VerificationSummary({ completion }: { completion: unknown }) {
  const t = useT();
  const summary = parseVerificationCompletion(completion);
  return (
    <div class="verification-summary">
      <strong>{t('verification.summary')}</strong>
      {summary ? (
        <div>
          <div>{summary.complete} {t('verification.countComplete')} · {summary.incomplete} {t('verification.countIncomplete')} · {summary.skipped} {t('verification.countSkipped')}</div>
          {summary.reasons.map((reason, index) => <div key={index}>{reason.explanation}</div>)}
        </div>
      ) : <div>{t('verification.unavailable')}</div>}
    </div>
  );
}
