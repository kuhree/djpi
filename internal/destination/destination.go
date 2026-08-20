package destination

import (
	"context"

	"git.kuhree.com/kuhree.com/djpi/internal/source"
)

type Evidence struct {
	SizeBytes          int64
	ContentDigest      string
	VerificationDigest string
	Location           string
}

// Destination preserves source items and independently verifies committed bytes.
type Destination interface {
	Name() string
	Deliver(context.Context, string, string, source.ObservedItem) (Evidence, error)
	Verify(context.Context, Evidence) (bool, error)
}
