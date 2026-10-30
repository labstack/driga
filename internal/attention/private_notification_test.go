package attention

import (
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/model"
	"strings"
	"testing"
	"time"
)

type privateNotificationRunner struct {
	ctx   context.Context
	input []byte
	args  []string
}

func (r *privateNotificationRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "open" || strings.Join(args, " ") != "-b run.driga" {
		panic("notification used argv transport")
	}
	return nil, nil
}
func (r *privateNotificationRunner) RunInput(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
	r.ctx = ctx
	r.input = append([]byte(nil), input...)
	r.args = append([]string{name}, args...)
	return nil, nil
}
func TestNotificationTargetUsesPrivateInput(t *testing.T) {
	r := &privateNotificationRunner{}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := notificationResponse(ctx, r, "/Applications/Driga.app/Contents/Helpers/Driga Service.app/Contents/MacOS/drigad", notificationDefaultAction, "private-item", "private-machine", "machine_health"); err != nil {
		t.Fatal(err)
	}
	if r.ctx.Done() != nil {
		t.Fatal("notification callback must not own the GUI lifetime")
	}
	if strings.Contains(strings.Join(r.args, " "), "private-") {
		t.Fatal(r.args)
	}
	var target model.NotificationTarget
	if err := json.Unmarshal(r.input, &target); err != nil || target.ItemID != "private-item" || target.MachineID != "private-machine" || target.Kind != "machine_health" {
		t.Fatalf("%+v %v", target, err)
	}
}
