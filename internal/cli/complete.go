package cli

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/store"
)

// Predicates selecting which of a manifest's variables a command can act on.
func isSecret(v manifest.Var) bool { return v.Secret }
func isPlain(v manifest.Var) bool  { return !v.Secret }
func anyVar(manifest.Var) bool     { return true }

// completeVarNames builds the ValidArgsFunction for a command taking a single
// <NAME>, offering the manifest variables that satisfy keep.
//
// Completion runs on a keystroke and has nowhere to report to: anything it
// writes lands in the middle of the line being typed, and anything slow is
// felt directly. So every failure here -- outside a project, an unreadable
// manifest, a backend that will not open -- degrades to fewer suggestions,
// never to an error and never to a wait.
func (o *Options) completeVarNames(keep func(manifest.Var) bool) func(
	*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {

	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Every command wired to this takes exactly one name. Past it there is
		// nothing to offer, and NoFileComp is what stops the shell from
		// filling `set NAME <TAB>` with the current directory's filenames.
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		app, err := o.resolveApp()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		names := make([]string, 0, len(app.Manifest.Vars))
		secrets := false
		for name, v := range app.Manifest.Vars {
			if !keep(v) || !strings.HasPrefix(name, toComplete) {
				continue
			}
			names = append(names, name)
			secrets = secrets || v.Secret
		}
		sort.Strings(names)

		// Only a secret has a presence to report, so a plain-only completion
		// never opens a store -- the same bargain resolveApp makes.
		probe := noPresence
		if secrets {
			probe = app.cheapPresenceProbe()
		}

		out := make([]string, 0, len(names))
		for _, name := range names {
			out = append(out, name+"\t"+describeVar(app.Manifest.Vars[name], probe(name)))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// describeVar is the tag shown beside a candidate. It reports kind and, when
// known, whether the keyring holds a value -- never the value itself, and
// never a plain value either: a variable marked plain is not thereby public.
func describeVar(v manifest.Var, presence string) string {
	if !v.Secret {
		return "plain"
	}
	if presence == "" {
		return "secret"
	}
	return "secret, " + presence
}

// noPresence is the probe used when presence cannot be established cheaply.
func noPresence(string) string { return "" }

// cheapPresenceProbe reports "stored" or "MISSING" per secret, but only on a
// backend that implements store.CheapPresence. On any other -- the file
// backend, whose Has decrypts, or the system keyring, whose Has reads and may
// prompt -- it reports nothing, so the tag is lost rather than TAB stalling
// or raising a dialog. A backend that will not open at all is the same case.
func (a *appContext) cheapPresenceProbe() func(string) string {
	s, err := a.store()
	if err != nil {
		return noPresence
	}
	cheap, ok := s.(store.CheapPresence)
	if !ok {
		return noPresence
	}
	return func(name string) string {
		present, err := cheap.Has(a.service(name))
		if err != nil {
			return ""
		}
		if present {
			return "stored"
		}
		return "MISSING"
	}
}

// completeProfileNames offers the manifest's declared profiles. Like every
// completion here it reads the manifest and nothing else: no store, no
// network, no prompt.
func (o *Options) completeProfileNames() func(
	*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {

	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		app, err := o.resolveApp()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for _, p := range app.Manifest.Project.Profiles {
			if !strings.HasPrefix(p, toComplete) {
				continue
			}
			tag := "profile"
			if p == app.Manifest.Project.DefaultProfile {
				tag = "profile, default"
			}
			out = append(out, p+"\t"+tag)
		}
		sort.Strings(out)
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeOutputNames offers the manifest's defined outputs, tagged with the
// path each one writes so a choice can be made without opening the manifest.
func (o *Options) completeOutputNames() func(
	*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {

	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		app, err := o.resolveApp()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for name, output := range app.Manifest.Outputs {
			if !strings.HasPrefix(name, toComplete) {
				continue
			}
			out = append(out, name+"\t"+output.Format+" -> "+output.Path)
		}
		sort.Strings(out)
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeProjectIDs offers registered ids. It reads the registry rather than
// resolveApp, because project move and rm work on a project named by id from
// anywhere on disk, including from outside every registered root.
func (o *Options) completeProjectIDs() func(
	*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {

	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		reg, err := config.LoadRegistry()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for _, e := range reg.Projects {
			if strings.HasPrefix(e.ID, toComplete) {
				out = append(out, e.ID+"\t"+e.Root)
			}
		}
		sort.Strings(out)
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}
