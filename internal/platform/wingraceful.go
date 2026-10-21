package platform

type gracefulStep uint8

const (
	closeWindows gracefulStep = iota
	consoleBreak
)

// A PID is not a console group ID. CTRL_BREAK requires proof of a driga-created
// group, a shared console and membership confined to driga's owned job.
func planWindowsGraceful(ownedGroup, sharedConsole, closedMembership bool) gracefulStep {
	if ownedGroup && sharedConsole && closedMembership {
		return consoleBreak
	}
	return closeWindows
}
