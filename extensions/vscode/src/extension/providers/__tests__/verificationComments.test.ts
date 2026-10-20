// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

import * as vscode from 'vscode';
import { CommentProvider } from '../CommentProvider';
import { GitService } from '../../services/GitService';
import { ReviewMode, CommentSyncState } from '@shared/types';

jest.mock('vscode', () => ({
  ...jest.requireActual('../../../../__mocks__/vscode.js'),
  comments: {},
}));

it('retains each occurrence reason after independent discard and false-positive actions', async () => {
  const threads: Array<{ comments: vscode.Comment[]; dispose: () => void }> = [];
  Object.defineProperty(vscode.comments, 'createCommentController', { value: () => ({
    createCommentThread: (_uri: unknown, _range: unknown, comments: vscode.Comment[]) => {
      const thread = { comments, dispose: jest.fn() };
      threads.push(thread);
      return thread;
    }, dispose: jest.fn(),
  }), configurable: true });
  Object.defineProperty(vscode.window, 'showTextDocument', {
    value: jest.fn().mockResolvedValue({}), configurable: true,
  });
  const git = {
    prepareReviewFileStatus: async () => {}, getReviewFileStatus: async () => 'modified',
    readWorkspaceFile: async (path: string) => path === 'missing.go' ? null : 'return req.Value',
  } as unknown as GitService;
  const provider = new CommentProvider(vscode.Uri.file('/extension'), git);
  let sync: CommentSyncState[] = [];
  provider.onSync((states) => { sync = states; });
  await provider.show(['one', 'two', 'sidebar'].map((candidateId) => ({
    path: candidateId === 'sidebar' ? 'missing.go' : 'handler.go',
    content: 'Equal claim', startLine: 1, endLine: 1, candidateId,
    verification: { verdict: 'uncertain', state: 'complete', reason: { code: 'caller', explanation: `Reason ${candidateId}` } },
  })), { mode: ReviewMode.Workspace });
  provider.discard(0);
  provider.falsePositive(1);
  expect(sync).toEqual([
    { index: 0, status: 'discarded', jumpable: true },
    { index: 1, status: 'falsePositive', jumpable: true },
    { index: 2, status: 'pending', jumpable: false },
  ]);
  const first = threads[0].comments[0].body as vscode.MarkdownString;
  const second = threads[1].comments[0].body as vscode.MarkdownString;
  expect(first.value).toContain('Reason one');
  expect(first.value).not.toContain('Reason two');
  expect(second.value).toContain('Reason two');
  expect(second.value).not.toContain('Reason one');
  expect(first.value).toContain('Equal claim');
  provider.dispose();
});
