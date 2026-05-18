# RFP: gem-summary

> Generated: 2026-05-18
> Status: Approved (2026-05-18)

## 1. Problem Statement

shell-agent-v2 で `.md` / `.txt` の単純な要約依頼を投げると、LLM は
built-in tool `analyze-text` を選び、sliding-window 要約で 3-5 回の
LLM call を消費してしまう。Vertex AI Gemini の数十万 token のコン
テキスト窓があれば、500 行クラスのドキュメントは 1 call で十分要約
できるにもかかわらず、高コストな経路が取られている。

**対象ユーザー**: shell-agent-v2 を使う日常開発者を主軸に、同種の
要約 wrapper を chatops-series / cybersecurity-series 等の別ツール
からパイプで呼びたいケースも含む。

**解決策**: Vertex AI Gemini に対する単一 LLM call で要約を返す軽量
CLI。stdin またはファイルパスを入力、stdout に要約を出力。コンテキ
スト窓を超える巨大入力に対してのみ自動的に chunking + 並列 + merge
で対応する。shell-agent-v2 からは `examples/shell_tools/` 配下の
シェルスクリプト経由で呼ぶ。

---

## 2. Functional Specification

### Commands / API Surface

```
gem-summary [flags] [FILE]

FILE を省略または "-" にすると stdin から読み込む。

Flags:
  --style SHORT|MEDIUM|LONG  出力長プリセット (default: MEDIUM)
  --lang LANG                出力言語強制指定 (default: auto-detect from input)
  --model MODEL              config の model を上書き
  --max-input-tokens N       入力サイズの hard cap (default: from config)
  --chunk-size N             fallback chunking 時のウィンドウサイズ
                             (default: 200000 tokens)
  --json                     構造化 JSON 出力
  --quiet                    stderr 進捗を抑制
  --version
  --help
```

### Input / Output

- **Input**: plain text (`.md` / `.txt` / stdin パイプ)。バイナリは
  弾く。
- **Output (default)**: 要約テキストのみを stdout。stderr に進捗
  (chunk 数 / 各 chunk 所要時間 / トークン消費等) を表示、`--quiet`
  で抑制可能。
- **Output (--json)**: 構造化 JSON:
  ```json
  {
    "summary": "…",
    "chunks": 3,
    "tokens_in": 12345,
    "tokens_out": 312,
    "duration_seconds": 18.4
  }
  ```

### Configuration

`~/.config/gem-summary/config.toml` (既存 gem-* 群統一スキーマ):

```toml
[gcp]
project  = "your-project-id"
location = "us-central1"

[model]
name = "gemini-2.5-flash"

[summary]
default_style       = "medium"
chunk_threshold     = 400000  # 入力 token > これで chunking 発動
chunk_size          = 200000  # 1 chunk あたり token 数
chunk_overlap       = 2000    # 隣接 chunk の overlap (文脈ブリッジ)
chunk_parallelism   = 3       # 並列度固定
output_reserve      = 4096
request_timeout     = 180
```

**セクション名統一**: `[gcp]` (project / location) + `[model]`
(name) + tool 固有 `[summary]` の 3 セクション構成は、gem-search /
gem-image / gem-query / gem-rag / gem-transcribe と同一。

**環境変数 override** (既存 gem-* と同パターン):

| 環境変数 | 上書き対象 |
|---------|----------|
| `GEMSUMMARY_PROJECT` | `[gcp].project` |
| `GEMSUMMARY_LOCATION` | `[gcp].location` |
| `GEMSUMMARY_MODEL` | `[model].name` |
| `GEMSUMMARY_DEFAULT_STYLE` | `[summary].default_style` |
| `GEMSUMMARY_CHUNK_THRESHOLD` | `[summary].chunk_threshold` |
| `GEMSUMMARY_CHUNK_SIZE` | `[summary].chunk_size` |
| `GEMSUMMARY_CHUNK_OVERLAP` | `[summary].chunk_overlap` |
| `GEMSUMMARY_CHUNK_PARALLELISM` | `[summary].chunk_parallelism` |
| `GEMSUMMARY_REQUEST_TIMEOUT` | `[summary].request_timeout` |

GCP 標準 env (`GOOGLE_CLOUD_PROJECT` / `GOOGLE_CLOUD_LOCATION`) を
最終 fallback として認識 (gem-search / gem-image と同じ動作)。

優先順位: `GEMSUMMARY_*` > `GOOGLE_CLOUD_*` > config.toml >
内蔵 default。

Go 実装: `BurntSushi/toml` + 明示的な `os.Getenv` (gem-search
`config.go:73-86` のパターンを踏襲、memory: Vertex AI config.toml
unified)。

### External Dependencies

- Vertex AI Gemini API (`google.golang.org/genai` SDK)
- Google Cloud Application Default Credentials (`gcloud auth
  application-default login`)
- nlk (Go util) — `guard` (prompt-injection 防御)、`backoff`
  (Vertex AI retry)、`jsonfix` (`--json` 出力時)

### Chunk-and-merge アルゴリズム (内部 fallback)

1. 入力を token 概算 (char/4 と word count の max)
2. ≤ `chunk_threshold` (default 400k) → 1 call で要約 → 返却
3. > `chunk_threshold` → `chunk_size` で分割 (overlap 付き) → 並列
   `chunk_parallelism` (固定 3) で各 chunk を要約 → 結果を merge
   prompt で再要約 → 返却
4. merge 結果が再度上限超なら再帰 (実用上 2 段で 100MB+ 級が処理
   可能)

---

## 3. Design Decisions

### 言語 / フレームワーク

- **Go** (既存 gem-search / gem-image / gem-query / gem-transcribe
  と一貫)
- SDK: `google.golang.org/genai` (vertexai/genai は 2025-06
  deprecated 済、memory: genai_go_sdk)
- Config: `BurntSushi/toml` + env override (memory: Vertex AI
  config.toml unified)
- 単一バイナリ配布、go install 対応

### 補完関係 (既存 nlink-jp ツールとの位置付け)

| ツール | スコープ | 関係性 |
|--------|---------|--------|
| `analyze-text` (shell-agent-v2 built-in) | 多 LLM call sliding-window 解析 — finding 抽出、running summary 構築 | `gem-summary` が **軽量代替**。深い分析が要らないケースをカバー |
| `gem-rag` (util-series) | コーパス全体に対する RAG QA | 役割が違う (検索 vs 要約) |
| `gem-search` (util-series) | エージェンティック Web 検索 | 領域が違う |
| `gem-transcribe` (util-series) | 音声 → テキスト | 後段で gem-summary 呼び出すパイプ可能 |
| `data-analyzer` (util-series) | 大規模 JSON/JSONL の段階的要約 | 入力形式が違う (構造化 JSON) — gem-summary は plain text |

### 明示的にスコープ外

- 翻訳機能 (要約後に翻訳したい場合は別ツール orchestration で)
- 質問応答 / Q&A (RAG の領域)
- 構造化抽出 (キーワード / 固有表現等 — 将来 gem-shot を作るなら)
- 画像 / PDF 入力 (plain text only)
- ストリーミング出力 (シェル統合の単純さ優先)

### shell-agent-v2 統合は `@description:` 単独で完結させる

内蔵 prompt 改修・System Rules テンプレ・Global Memory 事前 seed
は **どれも不要**。優先順位:

1. **Primary**: shell-tool の `@description:` を丁寧に書く。LLM は
   tool list を読んだ時点で `gem-summary` を選択判断できる。説明文
   には対比対象 (analyze-text) の名前と用途を併記し、相対的選択基準
   を提示。
2. **Fallback (運用で揺らぎが顕在化したら)**: `examples/
   system_rules/` に「summary を要約タスクで優先」テンプレート追加、
   user が opt-in 可能に。
3. **個別補正**: ユーザーがチャット中で「summary を使って」と
   指示すれば常時即解決。

**built-in 固定 prompt は変更しない**: 外部 / optional ツールへの
参照を built-in prompt に持ち込まない原則 (結合度上昇・メンテ性
低下を回避)。

### Prompt injection 防御

nlk/guard でフル防御 (G1 選択)。入力テキストを nonce タグで wrap、
プロンプト内の injection 攻撃を抑止。shell-agent-v2 経由で外部
ドキュメント要約する用途ではガード有効が必須。

### 並列度

固定 3 (H1 選択)。Vertex AI rate limit (memory: Gemini API rate
limit、大量逐次で 429 連発) に配慮、predictable な動作。

---

## 4. Development Plan

全 Phase 完了後に **v0.1.0 を一度だけリリース** (J3 選択)。Phase
はリリース境界でなく内部マイルストーン。

### Phase 1: Core

- リポジトリ scaffold (`github.com/nlink-jp/gem-summary`、
  CONVENTIONS.md 準拠)
- Go module init、`google.golang.org/genai` 統合
- `~/.config/gem-summary/config.toml` ロード + env override
- 基本 CLI: `gem-summary [FILE]` / `--style` / `--lang` / `--model`
  / `--version` / `--help` / `--quiet`
- Vertex AI 1 call で要約 (chunking なし、上限超で error)
- nlk/guard 統合
- stderr 進捗表示 / `--quiet` 抑制
- ユニットテスト: prompt builder、style preset 切替、guard wrap、
  token 推定
- README.md / README.ja.md / CHANGELOG.md / LICENSE (MIT、
  既存準拠) / AGENTS.md

**完了基準**: 短いドキュメントを 1 call 要約として動作完結。

### Phase 2: Chunking + JSON Output

- chunk-and-merge 実装 (並列 3 固定、overlap で文脈ブリッジ)
- `--chunk-size` / `--max-input-tokens` フラグ
- `--json` 出力 (token usage / chunks 数 / 所要時間)
- chunk 並列処理の Vertex AI rate-limit handling (nlk/backoff
  指数 backoff)
- マージ prompt 設計 + テスト
- 大規模入力テスト (1MB ログサンプル等)

**完了基準**: 大規模ドキュメントの chunked 要約として完結。

### Phase 3: shell-agent-v2 統合 + Release

- shell-agent-v2 側 `examples/shell_tools/summary.sh` 追加。
  主要設計は `@description:` field の crafting:
  - 短〜中サイズで fast/cheap な要約用と明示
  - analyze-text との対比 (深い分析向け) を併記
  - LLM が tool list 読んだだけで使い分けできる文章にする
- shell-agent-v2 側 `examples/shell_tools/README.md` テーブル
  更新
- E2E スモーク (shell-agent-v2 から `.md` 添付 → "サマリして"
  → summary shell-tool が呼ばれる)
- アンブレラ (util-series) サブモジュール追加
- check-org.sh パス
- `nlink-jp/.github/profile/README.md` ツールリスト更新 (alphabet
  順、memory: org_profile_sort)
- 5 platform binary build (Linux x64/ARM, macOS x64/ARM, Windows
  x64; I2 選択)
- GitHub Release v0.1.0 + zip upload

**完了基準**: shell-agent-v2 経由で実用される状態。

---

## 5. Required API Scopes / Permissions

### Google Cloud

- **API**: Vertex AI API (`aiplatform.googleapis.com`) を Project
  で有効化
- **IAM Role**: `roles/aiplatform.user` (Vertex AI User) を実行
  プリンシパルに付与
- **認証**: Application Default Credentials (ADC)
  - 開発時: `gcloud auth application-default login`
  - CI / サービスアカウント運用時: SA キー or Workload Identity
  - gem-search / gem-image / gem-transcribe と同じ仕組み

### 追加権限なし

- ローカルファイル読込は OS 権限のみ
- ストレージ書き込みなし (stderr/stdout/config read のみ)
- ネットワークアクセスは Vertex AI エンドポイントのみ

---

## 6. Series Placement

**Series: util-series**

理由:

- パイプフレンドリーな CLI (stdin → stdout)
- データ変換系 (テキスト → 要約テキスト)
- 既存 `gem-*` 群 (gem-search / gem-image / gem-rag / gem-query
  / gem-transcribe) と整然と並ぶ
- LLM 駆動だが「対話 CLI クライアント」ではないので cli-series
  ではない
- セキュリティ系でも実験でもない

---

## 7. External Platform Constraints

### Vertex AI Gemini API

- **Rate limit** (memory: Gemini API rate limit): 大量逐次で 429
  発生報告あり
  → chunk 並列度を 3 固定で緩和、nlk/backoff で指数 retry
- **Context window**: gemini-2.5-flash は 1M tokens 入力対応だが、
  output reserve + safety margin で実効上限を 400k token に設定
  → 超過時 chunking 自動発動
- **Output token limit**: gemini-2.5-flash は 65k output、
  SHORT/MEDIUM/LONG プロンプトで明示制御
- **Region**: `global` / `us-central1` 等、config 指定可能
  (default `global`)
- **SDK**: `google-genai` Vertex AI Backend モード
- **Gemini 3 マイグレーション**: 2026-10-16 以降の GA で `gemini-3.x`
  への切替 + `ThoughtSignature` 必要性検証 (memory: Gemini 3
  migration 14 ツール影響リストに **gem-summary を追加** する必要
  あり)

### 配布チャネル

- GitHub Releases (既存 nlink-jp ツールと同じ)
- 5 platform binary zip (Linux x64/ARM, macOS x64/ARM, Windows x64)

### shell-agent-v2 統合契約

shell-agent-v2 の shell-tool 規約 (`@tool:` / `@description:` /
`@category:` / `@timeout:` / `@mitl:` header) に準拠:

- `@category: read` (副作用なし、認証 only)
- `@timeout: 120` (chunked 入力で 1-2 分の可能性、既存 gem-* と同じ)
- `@mitl: off` (read tool なので承認不要)
- `@description:` は LLM のツール選択を導くため特に丁寧に書く
  (Design Decisions §「shell-agent-v2 統合は `@description:` 単独
  で完結させる」)

---

## Discussion Log

### Q&A の経緯

1. **入力サイズ上限の挙動** (a): Option C 採択 — 巨大入力時のみ
   自動 chunk+merge。小〜中サイズは 1 call の高速経路を優先する
   設計が、analyze-text との差別化価値の本質。
2. **用途の幅** (b): A1 採択 — 純粋な「要約のみ」。汎用 1-shot
   実行 (gem-shot 案) は別ツールとして将来検討。
3. **スタイルプリセット** (c): C1 採択 — SHORT/MEDIUM/LONG の 3 段階。
4. **JSON 出力** (d): D1 採択 — shell-tool wrap で構造化結果を
   取得できる。
5. **chunking 時の言語制約** (e): E1+E2 採択 — `--lang` 指定時は
   ユーザー指定固定、無ければ入力言語 auto detect。
6. **ストリーミング** (f): F1 採択 (修正) — 完成サマリのみ stdout、
   ただし進捗は stderr に出す (gem-search 等と同様)、`--quiet` で
   抑制。
7. **デフォルトモデル**: 当初 `gemini-3.1-pro-preview` 案だったが、
   GA 前なので `gemini-2.5-flash` に変更。Gemini 3 GA 後に
   migration 計画に追加。
8. **prompt injection 防御レベル** (g): G1 採択 — フル防御。
9. **chunk 並列度** (h): H1 採択 — 固定 3。
10. **配布 platform** (i): I2 採択 — 5 platform binary。
11. **リリース回数** (j): J3 採択 — 全 Phase 完了後に v0.1.0 を
    一度だけ。
12. **shell-agent-v2 ガイダンス改修** (k): 当初 K1 (analyze-text
    記述子に 1 行追記) 案だったが、ユーザー指摘により撤回:
    「built-in fixed prompt に外部ツール参照を持ち込むのは結合度
    上昇・メンテ性低下」。さらに発展して、shell-tool の
    `@description:` を丁寧に書けば System Rules テンプレや user
    指示すら不要 (`@description:` が LLM のツール選択を導く primary
    mechanism、System Rules / Global Memory / user 指示は fallback)。
    Phase 3 から analyze-text 改修も System Rules テンプレ提供も
    削除、`@description:` の crafting に焦点。

### 重要な設計原則

**Decoupled integration via shell-tool description**: shell-agent-v2
への外部ツール統合は、shell-tool の `@description:` field を
丁寧に書くことだけで完結させ、built-in fixed prompt は不変に保つ。
これは gem-summary だけでなく、今後のあらゆる shell-tool 統合に
適用すべき一般原則。
