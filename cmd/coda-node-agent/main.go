package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/immanuel-peter/opencoda/internal/nodeagent/cachefill"
)

func main() {
	var images string
	flag.StringVar(&images, "images", "", "comma-separated images to cachefill")
	flag.Parse()

	imgList := []string{}
	if images != "" {
		imgList = strings.Split(images, ",")
	}
	daemon := cachefill.New(imgList)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	log.Println("coda-node-agent starting cachefill daemon")
	if err := daemon.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
