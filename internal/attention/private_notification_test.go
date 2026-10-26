package attention

import (
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/model"
	"strings"
	"testing"
)

type privateNotificationRunner struct {
	input []byte
	args  []string
}

func (r *privateNotificationRunner) Run(context.Context, string, ...string) ([]byte, error) {
	panic("notification used argv transport")
}
func (r *privateNotificationRunner) RunInput(_ context.Context, input []byte, name string, args ...string) ([]byte, error) {
	r.input = append([]byte(nil), input...)
	r.args = append([]string{name}, args...)
	return nil, nil
}
func TestNotificationTargetUsesPrivateInput(t *testing.T) {
	r := &privateNotificationRunner{}
	if err := notificationResponse(context.Background(), r, "/app/driga", notificationDefaultAction, "private-item", "private-machine", "machine_health"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(r.args, " "), "private-") {
		t.Fatal(r.args)
	}
	var target model.NotificationTarget
	if err := json.Unmarshal(r.input, &target); err != nil || target.ItemID != "private-item" || target.MachineID != "private-machine" || target.Kind != "machine_health" {
		t.Fatalf("%+v %v", target, err)
	}
}
