package test

import sdd "github.com/networkteam/sdd/pkg/application"

// BlobsOnly is the staged store it wraps without that store's optional
// parts.
type BlobsOnly struct{ sdd.StagedBlobStore }
