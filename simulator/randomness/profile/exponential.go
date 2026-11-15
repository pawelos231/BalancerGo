package profile

func init() {
	definitions[KindExponential] = familySpec{
		fields:   []string{"mean", "min", "max"},
		validate: validateExponential,
	}
}

// The exponential mean has the same unit as each admitted sample.
func validateExponential(profile Profile) error {
	if _, err := positiveQuantity(profile.Unit, "mean", profile.Mean); err != nil {
		return err
	}
	_, _, err := validateBounds(profile)
	return err
}
