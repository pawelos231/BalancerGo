package profile

import "fmt"

func init() {
	definitions[KindPareto] = familySpec{
		fields:   []string{"scale", "shape", "min", "max"},
		validate: validatePareto,
	}
}

// Pareto's accepted interval starts at or above its positive scale.
func validatePareto(profile Profile) error {
	scale, err := positiveQuantity(profile.Unit, "scale", profile.Scale)
	if err != nil {
		return err
	}
	if _, err := positiveParameter("shape", profile.Shape); err != nil {
		return err
	}
	minimum, _, err := validateBounds(profile)
	if err != nil {
		return err
	}
	if minimum.Cmp(scale) < 0 {
		return fmt.Errorf("profile.min: must be at least scale for pareto")
	}
	return nil
}
