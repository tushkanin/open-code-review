# Contributing to OpenCodeReview

Thank you for your interest in contributing to OpenCodeReview! Every contribution matters — whether it's fixing a typo, reporting a bug, or implementing a new feature.

[简体中文版](docs/i18n/CONTRIBUTING.zh-CN.md) | [日本語版](docs/i18n/CONTRIBUTING.ja-JP.md) | [한국어](docs/i18n/CONTRIBUTING.ko-KR.md) | [Русский](docs/i18n/CONTRIBUTING.ru-RU.md)

## Code of Conduct

By participating in this project, you agree to maintain a respectful and inclusive environment. Please be kind and constructive in all interactions.

## Ways to Contribute

There are many ways to contribute beyond writing code:

- **Report bugs** — Found something broken? Open an issue with reproduction steps.
- **Suggest features** — Have an idea for improvement? You can start a conversation in [GitHub Discussions](https://github.com/alibaba/open-code-review/discussions/categories/ideas) or open a [Feature Request](https://github.com/alibaba/open-code-review/issues/new?template=feature_request.yml) issue.
- **Improve documentation** — Fix typos, clarify explanations, or add examples. You can also open a [Documentation Issue](https://github.com/alibaba/open-code-review/issues/new?template=docs_report.yml) to report problems.
- **Review pull requests** — Help us review code from other contributors.
- **Write code** — Fix bugs, add features, or improve performance.

## Getting Started

### Prerequisites

- [Go 1.26.9+](https://go.dev/dl/)
- [Git](https://git-scm.com/)
- [Make](https://www.gnu.org/software/make/)

### Setup

```bash
# 1. Fork the repository on GitHub

# 2. Clone your fork
git clone https://github.com/<your-username>/open-code-review.git
cd open-code-review

# 3. Add upstream remote (for syncing updates from the main repo)
git remote add upstream https://github.com/alibaba/open-code-review.git

# 4. Build the project
make build

# 5. Run tests
make test
```

If everything passes, you're ready to contribute.

> **Note:** The `upstream` remote is read-only for contributors — it is used to pull the latest changes from the main repository. You cannot push directly to upstream. All commits must be pushed to your fork (`origin`) and submitted via Pull Request.

### Line Endings

This project enforces LF line endings via `.gitattributes`. Configure Git to normalize line endings automatically:

```bash
git config core.autocrlf input
```

This ensures any CRLF is converted to LF on commit, preventing line-ending issues in CI.

## Development Workflow

### Branching

Create a feature branch from `main`:

```bash
git checkout main
git pull upstream main
git checkout -b feat/your-feature-name
```

Use prefixes to indicate the type of change:

| Prefix      | Purpose                               |
| ----------- | ------------------------------------- |
| `feat/`     | New feature                           |
| `fix/`      | Bug fix                               |
| `docs/`     | Documentation only                    |
| `refactor/` | Code refactoring (no behavior change) |
| `test/`     | Adding or updating tests              |
| `chore/`    | Build, CI, or tooling changes         |

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>(<scope>): <short summary>

[optional body]
```

Examples:

```
feat(agent): add support for custom tool definitions
fix(llm): handle timeout errors in Anthropic API calls
docs(README): update configuration examples
```

### License Headers

Every source file (`.go`, `.js`, `.mjs`, `.ts`, `.tsx`, `.kt`, `.kts`, `.sh`, `.py`, `.css`) must include an SPDX license header. After creating new files, run:

```bash
make license-add
```

This automatically adds the required header. CI will reject PRs with missing headers.

### Code Quality

Before submitting your changes, make sure they pass all checks:

```bash
# Format, lint, and verify license headers
make check

# Run tests with race detection
make test

# Build successfully
make build
```

### Project Structure

```
├── cmd/opencodereview/   # CLI entry point
├── internal/
│   ├── agent/            # Review agent logic
│   ├── config/           # Configuration management
│   ├── diff/             # Git diff parsing
│   ├── llm/              # LLM API client (Anthropic & OpenAI)
│   ├── model/            # Data models
│   ├── session/          # Review session management
│   ├── tool/             # Built-in tools (file_read, code_search, etc.)
│   ├── telemetry/        # OpenTelemetry integration
│   ├── verification/     # Evidence receipts and finding decisions
│   └── viewer/           # WebUI session viewer
├── pages/                # WebUI frontend
├── scripts/              # Build & install scripts
└── bin/                  # NPM wrapper
```

`internal/verification` contains the decision core. It does not run model requests or change the active review filter.
The caller supplies one captured `diff.EvidenceView`, a verification-context identity, and verifier provenance to `New`.
The context identity covers verifier inputs. It is separate from the captured code-state identity in each citation.

Use the core in this order:

1. Call `Capture` to assign stable candidate identifiers and retain each original `model.LlmComment`.
2. Call `Issue` with a repository path and inclusive line range.
   Deliver the returned citation and source to the verifier together.
   An unavailable source has an explicit reason and no citation.
3. Call `Submit` with decisions for candidate identifiers.
   Confirmation requires a trigger, failure path, consequence, and issued citation.
   Refutation requires a contradiction explanation and issued citation.
   The core checks citation integrity, not whether the cited code proves the claim.
4. Call `Finish`, `Fail`, `Exhaust`, or `Bypass` for undecided candidates.
   These operations accept optional candidate identifiers.
   Without identifiers, they apply to all undecided candidates.
   They preserve completed decisions.
5. Call `Result` for published findings and the separate versioned report.
   Publication omits only validated refuted claims.
   The report retains every candidate and its submitted decisions, including invalid or conflicting submissions.

Callers must serialize access to a core.
The core copies mutable inputs and outputs to protect stored claims, citations, and decisions.
It does not write sessions, admit resumed work, or connect to output surfaces.
Run its isolated Git fixtures with `make test PACKAGES=./internal/verification`.

`llmloop.Runner.RunVerification` provides an independent, bounded conversation for a supplied group and discovery round.
It does not replace the active review filter.
The caller supplies captured candidates, their core and shared mutex, the captured evidence view, group diffs, scope, rules, and background.
The caller retains its group concurrency slot and deadline.
The runner checks candidate identity and the core's evidence binding before it sends a request.
It never accepts discovery messages.

The dedicated dispatcher permits only captured file reads, search, file lookup, captured diff reads, and `verification_submit`.
It never resolves tools through the review registry or external configuration.
Source responses keep each receipt with its exact inclusive range and excerpt.
The dispatcher rejects citations that this conversation does not receive.
Limited or unavailable coverage cannot establish absence.

Each request uses the runner's provider, resolved model, completion cap, and shared usage counters.
Cancellation, the caller's deadline, prompt admission, tool rounds, and aggregate budget stop further requests.
Prompt admission includes verifier schemas and opaque provider history.
The operation adds no grace request, provider retry layer, or token reservation.
An interrupted investigation preserves accepted decisions and leaves undecided candidates incomplete and uncertain.

`VerifierInput.OnResult` receives validated snapshots after submissions and at the final disposition.
It runs outside the shared core mutex. A callback error stops the operation and preserves accepted decisions.
`VerifierResult` exposes the scoped result, disposition, reason, task key, conversation identity, and contract fingerprint.
`Runner.VerificationContract` returns the effective prompts, schemas, dispatch version, and deterministic fingerprint.
Callers include that fingerprint in `verification.ContextInputs.ContractFingerprint`.
The fingerprint excludes conversation and candidate identifiers.

Verifier request transcripts use `session.VerificationTask`.
Ordinary transcripts do not replace structured verification events, checkpoints, replay, or resume admission.
The session storage APIs below provide those records without active review integration.
Active filter replacement, bypass integration, CLI output, other presentation surfaces, scans, and delegation remain unchanged.
Run the runner fixtures with `make test PACKAGES=./internal/llmloop`.

### Durable verification sessions

`verification.NewContext` binds explicit verifier inputs to an existing captured evidence view.
`EvidenceView.State` exposes its sealed code-state identity without another repository capture.
The context includes fixed endpoints, captured state, ordered diffs, group context, scope, effective rules, background, verifier contracts, and verification mode.
It excludes candidate identifiers, session identifiers, provider names, and model names.
Provider and model admission remains separate.

Use the storage APIs in this order:

1. Call `Core.Capture`, then `SessionHistory.CaptureVerification` with the new records and their context.
   Wait for success before any verifier request.
   A successful write includes a flush to the existing JSONL stream.
2. Call `RecordVerificationSnapshot` with each scoped, validated result.
   This method can serve as `VerifierInput.OnResult`.
   It preserves candidates from other groups and earlier rounds.
3. Call `RecordVerificationTerminal` with the context and optional candidate identifiers.
   Terminal records can retain incomplete uncertainty after an interruption.
4. Call `RecordVerificationCheckpoint` for each file.
   The checkpoint binds the context's complete candidate inventory and exact terminal revisions.
   Revision digests bind the saved record contents.
   Publication does not define the inventory.
   A refutation-only checkpoint retains its complete report and an empty comment list.

The event schema is `ocr.verification/v1`, independent of the run manifest schema.
`LoadVerificationResult` and `ResumeState.VerificationResult` expose publication and the complete report before session finalization.
`VerificationContext` returns the saved context.
Malformed, unsupported, conflicting, or torn data prevents verification reuse.
Replay retains earlier intact audit records and reports invalid data through an error.
When replay detects invalid data, its publication retains recovered claims as incomplete uncertainty.
Invalid checkpoint bindings retain saved comments and recoverable file claims without verification.
A corrupt binding cannot select the recovery inventory.

`ResumeState.ReuseVerification` runs existing mode, input, repository, rules, provider, model, and manifest admission first.
Manifest coverage still decides whether a file checkpoint can participate.
The method then compares the full current context and validates accepted citations against the captured evidence view.
Its disposition distinguishes reusable decisions, valid candidates that require recheck, and fresh group review.
Later unfinished records cannot authorize reuse through an earlier terminal checkpoint.
Legacy checkpoints and bypass records remain unverified.
Workspace resume remains unsupported.

`Core.Restore` installs supported records atomically without recapture.
It preserves original claims, candidate identifiers, citation identifiers, and completed provenance.
It returns incomplete candidates separately.
Rejected submissions remain audit data, not accepted support.
Historical receipts do not count as evidence delivered to a new verifier conversation.
Call `RecordVerificationReuse` before the child's checkpoint to make its audit self-contained.

Verification writes and bound checkpoints return marshal, write, newline, and flush errors.
The session retains a persistence failure and returns it again from later checkpoints and finalization.
A final record also marks the failure when its writer remains available.
Affected checkpoints cannot claim verification reuse.
These APIs do not replace the active filter, orchestrate reviews, add CLI output, or change scans and delegation.
Run their fixtures with `make test PACKAGES='./internal/verification ./internal/session'`.


## AI-Assisted Development

Using AI to help with your development is perfectly welcome, and we are glad if it makes contributing easier. What we cannot accept is code that a model produced going straight into a commit unread, with none of its redundancy or mistakes cleaned up. Submissions like that slow down the review conversation and make a pull request harder to move forward.

If AI played a part in how you worked, please follow the rules below.

### Rules:

1. **You must disclose in your initial issue or pull request that you used AI/LLM, as well as the tools/models you used.**
2. You should understand every line of code written by AI and know what the AI did.
3. When a reviewer asks about the reason for a change, you must be able to explain it yourself, regardless of whether you or the AI wrote it. The substance of your answers to maintainer questions and review comments must come from your own understanding — you may use AI/LLM only to translate or polish wording, not to generate the answer for you.
4. Your PR should not contain repeated cycles like `AI generated -> fixed -> fixed -> fixed`. This may indicate that you did not review the AI-generated code, but instead let the AI fix issues as they arise, over and over.
5. You must review all code, text, and other content generated by AI/LLM yourself before proactively requesting a review from any member.
6. You must not attribute commits to AI/LLM, including through "Assisted-by", "Co-developed-by", or similar trailers.
7. Do not write overly long commit messages. Important information should go in the PR description rather than in collapsed commit messages.
8. If you are unwilling or unable to do all of the above, please close your issue or pull request.

Thanks!

## Contributing to Documentation

Documentation is a crucial part of OpenCodeReview. We welcome improvements to README files, inline code comments, configuration examples, and any user-facing text.

### What Counts as a Documentation Contribution

- Fixing typos, grammar errors, or broken links
- Clarifying confusing explanations or adding missing context
- Adding usage examples for commands or configuration options
- Updating outdated content (e.g., after a feature change)
- Translating or improving localized documentation (`docs/i18n/README.zh-CN.md`, `docs/i18n/README.ja-JP.md`, `docs/i18n/README.ko-KR.md`, `docs/i18n/README.ru-RU.md`, `docs/i18n/CONTRIBUTING.zh-CN.md`, `docs/i18n/CONTRIBUTING.ja-JP.md`, `docs/i18n/CONTRIBUTING.ko-KR.md`, `docs/i18n/CONTRIBUTING.ru-RU.md`)

### Documentation Workflow

1. If you spot an issue but don't plan to fix it yourself, open a [Documentation Issue](https://github.com/alibaba/open-code-review/issues/new?template=docs_report.yml).
2. If you'd like to fix it, fork the repo, make your changes, and submit a PR with the `docs/` branch prefix (e.g., `docs/fix-config-example`).
3. Documentation-only PRs don't require test changes, but please verify that any commands or code snippets you include are accurate.

### Documentation Files

| File                              | Purpose                              |
| --------------------------------- | ------------------------------------ |
| `README.md`                       | Main project documentation (English) |
| `docs/i18n/README.zh-CN.md`       | Chinese translation                  |
| `docs/i18n/README.ja-JP.md`       | Japanese translation                 |
| `docs/i18n/README.ko-KR.md`       | Korean translation                   |
| `docs/i18n/README.ru-RU.md`       | Russian translation                  |
| `CONTRIBUTING.md`                 | Contribution guide (English)         |
| `docs/i18n/CONTRIBUTING.zh-CN.md` | Contribution guide (Chinese)         |
| `docs/i18n/CONTRIBUTING.ja-JP.md` | Contribution guide (Japanese)        |
| `docs/i18n/CONTRIBUTING.ko-KR.md` | Contribution guide (Korean)          |
| `docs/i18n/CONTRIBUTING.ru-RU.md` | Contribution guide (Russian)         |

## Submitting Changes

### Opening an Issue

Before working on a significant change, please open an issue first to discuss the approach. This prevents duplicate work and ensures your contribution aligns with the project's direction.

When reporting a bug, include:

1. OpenCodeReview version (`ocr version`)
2. OS and architecture
3. Steps to reproduce
4. Expected vs. actual behavior
5. Relevant logs or error messages

### Pull Request Process

1. **Keep PRs focused** — One logical change per PR. If you have multiple independent changes, submit separate PRs.
2. **Write tests** — Add or update tests for any behavior changes.
3. **Update docs** — If your change affects user-facing behavior, update the relevant documentation.
4. **Sign the CLA** — All contributors must sign the Contributor License Agreement before their PR can be merged (see below).
5. **Fill in the PR template** — Describe what your change does and why.

### PR Title Format

Use the same Conventional Commits format as commit messages:

```
feat(agent): add support for custom tool definitions
```

### Review Process

- A maintainer will review your PR, usually within a few business days.
- We may request changes — this is normal and collaborative, not adversarial.
- Once approved, a maintainer will merge your PR.

## Tips for Faster PR Reviews

Want your PR to be reviewed and merged quickly? These practices help:

- **Sign the CLA early** — Many first-time contributors get blocked because they miss the CLA bot comment. Sign the Contributor License Agreement as soon as the bot prompts you — your PR cannot be merged without it.
- **Ensure all CI checks pass** — PRs with failing checks will not be reviewed. Run `make test` and `make build` locally before pushing to catch issues early.
- **Keep changes focused and small** — A PR that does one thing well is far easier to review than one that mixes unrelated changes. Smaller PRs get reviewed faster and are less likely to require multiple rounds of revision.
- **Write a clear, accurate description** — Explain *what* changed and *why*. The description must reflect the actual diff — reviewers lose trust when the two don't match. If the scope shifted during development, update the description before requesting review.
- **Include tests for behavior changes** — New features or bug fixes without tests raise questions. Tests demonstrate correctness and help reviewers understand the intended behavior.
- **Follow existing code patterns** — Match the style, naming conventions, and architecture of the surrounding code. Consistency reduces cognitive load for reviewers and avoids style-only review comments.
- **Respond to feedback promptly** — When a reviewer requests changes, address them quickly to keep the review cycle short. If you disagree, explain your reasoning rather than ignoring the comment.

## Contributor License Agreement (CLA)

We require all contributors to sign the Alibaba Open Source Contributor License Agreement before we can merge your contributions. This ensures that the project can be distributed under its license terms.

When you open your first PR, a CLA bot will post a comment with instructions. Simply follow the link to sign electronically — it only takes a minute.

## First-Time Contributors

New to the project? Look for issues labeled:

- [`good first issue`](https://github.com/alibaba/open-code-review/labels/good%20first%20issue) — Small, well-scoped tasks ideal for getting started.
- [`help wanted`](https://github.com/alibaba/open-code-review/labels/help%20wanted) — Issues where we'd appreciate community help.

Some good areas to start:

- Improving error messages and CLI output
- Writing tests for untested code paths
- Documentation improvements

## Community

- **Bug Reports** — [GitHub Issues](https://github.com/alibaba/open-code-review/issues)
- **Feature Suggestions** — [GitHub Discussions (Ideas)](https://github.com/alibaba/open-code-review/discussions/categories/ideas) or [Feature Request Issue](https://github.com/alibaba/open-code-review/issues/new?template=feature_request.yml)
- **Questions & Help** — If you have any questions about using OpenCodeReview, feel free to ask in [GitHub Discussions](https://github.com/alibaba/open-code-review/discussions)

## License

By contributing to OpenCodeReview, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
