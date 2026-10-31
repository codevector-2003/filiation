package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// status is the progress line a long command keeps rewriting on standard
// error. Plain, it prints exactly what fil always has: "\r  <text> ". In
// colour it also turns a spinner and, once nothing has changed for a few
// seconds, says how long it has been waiting, so a slow download never looks
// like a hung program.
type status struct {
	mu      sync.Mutex
	text    string
	changed time.Time
	stop    chan struct{}
	done    chan struct{}
	frame   int
}

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// quietAfter is how long the text may stand still before the wait is shown.
const quietAfter = 3 * time.Second

// setStatus shows text as the current progress.
func (a *app) setStatus(text string) {
	if !a.errPaint.on {
		fmt.Fprintf(a.stderr, "\r  %s ", text)
		return
	}
	st := &a.status
	st.mu.Lock()
	defer st.mu.Unlock()
	st.text, st.changed = text, time.Now()
	if st.stop == nil {
		st.stop, st.done = make(chan struct{}), make(chan struct{})
		go a.spin(st.stop, st.done)
	}
	a.drawStatus(true)
}

// endStatus finishes the progress line, so what follows starts on its own.
func (a *app) endStatus() {
	st := &a.status
	st.mu.Lock()
	stop, done := st.stop, st.done
	st.stop, st.done = nil, nil
	st.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
		st.mu.Lock()
		a.drawStatus(false)
		st.mu.Unlock()
	}
	fmt.Fprintln(a.stderr)
}

func (a *app) spin(stop, done chan struct{}) {
	defer close(done)
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			a.status.mu.Lock()
			a.status.frame++
			a.drawStatus(true)
			a.status.mu.Unlock()
		}
	}
}

// drawStatus redraws the line; the caller holds the lock. live adds the
// spinner and the wait, done marks the line finished.
func (a *app) drawStatus(live bool) {
	st, p := &a.status, a.errPaint
	var b strings.Builder
	b.WriteString("\r\x1b[2K  ")
	if live {
		b.WriteString(p.accent(spinner[st.frame%len(spinner)]))
	} else {
		b.WriteString(p.ok("✓"))
	}
	b.WriteString(" ")
	b.WriteString(st.text)
	if waited := time.Since(st.changed); live && waited >= quietAfter {
		b.WriteString(p.dim(fmt.Sprintf(" · waiting %d s", int(waited.Seconds()))))
	}
	fmt.Fprint(a.stderr, b.String())
}
