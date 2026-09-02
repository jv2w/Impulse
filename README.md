# Impulse

A Minecraft: Bedrock Edition server written in Go, derived from
[dragonfly](https://github.com/df-mc/dragonfly).

Impulse is a hardened dragonfly, not a redesign. It keeps dragonfly's structure and observable
behaviour and changes two things: defects that take the server down — panics, memory corruption and
irreversible world or inventory damage reachable from client packets and world files — and measured
hot-path cost. Changes outside those two categories are out of scope.

The world is ticked by a single goroutine. That is a choice, not a limitation: horizontal scaling
belongs to a proxy, and region-level parallelism would trade away the tick-level ordering that
interactions crossing a region boundary depend on. Performance comes from removing waste on the hot
path instead.

Impulse is deliberately not a feature-coverage project. Mobs, world generation and the unfinished
vanilla systems inherited from dragonfly stay out of scope.

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

Impulse keeps dragonfly's API where it can, so most code ports by changing the import path. A
signature only changes where a defect cannot be fixed otherwise.

## Licence

MIT. Impulse derives from dragonfly, also MIT; both copyright notices are kept in
[LICENSE](LICENSE).
