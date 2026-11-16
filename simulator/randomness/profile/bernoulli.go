package profile

import (
	"fmt"
	"math/big"
)

func init() {
	definitions[KindBernoulli] = familySpec{
		fields:   []string{"probability"},
		validate: validateBernoulli,
	}
}

// Bernoulli uses a dimensionless probability and emits the numbers zero or one.
func validateBernoulli(profile Profile) error {
	if err := requireNumberUnit(profile); err != nil {
		return err
	}
	probability, err := validateParameter("probability", profile.Probability)
	if err != nil {
		return err
	}
	if probability.Cmp(big.NewRat(1, 1)) > 0 {
		return fmt.Errorf("profile.probability: must be between 0 and 1")
	}
	return nil
}
