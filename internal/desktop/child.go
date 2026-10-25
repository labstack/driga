package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// stopTimeout bounds a graceful collector shutdown before the child is killed.
const stopTimeout = 15 * time.Second

// execChild is a `driga collect` process owned by the app. It holds the write
// end of the child's lifeline pipe: closing it, or the app dying, makes the
// collector shut down, so a collector never outlives the app.
type execChild struct {
	cmd      *exec.Cmd
	notes    *closingReader
	lifeline *os.File
	done     chan struct{}
	err      error
	stopOnce sync.Once
	stopErr  error
}

// StartCollector runs exe as the app's collector with login merged over a
// minimal environment. Notifications arrive on one pipe; the other is the
// lifeline. The collector writes its own log once it runs; what it prints
// before that, such as why it could not start, goes to the app's log and
// ends the error Wait returns.
func StartCollector(_ context.Context, exe string, login map[string]string) (Child, error) {
	notifyR, notifyW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	parentR, parentW, err := os.Pipe()
	if err != nil {
		return nil, errors.Join(err, notifyR.Close(), notifyW.Close())
	}
	cmd := exec.Command(exe)
	cmd.Env = childEnvironment(login)
	output := &outputLog{}
	cmd.Stdout, cmd.Stderr = output, output
	// A program the collector started may hold its output open after it exits.
	cmd.WaitDelay = time.Second
	notifyFD, parentFD, err := passFiles(cmd, notifyW, parentR)
	if err == nil {
		cmd.Args = append(cmd.Args, "collect", "--owner=app", fmt.Sprintf("--notify-fd=%d", notifyFD), fmt.Sprintf("--parent-fd=%d", parentFD))
		err = cmd.Start()
	}
	// The child holds its own copies; the app keeps only its ends.
	closeErr := errors.Join(notifyW.Close(), parentR.Close())
	if err == nil {
		err = closeErr
	}
	if err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		return nil, errors.Join(err, notifyR.Close(), parentW.Close())
	}
	c := &execChild{cmd: cmd, notes: &closingReader{f: notifyR}, lifeline: parentW, done: make(chan struct{})}
	go func() {
		c.err = cmd.Wait()
		output.flush()
		if line := output.last(); c.err != nil && line != "" {
			c.err = fmt.Errorf("%w: %s", c.err, line)
		}
		_ = c.lifeline.Close()
		close(c.done)
	}()
	return c, nil
}

func (c *execChild) Wait() error {
	<-c.done
	return c.err
}

// Stop closes the lifeline and waits for the collector to finish its work,
// killing it if it does not exit in time.
func (c *execChild) Stop() error {
	c.stopOnce.Do(func() {
		_ = c.lifeline.Close()
		select {
		case <-c.done:
		case <-time.After(stopTimeout):
			// It may exit between the timeout and the kill.
			if err := c.cmd.Process.Kill(); !errors.Is(err, os.ErrProcessDone) {
				c.stopErr = err
			}
			<-c.done
		}
	})
	return c.stopErr
}

func (c *execChild) Notifications() io.Reader { return c.notes }

// closingReader closes the pipe once the child's stream ends, after every
// buffered notification has been read.
type closingReader struct{ f *os.File }

func (r *closingReader) Read(p []byte) (int, error) {
	n, err := r.f.Read(p)
	if err != nil {
		_ = r.f.Close()
	}
	return n, err
}

// outputLog logs each line the collector prints and keeps the last one,
// which is why it exited when it could not start.
type outputLog struct {
	mu      sync.Mutex
	partial []byte
	line    string
}

// maxOutputLine bounds a line held while it has no newline yet.
const maxOutputLine = 4 << 10

func (o *outputLog) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.partial = append(o.partial, p...)
	for {
		i := bytes.IndexByte(o.partial, '\n')
		if i < 0 {
			break
		}
		o.emit(o.partial[:i])
		o.partial = o.partial[i+1:]
	}
	if len(o.partial) > maxOutputLine {
		o.emit(o.partial)
		o.partial = nil
	}
	return len(p), nil
}

// flush logs a last line that ended without a newline.
func (o *outputLog) flush() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.emit(o.partial)
	o.partial = nil
}

func (o *outputLog) emit(b []byte) {
	line := strings.TrimSpace(string(b))
	if line == "" {
		return
	}
	// The CLI prefixes its fatal error with its name.
	o.line = strings.TrimPrefix(line, "driga: ")
	slog.Warn("collector output", "event", "collector_output", "line", line)
}

func (o *outputLog) last() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.line
}
