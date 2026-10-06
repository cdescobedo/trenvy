package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newEditCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the interactive interface (the same as trenvy ui)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := opts.resolveApp()
			if errors.Is(err, ErrNotRegistered) {
				return runSetup(opts)
			}
			if err != nil {
				return err
			}
			return runManage(opts, app)
		},
	}
}
