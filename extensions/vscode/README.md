<p align="center">
  English | <a href="README.zh-CN.md">简体中文</a> | <a href="README.ja-JP.md">日本語</a> | <a href="README.ko-KR.md">한국어</a> | <a href="README.ru-RU.md">Русский</a>
</p>

# Open Code Review (VS Code Extension)

A VS Code code-review extension built on the [`open-code-review`](https://www.npmjs.com/package/@alibaba-group/open-code-review) (`ocr`) CLI. It recreates the prototype experience with a Preact WebView and brings AI code review into the editor: start reviews from the sidebar, stream logs live, and apply / dismiss / flag-as-false-positive each comment inline — kept in sync with the sidebar both ways.

---

## Features

- **Three review modes**: workspace changes, branch comparison (`--from` / `--to`), and a single commit (`--commit`).
- **Files-to-review preview**: lists changed files from the current Git state; click a file to view its changes in the native diff view.
- **Custom review prompt**: optionally append a `--background` hint for the current review.
- **Streaming logs**: tail the CLI output live during review, cancel anytime.
- **Results + two-way sync**: on completion, comment cards appear in the sidebar while CommentThreads render in the editor; apply / dismiss / false-positive actions stay in sync on both sides.
- **Empty / cancelled / failed states**: dedicated views for no issues, user cancellation, and CLI failure (failures are retryable and surface the real error returned by the CLI).
- **Configuration management**: view / edit the LLM provider config inside the extension (persisted via `ocr config set`).
- **Model switching / connectivity test**: switch models and test connectivity to the LLM from the status bar.
- **Verification labels**: sidebar cards and native comments show verification status and the supplied reason separately from the finding.

### Verification results

`Confirmed` identifies a supported, complete verification summary.
`Uncertain · verification complete` means that the available evidence does not settle the claim.
`Uncertain · verification incomplete` identifies an interrupted verification.
`Unverified · verification skipped` identifies an explicit CLI bypass with `--no-filter`.
Legacy findings and unsupported metadata show `Unverified`.

The verification summary shows complete, incomplete, and skipped candidate counts with supplied reasons.
These counts can include refuted candidates that do not appear in the published findings.
The summary stays separate from file coverage, published-finding counts, and the review's terminal state.
Missing completion data does not imply complete verification.

Verification does not change finding content, reasoning, suggestions, or user action status.
Equal findings retain separate action states and reasons.
Long reasons remain available through the card's scroll area.
Reasons appear as plain text, including text that resembles HTML or editor command links.
Findings with no native location remain available in the sidebar.


---

## Prerequisites

1. Install the `ocr` CLI globally:

   ```bash
   npm i -g @alibaba-group/open-code-review
   ```

2. Configure a working LLM (endpoint, API key, model). Configure it via the CLI directly, or in the extension's config view:

   ```bash
   ocr config set llm.url https://api.anthropic.com/v1/messages
   ocr config set llm.auth_token sk-...
   ocr config set llm.model claude-opus-4-6
   ocr config set llm.use_anthropic true
   ```

   The config is written to `~/.opencodereview/config.json`.

### Native CLI providers

Select **Claude Code CLI** or **Codex CLI** in the official provider form.
Install and authenticate `claude` or `codex` through its native configuration.
The native providers require a model but no OCR API key or HTTP URL.
The form accepts custom model names and retains saved model choices.
An HTTP protocol override restores the API-key field.
Use `ocr config set providers.<name>.url <url>` for a saved URL override.
Codex requires an exact model from its bundled catalog and does not restrict the CLI version.
Its suggested model is `gpt-6.1-sol`. Claude's suggested model is `sonnet`.
OCR controls review scope, prompts, rules, and tool execution.
See [CLI compatibility and limits](../../internal/llm/CLI_COMPLETIONS.md) for native safety controls and the Codex configuration race limit.

The integration smoke uses the actual OCR binary and both native providers:

```bash
OCR_INTEGRATION_BINARY=/absolute/path/to/ocr corepack yarn test --runInBand CliService.auth.test.ts
```

This smoke checks successful and failed drafts, saved configuration isolation, and temporary-home cleanup.
The runtime proof uses POSIX. It does not establish Windows runtime behavior.

### Draft connection checks

Draft checks use a temporary OCR home and leave the saved configuration unchanged.
The child process retains `CLAUDE_CONFIG_DIR` and `CODEX_HOME` from the effective shell environment.
Without an override, it uses `.claude` and `.codex` under the original home.
The extension does not read or copy CLI credentials.
The extension removes the temporary OCR home after each check.

To test the launch path with real Claude authentication, run:

```bash
OCR_IDE_AUTH_SMOKE=1 yarn test --runInBand CliService.auth.test.ts
```

This opt-in test requires Node.js and an authenticated `claude` CLI on a POSIX system.
It invokes Claude through the draft child environment, not through the OCR provider registry.
Executable fixture tests skip Windows because their launcher uses a POSIX shebang.
The mocked Windows test checks authentication roots and both home variables without a child process.

---

## Development

### Environment

- Node.js ≥ 18, with **Yarn** as the package manager (the repo ships a `yarn.lock`).
- VS Code ≥ 1.74.
- A globally available `ocr` CLI (see "Prerequisites" above) — the extension is essentially a GUI front-end for `ocr`.

### Start the dev environment

```bash
cd extensions/vscode
yarn install      # install dependencies
yarn watch        # watch-mode dev build (recommended: rebuilds out/ on change)
```

Then open the `extensions/vscode` folder in VS Code and press **F5** to launch the
Extension Development Host (debug config is provided in `.vscode/launch.json`). In the new
window, open a project with Git changes — you'll see the Open Code Review icon in the
activity bar and can start a review.

> After editing code: WebView changes require **reopening the sidebar** in the dev host window
> (or running `Developer: Reload Webviews`); Extension Host changes require **restarting the
> debug session** (the ⟳ button on the debug toolbar, or `Cmd+R` in the host window).

### Scripts

```bash
yarn compile      # one-off dev build (webpack development)
yarn watch        # watch-mode dev build
yarn build        # production build (webpack production; runs automatically before packaging)
yarn test         # run Jest unit tests
yarn lint         # ESLint
yarn package      # produce a distributable .vsix package (see "Build a release package")

# Focused verification regressions
corepack yarn test --runInBand verificationResult.test.ts verificationComments.test.ts
# Run from extensions/frontend for shared metadata and occurrence-state checks
npm test -- --runInBand verification.test.ts
```

### Debugging notes

- **Two-way messaging**: the WebView and Extension Host communicate via `postMessage`; message
  types live in `extensions/frontend/src/shared/messages.ts`. Both sides route through `dispatch`
  / `handle` — start
  there when debugging.
- **CLI invocation**: all `ocr` sub-commands run via `child_process.spawn` in
  `src/extension/services/CliService.ts`. `runRaw` rejects on a non-zero CLI exit code and includes
  the `Error:` text from stderr, which helps diagnose "review failed / connection failed".
- **Config read/write**: `ConfigService` reads `~/.opencodereview/config.json` and delegates writes to
  `ocr config set`. WebView fields are camelCase (e.g. `useAnthropic`) while the disk/CLI side is
  snake_case (e.g. `use_anthropic`); the conversion lives in `src/extension/services/configParse.ts`.

---

## Build

### Compile artifacts only

```bash
yarn build        # production build (webpack production)
```

Artifacts: `out/extension.js` (Extension Host) + `out/webview.js` (WebView SPA).

### Build a release package (.vsix)

```bash
yarn package      # = vsce package --no-yarn
```

This command:

1. Triggers `vscode:prepublish` → runs the `yarn build` production build;
2. Excludes source, tests, and dev files per `.vscodeignore`;
3. Produces `open-code-review-vscode-<version>.vsix` in the current directory.

> The packaging tool is `@vscode/vsce`, installed as a devDependency — no global install or network
> download needed. `--no-yarn` skips vsce's default npm dependency-tree check (this project uses Yarn).

The release package contains only the runtime essentials: `package.json`, `README.md`,
`resources/icon.svg`, `out/extension.js`, `out/webview.js`.

### Install / verify locally

```bash
code --install-extension open-code-review-vscode-<version>.vsix
```

Or in VS Code: Extensions panel → top-right `⋯` → **Install from VSIX…** → pick the generated `.vsix` file.

> To publish to the Marketplace, use `vsce publish` instead (requires a publisher account and PAT);
> for everyday distribution the `.vsix` above is enough.

---

## Architecture

It uses a **Monolithic WebView + Thin Extension Host** design:

- The **WebView** is a separately built Preact SPA, built from the shared frontend at
  `extensions/frontend/` (also used by the IntelliJ IDEA plugin).
- The **Extension Host** layer is thin, handling only CLI invocation, the file system, Git operations,
  and editor comments.
- The two communicate via `postMessage`, with shared TypeScript types in
  `extensions/frontend/src/shared/` for type safety.

```
src/
└── extension/          Extension Host (Node.js): services / providers / commands
```

The WebView and shared types are built from the shared frontend at `extensions/frontend/` (also
used by the IntelliJ IDEA plugin).

---

## License

Apache-2.0
