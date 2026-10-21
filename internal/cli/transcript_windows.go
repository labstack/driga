package cli

import "os"

func openTranscript(p string) (*os.File, error) { return os.Open(p) }
