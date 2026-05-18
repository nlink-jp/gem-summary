# gem-summary

> **ステータス:** Scaffolding (RFP [docs/ja/gem-summary-rfp.ja.md](docs/ja/gem-summary-rfp.ja.md) の Phase 2)。
> 要約処理本体は未配線 — CLI は stderr に通知メッセージを出すのみ。
> Phase 1 / 2 / 3 の進捗は [CHANGELOG](CHANGELOG.md) を参照。

`nlink-jp` util-series の単一機能テキスト要約 CLI。`.md` / `.txt` ファイル
(または stdin) を読み、Vertex AI Gemini に対する **1 回の LLM call**
で要約を stdout に出力する。モデルの実効コンテキスト窓を超える巨大入力
についてのみ、自動的に並列 chunk-merge にフォールバック。

shell-agent-v2 内蔵の `analyze-text` (sliding window で複数 LLM call を
消費) より軽量で、通常の要約依頼にはこちらが適切。

## なぜ gem-summary か

- **analyze-text より低コスト** — 短〜中サイズ文書は 1 LLM call で完了
- **コンテキスト窓 ~1M token に収まる** ほとんどのケースで十分
- **chunking は必要時のみ** — ~400k token 超の入力で初めて並列 chunk-merge
  パスに切替。silent reject はしない
- **パイプ親和** — stdin / stdout / `--json` で util-series の他ツールと
  組合せ可能 (`gem-transcribe` → `gem-summary` 等)

## インストール

```sh
# ソースから
git clone https://github.com/nlink-jp/gem-summary
cd gem-summary
make build      # → dist/gem-summary
```

リリース後は GitHub Releases から Linux / macOS / Windows 用の
プリビルドバイナリを取得可能。

## 設定

`gem-summary` は `~/.config/gem-summary/config.toml` を読む。
[`config.example.toml`](config.example.toml) をコピーして編集、または
下表の環境変数で個別フィールドを上書き。

```toml
[gcp]
project  = "your-project-id"
location = "us-central1"

[model]
name = "gemini-2.5-flash"

[summary]
default_style     = "medium"
chunk_threshold   = 400000
chunk_size        = 200000
chunk_overlap     = 2000
chunk_parallelism = 3
output_reserve    = 4096
request_timeout   = 180
```

### 環境変数 override

優先順位: `GEMSUMMARY_*` env > `GOOGLE_CLOUD_*` env > config file > 内蔵 default。

| 変数                            | 上書き対象                  |
|--------------------------------|----------------------------|
| `GEMSUMMARY_PROJECT`           | `[gcp].project`            |
| `GEMSUMMARY_LOCATION`          | `[gcp].location`           |
| `GEMSUMMARY_MODEL`             | `[model].name`             |
| `GEMSUMMARY_DEFAULT_STYLE`     | `[summary].default_style`  |
| `GEMSUMMARY_CHUNK_THRESHOLD`   | `[summary].chunk_threshold`|
| `GEMSUMMARY_CHUNK_SIZE`        | `[summary].chunk_size`     |
| `GEMSUMMARY_CHUNK_OVERLAP`     | `[summary].chunk_overlap`  |
| `GEMSUMMARY_CHUNK_PARALLELISM` | `[summary].chunk_parallelism`|
| `GEMSUMMARY_REQUEST_TIMEOUT`   | `[summary].request_timeout`|

`GOOGLE_CLOUD_PROJECT` / `GOOGLE_CLOUD_LOCATION` を最終 fallback として
受け付け、既存 GCP 環境のシェルで追加設定なしで動く。

### 認証

gem-summary は [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials)
を使う:

```sh
gcloud auth application-default login
```

対象プロジェクトで Vertex AI API を有効化し、実行プリンシパルに
**Vertex AI User** ロール (`roles/aiplatform.user`) を付与。

## 使い方

```sh
gem-summary path/to/notes.md             # config の default style で
gem-summary --style short notes.md       # 1-3 文の短い要約
cat notes.md | gem-summary --style long  # 詳細な複数段落要約
gem-summary --json notes.md              # 構造化 JSON 出力
```

### フラグ

| Flag                  | 用途                                                 |
|-----------------------|------------------------------------------------------|
| `--style`             | `short` / `medium` / `long` (default: config から)   |
| `--lang`              | 出力言語 (例: `ja`, `en`)。Default: 入力から自動検出  |
| `--model`             | model 名を上書き                                      |
| `--max-input-tokens`  | 入力サイズ上限                                        |
| `--chunk-size`        | chunking 発動時の 1 chunk あたり token 数             |
| `--json`              | 構造化 JSON 出力                                     |
| `--quiet` / `-q`      | stderr 進捗を抑制                                    |
| `--config` / `-c`     | config ファイルパスを上書き                          |
| `--version`           | バージョン表示                                        |

## shell-agent-v2 との連携

shell-agent-v2 のツールディレクトリ (`examples/shell_tools/`) に
`summary.sh` ラッパーをコピー。スクリプトの `@description:` field
が「analyze-text より gem-summary を優先すべきケース」を agent に
伝える設計 — shell-agent-v2 の内蔵 prompt は変更不要、疎結合な統合。

## ドキュメント

- [`docs/ja/gem-summary-rfp.ja.md`](docs/ja/gem-summary-rfp.ja.md) — 設計 RFP (primary)
- [`docs/en/gem-summary-rfp.md`](docs/en/gem-summary-rfp.md) — 同英語版
- [`AGENTS.md`](AGENTS.md) — contributor onboarding (build / test / structure)
- [`CHANGELOG.md`](CHANGELOG.md) — リリースノート

## ライセンス

MIT — [LICENSE](LICENSE) 参照。
