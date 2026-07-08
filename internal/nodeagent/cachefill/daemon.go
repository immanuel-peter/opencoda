package cachefill

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
			continue
		}
		if isNydusOCIImage(img) {
			log.Printf("cachefill: nydus OCI %s registered (runtime pull requires nydus-snapshotter)", img)
		} else {
			log.Printf("cachefill: pulled %s", img)
		}
		if err := d.warmNydusPrefetch(ctx, img); err != nil {
			log.Printf("cachefill: nydus prefetch %s: %v", img, err)
			continue
		}
		log.Printf("cachefill: nydus prefetch %s ok", img)
	}
	<-ctx.Done()
	return ctx.Err()
}

func isNydusOCIImage(image string) bool {
	return strings.Contains(image, "-nydus")
}

func (d *Daemon) pullFullBlob(ctx context.Context, image string) error {
	if isNydusOCIImage(image) {
		return nil
	}
	if d.imagePresent(ctx, image) {
		return nil
	}
	if _, err := exec.LookPath("nsenter"); err == nil {
		cmd := exec.CommandContext(ctx, "nsenter", "-t", "1", "-m", "-u", "-i", "-n", "crictl", "pull", image)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	if _, err := exec.LookPath("crictl"); err == nil {
		if err := exec.CommandContext(ctx, "crictl", "pull", image).Run(); err == nil {
			return nil
		}
	}
	if _, err := exec.LookPath("ctr"); err != nil {
		return fmt.Errorf("image %s not present and no pull tool succeeded", image)
	}
	return exec.CommandContext(ctx, "ctr", "-n", "k8s.io", "images", "pull", image).Run()
}

func (d *Daemon) imagePresent(ctx context.Context, image string) bool {
	var cmd *exec.Cmd
	if _, err := exec.LookPath("nsenter"); err == nil {
		cmd = exec.CommandContext(ctx, "nsenter", "-t", "1", "-m", "-u", "-i", "-n", "crictl", "images")
	} else if _, err := exec.LookPath("crictl"); err == nil {
		cmd = exec.CommandContext(ctx, "crictl", "images")
	} else {
		return false
	}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	text := string(out)
	if strings.Contains(text, image) {
		return true
	}
	if idx := strings.LastIndex(image, "/"); idx >= 0 {
		return strings.Contains(text, image[idx+1:])
	}
	return false
}

func (d *Daemon) warmNydusPrefetch(ctx context.Context, image string) error {
	if isNydusOCIImage(image) {
		return nil
	}
	if _, err := exec.LookPath("nydus-image"); err != nil {
		return nil
	}
	ref := strings.ReplaceAll(image, "/", "_")
	cacheDir := "/var/cache/opencoda/nydus"
	_ = os.MkdirAll(cacheDir, 0755)
	bootstrap := filepath.Join(cacheDir, ref+".bootstrap")
	blob := filepath.Join(cacheDir, ref+".blob")
	return exec.CommandContext(ctx, "nydus-image", "create",
		"--bootstrap", bootstrap,
		"--blob", blob,
		image,
	).Run()
}
