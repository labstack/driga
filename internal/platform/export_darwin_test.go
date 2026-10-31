package platform

// The lsof fallback, for tests that fake the lsof binary.
var (
	LsofExternalPeers = lsofExternalPeers
	LsofResolvedPeers = lsofResolvedPeers
)
