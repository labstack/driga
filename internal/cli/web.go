package cli

import "errors"

// WebCmd is the retired browser viewer; its pages now live in the desktop app.
type WebCmd struct {
	WebOptions
	Listen string `default:"127.0.0.1:0" help:"Loopback address and port for the dashboard."`
	Open   bool   `help:"Open the dashboard in your browser."`
}

// Run refuses before anything listens: the pages have no authentication of
// their own and must not be served over TCP.
func (c WebCmd) Run(*Globals) error {
	return errors.New("driga web was replaced by the desktop app; run driga app")
}
