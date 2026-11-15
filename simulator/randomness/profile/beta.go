package profile

func init() {
	definitions[KindBeta] = familySpec{
		fields:   []string{"alpha", "beta", "min", "max"},
		validate: validateBeta,
	}
}

// Beta's two shapes are dimensionless; bounds define its affine support.
func validateBeta(profile Profile) error {
	if _, err := positiveParameter("alpha", profile.Alpha); err != nil {
		return err
	}
	if _, err := positiveParameter("beta", profile.Beta); err != nil {
		return err
	}
	_, _, err := validateBounds(profile)
	return err
}
