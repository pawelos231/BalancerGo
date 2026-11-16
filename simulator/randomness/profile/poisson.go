package profile

import "fmt"

func init() {
	definitions[KindPoisson] = familySpec{
		fields:   []string{"mean", "min", "max"},
		validate: validatePoisson,
	}
}

// A zero Poisson mean is valid only when its integer interval includes zero.
func validatePoisson(profile Profile) error {
	if err := requireNumberUnit(profile); err != nil {
		return err
	}
	mean, err := validateParameter("mean", profile.Mean)
	if err != nil {
		return err
	}
	minimum, _, err := validateWholeBounds(profile)
	if err != nil {
		return err
	}
	if mean.Sign() == 0 && minimum.Sign() != 0 {
		return fmt.Errorf("profile.min: a zero poisson mean requires support including 0")
	}
	return nil
}
