package profile

func init() {
	definitions[KindNormal] = familySpec{
		fields:   []string{"mean", "stddev", "min", "max"},
		validate: validateNormal,
	}
}

// A normal profile keeps its location independent of the accepted interval.
func validateNormal(profile Profile) error {
	if _, err := validateQuantity(profile.Unit, "mean", profile.Mean); err != nil {
		return err
	}
	if _, err := positiveQuantity(profile.Unit, "stddev", profile.Stddev); err != nil {
		return err
	}
	_, _, err := validateBounds(profile)
	return err
}
