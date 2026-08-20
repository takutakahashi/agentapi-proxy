# Session Pool Binding Quota Design

## Goal

Session Pool の Binding に同時実行数の上限を持たせ、ユーザーまたはチームごとに
cluster-wide Session Manager の利用量を制御する。

新しい Quota リソースや厳密な reservation は追加せず、既存の Binding と Allocation を
拡張した best-effort quota とする。

## Data model

外部から設定する quota 項目は `max_concurrent` のみとする。

```go
type Binding struct {
	ID            string      `json:"id"`
	Pool          string      `json:"pool"`
	SubjectType   SubjectType `json:"subject_type"`
	SubjectID     string      `json:"subject_id"`
	Role          BindingRole `json:"role"`
	Enabled       bool        `json:"enabled"`
	Priority      int         `json:"priority,omitempty"`
	MaxConcurrent int         `json:"max_concurrent,omitempty"`
}
```

- `max_concurrent == 0`: 上限なし。既存 Binding もこの値になるため後方互換になる。
- `max_concurrent > 0`: この Binding を使う active Allocation の最大数。
- 負数は API で拒否する。
- quota は `use` と `manage` のどちらの Binding にも同じように適用する。

Allocation には、選択時に適用した Binding ID を保存する。

```go
type Allocation struct {
	// Existing fields omitted.
	BindingID string `json:"binding_id,omitempty"`
}
```

既存 Allocation の `binding_id` は空になる。空の Allocation は quota の集計対象に含めない。

日次利用時間、CPU、メモリ、コストなどの quota は対象外とする。必要になった時点で
別フィールドとして追加し、汎用的な quota expression は導入しない。

## Session owner

Binding の詳細度を決める owner は session scope から一意に決定する。

| Session scope | Exact subject | Subject ID |
| --- | --- | --- |
| user | `user` | authenticated user ID |
| team | `team` | request の `team_id` |

team scope では作成者個人の user Binding を評価せず、指定された team Binding を評価する。
user scope ではユーザーが所属する team の Binding を評価しない。これにより quota の帰属先が
作成者のチーム所属順に依存しない。

## Binding specificity

Pool ごとに、次の順番で effective Binding を解決する。

1. session owner と完全一致する `user` または `team` Binding
2. `all` Binding
3. Binding なし

```text
effectiveBinding(pool, session):
    owner = session.scope == team
        ? (team, session.team_id)
        : (user, session.user_id)

    if enabled binding(pool, owner.type, owner.id) exists:
        return exact binding

    if enabled binding(pool, all, "") exists:
        return all binding

    return none
```

exact Binding と `all` Binding は累積しない。exact Binding が存在する場合は exact Binding
だけを適用する。exact Binding の quota が上限に達しても `all` Binding へ fallback しない。
fallback すると個別 quota を迂回できるためである。

Binding の詳細度は同一 Pool 内で解決する。別 Pool の Binding には影響しない。

例:

```json
[
  {
    "pool": "shared-cluster",
    "subject_type": "all",
    "subject_id": "",
    "max_concurrent": 100
  },
  {
    "pool": "shared-cluster",
    "subject_type": "team",
    "subject_id": "org/team-a",
    "max_concurrent": 10
  },
  {
    "pool": "shared-cluster",
    "subject_type": "user",
    "subject_id": "alice",
    "max_concurrent": 2
  }
]
```

- Team A の team session は Team A の Binding を使い、Team A 全体で10まで。
- Alice の user session は Alice の Binding を使い、Alice 個人で2まで。
- exact Binding がない owner は `all` Binding を使い、全 owner で共有する100まで。

## Pool resolution

Resolver は `userID + teams` ではなく、session scope から作った owner を受け取る。

```go
type Subject struct {
	Type SubjectType
	ID   string
}

type ResolvedPool struct {
	Pool    *LogicalPool
	Binding *Binding
}

func (r *Resolver) Resolve(ctx context.Context, owner Subject, tags map[string]string) (*ResolvedPool, error)
```

`allocator.pool` があれば明示指定として扱い、一致する利用可能な Pool がなければエラーにする。
指定がなければ、現在の Binding・health・label 条件で利用可能な候補を effective Binding の
`priority` 降順で並べ、最も高い Pool を選ぶ。同じ priority は Pool 名の昇順で決定的に選ぶ。
候補がなければ local Session Manager 経路へ進む。Preference と default Pool は持たない。
Resolver が Pool と effective Binding を一緒に返すことで、quota 検査時に Binding を探し直さない。

これは旧 resolver との意図的な互換性変更である。旧 resolver は user scope でもユーザーの
所属 team Binding を候補に含めていたが、新 resolver では含めない。移行時に user scope を
cluster-wide Pool へ割り当て続ける場合は、対象 user Binding または `all` Binding を作成する。
Binding がない owner、または別 scope の Binding しかない owner は Pool を選択せず、従来の
local Session Manager 経路へ進む。

## Best-effort enforcement

セッション作成時に、同じ `binding_id` を持つ active Allocation を数える。

active status は次の4つとする。

- `pending`
- `leased`
- `claimed`
- `running`

`completed`、`failed`、削除済み Allocation は数えない。

```text
resolved = resolver.resolve(owner, tags)
if resolved == none:
    use existing local Session Manager path

binding = resolved.binding
if binding.max_concurrent > 0:
    active = count active allocations where binding_id == binding.id
    if active >= binding.max_concurrent:
        return 429

enqueue allocation with binding_id = binding.id
```

quota 専用の counter、reservation、CAS、release、reconciliation は実装しない。Allocation が
terminal status になるか削除されると、次回の集計から自然に外れる。Session API で
セッションを削除した場合は、対応する Allocation も同時に削除して枠を解放する。

集計と enqueue は atomic ではない。同時リクエストが同じ active 数を読んだ場合、上限を
一時的に超える可能性がある。この quota は課金やセキュリティ境界ではなく、インフラ利用量の
ガードレールとして扱い、この競合を許容する。厳密な上限が必要になった場合だけ、将来
`TryAcquire(bindingID, limit)` のような reservation を追加する。

## Quota exceeded response

quota 超過時は Allocation を作成せず HTTP `429 Too Many Requests` を返す。

```json
{
  "error": "session pool quota exceeded",
  "pool": "shared-cluster",
  "binding_id": "binding-...",
  "max_concurrent": 2,
  "active": 2
}
```

quota 超過を理由に `all` Binding、別 Pool、local Session Manager へ自動 fallback しない。

## Existing capacity limits

`PoolSupplier.MaxRunners` は Manager が提供できるインフラ容量であり、Binding の
`MaxConcurrent` はユーザー／チーム側の利用上限である。役割が異なるため両方を維持する。

この quota は cluster-wide Session Pool 経由で作成される Allocation のみに適用する。
直接 `manager_id` を指定する legacy External Session Manager 経路と local Session Manager
は対象外とする。

## API changes

Binding の create、patch、response と OpenAPI schema に `priority` と `max_concurrent` を含める。

- create: 省略時は0。
- patch: 指定時だけ更新する。
- validation: 0以上。
- response: 0は省略可能。

Preference API と保存モデル、Logical Pool の `default` フラグは廃止する。既存 Preference の
保存データは参照されず、Pool 削除時の cleanup 対象にも含めない。

## Required tests

Binding resolution:

- user scope で user Binding が `all` Binding より優先される。
- team scope で team Binding が `all` Binding より優先される。
- team scope で作成者の user Binding を使用しない。
- user scope で所属 team の Binding を使用しない。
- exact Binding がない場合だけ `all` Binding を使用する。
- 複数候補では effective Binding の priority が最も高い Pool を選ぶ。
- priority が同じ場合は Pool 名順で選ぶ。
- exact Binding の quota 超過時に `all` Binding へ fallback しない。

Quota enforcement:

- `max_concurrent == 0` は無制限になる。
- active Allocation が上限未満なら enqueue できる。
- active Allocation が上限と同じなら `429` になる。
- completed、failed、deleted Allocation は数えない。
- `binding_id` のない既存 Allocation は数えない。

API:

- create / patch で負の `max_concurrent` を拒否する。
- Binding response と OpenAPI schema に `max_concurrent` が含まれる。
