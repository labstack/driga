package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/secfile"
	"github.com/labstack/driga/internal/wincom"
	"golang.org/x/sys/windows"
)

type taskService struct {
	bin, sid, name, dataDir string
	run                     func(string, ...string) ([]byte, error)
}

func newWindowsService(bin string) (Controller, error) {
	if !filepath.IsAbs(bin) {
		return nil, errors.New("service binary path must be absolute")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := user.User.Sid.String()
	return &taskService{bin: bin, sid: sid, name: `\Driga\` + sid + `\Driga collector`, dataDir: config.DataDir(), run: runScheduler}, nil
}

func runScheduler(_ string, args ...string) ([]byte, error) {
	// Resolve from the system directory, never from an untrusted working directory.
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(system, "schtasks.exe"), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	b, err := cmd.CombinedOutput()
	if err != nil {
		return b, fmt.Errorf("Task Scheduler: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return b, nil
}
func (s *taskService) action(action string) error {
	args, err := taskArgs(action, s.name, "")
	if err != nil {
		return err
	}
	_, err = s.run("schtasks.exe", args...)
	return err
}
func (s *taskService) Install() error {
	if err := secfile.MkdirPrivate(s.dataDir); err != nil {
		return err
	}
	env := make([]string, 0, len(scheduledEnvKeys))
	// Persist only variables that are set: an empty HOME or TEMP must not
	// replace the login environment of a normal install.
	for _, key := range scheduledEnvKeys {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	b, err := taskXML(s.bin, s.sid, s.dataDir, env...)
	if err != nil {
		return err
	}
	f, err := secfile.CreateTemp(s.dataDir, ".driga-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(taskFile(b))
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	args, err := taskArgs("install", s.name, f.Name())
	if err != nil {
		return err
	}
	// No /F: never replace an existing task, even in our per-user namespace.
	_, err = s.run("schtasks.exe", args...)
	return err
}
func (s *taskService) Uninstall() error {
	state, err := s.TaskState()
	if err != nil {
		return err
	}
	if state == "running" || state == "queued" {
		if err := s.Stop(); err != nil {
			return err
		}
	}
	return s.action("uninstall")
}
func (s *taskService) Start() error { return s.action("start") }
func (s *taskService) Stop() error {
	state, err := s.TaskState()
	if err != nil {
		return err
	}
	if state != "running" && state != "queued" {
		return nil
	}
	if err := s.action("stop"); err != nil {
		return err
	}
	// /End is asynchronous. Do not let /Run race the old IgnoreNew instance.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state, err := s.TaskState()
		if err != nil {
			return err
		}
		// Task Scheduler reports ready before the ended collector exits; only a
		// released lock means a new run will not race the old instance.
		if state != "running" && state != "queued" && !s.collectorAlive() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("collector task did not stop within 10 seconds")
}

// collectorAlive checks the PID recorded in the collector lock file.
func (s *taskService) collectorAlive() bool {
	return collector.Running(s.dataDir)
}
func (s *taskService) Restart() error {
	if err := s.Stop(); err != nil {
		return err
	}
	return s.Start()
}
func (s *taskService) Status() (ks.Status, error) {
	state, err := s.TaskState()
	if err != nil {
		return ks.StatusUnknown, err
	}
	if state == "running" {
		return ks.StatusRunning, nil
	}
	return ks.StatusStopped, nil
}

// TaskState uses the numeric COM state, avoiding localized schtasks CSV labels.
// ABI source: Microsoft's taskschd.h, ITaskService/ITaskFolder/IRegisteredTask.
func (s *taskService) TaskState() (string, error) {
	done, err := wincom.Initialize()
	if err != nil {
		return "", err
	}
	defer done()
	if err := wincom.Security(); err != nil {
		return "", err
	}
	scheduler, err := wincom.Create("{0F87369F-A4E5-4CFC-BD3E-73E6154572DD}", "{2FABA4C7-4DA9-4013-9697-20CC3FD40F85}")
	if err != nil {
		return "", err
	}
	defer scheduler.Release()
	// VARIANT is passed by reference by both Windows 64-bit ABIs. VT_EMPTY is
	// all zero; reserve the full 24 bytes (including the BRECORD union).
	var empty [4][3]uint64
	if err := scheduler.Call(10, uintptr(unsafe.Pointer(&empty[0])), uintptr(unsafe.Pointer(&empty[1])), uintptr(unsafe.Pointer(&empty[2])), uintptr(unsafe.Pointer(&empty[3]))); err != nil {
		return "", err
	}
	root, free, err := wincom.BString(`\`)
	if err != nil {
		return "", err
	}
	defer free()
	var folder *wincom.Object
	if err := scheduler.Call(7, root, uintptr(unsafe.Pointer(&folder))); err != nil {
		return "", err
	}
	defer folder.Release()
	name, freeName, err := wincom.BString(s.name)
	if err != nil {
		return "", err
	}
	defer freeName()
	var task *wincom.Object
	err = folder.Call(13, name, uintptr(unsafe.Pointer(&task)))
	if errors.Is(err, wincom.HRESULT(0x80070002)) || errors.Is(err, wincom.HRESULT(0x80070003)) {
		return "not installed", ks.ErrNotInstalled
	}
	if err != nil {
		return "", err
	}
	defer task.Release()
	var state uint32
	if err := task.Call(9, uintptr(unsafe.Pointer(&state))); err != nil {
		return "", err
	}
	labels := []string{"unknown", "disabled", "queued", "ready", "running"}
	if state >= uint32(len(labels)) {
		return "unknown", nil
	}
	return labels[state], nil
}

// PrepareScheduled detaches only the scheduler-launched collector console.
func PrepareScheduled(dataDir string, env ...string) error {
	for _, value := range env {
		key, value, ok := strings.Cut(value, "=")
		if !ok || !scheduledEnvKey(key) {
			return errors.New("invalid scheduled collector environment")
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	if !filepath.IsAbs(dataDir) {
		return errors.New("scheduled collector requires an absolute data directory")
	}
	// FreeConsole closes only our console association; it never hides another
	// process's window (ShowWindow can affect a shared console).
	windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole").Call()
	return os.Setenv("DRIGA_DATA_DIR", dataDir)
}
