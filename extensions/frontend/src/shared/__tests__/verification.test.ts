// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

import { t } from '../i18n';
import { escapeVerificationText, parseVerificationCompletion, verificationDisplay } from '../verification';
import { initialState, reducer } from '../../webview/store';
import { CliResult } from '../types';

const translate = (key: string) => t('en', key);
const reason = { code: 'caller_unavailable', explanation: 'The remote caller is unavailable.' };

describe('verification trust and uncertainty', () => {
  it.each([
    ['confirmed', 'complete', 'verification.confirmed'],
    ['uncertain', 'complete', 'verification.uncertain'],
    ['uncertain', 'incomplete', 'verification.incomplete'],
    ['uncertain', 'skipped', 'verification.skipped'],
  ])('distinguishes %s with %s execution', (verdict, state, label) => {
    expect(verificationDisplay({ verdict, state, reason }, translate)).toEqual({
      label: translate(label), reason: reason.explanation,
    });
  });

  it.each([
    undefined, null, {}, { verdict: 'confirmed', state: 'complete' },
    { verdict: 'confirmed', state: 'future', reason },
    { verdict: 'future', state: 'complete', reason },
    { verdict: 'confirmed', state: 'incomplete', reason },
    { verdict: 'confirmed', state: 'skipped', reason },
    { verdict: 'confirmed', state: 'complete', reason: { code: 'ok', explanation: '' } },
  ])('does not establish trust from unsupported metadata: %j', (metadata) => {
    expect(verificationDisplay(metadata, translate)).toEqual({
      label: translate('verification.unverified'), reason: translate('verification.unavailable'),
    });
  });

  it.each([undefined, null, {}, { complete: -1, incomplete: 0, skipped: 0, reasons: [] },
    { complete: 1, incomplete: 0, skipped: 0 },
    { complete: 1, incomplete: 0.5, skipped: 0, reasons: [] },
    { complete: 1, incomplete: 0, skipped: 0, reasons: [null] }])(
    'does not claim completion from invalid counts: %j', (metadata) => {
      expect(parseVerificationCompletion(metadata)).toBeUndefined();
    },
  );

  it('keeps duplicate occurrence actions independent from verification completion and terminal state', () => {
    const result: CliResult = {
      status: 'partial', warnings: [],
      comments: ['first', 'second'].map((candidateId) => ({
        path: 'a.ts', content: 'Equal claim', startLine: 1, endLine: 1, candidateId,
        verification: { verdict: 'uncertain', state: 'complete', reason: { code: 'missing', explanation: candidateId } },
      })),
      verificationCompletion: { complete: 3, incomplete: 1, skipped: 0, reasons: [reason] },
    };
    let state = reducer(initialState, { type: 'reviewDone', result });
    state = reducer(state, { type: 'stateChange', state: 'done' });
    state = reducer(state, { type: 'commentSync', comments: [
      { index: 0, status: 'discarded', jumpable: false }, { index: 1, status: 'pending', jumpable: true },
    ] });
    expect(state.commentStatus).toEqual({ 0: 'discarded', 1: 'pending' });
    expect(state.commentJumpable).toEqual({ 0: false, 1: true });
    expect(state.session.result?.comments.map((comment) => verificationDisplay(comment.verification, translate).reason))
      .toEqual(['first', 'second']);
    expect(state.session.result?.status).toBe('partial');
    expect(parseVerificationCompletion(state.session.result?.verificationCompletion)).toEqual({
      complete: 3, incomplete: 1, skipped: 0, reasons: [reason],
    });
    expect(state.view).toBe('done');
  });
});

it('keeps HTML, Markdown links, and bare URLs inert at the trusted Markdown boundary', () => {
  const reason = '<script>run()</script> [execute](command:evil) https://example.invalid caller@example.invalid';
  const escaped = escapeVerificationText(reason);
  expect(escaped).not.toMatch(/<script|\\?\[execute\\?\]\(|command:|https:|@/);
  expect(escaped.replace(/&#(\d+);/g, (_, code: string) => String.fromCharCode(Number(code)))).toBe(reason);
});

it('preserves native location fallback when mount sync precedes result delivery', () => {
  const mounted = reducer(initialState, { type: 'commentSync', comments: [
    { index: 0, status: 'pending', jumpable: false },
  ] });
  const state = reducer(mounted, { type: 'reviewDone', result: {
    status: 'success', warnings: [],
    comments: [{ path: 'missing.ts', content: 'Claim', startLine: 0, endLine: 0 }],
  } });
  expect(state.commentJumpable[0]).toBe(false);
  expect(state.session.result?.comments[0].content).toBe('Claim');
});
