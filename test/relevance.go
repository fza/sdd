package test

import _ "embed"

// SegmentBeforeSource is a relevance ledger segment encoded before reader
// records carried their label source: its reader flags hold the audit bit
// alone. It holds the batch of the relevance codec test's sampleBatch.
//
//go:embed fixtures/relevance/before-source.seg
var SegmentBeforeSource []byte
