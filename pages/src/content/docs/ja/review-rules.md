---
title: レビュールール
sidebar:
  order: 7
---

ルールは、各ファイルをレビューする際に OCR が**何に注目すべきか**を伝えます。ルールは 3 層の JSON ファイルに格納され、加えてバイナリに同梱される埋め込みのシステムデフォルトルールがあります。

## 優先順位チェーン

OCR は**4 層の優先順位チェーン**でルールを解決します。各ファイルパスについて、層を順に試し、最初に一致したパターンが有効になります。

| 優先順位  | 出所               | パス                                  | 説明                                                     |
| --------- | ------------------ | ------------------------------------- | -------------------------------------------------------- |
| 1（最高） | `--rule` 引数      | ユーザー指定                          | CLI による上書き。指定されている限り常に有効になります。 |
| 2         | プロジェクト設定   | Git リポジトリのルートと親ディレクトリの `.opencodereview/rule.json` | 近い順。継承は明示的に有効化します。 |
| 3         | グローバル設定     | `~/.opencodereview/rule.json`         | ユーザーレベルの好み。                                   |
| 4（最低） | システムデフォルト | 埋め込み `system_rules.json`          | 一般的な言語をカバーする組み込みルール。                 |

OCR は Git リポジトリのルートからファイルシステムのルートまでプロジェクト設定を読み込みます。
既定では最も近い設定だけを使用します。一致がない場合、上位のプロジェクト設定を飛ばしてグローバル層を試します。
明示的な継承で親ディレクトリの層を有効にします。無効な設定はエラーを返します。

ホームディレクトリが親でも、その設定はグローバル層として一度だけ読み込みます。

> **信頼境界:** OCR は親ディレクトリの設定の所有者や書き込み権限を確認しません。
> `/tmp` などの共有ディレクトリの設定は、レビュー規則やファイルフィルターを変更できます。
> プロジェクトのルール参照は、設定を提供する親ディレクトリ内の許可されたファイルを読み取れます。
> ホームより上位の設定は、グローバル設定より優先されます。
> 無効または読み取り不能な親設定は、参加しない場合でもレビューを停止できます。
> 親設定の検索を無効にするオプションはありません。

ルールと include/exclude のパターンは、引き続きリポジトリのルートからの相対パスに一致します。
ルールファイルの相対参照は `.opencodereview` を含むディレクトリから解決します。
シンボリックリンクの解決後も、設定ファイルとルール参照はそのディレクトリ内にある必要があります。

プロジェクト設定がない場合、OCR はグローバル層とシステム層を使用します。`--rule` の優先順位は最も高いままです。

システム層は**常に**存在するため（バイナリに同梱）、必ず*何らかの*ルールが解決されます。

## ルールファイル形式（層 1〜3） {#rule-file-format-layers-1-3}

```json
{
  "include": ["src/**/*.{ts,tsx}", "src/**/*.go"],
  "exclude": ["**/*.test.ts", "**/generated/**"],
  "rules": [
    {
      "path": "src/api/**/*.go",
      "rule": "All exported handlers must validate request bodies before use."
    },
    {
      "path": "**/*mapper*.xml",
      "rule": "Check SQL for injection risks, parameter errors, and missing closing tags."
    }
  ]
}
```

最上位のフィールド:

- `include`: 任意。組み込みのデフォルト除外パターン（テストファイルの除外。下記参照）を*バイパス*するための glob パターンです。ホワイトリストではありません。どの `include` パターンにも一致しないファイルも、依然として `unsupported_ext` と `default_path` のチェックを通過し、レビューされる可能性があります。
- `exclude`: 任意。OCR がレビューしないファイルの glob パターンです。ユーザー設定のフィルターの中で最も優先されます。
- `rules`: `{path, rule}` エントリの配列で、**宣言順**に評価されます。そのファイルに最初に一致した `path` glob のエントリが、OCR がモデルに送る prompt を決定します。
- `filter_mode`: 任意。`"merge"` は下位フィルターを継承し、`"replace"` は継承を停止します。
  [ユーザー層の継承](#user-layer-inheritance)を参照してください。

各 `rules` エントリは、次の任意フィールドも受け付けます:

- `merge_system_rule` — 任意。既定値は `false`。`false`（既定）の場合、一致したエントリはそのファイルの組み込みシステムルールを**置き換え**ます。`true` の場合、一致したシステムルールが保持され、ユーザールールと**結合**されます。
- `merge_parent_rules` — 任意。既定値は `false`。`true` の場合、優先順位が低いユーザー層の一致したルールを継承します。

```json
{
  "rules": [
    {
      "path": "**/*",
      "rule": "Security review: flag hardcoded secrets, unvalidated redirects, and missing authz checks.",
      "merge_system_rule": true
    }
  ]
}
```

このエントリを置くと、`ocr rules check src/main/java/com/example/UserService.java`
は両方の半分を報告します:

```
$ ocr rules check src/main/java/com/example/UserService.java
Source: Project (.opencodereview/rule.json)
Pattern: **/*
Rule:
────────────────────────────────────────
## System-Specific Rules (Mandatory)

…contents of java.md…

---

## User-Specific Rules (Mandatory)

Security review: flag hardcoded secrets, unvalidated redirects, and missing authz checks.
────────────────────────────────────────
```

マージがどう組み立てられるかについて、知っておく価値のある点が 2 つあります:

- **システム側の半分はファイルごとに解決**されます（実行ごとに一度ではありません）。上のエントリは包括的なものですが、`.java` ファイルでは `java.md`、`.py` ファイルでは `python.md`、OCR が認識しない拡張子では `default.md` が使われます。したがって 1 つのエントリで、拡張子を列挙することなく、あらゆる場所で適切な言語ルールの上にあなたのルールを追加できます。
- どちらの半分も空になりえます。ファイルに対してシステム層が空に解決された場合、あなたのルールだけが得られます。あなたのルールのテキストが空の場合、システムルールだけが得られます。どちらの場合も、残った半分だけが使われ、もう半分が何らかのプレースホルダーで埋められることはありません。

すべてのユーザー層が `merge_system_rule` を受け付けます。`merge_parent_rules` がなければ、
最初に一致したユーザー層が下位のユーザー層を置き換えます。各設定では最初に一致したエントリだけを使用します。
`merge_system_rule` はユーザー層を結合しません。

### 1 つのルールに複数のソース {#multiple-sources-for-one-rule}

`rule` は文字列または文字列配列を受け付けます。配列では本文とファイル参照を混在できます:

```json
{
  "rules": [
    {
      "path": "**/*.go",
      "rule": [
        "review/account-isolation.md",
        "review/retry-safety.md",
        "Check field permissions."
      ],
      "merge_parent_rules": true,
      "merge_system_rule": true
    }
  ]
}
```

OCR は各要素を個別に解決し、配列順に本文を結合します。
1 つの配列で同じパスに複数の確認を指定できます。同じ設定の複数の一致エントリでは、最初だけを使用します。

- ファイル参照には文字列値と同じ判定と安全確認を適用します。
  拡張子は `.md`、`.txt`、`.markdown`、サイズは各ファイル 512 KB 以下です。
- プロジェクト参照は `.opencodereview` を含むディレクトリから解決します。
  グローバルと `--rule` の参照は JSON ファイルのディレクトリから解決します。
- 存在しない参照や安全でない参照は警告を出します。OCR はそれらを除き、有効なソースを保持します。
- 空要素と空白だけの要素は飛ばします。
- 空配列や有効な本文のない配列では、次のエントリを試します。
  ただし `merge_parent_rules` または `merge_system_rule` が有効なら、そのエントリが参加します。
- 要素は文字列だけです。数値、オブジェクト、入れ子の配列、`null` 要素は設定エラーを返します。
- 親層の継承でも各配列の内部順序を維持します。組み込みルールは最大一度だけ追加します。


### ユーザー層の継承 {#user-layer-inheritance}

ルール本文とファイルフィルターは別々に継承を制御します:

```json
{
  "filter_mode": "merge",
  "exclude": ["**/generated/**"],
  "rules": [
    {
      "path": "**/*",
      "rule": "Check account isolation and record permissions.",
      "merge_parent_rules": true,
      "merge_system_rule": true
    }
  ]
}
```

**ルール本文**

- 一致したエントリの `merge_parent_rules: true` は後続の下位ユーザー層を有効にします。
- 継承中は一致しない層を飛ばします。一致した親エントリのフラグが継承の継続を決めます。
  `merge_parent_rules: false` で継承を停止します。
- 空でない本文を優先順位の低い順に結合します。
- 参加エントリのどれかが `merge_system_rule` を有効にすると、組み込みルールを一度だけ追加します。
  `merge_parent_rules` だけでは組み込みルールを追加しません。
- 空のエントリも継承できます。継承本文がなく、`merge_system_rule` も有効でなければ、本文は空です。
- `ocr rules check` は優先順位が最も高い一致エントリの出所とパターンを表示します。
- 例えば `~/Projects/gearheart` 配下のリポジトリでは、順序は `--rule`、リポジトリ設定、Gearheart 設定、その他の親設定、グローバル設定です。
  グローバル、Gearheart、リポジトリの本文を結合するには、両方のプロジェクトエントリで `merge_parent_rules` を有効にします。

**ファイルフィルター**

- 任意の最上位フィールド `filter_mode` はフィルター継承だけを制御します。
- 省略時は、空でないフィルターリストを持つ最初の層が下位フィルターを置き換えます。
  空の既定設定は、最も近いプロジェクト設定からグローバル層へ進みます。
- `"merge"` は現在のリストを追加し、下位層を有効にします。空の merge 設定でも継承します。
- 継承中は空の既定設定を飛ばします。リストを持つ既定設定または明示的な `"replace"` で停止します。
- 空リストの明示的な `"replace"` は継承したユーザーフィルターを消去します。
- パターンは小文字に変換し、各リストの重複を除きます。除外は包含に優先します。
  組み込みの機密パス除外は常に有効です。
- `"merge"`、`"replace"`、空文字列以外の値はエラーを返します。

### glob の機能

OCR は [`bmatcuk/doublestar/v4`](https://pkg.go.dev/github.com/bmatcuk/doublestar/v4) でマッチングを行います:

- `*`: `/` 以外の任意の文字に一致します。
- `**`: ディレクトリ境界をまたいで一致します（`src/**/*.go` は任意の深さをカバー）。
- `{a,b,c}`: 波括弧の展開。`*.{ts,tsx,js,jsx}` は 4 つのパターンに展開され、順に一致が試されます。
- `?`: 単一の文字に一致します。
- `[abc]`: 文字クラス。

> パターンマッチングは**大文字小文字を区別しません**（マッチング前にファイルパスは小文字化されます）。確信が持てないときは `ocr rules check <path>` で確認してください。

## ファイルがどのようにフィルタリングされるか

フィルタリングは 6 段階のゲートアルゴリズムで、[`internal/agent/selection.go`](https://github.com/alibaba/open-code-review/blob/main/internal/agent/selection.go) にあります。各 diff について、OCR は順に次を問います:

1. **`binary`**: ファイルはバイナリか？ 除外します。
2. **`secret_exclude`**: 古いパスまたは新しいパスが組み込みのシークレットパス保護の対象か？ 対象なら除外します。この保護はユーザールールより先に適用され、`include` パターンでは上書きできません。パターンの一覧は下記の[組み込みのシークレットパス](#built-in-secret-paths)にあります。

3. **`user_exclude`**: パスがいずれかのユーザー `exclude` パターンに一致するか？ 除外します。
4. **`user_include`**: ユーザーが `include` を定義している場合、パスは一致するか？ 一致するなら**即座に保持**します（下記の `unsupported_ext` と `default_path` のゲートをバイパス）。
5. **`unsupported_ext`**: ファイルの拡張子は[ホワイトリスト](https://github.com/alibaba/open-code-review/blob/main/internal/config/allowlist/supported_file_types.json)にあるか？ なければ除外します。
6. **`default_path`**: パスがいずれかの組み込みテストファイル除外パターン（`**/*_test.go`、`**/*.test.{js,jsx,ts,tsx}`、`**/*_spec.rb`……）に一致するか？ 除外します。

6 つのゲートをすべて通過したファイルだけが LLM に送られます。ただし diff だけで `max_tokens` の 80% を超える場合は例外で、`selectFiles` がゲートのあとにその上限を適用し、そのファイルを `too_large` として除外します。同じく、新しいパスが `/dev/null` であるファイルは `deleted` と記されます。レビューすべき新しい内容がありません。`ocr review --preview` を使えば、token を消費せずにこのフィルタリング結果を出力できます。

### 組み込みのシークレットパス {#built-in-secret-paths}

組み込みのシークレットパスはレビューされません（
[`internal/config/allowlist/default_secret_patterns.json`](https://github.com/alibaba/open-code-review/blob/main/internal/config/allowlist/default_secret_patterns.json)
を参照）:

- `**/.ssh/**`
- `**/id_rsa`
- `**/id_dsa`
- `**/id_ecdsa`
- `**/id_ed25519`
- `**/.netrc`
- `**/_netrc`
- `**/.npmrc`
- `**/.pypirc`
- `**/.dockercfg`

また、`.env` と `.env.*` の各変種（`.env.example`、`.env.sample`、`.env.template` を除く）もシークレットパスとして扱われます。

### デフォルトパスの除外

組み込みの除外リスト（[`internal/config/allowlist/default_exclude_patterns.json`](https://github.com/alibaba/open-code-review/blob/main/internal/config/allowlist/default_exclude_patterns.json) を参照）は 2 つのグループを対象とします。1 つ目は各言語のテストファイルに加えて、テスト fixture、スナップショット、生成コードです:

- `**/*_test.go`
- `**/src/test/java/**/*.java`
- `**/src/test/**/*.{kt,kts}`
- `**/*Test.fs`
- `**/*.test.{js,jsx,ts,tsx}`
- `**/*.spec.{js,jsx,ts,tsx}`
- `**/__tests__/**`
- `**/test/**/*_test.py`
- `**/tests/**/*_test.py`
- `**/*_test.py`
- `**/test_*.py`
- `**/*_spec.rb`
- `**/spec/**/*_spec.rb`
- `**/*Test.java`
- `**/*Tests.java`
- `**/*_test.rs`
- `**/oh_modules/**`
- `**/*.test.ets`
- `**/test/**/*.jl`
- `**/test/**/*.hs`
- `**/*Spec.hs`
- `**/test/**/*.lhs`
- `**/*Spec.lhs`
- `**/tests/**/*.nim`
- `**/tests/**/*.R`
- `**/__snapshots__/**`
- `**/*.snap`
- `**/testdata/**`
- `**/fixtures/**`
- `**/.ipynb_checkpoints/**`
- `**/*.generated.*`
- `**/*.gen.go`
- `**/*.pb.go`
- `**/*.pb.cc`
- `**/*.pb.h`
- `**/*Test.swift`
- `**/*Tests.swift`
- `**/Tests/**/*.swift`
- `**/tests/**/*.elm`
- `**/vendor/**/*.{jsonnet,libsonnet}`
- `**/test/**/*.zig`
- `**/*_test.zig`
- `**/kitex_gen/**/*.go`
- `**/*.capnp.h`
- `**/*.capnp.go`
- `**/*.capnp.ts`
- `**/*_capnp.rs`
- `**/*_capnp.py`
- `**/test/**/*.ml`
- `**/tb_*.{v,sv,vhd,vhdl}`
- `**/*_tb.{v,sv,vhd,vhdl}`
- `lib/**/*.sol`
- `**/*.t.sol`
- `**/test/**/*.sol`
- `**/tests/**/*.sol`
- `**/test/**/*.vy`
- `**/tests/**/*.vy`

……および依存関係とビルド出力のディレクトリ:

- `**/node_modules/**`
- `**/bower_components/**`
- `**/vendor/**`
- `**/target/**`
- `**/dist/**`
- `**/__pycache__/**`、`**/.venv/**`、`**/site-packages/**`
- `**/Pods/**`、`**/Carthage/**`
- `**/.next/**`、`**/.nuxt/**`、`**/.gradle/**`、`**/.terraform/**`……

`**/build/**` と `**/bin/**` は意図的に含めていません。手書きのソースをそこに置くプロジェクトが多いためです。

同じディレクトリは、より早い [`internal/diff/git.go`](https://github.com/alibaba/open-code-review/blob/main/internal/diff/git.go) の diff 層でもフィルタリングされます。このリストはパスの接頭辞で照合するため、**リポジトリルート**のディレクトリしか捕捉しません。`vendor/pkg/x.go` はファイルごとのフィルタに届かず `provider_directory` として報告され、`api/vendor/pkg/x.go` は届いて `default_path` で除外されます。`include` ルールで戻せるのは後者だけです。

上記いずれかのパターンに一致するファイルを**レビューする**には、それをユーザー `include` リストに追加してください。それが default-path ゲートを上書きします。

## ファイルごとのルール解決

フィルタリングによってあるファイルが*レビューされる*と決まったあと、OCR は agent が従うべきルールテキストを選びます:

1. 宣言順に `--rule`（custom）層を試します。
2. 宣言順に `<repo>/.opencodereview/rule.json` を試します。
3. 宣言順に `~/.opencodereview/rule.json` を試します。
4. 埋め込みのシステムルール層にフォールバックします。

埋め込みの `system_rules.json` から主なパターンを相対的なマッチ順で示します:

| パターン                            | ルールドキュメント                                                                              |
| ----------------------------------- | ----------------------------------------------------------------------------------------------- |
| `**/*.properties`                   | `properties.md`: i18n / 設定ファイル。                                                          |
| `**/*{mapper,dao}*.xml`             | `mapper_dao_xml.md`: MyBatis 形式の mapper SQL。                                                |
| `**/pom.xml`                        | `pom_xml.md`: Maven 依存関係。                                                                  |
| `**/build.gradle`                   | `build_gradle.md`: Gradle 依存関係。                                                            |
| `**/package.json`                   | `package_json.md`: NPM 依存関係 / スクリプト。                                                  |
| `**/Cargo.toml`                     | `cargo_toml.md`: Rust manifest。                                                                |
| `**/composer.json`                  | `composer_json.md`: Composer の依存関係、自動読み込み、スクリプト、プラグイン、パッケージ設定。 |
| `**/*.{json,json5}`                 | `json.md`: 汎用 JSON（`.json5` にも一致）。                                                     |
| `.github/workflows/**/*.{yaml,yml}` | `github_workflows.md`: GitHub Actions ワークフロー YAML。                                       |
| `.github/**/*.{yaml,yml}`           | `github_config.md`: その他の `.github` 設定 YAML。                                              |
| `**/*.{yaml,yml}`                   | `yaml.md`                                                                                       |
| `**/*.java`                         | `java.md`                                                                                       |
| `**/*.go`                           | `go.md`: Go ソースコード。                                                                      |
| `**/*.{ftl,ftlh,ftlx}`              | `freemarker.md`: FreeMarker テンプレート（SSTI / XSS / null 処理）。                            |
| `**/*.{hbs,mustache}`               | `handlebars_mustache.md`: Handlebars / Mustache テンプレート。                                  |
| `**/*.{jinja2,j2}`                  | `jinja.md`: Jinja テンプレート                                                                 |
| `**/*.ets`                          | `arkts.md`: ArkTS / HarmonyOS。                                                                 |
| `**/*.astro`                        | `astro.md`: Astro コンポーネントと islands。                                                    |
| `**/*.{ts,js,tsx,jsx,mjs,cjs}`      | `ts_js_tsx_jsx.md`                                                                              |
| `**/*.{kt,kts}`                     | `kotlin.md`                                                                                     |
| `**/*.{fs,fsi,fsx}`                 | `fsharp.md`: F# の実装、シグネチャ、スクリプトファイル。                                                            |
| `**/*.rs`                           | `rust.md`                                                                                       |
| `**/*.R`                            | `r.md`                                                                                          |
| `**/*.{cpp,cc,cxx,hpp,hxx}`         | `cpp.md`                                                                                        |
| `**/*.c`                            | `c.md`                                                                                          |
| `**/*.{py,pyi,ipynb}`               | `python.md`: Python ソースコード。                                                              |
| `**/*.{php,phtml}`                  | `php.md`: PHP ソースと PHP テンプレート。                                                       |
| `**/*.proto`                        | `protobuf.md`: Protocol Buffers のワイヤ互換性。                                                |
| `**/*.po`                           | `po.md`: gettext 翻訳ソースカタログ。                                                           |
| `**/*.pot`                          | `pot.md`: gettext テンプレートファイル。                                                        |
| `**/*.{graphql,gql}`                | `graphql.md`: GraphQL スキーマと操作。                                                          |
| `**/*.prisma`                       | `prisma.md`: Prisma スキーマ。                                                                  |
| `**/*.jl`                           | `julia.md`: Julia ソースコード。                                                                |
| `**/*.{tf,hcl,tfvars}`              | `terraform.md`: Terraform / HCL。                                                               |
| `**/*.bicep`                        | `bicep.md`: Bicep（Azure）テンプレート。                                                        |
| `**/*.elm`                          | `elm.md` - Elm ソースコード。                                                                   |
| `**/*.{jsonnet,libsonnet}`          | `jsonnet.md`: Jsonnet の設定テンプレートとライブラリ。                                          |
| `**/*.thrift`                       | `thrift.md`: Apache Thrift IDL のワイヤ互換性。                                                 |
| `**/*.capnp`                        | `capnp.md`: Cap'n Proto スキーマのワイヤ互換性。                                                |
| `**/*.{v,sv,vh}`                    | `verilog.md`: Verilog および SystemVerilog の RTL。                                             |
| `**/*.{vhd,vhdl}`                   | `vhdl.md`: VHDL の RTL。                                                                        |
| `**/*.m`                            | `matlab.md`（または[コンテンツスニッフィング](#content-sniffing-for-m-files)により `objc.md`）  |
| `**/*.mm`                           | `objc.md`: Objective-C++ ソースコード。                                                         |
| `**/*.sol`                          | `solidity.md`: Solidity スマートコントラクト。                                                  |
| `**/*.vy`                           | `vyper.md`: Vyper スマートコントラクト。                                                        |
| `**/*.rego`                         | `rego.md`: Rego ポリシー（OPA）。                                                               |
| _(fallback)_                        | `default.md`                                                                                    |

解決されたルール本文は、plan および main task prompt 内の `{{system_rule}}` プレースホルダーの内容になります。

### `.m` ファイルのコンテンツスニッフィング {#content-sniffing-for-m-files}

`.m` は MATLAB と Objective-C で共有されています。OCR はファイルの先頭の空でない
行を覗き見して区別します: Objective-C らしい内容（例: `#import`、
`@implementation`、C スタイルコメント）であれば `matlab.md` の代わりに `objc.md`
を使用します。コンテンツを読み取れない場合は `matlab.md` にフォールバックします。

> **安定性に関する注意。** スニッフィングのヒューリスティックは OCR のバージョン間
> で変更される可能性があります。確定的な `.m` ルーティングが必要な場合は、`.m`
> パスに明示的なプロジェクトレベルのルールを設定してください——プロジェクトルールは
> 常にシステム層より優先されます。

## どのルールが有効かを確認する: `ocr rules check`

```bash
$ ocr rules check src/main/java/com/example/UserService.java
File: src/main/java/com/example/UserService.java
Source: System built-in
Pattern: **/*.java
Rule:
────────────────────────────────────────
…contents of java.md…
────────────────────────────────────────
```

```bash
$ ocr rules check --rule custom.json src/main/resources/mapper/UserMapper.xml
File: src/main/resources/mapper/UserMapper.xml
Source: Custom (--rule)
Pattern: **/*mapper*.xml
Rule:
────────────────────────────────────────
…contents of your custom rule…
────────────────────────────────────────
```

あるルールが期待どおりに有効にならないときに使用してください。有効な**層**と**パターン**を表示します。

## レシピ

### プロジェクトレベル: コーディング規約を強制する

`<repo>/.opencodereview/rule.json` として保存し、commit します:

```json
{
  "rules": [
    {
      "path": "src/api/**/*.go",
      "rule": "Every public handler must `defer tx.Rollback()` immediately after starting a transaction."
    },
    {
      "path": "**/*mapper*.xml",
      "rule": "Check SQL for injection risks, missing parameter binding, and unclosed XML tags."
    }
  ]
}
```

### プロジェクトレベル: 生成コードをスキップし、src に集中する

```json
{
  "include": ["src/**/*.{ts,tsx,js,jsx}"],
  "exclude": ["**/*.gen.ts", "**/generated/**"]
}
```

`include` を設定すると、`src/` 内のファイルは、本来は組み込みのデフォルト除外パターン（テストファイルなど）で除外されるものであっても保持されます。`src/` 以外のファイルは依然として通常の ext / default チェックを通ります。`include` はバイパスの仕組みであり、ホワイトリストではありません。

### PR ごとの上書き

```bash
ocr review --rule ./.review-rules-only-for-this-pr.json
```

プロジェクト層とグローバル層の両方を同時にバイパスします。単一の PR が完全に異なるレビューチェックリスト（例: セキュリティレビューのみ）を必要とするときに便利です。

### グローバルな個人設定

`~/.opencodereview/rule.json` に置くと、自分のマシン上のすべてのリポジトリが継承します:

```json
{
  "rules": [
    {
      "path": "**/*.{ts,tsx,js,jsx}",
      "rule": "Always check for unhandled promise rejections; warn on `// eslint-disable` without a reason comment."
    }
  ]
}
```

### 組み込みの言語ルールの上にグローバルなセキュリティルールを重ねる

包括的なユーザールールは、既定では組み込みの言語別ルールを**置き換え**ます。そのため `**/*` に 1 つエントリを追加すると、いたる所で `java.md`、`python.md` などが静かに失われます。`merge_system_rule` を設定すれば両方を維持できます:

```json
{
  "rules": [
    {
      "path": "**/*",
      "rule": "Security review: flag hardcoded secrets, unvalidated redirects, and missing authz checks.",
      "merge_system_rule": true
    }
  ]
}
```

すべてのリポジトリに適用するなら `~/.opencodereview/rule.json` に、1 つだけに適用するなら `<repo>/.opencodereview/rule.json` に保存してください。システム側の半分はファイルごとに解決されるため、各言語はあなたのルールに加えて引き続き自分自身の組み込みルールを得ます — 拡張子を列挙する必要はありません。

グローバルファイルは 3 つのユーザー層の中で**最も低い**層です。`--rule` またはプロジェクトの `.opencodereview/rule.json` に同じファイルに一致するエントリがあれば、そのエントリが勝ち、グローバルなエントリはまったく読み取られません — 包括的なエントリは 1 か所だけに置いてください。

## 関連項目

- [CLI リファレンス](../cli-reference/): `ocr review --rule`、`--preview`、`ocr rules check`。
- [設定](../configuration/): config ファイルの場所と階層的な解決チェーン。
- [アーキテクチャ](../architecture/): 解決されたルールがどのように agent prompt に供給されるか。
