package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"slices"
	"sync"
	"time"
)

// Original source: https://rosettacode.org/wiki/Conway%27s_Game_of_Life#Go
// And adapted for readability and newer Go code/optimizations. AI 🤖 helped.
// Also, we do not wrap around cells at the border but let them just die.
// Added context cancellation.
// Added Pub/Sub functionality.

type Cells [][]bool

func (c Cells) clone() Cells {
	clone := make(Cells, len(c))
	for i := range c {
		clone[i] = slices.Clone(c[i])
	}
	return clone
}

type Board struct {
	Cells Cells
	w, h  int
}

func newBoard(w, h int) Board {
	s := make(Cells, h)
	for i := range s {
		s[i] = make([]bool, w)
	}
	return Board{Cells: s, w: w, h: h}
}

func (b Board) set(x, y int, alive bool) {
	b.Cells[y][x] = alive
}

func (b Board) nextAlive(x, y int) bool {
	on := 0
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			if b.alive(x+i, y+j) && !(j == 0 && i == 0) {
				on++
			}
		}
	}
	return on == 3 || on == 2 && b.alive(x, y)
}

func (b Board) alive(x, y int) bool {
	if x < 0 || x >= b.w || y < 0 || y >= b.h {
		return false
	}

	return b.Cells[y][x]
}

type Game struct {
	logger *slog.Logger
	subs   map[*Sub]struct{}
	taps   chan tap

	current, next Board
	interval      time.Duration
	w, h          int

	mu sync.Mutex
}

type tap struct {
	x, y int
}

func NewGame(numCells uint, refreshInterval time.Duration, l *slog.Logger) *Game {
	logger := l.WithGroup("game")

	sqrt := int(math.Sqrt(float64(numCells)))
	n := uint(sqrt * sqrt)
	logFn := logger.Info
	if numCells != n {
		numCells = n
		logFn = logger.Warn
	}

	w, h := sqrt, sqrt
	logFn(fmt.Sprintf("effective num-cells: %d (%dx%d)", numCells, w, h))

	a := newBoard(w, h)
	for i := 0; i < (w * h / 10); i++ {
		a.set(rand.Intn(w), rand.Intn(h), true)
	}

	return &Game{
		current:  a,
		next:     newBoard(w, h),
		interval: refreshInterval,
		w:        w,
		h:        h,
		subs:     make(map[*Sub]struct{}),
		logger:   logger,
		taps:     make(chan tap, 10), // 10 as sensible default to avoid blocking clients
	}
}

func (g *Game) Start(ctx context.Context) {
	t := time.NewTicker(g.interval)
	defer t.Stop()

	g.logger.Debug("game started", "w", g.w, "h", g.h, "refresh-interval", g.interval)
	for {
		select {
		case <-ctx.Done():
			g.logger.Debug("game stopped", "err", ctx.Err())
			return
		case <-t.C:
			// Nobody here, lets not advance to the next generation
			if g.SubCount() == 0 {
				continue
			}

			g.applyTaps() // Drains queued taps so they are all applied before the next generation
			g.step()
			g.publish()
		}
	}
}

func (g *Game) applyTaps() {
	for {
		select {
		case tap := <-g.taps:
			g.current.set(tap.x, tap.y, true)
		default:
			return
		}
	}
}

func (g *Game) Set(x, y int) error {
	err := g.validCoordinates(x, y)
	if err != nil {
		return err
	}

	g.taps <- tap{x: x, y: y}
	return nil
}

func (g *Game) step() {
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			g.next.set(x, y, g.current.nextAlive(x, y))
		}
	}
	g.current, g.next = g.next, g.current
}

func (g *Game) publish() {
	state := Board{
		Cells: g.current.Cells.clone(),
		w:     g.current.w,
		h:     g.current.h,
	}

	g.mu.Lock()
	subs := make([]*Sub, 0, len(g.subs))
	for sub := range g.subs {
		subs = append(subs, sub)
	}
	g.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub.StateCh <- state:
		default:
			// Subscriber is behind; drop this generation.
		}
	}
}

func (g *Game) Sub() *Sub {
	s := &Sub{StateCh: make(chan Board, 1)}
	g.mu.Lock()
	g.subs[s] = struct{}{}
	g.mu.Unlock()
	return s
}

func (g *Game) Unsub(s *Sub) {
	g.mu.Lock()
	delete(g.subs, s)
	g.mu.Unlock()
}

func (g *Game) validCoordinates(x, y int) error {
	if x < 0 || x >= g.w {
		return errors.New("invalid x coordinate")
	}
	if y < 0 || y >= g.h {
		return errors.New("invalid y coordinate")
	}
	return nil
}

func (g *Game) SubCount() int {
	g.mu.Lock()
	n := len(g.subs)
	g.mu.Unlock()
	return n
}

type Sub struct {
	StateCh chan Board
}
