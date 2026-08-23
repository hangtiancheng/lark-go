---
name: update-go-skills
description: >
  Regenerate the swifty.go framework skill documentation by reading the latest package source code and rewriting SKILL.md files for swifty_cache (swifty_cache → .github/skills/swifty-cache), swifty_http (swifty_http → .github/skills/swifty-http), swifty_orm (swifty_orm → .github/skills/swifty-orm), and swifty_rpc (swifty_rpc → .github/skills/swifty-rpc).
  Use this skill when the user asks to update, refresh, regenerate, or sync the skill docs with the current source, or when source code under swifty_cache or swifty_http or swifty_orm or swifty_rpc has changed and the skills need to reflect the new API surface. Also trigger when the user mentions "update skills", "refresh skill docs", "sync skills with source", or says the skill documentation is outdated.
  Do NOT use for creating entirely new skills unrelated to these four packages, for documenting the application modules swiftx, swifty_chat, or swifty_chatbot (they expose no library API), or for editing the skill description/triggering metadata only.
---

# Update Swifty-Go Framework Skills

This skill regenerates four framework skill documents from source. The output is
reference documentation that must be complete enough that a reader never needs
to open the Go source to use the package correctly.

## Repository context

The repository is a Go workspace (`go.work`, Go 1.26.4) containing seven
modules. Only these four are libraries with an exported API surface and have a
corresponding skill:

| Source directory | Module path                                       | Skill file to update                   |
| ---------------- | ------------------------------------------------- | -------------------------------------- |
| `swifty_cache/`  | `github.com/hangtiancheng/swifty.go/swifty_cache` | `.github/skills/swifty-cache/SKILL.md` |
| `swifty_http/`   | `github.com/hangtiancheng/swifty.go/swifty_http`  | `.github/skills/swifty-http/SKILL.md`  |
| `swifty_orm/`    | `github.com/hangtiancheng/swifty.go/swifty_orm`   | `.github/skills/swifty-orm/SKILL.md`   |
| `swifty_rpc/`    | `github.com/hangtiancheng/swifty.go/swifty_rpc`   | `.github/skills/swifty-rpc/SKILL.md`   |

The remaining modules (`swiftx`, `swifty_chat`, `swifty_chatbot`) are
applications whose code lives under `internal/`; they are out of scope.

Source layout to expect (verify before reading, it changes):

- `swifty_http/` and `swifty_orm/` are flat: all `.go` files sit in the module root.
- `swifty_cache/` has the module root plus `pb/` (generated protobuf and gRPC stubs).
- `swifty_rpc/` splits into `pkg/rpc/` and `pkg/api/` (the public surface) and
  `internal/` subpackages: `breaker`, `client`, `codec`, `limiter`,
  `load_balance`, `protocol`, `registry`, `server`, `stream`, `transport`.

All paths are relative to the repository root.

## Scope selection

Default to regenerating all four skills. Narrow the scope when the request or
the repository state points at a subset:

- The user names specific packages: update only those.
- The user says "sync with source" without naming packages: run
  `git log --oneline -20 -- <dir>` and `git status --short <dir>` per package
  and prioritise the ones with recent changes, but still verify the others.

Each package is independent. If one fails (missing directory, unreadable
source), report the failure and continue with the rest.

## Procedure

For each package in scope:

1. Build a symbol inventory first, so completeness can be checked mechanically
   rather than by memory. Prefer the toolchain:

   ```sh
   go doc -all ./swifty_http
   go doc -all ./swifty_cache/pb
   go doc -all ./swifty_rpc/pkg/rpc
   ```

   If the module does not build, fall back to a textual inventory:

   ```sh
   grep -rn '^func \|^type \|^var \|^const \|^\t[A-Z][A-Za-z0-9_]* ' swifty_http --include='*.go'
   ```

   Keep the inventory list; step 5 checks the finished document against it.

2. Read every `.go` file in the source directory and its subdirectories,
   including `_test.go` files. Tests reveal usage patterns, edge cases, and
   invariants that the exported signatures do not express. For `swifty_rpc`,
   read `internal/` too: its behaviour (wire format, breaker state machine,
   balancer selection, stream framing) is user-visible even though the packages
   are not importable.

3. Read the module's `go.mod` to capture the module path, Go directive, and
   direct dependencies. Distinguish direct requires from `// indirect` ones.

4. Extract, per package:
   - Component architecture and ownership relationships
   - Every exported type, interface, function, method, constant, and variable
   - Constructor patterns, option types, and their defaults
   - Method signatures with exact parameter and return types
   - Behavioural contracts, invariants, and non-obvious semantics
   - Concurrency safety: what is safe to share, what is not, which locks cover what
   - Goroutine lifecycle: who starts them, what stops them, what leaks if you skip cleanup
   - Sentinel errors and error wrapping behaviour
   - Zero-value behaviour and what happens when required options are omitted
   - Known limitations, caveats, and discrepancies between intent and implementation
   - File-to-purpose mapping

5. Write the SKILL.md per the format below, then verify it (see Verification).

## Output format specification

### YAML front matter

```yaml
---
name: <directory-name-of-the-skill>
description: >
  <multi-line description optimized for skill triggering; include the module
  path, key type names, key function names, and explicit trigger/skip guidance>
---
```

Rules:

- `name` must exactly match the containing directory name (`swifty-cache`,
  `swifty-http`, `swifty-orm`, `swifty-rpc`). Do not rename existing skills.
- `description` is the only text the model sees when deciding whether to load
  the skill, so it must carry the trigger tokens: the module import path, the
  most important exported symbols, a one-sentence statement of what the package
  does, an explicit "Use when ..." clause with concrete identifiers, and an
  explicit "Do NOT use for ..." exclusion.
- Preserve trigger tokens that already exist unless the corresponding symbol
  was removed from the source. Removing a working trigger regresses the skill.

### Body structure

Follow the section order already established across the four documents; add
package-specific sections where the subject matter demands it.

1. `# <package_name>` heading, then a one-paragraph summary: what the package
   is, its design philosophy, and its module path.
2. When to load adjacent skills, if the package composes with siblings.
3. Architecture overview: ASCII diagram of the component graph and data flow.
   Name every major type and show ownership and composition.
4. Core types: for each exported type, its constructors and option types, a
   method table with full signatures and behavioural notes, field semantics for
   public fields, and its sentinel errors.
5. Package-specific deep dives as needed (for example: consistency model,
   consistent hashing, service registration, routing, built-in middleware,
   protobuf surface, dashboard).
6. Internal implementation details affecting correctness: lock scope, goroutine
   lifecycle, wire format, codec constraints, retry and timeout interaction,
   buffer reuse.
7. Typical usage: realistic examples covering the common paths, plus at least
   one error-handling and one shutdown example.
8. Testing patterns, where the test suite shows a reusable harness.
9. Pitfalls and known limitations: numbered list. Each item states the
   behaviour, why it happens, and how to avoid or work around it.
10. File map: table mapping each source file to its purpose. Every `.go` file in
    the package must appear.
11. Dependencies: direct external modules and what each is used for.
12. Cross-references to sibling skills at real integration points.

## Writing standards

- Professional, precise English throughout. Active voice, imperative mood for
  instructions.
- Document what the code does, not what it should do. When intent and
  implementation diverge (for example an unenforced `MaxBytes` field), document
  the actual behaviour and flag the discrepancy.
- Include every exported symbol. A skill that omits one forces the reader back
  into the source, which defeats its purpose.
- For each behavioural contract, state the invariant, then state what breaks
  when it is violated.
- Code examples must be minimal and must compile against the current API. Use
  `// ...` to elide irrelevant setup, never to hide API surface.
- Signatures must be copied from source, not paraphrased. Exact types, exact
  parameter order, exact variadic and pointer forms.
- No bold text, no emoji.
- Never invent features, options, or defaults that the source does not contain.
  If behaviour is genuinely unclear from the source, say so explicitly rather
  than guessing.
- Do not add license headers to any file; the user applies those separately.

## Verification

Before reporting a package as done:

1. Diff the symbol inventory from step 1 against the finished document. Every
   exported symbol must appear. List any deliberate omissions and why.
2. Confirm every `.go` file in the package appears in the file map.
3. Re-read each code example against the current signatures. Fix drift.
4. Confirm the front matter `name` still matches the directory and the
   description still names the module path.
5. Check for stale content: symbols documented that no longer exist, options
   that were renamed, defaults that changed.
6. Where the module builds, `go build ./...` and `go vet ./...` in the module
   directory to confirm the source you documented is the source that compiles.

## Reporting

After processing the packages in scope, report per package:

- Added: new exported symbols now documented
- Removed: symbols that disappeared from source and were deleted from the doc
- Changed: signature, default, or behavioural changes
- Unverified: anything you could not confirm from source

Keep the report short enough to scan. Its purpose is to let the user spot a
wrong or hallucinated change quickly.
