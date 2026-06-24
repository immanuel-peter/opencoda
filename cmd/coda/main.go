package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/immanuel-peter/opencoda/cli"
)

func main() {
	root := &cobra.Command{
		Use:   "coda",
		Short: "OpenCoda CLI",
	}
	root.AddCommand(cli.DeployCmd())
	root.AddCommand(cli.LogsCmd())
	root.AddCommand(cli.ScaleCmd())
	root.AddCommand(cli.TokenCmd())
	root.AddCommand(cli.ImageConvertCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
