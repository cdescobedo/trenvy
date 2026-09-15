package resolve

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cdescobedo/envctl/internal/manifest"
)

// ProfileEnvVar names the profile for one shell session. `envctl profile use`
// emits an export of it.
const ProfileEnvVar = "ENVCTL_PROFILE"

// ErrUnknownProfile is returned when a named profile is not declared.
var ErrUnknownProfile = errors.New("unknown profile")

// ActiveProfile is the profile in force and a human-readable account of why.
// Source exists because "which profile am I on" and "why" are one question:
// a stale exported ENVCTL_PROFILE is invisible until something names it.
type ActiveProfile struct {
	Name   string
	Source string
}

// Profile picks the active profile: the --profile flag, then ENVCTL_PROFILE,
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
		p = ActiveProfile{Name: m.Project.DefaultProfile, Source: "default_profile in the manifest"}
	}

	for _, declared := range m.Project.Profiles {
		if declared == p.Name {
			return p, nil
		}
	}
	return ActiveProfile{}, fmt.Errorf("%w %q (from %s); declared profiles: %s",
		ErrUnknownProfile, p.Name, p.Source, strings.Join(m.Project.Profiles, ", "))
}
