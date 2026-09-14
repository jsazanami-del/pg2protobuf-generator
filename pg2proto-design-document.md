# `pg2proto` 技術設計ドキュメント (Design Document)

PostgreSQL のシステムカタログから Protobuf (proto3) の **message / enum** を生成する CLI ツール `pg2proto` の仕様・設計です。

型の識別は **pgx v5 の OID / `pgtype.Map`** を一次ソースにします。フィールド番号は `.pg2proto.lock` で固定します。`buf validate` アノテーションはデフォルトで付与しますが、**buf CLI・`buf.yaml`・`buf.gen.yaml` は生成・実行しません**。

---

## 1. 概要 (Overview)

`pg2proto` は、PostgreSQL スキーマ（テーブル / view / materialized view と ENUM 型）を抽出し、`.proto` を生成します。

実現すること:

- **proto3 JSON と JavaScript の制約を踏まえた型写像**（`int64` は JSON では string、`numeric` は proto `string`、`bytea` は `bytes` など）
- **`buf validate` アノテーションの自動挿入**（`uuid`、`varchar(n)` の `max_len`。email はカラム override のみ）
- **フィールド番号・enum 値番号の後方互換**（`.pg2proto.lock`）
- **型・カラム単位の差し替え**（手書き message への参照。mixin やファイル全体テンプレはしない）

やらないこと（詳細は §7）:

- gRPC `service` / HTTP / OpenAPI アノテーションの生成
- buf CLI の組み込み、`buf.yaml` / `buf.gen.yaml` の出力
- 生成 message への共通フィールド差し込み（mixin）

利用者が TypeScript や OpenAPI を得る場合は、生成された `.proto` を自分の `buf generate` に渡します。

---

## 2. CLI コマンド・アーキテクチャ (CLI Design)

依存: **pgx v5** のみで接続する（`database/sql` / lib/pq は使わない）。SSL やパスワードは接続文字列（または `$DATABASE_URL`）に含める。パスワード専用フラグは設けない（`ps aux` / シェル履歴への露出防止）。

設定とフラグの優先順位: **フラグ > `.pg2proto.yaml` > デフォルト**。

### 2.1 サブコマンド構成

```bash
pg2proto [subcommand] [flags]
```

- **`init`**: `.pg2proto.yaml` の初期テンプレートを出力する。既存ファイルがある場合は上書きしない（`--force` で上書き）。
- **`generate`**: DB スキーマを解析し、`.proto` と `.pg2proto.lock` を出力・更新する。
- **`check`**: 現行 DB スキーマと既存 lock を比較し、非互換を検知する（CI 用。ファイルは書かない）。

### 2.2 終了コード

| コード | 意味 |
| :--- | :--- |
| `0` | 成功（`check` なら互換） |
| `1` | スキーマ非互換（番号再利用、proto 型の破壊的変更、reserved 再利用、未宣言リネームなど） |
| `2` | 接続失敗・設定不正・未知型など、比較以前のエラー |

### 2.3 `generate` のフラグ

| フラグ | 短縮 | デフォルト | 説明 |
| :--- | :--- | :--- | :--- |
| `--conn` | `-c` | `$DATABASE_URL` | PostgreSQL 接続文字列（DSN） |
| `--schema` | `-s` | `public` | 対象スキーマ（複数指定可） |
| `--out` | `-o` | `./proto` | `.proto` の出力先ディレクトリ |
| `--config` | | `.pg2proto.yaml` | 設定ファイル |
| `--lock-file` | | `.pg2proto.lock` | lock ファイル |
| `--dry-run` | | `false` | `.proto` も lock も書かない。preview を stdout へ |
| `--force` | | `false` | 非互換があっても lock を更新する（`reserved` は維持する） |
| `--prune` | | `false` | DB から消えた relation / enum の生成ファイルを削除する。無い場合は stale を残し、`check` は fail |
| `--exclude` | | （なし） | 除外 glob（yaml の exclude に加算） |

非互換な変更があるとき、`--force` 無しの `generate` は終了コード `1` で失敗し、ファイルを更新しない。

### 2.4 `check` のフラグ

`generate` と同じ `--conn` / `--schema` / `--config` / `--lock-file` を取る。DB に接続する。出力ファイルは書かない。

fail 条件:

- フィールド番号または enum 値番号の再利用
- reserved 番号・名前の再利用
- 非互換な `proto_type` 変更（例: `int32` → `string`、`string` → カスタム message）
- カラム / enum ラベルのリネームが yaml / lock の `renames` で未宣言
- 対象だった relation / enum の削除（`--prune` 前の状態）

互換な追加（新カラムに新番号、新 enum ラベルに `max+1`）は OK。

### 2.5 `init` のフラグ

| フラグ | デフォルト | 説明 |
| :--- | :--- | :--- |
| `--config` | `.pg2proto.yaml` | 出力先 |
| `--force` | `false` | 既存ファイルを上書き |

`buf.yaml` / `buf.gen.yaml` は出さない。

---

## 3. 型システム (pgx OID → proto)

接続と型の識別は pgx v5。pgx は **PostgreSQL → Go の Codec** であり、proto 生成器ではない。写像は二段にする。

```
atttypid (OID)
  → pgtype.Map.TypeForOID
  → 未知ならカタログの型名で Conn.LoadType → Map.RegisterType
  → ArrayCodec / DOMAIN を剥がす
  → OID（または Codec 種別）→ proto 型
  → attnotnull / atttypmod / override / lock を適用
```

行をスキャンするときの `pgtype.Int4{Valid: false}`（SQL NULL）は **使わない**。スキーマの NULL 可否は `pg_attribute.attnotnull` だけを見る。

`varchar(n)` の `n` は `atttypmod` から取る。pgx の型マップには長さが無い。

### 3.1 カタログ抽出範囲

対象 relation（`pg_class.relkind`）:

| relkind | 意味 | 扱い |
| :--- | :--- | :--- |
| `r` | 普通のテーブル | 含める |
| `v` | view | 含める |
| `m` | materialized view | 含める |
| `f` | 外部テーブル（FDW） | 除外 |
| `p` | パーティション親 | 除外 |
| （`relispartition`） | パーティション子 | 除外 |
| `S` / `c` など | シーケンス / composite type | 除外 |

対象 ENUM: 選択スキーマ内の `pg_type.typtype = 'e'`。カラムからの参照有無を問わず出す。

除外する列:

- `attnum < 1`（システム列）
- `attisdropped`

生成カラムは含める（読み取り用 message として妥当）。外部キーはネスト message にせず、フラットな列として出す。

参照カタログ:

- `pg_class`, `pg_namespace`, `pg_attribute`（`atttypid`, `atttypmod`, `attnotnull`）
- `pg_enum`（`enumlabel`。**番号に `enumsortorder` は使わない**）
- `pg_description`（`COMMENT ON` → proto コメント）
- 型の中身は pgx。`pg_attrdef` は見ない（proto3 にカスタム default が無い）
- `CHECK` 制約は自動 CEL 化しない

view / matview:

- 列と型はテーブルと同じ `pg_attribute` パイプライン
- `attnotnull` は基底が NOT NULL でも **false になりやすい**。view フィールドはほぼ `optional`。これは正しい（view 定義で NULL になり得る）
- 同一スキーマで table と view が同名になることは PostgreSQL 上あり得ない

外部テーブル（除外する理由）: `CREATE FOREIGN TABLE` + FDW。カタログに列はあるが実体は別 DB / CSV など。接続先は proto に無関係。

### 3.2 一段目: pgx

- 組込み型: `pgtype.NewMap().TypeForOID(oid)`。定数は `pgtype.Int4OID` など
- ユーザー定義（DOMAIN / ENUM / 配列 / composite）は OID が DB ごとに違う。未知 OID は型名をカタログから取り `Conn.LoadType` → `RegisterType`
- 配列・DOMAIN の unwrap は pgx の Codec に委譲する（手で `typbasetype` を辿らない）
- 2 次元以上の配列はエラー

### 3.3 二段目: OID → proto

キーは typname 文字列ではなく **`pgtype.*OID`（剥がしたあと）**。未知 OID かつ `LoadType` 失敗は **エラー**（黙って `string` にしない）。composite / `RecordOID` もエラー。override で型名を差し替えれば救済できる。

| PostgreSQL（pgx OID） | Protobuf | 自動 validate | 備考 |
| :--- | :--- | :--- | :--- |
| `BoolOID` | `bool` | - | |
| `Int2OID` / `Int4OID` | `int32` | - | proto に int16 が無いため int2 も int32 |
| `Int8OID` | `int64` | 付けない | Proto3 JSON では string。ワイヤ型は int64 のまま |
| `Float4OID` | `float` | - | |
| `Float8OID` | `double` | - | |
| `TextOID` / `VarcharOID` / `BPCharOID` / `NameOID` | `string` | varchar は `max_len` | |
| `ByteaOID` | `bytes` | - | JSON では proto3 標準の base64。デフォルトを string にしない |
| `UUIDOID` | `string` | `string.uuid = true` | |
| `JSONOID` / `JSONBOID` | `string` | - | `options.jsonb_as_struct: true` で `google.protobuf.Struct` |
| `TimestamptzOID` | `google.protobuf.Timestamp` | - | `timestamp.proto` を import |
| `TimestampOID` | `string` | - | TZ 無しを Timestamp にしない |
| `DateOID` | `string` | 日付 pattern `^\d{4}-\d{2}-\d{2}$` | `google.type.Date` は override |
| `TimeOID` / `TimetzOID` / `IntervalOID` | `string` | - | |
| `NumericOID` | `string` | - | JS Number 精度と proto 標準 decimal が無いため |
| `InetOID` / `CIDROID` / `MacaddrOID` / `Macaddr8OID` | `string` | - | |
| `BitOID` / `VarbitOID` / `XMLOID` / `JSONPathOID` | `string` | - | |
| 幾何（`PointOID` 等） | `string` | - | override で message 可 |
| range / multirange | `string` | - | override で専用 message 可 |
| 配列 / `ArrayCodec` | `repeated T` | 要素に準拠 | PG の NULL 配列と `[]` は区別しない |
| ENUM | proto `enum` | - | §3.6 |
| composite / `RecordOID` | （エラー） | - | override 必須 |

`int64_as_string` オプションは **設けない**。Proto3 JSON は既に int64 を string 化する。proto のワイヤ型まで string にすると gRPC binary 互換が壊れる。

SERIAL / IDENTITY / PK だからといって `int64.gt = 0` は **自動付与しない**（UUID PK や負値を壊す）。

### 3.4 NULL と presence

| カタログ | proto |
| :--- | :--- |
| `attnotnull = true` | `T name = N;` |
| `attnotnull = false` | `optional T name = N;` |

- 配列は常に `repeated T`（NULL 配列と空配列は区別しない）
- PK / UNIQUE は型に影響しない。proto3 の `required` は使わない
- 部分更新用メッセージは生成しない
- proto3 の **submessage は `optional` 無しでも presence がある**。override でカスタム message にした列は、スカラの `optional` 規則と非対称になる

pgx の `Valid` は行スキャン用である:

```go
var id pgtype.Int4
// id.Int32 == 42, id.Valid == true  → SQL の 42
// id.Valid == false                 → SQL の NULL
```

生成器はこれを message の型に写さない。

### 3.5 命名規則とファイル配置

| 対象 | 規則 |
| :--- | :--- |
| ファイル（relation） | `{out}/{schema}/{relation}.proto`（PG 名を snake のまま） |
| ファイル（ENUM） | `{out}/{schema}/{enum_type}.proto` |
| package | `{package_prefix}.{schema}`（テーブルごと package にしない） |
| `go_package` | `{go_package_prefix}/{schema}` |
| message 名 | テーブル / view 名を PascalCase。**自動単数化しない**（`users` → `Users`） |
| enum 型名 | PG 型名を PascalCase（`order_status` → `OrderStatus`） |
| enum 値 | `{ENUM}_UNSPECIFIED = 0` を合成。PG ラベルは `{ENUM}_{LABEL}` の UPPER_SNAKE |
| フィールド | PG カラム名を snake_case のまま。**`json_name` は付けない**（JSON は proto3 標準で camelCase） |

識別子が proto 予約語または不正な場合は末尾 `_` を付け、lock に `proto_name` を記録する。enum ラベルの不正文字は `_` に置換する。

テーブル proto は、参照する同じスキーマの enum ファイルを `import` する。

生成ファイル先頭:

```protobuf
// Code generated by pg2proto. DO NOT EDIT.
```

### 3.6 PG ENUM → proto enum

pgx は ENUM を主に string としてスキャンする。proto は共有語彙なので **`enum` にする**（デフォルトで string に落とさない）。override で特定 ENUM を `string` にすることは可。

- ラベル一覧は `pg_enum.enumlabel`
- **値番号に `enumsortorder` を使わない**（`ADD VALUE ... BEFORE` で順序が変わってもワイヤ番号は不変）
- proto3 は 0 が必須。PG に未指定値は無いので **`{ENUM}_UNSPECIFIED = 0` を常に足し、PG 値は 1 から lock で採番**
- 追加ラベルは `max+1`。型再作成で消えたラベルは番号・名前を `reserved`
- `ALTER TYPE ... RENAME VALUE` は `renames` 未宣言なら `check` fail
- NULL な enum 列は `optional OrderStatus`。0 は「未指定」、NULL はフィールド無し
- ENUM 配列は `repeated OrderStatus`

例（`public.order_status`）:

```protobuf
syntax = "proto3";

package db.v1.public;

option go_package = "github.com/example/app/gen/proto/db/v1/public";

enum OrderStatus {
  ORDER_STATUS_UNSPECIFIED = 0;
  ORDER_STATUS_PENDING = 1;
  ORDER_STATUS_SHIPPED = 2;
}
```

---

## 4. 堅牢性 (lock / override / 責務)

### 4.1 フィールド番号・enum 値番号 (`.pg2proto.lock`)

**課題:** `attnum` や `enumsortorder` に番号を載せると、`ALTER` でワイヤ互換が壊れる。OID も dump/restore で変わるので lock のキーにしない。

**解決:** YAML の `.pg2proto.lock`（`version: "1"`）。安定キーは名前。

- メッセージ: `schema.relation` / `column`
- ENUM: `schema.enum_type` / PG ラベル名
- 各フィールド: `number`, `proto_type`, `pg_type`（正規名は pgx `Type.Name`）、必要なら `proto_name`
- 削除した列 / ラベルは **番号も名前も `reserved` に残し、再利用しない**
- 新規番号は「使用中 + reserved の最大 + 1」。**19000–19999 は飛ばす**（protobuf 内部予約）
- リネーム: yaml または lock の `renames`（旧名 → 新名）が無いと drop + add 扱いになりワイヤ破壊。`check` は fail
- lock に `pg_oid` をデバッグ用に残すのは可。キーには使わない

```yaml
version: "1"
messages:
  public.users:
    proto_name: Users
    fields:
      id:
        number: 1
        proto_type: int64
        pg_type: int8
      email:
        number: 2
        proto_type: string
        pg_type: varchar
    reserved_numbers: [5]
    reserved_names: ["legacy_col"]
    renames: {}
enums:
  public.order_status:
    proto_name: OrderStatus
    values:
      pending:
        number: 1
        proto_name: ORDER_STATUS_PENDING
      shipped:
        number: 2
        proto_name: ORDER_STATUS_SHIPPED
    reserved_numbers: []
    reserved_names: []
    # UNSPECIFIED=0 は合成値。lock の values には PG ラベルだけを置く
```

### 4.2 型・カラム override（カスタムはこれだけ）

機械的写像だけでは「この `jsonb` だけ Struct」「この列だけ email」に応えられない。`.pg2proto.yaml` で **型名または `schema.relation.column` の差し替え**を許す。

できること: `proto_type` / `import` / validate（protovalidate と 1:1）。

できないこと:

- 生成 message への共通フィールド差し込み（mixin）
- Go `text/template` による `.proto` 全体上書き

message の骨格・番号・列集合のオーナーは常にカタログ + lock。共通メタが必要なら、生成 message を包む手書き wrap を使う。

override の制約:

- 参照先 message の中身は生成器が検証しない。import 切れは `protoc` / `buf` まで分からない
- lock は `proto_type` 文字列を持つ。カスタム message **内部**の番号変更は `check` の対象外
- override で proto 型が変わること自体は非互換。`check` は fail
- カスタム型に `string.uuid` は付けない。CEL はカスタム側フィールド名に依存し、生成器は型チェックしない
- NULL / 配列の出し方（`optional` / `repeated` / 素のフィールド）は元カラムに従う

優先順位: **カラム override > 型 override > グローバル options > 既定マップ**。

### 4.3 buf との境界

`pg2proto` は `.proto` と `.pg2proto.lock` と、`init` 時の `.pg2proto.yaml` だけを出す。

`import "buf/validate/validate.proto"` を書くが、protovalidate のモジュール解決（`buf.yaml` の `deps` など）は利用者の作業である。ツールは `buf generate` を呼ばない。

---

## 5. 設定ファイル仕様 (`.pg2proto.yaml`)

```yaml
version: "1"

proto:
  package_prefix: "db.v1"
  go_package_prefix: "github.com/example/app/gen/proto/db/v1"

options:
  jsonb_as_struct: false  # true なら json/jsonb を google.protobuf.Struct
  validate: true          # false なら自動アノテーションも import も出さない

tables:
  include: []             # 空なら対象スキーマの全 table/view/matview
  exclude: ["_*"]         # glob（schema.relation または relation）

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

validate キーは出す protovalidate と 1:1 にする。

| yaml | 生成 |
| :--- | :--- |
| `email: true` | `(buf.validate.field).string.email = true` |
| `uuid: true` | `(buf.validate.field).string.uuid = true` |
| `max_len: 255` | `(buf.validate.field).string.max_len = 255` |
| `cel: "..."` | `(buf.validate.field).cel` |
| `pattern: "..."` | `(buf.validate.field).string.pattern` |

自動付与（`options.validate: true` のとき）:

- `UUIDOID` → `string.uuid`
- `varchar(n)` → `max_len`
- `date` を string にした場合 → 日付 `pattern`
- ルールが1つでもあるファイルだけ `buf/validate/validate.proto` を import
- 列名からの email 推論はしない

---

## 6. 生成物例

### 6.1 `proto/public/users.proto`

```protobuf
// Code generated by pg2proto. DO NOT EDIT.
syntax = "proto3";

package db.v1.public;

option go_package = "github.com/example/app/gen/proto/db/v1/public";

import "buf/validate/validate.proto";
import "google/protobuf/timestamp.proto";
import "public/order_status.proto";

message Users {
  int64 id = 1;
  string email = 2 [(buf.validate.field).string.email = true];
  google.protobuf.Timestamp created_at = 3;
  repeated string tags = 4;
  optional string nickname = 5;
  OrderStatus status = 6;
}

message UsersView {
  optional int64 id = 1;
  optional string email = 2;
}
```

- `id` に `gt = 0` は付けない
- JSON では `createdAt` になる（proto3 標準。`json_name` 無し）
- view は `attnotnull` が false なら `optional`

### 6.2 利用者が自分で持つ buf 設定（本ツールは出さない）

validate 付き proto をコンパイルするには、利用者が例えば次を用意する。これは例示であり、`generate` の成果物ではない。

```yaml
# buf.yaml（利用者が管理）
version: v2
deps:
  - buf.build/bufbuild/protovalidate
```

---

## 7. 非目標

- gRPC `service` / RPC / `google.api.http` / OpenAPI 生成
- 外部テーブル（FDW）
- パーティション親・子
- proto `enum` 以外の複合ユーザー型（composite）の自動 message 化
- decimal 専用 proto 型（`numeric` は `string`）
- 生成 Go コードに `pgtype.*` を出すこと
- `buf.yaml` / `buf.gen.yaml` / buf CLI の実行
- 生成 message への mixin
- Go `text/template` による `.proto` 全体上書き
- CHECK / DOMAIN 制約の自動 CEL 化
- FK のネスト message 化
)
