# CLAUDE.md

This document defines **how** code is written in this repository. **What** to build comes from the user's instructions.
Where this document overlaps or conflicts with the global instructions (`~/.claude/CLAUDE.md`), this document wins.

**Everything below is binding, not advice.** It applies to every change in this repository, in every session, no matter how small the change is.

- "Do not X" means never X. Not "prefer not to", not "avoid when convenient". A one-line edit is still a change.
- No rule here carries an implicit exception for urgency, tidiness, consistency, "it was already broken", "it is obviously better", or "the user probably wants it".
- The only thing that overrides a rule is an **explicit instruction from the user in the current conversation**, and only for the specific rule it names. Silence is not an instruction. Inferred intent is not an instruction. A rule you did not read is still in force.
- When a rule blocks the task, stop and report to the user. Do not route around it, do not comply partially and hope, do not leave a TODO in the code in place of reporting.
- "Report" means writing it in your reply to the user. A code comment, a commit message, or an unwritten thought is not a report.
- If you cannot show that you complied, you did not comply. Say so instead of claiming completion.

---

## 1. Project

Impulse is a Minecraft: Bedrock Edition server runtime written in Go. The module path is `github.com/jv2w/Impulse`.

- `gophertunnel` owns the Bedrock protocol. Never reimplement protocol handling inside this repository.
- Performance is won by **removing waste on the hot path**, never by parallelizing.
- Never change a name that is baked into the storage format. The NBT key `dragonflyData` (`server/item/nbt.go`) and the entity identifier `dragonfly:text` (`server/entity/text.go`) are such names. They look like legacy names, but changing them makes existing worlds and items unreadable.
- The license is MIT. Never delete the existing copyright lines in `LICENSE`.

---

## 2. Code rules

### 2.1 Minimal change

- Never improve adjacent code. Report it instead, and only if it matters.
- Never rename anything. Never rewrite an existing comment.
- If a public signature has to change, **report first**, then fix every call site to match.

### 2.2 Result integrity

Behavior that must match vanilla is never a target for optimization or simplification. If a result has to change, report before changing it.

### 2.3 External input is not trusted

Enforce this **only on paths reachable from external input (client packets, world files, player data)**.

- Validate values. Use comma-ok for type assertions. Check the length before indexing a slice.
- Return `(T, bool)` or `(T, error)` instead of `panic`.
- `recover` is legitimate at a boundary where a third-party implementation may panic. Write down in a comment what is being swallowed.

A `panic` reachable only from internal calls **must be left alone.** It is the device that surfaces the author's mistake fast.

### 2.4 unsafe

- Never introduce new `unsafe`.
- `uintptr` arithmetic is forbidden. Use `unsafe.Add` when a pointer offset is needed.
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

**Never change a data structure.** Swapping map↔slice changes iteration order, so it is a behavior change.

If you touched the hot path, attach a benchmark. `allocs/op` from `-benchmem` is the primary metric.

### 3.2 Hot path rules

On the tick loop, block setting, entity iteration, and packet handling paths:

- Do not create a heap allocation per call. Avoid a `slices.Clone` that never escapes the function, allocating empty option structs, unnecessary closure captures, and interface boxing.
- Put reusable buffers in an instance field or hand ownership to the caller. `sync.Pool` is the last resort.
- Hoist loop invariants out of the loop. Never do the same lookup twice in one function.
- Never determine a type by string comparison. Use a type switch or a precomputed flag.
- Never filter by iterating a whole map. Maintain an index on the axis you need.

### 3.3 Tick budget

A tick must finish within 50ms.

- Never wait on blocking I/O in the tick loop. Hand chunk loads and saves to a worker and take only the result.
- Never load a chunk while holding a lock. Release a held lock the moment the computation inside it finishes.
- Do not increase queue round-trips per request. One packet passing through the queue twice is a regression.
- Do not write new code whose per-tick work scales linearly with the number of loaded chunks.

---

## 4. Verification

### 4.1 Pass criteria

Report a change complete only after all of the below pass. If even one fails, report the failure and its output verbatim.

```
gofmt -l .          # must print nothing
go vet ./...        # must print nothing
go build ./...
go test -race ./...
```

If you touched buffer reuse or global state, run repeatedly with `-count=5` or more. Bugs that leak state only show up on repeats.

Anything you started to touch and backed away from — **report why you did not do it.**

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

The `Pinned as-is; decide in stage 1` text and the `CLAUDE.md N.N` references trailing existing markers are residue of an abandoned plan. Do not follow them, and do not carry them into new markers.

| Task | If a marker breaks |
|---|---|
| **Defect fix** | If what that marker pinned is the very bug you just fixed, update the test, delete the marker, and **report why** |
| **Performance optimization** | **The optimization is wrong.** Revert the optimization, not the marker |

**Never fit expectations to the new output without judgment.** That is throwing verification away. Never touch a marker unrelated to your change.

### 4.4 Fault injection

If you wrote a new result-integrity test, **deliberately inject a defect into the production code and confirm the test catches it.** After confirming, revert everything and see that `git status` is clean.

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
