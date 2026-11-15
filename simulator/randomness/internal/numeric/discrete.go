package numeric

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"sort"

	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

// ExactChoice samples rational decimal weights as integer coordinates, without
// rounding probabilities through binary64. Zero-weight entries remain absent.
type ExactChoice struct {
	cumulative []*big.Int
	total      *big.Int
	words      int
	mask       uint64
}

// NewChoice scales rational decimal weights to exact integer coordinates.
func NewChoice(weights []string) (*ExactChoice, error) {
	if len(weights) == 0 {
		return nil, fmt.Errorf("randomness: at least one weight is required")
	}

	rationals := make([]*big.Rat, len(weights))
	scale := big.NewInt(1)
	for i, text := range weights {
		value, err := profile.ParseDecimal(text)
		if err != nil {
			return nil, err
		}
		rationals[i] = value
		gcd := new(big.Int).GCD(nil, nil, scale, value.Denom())
		scale.Quo(scale, gcd)
		scale.Mul(scale, value.Denom())
	}

	choice := &ExactChoice{total: new(big.Int)}
	for _, weight := range rationals {
		coordinate := new(big.Int).Quo(new(big.Int).Set(scale), weight.Denom())
		coordinate.Mul(coordinate, weight.Num())
		choice.total.Add(choice.total, coordinate)
		choice.cumulative = append(choice.cumulative, new(big.Int).Set(choice.total))
	}
	if choice.total.Sign() <= 0 {
		return nil, fmt.Errorf("randomness: weights must have a positive sum")
	}

	last := new(big.Int).Sub(choice.total, big.NewInt(1))
	bits := last.BitLen()
	choice.words = (bits + 63) / 64
	choice.mask = ^uint64(0)
	if remainder := bits % 64; remainder != 0 {
		choice.mask = (uint64(1) << remainder) - 1
	}
	return choice, nil
}

// Index selects an input entry with its exact rational weight.
func (choice *ExactChoice) Index(rng source.RNG) int {
	if choice.words == 0 {
		return sort.Search(len(choice.cumulative), func(i int) bool {
			return choice.cumulative[i].Sign() > 0
		})
	}
	buffer := make([]byte, choice.words*8)
	var coordinate big.Int
	// Masked rejection accepts at least half the candidate coordinates, unlike
	// rejection from a truncated unbounded distribution. It preserves exact mass.
	for {
		for word := 0; word < choice.words; word++ {
			value := rng.Uint64()
			if word == 0 {
				value &= choice.mask
			}
			binary.BigEndian.PutUint64(buffer[word*8:], value)
		}
		coordinate.SetBytes(buffer)
		if coordinate.Cmp(choice.total) < 0 {
			break
		}
	}
	return sort.Search(len(choice.cumulative), func(i int) bool {
		return choice.cumulative[i].Cmp(&coordinate) > 0
	})
}

type integerChoice struct {
	choice *ExactChoice
	values []int64
	fixed  bool
}

func (sampler *integerChoice) Sample(rng source.RNG) int64 {
	if sampler.fixed {
		return sampler.values[0]
	}
	return sampler.values[sampler.choice.Index(rng)]
}

type floatChoice struct {
	choice *ExactChoice
	values []float64
	fixed  bool
}

func (sampler *floatChoice) Sample(rng source.RNG) float64 {
	if sampler.fixed {
		return sampler.values[0]
	}
	return sampler.values[sampler.choice.Index(rng)]
}

// WeightedInteger preserves exact decimal floor values and rational weights.
func WeightedInteger(values, weights []string) (source.IntegerSampler, error) {
	if len(values) != len(weights) {
		return nil, fmt.Errorf("randomness: values and weights lengths differ")
	}
	converted := make([]int64, 0, len(values))
	active := make([]string, 0, len(weights))
	for i, text := range values {
		value, err := profile.ParseDecimal(text)
		if err != nil {
			return nil, err
		}
		weight, err := profile.ParseDecimal(weights[i])
		if err != nil {
			return nil, err
		}
		if weight.Sign() == 0 {
			continue
		}
		whole := new(big.Int).Quo(value.Num(), value.Denom())
		if !whole.IsInt64() {
			return nil, fmt.Errorf("randomness: integer value exceeds int64")
		}
		converted = append(converted, whole.Int64())
		active = append(active, weights[i])
	}
	choice, err := NewChoice(active)
	if err != nil {
		return nil, err
	}
	fixed := true
	for _, value := range converted {
		if value != converted[0] {
			fixed = false
			break
		}
	}
	return &integerChoice{choice: choice, values: converted, fixed: fixed}, nil
}

// WeightedFloat requires exact floating outcomes while preserving rational weights.
func WeightedFloat(values, weights []string) (source.FloatSampler, error) {
	if len(values) != len(weights) {
		return nil, fmt.Errorf("randomness: values and weights lengths differ")
	}
	converted := make([]float64, 0, len(values))
	active := make([]string, 0, len(weights))
	for i, text := range values {
		value, err := profile.ParseDecimal(text)
		if err != nil {
			return nil, err
		}
		weight, err := profile.ParseDecimal(weights[i])
		if err != nil {
			return nil, err
		}
		if weight.Sign() == 0 {
			continue
		}
		floating, exact := value.Float64()
		if !exact {
			return nil, fmt.Errorf("randomness: discrete value %s is not exactly representable as float64; use the integer sampler", text)
		}
		converted = append(converted, floating)
		active = append(active, weights[i])
	}
	choice, err := NewChoice(active)
	if err != nil {
		return nil, err
	}
	fixed := true
	for _, value := range converted {
		if value != converted[0] {
			fixed = false
			break
		}
	}
	return &floatChoice{choice: choice, values: converted, fixed: fixed}, nil
}
