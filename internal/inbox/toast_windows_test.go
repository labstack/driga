package inbox

import (
	"errors"
	"testing"

	"github.com/labstack/driga/internal/wincom"
)

func requireToastRuntime(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, wincom.HRESULT(0x80040154)) || errors.Is(err, wincom.HRESULT(0x80040111)) {
		t.Skipf("Windows image has no registered WinRT toast/XML classes; doctor reports notifications unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestWindowsToastRuntimeFactories(t *testing.T) {
	// Instantiate native classes only. No registry writes or visible toast, even
	// on a developer's desktop or CI runner.
	done, err := wincom.Initialize()
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	doc, err := wincom.Activate("Windows.Data.Xml.Dom.XmlDocument")
	requireToastRuntime(t, err)
	defer doc.Release()
	io, err := doc.Query("{6CD0E74E-EE65-4489-9EBF-CA43E87BA637}")
	if err != nil {
		t.Fatal(err)
	}
	defer io.Release()
	payload, err := toastPayload(Notification{Title: "界 & title", Body: "<body>"})
	if err != nil {
		t.Fatal(err)
	}
	text, err := wincom.String(payload)
	if err != nil {
		t.Fatal(err)
	}
	defer text.Close()
	if err := io.Call(6, uintptr(text)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ class, iid string }{
		{"Windows.UI.Notifications.ToastNotification", "{04124B20-82C6-4229-B109-FD9ED4662B53}"},
		{"Windows.UI.Notifications.ToastNotificationManager", "{50AC103F-D235-4598-BBEF-98FE4D1A3AD4}"},
	} {
		factory, err := wincom.Factory(tc.class, tc.iid)
		requireToastRuntime(t, err)
		factory.Release()
	}
}
