package logger

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	colorReset  = "\x1b[0m"
	colorCyan   = "\x1b[36m"
	colorGreen  = "\x1b[32m"
	colorYellow = "\x1b[33m"
	colorRed    = "\x1b[31m"
)

var (
	mu     sync.Mutex
	output io.Writer = os.Stdout
)

func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	if w == nil {
		output = os.Stdout
		return
	}
	output = w
}

func Info(host, format string, args ...any) {
	logf("INFO", host, format, args...)
}

func Warn(host, format string, args ...any) {
	logf("WARN", host, format, args...)
}

func Error(host, format string, args ...any) {
	logf("ERROR", host, format, args...)
}

func logf(level, host, format string, args ...any) {
	line := formatLine(level, host, fmt.Sprintf(format, args...))
	mu.Lock()
	defer mu.Unlock()
	_, _ = io.WriteString(output, line+"\n")
}

func formatLine(level, host, msg string) string {
	ts := time.Now().Format("2006-01-02 15:04:05")
	return fmt.Sprintf("[%s] [%s%s%s] %s%s%s %s", ts, colorCyan, host, colorReset, levelColor(level), level, colorReset, msg)
}

func levelColor(level string) string {
	switch strings.ToUpper(level) {
	case "WARN":
		return colorYellow
	case "ERROR":
		return colorRed
	default:
		return colorGreen
	}
}

// PrefixWriter prefixes each line with host and level.
func PrefixWriter(host, level string) io.Writer {
	return &prefixWriter{host: host, level: strings.ToUpper(level)}
}

type prefixWriter struct {
	host  string
	level string
	buf   bytes.Buffer
	mu    sync.Mutex
}

func (w *prefixWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	total := len(p)
	for len(p) > 0 {
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			_, _ = w.buf.Write(p)
			break
		}
		_, _ = w.buf.Write(p[:idx])
		w.flushLocked()
		p = p[idx+1:]
	}
	return total, nil
}

func (w *prefixWriter) flushLocked() {
	line := strings.TrimSpace(w.buf.String())
	w.buf.Reset()
	if line == "" {
		return
	}
	logf(w.level, w.host, "%s", line)
}
