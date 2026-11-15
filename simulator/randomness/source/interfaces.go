package source

// FloatSampler is prepared without consuming randomness and uses a caller-owned stream.
type FloatSampler interface{ Sample(RNG) float64 }

// IntegerSampler returns whole base units without losing large integers through float64.
type IntegerSampler interface{ Sample(RNG) int64 }
