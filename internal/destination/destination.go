package destination

import (
	"context"

	"git.kuhree.com/kuhree.com/djpi/internal/source"
)

type Evidence struct {
	SizeBytes     int64
	ContentDigest string
}

// Destination preserves source items and independently verifies committed bytes.
type Destination interface {
	Name() string
	Deliver(context.Context, string, source.ObservedItem) (Evidence, error)
	Verify(context.Context, source.ObservedItem, Evidence) (bool, error)
}
