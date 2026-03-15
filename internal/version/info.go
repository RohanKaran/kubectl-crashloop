package version

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

func Get() Info {
	return Info{
		Version: buildVersion,
		Commit:  buildCommit,
		Date:    buildDate,
	}
}
