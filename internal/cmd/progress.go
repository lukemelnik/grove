package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

var progressFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	progressDelay    = 150 * time.Millisecond
	progressInterval = 80 * time.Millisecond
	progressClear    = "\r\x1b[2K"
)

type terminalProgress struct {
	writer  io.Writer
	enabled bool

	mu       sync.Mutex
	label    string
	active   bool
	rendered bool
	started  time.Time
	frame    int

	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newCommandProgress(cmd *cobra.Command) *terminalProgress {
	writer := cmd.ErrOrStderr()
	file, isFile := writer.(*os.File)
	enabled := !shouldOutputJSON(cmd) && isFile && isTerminal(int(file.Fd()))
	return newTerminalProgress(writer, enabled)
}

func newTerminalProgress(writer io.Writer, enabled bool) *terminalProgress {
	p := &terminalProgress{writer: writer, enabled: enabled}
	if !enabled {
		return p
	}
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	go p.run()
	return p
}

func (p *terminalProgress) run() {
	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	defer close(p.done)

	for {
		select {
		case now := <-ticker.C:
			p.render(now)
		case <-p.stop:
			p.mu.Lock()
			p.clearLocked()
			p.active = false
			p.mu.Unlock()
			return
		}
	}
}

func (p *terminalProgress) Update(label string) {
	if p == nil || !p.enabled {
		return
	}
	label = strings.TrimSpace(label)
	if label == "" {
		p.Clear()
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.clearLocked()
	p.label = label
	p.active = true
	p.rendered = false
	p.started = time.Now()
	p.frame = 0
}

func (p *terminalProgress) Clear() {
	if p == nil || !p.enabled {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clearLocked()
	p.active = false
}

func (p *terminalProgress) Stop() {
	if p == nil || !p.enabled {
		return
	}
	p.stopOnce.Do(func() { close(p.stop) })
	<-p.done
}

func (p *terminalProgress) render(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.active || now.Sub(p.started) < progressDelay {
		return
	}
	_, _ = fmt.Fprint(p.writer, formatProgressFrame(p.frame, p.label))
	p.rendered = true
	p.frame = (p.frame + 1) % len(progressFrames)
}

func (p *terminalProgress) clearLocked() {
	if !p.rendered {
		return
	}
	_, _ = io.WriteString(p.writer, progressClear)
	p.rendered = false
}

func formatProgressFrame(frame int, label string) string {
	if frame < 0 {
		frame = 0
	}
	return progressClear + progressFrames[frame%len(progressFrames)] + " " + label
}
