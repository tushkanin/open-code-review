// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'fs';
import * as os from 'os';
import { join } from 'path';
import { CliService } from '../CliService';
import { ConfigService } from '../ConfigService';
import * as shellEnv from '../shellEnv';

jest.mock('os', () => ({ ...jest.requireActual('os'), homedir: jest.fn() }));

const fixtureTest = process.platform !== 'win32' ? it : it.skip;
const liveTest = process.env.OCR_IDE_AUTH_SMOKE === '1' ? fixtureTest : it.skip;
const liveEnv = { ...shellEnv.getShellEnv() };

const fixture = `#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const cp = require('child_process');
if (process.argv.slice(2).join(' ') !== 'llm test') process.exit(2);
const home = process.env.HOME;
const draft = JSON.parse(fs.readFileSync(path.join(home, '.opencodereview/config.json'), 'utf8'));
if (process.env.OCR_AUTH_LIVE === '1') {
  const result = cp.spawnSync(process.env.OCR_AUTH_CLAUDE, [
    '-p', '--model', 'sonnet', '--output-format', 'json', '--tools', '',
    '--strict-mcp-config', '--safe-mode', '--permission-mode', 'dontAsk', '--no-session-persistence',
    'Reply with exactly OCR_IDE_AUTH_OK.',
  ], { encoding: 'utf8', timeout: 120000 });
  if (result.status !== 0) { process.stderr.write(result.stderr || String(result.error)); process.exit(3); }
  const response = JSON.parse(result.stdout);
  if (response.is_error || !response.result.includes('OCR_IDE_AUTH_OK')) process.exit(4);
}
fs.writeFileSync(process.env.OCR_AUTH_AUDIT, JSON.stringify({
  home, claude: process.env.CLAUDE_CONFIG_DIR, codex: process.env.CODEX_HOME,
  userprofile: process.env.USERPROFILE, draft,
  files: fs.readdirSync(home), configFiles: fs.readdirSync(path.join(home, '.opencodereview')),
}));
if (draft.providers.openai.model !== 'draft-model') process.exit(5);
if (process.env.OCR_AUTH_FAIL === '1') { process.stderr.write('Error: draft failure'); process.exit(6); }
`;

describe('draft authentication roots', () => {
  let dir: string;
  let savedHome: string;
  let audit: string;
  let env: NodeJS.ProcessEnv;
  let cli: CliService;
  let config: ConfigService;

  beforeEach(() => {
    dir = mkdtempSync(join(os.tmpdir(), 'ocr-auth-test-'));
    savedHome = join(dir, 'saved-home');
    mkdirSync(join(savedHome, '.opencodereview'), { recursive: true });
    writeFileSync(join(savedHome, '.opencodereview/config.json'), '{"provider":"openai","providers":{"openai":{"api_key":"saved-key","model":"saved-model"}}}');
    audit = join(dir, 'audit.json');
    env = { ...process.env, HOME: savedHome, OCR_AUTH_AUDIT: audit };
    delete env.CLAUDE_CONFIG_DIR;
    delete env.CODEX_HOME;
    delete env.OCR_AUTH_LIVE;
    delete env.OCR_AUTH_FAIL;
    const binary = join(dir, 'ocr');
    writeFileSync(binary, fixture, { mode: 0o700 });
    jest.spyOn(shellEnv, 'getShellEnv').mockImplementation(() => env);
    jest.spyOn(shellEnv, 'resolveBin').mockImplementation((name) => name);
    jest.mocked(os.homedir).mockReturnValue(savedHome);
    cli = new CliService(binary);
    config = new ConfigService(cli);
  });

  afterEach(() => {
    jest.restoreAllMocks();
    rmSync(dir, { recursive: true, force: true });
  });

  async function checkDraft(ok = true): Promise<{ home: string; claude: string; codex: string }> {
    const saved = readFileSync(join(savedHome, '.opencodereview/config.json'), 'utf8');
    const result = await config.testWithEntries([{ key: 'providers.openai.model', value: 'draft-model' }]);
    expect(result.ok).toBe(ok);
    const record = JSON.parse(readFileSync(audit, 'utf8'));
    expect(record.home).not.toBe(savedHome);
    expect(record.files).toEqual(['.opencodereview']);
    expect(record.configFiles).toEqual(['config.json']);
    expect(record.draft.providers.openai).toEqual({ api_key: 'saved-key', model: 'draft-model' });
    expect(existsSync(record.home)).toBe(false);
    expect(readFileSync(join(savedHome, '.opencodereview/config.json'), 'utf8')).toBe(saved);
    return record;
  }

  fixtureTest('derives both roots from the effective original home', async () => {
    const record = await checkDraft();
    expect(record.claude).toBe(join(savedHome, '.claude'));
    expect(record.codex).toBe(join(savedHome, '.codex'));
  });

  fixtureTest('preserves both explicit overrides', async () => {
    env.CLAUDE_CONFIG_DIR = join(dir, 'explicit-claude');
    env.CODEX_HOME = join(dir, 'explicit-codex');
    const record = await checkDraft();
    expect(record.claude).toBe(env.CLAUDE_CONFIG_DIR);
    expect(record.codex).toBe(env.CODEX_HOME);
  });

  fixtureTest('keeps cleanup and the saved API configuration on failure', async () => {
    env.OCR_AUTH_FAIL = '1';
    await checkDraft(false);
  });

  it('does not add overrides for a normal connection check', async () => {
    const run = jest.spyOn(cli, 'runRaw').mockResolvedValue('');
    expect(await cli.testConnection()).toEqual({ ok: true });
    expect(run).toHaveBeenCalledWith(['llm', 'test'], process.cwd(), expect.any(Function), undefined);
  });

  it('uses the original USERPROFILE when Windows replaces both home variables', async () => {
    const platform = Object.getOwnPropertyDescriptor(process, 'platform')!;
    Object.defineProperty(process, 'platform', { value: 'win32' });
    env.USERPROFILE = join(dir, 'windows-home');
    const run = jest.spyOn(cli, 'runRaw').mockResolvedValue('');
    try {
      expect(await cli.testConnection({ home: 'draft-home' })).toEqual({ ok: true });
      expect(run).toHaveBeenCalledWith(['llm', 'test'], process.cwd(), expect.any(Function), {
        HOME: 'draft-home', USERPROFILE: 'draft-home',
        CLAUDE_CONFIG_DIR: join(env.USERPROFILE, '.claude'),
        CODEX_HOME: join(env.USERPROFILE, '.codex'),
      });
    } finally {
      Object.defineProperty(process, 'platform', platform);
    }
  });

  liveTest('uses real CLI authentication through the changed draft launch path', async () => {
    const original = liveEnv;
    env = { ...original, OCR_AUTH_AUDIT: audit, OCR_AUTH_LIVE: '1',
      OCR_AUTH_CLAUDE: process.env.OCR_AUTH_CLAUDE || 'claude' };
    const originalSavedPath = join(original.HOME || savedHome, '.opencodereview/config.json');
    const originalSaved = existsSync(originalSavedPath) ? readFileSync(originalSavedPath, 'utf8') : undefined;
    const record = await checkDraft();
    expect(record.claude).toBe(original.CLAUDE_CONFIG_DIR ?? join(original.HOME || os.homedir(), '.claude'));
    expect(record.codex).toBe(original.CODEX_HOME ?? join(original.HOME || os.homedir(), '.codex'));
    expect(existsSync(originalSavedPath) ? readFileSync(originalSavedPath, 'utf8') : undefined).toBe(originalSaved);
  }, 150000);
});
