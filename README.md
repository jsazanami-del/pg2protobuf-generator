# pg2proto

PostgreSQL のテーブル / view / materialized view と ENUM から、Protobuf (proto3) の message / enum を生成する CLI です。

フィールド番号は `.pg2proto.lock` で固定します。gRPC service や OpenAPI、`buf.yaml` は生成しません。詳細仕様は [pg2proto-design-document.md](pg2proto-design-document.md) を見てください。

## インストール

Go 1.26 以降が必要です。

```bash
go install github.com/jsazanami-del/pg2protobuf-generator/cmd/pg2proto@latest
```

ソースから使う場合:

```bash
git clone https://github.com/jsazanami-del/pg2protobuf-generator.git
cd pg2protobuf-generator
go run ./cmd/pg2proto --help
# または
go build -o pg2proto ./cmd/pg2proto
```

## 使い方

接続情報は `--conn`（`-c`）または環境変数 `DATABASE_URL` に載せます。パスワード専用フラグはありません。

```bash
export DATABASE_URL='postgres://user:pass@localhost:5432/db?sslmode=disable'

pg2proto init
pg2proto generate
pg2proto check
```

設定とフラグの優先順位は **フラグ > `.pg2proto.yaml` > デフォルト** です。

### コマンド

| コマンド | 役割 |
| :--- | :--- |
| `init` | `.pg2proto.yaml` のテンプレートを書く。既存があれば失敗（`--force` で上書き） |
| `generate` | カタログを読んで `.proto` と `.pg2proto.lock` を出す |
| `check` | 現行スキーマと lock を比較する（CI 用。ファイルは書かない） |

生成物:

- `{--out}/{schema}/{relation}.proto`（既定の `--out` は `./proto`）
- `{--out}/{schema}/{enum_type}.proto`
- `.pg2proto.lock`

### `generate` / `check` のフラグ

| フラグ | 短縮 | デフォルト | 説明 |
| :--- | :--- | :--- | :--- |
| `--conn` | `-c` | `$DATABASE_URL` | PostgreSQL 接続文字列 |
| `--schema` | `-s` | `public` | 対象スキーマ（複数可） |
| `--out` | `-o` | `./proto` | `.proto` の出力先 |
| `--config` | | `.pg2proto.yaml` | 設定ファイル |
| `--lock-file` | | `.pg2proto.lock` | lock ファイル |
| `--dry-run` | | `false` | ファイルを書かず stdout に出す（`generate` のみ） |
| `--force` | | `false` | 非互換でも lock を更新する（`generate` のみ。`reserved` は維持） |
| `--prune` | | `false` | 消えた relation / enum の生成 proto を削除（`generate` のみ） |
| `--exclude` | | （なし） | 除外 glob（yaml の exclude に加算） |
| `--strict-types` | | `false` | 未知型・composite でエラー。既定は `google.protobuf.Any` |

```bash
pg2proto generate -c "$DATABASE_URL" -s public -s app -o ./proto
pg2proto generate --dry-run
pg2proto generate --strict-types
pg2proto check
```

### 終了コード

| コード | 意味 |
| :--- | :--- |
| `0` | 成功（`check` なら互換） |
| `1` | スキーマ非互換（型の破壊的変更、未宣言リネーム、削除など） |
| `2` | 接続失敗・設定不正など |

## YAML（`.pg2proto.yaml`）

`pg2proto init` でひな形が出ます。例:

```yaml
version: "1"

proto:
  package_prefix: "db.v1"
  go_package_prefix: "github.com/example/app/gen/proto/db/v1"

options:
  jsonb_as_struct: false  # true なら json/jsonb を google.protobuf.Struct
  validate: true          # false なら buf validate のアノテーションも import も出さない
  strict_types: false     # true なら未知型・composite でエラー（既定は google.protobuf.Any）

tables:
  include: []             # 空なら対象スキーマの全 table/view/matview
  exclude: ["_*"]         # glob。schema.relation または relation にマッチ

fields:
  omit: ["password", "*_hash"]   # glob。column / relation.column / schema.relation.column
  extra:                         # 全 message に足す
    - name: etag
      proto_type: string
      optional: true

messages:
  "public.users":
    omit: ["internal_notes"]
    extra:
      - name: display_name
        proto_type: string
        optional: true
        validate:
          max_len: 255

renames:
  columns:
    "public.users.old_email": "email"
  enum_values:
    "public.order_status.pendng": "pending"

overrides:
  types:
    int4multirange:
      proto_type: "my.package.Int4MultiRange"
      import: "my/package/ranges.proto"
      validate:
        cel: "this.ranges.size() > 0"
  columns:
    "public.users.email":
      proto_type: "string"
      validate:
        email: true
    "public.users.metadata":
      proto_type: "google.protobuf.Struct"
      import: "google/protobuf/struct.proto"
```

### キーの意味

| キー | 内容 |
| :--- | :--- |
| `proto.package_prefix` | `package {prefix}.{schema};` |
| `proto.go_package_prefix` | `option go_package = "{prefix}/{schema}";` |
| `options.jsonb_as_struct` | json/jsonb を Struct にする |
| `options.validate` | uuid / varchar `max_len` / date pattern などの自動アノテーション |
| `options.strict_types` | 未知型をエラーにする。`false` なら `google.protobuf.Any` |
| `tables.include` / `exclude` | 対象 relation の glob。include が空なら全件 |
| `fields.omit` | 生成しない列の glob（`column` / `relation.column` / `schema.relation.column`） |
| `fields.extra` | 全 message に足すフィールド。番号は lock が振る |
| `messages` | `schema.relation` 単位の omit / extra。グローバルに加算 |
| `renames` | カラム / enum ラベルの改名。無いと drop+add 扱いで `check` が失敗することがある |
| `overrides.types` | PG 型名ごとの proto 型・import・validate |
| `overrides.columns` | `schema.relation.column` 単位。型 override より優先 |

validate の yaml キーと生成物の対応:

| yaml | 生成 |
| :--- | :--- |
| `email: true` | `(buf.validate.field).string.email = true` |
| `uuid: true` | `(buf.validate.field).string.uuid = true` |
| `max_len: 255` | `(buf.validate.field).string.max_len = 255` |
| `pattern: "..."` | `(buf.validate.field).string.pattern` |
| `cel: "..."` | `(buf.validate.field).cel` |

extra のキー: `name`（lock のキー兼 proto 名）、`proto_type`、`optional` / `repeated`（排他）、`import`、`validate`、`comment`。番号は YAML に書かない。`google.protobuf.*` は import 省略可。それ以外のカスタム型は `import` 必須。カタログ列と同名の extra はエラー（型の差し替えは `overrides.columns`）。

omit した列の番号は lock の `reserved` に残る。omit と extra を同時にしても未宣言リネームにはしない。

テンプレ全体の上書きはできません。

## 注意

- 対象は `relkind` が table / view / matview のものと、対象スキーマ内の ENUM です。外部テーブル（FDW）とパーティション親・子は出しません。
- `import "buf/validate/validate.proto"` は出しますが、`buf.yaml` の deps は自分で用意してください。このツールは `buf generate` を呼びません。
- 未知の PostgreSQL 型と composite は、既定で `google.protobuf.Any` になります。写像漏れをエラーにしたいときは `--strict-types` または `options.strict_types: true` を使います。

## テスト / lint

Go のソースは `cmd/` と `internal/` にあります。リポジトリ直下で `go test` だけを実行すると `no Go files` で失敗します。サブパッケージまで含めてください。

```bash
go test ./...
golangci-lint run
```

設定は [`.golangci.yml`](.golangci.yml) です。CI は push / pull request で `golangci-lint` と `go test -race ./...` を回します。
)