package capacity

import "errors"

// ErrICE indicates insufficient capacity from the cloud provider.
var ErrICE = errors.New("insufficient instance capacity")

// IsICE returns true if err is an ICE-class failure.
func IsICE(err error) bool {
	return errors.Is(err, ErrICE)
}
