//go:build !windows
package cli
import (
 "context"
 "strings"
 "github.com/shirou/gopsutil/v4/process"
)
func parentCommand(ctx context.Context,p *process.Process) (string,[]string,error) {
 argv,err:=p.CmdlineSliceWithContext(ctx)
 return strings.Join(argv," "),argv,err
}
