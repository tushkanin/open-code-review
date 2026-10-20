// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

import { parseCliResult } from '../cliParse';
import { verificationDisplay } from '@shared/verification';
import { t } from '@shared/i18n';

const claim = { path: 'handler.go', content: 'Equal claim', start_line: 4, end_line: 4,
  existing_code: 'return req.Value', suggestion_code: 'return 0', thinking: 'A caller can pass nil.' };

it('uses only published occurrences and does not borrow decisions from the audit report', () => {
  const reason = { code: 'caller', explanation: 'The caller is unavailable.' };
  const result = parseCliResult(JSON.stringify({
    status: 'partial', comments: [
      { ...claim, candidate_id: 'one', verification: { verdict: 'uncertain', state: 'complete', reason } },
      { ...claim, candidate_id: 'two', verification: { verdict: 'confirmed', state: 'future', reason } },
    ],
    verification_report: { records: [{ candidate: { id: 'refuted', finding: claim }, verdict: 'refuted' }] },
    verification_completion: { complete: 2, incomplete: 1, skipped: 0, reasons: [reason] },
  }));
  expect(result.comments.map((comment) => comment.candidateId)).toEqual(['one', 'two']);
  const labels = result.comments.map((comment) => verificationDisplay(comment.verification, (key) => t('en', key)));
  expect(labels).toEqual([
    { label: t('en', 'verification.uncertain'), reason: reason.explanation },
    { label: t('en', 'verification.unverified'), reason: t('en', 'verification.unavailable') },
  ]);
  expect(result.comments.map((comment) => [comment.content, comment.existingCode, comment.suggestionCode, comment.thinking]))
    .toEqual(Array(2).fill([claim.content, claim.existing_code, claim.suggestion_code, claim.thinking]));
  expect(result.verificationCompletion).toEqual({ complete: 2, incomplete: 1, skipped: 0, reasons: [reason] });
});

it.each([undefined, null, 'unsupported', { verdict: 'confirmed', state: 'complete', reason: null }])(
  'retains a usable claim without optional verification: %j', (verification) => {
    const result = parseCliResult(JSON.stringify({ status: 'success', comments: [{ ...claim, verification }] }));
    expect(result.comments.map((comment) => comment.content)).toEqual([claim.content]);
    expect(verificationDisplay(result.comments[0].verification, (key) => t('en', key)).label)
      .toBe(t('en', 'verification.unverified'));
    expect(result.verificationCompletion).toBeUndefined();
  },
);
