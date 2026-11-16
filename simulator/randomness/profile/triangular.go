package profile

import "fmt"

func init() {
	definitions[KindTriangular] = familySpec{
		fields:   []string{"min", "mode", "max"},
		validate: validateTriangular,
	}
}

// A triangular mode may coincide with either interval endpoint.
func validateTriangular(profile Profile) error {
	minimum, maximum, err := validateBounds(profile)
	if err != nil {
		return err
	}
	mode, err := validateQuantity(profile.Unit, "mode", profile.Mode)
	if err != nil {
		return err
	}
	if mode.Cmp(minimum) < 0 || mode.Cmp(maximum) > 0 {
		return fmt.Errorf("profile.mode: must be between min and max")
	}
	return nil
}
