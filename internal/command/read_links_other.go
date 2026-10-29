//go:build !windows

package command

import (
	"os"
	"reflect"
)

func singleReadLink(_ *os.File, st os.FileInfo) bool {
	v := reflect.Indirect(reflect.ValueOf(st.Sys()))
	if v.IsValid() && v.Kind() == reflect.Struct {
		n := v.FieldByName("Nlink")
		if n.IsValid() && n.CanUint() && n.Uint() > 1 {
			return false
		}
	}
	return true
}
