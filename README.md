# Impulse

A Minecraft: Bedrock Edition server written in Go — a hardened build of
[dragonfly](https://github.com/df-mc/dragonfly). It keeps dragonfly's behaviour and API; the
changes are defect fixes and hot-path cost.

The hardening covers crashes from untrusted input, irreversible data damage, memory safety and
per-call allocations on the hot path. Each fix carries a test, and the hot-path changes carry
benchmarks.

Some tests pin behaviour that is wrong, marked PINNED BUG. A green run means behaviour has not
drifted, not that it is correct.

## Requirements

Go 1.26 or later.

## Running

```shell
git clone https://github.com/jv2w/Impulse
cd Impulse
go run .
```

Settings live in `userConfig` in `main.go`; edit them there. No config file is read or written. Press
ctrl+c to shut down.

## Using it as a library

```shell
go get github.com/jv2w/Impulse
```

## Porting code written for dragonfly

Rewrite the module path. A `replace` directive cannot stand in for it, because Impulse's own
imports carry the new path.

Windows:

```powershell
Get-ChildItem -Recurse -Filter *.go -File | ForEach-Object { $p = $_.FullName; $t = [IO.File]::ReadAllText($p); $n = $t.Replace('github.com/df-mc/dragonfly', 'github.com/jv2w/Impulse'); if ($n -ne $t) { [IO.File]::WriteAllText($p, $n) } }
```

macOS and Linux:

```shell
find . -name '*.go' -exec sed -i.bak 's#github.com/df-mc/dragonfly#github.com/jv2w/Impulse#g' {} + && find . -name '*.go.bak' -delete
```

Identifiers and package names are dragonfly's. Where dragonfly swallowed a malformed value,
Impulse reports it, so a few readers return an error dragonfly did not, and calls that used to
succeed on bad input now fail. Read the doc comment of what you call before relying on the old
outcome.

## Licence

MIT, including code derived from [dragonfly](https://github.com/df-mc/dragonfly). Both copyright
notices are kept in [LICENSE](LICENSE).
