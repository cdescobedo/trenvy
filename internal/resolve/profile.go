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
// then the manifest's default_profile. A name from any source must be
// declared, and the error says which source supplied the bad name.
func Profile(m *manifest.Manifest, flag, env string) (ActiveProfile, error) {
	var p ActiveProfile
	switch {
	case flag != "":
		p = ActiveProfile{Name: flag, Source: "--profile"}
	case env != "":
		p = ActiveProfile{Name: env, Source: ProfileEnvVar}
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
	if p.Source == ProfileEnvVar {
		// Every command, including `profile use` -- the one that would fix
		// this -- validates the env var before its body runs, so a stale
		// export is a dead end unless the error itself names the escape.
		err = fmt.Errorf("%w; escape it with: unset %s", err, ProfileEnvVar)
	}
	return ActiveProfile{}, err
}
