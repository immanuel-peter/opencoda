package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// ConvertImage wraps nydusify convert (FR-4).
func ConvertImage(cmd *cobra.Command, args []string) error {
	source, _ := cmd.Flags().GetString("source")
	target, _ := cmd.Flags().GetString("target")
	if source == "" || target == "" {
		return fmt.Errorf("--source and --target required")
	}
	nydusify, err := exec.LookPath("nydusify")
	if err != nil {
		return fmt.Errorf("nydusify not found in PATH: install from dragonflyoss/nydus releases")
	}
	c := exec.Command(nydusify, "convert", "--source", source, "--target", target)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func init() {
	// flags attached when command is built
}
