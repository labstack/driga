package tui

import "github.com/labstack/driga/internal/viewer"

// Source and Snapshot are shared with the browser viewer.
type Source = viewer.Source
type Snapshot = viewer.Snapshot
type StoreSource = viewer.StoreSource
type LiveSnapshotSource = viewer.LiveSnapshotSource

var NewStoreSource = viewer.NewStoreSource
var NewLiveSnapshotSource = viewer.NewLiveSnapshotSource
