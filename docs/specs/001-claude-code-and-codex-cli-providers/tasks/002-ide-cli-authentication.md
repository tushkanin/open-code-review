---
schema_version: 13
title: "Preserve CLI authentication in IDE connection checks"
type: delivery
status: "completed"
order: 20
requirements: [PROVIDER-07, PROVIDER-11]
created_at: "2026-10-12"
updated_at: "2026-10-12T07:36:43.948653Z"
approved_at: "2026-10-12T06:34:59Z"
---

## Outcome

VS Code and IntelliJ retain the user's CLI authentication roots while testing an isolated draft OCR configuration.

## Scope

- The child environment for draft connection checks in both IDE extensions.
- Preservation of existing `CLAUDE_CONFIG_DIR` and `CODEX_HOME` overrides.
- Default authentication roots derived from the original home directory.
- Existing temporary OCR configuration isolation and cleanup.
- Focused verification and documentation for authentication-root preservation.

### Excluded

- Provider adapters, provider catalogs, and model selection.
- Credential reads, copies, or storage by the extensions.
- Changes to normal review launches or API-provider authentication.

## Acceptance

- Each extension preserves explicit authentication-root overrides when it replaces `HOME`.
- Without an override, each extension derives authentication roots from the original home, not the temporary draft home.
- A launch-path smoke demonstrates that the child CLI can use the original authentication state under the temporary OCR home.
- Draft checks leave the user's saved OCR configuration unchanged.
- Temporary directories contain no copied CLI credentials.
- Temporary OCR configuration still controls the check, and existing cleanup remains effective.
- Existing API-provider draft connection checks retain their behavior.

## Execution evidence

### Completion

#### Source commit

Repository: /home/oleksii/Projects/personal/open-code-review, branch main.

Base: 6cbef414c243a1122983cb29c52b81c622cb3aae.

Commit: 8e8e3d1966d8673357c24acd7fe3d410a80cd16c.

Commit title: fix(ide): preserve CLI authentication in draft checks.

The commit contains only the two CliService files, two authentication test files, and two extension README files.
No Specs path enters the commit.

Verifier: pass after 1 fix rounds.

Independent verdict: agent://VerifyIDE002Round1. The verifier finds no remaining defect. Earlier F1 clears.
No source changes or repeated successful checks follow the verifier pass.

#### Verification commands

VS Code working directory: /home/oleksii/Projects/personal/open-code-review/extensions/vscode.

- `OCR_IDE_AUTH_SMOKE=1 OCR_AUTH_CLAUDE=/home/oleksii/.local/bin/claude corepack yarn test --runInBand CliService.auth.test.ts CliService.test.ts CliService.cancel.test.ts configDraft.test.ts`
  Result: 4 suites and 22 tests pass after the F1 fix. The real Claude smoke runs and passes. Evidence: background job bg_14.
- `./node_modules/.bin/tsc --noEmit -p tsconfig.extension.json`
  Result: pass with no output after the F1 fix. Evidence: the successful bg_14 command chain.
- `./node_modules/.bin/eslint src/extension/services/CliService.ts src/extension/services/__tests__/CliService.auth.test.ts`
  Result: pass with no output after the F1 fix. Evidence: the successful bg_14 command chain.

IntelliJ working directory: /home/oleksii/Projects/personal/open-code-review/extensions/idea.

- `OCR_IDE_AUTH_SMOKE=1 OCR_AUTH_CLAUDE=/home/oleksii/.local/bin/claude mise exec java@temurin-21 -- ./gradlew test -PskipFrontend=true --tests '*CliServiceAuthTest' --tests '*CliServiceConcurrencyTest' --tests '*CliServiceTreeKillTest' --tests '*ConfigDraftTest'`
  Result: BUILD SUCCESSFUL. Production and test Kotlin compilation pass. The four selected suites contain 37 tests with no skips, failures, or errors.
  Evidence: artifact://50 and build/test-results/test/*.xml. CliServiceAuthTest.xml records the successful real Claude smoke.
  No IntelliJ file changes occur in the F1 fix.

#### Acceptance evidence

1. Explicit authentication-root overrides remain intact in both extensions.
   Both testConnection methods select CLAUDE_CONFIG_DIR and CODEX_HOME from the effective shell environment before home replacement.
   Both focused launch suites pass assertions for distinct explicit roots.

2. Missing authentication roots use the original home in both extensions.
   Both actual POSIX child launches expose .claude and .codex under the original HOME.
   The active mocked VS Code Windows test confirms original USERPROFILE selection and replacement of both home variables.

3. Both launch-path smokes use real original CLI authentication under the temporary OCR home.
   Each smoke calls ConfigService.testWithEntries, the changed testConnection method, and the actual runRaw process launch.
   The child fixture invokes the installed Claude CLI and requires OCR_IDE_AUTH_OK. Both smokes pass.
   These smokes run on POSIX. Windows fixture tests skip. Windows environment behavior uses a mocked launch.
   The evidence does not claim full OCR provider integration or a live Codex completion.

4. Draft checks leave saved OCR configuration unchanged.
   Both suites compare saved configuration before and after successful and failed draft checks.
   Both live smoke cases also compare the original OCR configuration before and after the launch.

5. Temporary directories contain no copied CLI credentials.
   After the real CLI returns, both fixtures inspect the temporary home and its OCR directory.
   The directories contain only .opencodereview/config.json.
   Production changes pass environment paths and perform no credential I/O.

6. Temporary OCR configuration controls the check and cleanup remains effective.
   Both child fixtures read the draft from the replaced HOME and require draft-model.
   Both suites confirm temporary-home removal after success and failure.

7. Existing API-provider draft behavior remains intact.
   Both launch suites preserve the OpenAI draft API-key field, apply the draft model, and retain saved configuration.
   Existing configDraft suites pass in both extensions.
   HTTP-provider and ConfigService implementations remain unchanged.
   No external HTTP-provider endpoint receives a live request.

#### Scope and filesystem review

The implementation changes only draft child environments, focused authentication tests, and extension documentation.
Normal review launches, provider adapters, registry integration, catalogs, and model selection remain unchanged.
The Task contract body, Plan absence, and approval remain unchanged.
The helper changes only Task status and updated_at before completion.
The untracked docs/specs/ root remains outside source staging.
No source-side compiler output remains after cleanup.

#### Deviations

- Both executable fixture suites and their live smokes use POSIX launchers. The README files state this limit.
- The real smoke uses a test-only OCR launcher to invoke Claude through the changed IDE environment. Provider integration remains excluded.
- Corepack supplies Yarn because the shell has no yarn command.
- A managed JDK 21 supplies the IntelliJ runtime because the system JDK 27 fails Gradle configuration.

#### Earlier failed attempts

The initial VS Code test cannot spy on the os namespace homedir export. An explicit os mock resolves that test setup failure.
An initial Yarn exec invocation consumes the TypeScript flags. TypeScript uses the default scope and emits source-side JavaScript.
All generated JavaScript and maps from that attempt are removed. The direct configured type check passes.
The first Gradle attempt fails under JDK 27 before compilation. The JDK 21 command passes.

#### Not fixed

The accidental default TypeScript invocation reports TS2743 in unchanged CliService.cancel.test.ts.
It also reports TS2531 in unchanged cliParse.test.ts.
These files remain outside Task scope. The configured extension type check passes.
Existing Yarn metadata, ts-jest deprecation, and IntelliJ Gradle plugin warnings remain.

#### Consequences

Both IDE launch paths support authentication-root preservation for later provider integration.
No contract, Design, dependency, or later Task change is necessary.
