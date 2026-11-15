package profile

func init() {
	definitions[KindStudentT] = familySpec{
		fields:   []string{"location", "scale", "degreesOfFreedom", "min", "max"},
		validate: validateStudentT,
	}
}

// Student's t keeps its location separate from the accepted interval.
func validateStudentT(profile Profile) error {
	if _, err := validateQuantity(profile.Unit, "location", profile.Location); err != nil {
		return err
	}
	if _, err := positiveQuantity(profile.Unit, "scale", profile.Scale); err != nil {
		return err
	}
	if _, err := positiveParameter("degreesOfFreedom", profile.DegreesOfFreedom); err != nil {
		return err
	}
	_, _, err := validateBounds(profile)
	return err
}
