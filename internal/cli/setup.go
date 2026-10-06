package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"

	"github.com/cdescobedo/trenvy/internal/config"
	"github.com/cdescobedo/trenvy/internal/guard"
	"github.com/cdescobedo/trenvy/internal/manage"
	"github.com/cdescobedo/trenvy/internal/manifest"
	"github.com/cdescobedo/trenvy/internal/resolve"
	"github.com/cdescobedo/trenvy/internal/sample"
	"github.com/cdescobedo/trenvy/internal/style"
)

// runSetup is `trenvy ui` / `trenvy edit` outside a registered project: it
// gathers the facts setup shows (setup itself reads nothing), runs it, and
// only after its summary's enter writes the project, in project init's order,
// then lands on Variables. Cancelling writes nothing.
func runSetup(opts *Options) error {
	th, err := style.ThemeFor(lipgloss.NewRenderer(opts.Stdout), os.Getenv("TRENVY_THEME"))
	if err != nil {
		return err
	}
	reg, err := config.LoadRegistry()
	if err != nil {
		return err
	}
	var ids, roots []string
	for _, e := range reg.Projects {
		ids = append(ids, e.ID)
		roots = append(roots, e.Root)
	}

	root := resolve.ProjectRootGuess(opts.Dir)
	// Outside a repository there is no worktree: the directory stands in, so
	// detection still looks where the user is.
	wt, err := resolve.WorktreeRoot(opts.Dir)
	if err != nil {
		wt = opts.Dir
	}
	samples := detectSamples(wt)

	final, err := opts.RunSetup(manage.NewSetup(manage.SetupInput{
		Mode: manage.SetupNew, ID: filepath.Base(root), Root: root, Dir: opts.Dir,
		TakenIDs: ids, TakenRoots: roots, ManifestPath: config.ManifestPath, Samples: samples,
	}, th))
	if err != nil {
		return err
	}
	res, ok := final.Result()
	if !ok {
		fmt.Fprintln(opts.Stderr, "setup cancelled -- nothing was written")
		return nil
	}

	m, err := newProjectManifest(res.ID)
	if err != nil {
		return err
	}
	if res.Sample >= 0 {
		if _, err := importInto(m, wt, filepath.Join(wt, samples[res.Sample].Path), res.Entries, "", false, false); err != nil {
			return err
		}
	}
	if err := registerProject(res.ID, res.Root, m); err != nil {
		return err
	}
	app, err := opts.resolveApp()
	if err != nil {
		return err
	}
	return runManage(opts, app, func(mm manage.Model) manage.Model { return mm.AfterSetup(res.ID) })
}

// detectSamples lists the samples in the worktree root that setup can offer:
// each parses, holds variables, and previews an output. One that does not is
// left out rather than shown broken, so a stray file never blocks setup.
func detectSamples(wt string) []manage.SetupSample {
	names, err := sample.Detect(wt)
	if err != nil {
		return nil
	}
	var out []manage.SetupSample
	for _, name := range names {
		path := filepath.Join(wt, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entries, err := sample.Parse(name, data)
		if err != nil || len(entries) == 0 {
			continue
		}
		// force: the preview is of an empty manifest, so there is nothing to
		// conflict with, and it must not fail on a name it will overwrite.
		scratch := &manifest.Manifest{Vars: map[string]manifest.Var{}, Outputs: map[string]manifest.Output{}}
		rep, err := importInto(scratch, wt, path, entries, "", false, true)
		if err != nil {
			continue
		}
		outPath := scratch.Outputs[rep.Output].Path
		ignored, _ := guard.GitIgnored(wt, outPath)
		out = append(out, manage.SetupSample{
			Path: name, Entries: entries, OutputName: rep.Output, OutputPath: outPath, Ignored: ignored,
		})
	}
	return out
}
