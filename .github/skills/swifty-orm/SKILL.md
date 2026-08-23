---
name: swifty-orm
description: >
  Knex-style chainable MongoDB ORM for Go (module
  github.com/hangtiancheng/swifty.go/swifty_orm). Use when Go code calls
  swifty_orm.NewEngine, engine.Collection, engine.Model, engine.Client,
  engine.Database, engine.DatabaseName, engine.Close, engine.DropDatabase,
  the chainable Query builder (Where equality/operator/object forms, WhereNot,
  WhereIn, WhereNotIn, WhereNull, WhereNotNull, WhereBetween, WhereNotBetween,
  WhereLike, WhereILike, OrWhere and Or-variants OrWhereNot/OrWhereIn/
  OrWhereNotIn/OrWhereNull/OrWhereNotNull/OrWhereBetween, OrderBy, Limit,
  Offset, Select, Clone), terminal methods (Insert, First, Find, Update,
  Upsert, Increment, Decrement, Delete, Count, Exists, EnsureIndexes,
  DropCollection) and their InsertResult/UpsertResult return structs,
  aggregation (Sum, Avg, Min, Max, Distinct, CountDistinct, Pluck), grouped
  aggregation (GroupBy, Having, CountAs, SumAs, AvgAs, MinAs, MaxAs,
  Aggregate), streaming (Query.Cursor, Each, Cursor.Next, Cursor.Decode,
  Cursor.Current, Cursor.Err, Cursor.Close), Transaction with auto session
  binding, NextSequence, CollectionName, ErrNotFound, ErrCollectionRequired,
  the logging helpers SetLevel/Info/Infof/Error/Errorf with InfoLevel/
  ErrorLevel/Disabled, or any import of the module. Also use for Knex-style
  query chaining over MongoDB in Go, for bson filter construction through this
  builder, and for questions about builder mutation versus Clone semantics. Do
  NOT use for GORM, sqlx, ent, lark_orm, raw mongo-driver code without
  swifty_orm, or any non-MongoDB datastore.
---

# swifty_orm

A Knex-inspired, chainable query builder ORM for MongoDB in Go, built directly
on the official `go.mongodb.org/mongo-driver`. The design philosophy is a
faithful mapping of Knex.js query semantics onto MongoDB: conditions on the
same field AND-combine without silent overwrites, invalid builder input is
recorded and surfaced as an error at execution time rather than panicking,
plain update documents are auto-wrapped in `$set`, and `Update` returns the
matched count (Knex "affected rows" semantics). The package exposes three
exported types: `Engine` (connection, transaction, and sequence management),
`Query` (a mutable chainable builder with terminal, aggregation, grouping, and
streaming methods), and `Cursor` (streaming iteration). Flat layout, no
sub-packages.

Module path: `github.com/hangtiancheng/swifty.go/swifty_orm`

Source root: `swifty_orm/`

Go directive: `go 1.26.0`

## When to load adjacent skills

Load `swifty-http` or `swifty-rpc` alongside this skill when wiring an `Engine`
into a server process (construction at startup, request-context propagation,
shutdown ordering). Load `swifty-cache` when layering a read-through cache in
front of ORM reads. swifty_orm has no compile-time dependency on any sibling
module; `go.mod` only carries `replace` directives for them.

## Architecture overview

```
Engine (engine.go)                     [connection + session + sequence owner]
  |-- client       *mongo.Client       (mongo.Connect + Ping at construction)
  |-- database     *mongo.Database
  |-- databaseName string
  |-- session      mongo.Session       (non-nil only on a Transaction sub-Engine)
  |
  |-- Client() / Database() / DatabaseName()   [nil-receiver-safe accessors]
  |-- Close(ctx) / DropDatabase(ctx)           [lifecycle, nil-safe no-ops]
  |-- Collection(name) -> *Query               [entry point to the builder]
  |-- Model(value)     -> *Query               [Collection(CollectionName(v))]
  |-- Transaction(ctx, fn)                     [session-scoped sub-Engine]
  |-- NextSequence(ctx, name) -> int64         ["counters" collection, $inc]
  |-- sessionContext(ctx)                      [binds tx session onto ctx]

Query (query.go)                       [mutable, chainable; owns builder state]
  |-- collection  *mongo.Collection    (nil => ErrCollectionRequired)
  |-- engine      *Engine              (for session binding via execCtx)
  |-- conditions  []condition          <- Where family (AND chain)
  |-- orGroups    [][]condition        <- OrWhere family ($or branches)
  |-- sort        bson.D               <- OrderBy (ordered, appended)
  |-- limit, skip int64                <- Limit / Offset
  |-- fields      []string             <- Select ("-" prefix = exclude)
  |-- groupFields []string             <- GroupBy
  |-- havingConds []condition          <- Having
  |-- aggSpecs    []aggSpec            <- CountAs/SumAs/AvgAs/MinAs/MaxAs
  |-- err         error                <- first builder error, surfaced at exec
  |
  |-- [exec: query_exec.go]       Insert / First / Find / Update / Upsert /
  |                               Increment / Decrement / Delete / Count /
  |                               Exists / EnsureIndexes / DropCollection
  |-- [scalar: query_aggregate.go] Sum / Avg / Min / Max / Distinct /
  |                               CountDistinct / Pluck
  |-- [group: query_group.go]     GroupBy / Having / *As / Aggregate
  |-- [stream: query_stream.go]   Cursor / Each  -> *Cursor
  |-- Clone()                     independent deep copy of builder state

Cursor (query_stream.go)               [thin wrapper over *mongo.Cursor]
  |-- cursor *mongo.Cursor
  |-- engine *Engine                   (keeps getMore/killCursors on the session)
  |-- Next / Decode / Current / Err / Close

Filter builder (filter.go)
  |-- condition{field, op, value}, opAliases
  |-- parseWhere / parseWhereMap / normalizeOp / toBetweenPair / likeToRegex
  |-- buildFilter          [conditions + orGroups -> bson.M, $or composition]
  |-- buildConditionFilter [condition slice -> bson.M, $and overflow]
  |-- buildProjection      [fields -> inclusive/exclusive projection]

Naming (naming.go):  CollectionName(value) -> snake_case pluralized name
Logging (log.go):    Error / Errorf / Info / Infof, SetLevel, level constants
```

Query construction and execution flow:

```
engine.Collection("users")            -> &Query{collection, engine}
  .Where(...)/.OrWhere(...)           -> append to conditions / orGroups (in place)
  .OrderBy/.Limit/.Offset/.Select     -> mutate sort / limit / skip / fields
  .Clone()                            -> independent copy (optional branch point)
  .Find(ctx, &out)                    -> terminal
        |
        +-- preflight()               1. nil Query          -> ErrCollectionRequired
        |                             2. q.err              -> builder error
        |                             3. nil collection     -> ErrCollectionRequired
        |                             4. pending group state -> error
        +-- buildFilter()             conditions + orGroups -> bson.M
        +-- findOptions()             sort / limit / skip / projection
        +-- execCtx(ctx)              bind transaction session if the engine has one
        +-- mongo-driver call         Find / FindOne / UpdateMany / ...
```

Grouped aggregation pipeline emitted by `Aggregate`:

```
$match(Where)  ->  $group  ->  $project  ->  $match(Having)  ->  $sort  ->  $skip  ->  $limit
   (omitted        (always)     (drops _id,      (omitted        (only     (only     (only
    if empty)                   surfaces keys     if empty)      if any    if > 0)   if > 0)
                                and aliases)                     OrderBy)
```

## Core types

### Engine

```go
type Engine struct {
    // unexported: client, database, databaseName, session
}
```

The `Engine` has no exported fields and no option type: `NewEngine` takes the
URI and database name positionally and applies no defaults beyond
`options.Client().ApplyURI(uri)`. There is no way to inject custom
`*options.ClientOptions`; construct a `*mongo.Client` yourself only if you are
prepared to bypass this package entirely.

| Symbol       | Signature                                                                                                | Behavior                                                                                                                                                                                                    |
| ------------ | -------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| NewEngine    | `func NewEngine(ctx context.Context, uri string, database string) (*Engine, error)`                      | Validates that `uri` and `database` are non-blank after `strings.TrimSpace`, then `mongo.Connect` + `client.Ping(ctx, nil)`. On ping failure the client is disconnected before returning the error.           |
| Client       | `func (e *Engine) Client() *mongo.Client`                                                                | Underlying driver client. Nil-receiver safe (returns nil).                                                                                                                                                  |
| Database     | `func (e *Engine) Database() *mongo.Database`                                                            | Active database handle. Nil-receiver safe (returns nil).                                                                                                                                                    |
| DatabaseName | `func (e *Engine) DatabaseName() string`                                                                 | Database name as passed to `NewEngine`. Nil-receiver safe (returns `""`).                                                                                                                                   |
| Collection   | `func (e *Engine) Collection(name string) *Query`                                                        | Always returns a non-nil `*Query`. The collection is bound only when the engine, its database, and the trimmed name are all non-empty; otherwise execution methods return `ErrCollectionRequired`.           |
| Model        | `func (e *Engine) Model(value any) *Query`                                                               | Exactly `e.Collection(CollectionName(value))`. A non-struct `value` yields `""` and therefore an unbound Query.                                                                                              |
| Close        | `func (e *Engine) Close(ctx context.Context) error`                                                      | `client.Disconnect(ctx)`. Returns nil when the engine or client is nil.                                                                                                                                     |
| DropDatabase | `func (e *Engine) DropDatabase(ctx context.Context) error`                                               | `database.Drop(ctx)`. Returns nil when the engine or database is nil.                                                                                                                                       |
| NextSequence | `func (e *Engine) NextSequence(ctx context.Context, name string) (int64, error)`                          | `FindOneAndUpdate` with `$inc: {value: 1}`, `SetUpsert(true)`, `SetReturnDocument(options.After)` on the hard-coded `counters` collection keyed by `_id: name`. First call returns 1. Joins an active session. |
| Transaction  | `func (e *Engine) Transaction(ctx context.Context, fn func(sc context.Context, tx *Engine) error) error` | Starts a session, defers `EndSession(ctx)`, runs `fn` inside `session.WithTransaction`. `fn` receives the session context and a sub-Engine carrying the session. Returning nil commits; an error aborts.     |

Error cases that return plain `errors.New` values (not sentinels):
`"mongo uri is required"`, `"mongo database is required"` from `NewEngine`;
`"engine is not initialized"` from `NextSequence` and `Transaction` on a nil
engine or nil database/client; `"sequence name is required"` from
`NextSequence`.

### Query

`Query` has no exported fields and no exported constructor: obtain one from
`Engine.Collection`, `Engine.Model`, or `Query.Clone`. A zero-value `&Query{}`
is usable for building and inspecting filters but has no collection, so every
execution method returns `ErrCollectionRequired`.

Every chainable method mutates the receiver in place and returns the same
`*Query` pointer. See "Builder mutation versus cloning" below; this is the most
important contract in the package.

Condition methods, main AND chain (query.go):

| Method          | Signature                                                                | BSON produced / behavior                                                                                                                                                                                                                            |
| --------------- | ------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Where           | `func (q *Query) Where(args ...any) *Query`                              | 1 arg: `bson.M`/`map[string]any`, one equality condition per key, keys processed in `sort.Strings` order, a nil value becoming a null check. 2 args: `(field, value)` equality; nil value becomes a null check. 3 args: `(field, op, value)`. Invalid input records a builder error. |
| WhereNot        | `func (q *Query) WhereNot(field string, value any) *Query`               | `{field: {$ne: value}}`.                                                                                                                                                                                                                            |
| WhereIn         | `func (q *Query) WhereIn(field string, values any) *Query`               | `{field: {$in: values}}`. `values` is stored by reference and marshaled by the driver at execution time.                                                                                                                                             |
| WhereNotIn      | `func (q *Query) WhereNotIn(field string, values any) *Query`            | `{field: {$nin: values}}`.                                                                                                                                                                                                                          |
| WhereNull       | `func (q *Query) WhereNull(field string) *Query`                         | `{field: nil}` (a BSON null equality, which also matches missing fields).                                                                                                                                                                           |
| WhereNotNull    | `func (q *Query) WhereNotNull(field string) *Query`                      | `{field: {$ne: nil}}`.                                                                                                                                                                                                                              |
| WhereBetween    | `func (q *Query) WhereBetween(field string, low any, high any) *Query`   | `{field: {$gte: low, $lte: high}}`, inclusive on both ends.                                                                                                                                                                                         |
| WhereNotBetween | `func (q *Query) WhereNotBetween(field string, low any, high any) *Query`| `{field: {$not: {$gte: low, $lte: high}}}`.                                                                                                                                                                                                         |
| WhereLike       | `func (q *Query) WhereLike(field string, pattern string) *Query`         | `{field: {$regex: primitive.Regex{Pattern: "^...$"}}}` from `likeToRegex`. Case-sensitive.                                                                                                                                                           |
| WhereILike      | `func (q *Query) WhereILike(field string, pattern string) *Query`        | Same as `WhereLike` with `Options: "i"`. Case-insensitive.                                                                                                                                                                                          |

Or-branch methods; each call appends exactly one `$or` branch (query.go):

| Method         | Signature                                                                  | Behavior                                                                                                                                                                            |
| -------------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| OrWhere        | `func (q *Query) OrWhere(args ...any) *Query`                              | Same argument forms as `Where`. The object form yields one branch whose keys AND-combine (Knex semantics). An empty map produces zero conditions and is skipped, because an empty branch would match every document. |
| OrWhereNot     | `func (q *Query) OrWhereNot(field string, value any) *Query`               | Single-condition branch with `$ne`.                                                                                                                                                 |
| OrWhereIn      | `func (q *Query) OrWhereIn(field string, values any) *Query`               | Single-condition branch with `$in`.                                                                                                                                                 |
| OrWhereNotIn   | `func (q *Query) OrWhereNotIn(field string, values any) *Query`            | Single-condition branch with `$nin`.                                                                                                                                                |
| OrWhereNull    | `func (q *Query) OrWhereNull(field string) *Query`                         | Single-condition branch with a null equality.                                                                                                                                       |
| OrWhereNotNull | `func (q *Query) OrWhereNotNull(field string) *Query`                      | Single-condition branch with `{$ne: nil}`.                                                                                                                                          |
| OrWhereBetween | `func (q *Query) OrWhereBetween(field string, low any, high any) *Query`   | Single-condition branch with `{$gte, $lte}`.                                                                                                                                        |

There is no `OrWhereLike`, `OrWhereILike`, or `OrWhereNotBetween`. Use
`OrWhere(field, "ilike", pattern)` or `OrWhere(field, "not between", []any{lo, hi})`
to reach those operators through the generic form.

Modifier methods (query.go):

| Method  | Signature                                                           | Behavior                                                                                                                                                        |
| ------- | ------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| OrderBy | `func (q *Query) OrderBy(field string, direction ...string) *Query` | Appends a `bson.E` to `sort`, preserving call order. `direction[0]` is trimmed and compared with `strings.EqualFold` against `"desc"` for -1; anything else (including no argument and extra arguments beyond the first) yields 1. |
| Limit   | `func (q *Query) Limit(n int64) *Query`                             | Stores `n` unconditionally; execution applies `SetLimit` only when `n > 0`, so `Limit(0)` and negative values mean "no limit".                                   |
| Offset  | `func (q *Query) Offset(n int64) *Query`                            | Stores `n` unconditionally; execution applies `SetSkip` only when `n > 0`.                                                                                       |
| Select  | `func (q *Query) Select(fields ...string) *Query`                   | Replaces the projection field list wholesale. Inclusive by default; a `-` prefix excludes. Assigns the variadic slice directly, so `Select(mySlice...)` aliases `mySlice`. |
| Clone   | `func (q *Query) Clone() *Query`                                    | Independent copy of all builder state. Returns nil for a nil receiver. Shares the `collection` and `engine` pointers, which is intentional and safe.             |

Execution methods (query_exec.go):

| Method         | Signature                                                                                           | Behavior                                                                                                                                                                                                                                                                            |
| -------------- | --------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Insert         | `func (q *Query) Insert(ctx context.Context, documents ...any) (InsertResult, error)`               | A single slice/array argument is expanded into individual documents (`bson.D` and `[]byte`-kind element slices excluded). Exactly one document uses `InsertOne`, two or more use `InsertMany`. On partial `InsertMany` failure the successfully inserted IDs are returned alongside the error. Zero documents after expansion returns `"at least one document is required"`. |
| First          | `func (q *Query) First(ctx context.Context, out any) error`                                         | `FindOne` honoring sort, offset, and projection. `Limit` is irrelevant. Returns `ErrNotFound` when nothing matches.                                                                                                                                                                  |
| Find           | `func (q *Query) Find(ctx context.Context, out any) error`                                          | `Find` + `cursor.All(ctx, out)`; loads the whole result set. The cursor is closed via `defer`. Honors sort, limit, offset, projection.                                                                                                                                               |
| Update         | `func (q *Query) Update(ctx context.Context, update any) (int64, error)`                            | `UpdateMany`. Returns `MatchedCount`, not `ModifiedCount`. Plain documents are auto-wrapped in `$set` by `normalizeUpdate`.                                                                                                                                                          |
| Upsert         | `func (q *Query) Upsert(ctx context.Context, update any) (UpsertResult, error)`                     | `UpdateMany` with `SetUpsert(true)`. When nothing matches, MongoDB derives the new document from the filter's equality fields plus the update.                                                                                                                                       |
| Increment      | `func (q *Query) Increment(ctx context.Context, field string, amount ...int64) (int64, error)`      | Delegates to `Update(ctx, bson.M{"$inc": bson.M{field: n}})` with `n = amount[0]` or 1. Returns the matched count.                                                                                                                                                                   |
| Decrement      | `func (q *Query) Decrement(ctx context.Context, field string, amount ...int64) (int64, error)`      | Delegates to `Update` with `$inc: {field: -n}`. Returns the matched count.                                                                                                                                                                                                          |
| Delete         | `func (q *Query) Delete(ctx context.Context) (int64, error)`                                        | `DeleteMany`. Returns `DeletedCount`.                                                                                                                                                                                                                                               |
| Count          | `func (q *Query) Count(ctx context.Context) (int64, error)`                                         | `CountDocuments` with the built filter only. `Limit`, `Offset`, and `OrderBy` are ignored.                                                                                                                                                                                           |
| Exists         | `func (q *Query) Exists(ctx context.Context) (bool, error)`                                         | `count, err := q.Count(ctx); return count > 0, err`. The error is returned together with the boolean, so check it.                                                                                                                                                                  |
| EnsureIndexes  | `func (q *Query) EnsureIndexes(ctx context.Context, indexes []mongo.IndexModel) ([]string, error)`  | `Indexes().CreateMany`. Returns `(nil, nil)` when `indexes` is empty, after preflight.                                                                                                                                                                                              |
| DropCollection | `func (q *Query) DropCollection(ctx context.Context) error`                                         | `collection.Drop(ctx)`.                                                                                                                                                                                                                                                             |

Scalar aggregation and extraction methods (query_aggregate.go):

| Method        | Signature                                                                         | Behavior                                                                                                                                                                                                                                                                     |
| ------------- | --------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Sum           | `func (q *Query) Sum(ctx context.Context, field string) (float64, error)`          | Pipeline `[{$match: filter}, {$group: {_id: nil, result: {$sum: "$field"}}}]`. Returns `(0, nil)` when no documents match. Ignores sort, limit, and offset.                                                                                                                    |
| Avg           | `func (q *Query) Avg(ctx context.Context, field string) (float64, error)`          | Same pipeline with `$avg`. Returns `(0, nil)` when no documents match.                                                                                                                                                                                                        |
| Min           | `func (q *Query) Min(ctx context.Context, field string) (float64, error)`          | Same pipeline with `$min`. Non-numeric results fail while decoding into `float64`.                                                                                                                                                                                            |
| Max           | `func (q *Query) Max(ctx context.Context, field string) (float64, error)`          | Same pipeline with `$max`. Non-numeric results fail while decoding into `float64`.                                                                                                                                                                                            |
| Distinct      | `func (q *Query) Distinct(ctx context.Context, field string) ([]any, error)`       | `collection.Distinct(ctx, field, filter)`. Callers must type-assert elements. Ignores sort, limit, and offset.                                                                                                                                                                 |
| CountDistinct | `func (q *Query) CountDistinct(ctx context.Context, field string) (int64, error)`  | `int64(len(values))` from `Distinct`. Materializes all distinct values on the client.                                                                                                                                                                                          |
| Pluck         | `func (q *Query) Pluck(ctx context.Context, field string, out any) error`          | Streams matching documents with projection `{field: 1, "_id": 0}` (the `_id: 0` is omitted when `field == "_id"`) and appends `bson.Raw.Lookup(path...)` values into `out`. `out` must be a non-nil pointer to a slice; blank `field` errors. Honors sort, limit, offset. Dotted paths are split on `.`. Documents lacking the field contribute the element zero value. Does not mutate `q.fields`. |

Grouped aggregation methods (query_group.go):

| Method    | Signature                                                       | Behavior                                                                                                                                                                            |
| --------- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| GroupBy   | `func (q *Query) GroupBy(fields ...string) *Query`              | Appends group keys. A blank field records a builder error and appends nothing from that call. Each key surfaces in result rows under its flattened name; dots become underscores (`addr.city` -> `addr_city`). |
| Having    | `func (q *Query) Having(args ...any) *Query`                     | Filters grouped rows. Accepts the same argument forms as `Where`, but field names must be result column names (flattened group keys or accumulator aliases).                         |
| CountAs   | `func (q *Query) CountAs(alias string) *Query`                   | Per-group `{$sum: 1}` under `alias`. Internally an `aggSpec` with an empty field.                                                                                                    |
| SumAs     | `func (q *Query) SumAs(field string, alias string) *Query`        | Per-group `{$sum: "$field"}` under `alias`. Blank `field` records a builder error.                                                                                                   |
| AvgAs     | `func (q *Query) AvgAs(field string, alias string) *Query`        | Per-group `{$avg: "$field"}` under `alias`.                                                                                                                                          |
| MinAs     | `func (q *Query) MinAs(field string, alias string) *Query`        | Per-group `{$min: "$field"}` under `alias`.                                                                                                                                          |
| MaxAs     | `func (q *Query) MaxAs(field string, alias string) *Query`        | Per-group `{$max: "$field"}` under `alias`.                                                                                                                                          |
| Aggregate | `func (q *Query) Aggregate(ctx context.Context, out any) error`   | Uses `preflightBase` (so pending group state is allowed), builds the pipeline, runs `collection.Aggregate`, and decodes all rows via `cursor.All(ctx, out)`. `out` must be a pointer to a slice. |

Note the parameter order of the `*As` methods: field first, alias second.
`CountAs` takes only an alias.

Alias validation happens at call time and records a builder error: the alias
must be non-blank, must not equal `_id`, must not start with `$`, and must not
contain `.`. Pipeline-level validation happens inside `Aggregate` and is
returned directly rather than stored: `GroupBy` is required, `Select` cannot be
combined with `GroupBy`, flattened group keys must be unique, aliases must not
collide with group keys or with each other, and every `Having` field and
`OrderBy` key must be a known result column.

Streaming methods (query_stream.go):

| Method | Signature                                                                   | Behavior                                                                                                                                                                                    |
| ------ | --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Cursor | `func (q *Query) Cursor(ctx context.Context) (*Cursor, error)`               | Runs `Find` with the same `findOptions` as `Find` (sort, limit, offset, projection) and wraps the driver cursor. The caller owns closing it.                                                 |
| Each   | `func (q *Query) Each(ctx context.Context, fn func(c *Cursor) error) error`  | Opens a cursor, defers `Close(ctx)`, calls `fn` per document, returns the first `fn` error immediately, otherwise returns `cursor.Err()`. `fn` receives the `*Cursor` itself, not a decoded value. |

### Cursor

```go
type Cursor struct {
    // unexported: cursor *mongo.Cursor, engine *Engine
}
```

| Method  | Signature                                           | Behavior                                                                              |
| ------- | --------------------------------------------------- | ------------------------------------------------------------------------------------- |
| Next    | `func (c *Cursor) Next(ctx context.Context) bool`   | Advances one document; false at end of stream or on error. Check `Err` afterwards.     |
| Decode  | `func (c *Cursor) Decode(out any) error`            | Unmarshals the current document into `out`. Does not bind the session (no I/O).        |
| Current | `func (c *Cursor) Current() bson.Raw`               | Raw BSON of the current document, valid until the next `Next` call.                    |
| Err     | `func (c *Cursor) Err() error`                      | The error that terminated iteration, if any.                                          |
| Close   | `func (c *Cursor) Close(ctx context.Context) error` | Releases the server-side cursor. Safe to call after a partial iteration.               |

`Next` and `Close` route ctx through `Cursor.bind`, which applies
`Engine.sessionContext`, keeping `getMore` and `killCursors` inside the
transaction when the cursor was opened through a `Transaction` sub-Engine.

A `Cursor` value is only valid when produced by `Query.Cursor` or `Query.Each`.
A zero-value `Cursor{}` has a nil inner `*mongo.Cursor` and every method
dereferences it, so it panics; the type is not usable standalone.

### InsertResult

```go
type InsertResult struct {
    InsertedIDs   []any
    InsertedCount int64
}
```

`InsertedIDs` holds the driver-reported `_id` values (server-generated
`primitive.ObjectID` values when the documents carried none). `InsertedCount`
is `len(InsertedIDs)`, computed locally rather than reported by the server. On
a partial `InsertMany` failure, both fields describe the documents that were
inserted and are returned alongside a non-nil error, so inspect the result even
on error.

### UpsertResult

```go
type UpsertResult struct {
    MatchedCount  int64
    ModifiedCount int64
    UpsertedCount int64
    UpsertedID    any
}
```

A straight copy of the driver's `*mongo.UpdateResult`. This is the only place
the package exposes `ModifiedCount`; `Update` discards it.

### Sentinel errors

```go
var ErrCollectionRequired = errors.New("collection is required before query execution")

// ErrNotFound aliases mongo.ErrNoDocuments so callers need not import the driver.
var ErrNotFound = mongo.ErrNoDocuments
```

`ErrCollectionRequired` is returned by every execution method when the receiver
is nil or no collection is bound (`engine.Collection("")`, `engine.Model(42)`,
a nil engine, or an engine with a nil database). `ErrNotFound` is returned by
`First` when nothing matches; because it is a variable alias,
`errors.Is(err, swifty_orm.ErrNotFound)` and
`errors.Is(err, mongo.ErrNoDocuments)` are both true.

No other error is a sentinel. Every remaining error is either a fresh
`errors.New`/`fmt.Errorf` value with no `%w` verb, or a driver error returned
verbatim. There is no error wrapping anywhere in the package, so match driver
errors with driver-provided helpers (for example
`mongo.IsDuplicateKeyError(err)`) rather than against package sentinels.

### CollectionName

```go
func CollectionName(value any) string
```

Derives a collection name by reflection: unwrap pointers repeatedly, then if
the result is a slice or array, take the element type and unwrap its pointers,
then require `reflect.Struct` (returning `""` for anything else), then
`pluralize(toSnake(typ.Name()))`. Examples verified by tests: `&testUser{}` ->
`test_users`, `[]testCity{}` -> `test_cities`, `42` -> `""`. Additional
examples from the algorithm: `User` -> `users`, `ChatHistory` ->
`chat_histories`, `Category` -> `categories`, `Address` -> `addresses`.

### Logging

```go
const (
    InfoLevel  = iota // 0
    ErrorLevel        // 1
    Disabled          // 2
)

var (
    Error  = errorLogger.Println // stderr, red "[error]" prefix
    Errorf = errorLogger.Printf
    Info   = infoLogger.Println  // stdout, blue "[info ]" prefix
    Infof  = infoLogger.Printf
)

func SetLevel(level int)
```

The constants are untyped integer constants, so `SetLevel` accepts any `int`.
Both loggers are created with `log.LstdFlags|log.Lshortfile` and ANSI-colored
prefixes. `SetLevel` takes a package-level mutex, resets both loggers to
`os.Stdout`, restores the error logger to `os.Stderr`, then redirects to
`io.Discard` any logger whose level is below the requested one: `InfoLevel`
enables both, `ErrorLevel` keeps errors only, `Disabled` silences both. Levels
above `Disabled` behave like `Disabled`.

`Error`, `Errorf`, `Info`, and `Infof` are method values bound at package
initialization; the underlying `*log.Logger` values are unexported and cannot
be replaced. The package itself never calls these helpers, so no ORM operation
produces log output. They exist purely as a convenience for application code.

## Filter DSL

### Accepted Where forms and the BSON they produce

```go
q.Where(bson.M{"a": 1, "b": nil})   // {a: 1, b: nil}          (keys sorted, one equality each)
q.Where(map[string]any{"a": 1})     // {a: 1}
q.Where("age", 18)                  // {age: 18}
q.Where("deleted_at", nil)          // {deleted_at: nil}       (2-arg nil becomes a null check)
q.Where("age", ">=", 18)            // {age: {$gte: 18}}
q.Where("tags", "in", []string{"x"})// {tags: {$in: ["x"]}}
q.Where("name", "like", "Tom%")     // {name: {$regex: /^Tom.*$/}}
q.Where("age", "between", []int{1, 9})     // {age: {$gte: 1, $lte: 9}}
q.Where("age", "not between", []int{1, 9}) // {age: {$not: {$gte: 1, $lte: 9}}}
q.Where("meta", "$exists", true)    // {meta: {$exists: true}}  ("$" ops pass through)
```

Rejected forms, each recording a builder error surfaced at execution:

```go
q.Where()                        // "where: expected 1 (map), 2 or 3 arguments, got 0"
q.Where(123, "x")                // field must be a non-empty string
q.Where("  ", "x")               // blank field after TrimSpace
q.Where("f", 1, 2)               // operator must be a string
q.Where("f", "bogus", 1)         // unsupported operator
q.Where("f", "like", 42)         // like requires a string pattern
q.Where("f", "between", 5)       // between requires a 2-element slice or array
q.Where([]string{"a"})           // single argument must be bson.M or map[string]any
```

### Operator translation

`Where(field, op, value)` lowercases and trims `op`, then resolves it through
`opAliases`:

```
=  ==             -> "="          equality
!=  <>            -> $ne
>                 -> $gt
>=                -> $gte
<                 -> $lt
<=                -> $lte
in                -> $in
not in  nin       -> $nin
like              -> anchored $regex, case-sensitive
ilike             -> anchored $regex with option "i"
between           -> $gte + $lte      (value: any 2-element slice or array, or [2]any)
not between       -> $not: {$gte, $lte}
```

An unrecognized operator that starts with `$` passes through to MongoDB
verbatim, using the raw (untrimmed, unlowercased) string as the key. Any other
unrecognized operator records `where: unsupported operator %q`, so a broken
filter never reaches the server. The internal canonical ops beyond MongoDB
operators are `"="`, `"null"`, `"notNull"`, `"between"`, `"notBetween"`,
`"like"`, and `"ilike"`.

`toBetweenPair` accepts `[2]any` directly, otherwise any slice or array of
length exactly 2 via reflection, and normalizes it to `[2]any`.

`likeToRegex` runs `regexp.QuoteMeta` over the pattern, then replaces `%` with
`.*` and `_` with `.`, and anchors the result with `^` and `$`. Verified
mappings: `"tom"` -> `"^tom$"`, `"%tom%"` -> `"^.*tom.*$"`, `"to_m"` ->
`"^to.m$"`, `"a.b%"` -> `"^a\\.b.*$"`, `"100%"` -> `"^100.*$"`. Because
`QuoteMeta` runs first and no escape syntax exists, a literal `%` or `_` in the
data cannot be matched literally.

### AND composition without silent loss

`buildConditionFilter` folds a condition slice into one `bson.M`, tracking
which fields hold an operator map that it created (as opposed to a
user-supplied equality value that happens to be a `bson.M` sub-document, which
it never mutates):

- Equality on a free field sets `field: value`.
- Equality on a field that already holds a builder-created operator map merges
  in as `$eq`, unless `$eq` is already present.
- An operator on a field that already holds a builder-created operator map
  merges into that map, unless the same operator key is already present.
- Anything that cannot merge (duplicate equality, operator after equality,
  duplicate operator) is appended to a top-level `$and` array of `bson.M`
  instead of overwriting.

Verified outcomes:

```go
Where("age", ">", 18).Where("age", "<", 30)  // {age: {$gt: 18, $lt: 30}}
Where("age", ">", 10).Where("age", 18)       // {age: {$gt: 10, $eq: 18}}
Where("age", 18).Where("age", ">", 10)       // {age: 18, $and: [{age: {$gt: 10}}]}
Where("a", 1).Where("a", 2)                  // {a: 1, $and: [{a: 2}]}
```

The invariant is that no condition is ever dropped or overwritten. If it were
violated, `Where("age", 18).Where("age", ">", 10)` would silently return
documents that satisfy only one of the two predicates.

### Or-group composition

`buildFilter` returns `buildConditionFilter(q.conditions)` when `orGroups` is
empty. Otherwise it assembles branches: the main-chain filter becomes a branch
only when `len(q.conditions) > 0`, then each non-empty or-group becomes a
branch. A single resulting branch is returned unwrapped rather than as
`{$or: [x]}`; two or more become `{$or: [...]}`.

Consequences, all covered by tests:

- An `OrWhere`-only query never degenerates to match-all: `OrWhere("name","Tom").OrWhere("name","Amy")`
  produces `{$or: [{name: "Tom"}, {name: "Amy"}]}` with no empty base branch,
  and `OrWhere("name","Nobody").Delete(ctx)` deletes 0 documents rather than
  the whole collection.
- A single `OrWhere` acts exactly like `Where`.
- `OrWhere(bson.M{})` is a no-op; the branch is never appended.
- A `Where` added after an `OrWhere` joins the main AND chain, not the last
  branch: `Where(a).OrWhere(b).Where(c)` means `(a AND c) OR b`.

`buildProjection` returns nil when `fields` is empty, so no projection option is
set. Otherwise each entry maps to 1, or the name after a `-` prefix maps to 0.

## Grouped aggregation

`buildGroupPipeline` validates first, then emits stages. Validation order:

1. `GroupBy` must have at least one key, else `"aggregate: GroupBy is required"`.
2. `Select` must not be set, else `"aggregate: Select cannot be combined with GroupBy; ..."`.
3. Flattened group keys must be unique, so `GroupBy("addr.city", "addr_city")`
   and `GroupBy("city", "city")` both fail.
4. Each alias must not collide with a group key and must not repeat.
5. Every `Having` condition field must be a known result column.
6. Every `OrderBy` key must be a known result column, so `GroupBy("addr.city").OrderBy("addr.city")`
   fails and `GroupBy("addr.city").OrderBy("addr_city")` succeeds.

Stage emission:

- `$match` with the `Where` filter, omitted when the filter is empty.
- `$group` with `_id: "$field"` for a single group key, or
  `_id: {flatKey: "$field", ...}` for several. Accumulators come from
  `aggSpecs`: an empty spec field means `{$sum: 1}`, otherwise `{op: "$field"}`.
- `$project` with `_id: 0`, each flattened group key mapped from `$_id` (single
  key) or `$_id.flatKey` (multiple keys), and each alias set to 1.
- `$match` with the `Having` filter, built by the same `buildConditionFilter`,
  omitted when empty.
- `$sort` with the `bson.D` from `OrderBy`, only when non-empty.
- `$skip` and `$limit`, each only when the stored value is greater than 0.

A fully populated query therefore yields exactly 7 stages. Because grouping
happens before `$sort`/`$skip`/`$limit`, ordering and pagination apply to the
grouped rows, and `Where` filters documents while `Having` filters rows.

## Streaming and cursor lifecycle

`Query.Cursor` hands ownership of a server-side cursor to the caller. Skipping
`Close` leaks that cursor on the server until the server-side idle timeout
expires (10 minutes by default), and leaves the driver connection accounted to
the cursor. Always `defer cursor.Close(ctx)` immediately after a successful
`Cursor` call.

`Query.Each` owns the cursor itself: it defers `Close(ctx)` and returns either
the first error from `fn` or `cursor.Err()`. Returning a sentinel error from
`fn` is the supported way to stop iteration early; the cursor is still closed.

`Find` and `Aggregate` also open cursors internally but close them via `defer`
before returning, so callers never see them.

Neither `Cursor` nor `Query` starts a goroutine. The only goroutines involved
belong to the mongo driver's topology and connection pool machinery, created by
`mongo.Connect` inside `NewEngine` and released by `Engine.Close`. Skipping
`Close` leaks those monitoring goroutines and their sockets for the lifetime of
the process.

## Struct tags and field naming

swifty_orm implements exactly one naming rule of its own: `CollectionName` in
`naming.go`, which maps a Go type name to a collection name. Everything about
document field naming is delegated to the mongo driver's BSON codec; this
package neither reads nor rewrites `bson` struct tags, and there is no
registry, hook, or interface for overriding field names.

`CollectionName` algorithm, exactly as implemented:

1. `reflect.TypeOf(value)`, then unwrap `reflect.Pointer` in a loop.
2. Return `""` if the type is nil (a nil `any`).
3. If the type is a slice or array, replace it with its element type and unwrap
   pointers again (one level of slice only; `[][]User` yields `[]User`, which is
   not a struct, so `""`).
4. Return `""` if the result is not `reflect.Struct`.
5. Return `pluralize(toSnake(typ.Name()))`.

`toSnake` inserts `_` before every uppercase ASCII letter past index 0, then
lowercases the whole string. It has no acronym handling, so `HTTPServer`
becomes `h_t_t_p_server`. Non-ASCII uppercase letters are not detected because
the comparison is `r >= 'A' && r <= 'Z'`.

`pluralize` applies the first matching rule:

1. Empty string returns empty.
2. Ends in `y` with length at least 2 and a non-vowel before the `y`: replace
   `y` with `ies` (`city` -> `cities`, `category` -> `categories`).
3. Ends in `s`, `x`, `sh`, or `ch`: append `es` (`bus` -> `buses`,
   `address` -> `addresses`, `fox` -> `foxes`, `dish` -> `dishes`,
   `match` -> `matches`).
4. Otherwise append `s` (`user` -> `users`, `day` -> `days`, `key` -> `keys`,
   `boy` -> `boys`).

Irregular plurals are not handled, so `Person` becomes `persons` and `Datum`
becomes `datums`. Anonymous struct types have an empty `Name()`, so
`CollectionName(struct{}{})` returns `""` and yields an unbound Query.

Field naming is driver behavior, not swifty_orm behavior, and matters because
the builder takes field names as raw strings that must match stored keys:

- Without a `bson` tag, the driver uses the lowercased Go field name with no
  underscore insertion, so `CreatedAt` maps to `createdat`, which will not
  match a `Where("created_at", ...)` filter. Tag every field explicitly.
- The primary key is only `_id` when tagged `bson:"_id"`; an untagged `ID`
  field maps to `id` and MongoDB adds a separate generated `_id`.
- Embedded structs are flattened only with `bson:",inline"`; otherwise they
  become a nested sub-document under the embedded type's lowercased name, and
  filters must use dotted paths.
- `omitempty` on an update struct drops zero-valued fields from the generated
  `$set`, which changes what `Update` writes.

Discrepancy to be aware of: `naming.go` is sometimes assumed to implement
struct-tag mapping, acronym-aware field naming, embedded-struct flattening, and
`_id` handling. It implements none of those. Only `CollectionName`, `toSnake`,
`pluralize`, and `isVowel` live there, and only `CollectionName` is exported.

## Internal implementation details affecting correctness

### Builder mutation versus cloning

Every chainable method on `*Query` mutates the receiver and returns the same
pointer. There is no copy-on-write. The invariant is: a `*Query` value has
exactly one owner, and any chain you build off it modifies it permanently.

What breaks when that invariant is violated:

```go
base := engine.Collection("users").Where("age", ">=", 18)

// WRONG: both calls mutate the same Query. The second Count sees
// WhereNotNull("email") because the first chain appended it to base.
withEmail, _ := base.WhereNotNull("email").Count(ctx)
total, _ := base.Count(ctx) // still filtered by email

// CORRECT: branch from an explicit clone per variant.
withEmail, _ = base.Clone().WhereNotNull("email").Count(ctx)
total, _ = base.Clone().Count(ctx)
```

`Clone` produces genuinely independent state. It copies `collection`, `engine`,
`limit`, `skip`, and `err` by value, and reallocates `conditions`, `sort`,
`fields`, `groupFields`, `havingConds`, and `aggSpecs` with `append(nil, ...)`.
`orGroups` gets a fresh outer slice plus a fresh copy of every inner branch, so
appending to a clone's branch list or to any branch cannot reach the original.
Tests verify both directions for the condition/or/sort/field state and for the
group/having/alias state.

Three aliasing details `Clone` does not and cannot fix, because the values are
held by reference:

- `collection` and `engine` are shared pointers by design; both are safe for
  concurrent use by the driver.
- `Select(fields...)` stores the caller's slice, so `Select(mySlice...)`
  followed by mutating `mySlice` changes the projection of the original and
  every prior clone that shared it.
- Condition values are stored as `any` without copying. `WhereIn("id", ids)`
  keeps a reference to `ids`; mutating `ids` before execution changes the
  filter, in the original and in every clone.

A partially built `Query` is also unsafe to stash on a struct and reuse across
requests, both because of mutation aliasing and because it pins a collection
handle and, potentially, a transaction session.

### Builder error accumulation and preflight

`setErr` records only the first error, so the earliest mistake in a chain wins
and later valid calls do not mask it. `Where`, `OrWhere`, and `Having` record
parse errors; `GroupBy` records blank-field errors; `addAggSpec` and
`addFieldAggSpec` record alias and field errors. The direct-append condition
methods (`WhereNot`, `WhereIn`, `WhereNotIn`, `WhereNull`, `WhereNotNull`,
`WhereBetween`, `WhereNotBetween`, `WhereLike`, `WhereILike`, and every
`OrWhere*` variant) validate nothing at all, so a blank field name silently
produces a filter keyed on `""`.

`preflightBase` checks, in this exact order:

1. Nil receiver -> `ErrCollectionRequired`.
2. `q.err != nil` -> that builder error.
3. `q.collection == nil` -> `ErrCollectionRequired`.

The builder error therefore outranks the missing collection, which the test
suite asserts explicitly. `preflight` calls `preflightBase` and then rejects
pending group state with `"GroupBy/Having/aggregation aliases are only
supported by Aggregate"`. Every execution method except `Aggregate` uses
`preflight`; `Aggregate` uses `preflightBase`. `Increment`, `Decrement`,
`Exists`, and `CountDistinct` inherit preflight through the method they
delegate to.

### Update normalization

`normalizeUpdate` wraps a plain document in `{$set: doc}` when it contains no
`$`-prefixed key. Handled inputs: `bson.M`, `map[string]any`, `bson.D` (scanned
key by key), and, via reflection after unwrapping non-nil pointers, any struct.
A nil pointer and every other type pass through unchanged. Verified: plain
`bson.M`, plain `map[string]any`, plain `bson.D`, a struct, and a pointer to a
struct all get wrapped; `bson.M{"$inc": ...}` and `bson.D{{"$inc", 1}}` pass
through. An empty `bson.M{}` still gets wrapped into `{$set: {}}`, which the
server rejects.

### Insert expansion

`expandInsertDocs` only acts when `len(documents) == 1`. It returns the input
unchanged for a `bson.D`, for a non-slice/array value, and for a slice whose
element kind is `reflect.Uint8`. Otherwise it flattens the slice or array into
one element per document, so `Insert(ctx, []*User{...})` and
`Insert(ctx, []User{...})` both work without manual conversion. Multi-argument
calls always pass through.

### Lock scope and concurrency safety

The package contains exactly one lock: the package-level `sync.Mutex` in
`log.go`, which guards `SetLevel` only. Nothing else in the package is
synchronized.

- `*Engine` is safe for concurrent use by multiple goroutines. Its fields are
  written only at construction, and `*mongo.Client` and `*mongo.Database` are
  themselves concurrency-safe. Sharing one `Engine` process-wide is the intended
  pattern.
- The transaction sub-`Engine` is not safe to use from multiple goroutines,
  because a `mongo.Session` does not support concurrent operations. Keep all
  work inside the `Transaction` callback sequential.
- `*Query` is not safe for concurrent use. Chainable methods perform unguarded
  `append` on shared slices, so concurrent building is a data race. Concurrent
  execution of the same fully built `Query` is also unsafe in principle,
  because execution reads state that any other goroutine may still be mutating.
  Give each goroutine its own `Clone`.
- `*Cursor` is not safe for concurrent use; `Next`/`Decode`/`Current` form a
  stateful sequence over a single driver cursor.
- `SetLevel` is safe to call concurrently with itself, but the `Error`, `Errorf`,
  `Info`, and `Infof` package variables are plain vars: reassigning them while
  another goroutine calls them is a race.

### Context and timeout propagation

The package sets no timeouts of its own and never calls `context.WithTimeout`.
Every ctx a caller passes goes straight to the driver, optionally after session
binding, so all deadline and cancellation behavior comes from the caller.
`NewEngine` uses its ctx for `mongo.Connect` and `Ping` only; it is not
retained, so an expired construction context does not affect later queries.
`Engine.Close` and `DropDatabase` take their own ctx, which matters at shutdown:
passing an already-cancelled request context makes `Disconnect` fail.

### Transaction session propagation

`Transaction` builds a sub-`Engine` that shares the client, database, and name
but additionally holds the session. Every execution path calls `Query.execCtx`,
which delegates to `Engine.sessionContext`: when the engine holds a session and
the incoming ctx does not already carry one (`mongo.SessionFromContext(ctx) ==
nil`), the session is bound with `mongo.NewSessionContext`. Queries issued
through `tx` therefore join the transaction even when the plain outer ctx is
passed instead of `sc`, which the test suite verifies with a rollback assertion.
Prefer `sc` regardless, for correct deadline and cancellation semantics.
`NextSequence` and `Cursor.Next`/`Cursor.Close` bind the session the same way.

`Transaction` defers `session.EndSession(ctx)`, so the sub-`Engine` and any
`Query` or `Cursor` derived from it are invalid after the callback returns. Do
not capture `tx` in a closure that outlives the callback.

### Zero values and omitted arguments

- `&Query{}`: builds and inspects filters fine, but every execution method
  returns `ErrCollectionRequired`. This is how the unit tests exercise the
  filter and pipeline builders without a server.
- `&Engine{}`: `Client`, `Database`, `DatabaseName` return zero values;
  `Collection`/`Model` return unbound queries; `NextSequence` and `Transaction`
  return `"engine is not initialized"`; `Close` and `DropDatabase` are no-ops.
- A nil `*Engine`: all of the above hold, since every method checks `e == nil`.
- A nil `*Query`: `Clone` returns nil; execution methods return
  `ErrCollectionRequired`.
- `OrderBy(field)` with no direction: ascending.
- `Increment(ctx, field)` / `Decrement(ctx, field)` with no amount: 1.
- `Limit(0)` / `Offset(0)`: no limit and no skip applied.
- `EnsureIndexes(ctx, nil)`: no-op returning `(nil, nil)` after preflight.

## Typical usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/hangtiancheng/swifty.go/swifty_orm"
    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
)

type User struct {
    ID        int64     `bson:"_id"`
    Name      string    `bson:"name"`
    Email     string    `bson:"email"`
    Age       int       `bson:"age"`
    CreatedAt time.Time `bson:"created_at"`
}

type Order struct {
    ID     int64   `bson:"_id"`
    City   string  `bson:"city"`
    Status string  `bson:"status"`
    Amount float64 `bson:"amount"`
}

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    engine, err := swifty_orm.NewEngine(ctx, "mongodb://localhost:27017", "my-app")
    if err != nil {
        log.Fatal(err)
    }
    // ... see the shutdown example below for a non-cancelled Close context.

    // Insert: variadic documents or a single slice argument (auto-expanded).
    users := []*User{
        {ID: 1, Name: "Alice", Email: "alice@example.com", Age: 30, CreatedAt: time.Now()},
        {ID: 2, Name: "Bob", Email: "bob@example.com", Age: 25, CreatedAt: time.Now()},
    }
    res, err := engine.Collection("users").Insert(ctx, users)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("inserted:", res.InsertedCount, res.InsertedIDs)

    // Chainable predicates, ordering, pagination, projection.
    var adults []User
    err = engine.Model(&User{}).
        Where("age", ">=", 18).
        WhereNotNull("email").
        WhereLike("name", "A%").
        OrderBy("created_at", "desc").
        Limit(10).
        Offset(0).
        Select("name", "email", "-_id").
        Find(ctx, &adults)
    if err != nil {
        log.Fatal(err)
    }

    // Object form plus an Or-branch: (age == 30) OR (name in [...]).
    var mixed []User
    err = engine.Collection("users").
        Where(bson.M{"age": 30}).
        OrWhereIn("name", []string{"Bob", "Carol"}).
        Find(ctx, &mixed)
    if err != nil {
        log.Fatal(err)
    }

    // Update returns the matched count; plain documents auto-wrap in $set.
    matched, err := engine.Collection("users").
        Where("_id", int64(1)).
        Update(ctx, bson.M{"name": "Alice Updated"})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("matched:", matched)

    // Upsert exposes ModifiedCount and UpsertedID, which Update discards.
    ur, err := engine.Collection("users").Where("_id", int64(3)).
        Upsert(ctx, bson.M{"name": "Carol", "age": 40})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(ur.MatchedCount, ur.ModifiedCount, ur.UpsertedCount, ur.UpsertedID)

    if _, err := engine.Collection("users").Where("_id", int64(1)).Increment(ctx, "age"); err != nil {
        log.Fatal(err)
    }
    if _, err := engine.Collection("users").Where("_id", int64(1)).Decrement(ctx, "age", 2); err != nil {
        log.Fatal(err)
    }

    // Branch variants from a shared base with Clone; never reuse base directly.
    base := engine.Collection("users").Where("age", ">=", 18)
    withEmail, err := base.Clone().WhereNotNull("email").Count(ctx)
    if err != nil {
        log.Fatal(err)
    }
    total, err := base.Clone().Count(ctx)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(withEmail, total)

    // Scalar aggregation, Pluck, and distinct counting.
    avg, err := engine.Collection("users").Avg(ctx, "age")
    if err != nil {
        log.Fatal(err)
    }
    var names []string
    if err := engine.Collection("users").OrderBy("name").Pluck(ctx, "name", &names); err != nil {
        log.Fatal(err)
    }
    n, err := engine.Collection("users").CountDistinct(ctx, "age")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(avg, names, n)

    // Grouped aggregation: Where filters documents, Having filters rows.
    type cityAgg struct {
        City  string  `bson:"city"`
        N     int64   `bson:"n"`
        Total float64 `bson:"total"`
    }
    var rows []cityAgg
    err = engine.Collection("orders").
        Where("status", "paid").   // applied before grouping
        GroupBy("city").
        CountAs("n").
        SumAs("amount", "total").  // field first, alias second
        Having("n", ">=", 2).      // references result columns
        OrderBy("total", "desc").  // references result columns
        Limit(10).
        Aggregate(ctx, &rows)
    if err != nil {
        log.Fatal(err)
    }

    // Streaming, callback style: the cursor is closed automatically.
    err = engine.Collection("orders").
        Where("status", "paid").
        OrderBy("amount", "desc").
        Each(ctx, func(c *swifty_orm.Cursor) error {
            var o Order
            if err := c.Decode(&o); err != nil {
                return err
            }
            fmt.Println(o.City, o.Amount) // returning an error stops iteration
            return nil
        })
    if err != nil {
        log.Fatal(err)
    }

    // Streaming, manual style: closing is the caller's responsibility.
    cursor, err := engine.Collection("orders").OrderBy("_id").Limit(100).Cursor(ctx)
    if err != nil {
        log.Fatal(err)
    }
    defer cursor.Close(ctx)
    for cursor.Next(ctx) {
        var o Order
        if err := cursor.Decode(&o); err != nil {
            log.Fatal(err)
        }
        _ = cursor.Current() // raw bson.Raw of the current document
    }
    if err := cursor.Err(); err != nil {
        log.Fatal(err)
    }

    // Transaction (requires a replica set); tx queries auto-join the session.
    err = engine.Transaction(ctx, func(sc context.Context, tx *swifty_orm.Engine) error {
        if _, err := tx.Collection("users").Where("_id", int64(1)).
            Update(sc, bson.M{"$inc": bson.M{"age": -1}}); err != nil {
            return err
        }
        _, err := tx.Collection("users").Where("_id", int64(2)).
            Update(sc, bson.M{"$inc": bson.M{"age": 1}})
        return err // nil commits, non-nil aborts
    })
    if err != nil {
        log.Fatal(err)
    }

    // Auto-increment sequence and index management.
    nextID, err := engine.NextSequence(ctx, "order_id")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("next id:", nextID)
    if _, err := engine.Collection("users").EnsureIndexes(ctx, []mongo.IndexModel{
        {
            Keys:    bson.D{{Key: "email", Value: 1}},
            Options: options.Index().SetUnique(true).SetName("uniq_email"),
        },
    }); err != nil {
        log.Fatal(err)
    }
}
```

Error handling. Distinguish the not-found sentinel, the unbound-collection
sentinel, and driver errors, which arrive unwrapped:

```go
var u User
err := engine.Model(&User{}).Where("_id", int64(1)).First(ctx, &u)
switch {
case err == nil:
    // found
case errors.Is(err, swifty_orm.ErrNotFound):
    // no document matched; also matches mongo.ErrNoDocuments
case errors.Is(err, swifty_orm.ErrCollectionRequired):
    // nil engine, blank collection name, or a non-struct passed to Model
default:
    // builder error (bad operator, bad Where arity) or a driver error
    log.Printf("query failed: %v", err)
}

// A builder error surfaces at execution, not at the call site.
q := engine.Collection("users").Where("age", "=>", 18) // typo: unsupported operator
if _, err := q.Count(ctx); err != nil {
    log.Printf("deferred builder error: %v", err) // "where: unsupported operator \"=>\""
}

// Driver errors are returned verbatim; match them with driver helpers.
if _, err := engine.Collection("users").Insert(ctx, &User{ID: 1}); err != nil {
    if mongo.IsDuplicateKeyError(err) {
        // unique index violation
    }
}

// Partial InsertMany failures still report what was inserted.
res, err := engine.Collection("users").Insert(ctx, batch)
if err != nil {
    log.Printf("inserted %d of %d before failing: %v", res.InsertedCount, len(batch), err)
}
```

Shutdown and disconnect. Close the engine with a context that is not the
already-cancelled request context, or `Disconnect` fails and the driver's
background goroutines and sockets leak:

```go
engine, err := swifty_orm.NewEngine(startCtx, uri, "my-app")
if err != nil {
    log.Fatal(err)
}
defer func() {
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := engine.Close(shutdownCtx); err != nil {
        log.Printf("mongo disconnect: %v", err)
    }
}()
```

## Testing patterns

`swifty_orm_test.go` mixes pure unit tests against a zero-value `&Query{}` with
integration tests against a live server, and the split matters for whether a
given test can run without MongoDB.

Unit tests, no server required: they construct `&Query{}` directly and inspect
the unexported builders, since the tests live in package `swifty_orm`. Examples
are `TestBuildFilterMergesMultipleOpsOnSameField`,
`TestBuildFilterEqualityThenOperatorPreserved`,
`TestBuildFilterOrWhereWithoutBaseCondition`, `TestWhereOperatorAliases`,
`TestLikeToRegex`, `TestPluralize`, `TestNormalizeUpdate`,
`TestNormalizeUpdateExtended`, `TestExpandInsertDocs`, `TestCloneIndependence`,
`TestCloneCopiesGroupState`, `TestBuildProjectionExclusion`,
`TestOrderByCaseInsensitive`, `TestGroupPipelineShape`,
`TestGroupBuilderValidation`, and `TestGroupResultColumnValidation`. They assert
on `q.err`, `q.buildFilter()`, `q.buildProjection()`, and
`q.buildGroupPipeline()`. `TestCollectionName` and `TestNewEngineValidation`
also run without a server.

Integration tests use the `openTestEngine(t)` harness:

```go
func openTestEngine(t *testing.T) (*Engine, context.Context) {
    // ctx with a 10s timeout, cancel registered via t.Cleanup
    // uri from MONGO_URI, defaulting to "mongodb://localhost:27017/"
    // database name "swifty_orm_test_<UnixNano>" for per-test isolation
    // RunCommand({create: "__access_check"}) probe; t.Skipf on "Unauthorized"
    // t.Cleanup drops the database and closes the engine with a fresh 10s ctx
}
```

Gating behavior to know before running the suite:

- A reachable MongoDB is required. `NewEngine` failure is a hard `t.Fatalf`, so
  with no server the integration tests fail rather than skip.
- If the server requires authentication, the `__access_check` probe returns an
  error containing `"Unauthorized"` and the test skips with a message telling
  you to set `MONGO_URI` with credentials.
- Each test creates its own timestamp-named database and drops it in cleanup, so
  tests do not interfere and can run against a shared server.
- `TestTransactionAutoSessionBinding` skips when the error text contains
  `"Transaction numbers"` or `"IllegalOperation"`, which is how a standalone
  mongod rejects transactions. It deliberately passes the plain outer ctx rather
  than `sc` to prove that `execCtx` binds the session, then asserts the value
  rolled back.

Run the unit tests only with `go test -run 'TestBuild|TestWhere(Object|Operator|Invalid|NotBetween)|TestLike|TestPluralize|TestNormalize|TestExpand|TestClone|TestGroup(Pipeline|Builder|Result|Flattened)|TestOrderBy|TestCollectionName|TestNewEngine|TestSingleDotted|TestAggAlias|TestOrWhereEmptyMap' ./swifty_orm`,
or point `MONGO_URI` at a replica set and run the whole suite.

## Pitfalls and known limitations

1. Chaining mutates in place. Reusing a partially built `Query` for two
   variants silently applies the first variant's conditions to the second,
   because every `Where*`/`OrWhere*`/`OrderBy` call appends to the receiver's
   own slices. Call `Clone()` at each branch point, and never stash a
   partially built `Query` on a shared struct.
2. `Clone` copies slices but not the values inside them. `Select(mySlice...)`
   aliases the caller's slice, and condition values such as the slice passed to
   `WhereIn` are stored by reference, so mutating those values after building
   changes the filter of the original and of every clone. Pass values you do
   not intend to modify.
3. An empty filter on `Update` or `Delete` targets the whole collection. The
   package has no guard against unconditional mass mutation, so enforce at least
   one condition in application code before calling either.
4. A `Where` added after an `OrWhere` joins the main AND chain, so
   `Where(a).OrWhere(b).Where(c)` means `(a AND c) OR b`, not SQL's
   `a OR (b AND c)`. Put `OrWhere` calls last, or restructure into an explicit
   `Where("$or", ...)` raw operator condition.
5. Builder errors are deferred. Invalid `Where`/`OrWhere`/`Having`/`GroupBy`/
   alias input records an error and is returned only by the next execution
   method, so an ignored error makes a mistyped operator look like an empty
   result. Never discard execution errors.
6. Only `Where`, `OrWhere`, `Having`, `GroupBy`, and the `*As` methods validate
   input. `WhereNot`, `WhereIn`, `WhereNotIn`, `WhereNull`, `WhereNotNull`,
   `WhereBetween`, `WhereNotBetween`, `WhereLike`, `WhereILike`, and all
   `OrWhere*` variants append blindly, so a blank field name produces a filter
   keyed on `""` that matches nothing and reports no error.
7. `Update` returns `MatchedCount`, not `ModifiedCount`. An idempotent update
   reports 1 even though nothing changed. Use `Upsert`, whose `UpsertResult`
   carries `ModifiedCount`, when you need to know whether a write occurred.
8. `Update` with an empty document is wrapped into `{$set: {}}`, which the
   server rejects. Validate that the update document is non-empty first.
9. Struct updates are wrapped in `$set` with every marshaled field, including
   zero values unless the `bson` tags carry `omitempty`. Use `bson.M` for
   partial updates so absent fields stay untouched.
10. Pending `GroupBy`/`Having`/alias state makes every non-`Aggregate`
    execution method fail by design, including `Find`, `Count`, `Delete`, and
    `Cursor`. Only `Aggregate` consumes group state, and `Select` cannot be
    combined with `GroupBy`.
11. In grouped aggregation, `Having` and `OrderBy` must name result columns:
    flattened group keys (`addr.city` becomes `addr_city`) or accumulator
    aliases. Raw dotted paths and unknown names are pipeline build errors.
    Aliases must not be `_id`, start with `$`, contain `.`, repeat, or collide
    with a group key.
12. `Sum`, `Avg`, `Min`, and `Max` always decode into `float64` and ignore
    `OrderBy`, `Limit`, and `Offset` because their pipeline is only `$match` +
    `$group`. `Sum` and `Avg` return 0 both for "nothing matched" and for a
    genuine zero, which is indistinguishable; `Min`/`Max` over strings or dates
    fail while decoding. Use `Aggregate` or a manual pipeline for non-numeric
    extremes.
13. `Count` and `Distinct` also ignore `OrderBy`, `Limit`, and `Offset`. In
    particular `Limit(10).Count(ctx)` returns the full matching count, not 10.
14. `CountDistinct` fetches every distinct value to the client and returns its
    length, so cardinality drives memory and network use. `Distinct` itself is
    also subject to MongoDB's 16 MB BSON limit on the command reply.
15. `Distinct` returns `[]any`; callers must type-assert each element, and
    numeric types arrive as whatever BSON type the documents stored.
16. `Pluck` requires a non-nil pointer to a slice of the value type. Documents
    missing the field contribute the element zero value, indistinguishable from
    a stored zero. It projects `{field: 1, "_id": 0}` internally but leaves the
    Query's own `Select` untouched.
17. LIKE patterns support only `%` and `_`, with no escape syntax, so a literal
    `%` or `_` in the data cannot be matched. All other regex metacharacters are
    escaped by `regexp.QuoteMeta`, and the pattern is anchored, so `WhereLike`
    is never a substring match unless you write `%...%`.
18. `Where(field, op, value)` passes any unknown `$`-prefixed operator straight
    through without validating the value shape, so a malformed `$`-operator
    surfaces only as a server-side error.
19. Cursors from `Query.Cursor` are the caller's to close. Skipping `Close`
    leaks a server-side cursor until the server's idle timeout and holds driver
    resources. Prefer `Each`, which closes for you, unless you need manual
    control.
20. Transactions require a replica set or sharded cluster; a standalone mongod
    rejects them. Queries through the `tx` sub-Engine auto-join the session even
    with a plain ctx, but the session ends when the callback returns, so a
    captured `tx`, `Query`, or `Cursor` is invalid afterwards. The sub-Engine is
    also not safe for concurrent use.
21. `NextSequence` hard-codes the `counters` collection name. It is not
    configurable and collides with an application collection of the same name.
22. `CollectionName` splits acronyms letter by letter (`HTTPServer` becomes
    `h_t_t_p_servers`), does not handle irregular plurals (`Person` becomes
    `persons`), and returns `""` for non-structs and anonymous structs, which
    yields a Query that fails with `ErrCollectionRequired`. There is no
    override interface; call `engine.Collection("explicit_name")` instead of
    `Model` when the derived name is wrong.
23. Field names are raw strings matched against stored BSON keys, and this
    package does not read `bson` tags. An untagged Go field is stored under its
    fully lowercased name (`CreatedAt` becomes `createdat`), so untagged structs
    and snake_case filters silently fail to match. Tag every field explicitly,
    including `bson:"_id"` for the primary key.
24. `NewEngine` exposes no client options: no configurable pool size, read
    preference, write concern, timeouts, TLS settings, or monitors beyond what
    the URI encodes. Everything must go into the connection string.
25. `NewEngine` pings eagerly, so construction fails fast when the server is
    unreachable. There is no lazy-connect or retry mode; add retries at the call
    site if startup must tolerate a not-yet-ready database.
26. The exported logging helpers are never used by the ORM itself, so enabling
    `InfoLevel` produces no query logging. There is no hook, monitor, or
    statement logger; use the driver's own `options.Client().SetMonitor` through
    a hand-built client if you need observability, which means bypassing
    `NewEngine`.
27. `First` honors `Offset` but not `Limit`, which is easy to misread when
    porting a `Limit(1)` idiom from another ORM. `Limit(1).First(ctx, &out)` is
    equivalent to `First(ctx, &out)`.

## File map

| File                 | Purpose                                                                                                                                                                                                                                                    |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `swifty_orm.go`      | Package clause only; the license/anchor file. No declarations.                                                                                                                                                                                             |
| `engine.go`          | `Engine`, `NewEngine`, `Client`, `Database`, `DatabaseName`, `Collection`, `Model`, `Close`, `DropDatabase`, `NextSequence`, `Transaction`, and the unexported `sessionContext`.                                                                             |
| `query.go`           | `Query` struct and its builder state, `setErr`, the whole `Where`/`OrWhere` family, `OrderBy`, `Limit`, `Offset`, `Select`, `Clone`.                                                                                                                        |
| `query_exec.go`      | `ErrCollectionRequired`, `ErrNotFound`, `InsertResult`, `UpsertResult`, `Insert`, `First`, `Find`, `Update`, `Upsert`, `Increment`, `Decrement`, `Delete`, `Count`, `Exists`, `EnsureIndexes`, `DropCollection`, plus `findOptions`, `preflight`, `preflightBase`, `execCtx`, `normalizeUpdate`, `hasOperatorKey`, `expandInsertDocs`. |
| `query_aggregate.go` | `Distinct`, `CountDistinct`, `Pluck`, `Sum`, `Avg`, `Min`, `Max`, and the unexported `aggregate` helper that runs the `$match` + `$group` scalar pipeline.                                                                                                   |
| `query_group.go`     | `aggSpec`, `GroupBy`, `Having`, `CountAs`, `SumAs`, `AvgAs`, `MinAs`, `MaxAs`, `Aggregate`, plus `addAggSpec`, `addFieldAggSpec`, `buildGroupPipeline`, `groupKeyName`.                                                                                      |
| `query_stream.go`    | `Cursor` type with `Next`, `Decode`, `Current`, `Err`, `Close`, `bind`; `Query.Cursor` and `Query.Each`.                                                                                                                                                    |
| `filter.go`          | `condition`, `opAliases`, `parseWhere`, `parseWhereMap`, `normalizeOp`, `toBetweenPair`, `likeToRegex`, `Query.buildFilter`, `buildConditionFilter`, `Query.buildProjection`.                                                                                |
| `naming.go`          | `CollectionName` and the unexported `toSnake`, `pluralize`, `isVowel`.                                                                                                                                                                                      |
| `log.go`             | `Error`, `Errorf`, `Info`, `Infof`, `InfoLevel`, `ErrorLevel`, `Disabled`, `SetLevel`, and the unexported loggers plus the mutex guarding `SetLevel`.                                                                                                       |
| `swifty_orm_test.go` | Unit tests over `&Query{}` (filter merging, or-composition, operator aliases, LIKE translation, projection, clone independence, update normalization, insert expansion, group pipeline shape and validation, pluralization) and integration tests behind the `openTestEngine` harness. |
| `README.md`          | User-facing documentation, including a Knex.js alignment table.                                                                                                                                                                                             |
| `go.mod` / `go.sum`  | Module declaration, `go 1.26.0`, the mongo-driver requirement, indirect dependencies, and `replace` directives for the sibling swifty modules.                                                                                                              |

## Dependencies

Direct:

- `go.mongodb.org/mongo-driver` v1.17.9. The only direct requirement, used
  throughout: `mongo.Connect`/`Client`/`Database`/`Collection` for connection
  and CRUD, `mongo.Session` and `mongo.NewSessionContext` for transactions,
  `mongo.Cursor` behind the `Cursor` type, `mongo.IndexModel` in
  `EnsureIndexes`, `mongo.ErrNoDocuments` as `ErrNotFound`, `bson.M`/`bson.D`/
  `bson.A`/`bson.E`/`bson.Raw` for documents and pipelines,
  `bson/primitive.Regex` for LIKE translation, and `mongo/options` for find,
  update, index, and `FindOneAndUpdate` options.

Standard library only otherwise: `context`, `errors`, `fmt`, `io`, `log`, `os`,
`reflect`, `regexp`, `sort`, `strings`, `sync`.

Everything else in `go.mod` is marked `// indirect` and belongs to the mongo
driver's transitive closure, not to this package: `github.com/davecgh/go-spew`,
`github.com/golang/snappy`, `github.com/google/go-cmp`,
`github.com/klauspost/compress`, `github.com/montanaflynn/stats`,
`github.com/xdg-go/pbkdf2`, `github.com/xdg-go/scram`,
`github.com/xdg-go/stringprep`, `github.com/youmark/pkcs8`,
`golang.org/x/crypto`, `golang.org/x/sync`, `golang.org/x/text`.

`go.mod` also carries `replace` directives pointing
`github.com/hangtiancheng/swifty.go/swifty_cache`, `.../swifty_http`, and
`.../swifty_rpc` at their sibling directories, with the self-replace for
`swifty_orm` commented out. None of the three is required, so they contribute
nothing to the build graph of this module.

External requirement at runtime: a MongoDB server. Transactions additionally
require a replica set or sharded cluster.

## Cross-references to sibling skills

- `swifty-cache`: distributed cache framework. swifty_orm has no caching layer
  of any kind, so pair the two when read-through caching of query results is
  needed. Cache the decoded result structs, not the `Query`, which is mutable
  and not safe to share.
- `swifty-http`: HTTP server framework. Construct one `Engine` at startup,
  inject it into handlers, pass the request context into query methods so HTTP
  cancellation and deadlines propagate to MongoDB, and call `Engine.Close` with
  a separate shutdown context after the server stops accepting requests.
- `swifty-rpc`: TCP RPC framework. The `Engine` is the persistence layer behind
  RPC method implementations; build it once per process and share it, since
  `*Engine` is concurrency-safe while `*Query` is not.
