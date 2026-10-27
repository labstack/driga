package platform

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// StartChild suspends the initial thread until the child belongs to a new,
// non-breakaway job. This closes the spawn/assignment race: even grandchildren
// created immediately at startup are owned. Closing our non-inherited handle
// ends the entire job, including when driga crashes or a parent exits first.
func StartChild(cmd *exec.Cmd) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	var h windows.Handle
	var mu sync.Mutex
	var once sync.Once
	closeJob := func() {
		once.Do(func() {
			mu.Lock()
			defer mu.Unlock()
			_ = windows.CloseHandle(job)
			if h != 0 {
				_ = windows.CloseHandle(h)
				h = 0
			}
		})
	}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		closeJob()
		return nil, err
	}
	attr := syscall.SysProcAttr{}
	if cmd.SysProcAttr != nil {
		attr = *cmd.SysProcAttr
	}
	// These children use pipes or files, never an interactive console. Without
	// CREATE_NO_WINDOW, a console-less app or collector allocates a new console
	// for every Git probe or agent call (HideWindow alone still allocates one).
	attr.CreationFlags |= windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW
	cmd.SysProcAttr = &attr
	// Console-less children cannot receive CTRL_BREAK. Cancellation retains
	// the window-close attempt, grace period and job termination fallback.
	ready := make(chan struct{})
	var readyOnce sync.Once
	markReady := func() { readyOnce.Do(func() { close(ready) }) }
	if cmd.Cancel != nil {
		cmd.Cancel = func() error {
			<-ready
			mu.Lock()
			defer mu.Unlock()
			if h == 0 {
				return nil
			}
			return GracefulStop(h, uint32(cmd.Process.Pid), true)
		}
	}
	if cmd.WaitDelay <= 0 {
		cmd.WaitDelay = 2 * time.Second
	}
	if err := cmd.Start(); err != nil {
		markReady()
		closeJob()
		return nil, err
	}
	fail := func(err error) (func(), error) {
		_ = cmd.Process.Kill() // Only this suspended child; it cannot have spawned.
		markReady()
		_ = cmd.Wait()
		closeJob()
		return nil, err
	}
	h, err = windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fail(err)
	}
	err = windows.AssignProcessToJobObject(job, h)
	if err != nil {
		return fail(fmt.Errorf("assign child job: %w", err))
	}
	if err := resumeChild(uint32(cmd.Process.Pid)); err != nil {
		return fail(err)
	}
	markReady()
	return closeJob, nil
}

func resumeChild(pid uint32) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snap)
	e := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snap, &e); err == nil; err = windows.Thread32Next(snap, &e) {
		if e.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, e.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		return err
	}
	return fmt.Errorf("suspended child's initial thread not found: %w", err)
}

func sharedConsole(pid uint32) bool {
	ids := make([]uint32, 4096)
	n, _, _ := winKernel.NewProc("GetConsoleProcessList").Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	if n == 0 || n > uintptr(len(ids)) {
		return false
	}
	for _, id := range ids[:n] {
		if id == pid {
			return true
		}
	}
	return false
}

func RunChild(cmd *exec.Cmd) error {
	stop, err := StartChild(cmd)
	if err != nil {
		return err
	}
	defer stop()
	return cmd.Wait()
}

func ChildOutput(cmd *exec.Cmd, combined bool) ([]byte, error) {
	if cmd.Stdout != nil || combined && cmd.Stderr != nil {
		return nil, errors.New("child output already set")
	}
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	if combined {
		cmd.Stderr = &out
	} else {
		cmd.Stderr = &stderr
	}
	err := RunChild(cmd)
	if exit, ok := err.(*exec.ExitError); ok && !combined {
		exit.Stderr = stderr.Bytes()
	}
	return out.Bytes(), err
}
