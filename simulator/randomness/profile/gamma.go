package profile

func init() {
	definitions[KindGamma] = familySpec{
		fields:   []string{"shape", "scale", "min", "max"},
		validate: validateGamma,
	}
}

// Gamma shape is dimensionless; scale follows the profile unit.
func validateGamma(profile Profile) error {
	if _, err := positiveParameter("shape", profile.Shape); err != nil {
		return err
	}
	if _, err := positiveQuantity(profile.Unit, "scale", profile.Scale); err != nil {
		return err
	}
	_, _, err := validateBounds(profile)
	return err
}
