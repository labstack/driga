package service

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestTaskArgs(t *testing.T) {
	for _, tc := range []struct {
		action string
		want   []string
	}{
		{"install", []string{"/Create", "/TN", `\Driga\test`, "/XML", `C:\a b\task.xml`}},
		{"uninstall", []string{"/Delete", "/TN", `\Driga\test`, "/F"}},
		{"start", []string{"/Run", "/TN", `\Driga\test`}},
		{"stop", []string{"/End", "/TN", `\Driga\test`}},
		{"status", []string{"/Query", "/TN", `\Driga\test`, "/FO", "CSV", "/NH"}},
		{"xml", []string{"/Query", "/TN", `\Driga\test`, "/XML"}},
	} {
		got, err := taskArgs(tc.action, `\Driga\test`, `C:\a b\task.xml`)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatal(tc.action, got, err)
		}
	}
	if _, err := taskArgs("bad", `\Driga\test`, ""); err == nil {
		t.Fatal("unknown action")
	}
}

func TestTaskFileIsUTF16(t *testing.T) {
	b := []byte(`<?xml version="1.0" encoding="UTF-8"?><Task>Zoë</Task>`)
	f := taskFile(b)
	if f[0] != 0xff || f[1] != 0xfe || len(f)%2 != 0 {
		t.Fatalf("missing UTF-16LE byte order mark: % x", f[:4])
	}
	units := make([]uint16, (len(f)-2)/2)
	for i := range units {
		units[i] = uint16(f[2+2*i]) | uint16(f[3+2*i])<<8
	}
	got := string(utf16.Decode(units))
	if !strings.HasPrefix(got, `<?xml version="1.0" encoding="UTF-16"?>`) || !strings.Contains(got, `Zoë`) {
		t.Fatalf("UTF-16 task file: %.120s", got)
	}
}

func TestTaskArguments(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
 <Actions Context="User"><Exec><Command>C:\driga.exe</Command><Arguments>collect --service-data-dir "C:\a &amp; b"</Arguments></Exec></Actions>
</Task>
`
	want := `collect --service-data-dir "C:\a & b"`
	// schtasks /Query /XML prints the definition as UTF-16, as /Create reads it.
	for name, b := range map[string][]byte{"utf-8": []byte(doc), "utf-16": taskFile([]byte(doc))} {
		if got, err := taskArguments(b); err != nil || got != want {
			t.Fatalf("%s: arguments %q, %v", name, got, err)
		}
	}
	if _, err := taskArguments([]byte(`<Task><Actions/></Task>`)); err == nil {
		t.Fatal("task without an action read")
	}
}
