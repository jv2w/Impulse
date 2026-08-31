# Impulse

A Minecraft: Bedrock Edition server written in Go, derived from
[dragonfly](https://github.com/df-mc/dragonfly).

Impulse keeps dragonfly's observable behaviour and replaces its internals. The two goals are an
execution model that is not bound to a single core, and a codebase that holds up as Go: failures
returned as values rather than panics, small interfaces, no package-level mutable state, and
documentation that says something.

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

Impulse does not preserve dragonfly's API. Code written against dragonfly needs porting.

## Licence

MIT. Impulse derives from dragonfly, also MIT; both copyright notices are kept in
[LICENSE](LICENSE).
