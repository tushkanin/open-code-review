// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

import { VerificationCompletion, VerificationReason, VerificationSummary } from './types';

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isReason(value: unknown): value is VerificationReason {
  return isObject(value) && typeof value.code === 'string' && !!value.code.trim()
    && typeof value.explanation === 'string' && !!value.explanation.trim();
}

export function parseVerification(value: unknown): VerificationSummary | undefined {
  if (!isObject(value) || !isReason(value.reason)) return undefined;
  if (!['confirmed', 'refuted', 'uncertain'].includes(value.verdict as string)
    || !['complete', 'incomplete', 'skipped'].includes(value.state as string)) return undefined;
  if (value.verdict !== 'uncertain' && value.state !== 'complete') return undefined;
  return { verdict: value.verdict, state: value.state, reason: value.reason } as VerificationSummary;
}

export function parseVerificationCompletion(value: unknown): VerificationCompletion | undefined {
  if (!isObject(value) || ![value.complete, value.incomplete, value.skipped]
    .every((count) => Number.isSafeInteger(count) && (count as number) >= 0)
    || !Array.isArray(value.reasons) || !value.reasons.every(isReason)) return undefined;
  return {
    complete: value.complete as number,
    incomplete: value.incomplete as number,
    skipped: value.skipped as number,
    reasons: value.reasons,
  };
}

export function verificationDisplay(value: unknown, t: (key: string) => string): { label: string; reason: string } {
  const summary = parseVerification(value);
  if (!summary || summary.verdict === 'refuted') {
    return { label: t('verification.unverified'), reason: t('verification.unavailable') };
  }
  const key = summary.state === 'complete' ? summary.verdict : summary.state;
  return { label: t(`verification.${key}`), reason: summary.reason.explanation };
}

/** Trusted Markdown must not interpret metadata as links, HTML, or block syntax. */
export function escapeVerificationText(text: string): string {
  return text.replace(/[&<>\\`*_{}[\]()#+.!|~:@-]/g, (character) => `&#${character.charCodeAt(0)};`)
    .replace(/\r\n|\r|\n/g, ' ');
}
