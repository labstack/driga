package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type installOrderService struct {
	calls                *[]string
	installErr, startErr error
}

func (s installOrderService) Install() error {
	*s.calls = append(*s.calls, "install")
	return s.installErr
}
func (s installOrderService) Start() error { *s.calls = append(*s.calls, "start"); return s.startErr }

func TestInstallServiceOrdering(t *testing.T) {
	failure := errors.New("injected failure")
	for _, tc := range []struct {
		name                                     string
		installErr, removeErr, hintErr, startErr error
		want                                     []string
		message                                  string
	}{
		{name: "success", want: []string{"install", "remove", "hint", "start"}},
		{name: "failed install keeps legacy collector", installErr: failure, want: []string{"install"}},
		{name: "failed removal stops before start", removeErr: failure, want: []string{"install", "remove"}, message: "remove the collector an earlier release installed"},
		{name: "failed hint stops before start", hintErr: failure, want: []string{"install", "remove", "hint"}},
		{name: "failed start explains installed state", startErr: failure, want: []string{"install", "remove", "hint", "start"}, message: "installed the Driga service; start it with driga service start"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			service := installOrderService{calls: &calls, installErr: tc.installErr, startErr: tc.startErr}
			err := installService(service, func() error { calls = append(calls, "remove"); return tc.removeErr }, func() error { calls = append(calls, "hint"); return tc.hintErr })
			if !reflect.DeepEqual(calls, tc.want) {
				t.Errorf("service calls: %v, want %v", calls, tc.want)
			}
			if tc.name == "success" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatalf("lost original error: %v", err)
			}
			if tc.message != "" && (err == nil || !strings.Contains(err.Error(), tc.message)) {
				t.Fatalf("missing context %q: %v", tc.message, err)
			}
		})
	}
}
