# CLAUDE.md

This document defines **how** code is written in this repository. **What** to build comes from the user's instructions.

**Everything below is binding, not advice.** It applies to every change in this repository, in every session, no matter how small the change is.

- "Do not X" means never X, not "prefer not to". No rule carries an implicit exception for urgency, tidiness, consistency, "it was already broken", "it is obviously better", or "the user probably wants it".
- The only thing that overrides a rule is an **explicit instruction from the user in the current conversation**, and only for the specific rule it names. Silence is not an instruction. Inferred intent is not an instruction.
- When a rule blocks the task, stop and report to the user. Do not route around it, do not comply partially and hope, do not leave a TODO in the code in place of reporting.
- "Report" means writing it in your reply to the user. A code comment, a commit message, or an unwritten thought is not a report.

---

## 1. Project

Impulse is a Minecraft: Bedrock Edition server runtime written in Go. The module path is `github.com/jv2w/Impulse`.

- `gophertunnel` owns the Bedrock protocol. Never reimplement protocol handling inside this repository.
- Performance is won by **removing waste on the hot path**, never by splitting tick work across goroutines. Handing blocking I/O to a worker (§3.3) is about not waiting, not about parallelism.
- Never change a name that is baked into the storage format. The NBT key `dragonflyData` (`server/item/nbt.go`) and the entity identifier `dragonfly:text` (`server/entity/text.go`) are such names. They look like legacy names, but changing them makes existing worlds and items unreadable.
- **The project is called Impulse, and Go source says so.** The words `dragonfly` and `Dragonfly`, in any case, appear in a `.go` file only on a line that also names one of the two storage values above — that covers the values themselves and the identifiers built on them, such as `writeDragonflyData`. Every other mention is Impulse: doc comments, log messages, user-visible defaults, test fixture identifiers. The code is derived from dragonfly and says so in `LICENSE` and `README.md`, which are not Go source and are not covered. `TestGoSourceNamesImpulse` (`internal/repo`) enforces this; a new mention fails the build rather than being noticed later.
- The license is MIT. Never delete the existing copyright lines in `LICENSE`.

---

## 2. Code rules

### 2.1 Minimal change

- Never improve adjacent code. Report it instead, and only if it matters.
- Never rename anything. Never rewrite an existing comment.
- If a public signature has to change, **report first**, then fix every call site to match.

### 2.2 Result integrity

Behavior that must match vanilla is never a target for optimization or simplification. If a result has to change, report before changing it.

**The one exception is `world.Tuning` (`server/world/tuning.go`).** Every field on it exists to switch a vanilla system off, and that is intended, not a defect. The exception is exactly this wide:

- The zero value of `world.Tuning` must leave the World behaving exactly as vanilla does. A toggle changes behavior only when the operator sets it.
- A new toggle is added to that type and to nothing else. Turning a vanilla system off anywhere outside it is still forbidden.
- `world.Tuning` is not the Minecraft game rule set. The rules a client knows are `protocol.GameRule` values and are sent separately. Never name a field here after one of them, and never route one of them through this type.

No other part of the tree may cite this exception.

### 2.3 External input is not trusted

Enforce this **only on paths reachable from external input (client packets, world files, player data)**.

- Validate values. Use comma-ok for type assertions. Check the length before indexing a slice.
- Return `(T, bool)` or `(T, error)` instead of `panic`.
- `recover` is legitimate at a boundary where a third-party implementation may panic. Write down in a comment what is being swallowed.

A `panic` reachable only from internal calls **must be left alone.** It is the device that surfaces the author's mistake fast.

### 2.4 unsafe

- Never introduce new `unsafe`.
- When editing existing `unsafe` code, use `unsafe.Add` for a pointer offset. `uintptr` arithmetic is forbidden.
- Never cache a slice data pointer in a struct field. There is no way to enforce synchronization on it.

### 2.5 Comments

In code you touched, write the following.

- If you fixed a defect — **which input was the problem**
- If it is an optimization — **why the behavior is identical**
- The concurrency contract, ownership, and whether the return value aliases — write these down whenever they are not self-evident

### 2.6 Errors

- Error strings start lowercase and do not end with a period.
- Wrapping errors use `%w`. Never swallow the cause with `%v`.
- An error the caller must branch on is exposed as a sentinel (`ErrXxx`) or an error type.
- Never discard an error into `_`. If you must discard one, leave a comment saying **why it is safe**.

---

## 3. Performance

### 3.1 The precondition for optimizing is behavioral identity

The threshold is not a benchmark number. It is **behavior not changing by one bit**.

- Return values and errors are the same
- Side effects are the same — down to the number and order of callback invocations
- **Aliasing is the same** — a function that handed out a copy must not start handing out an internal slice, and a function that copied its argument must not start holding onto the argument itself
- Where iteration order was observable, the order is the same too
- **Floating-point results are bit-identical** — reordering operations changes the value through reassociation

**If you cannot prove identity by eye, do not do it.** When it is ambiguous, leave it alone.

**Never swap the data structure of existing code as an optimization.** map↔slice changes iteration order, so it is a behavior change under the rule above. Adding an index to existing code (§3.2) is the same kind of change — report first.

If you touched the hot path, attach a benchmark. `allocs/op` from `-benchmem` is the primary metric.

### 3.2 Hot path rules

On the tick loop, block setting, entity iteration, and packet handling paths:

- Do not create a heap allocation per call. Avoid a `slices.Clone` that never escapes the function, allocating empty option structs, unnecessary closure captures, and interface boxing.
- Put reusable buffers in an instance field or hand ownership to the caller. `sync.Pool` is the last resort.
- Hoist loop invariants out of the loop. Never do the same lookup twice in one function.
- Never determine a type by string comparison. Use a type switch or a precomputed flag.
- Never filter by iterating a whole map. In new code, keep an index on the axis you need; in existing code, adding one is a data-structure change (§3.1).

### 3.3 Tick budget

A tick must finish within 50ms.

- Never wait on blocking I/O in the tick loop. Hand chunk loads and saves to a worker and take only the result.
- Never load a chunk while holding a lock. Release a held lock the moment the computation inside it finishes.
- Do not increase queue round-trips per request. One packet passing through the queue twice is a regression.
- Do not add per-tick work that scales with the number of loaded chunks unless vanilla itself does that work per chunk. Vanilla parity (§2.2) wins.

---

## 4. Verification

### 4.1 Pass criteria

Report a change complete only after all of the below pass. If even one fails, report the failure and its output verbatim. If you started something and backed away from it, report why.

```
gofmt -l .          # must print nothing
go vet ./...        # must print nothing
go build ./...
go test -race ./...
```

If you touched buffer reuse or global state, run repeatedly with `-count=5` or more. Bugs that leak state only show up on repeats.

### 4.2 Tests

Tests are written for the two purposes below only. Never write one to raise coverage.

- **Bug fixes** — write the reproduction test first, actually watch it fail, then fix. A test you never saw fail proves nothing.
- **Code where result integrity is at stake** — serialization, palette storage, inventory, packet handlers. Never modify these without a test.

When you write one:

- **A test that only checks "it does not panic" is not a safety net.** Assert observable values.
- Where reading and writing use the same encoding, a round-trip alone is not enough. **Assert keys and field names directly as golden values.** If both sides change together, the round-trip still passes.

### 4.3 PINNED markers

A `PINNED` comment in a test deliberately pins **"current behavior"**, not "correct behavior".

```go
// PINNED BUG: <what is wrong>
// PINNED SCOPE: <a feature that is intentionally absent>
// PINNED CONSTRAINT: <a limitation of the test environment>
```

Write one exactly like that, on a line of its own. `internal/repo/PINNED.md` indexes every marker in the tree and is generated; the tests in `internal/repo` reject any other wording and fail when the index is stale. After adding, removing or rewording a marker, regenerate it:

```
IMPULSE_UPDATE_PINNED=1 go test ./internal/repo -run TestPinnedIndexIsCurrent
```

| Task | If a marker breaks |
|---|---|
| **Defect fix** | If what that marker pinned is the very bug you just fixed, update the test, delete the marker, and **report why** |
| **Performance optimization** | **The optimization is wrong.** Revert the optimization, not the marker |

**Never fit expectations to the new output without judgment.** That is throwing verification away. Never touch a marker unrelated to your change.

### 4.4 Fault injection

If you wrote a new result-integrity test, **deliberately inject a defect into the production code and confirm the test catches it.** Then remove the injected defect — only that — and check with `git diff -- <file>` that nothing else in that file changed. Never revert with `git checkout .` or `git stash`; other sessions may have uncommitted work in the tree.

If the intended defect is not caught, the test has a hole. Fix the test.

---

## 5. Repository hygiene

- When the work is done, **commit without asking.**
- **Never push.** The user pushes.
- **Never attach trailers.** That includes `Co-Authored-By` and `Generated with`.
- One commit per change. Mixing several makes the cause untraceable when a regression appears.

Multiple sessions may work in the same repository at once. **Never use `git add -A`.** Specify paths.

```
git add <new file>
git commit -m "message" -- <path>
```

`git commit -- <path> -m "message"` fails because `-m` is interpreted as a path.
