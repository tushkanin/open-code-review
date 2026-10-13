---
schema_version: 13
title: "CLI providers in existing OCR interfaces"
type: delivery
status: "completed"
order: 40
requirements: [PROVIDER-01, PROVIDER-02, PROVIDER-03, PROVIDER-04, PROVIDER-05, PROVIDER-06, PROVIDER-07, PROVIDER-08, PROVIDER-09, PROVIDER-10, PROVIDER-11]
dependencies: [TASK-001, TASK-002, TASK-003]
created_at: "2026-10-12"
updated_at: "2026-10-13T05:46:55.742601Z"
approved_at: "2026-10-12T14:57:05Z"
---

## Outcome

Users select either CLI provider in the existing OCR interfaces.
OCR runs its normal workflow with the completed adapters and native CLI authentication.

## Scope

- Add `claude-code` and `codex` to the Go provider registry.
- Add distinct CLI protocols and factory dispatch to the completed adapters.
- Apply authentication and URL requirements from the effective protocol.
- Update provider selection, model selection, and connection checks in the CLI and terminal setup.
- Update the shared provider form that VS Code and IntelliJ use.
- Generate both IDE catalogs from the Go registry.
- Preserve existing configuration precedence, IDE draft isolation, and authentication roots.
- Update affected tests and user documentation.

### Excluded

- Adapter redesign or changes to their approved safety controls.
- A separate review mode or workflow.
- Credential storage, login management, or credential copies.
- Custom executable paths or native command arguments.
- New HTTP retry behavior or a claimed hard Codex output-token limit.

## Acceptance

- **Provider selection:** The provider list, terminal setup, VS Code, and IntelliJ expose both distinct provider choices.
- **Native configuration:** CLI providers require a model, but no OCR API key, API-key command, or HTTP URL. They use the native CLI authentication configuration.
- **Effective protocol:** Native CLI protocols do not execute `api_key_cmd`. An HTTP protocol override restores the applicable HTTP URL and credential requirements. Setup controls follow the effective protocol, not only the preset's authentication flag.
- **Model selection:** Existing provider and model precedence remains unchanged. Users can save model names that do not appear in the suggestions. Codex suggestions match the bundled catalog for the supported release. Native compatibility checks remain intact.
- **Setup controls:** Native CLI setup omits irrelevant API-key and URL controls. Its guidance identifies native CLI authentication without AWS-specific instructions or empty environment-variable hints.
- **Generated catalogs:** Both generated catalogs include the new providers. Both IDEs store their entries under `providers`, not `custom_providers`.
- **Connection checks:** Both providers pass real connection checks through the public resolver and factory. Each provider demonstrates the existing OCR-controlled tool round trip.
- **Normal OCR review:** Each provider completes a bounded standard OCR review. OCR retains control of task groups, scope, rules, prompts, and tool execution. The CLIs do not perform independent reviews or execute native review tools.
- **Existing contracts:** The integration preserves conversation data, tool-call IDs, arguments, results, and usage data. Cancellation, timeouts, and provider errors use the existing paths. Existing providers and review formats remain unchanged.
- **IDE draft checks:** Both IDEs test the selected native provider through the normal OCR CLI. They preserve the original authentication roots without copying credentials. Draft checks leave saved configuration unchanged and remove temporary files after success or failure.
- **Verification and documentation:** Verification exercises the terminal setup and shared IDE provider form. Tests cover credential-helper side effects, HTTP overrides, precedence, and draft isolation. The delivery runs the required repository checks and affected frontend and IDE checks. Documentation explains provider selection, native authentication, and existing compatibility limits.

## Plan

### Execution boundary

Use `/home/oleksii/Projects/personal/open-code-review` and its `docs/specs` root.
Follow the approved [Design](../design.md).
Keep the Task contract, completed dependency evidence, and Specs files outside source commits.
This Plan coordinates the registry, resolver, terminal setup, shared frontend, and both IDE draft paths.

### Steps

1. Re-read the affected locations before source changes.
   Use LSP references before changes to exported symbols.
   `NewLLMClient` references identify the connection check in `cmd/opencodereview/llm_cmd.go` and review runtime in `cmd/opencodereview/shared.go`.
   Preserve the shared orchestration in `internal/agent/agent.go` and `internal/llmloop/loop.go`.

2. Extend `internal/llm/protocol.go` and `internal/llm/providers.go` together.
   Use provider IDs `claude-code` and `codex`, with canonical protocols `claude-code-cli` and `codex-cli`.
   Add explicit factory cases in `internal/llm/client.go` for `NewClaudeCodeClient` and `NewCodexClient`.
   Set CLI presets to ambient authentication, with no API URL, API-key environment variable, or auth header.
   Start with the proven model suggestions `sonnet` and `gpt-6.1-sol`.
   Confirm the Codex suggestion against the installed bundled catalog before publication.
   Keep suggestions separate from runtime compatibility checks and free-text model input.

3. Add small protocol predicates for CLI transport and ambient authentication.
   Ambient authentication includes Bedrock and both CLI protocols.
   CLI transport includes only the two CLI protocols; Bedrock still uses HTTP.
   Avoid a capability framework or duplicate preset-specific credential rules.

4. Update `tryProviderConfig` in `internal/llm/resolver.go` after it resolves the effective protocol.
   Native protocols accept a model without URL or OCR credentials and never execute `api_key_cmd`.
   Generalize the custom-provider URL exception through the same protocol rule.
   Require the applicable HTTP URL and credentials when a CLI preset has an HTTP override.
   Check missing HTTP URLs before credential-helper execution, including the explicit `--provider` resolution path.
   Preserve provider precedence, entry/global/flag model precedence, timeout overrides, and existing HTTP credential-source precedence.
   Keep the legacy URL/token configuration routes HTTP-only.
   Extend their existing Bedrock rejection pattern to reject CLI protocols rather than silently discard URL/token settings.
   Add no environment selection mode or product configuration knob.

5. Update `cmd/opencodereview/config_cmd.go`, `provider_cmd.go`, and `provider_tui.go`.
   Base key requirements and wizard transitions on the saved protocol override or preset protocol.
   Reuse the custom terminal form's ambient branch when its canonical protocol list gains the CLI protocols.
   Keep the HTTP-only Manual form HTTP-only.
   Select native authentication guidance from the effective protocol and omit empty environment-variable hints.
   Keep `providerAcceptsAWSSettings` Bedrock-only; its current preset `AmbientAuth` fallback must not admit AWS fields on CLI presets.
   Preserve saved HTTP overrides, model selection, and the existing save-before-connection-check behavior.

6. Integrate the existing command consumers without a second review path.
   Keep `runLLMTestWithConfigPath` in `cmd/opencodereview/llm_cmd.go` on the public resolver and factory.
   Report a CLI transport instead of an empty HTTP URL for native endpoints.
   Preserve its existing optional tool-round-trip semantics.
   In `loadLLMRuntime`, avoid HTTP-only retry/raw collector allocation for CLI protocols after checking their consumers for nil safety.
   Retain those collectors for all existing HTTP transports, including Bedrock.
   Do not fabricate HTTP attempts or change native adapter safety, parsing, authentication, or request limits.

7. Update `extensions/frontend/src/shared/providers.ts` and the official form in `extensions/frontend/src/webview/views/ConfigView.tsx`.
   Extend the preset protocol type and `usesAmbientAuth` for both CLI protocols.
   Apply the saved protocol override before preset authentication metadata.
   Show native authentication guidance and normal model controls without a key field for native protocols.
   Restore the API-key field for HTTP overrides without an empty environment-variable hint.
   Keep the official form's existing absence of a URL editor; use existing configuration commands for saved URL overrides.
   Use the existing translation mechanism for new UI copy.
   Preserve free-text models, saved-model precedence, and the existing save/test entry format in `shared/configUtils.ts`.
   Do not add a separate native CLI form.

8. Run `go generate ./internal/llm` after the registry change.
   Publish `extensions/frontend/src/shared/providers.generated.ts` and `extensions/idea/src/main/kotlin/com/alibaba/opencodereview/idea/services/ProviderNames.generated.kt` together.
   Do not edit either generated file by hand.
   Verify the built-in bucket behavior in VS Code `configDraft.ts` and IntelliJ `ConfigDraft.kt`.
   Preserve both `ConfigService.testWithEntries` paths and both `CliService.testConnection` authentication-root rules.
   Change those services only when integration evidence requires a change.

9. Extend existing behavior regressions at the resolver, setup, frontend, and IDE boundaries.
   Cover native credential-helper non-execution, HTTP overrides, missing HTTP URLs, and rejection of AWS fields on native protocols.
   Cover model precedence, provider-switch draft isolation, and success/failure cleanup.
   Use local HTTP fixtures for HTTP override checks; do not use live API accounts for permanent tests.
   Remove touched constructor-type tests in `internal/llm/client_test.go` instead of adding native type assertions.
   Remove the exact supported-protocol error-list test in `internal/llm/protocol_test.go` instead of updating its wording expectations.
   Retain consumer-visible validation, lifecycle, and API behavior tests.
   Use throwaway smoke code for catalog/list output and factory dispatch; avoid metadata-copy or mock-echo tests.

10. Complete the real exercises below after the source change is complete.
    Record each exercised surface and its actual result.
    Update `internal/llm/CLI_COMPLETIONS.md` and both extension README files after smoke proof.
    Remove the guide's obsolete statements that the registry and IDEs cannot select the adapters.
    Preserve existing authentication-root, compatibility, native safety, and limit documentation.
    If the root README changes, synchronize all four localized README files.
    Remove temporary smoke code and generated test artifacts.

### Focused verification

Run checks after the complete source change, not during parallel edits.
Run `make license-add` if the implementation creates source files.
Use the existing Make targets for Go tests.

From the repository root:

```sh
make check
make test PACKAGES='./internal/llm ./cmd/opencodereview ./internal/agent ./internal/llmloop ./internal/tool'
make coverage
```

From `extensions/frontend`:

```sh
npm test -- --runInBand
npm run typecheck
OCR_TARGET=vscode npm run build
OCR_TARGET=idea npm run build
```

From `extensions/vscode`:

```sh
corepack yarn test --runInBand configDraft.test.ts CliService.auth.test.ts CliService.test.ts CliService.cancel.test.ts
./node_modules/.bin/tsc --noEmit -p tsconfig.extension.json
```

Run scoped ESLint on changed VS Code source and test files.
Do not use a default TypeScript invocation that emits JavaScript into source directories.

From `extensions/idea`:

```sh
mise exec java@temurin-21 -- ./gradlew test -PskipFrontend=true --tests '*ProvidersTest' --tests '*ConfigDraftTest' --tests '*ConfigParseTest' --tests '*ConfigUtilsTest' --tests '*CliServiceAuthTest' --tests '*CliServiceConcurrencyTest' --tests '*CliServiceTreeKillTest'
```

Use JDK 21 for IntelliJ checks.
The shared frontend build above supplies the resources that `-PskipFrontend=true` omits.

### Real exercises

- Check installed CLI versions before live requests.
  The preparation run observes `codex-cli 0.162.1` and Claude Code `2.1.296`.
  This observation does not replace authentication or completion evidence.
- Build the changed OCR binary and use that binary for every integration exercise.
  Use a temporary OCR home and a small temporary Git repository with one controlled diff.
  Preserve explicit `CLAUDE_CONFIG_DIR` and `CODEX_HOME` values exactly.
  Derive missing roots from the original home before replacing `HOME` or `USERPROFILE`.
  Copy no CLI credentials and change no native configuration or managed policy during a request.
- Run the provider list and interact with the actual terminal setup for each provider.
  Inspect its saved ID and model, native guidance, and absence of key/URL prompts.
  Exercise a saved HTTP override and confirm that the key step returns.
  Save a model outside the suggestion list without weakening native runtime validation.
- Run the actual `ocr llm test` command for each native provider.
  Require a demonstrated tool round trip, not only successful text or a skipped-tool notice.
  Preserve the default command semantics rather than forcing new native-only behavior.
- Exercise the public resolver and factory with the existing OCR tool registry and a random file fixture.
  Adapt the existing Claude and Codex smoke patterns with throwaway code if the command exercise does not expose call identifiers.
  Require the second turn to use a value absent from the initial prompt.
  Observe actual usage, call/result relationships, native-tool rejection, and temporary cleanup.
  Direct adapter constructors alone do not prove integration.
- Run a bounded normal OCR review for each provider on the controlled diff.
  Use the existing `--repo`, `--provider`, `--model`, `--effort low`, `--concurrency 1`, `--timeout`, and `--format json` flags.
  Inspect selected-item completion, standard result format, session tool records, and usage.
  Exit zero or an empty findings list alone does not prove a completed review.
- Exercise the actual shared provider UI with both IDE-target bundles.
  Inspect both provider choices, saved/custom models, native authentication guidance, and key visibility under HTTP overrides.
  Prefer the installed IDE hosts; if they are unavailable, render the real bundles in a browser and report the host limit.
  Keep browser surface proof separate from the real IDE-service process launches below.
- Invoke both real `ConfigService.testWithEntries` paths with each native provider and the changed OCR binary.
  Reuse the constructor injection and shell-environment setup from each existing `CliServiceAuthTest` fixture.
  Replace the direct-Claude test launcher with the real OCR binary for this integration exercise.
  Observe the native completion through resolver/factory dispatch, unchanged saved OCR configuration, and temporary-home cleanup.
  Exercise a failing draft check without changing the user's native authentication state.
  A mocked `runRaw` result or the earlier direct-Claude launcher does not prove this path.
- Report platform and installed-host limits explicitly.
  Do not claim Windows runtime proof from Unix launches or source checks.

## Execution evidence

### Completion

#### Delivery and source commit

Task: 001/004, CLI providers in existing OCR interfaces.
Task path: /home/oleksii/Projects/personal/open-code-review/docs/specs/001-claude-code-and-codex-cli-providers/tasks/004-cli-provider-integration.md.
Execution follows the approved ## Plan section and shared Design.
Repository and checkout: /home/oleksii/Projects/personal/open-code-review.
Branch: main.
Specs root: /home/oleksii/Projects/personal/open-code-review/docs/specs.
Base commit: da0dd55c6accc66a9aa8ab43ac793919f1913ce9.
Source commit: 37fff643a1a1a82dcac18964c2c348eb97967e78.
Commit subject: feat(llm): integrate native CLI providers.
The user selects the current checkout and later answers continue.
The commit contains exactly 31 Task source, test, generated, and documentation paths.
It contains no Specs file or AI trailer.
Scoped git add --renormalize applies the repository's text=auto eol=lf rule.
All 31 paths already use LF in the index and worktree.
No source change follows the final independent pass.
The Spec remains active. No Spec completion approval exists.

#### Independent verification and evidence boundary

VerifyIntegration004Final reports pass with findings=[].
The read-only verdict clears F1 through F6 and resolves F7 through the directed policy exception.
The verifier inspects the full 31-path diff, canonical Specs files, every context read, and six repository source references.
Its verdict establishes source-contract compliance, not runtime acceptance.
The parent assesses the retained runtime evidence separately and authorizes Task completion.

Two failed source fix rounds occur before the user's continue answer.
The implementation records three credential repairs separately: F1, F2, and F3.
F1 binds Official credential drafts to the selected entry.
F2 binds Custom credential drafts to the selected entry.
F3 preserves the selected Official ambient entry's own saved key.
R3's initial E1 correction concerns verification scope, not a failed source repair.
The required OCR review then identifies missing documentation.
CompleteIntegration004 adds provider rows and native authentication guidance to all four guides.
FixIntegration004Docs repairs F4, F5, and F6 in all four guides.
The final independent verifier covers these documentation repairs.
This report uses these actual actions instead of an ambiguous combined fix-round count.

The original /tmp/ocr-task001004-evidence.md and /tmp/ocr004-real-2GpF81 directory do not survive the resumed session.
The retained /tmp/ocr-task001004-completion-held-evidence.md supplies the prior complete report.
Agent results and retained raw outputs supply factual evidence where original files do not survive.
Sources include agent://CompleteIntegration004, agent://FixIntegration004Docs, agent://VerifyIntegration004Final, and the integration and repair transcripts.
Raw evidence includes artifact://16, artifact://23, artifact://39, artifact://45, artifact://49, artifact://152, artifact://171, and artifact://188.
This completion run repeats no earlier successful review, test, build, or live request.

#### Implementation

The registry exposes claude-code and codex with distinct native CLI protocols.
The public resolver and factory select the completed adapters.
Authentication and URL requirements follow the effective protocol.
Native protocols skip api_key_cmd. HTTP overrides restore HTTP requirements.
Terminal setup and the shared IDE form retain model selection and native authentication guidance.
Both generated IDE catalogs include both providers.
The runtime omits HTTP collectors only for native CLI transports.
The adapters retain their approved safety controls and existing OCR orchestration.
Credential drafts retain only the selected entry's own key.
All four configuration guides cover native selection, Custom native protocols, and HTTP-only proxy recipes.
They identify native Codex token limits as transcript guidance, not an enforced native output-token cap.

#### Acceptance evidence

1. Provider selection: met.
   The actual provider list and terminal setup expose both choices.
   Both actual IDE-target bundles and installed VS Code 1.140.0 expose both choices.
   The final verifier confirms registry, terminal, TypeScript, and Kotlin catalogs.
   IntelliJ surface proof uses its actual target bundle, not an installed JCEF host.

2. Native configuration: met.
   Actual public connection checks and real IDE draft launches use native authentication without OCR keys or URLs.
   F1 terminal sessions show no credential transfer from active OpenAI to a native entry.
   F2 and F3 terminal sessions preserve the selected entry's own key or absence of a key.
   These focused persistence sessions deliberately fail the connection after the real save.
   Earlier successful native completions supply completion evidence.

3. Effective protocol: met.
   Resolver regressions cover helper non-execution, missing HTTP URLs, HTTP completion fixtures, and legacy-protocol rejection.
   Actual HTTP override terminal flows restore key steps for both presets.
   Both target bundles and installed VS Code restore the key field.
   F1/F2 regressions preserve the selected HTTP entry's own key and endpoint fields.
   F3 terminal sessions show no helper execution.

4. Model selection: met.
   Resolver tests preserve entry, global, and flag precedence.
   The actual terminal picker saves ocr-custom-model outside the suggestions.
   Both target bundles support saved and custom models.
   Installed VS Code saves host-custom-model under providers.codex.
   The implementation observes gpt-6.1-sol in the bundled catalog of Codex 0.162.1.
   An initial IntelliJ launch selects unsupported 0.160.1 and correctly fails closed.
   This completion run does not export the catalog again.

5. Setup controls: met.
   Actual native terminal sessions and target bundles omit irrelevant key and URL controls.
   Native guidance has no AWS instruction or empty environment-variable hint.
   Installed VS Code confirms native guidance and restored HTTP key controls.
   F3 sessions cover Bedrock, Claude Code, and Codex without a key prompt.

6. Generated catalogs: met.
   The implementation runs go generate ./internal/llm and publishes both generated catalogs.
   Draft tests and real launches use providers rather than custom_providers.
   Installed VS Code saves the Codex custom model in providers.codex.
   The final verifier confirms generated content agrees with the registry.

7. Connection checks: met.
   Both actual ocr llm test commands report Tool-call round trip verified.
   The public resolver/factory exercise uses the existing OCR file_read registry and two real turns.
   Claude requests call_file_read_1 with {"file_path":"fixture.txt"}.
   Its result contains a31864870c33dfde3776be53de831816, which the initial prompt excludes.
   First usage: 3072 total, 2950 input, 122 output, 0 cache read, 2948 cache write.
   Second usage: 3217 total, 3125 input, 92 output, 820 cache read, 2303 cache write.
   Codex requests call_1 with the same arguments.
   Its result contains b0f6782611f5adbeca3ff30f9bf732d6, which the initial prompt excludes.
   First usage: 1109 total, 1069 input, 40 output.
   Second usage: 1212 total, 1174 input, 38 output.
   Codex cache counts are zero in both turns.
   Both exercises preserve call/result relationships and leave the request temporary root empty.
   Raw evidence: artifact://16:1967-1974.
   The initial aggregate run later fails elsewhere; subsequent final checks pass at artifact://49.

8. Normal OCR review: met.
   Both providers run the standard review on one controlled divide.go diff.
   Arguments include --effort low --concurrency 1 --timeout 180 --format json.
   Claude session d377d7eb-d790-475b-97f8-c23d7b4613d1 completes with one finding in 16 seconds.
   OCR records code_search, code_comment, and review-filter approve_all_comments.
   Usage: 28518 total, 27412 input, 1106 output, 2460 cache read, 24946 cache write.
   Codex session 2e2a21f5-6ecb-44d5-b74d-dd457c2f86ce completes with no findings in eight seconds.
   Its session records OCR task_done, identifier review_done_1, and {"state":"DONE"}.
   Usage: 5901 total, 5805 input, 96 output.
   Both manifests match selected and completed fingerprints, with no failed, waived, or reused items.
   The implementation transcript retains raw review stdout and session reads.
   Neither CLI performs an independent native review.

9. Existing contracts: met.
   Real public completions preserve conversation data, identifiers, arguments, results, and actual usage.
   Focused race tests cover existing providers, orchestration, cancellation, deadlines, errors, and tools.
   The final verifier confirms unchanged adapter safety and normal OCR orchestration.
   It also confirms process ownership and nil-safe HTTP collector consumers.
   F1/F2/F3 regressions compare persisted configuration and preserve unrelated entries and unknown fields.
   The final command-package race run passes after F3.

10. IDE draft checks: met with the stated assertion limit.
    Both real ConfigService.testWithEntries paths launch the changed OCR binary for both native providers.
    Successful and missing-executable checks preserve saved configuration and remove temporary draft homes.
    Both paths retain original native authentication roots and copy no credentials.
    VS Code asserts the actual tool-round-trip output.
    IntelliJ asserts success and isolation; its service discards stdout.
    Its success Boolean alone does not independently establish a tool round trip.
    Separate public checks and two-turn exercises establish both provider round trips.
    Installed VS Code completes both native checks without a saved configuration change.

11. Verification and documentation: met.
    Final Go checks and focused race tests pass. Full coverage is 91.1%, above 90%.
    Frontend tests, type checks, both target builds, VS Code checks, and selected JDK 21 IntelliJ checks pass.
    Actual terminal, both target bundles, and installed VS Code receive interface exercises.
    CLI_COMPLETIONS.md and both extension README files describe native selection, authentication, and compatibility limits.
    All four configuration guides cover F4/F5/F6, with a successful production site build.
    The final independent verifier passes after all documentation repairs.

#### Exercised commands and results

Initial implementation:
- go generate ./internal/llm: pass.
- make license-add: pass.
- make check: final pass; license, English, tidy, format, and vet. artifact://49.
- make test PACKAGES='./internal/llm ./cmd/opencodereview ./internal/agent ./internal/llmloop ./internal/tool': final race pass. artifact://49.
- go build -o /tmp/ocr-integration004 ./cmd/opencodereview: pass.
- GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=diff.mnemonicPrefix GIT_CONFIG_VALUE_0=false make coverage: pass, 91.1%. artifact://45.
- Frontend npm install --package-lock=false --ignore-scripts: pass; no new lock file.
- Frontend npm test -- --runInBand: pass, two suites and 56 tests. artifact://23.
- Frontend npm run typecheck: pass.
- Frontend OCR_TARGET=vscode npm run build: pass.
- Frontend OCR_TARGET=idea npm run build: pass.
- VS Code OCR_INTEGRATION_BINARY=/tmp/ocr-integration004 corepack yarn test --runInBand configDraft.test.ts CliService.auth.test.ts CliService.test.ts CliService.cancel.test.ts: pass, four suites, 23 tests, one older opt-in skip.
- VS Code ./node_modules/.bin/tsc --noEmit -p tsconfig.extension.json: pass.
- VS Code ./node_modules/.bin/eslint src/extension/services/__tests__/CliService.auth.test.ts: pass.
- VS Code corepack yarn build:extension: pass before the installed-host exercise.
- IntelliJ OCR_INTEGRATION_BINARY=/tmp/ocr-integration004 OCR_INTEGRATION_PATH=/home/oleksii/.local/share/mise/installs/node/22/bin:/home/oleksii/.local/bin:/usr/local/bin:/usr/bin:/bin mise exec java@temurin-21 -- ./gradlew test -PskipFrontend=true --tests '*ProvidersTest' --tests '*ConfigDraftTest' --tests '*ConfigParseTest' --tests '*ConfigUtilsTest' --tests '*CliServiceAuthTest' --tests '*CliServiceConcurrencyTest' --tests '*CliServiceTreeKillTest': pass. artifact://39.

Credential repairs:
- F1 TestProviderTUI_HTTPToNativeCredentialIsolation: fails before the repair; four cases pass afterward. artifact://111 and artifact://116.
- F1 go build -o /tmp/ocr-integration004-f1 ./cmd/opencodereview and python3 /tmp/ocr004-f1-terminal.py: pass; two actual terminal switches.
- F1 make check PACKAGES=./cmd/opencodereview && make test PACKAGES=./cmd/opencodereview: race pass. artifact://116.
- F2 TestProviderTUI_CustomCredentialIsolation: six ambient cases fail before the repair; eight cases pass afterward. artifact://131 and artifact://135.
- F2 go build -o /tmp/ocr-integration004-f2 ./cmd/opencodereview and python3 /tmp/ocr004-f2-terminal.py: pass; eight actual Custom terminal sessions.
- F2 make check PACKAGES=./cmd/opencodereview && make test PACKAGES=./cmd/opencodereview: race pass. artifact://135.
- F3 TestProviderTUI_OfficialAmbientOwnCredential: six saved-key cases fail before the repair; twelve cases pass afterward. artifact://150 and artifact://152.
- F3 make check PACKAGES=./cmd/opencodereview && make test PACKAGES=./cmd/opencodereview && go build -o /tmp/ocr-integration004-f3 ./cmd/opencodereview && python3 /tmp/ocr004-f3-terminal.py: pass; complete command package and six actual terminal sessions. artifact://152.

Required review and documentation repair:
- The first ocr review --audience agent --background invocation stops before review because the mise shim has no selected Node version.
- mise exec node@22 -- ocr review --audience agent --background <integration invariants>: completes with seven findings across twelve selected items.
- Review session: 5a5309b8-7357-482d-9b47-4bab3a2c0281. Raw output: artifact://171.
- Four-guide git diff --check: pass in the completion attempt and documentation repair.
- Pages mise exec node@22 -- npm install --no-package-lock --ignore-scripts: fails in peer resolution before a build.
- Pages mise exec node@24 -- corepack pnpm install --lockfile=false --ignore-scripts: pass without a new lock file.
- Pages mise exec node@24 -- npm run build: pass; Webpack 5.111.1 reports two size warnings. artifact://188.

This completion run:
- specs.py resolve-root and context --scope 001/004: pass, expected roots, approved Task and Design, completed dependencies, no findings or warnings.
- git status --short --branch and git log -1: main at the assigned base, zero staged paths and 31 unstaged Task paths.
- git ls-files --eol -- <31 explicit Task paths>: all index and worktree content uses LF under text=auto eol=lf.
- git add --renormalize -- <31 explicit Task paths>: pass; exactly 31 staged paths, zero unstaged paths, existing untracked docs/specs/.
- git diff --cached --check: pass.
- cmp <Task path> /tmp/ocr-task001004-commit-before.md: pass before commit; contract and Plan bytes remain unchanged.
- git commit -m "feat(llm): integrate native CLI providers": pass; exactly one source commit, 31 files, 823 insertions and 205 deletions.
- git rev-parse HEAD: 37fff643a1a1a82dcac18964c2c348eb97967e78.

All specs.py calls use python3 -I -B /home/oleksii/.agents/plugins/specs/scripts/specs.py.
Lifecycle calls set SPECS_ROOT to the supplied main Specs root and --project-root to the integration root.

#### Required local review disposition and policy exception

1. Ambient-auth metadata fallback: invalid for the supported registry.
   Every ambient registry entry uses Bedrock or a native CLI protocol.
   The suggested fallback creates a second credential rule and conflicts with effective-protocol authentication.
2. Missing localized configuration tables: valid and repaired in all four guides.
   The final verifier also clears their token-cap, Custom protocol, and proxy recipe contradictions.
3. Named metadata-copy tests: not applied under the directed policy exception.
   Higher-priority developer test policy and the approved Plan prohibit metadata-copy and wiring tests.
   They override the lower-priority .opencodereview/rule.json detail-test request.
   Behavioral resolver/setup regressions, generated catalogs, actual interface exercises, and real dispatch cover the observable contract.
   F7 status is resolved_policy_exception.
   This report does not claim literal compliance with TestLookupProvider_<Name>Details requirements.
   No metadata-copy test or review-rule change occurs.
4. Nil RetryCollector panic: invalid.
   RetryCollector.Freeze handles a nil receiver; both bounded native reviews complete through that path.
5. Official ambient own-key retention: invalid.
   Selection preserves only that entry's key; deletion would reproduce F3 data loss.
   Native resolution ignores dormant credentials and never executes their helpers.
6. Custom ambient own-key retention: invalid.
   Selection preserves only that Custom entry's key, as F2 regressions and actual terminal sessions show.
   Explicit Custom protocol edits clear unused fields separately.
7. Custom protocol edit retains URL/header: invalid.
   The real edit persistence path assigns those fields unconditionally and clears ambient keys.
   The reviewer cites a different save path.

#### Deviations, failures, and warnings

The harness has no Skill tool. skill://specs:implement returns Unknown skill.
skill://implement supplies the actual Task-mode instructions. No Spec mode runs.
The initial implementation lacks its expected LSP device and uses repository reference searches.
A browser tool rejects a nested iframe reference; the implementation reports the tool issue.
The Node REPL exits during host inspection; a temporary CDP probe supplies the fallback and is removed.

The initial frontend command lacks Jest; a local install resolves it without a lock file.
The user's diff.mnemonicPrefix setting affects the first coverage run.
A process-local override resolves that issue without a user configuration change.
A test-only PATH selects supported Codex 0.162.1 after an initial 0.160.1 compatibility failure.
It changes no native authentication root.
PTY harness assumptions and fixture issues receive harness-only corrections.
The first F3 regression compile lacks fmt; its import precedes the behavioral reproduction.
The installed VS Code exercise initially selects an older binary, then uses the changed binary.
An isolated empty window avoids Restricted Mode without a trust-setting change.

JDK 21 reports a PathClassLoader class-data-sharing warning.
Dependency evidence includes Yarn metadata, ts-jest deprecation, and IntelliJ Gradle plugin warnings.
The documentation build reports two Webpack asset/entrypoint size warnings.
The local reviewer guesses two nonexistent adapter paths; both file_read calls fail, but the review completes.
The real adapters use *_client.go paths.
No adapter redesign, native safety change, contract revision, Design revision, or Plan revision occurs.

#### Not fixed and verification limits

No known actionable Task source defect remains after the final verifier pass and assessed runtime evidence.
Runtime evidence covers Linux amd64 and POSIX launches only.
No Windows or macOS runtime proof applies.
IntelliJ evidence covers its real JVM service and actual target bundle, not an installed JCEF host.
Independent verifiers run no tests or live requests.
IDE live tests can skip without OCR_INTEGRATION_BINARY; recorded opt-in commands supply the real evidence.
Permanent persistence regressions substitute only the post-save connection callback.
Actual PTY sessions exercise the real save and post-save failure path.
IntelliJ's success Boolean alone does not prove a tool round trip.
The separate public checks establish those round trips.

Codex retains exact release 0.162.1 compatibility and exact-model catalog checks.
It retains native-tool rejection and conservative event handling.
The approved stable-configuration assumption and later reload race remain.
No hard Codex output-token cap or race-free isolation claim applies.
The production site build establishes compilation, not browser rendering by itself.
F7 remains an explicit policy exception, not literal lower-priority rule compliance.

#### Filesystem, helper ownership, and remaining Spec consequence

The source commit includes only the 31 approved Task paths.
The existing untracked docs/specs/ remains outside the source commit.
The contract, Plan, Design, approvals, and completed dependency evidence remain intact.
Only the helper may add Task lifecycle fields and this Execution evidence at completion.
Temporary smoke code, harness scripts, draft homes, review fixtures, browser services, and coverage.out receive cleanup in prior runs.
The documentation build leaves only ignored pages/dist and pages/node_modules outputs.
This run creates external evidence and a Task snapshot under /tmp, not repository scaffolds.

The Task implements the remaining provider integration scope.
The Spec stays active pending an explicit user decision through the Spec workflow.
This Task completion does not authorize a push, PR, deployment, or Spec completion.
