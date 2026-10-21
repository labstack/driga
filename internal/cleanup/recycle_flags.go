package cleanup

// IFileOperation flags: explicitly recycle, preserve undo, suppress UI, and stop
// on the first error. No permanent-delete fallback is offered by driga.
func recycleFlags() uint32 {
	return 0x00080000 | 0x0040 | 0x0010 | 0x0004 | 0x0400 | 0x20000000 | 0x00100000
}
