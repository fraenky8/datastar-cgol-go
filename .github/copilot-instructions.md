# Copilot instructions

## Project overview

This repository is a Go 1.27 module (`datastar-cgol-go`) implemented as a single
`main` package. It serves a realtime, multiplayer Conway's Game of Life board
using Go's standard `net/http` server, Datastar, and Templ. The Templ source in
`components.templ` is compiled to `components_templ.go`; browser assets are
compiled into the binary with `embed`, so the application does not need a
separate frontend build step.

## Build, run, test, and lint

Run commands from the repository root:

```text
go build .
go run .
templ generate
go test ./...
go test ./... -run '^TestName$' -count=1
go vet ./...
gofmt -w main.go game.go components_templ.go
```

There are currently no repository test files, but `go test ./...` still checks
that all packages compile. Replace `TestName` with the exact test name when
running one test after tests are added. There is no configured third-party
linter or task runner; `gofmt` and `go vet` are the repository's standard
formatting and static checks. `components_templ.go` is generated code; edit
`components.templ` rather than the generated file, then regenerate it with the
Templ CLI before building. Dependencies are vendored, so preserve `go.mod`,
`go.sum`, and `vendor/modules.txt` together when changing dependencies.

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
  embeds `assets`, and configures the HTTP routes.
- `GET /` renders the `indexPage` Templ component from `components.templ`.
  That component includes `gameBoard` and `clientCount` and starts the
  Datastar connection with `@post('/')`.
- `POST /` is a long-lived SSE subscription. It registers a `Sub`, receives
  cloned `Board` values from the game, and patches the `game-board` and
  `client-count` elements with Datastar through `PatchElementTempl`. Brotli
  compression is enabled for this stream.
- `POST /tap` accepts `x` and `y` query parameters from the browser and queues
  a cell activation in the game. Invalid coordinates or malformed input are
  logged and intentionally treated as no-ops with HTTP 204.
- `game.go` owns the simulation. A ticker skips advancement when there are no
  subscribers; otherwise it drains queued taps, advances the finite
  non-wrapping board, and publishes a cloned snapshot. The `current` and
  `next` boards are swapped each generation. The mutex protects only the
  subscriber map; board snapshots must be cloned before being sent to
  subscribers.
- `components.templ` renders the board and contains the Datastar pointer
  handlers for click-and-drag input. `assets/datastar.js` is the vendored
  browser runtime and `assets/style.css` provides the board layout and cell
  styling.

## Repository-specific conventions

- Keep the application in the existing `main` package and use the standard
  library unless an existing Datastar or vendored dependency is the appropriate
  integration point.
- Preserve the Templ component and DOM contracts: `indexPage` includes
  `gameBoard` and `clientCount`, and server-side updates patch the
  `game-board` and `client-count` elements.
- Treat the SSE subscription as a streaming request and unregister the
  subscriber when either the client request context or application context
  ends. Its `Board` channel is intentionally buffered with capacity one.
  Publishing is non-blocking and drops a generation for subscribers that are
  already behind, so a slow client must not block the simulation or other
  clients.
- Apply user taps in the simulation goroutine before calculating the next
  generation. Do not mutate a published board or bypass the tap channel from
  an HTTP handler.
- Board dimensions are derived by flooring the square root of `-num-cells`;
  the effective board is always square, and cells outside its finite bounds are
  dead rather than wrapped around.
- Keep browser behavior declarative in the Templ `data-*` attributes. The
  project does not use a frontend framework or a separate JavaScript build.
- The `embed.FS` path is part of the runtime contract: assets are served below
  `/assets/`. Templ components are compiled into Go code rather than loaded at
  runtime.
- Use the existing structured `slog` logging and explicit error propagation
  patterns when changing handlers, template rendering, or the game loop.
