package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/cdescobedo/envctl/internal/config"
	"github.com/cdescobedo/envctl/internal/fsutil"
	"github.com/cdescobedo/envctl/internal/guard"
	"github.com/cdescobedo/envctl/internal/manifest"
	"github.com/cdescobedo/envctl/internal/render"
	"github.com/cdescobedo/envctl/internal/resolve"
)

func newRenderCmd(opts *Options) *cobra.Command {
	var force, allowTracked bool

	c := &cobra.Command{
		Use:               "render [output]",
		Short:             "Write an output's file into this worktree",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: opts.completeOutputNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name, err := app.outputOrDefault(cmd, firstOrEmpty(args))
			if err != nil {
				return err
			}
			// No kind check: every output is a file. Process delivery is the
			// verb -- exec and shell -- not a configurable destination.
			o := app.Manifest.Outputs[name]

			values, err := resolve.Values(app.Manifest, app.Profile.Name, name, app.secretLookup())
			if err != nil {
				return err
			}

			var data []byte
			switch o.Format {
			case manifest.FormatDotenv:
				data, err = render.Dotenv(values)
			case manifest.FormatJSON:
				data, err = render.JSON(values)
			default:
				err = fmt.Errorf("output %q: unsupported format %q", name, o.Format)
			}
			if err != nil {
				return err
			}

			abs := filepath.Join(app.Worktree, o.Path)

			if !allowTracked {
				ignored, err := guard.GitIgnored(app.Worktree, o.Path)
				if err != nil {
					return err
				}
				if !ignored {
					return fmt.Errorf(
						"%s is not ignored by git\n  add to .gitignore:\n    %s\n"+
							"  or re-run with --allow-tracked",
						o.Path, o.Path)
				}
			}

			wantSum := render.Sum(app.Manifest.Project.Salt, values)
			entry, have := app.State.Find(app.Worktree, name)
			status, err := guard.Inspect(abs, wantSum, entry, have)
			if err != nil {
				return err
			}
			if err := guard.CheckWritable(status, abs, "overwrite", force); err != nil {
				return err
			}

			if err := fsutil.WriteAtomic(abs, data); err != nil {
				return err
			}
			app.State.Record(config.RenderEntry{
				Worktree: app.Worktree,
				Output:   name,
				Path:     o.Path,
				Sum:      wantSum,
				File:     render.FileSum(data),
				At:       time.Now().UTC(),
			})
			if err := app.State.Save(); err != nil {
				return err
			}

			secrets := 0
			for _, n := range values.Names {
				if app.Manifest.Vars[n].Secret {
					secrets++
				}
			}
			fmt.Fprintf(opts.Stdout, "wrote %s  (%d vars, %d from the keyring, 0600)\n",
				o.Path, len(values.Names), secrets)
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "overwrite a file envctl did not write, or one that was edited")
	c.Flags().BoolVar(&allowTracked, "allow-tracked", false, "write even if git would track the path")
	return c
}

func newCleanCmd(opts *Options) *cobra.Command {
	var force bool

	c := &cobra.Command{
		Use:               "clean [output]",
		Short:             "Remove a rendered file from this worktree",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: opts.completeOutputNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := opts.resolveApp()
			if err != nil {
				return err
			}
			name, err := app.outputOrDefault(cmd, firstOrEmpty(args))
			if err != nil {
				return err
			}
			o := app.Manifest.Outputs[name]

			abs := filepath.Join(app.Worktree, o.Path)

			// The same guard render uses: a FOREIGN or MODIFIED file must not
			// be deleted without --force, or clean would silently destroy a
			// hand-written file the guard exists to protect. There is no
			// "wanted sum" to compare here — clean only cares whether the
			// file is safe to touch at all, not whether it is stale — so an
			// empty sum is passed; a genuinely rendered-but-stale file is
			// still Stale (writable), never Foreign or Modified because of
			// this.
			entry, have := app.State.Find(app.Worktree, name)
			status, err := guard.Inspect(abs, "", entry, have)
			if err != nil {
				return err
			}
			if err := guard.CheckWritable(status, abs, "delete", force); err != nil {
				return err
			}

			err = os.Remove(abs)
			switch {
			case err == nil:
				fmt.Fprintf(opts.Stdout, "removed %s\n", o.Path)
			case errors.Is(err, fs.ErrNotExist):
				fmt.Fprintf(opts.Stdout, "no %s here\n", o.Path)
			default:
				return fmt.Errorf("remove %s: %w", abs, err)
			}

			app.State.Forget(app.Worktree, name)
			return app.State.Save()
		},
	}
	c.Flags().BoolVar(&force, "force", false, "remove even a file envctl did not write, or one that was edited")
	return c
}

func firstOrEmpty(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}
