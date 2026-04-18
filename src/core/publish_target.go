package core

// PublishTarget bitmask for publish destinations.
type PublishTarget uint8

const (
	PublishGitHubReleases PublishTarget = 1 << iota // GitHub Releases in NurOS-Packages/<pkg>
	PublishNurOSOrg                                 // NurOS-Packages org repo (file commit)
	PublishLocal                                    // Local only, no remote publish
)
