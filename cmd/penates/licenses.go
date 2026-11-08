package main

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/dpleshakov/penates/internal/licenses"
)

// newLicensesCmd builds "penates licenses", a local command like version: it
// reads no state and opens no interface.
func newLicensesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "licenses",
		Short: "Print the licenses of the code included in penates",
		Long: "Print the components included in this binary — penates itself, the Go\n" +
			"standard library, and the third-party modules — followed by the license text\n" +
			"of each. The third-party texts are embedded only in the release builds.",
		Args: rejectArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := io.WriteString(cmd.OutOrStdout(), licenses.Text())
			return err
		},
	}
}
