# Impulse

A Minecraft: Bedrock Edition server written in Go — a hardened build of
[dragonfly](https://github.com/df-mc/dragonfly). It keeps dragonfly's behaviour and API; the
changes are defect fixes and hot-path cost.

What was hardened:

- **Crashes from untrusted input.** Item and block entity NBT, palette sizes, sub-chunk indices
  and unknown block states report errors instead of panicking on malformed packets and world
  files.
- **Irreversible data damage.** Item duplication and loss in containers, an unregistered
  dimension overwriting the overworld, a shared legacy mapping table, entity index corruption.
- **Memory safety.** Paletted storage indices are bounds-checked; the `uintptr` arithmetic is
  gone.
- **Hot path.** Per-call heap allocations removed from the tick loop, chunk access, entity
  iteration and packet handling.

Held in place by 1,395 tests and 52 benchmarks.

## Requirements

Go 1.26 or later.

## Running

```shell
git clone https://github.com/jv2w/Impulse
cd Impulse
go run .
```

The server writes a `config.toml` on first start and reads it on every start after that. Press
ctrl+c to shut down.

## Using it as a library

```shell
go get github.com/jv2w/Impulse
```

## Licence

MIT, including code derived from [dragonfly](https://github.com/df-mc/dragonfly). Both copyright
notices are kept in [LICENSE](LICENSE).
