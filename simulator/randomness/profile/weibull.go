package profile

func init() {
	definitions[KindWeibull] = familySpec{
		fields:   []string{"scale", "shape", "min", "max"},
		validate: validateWeibull,
	}
}

// Weibull scale follows the profile unit and shape stays dimensionless.
func validateWeibull(profile Profile) error {
	if _, err := positiveQuantity(profile.Unit, "scale", profile.Scale); err != nil {
		return err
	}
	if _, err := positiveParameter("shape", profile.Shape); err != nil {
		return err
	}
	_, _, err := validateBounds(profile)
	return err
}
