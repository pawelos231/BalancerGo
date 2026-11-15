package profile

func validateUniform(definition Profile) error {
	_, _, err := validateBounds(definition)
	return err
}
