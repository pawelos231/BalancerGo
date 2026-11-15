// Package randomness defines explicit stochastic profiles independently of scenario fields.
package randomness

import (
	"fmt"
	"math"
	"math/big"
	"strings"
)

// Kind identifies the supported profile definition; future families are rejected.
type Kind string

const (
	KindConstant Kind = "constant"
	KindUniform  Kind = "uniform"
)

// Unit is the canonical base unit of every quantity in a profile.
type Unit string

const (
	UnitNanoseconds   Unit = "nanoseconds"
	UnitBytes         Unit = "bytes"
	UnitBitsPerSecond Unit = "bitsPerSecond"
	UnitNumber        Unit = "number"
	UnitPercentage    Unit = "percentage"
)

// Profile describes a constant or the exact half-open uniform interval [min, max).
// Decimal strings preserve fractional bounds and integers beyond JavaScript's safe range.
// Presence pointers distinguish missing parameters from an explicitly supplied zero.
// Use DecodeProfile and EncodeProfile for strict JSON handling.
type Profile struct {
	Kind  Kind    `json:"kind"`
	Unit  Unit    `json:"unit"`
	Value *string `json:"value,omitempty"`
	Min   *string `json:"min,omitempty"`
	Max   *string `json:"max,omitempty"`
}

// ValidateProfile checks the definition without sampling or selecting a target field.
// Target conversion, including whole-unit realization, is a separate operation.
func ValidateProfile(profile Profile) error {
	if profile.Kind != KindConstant && profile.Kind != KindUniform {
		return fmt.Errorf("profile.kind: unsupported profile kind %q", profile.Kind)
	}
	switch profile.Unit {
	case UnitNanoseconds, UnitBytes, UnitBitsPerSecond, UnitNumber, UnitPercentage:
	default:
		return fmt.Errorf("profile.unit: unsupported profile unit %q", profile.Unit)
	}

	if profile.Kind == KindConstant {
		if profile.Min != nil || profile.Max != nil {
			return fmt.Errorf("profile: constant accepts only value, not min or max")
		}
		_, err := validateProfileQuantity(profile.Unit, "value", profile.Value)
		return err
	}
	if profile.Value != nil {
		return fmt.Errorf("profile.value: uniform accepts only min and max")
	}
	minimum, err := validateProfileQuantity(profile.Unit, "min", profile.Min)
	if err != nil {
		return err
	}
	maximum, err := validateProfileQuantity(profile.Unit, "max", profile.Max)
	if err != nil {
		return err
	}
	if minimum.Cmp(maximum) >= 0 {
		return fmt.Errorf("profile.max: uniform min must be less than max; use a constant for a fixed value")
	}
	return nil
}

func validateProfileQuantity(unit Unit, field string, text *string) (*big.Rat, error) {
	if text == nil {
		return nil, fmt.Errorf("profile.%s: required decimal string", field)
	}
	value, err := parseProfileDecimal(*text)
	if err != nil {
		return nil, fmt.Errorf("profile.%s: %w", field, err)
	}
	limit := new(big.Rat).SetInt64(math.MaxInt64)
	if unit == UnitPercentage {
		limit.SetInt64(100)
	}
	if value.Cmp(limit) > 0 {
		return nil, fmt.Errorf("profile.%s: must be between 0 and %s in %s", field, limit.RatString(), unit)
	}
	return value, nil
}

// parseProfileDecimal keeps all arithmetic exact, including fractions near int64 limits.
// The canonical spelling also bounds the size of later sampler arithmetic.
func parseProfileDecimal(text string) (*big.Rat, error) {
	if len(text) == 0 || len(text) > 128 {
		return nil, fmt.Errorf("expected a canonical nonnegative decimal string of 1 to 128 characters")
	}
	decimalPoint := strings.IndexByte(text, '.')
	if decimalPoint == 0 || decimalPoint == len(text)-1 || (decimalPoint >= 0 && text[len(text)-1] == '0') {
		return nil, fmt.Errorf("expected a canonical nonnegative decimal string")
	}
	if len(text) > 1 && text[0] == '0' && text[1] != '.' {
		return nil, fmt.Errorf("expected a canonical nonnegative decimal string without leading zeros")
	}
	for index := 0; index < len(text); index++ {
		if index == decimalPoint {
			continue
		}
		if text[index] < '0' || text[index] > '9' {
			return nil, fmt.Errorf("expected a canonical nonnegative ASCII decimal string")
		}
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("expected a canonical nonnegative decimal string")
	}
	return value, nil
}
