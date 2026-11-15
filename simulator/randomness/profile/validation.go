package profile

import (
	"fmt"
	"math"
	"math/big"
)

const maxProfileItems = 4096

type familySpec struct {
	fields   []string
	validate func(Profile) error
}

// Accepted families and their required fields. Read-only after package initialization.
var definitions = map[Kind]familySpec{
	KindConstant: {fields: []string{"value"}, validate: validateConstant},
	KindUniform:  {fields: []string{"min", "max"}, validate: validateUniform},
}

// ValidateProfile checks the definition without sampling or selecting a target field.
// Whole-unit realization remains a separate operation.
func ValidateProfile(definition Profile) error {
	spec, supported := definitions[definition.Kind]
	if !supported {
		return fmt.Errorf("profile.kind: unsupported profile kind %q", definition.Kind)
	}
	switch definition.Unit {
	case UnitNanoseconds, UnitBytes, UnitBitsPerSecond, UnitNumber, UnitPercentage:
	default:
		return fmt.Errorf("profile.unit: unsupported profile unit %q", definition.Unit)
	}

	allowed := make(map[string]bool, len(spec.fields))
	for _, name := range spec.fields {
		allowed[name] = true
	}
	present := make(map[string]bool, len(spec.fields))
	for _, field := range scalarFields(&definition) {
		if *field.target != nil {
			present[field.name] = true
			if !allowed[field.name] {
				return unexpectedField(definition.Kind, field.name)
			}
		}
	}
	for _, field := range []struct {
		name    string
		present bool
	}{
		{"entries", definition.Entries != nil},
		{"observations", definition.Observations != nil},
	} {
		if field.present {
			present[field.name] = true
			if !allowed[field.name] {
				return unexpectedField(definition.Kind, field.name)
			}
		}
	}
	for _, name := range spec.fields {
		if !present[name] {
			return fmt.Errorf("profile.%s: required by %s", name, definition.Kind)
		}
	}
	return spec.validate(definition)
}

func unexpectedField(kind Kind, field string) error {
	return fmt.Errorf("profile.%s: field is not accepted by %s", field, kind)
}

func validateQuantity(unit Unit, field string, text *string) (*big.Rat, error) {
	if text == nil {
		return nil, fmt.Errorf("profile.%s: required decimal string", field)
	}
	value, err := ParseDecimal(*text)
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

func validateParameter(field string, text *string) (*big.Rat, error) {
	return validateQuantity(UnitNumber, field, text)
}

func positiveQuantity(unit Unit, field string, text *string) (*big.Rat, error) {
	value, err := validateQuantity(unit, field, text)
	if err != nil {
		return nil, err
	}
	if value.Sign() == 0 {
		return nil, fmt.Errorf("profile.%s: must be greater than zero", field)
	}
	return value, nil
}

func positiveParameter(field string, text *string) (*big.Rat, error) {
	return positiveQuantity(UnitNumber, field, text)
}

func validateBounds(definition Profile) (*big.Rat, *big.Rat, error) {
	minimum, err := validateQuantity(definition.Unit, "min", definition.Min)
	if err != nil {
		return nil, nil, err
	}
	maximum, err := validateQuantity(definition.Unit, "max", definition.Max)
	if err != nil {
		return nil, nil, err
	}
	if minimum.Cmp(maximum) >= 0 {
		return nil, nil, fmt.Errorf("profile.max: %s min must be less than max; use a constant for a fixed value", definition.Kind)
	}
	return minimum, maximum, nil
}

func requireNumberUnit(definition Profile) error {
	if definition.Unit != UnitNumber {
		return fmt.Errorf("profile.unit: %s requires number", definition.Kind)
	}
	return nil
}

func validateWholeBounds(definition Profile) (*big.Rat, *big.Rat, error) {
	minimum, maximum, err := validateBounds(definition)
	if err != nil {
		return nil, nil, err
	}
	if !minimum.IsInt() {
		return nil, nil, fmt.Errorf("profile.min: must be a whole number")
	}
	if !maximum.IsInt() {
		return nil, nil, fmt.Errorf("profile.max: must be a whole number")
	}
	return minimum, maximum, nil
}
