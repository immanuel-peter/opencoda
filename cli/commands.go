package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func DeployCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a CodaEndpoint or CodaFunction",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("coda deploy: apply CodaEndpoint CR via gateway (stub)")
			return nil
		},
	}
}

func LogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs",
		Short: "Stream endpoint logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("coda logs: stream from Loki via gateway (stub)")
			return nil
		},
	}
}

func ScaleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scale",
		Short: "Scale an endpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("coda scale: patch scaling annotation (stub)")
			return nil
		},
	}
}

func ImageConvertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "image convert",
		Short: "Convert OCI image to Nydus format",
		RunE:  ConvertImage,
	}
	cmd.Flags().String("source", "", "source image reference")
	cmd.Flags().String("target", "", "target Nydus image reference")
	return cmd
}
