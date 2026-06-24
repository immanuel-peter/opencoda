package weights

import (
	"context"
	"io"
)

// ModelRef identifies model weights.
type ModelRef struct {
	Source string
}

// WeightSource extension point (§17.5).
type WeightSource interface {
	Open(ctx context.Context, ref ModelRef) (io.ReaderAt, int64, error)
}
