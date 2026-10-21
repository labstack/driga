package collector

import (
	"context"
	"os"
	"path/filepath"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func stateSpool(d Deps) bool {
	return os.Getenv("DRIGA_SPOOL_DIR") == "" && filepath.Clean(d.SpoolDir) == filepath.Clean(config.SpoolDir())
}

func spoolOffsetPrefix(d Deps) string {
	if stateSpool(d) {
		return "spool/state/"
	}
	return "spool/"
}

// MigrateSpool runs only for the platform default. Explicit spool overrides
// retain their location and offset namespace. Call while holding the writer lock.
func MigrateSpool(ctx context.Context, d Deps, st *store.Store) error {
	if !stateSpool(d) {
		return nil
	}
	return spool.Migrate(config.LegacySpoolDir(), d.SpoolDir,
		func(name string) (model.Offset, error) { o, _, err := st.GetOffset("spool/" + name); return o, err },
		func(name string, o model.Offset) error {
			return st.Tx(ctx, func(tx *store.Tx) error { return tx.SetOffset(spoolOffsetPrefix(d)+name, o) })
		})
}
