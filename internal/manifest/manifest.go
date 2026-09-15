// Package manifest defines the per-project trenvy manifest: the single source
// of truth for a project's variable names, secret-ness, plain values and
// outputs. Secret values are never stored here.
package manifest

// Manifest is one project's complete configuration.
type Manifest struct {
	Project Project           `toml:"project"`
	Outputs map[string]Output `toml:"outputs"`
	Vars    map[string]Var    `toml:"vars"`

	// LegacyTargets captures a pre-profile manifest's [targets.*] block.
	// toml silently ignores unknown keys, so without capturing it an old
	// manifest would parse as a project with no outputs and no complaint.
	// Validate refuses it; nothing else reads it.
	LegacyTargets map[string]any `toml:"targets,omitempty"`
}

// Project holds project-level settings.
type Project struct {
	ID string `toml:"id"`
	// Profiles names every value set this project understands. A profile
	// selects what a variable's value is, including which keyring item a
	// secret reads, and is independent of which output it is written to.
	Profiles []string `toml:"profiles,omitempty"`
	// DefaultProfile is used when neither --profile nor TRENVY_PROFILE says
	// otherwise. It must be one of Profiles.
	DefaultProfile string `toml:"default_profile"`
	// Output names the output `render` writes when given no argument. It may
	// be empty: a project that only feeds exec and shell needs no outputs.
	Output string `toml:"output,omitempty"`
	// Salt keys the render sum so that an unkeyed digest of a low-entropy
	// secret is not left lying around in the state file. Generated at init.
	Salt string `toml:"salt"`

	// LegacyDefaultTarget captures a pre-profile manifest's default_target,
	// for the same reason as LegacyTargets.
	LegacyDefaultTarget string `toml:"default_target,omitempty"`
}

// Format values for Output.Format.
const (
	FormatDotenv = "dotenv"
	FormatJSON   = "json"
)

// Output is one named destination: a file's format, path and subset. It holds
// no values -- a profile decides those -- and it has no notion of a process,
// because process delivery is the verb: exec and shell, not a config entry.
type Output struct {
	Format  string   `toml:"format"`
	Path    string   `toml:"path"`
	Include []string `toml:"include,omitempty"`
	Exclude []string `toml:"exclude,omitempty"`
}

// Var is one variable. Value is a pointer so that an empty string (a legal
// value) is distinguishable from no value at all.
type Var struct {
	Value *string `toml:"value,omitempty"`
	// Profiles holds per-profile values, consulted before Value. It is sparse
	// on purpose: most variables do not vary, and a dense table would grow a
	// row per variable per profile and stop being readable.
	//
	// There is deliberately no per-output equivalent: an output decides which
	// variables are written and in what syntax, never what they are worth.
	Profiles map[string]string `toml:"profiles,omitempty"`
	Secret   bool              `toml:"secret,omitempty"`
	// Optional lets the variable resolve to nothing. A required variable with
	// no value under the active profile is an error that stops a render; an
	// optional one is simply absent from the resulting set.
	Optional bool `toml:"optional,omitempty"`
}
