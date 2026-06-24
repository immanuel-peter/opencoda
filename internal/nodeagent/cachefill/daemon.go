package cachefill

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
)

// Daemon pre-warms Nydus blobs and Spegel-seeds full blobs on buffered nodes (§16).
type Daemon struct {
	Images []string
}

func New(images []string) *Daemon {
	return &Daemon{Images: images}
}

func (d *Daemon) Run(ctx context.Context) error {
	if len(d.Images) == 0 {
		d.Images = []string{"ghcr.io/immanuel-peter/opencoda-fakevllm:latest"}
	}
	for _, img := range d.Images {
		if err := d.pullFullBlob(ctx, img); err != nil {
			log.Printf("cachefill: pull %s: %v", img, err)
		}
		if err := d.warmNydusPrefetch(ctx, img); err != nil {
			log.Printf("cachefill: nydus prefetch %s: %v", img, err)
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

func (d *Daemon) pullFullBlob(ctx context.Context, image string) error {
	if _, err := exec.LookPath("ctr"); err != nil {
		return err
	}
	return exec.CommandContext(ctx, "ctr", "-n", "k8s.io", "images", "pull", image).Run()
}

func (d *Daemon) warmNydusPrefetch(ctx context.Context, image string) error {
	if _, err := exec.LookPath("nydus-image"); err != nil {
		return nil
	}
	ref := strings.ReplaceAll(image, "/", "_")
	cacheDir := "/var/cache/opencoda/nydus"
	_ = os.MkdirAll(cacheDir, 0755)
	return exec.CommandContext(ctx, "nydus-image", "create", "--bootstrap", cacheDir+"/"+ref+".bootstrap", image).Run()
}
