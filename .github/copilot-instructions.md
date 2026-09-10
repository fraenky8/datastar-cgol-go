# Copilot instructions

## Project overview

This repository is a Go 1.27 module (`datastar-cgol-go`) implemented as a single
`main` package. It serves a realtime, multiplayer Conway's Game of Life board
using Go's standard `net/http` server and Datastar. The templates and browser
assets are compiled into the binary with `embed`, so the application does not
need a separate frontend build step.

## Build, run, test, and lint

Run commands from the repository root:

```text
go build .
go run .
go test ./...
go test ./... -run '^TestName$' -count=1
go vet ./...
gofmt -w main.go game.go
```

There are currently no repository test files, but `go test ./...` still checks
that all packages compile. Replace `TestName` with the exact test name when
running one test after tests are added. There is no configured third-party
linter or task runner; `gofmt` and `go vet` are the repository's standard
formatting and static checks. Dependencies are vendored, so preserve
`go.mod`, `go.sum`, and `vendor/modules.txt` together when changing
dependencies.

The documented application defaults are `localhost:8000`, a 2500-cell board,
and a 200 ms refresh interval:

```text
go run . -addr localhost:8000 -num-cells 2500 -refresh-int 200ms
```

The `PORT` environment variable supplies the default port for deployments such
as Render. `REVISION`, `BUILD_TS`, and `DEBUG` control build metadata and the
optional debug footer.

## Architecture

- `main.go` parses flags and environment values, starts the `Game` goroutine,
  embeds `templates` and `assets`, and configures the HTTP routes.
- `GET /` renders `templates/index.gohtml`. That page includes the named
  `gameboard` and `clientcount` templates and starts the Datastar connection
  with `@post('/')`.
- `POST /` is a long-lived SSE subscription. It registers a `Sub`, receives
  cloned `Board` values from the game, and patches the `gameboard` and
  `clientcount` elements with Datastar. Brotli compression is enabled for this
  stream.
- `POST /tap` accepts `x` and `y` query parameters from the browser and queues
  a cell activation in the game. Invalid coordinates or malformed input are
  logged and intentionally treated as no-ops with HTTP 204.
- `game.go` owns the simulation. A ticker drains queued taps, advances the
  finite non-wrapping board, then publishes a cloned snapshot to every
  subscriber. The `current` and `next` boards are swapped each generation.
  The mutex protects only the subscriber map; board snapshots must be cloned
  before being sent to subscribers.
- `templates/gameboard.gohtml` renders the board and contains the Datastar
  pointer handlers for click-and-drag input. `assets/datastar.js` is the
  vendored browser runtime and `assets/style.css` provides the board layout and
  cell styling.

## Repository-specific conventions

- Keep the application in the existing `main` package and use the standard
  library unless an existing Datastar or vendored dependency is the appropriate
  integration point.
- Preserve the named template contract: `index.gohtml` includes `gameboard` and
  `clientcount`, and server-side updates refer to those names in
  `patchTemplate`.
- Treat the SSE subscription as a streaming request and unregister the
  subscriber when the request context ends. Its state channel is intentionally
  buffered with capacity one; changes to publisher backpressure should be made
  deliberately because the current publisher sends each snapshot to every
  subscriber.
- Apply user taps in the simulation goroutine before calculating the next
  generation. Do not mutate a published board or bypass the tap channel from
  an HTTP handler.
- Board dimensions are derived by flooring the square root of `-num-cells`;
  the effective board is always square, and cells outside its finite bounds are
  dead rather than wrapped around.
- Keep browser behavior declarative in the `.gohtml` `data-*` attributes. The
  project does not use a frontend framework or a separate JavaScript build.
- The `embed.FS` paths are part of the runtime contract: templates are parsed
  from `templates/*.gohtml`, and assets are served below `/assets/`.
- Use the existing structured `slog` logging and explicit error propagation
  patterns when changing handlers, template rendering, or the game loop.
