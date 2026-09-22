package resolve

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cdescobedo/trenvy/internal/manifest"
)

// ProfileEnvVar names the profile for one shell session. `trenvy profile use`
// emits an export of it.
const ProfileEnvVar = "TRENVY_PROFILE"

// ErrUnknownProfile is returned when a named profile is not declared.
var ErrUnknownProfile = errors.New("unknown profile")

// ActiveProfile is the profile in force and a human-readable account of why.
// Source exists because "which profile am I on" and "why" are one question:
// a stale exported TRENVY_PROFILE is invisible until something names it.
type ActiveProfile struct {
	Name   string
	Source string
}

// Profile picks the active profile: the --profile flag, then TRENVY_PROFILE,
// then the active selection recorded in this machine's state file, then the
// manifest's default_profile. A name from any source must be declared, and
// the error says which source supplied the bad name.
//
// active is passed in, not read here: the caller loads the state file, the
// same way it already reads TRENVY_PROFILE from the environment before
// calling in. Keeping this function pure is what makes the whole chain
// testable as a table with no filesystem.
func Profile(m *manifest.Manifest, flag, env, active string) (ActiveProfile, error) {
	var p ActiveProfile
	switch {
	case flag != "":
		p = ActiveProfile{Name: flag, Source: "--profile"}
	case env != "":
		p = ActiveProfile{Name: env, Source: ProfileEnvVar}
	case active != "":
		p = ActiveProfile{Name: active, Source: "active"}
	default:
		p = ActiveProfile{Name: m.Project.DefaultProfile, Source: "default"}
	}

	for _, declared := range m.Project.Profiles {
		if declared == p.Name {
			return p, nil
		}
	}
	err := fmt.Errorf("%w %q (from %s); declared profiles: %s",
		ErrUnknownProfile, p.Name, p.Source, strings.Join(m.Project.Profiles, ", "))
	switch p.Source {
	case ProfileEnvVar:
		// Every command, including `profile use` -- the one that would fix
		// this -- validates the env var before its body runs, so a stale
		// export is a dead end unless the error itself names the escape.
		err = fmt.Errorf("%w; escape it with: unset %s", err, ProfileEnvVar)
	case "active":
		// `profile rm` does not clear the active selection, so removing the
		// profile you're on leaves the state file naming something the
		// manifest no longer declares. Every command reads that file before
		// its body runs, so without a named escape here this is a dead end
		// the same way a stale TRENVY_PROFILE is.
		err = fmt.Errorf("%w; escape it with: trenvy profile use --clear", err)
	}
	return ActiveProfile{}, err
}
