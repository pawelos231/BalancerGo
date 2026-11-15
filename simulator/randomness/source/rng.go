// Package source describes caller-owned randomness used by distribution samplers.
package source

// RNG supplies uniform full-width words from an explicit stream.
// Samplers never own or select a stream.
type RNG interface {
	Uint64() uint64
}
