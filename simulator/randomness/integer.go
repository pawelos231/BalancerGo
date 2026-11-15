package randomness

import (
	"encoding/binary"
	"math/big"
)

// RNG supplies uniform full-width words from an explicit stream.
// Samplers never own or select a stream.
type RNG interface {
	Uint64() uint64
}

// IntegerSampler compiles a constant or floor of a continuous uniform value.
// Its bounds are immutable, so one definition can serve separate resource streams.
type IntegerSampler struct {
	// A zero span and no fractional bounds mean one deterministic result.
	minimum int64

	// Whole bounds use a width and a precomputed rejection threshold.
	span      uint64
	threshold uint64

	// Fractional bounds use exact integer coordinates and their common denominator.
	lower *big.Int
	scale *big.Int
	width *big.Int

	wordCount     int
	firstWordMask uint64
}

// NewIntegerSampler validates and snapshots exact bounds without consuming RNG.
// Constants and intervals realizing only one integer need no random draws.
func NewIntegerSampler(profile Profile) (*IntegerSampler, error) {
	if err := ValidateProfile(profile); err != nil {
		return nil, err
	}
	if profile.Kind == KindConstant {
		value, _ := parseProfileDecimal(*profile.Value)
		wholeValue := new(big.Int).Quo(value.Num(), value.Denom())
		return &IntegerSampler{minimum: wholeValue.Int64()}, nil
	}

	minimum, _ := parseProfileDecimal(*profile.Min)
	maximum, _ := parseProfileDecimal(*profile.Max)
	first := new(big.Int).Quo(minimum.Num(), minimum.Denom()).Int64()

	// Subtract one numerator step to exclude max: 20 becomes 19; 20.5 still floors to 20.
	numeratorBeforeEnd := new(big.Int).Sub(maximum.Num(), big.NewInt(1))
	last := new(big.Int).Quo(numeratorBeforeEnd, maximum.Denom()).Int64()
	sampler := &IntegerSampler{minimum: first}
	if first == last {
		return sampler, nil
	}

	if minimum.IsInt() && maximum.IsInt() {
		sampler.span = maximum.Num().Uint64() - uint64(first)
		sampler.threshold = -sampler.span % sampler.span
		return sampler, nil
	}

	// Scale both bounds to integer coordinates before drawing an offset.
	gcd := new(big.Int).GCD(nil, nil, minimum.Denom(), maximum.Denom())
	scale := new(big.Int).Mul(minimum.Denom(), maximum.Denom())
	scale.Quo(scale, gcd)

	lower := new(big.Int).Quo(scale, minimum.Denom())
	lower.Mul(lower, minimum.Num())
	upper := new(big.Int).Quo(scale, maximum.Denom())
	upper.Mul(upper, maximum.Num())
	width := new(big.Int).Sub(upper, lower)

	// Keep only the bits needed for [0,width), rejecting candidates outside it.
	lastOffset := new(big.Int).Sub(width, big.NewInt(1))
	bits := lastOffset.BitLen()
	sampler.lower, sampler.scale, sampler.width = lower, scale, width
	sampler.wordCount = (bits + 63) / 64
	sampler.firstWordMask = ^uint64(0)
	if remainder := bits % 64; remainder != 0 {
		sampler.firstWordMask = (uint64(1) << remainder) - 1
	}
	return sampler, nil
}

// Sample floors exactly once, preserving fractional endpoint bucket weights.
// A nondeterministic sample requires a non-nil RNG. Scratch values stay local.
func (sampler *IntegerSampler) Sample(rng RNG) int64 {
	if sampler.width == nil {
		if sampler.span == 0 {
			return sampler.minimum
		}
		// Reject the short remainder so every residue has the same probability.
		for {
			value := rng.Uint64()
			if value >= sampler.threshold {
				return sampler.minimum + int64(value%sampler.span)
			}
		}
	}

	buffer := make([]byte, sampler.wordCount*8)
	var offset big.Int
	for {
		// Fixed 64-bit chunks keep draw consumption independent of machine word size.
		for word := 0; word < sampler.wordCount; word++ {
			value := rng.Uint64()
			if word == 0 {
				value &= sampler.firstWordMask
			}
			binary.BigEndian.PutUint64(buffer[word*8:], value)
		}
		offset.SetBytes(buffer)
		if offset.Cmp(sampler.width) < 0 {
			break
		}
	}
	offset.Add(&offset, sampler.lower)
	offset.Quo(&offset, sampler.scale)
	return offset.Int64()
}
