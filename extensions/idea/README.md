<p align="center">
  English | <a href="README.zh-CN.md">简体中文</a>
</p>

# Open Code Review (IntelliJ IDEA Plugin)

An IntelliJ IDEA plugin built on the [`open-code-review`](https://www.npmjs.com/package/@alibaba-group/open-code-review) (`ocr`) CLI. It hosts the VS Code extension's Preact webview inside a JCEF browser and brings AI code review into the editor: start reviews from the tool window, stream logs live, and apply / dismiss / flag-as-false-positive each comment inline — kept in sync with the tool window both ways.

---

## Features

- **Three review modes**: workspace changes, branch comparison (`--from` / `--to`), and a single commit (`--commit`).
- **Files-to-review preview**: lists changed files from the current Git state; click a file to view its changes in the native diff view.
- **Custom review prompt**: optionally append a `--background` hint for the current review.
- **Streaming logs**: tail the CLI output live during review, cancel anytime.
- **Results + two-way sync**: on completion, comment cards appear in the tool window while inline comment panels render in the editor; apply / dismiss / false-positive actions stay in sync on both sides.
- **Empty / cancelled / failed states**: dedicated views for no issues, user cancellation, and CLI failure (failures are retryable and surface the real error returned by the CLI).
- **Configuration management**: view / edit the LLM provider config inside the plugin (persisted via `ocr config set`).
- **Model switching / connectivity test**: switch models and test connectivity to the LLM from the config panel.

---

## Prerequisites

1. Install the `ocr` CLI globally:

   ```bash
   npm i -g @alibaba-group/open-code-review
   ```

2. Configure a working LLM (endpoint, API key, model). Configure it via the CLI directly, or in the plugin's config panel:

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
Codex supports release `0.162.1` and requires an exact model from its bundled catalog.
Its suggested model is `gpt-6.1-sol`. Claude's suggested model is `sonnet`.
OCR controls review scope, prompts, rules, and tool execution.
See [CLI compatibility and limits](../../internal/llm/CLI_COMPLETIONS.md) for native safety controls and the Codex configuration race limit.

The integration smoke uses the actual OCR binary and both native providers:

```bash
OCR_INTEGRATION_BINARY=/absolute/path/to/ocr \
  mise exec java@temurin-21 -- ./gradlew test -PskipFrontend=true --tests '*CliServiceAuthTest'
```

Set the test-only `OCR_INTEGRATION_PATH` when the shell selects an unsupported Codex release.
The directory with the supported CLI must precede other CLI directories on that path.
This smoke checks successful and failed drafts, saved configuration isolation, and temporary-home cleanup.
The runtime proof uses POSIX. It does not establish Windows runtime behavior.

### Draft connection checks

Draft checks use a temporary OCR home and leave the saved configuration unchanged.
The child process retains `CLAUDE_CONFIG_DIR` and `CODEX_HOME` from the effective shell environment.
Without an override, it uses `.claude` and `.codex` under the original home.
The plugin does not read or copy CLI credentials.
The plugin removes the temporary OCR home after each check.

To test the launch path with real Claude authentication, run:

```bash
OCR_IDE_AUTH_SMOKE=1 ./gradlew test -PskipFrontend=true --tests '*CliServiceAuthTest'
```

This opt-in test requires Node.js and an authenticated `claude` CLI on a POSIX system.
It invokes Claude through the draft child environment, not through the OCR provider registry.

---

## Development

### Environment

- JDK 21 (used by Gradle to run the build). The Gradle wrapper is included, so no separate Gradle install is required.
- Node.js + npm — build-time only, to bundle the webview. Not required at runtime.
- A globally available `ocr` CLI and `git` (see "Prerequisites") — the plugin is essentially a GUI front-end for `ocr`.

### Start the dev environment

```bash
cd extensions/idea
./gradlew runIde      # launch a sandbox IDE with the plugin installed
```

Open a project with Git changes in the sandbox IDE — you'll see the Open Code Review icon in the
tool window strip and can start a review.

> After editing code: webview changes require **reopening the tool window** in the sandbox IDE;
> host-side (Kotlin) changes require **restarting `runIde`**.

### Scripts

```bash
./gradlew build          # compile Kotlin + npm install + npm run build (bundles the webview)
./gradlew test           # run unit tests
./gradlew runIde         # launch a sandbox IDE with the plugin
./gradlew buildPlugin    # produce a distributable .zip (see "Build a release package")
```

> No Node.js / npm on PATH? Skip the frontend build with `./gradlew <task> -PskipFrontend=true`
> (uses the previously bundled `src/main/resources/webview/` output).

### Debugging notes

- **Two-way messaging**: the webview and host communicate via `postMessage` through the JCEF bridge;
  message types live in `extensions/frontend/src/shared/messages.ts`. Both sides route through `dispatch`
  / `handle` — start there when debugging. The bridge transport is the only file that differs from the
  VS Code extension: `frontend/src/webview/bridge.idea.ts` (vs. `bridge.vsc.ts`). webpack selects the
  correct bridge via `NormalModuleReplacementPlugin` based on the `OCR_TARGET` environment variable.
- **CLI invocation**: all `ocr` sub-commands run via `ProcessBuilder` in
  `src/main/kotlin/com/alibaba/opencodereview/idea/services/CliService.kt`. A non-zero CLI exit code
  rejects with the `Error:` text from stderr, which helps diagnose "review failed / connection failed".
- **Config read/write**: `ConfigService.kt` reads `~/.opencodereview/config.json` and delegates writes
  to `ocr config set`. Webview fields are camelCase (e.g. `useAnthropic`) while the disk/CLI side is
  snake_case (e.g. `use_anthropic`); the conversion lives in `ConfigParse.kt`.

---

## Build

### Compile artifacts only

```bash
./gradlew build        # compiles Kotlin and bundles the webview into src/main/resources/webview/
```

Artifacts: the plugin jar (Kotlin host) + `src/main/resources/webview/{webview.js,configPanel.js}` (webview SPA).

### Build a release package (.zip)

```bash
./gradlew buildPlugin
```

This compiles the host, bundles the webview, and produces
`build/distributions/open-code-review-idea-<version>.zip` in the `extensions/idea` directory.

### Install / verify locally

In IntelliJ IDEA: `Settings → Plugins → ⚙️ → Install Plugin from Disk…` → pick the generated `.zip`.

> To use a locally installed IntelliJ IDEA instead of the downloaded platform, set `localIdePath`
> in `~/.gradle/gradle.properties` (not the repo's `gradle.properties`, which is machine-independent).
> The build falls back to the downloaded platform if the path is missing.

---

## Architecture

It uses the same **Monolithic WebView + Thin Host** design as the VS Code extension:

- The **WebView** is a separately built Preact SPA, shared with the VS Code extension at
  `extensions/frontend/`. The only IDEA-specific file is `bridge.idea.ts`; webpack selects the
  correct bridge (`bridge.idea.ts` vs. `bridge.vsc.ts`) via `NormalModuleReplacementPlugin` based on
  the `OCR_TARGET` environment variable.
- The **Host** layer (Kotlin / IntelliJ) is thin, handling only CLI invocation, the file system, Git
  operations, and editor comments. It hosts the webview inside JCEF.
- The two communicate via `postMessage` through a JCEF bridge, with shared TypeScript types in
  `extensions/frontend/src/shared/` for type safety.

```
extensions/
├── frontend/                shared frontend (webview SPA + shared types)
│   ├── src/shared/          shared types & postMessage protocol
│   └── src/webview/         WebView SPA (Preact): views / components / store / bridge
└── idea/
    ├── src/main/kotlin/     Host (Kotlin / IntelliJ): services / providers / jcef / messages
    └── src/main/resources/  plugin.xml / icons / bundled webview output
```

---

## Compatibility

- `com.intellij.modules.jcef` is declared as an **optional** dependency, so the plugin loads on both
  2025.1 (JCEF in the core) and 2026.2 (JCEF as a separate bundled plugin).
- The webview's `--vscode-*` CSS variables are injected from the current IntelliJ theme, so the UI
  follows Darcula / Light automatically.

---

## License

Apache-2.0
