// Package version exposes build metadata embedded at link time.
package version

// Info contains build metadata for the current binary.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

// Get returns the current build metadata.
func Get() Info {
	return Info{
		Version: buildVersion,
		Commit:  buildCommit,
		Date:    buildDate,
	}
}
