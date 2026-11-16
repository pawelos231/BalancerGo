package profile

import "fmt"

func init() {
	definitions[KindEmpirical] = familySpec{
		fields:   []string{"observations"},
		validate: validateEmpirical,
	}
}

// Empirical observations keep duplicates as separate equal-mass outcomes.
func validateEmpirical(profile Profile) error {
	if len(profile.Observations) == 0 || len(profile.Observations) > maxProfileItems {
		return fmt.Errorf("profile.observations: expected between 1 and 4096 observations")
	}
	for index, observation := range profile.Observations {
		if _, err := validateQuantity(profile.Unit, fmt.Sprintf("observations[%d]", index), &observation); err != nil {
			return err
		}
	}
	return nil
}
