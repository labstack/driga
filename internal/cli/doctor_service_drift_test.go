package cli

import (
	"context"
	"testing"
)

func TestDoctorWarnsAboutStaleMacOSService(t *testing.T) {
	d, _ := doctorFixture(t)
	d.OS = "darwin"
	d.ServicePlistStale = func() (bool, error) { return true, nil }
	requireCheck(t, Doctor(context.Background(), d), "service_definition", "warn", "driga service install")
}
