package destination

import (
	"context"

	"git.kuhree.com/kuhree.com/platform.djpi/internal/source"
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

// StagedDelivery receives the one shared Source stream, then independently
// commits and verifies those bytes after their content digest is known.
type StagedDelivery interface {
	Write(context.Context, []byte) (int, error)
	Commit(context.Context, string) (Evidence, error)
	Abort() error
}

// FanoutDestination can participate in the engine's single-read fan-out.
type FanoutDestination interface {
	Destination
	Stage(context.Context, string, string, source.ObservedItem) (StagedDelivery, error)
}
