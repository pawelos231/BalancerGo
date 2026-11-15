package profile

func validateConstant(definition Profile) error {
	_, err := validateQuantity(definition.Unit, "value", definition.Value)
	return err
}
