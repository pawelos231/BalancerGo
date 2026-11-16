package profile

import (
	"fmt"
	"math/big"
)

func init() {
	definitions[KindDiscreteWeighted] = familySpec{
		fields:   []string{"entries"},
		validate: validateDiscreteWeighted,
	}
}

// Weighted entries retain exact values and nonnegative unnormalized weights.
func validateDiscreteWeighted(profile Profile) error {
	if len(profile.Entries) == 0 || len(profile.Entries) > maxProfileItems {
		return fmt.Errorf("profile.entries: expected between 1 and 4096 entries")
	}
	var total big.Rat
	for index, entry := range profile.Entries {
		if _, err := validateQuantity(profile.Unit, fmt.Sprintf("entries[%d].value", index), &entry.Value); err != nil {
			return err
		}
		weight, err := validateParameter(fmt.Sprintf("entries[%d].weight", index), &entry.Weight)
		if err != nil {
			return err
		}
		total.Add(&total, weight)
	}
	if total.Sign() == 0 {
		return fmt.Errorf("profile.entries: weights must have a positive sum")
	}
	return nil
}
