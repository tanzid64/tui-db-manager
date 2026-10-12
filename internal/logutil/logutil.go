package logutil

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelError
)

type Logger struct {
	mu      sync.Mutex
	w       io.Writer
	level   Level
	secrets []string
}

func New(w io.Writer, level Level) *Logger { return &Logger{w: w, level: level} }

// Redact registers a literal secret to be masked in all future output.
func (l *Logger) Redact(secret string) {
	if secret == "" {
		return
	}
	l.mu.Lock()
	l.secrets = append(l.secrets, secret)
	l.mu.Unlock()
}

func (l *Logger) mask(s string) string {
	for _, sec := range l.secrets {
		s = strings.ReplaceAll(s, sec, "***")
	}
	return s
}

func (l *Logger) logf(level Level, prefix, format string, args ...any) {
	if level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "%s %s\n", prefix, l.mask(fmt.Sprintf(format, args...)))
}

// MaskError returns err with every occurrence of each secret literal replaced
// by *** in its message, so a secret that reaches any error sink (including
// ones that bypass the logger, such as the CLI's top-level os.Stderr print) is
// scrubbed. A nil error or empty secret passes through unchanged.
func MaskError(err error, secrets ...string) error {
	if err == nil {
		return err
	}
	msg := err.Error()
	changed := false
	for _, s := range secrets {
		if s == "" {
			continue
		}
		if strings.Contains(msg, s) {
			msg = strings.ReplaceAll(msg, s, "***")
			changed = true
		}
	}
	if !changed {
		return err
	}
	return errors.New(msg)
}

func (l *Logger) Debugf(f string, a ...any) { l.logf(LevelDebug, "[debug]", f, a...) }
func (l *Logger) Infof(f string, a ...any)  { l.logf(LevelInfo, "[info]", f, a...) }
func (l *Logger) Errorf(f string, a ...any) { l.logf(LevelError, "[error]", f, a...) }
