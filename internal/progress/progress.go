// Package progress renders a single-line transfer progress bar on stderr.
package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// IsTerminal reports whether f is attached to a character device (console).
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Bar tracks the progress of one operation (upload or download) made of several files.
type Bar struct {
	w        io.Writer
	tty      bool
	enabled  bool
	total    int64
	done     atomic.Int64
	start    time.Time
	mu       sync.Mutex
	label    string
	lastLine int
	lastDraw time.Time
	stop     chan struct{}
	wg       sync.WaitGroup
}

// New creates a bar writing to w. When enabled is false, nothing is ever printed.
func New(w io.Writer, tty bool, enabled bool, total int64) *Bar {
	b := &Bar{w: w, tty: tty, enabled: enabled, total: total, start: time.Now(), stop: make(chan struct{})}
	if enabled && tty {
		b.wg.Add(1)
		go b.loop()
	}
	return b
}

func (b *Bar) loop() {
	defer b.wg.Done()
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			b.draw(false)
		case <-b.stop:
			return
		}
	}
}

// SetLabel changes the name shown on the line (typically the current file).
func (b *Bar) SetLabel(s string) {
	b.mu.Lock()
	b.label = s
	b.mu.Unlock()
}

// Add records n more transferred bytes.
func (b *Bar) Add(n int64) { b.done.Add(n) }

// Done returns the number of bytes transferred so far.
func (b *Bar) Done() int64 { return b.done.Load() }

// FileDone prints a one-line summary for a finished file when not on a terminal.
func (b *Bar) FileDone(path string, size int64) {
	if !b.enabled || b.tty {
		return
	}
	fmt.Fprintf(b.w, "%s  %s\n", path, Bytes(size))
}

// Finish stops the ticker and prints the final state.
func (b *Bar) Finish() {
	if !b.enabled {
		return
	}
	if b.tty {
		close(b.stop)
		b.wg.Wait()
		b.draw(true)
		fmt.Fprintln(b.w)
	} else {
		el := time.Since(b.start)
		fmt.Fprintf(b.w, "%s in %s (%s/s)\n", Bytes(b.done.Load()), el.Round(time.Millisecond*100), Bytes(rate(b.done.Load(), el)))
	}
}

// Clear erases the current line so another message can be printed, then redraws on next tick.
func (b *Bar) Clear() {
	if !b.enabled || !b.tty {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lastLine > 0 {
		fmt.Fprintf(b.w, "\r%s\r", strings.Repeat(" ", b.lastLine))
		b.lastLine = 0
	}
}

func (b *Bar) draw(final bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	done := b.done.Load()
	el := time.Since(b.start)
	r := rate(done, el)
	var pct float64
	if b.total > 0 {
		pct = float64(done) / float64(b.total) * 100
	}
	eta := "--"
	if r > 0 && b.total > done {
		eta = (time.Duration(float64(b.total-done)/float64(r)) * time.Second).Round(time.Second).String()
	}
	if final {
		eta = el.Round(time.Millisecond * 100).String()
	}
	label := b.label
	if len(label) > 32 {
		label = "…" + label[len(label)-31:]
	}
	line := fmt.Sprintf("%-32s %10s / %-10s %5.1f%%  %9s/s  %s", label, Bytes(done), Bytes(b.total), pct, Bytes(r), eta)
	pad := ""
	if n := b.lastLine - len(line); n > 0 {
		pad = strings.Repeat(" ", n)
	}
	fmt.Fprintf(b.w, "\r%s%s", line, pad)
	b.lastLine = len(line)
}

func rate(done int64, el time.Duration) int64 {
	if el <= 0 {
		return 0
	}
	return int64(float64(done) / el.Seconds())
}

// Bytes formats a byte count using binary units.
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Reader counts bytes read through it.
type Reader struct {
	R   io.Reader
	Bar *Bar
}

func (r *Reader) Read(p []byte) (int, error) {
	n, err := r.R.Read(p)
	if n > 0 && r.Bar != nil {
		r.Bar.Add(int64(n))
	}
	return n, err
}
