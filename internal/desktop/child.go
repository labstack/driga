package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
// lifeline. The collector writes its own log, so its output is discarded.
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
