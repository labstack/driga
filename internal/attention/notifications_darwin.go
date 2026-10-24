//go:build darwin

package attention

import (
	"context"
	"fmt"
	"os"
	"structs"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Published before the CLI goroutine starts; never mutated afterwards. Tests
// inject NativeNotificationCenter instead of initializing this native backend.
var macNotifier Notifier = &MacNotifier{}
var macStartupErr error

func MacDesktopNotifier() Notifier { return macNotifier }

func nsClass(name string) objc.ID { return objc.ID(objc.GetClass(name)) }
func nsString(s string) objc.ID {
	return nsClass("NSString").Send(objc.RegisterName("stringWithUTF8String:"), s)
}
func goString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	return objc.Send[string](s, objc.RegisterName("UTF8String"))
}
func autoreleasePool() func() {
	pool := nsClass("NSAutoreleasePool").Send(objc.RegisterName("new"))
	return func() { pool.Send(objc.RegisterName("drain")) }
}

func MacNotificationStatus() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if !drigaAppExecutable(exe) {
		return "", fmt.Errorf("install Driga.app for native macOS notifications (unbundled builds cannot send them)")
	}
	if macStartupErr != nil {
		return "", macStartupErr
	}
	if _, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return "", err
	}
	defer autoreleasePool()()
	bundle := nsClass("NSBundle").Send(objc.RegisterName("mainBundle"))
	if goString(bundle.Send(objc.RegisterName("bundleIdentifier"))) != "run.driga" {
		return "", fmt.Errorf("Driga.app must have the run.driga bundle identity; reinstall the app")
	}
	switch macAuthorizationStatus() {
	case 1:
		return "", ErrNotificationsDenied
	case 0:
		return "native, not asked yet: macOS asks when Driga first notifies", nil
	case -1:
		return "native UserNotifications (Driga); permission unknown", nil
	}
	return "native UserNotifications (Driga), allowed", nil
}

// macAuthorizationStatus reads UNAuthorizationStatus without prompting:
// 0 not determined, 1 denied, 2+ allowed. -1 when macOS does not answer.
func macAuthorizationStatus() int64 {
	if _, err := purego.Dlopen("/System/Library/Frameworks/UserNotifications.framework/UserNotifications", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return -1
	}
	defer autoreleasePool()()
	result := make(chan int64, 1)
	block := objc.NewBlock(func(_ objc.Block, settings objc.ID) {
		result <- objc.Send[int64](settings, objc.RegisterName("authorizationStatus"))
	})
	defer block.Release()
	center := nsClass("UNUserNotificationCenter").Send(objc.RegisterName("currentNotificationCenter"))
	center.Send(objc.RegisterName("getNotificationSettingsWithCompletionHandler:"), block)
	select {
	case status := <-result:
		return status
	case <-time.After(2 * time.Second):
		return -1
	}
}

// RunNotificationMain is called only by the executable's locked initial thread.
// Keep NSApplication's main run loop alive for the entire daemon/viewer lifetime:
// locking an arbitrary worker thread is insufficient for Cocoa/dispatch callbacks.
// run owns process exit, just as the ordinary CLI main does.
func RunNotificationMain(run func()) {
	exe, _ := os.Executable()
	if !drigaAppExecutable(exe) {
		run()
		return
	}
	app, center, err := startMacNotifications(exe)
	if err != nil {
		macStartupErr = err
		run()
		return
	}
	macNotifier = &MacNotifier{Center: center}
	go run()
	defer autoreleasePool()()
	app.Send(objc.RegisterName("run"))
}

type macNotificationCenter struct {
	center, delegate objc.ID // UNUserNotificationCenter.delegate is weak: retain it ourselves.
	queue            uintptr
	dispatchAsync    func(uintptr, objc.Block)
}

func startMacNotifications(exe string) (objc.ID, *macNotificationCenter, error) {
	for _, framework := range []string{"AppKit", "UserNotifications"} {
		if _, err := purego.Dlopen("/System/Library/Frameworks/"+framework+".framework/"+framework, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			return 0, nil, err
		}
	}
	defer autoreleasePool()()
	if _, err := MacNotificationStatus(); err != nil {
		return 0, nil, err
	}
	app := nsClass("NSApplication").Send(objc.RegisterName("sharedApplication"))
	// Accessory app: no Dock icon or window, while retaining Driga's bundle identity.
	app.Send(objc.RegisterName("setActivationPolicy:"), int64(1))
	c := &macNotificationCenter{center: nsClass("UNUserNotificationCenter").Send(objc.RegisterName("currentNotificationCenter"))}
	var err error
	c.queue, err = purego.Dlsym(purego.RTLD_DEFAULT, "_dispatch_main_q")
	if err != nil {
		return 0, nil, err
	}
	purego.RegisterLibFunc(&c.dispatchAsync, purego.RTLD_DEFAULT, "dispatch_async")
	delegate, err := objc.RegisterClass("DrigaNotificationDelegate", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("UNUserNotificationCenterDelegate")}, nil, []objc.MethodDef{
			{Cmd: objc.RegisterName("userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:"),
				Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, response objc.ID, completion objc.Block) {
					defer autoreleasePool()()
					// Copy all values before returning to Cocoa; no borrowed objects
					// cross the asynchronous jump boundary. Always complete, even for dismissals.
					action := goString(response.Send(objc.RegisterName("actionIdentifier")))
					info := response.Send(objc.RegisterName("notification")).Send(objc.RegisterName("request")).Send(objc.RegisterName("content")).Send(objc.RegisterName("userInfo"))
					item := goString(info.Send(objc.RegisterName("objectForKey:"), nsString("itemID")))
					completeNativeBlock(completion)
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
						defer cancel()
						_ = notificationResponse(ctx, CommandRunner{}, exe, action, item)
					}()
				}},
			{Cmd: objc.RegisterName("userNotificationCenter:willPresentNotification:withCompletionHandler:"),
				Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID, _ objc.ID, completion objc.Block) {
					// Sound + banner + Notification Center list, even in the foreground.
					presentNativeBlock(completion, 1<<1|1<<3|1<<4)
				}},
		})
	if err != nil {
		return 0, nil, err
	}
	c.delegate = objc.ID(delegate).Send(objc.RegisterName("new"))
	c.center.Send(objc.RegisterName("setDelegate:"), c.delegate)
	// NSApplication.run finishes launching after the delegate is installed.
	return app, c, nil
}

func (c *macNotificationCenter) onMain(fn func()) {
	block := objc.NewBlock(func(_ objc.Block) {
		defer autoreleasePool()()
		fn()
	})
	c.dispatchAsync(c.queue, block) // dispatch copies the block until execution.
	block.Release()
}

func nativeNotificationError(err objc.ID) error {
	if err == 0 {
		return nil
	}
	return fmt.Errorf("native macOS notifications: %s", goString(err.Send(objc.RegisterName("localizedDescription"))))
}

func (c *macNotificationCenter) Authorize(complete func(error)) {
	c.onMain(func() {
		block := objc.NewBlock(func(_ objc.Block, granted bool, err objc.ID) {
			defer autoreleasePool()()
			e := nativeNotificationError(err)
			if e == nil && !granted {
				e = fmt.Errorf("notifications unavailable: allow Driga in System Settings > Notifications")
			}
			complete(e)
		})
		defer block.Release()
		c.center.Send(objc.RegisterName("requestAuthorizationWithOptions:completionHandler:"), uintptr(1<<1|1<<2), block)
	})
}

func (c *macNotificationCenter) Add(ctx context.Context, n NativeNotificationRequest) error {
	result := make(chan error, 1)
	c.onMain(func() {
		if err := ctx.Err(); err != nil {
			result <- err
			return
		}
		content := nsClass("UNMutableNotificationContent").Send(objc.RegisterName("new"))
		defer content.Send(objc.RegisterName("release"))
		content.Send(objc.RegisterName("setTitle:"), nsString(n.Title))
		content.Send(objc.RegisterName("setBody:"), nsString(n.Body))
		content.Send(objc.RegisterName("setThreadIdentifier:"), nsString(n.ThreadIdentifier))
		if n.Sound != "" {
			content.Send(objc.RegisterName("setSound:"), nsClass("UNNotificationSound").Send(objc.RegisterName("defaultSound")))
		}
		info := nsClass("NSMutableDictionary").Send(objc.RegisterName("dictionary"))
		info.Send(objc.RegisterName("setObject:forKey:"), nsString(n.ItemID), nsString("itemID"))
		info.Send(objc.RegisterName("setObject:forKey:"), nsString(n.MachineID), nsString("machineID"))
		content.Send(objc.RegisterName("setUserInfo:"), info)
		request := nsClass("UNNotificationRequest").Send(objc.RegisterName("requestWithIdentifier:content:trigger:"), nsString(n.Identifier), content, objc.ID(0))
		block := objc.NewBlock(func(_ objc.Block, err objc.ID) {
			defer autoreleasePool()()
			result <- nativeNotificationError(err)
		})
		defer block.Release()
		// The same identifier replaces pending AND delivered notifications.
		c.center.Send(objc.RegisterName("addNotificationRequest:withCompletionHandler:"), request, block)
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-result:
		return err
	}
}

// objc.Block.Invoke dispatches Go-created blocks only. Delegate completion
// handlers belong to Cocoa; call their ABI invoke pointer instead.
type nativeBlockHeader struct {
	_               structs.HostLayout
	isa             uintptr
	flags, reserved uint32
	invoke          uintptr
}

func nativeBlockInvoke(block objc.Block) uintptr {
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&block))
	return (*nativeBlockHeader)(ptr).invoke
}
func completeNativeBlock(block objc.Block) {
	var call func(objc.Block)
	purego.RegisterFunc(&call, nativeBlockInvoke(block))
	call(block)
}
func presentNativeBlock(block objc.Block, options uintptr) {
	var call func(objc.Block, uintptr)
	purego.RegisterFunc(&call, nativeBlockInvoke(block))
	call(block, options)
}
