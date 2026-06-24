package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	opencodav1alpha1 "github.com/immanuel-peter/opencoda/api/v1alpha1"
	"github.com/immanuel-peter/opencoda/internal/gateway"
)

func TokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage CodaToken credentials",
	}
	cmd.AddCommand(tokenNewCmd())
	return cmd
}

func tokenNewCmd() *cobra.Command {
	var namespace string
	var tokenID string
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a CodaToken and print one-time id:secret pair",
		RunE: func(cmd *cobra.Command, args []string) error {
			if tokenID == "" {
				tokenID = fmt.Sprintf("token-%d", os.Getpid())
			}
			scheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(scheme))
			utilruntime.Must(opencodav1alpha1.AddToScheme(scheme))
			cfg, err := config.GetConfig()
			if err != nil {
				return err
			}
			c, err := client.New(cfg, client.Options{Scheme: scheme})
			if err != nil {
				return err
			}
			pair, err := gateway.CreateToken(context.Background(), c, namespace, tokenID)
			if err != nil {
				return err
			}
			fmt.Println(pair)
			return nil
		},
	}
	cmd.Flags().StringVar(&namespace, "namespace", "default", "namespace for CodaToken")
	cmd.Flags().StringVar(&tokenID, "id", "", "token id")
	return cmd
}
