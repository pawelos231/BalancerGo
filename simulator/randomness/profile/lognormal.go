package profile

func init() {
	definitions[KindLognormal] = familySpec{
		fields:   []string{"median", "logStddev", "min", "max"},
		validate: validateLognormal,
	}
}

// Median follows the profile unit; logarithmic deviation is dimensionless.
func validateLognormal(profile Profile) error {
	if _, err := positiveQuantity(profile.Unit, "median", profile.Median); err != nil {
		return err
	}
	if _, err := positiveParameter("logStddev", profile.LogStddev); err != nil {
		return err
	}
	_, _, err := validateBounds(profile)
	return err
}
