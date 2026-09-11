// Package manifest defines the per-project envctl manifest: the single source
// of truth for a project's variable names, secret-ness, plain values and
// output targets. Secret values are never stored here.
package manifest

// Manifest is one project's complete configuration.
type Manifest struct {
	Project Project           `toml:"project"`
	Targets map[string]Target `toml:"targets"`
	Vars    map[string]Var    `toml:"vars"`
}

// Project holds project-level settings.
type Project struct {
	ID            string `toml:"id"`
	DefaultTarget string `toml:"default_target"`
	// Salt keys the render sum so that an unkeyed digest of a low-entropy
	// secret is not left lying around in the state file. Generated at init.
	Salt string `toml:"salt"`
}

// Dest values for Target.Dest.
const (
	DestFile    = "file"
	DestProcess = "process"
)

// Format values for Target.Format.
const (
	FormatDotenv = "dotenv"
	FormatJSON   = "json"
)

// Target is one named output: where values go, in what syntax, and which ones.
type Target struct {
	Dest    string   `toml:"dest"`
	Format  string   `toml:"format,omitempty"`
	Path    string   `toml:"path,omitempty"`
	Include []string `toml:"include,omitempty"`
	Exclude []string `toml:"exclude,omitempty"`
}

// Var is one variable. Value is a pointer so that an empty string (a legal
// value) is distinguishable from no value at all.
type Var struct {
	Value    *string           `toml:"value,omitempty"`
	Targets  map[string]string `toml:"targets,omitempty"`
	Secret   bool              `toml:"secret,omitempty"`
	Optional bool              `toml:"optional,omitempty"`
}
