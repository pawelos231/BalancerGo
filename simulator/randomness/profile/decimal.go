package profile

import (
	"fmt"
	"math/big"
	"strings"
)

// ParseDecimal validates canonical nonnegative decimals and keeps arithmetic exact.
// The canonical spelling also bounds the size of later sampler arithmetic.
func ParseDecimal(text string) (*big.Rat, error) {
	if len(text) == 0 || len(text) > 128 {
		return nil, fmt.Errorf("expected a canonical nonnegative decimal string of 1 to 128 characters")
	}
	decimalPoint := strings.IndexByte(text, '.')
	if decimalPoint == 0 || decimalPoint == len(text)-1 || (decimalPoint >= 0 && text[len(text)-1] == '0') {
		return nil, fmt.Errorf("expected a canonical nonnegative decimal string")
	}
	if len(text) > 1 && text[0] == '0' && text[1] != '.' {
		return nil, fmt.Errorf("expected a canonical nonnegative decimal string without leading zeros")
	}
	for index := 0; index < len(text); index++ {
		if index == decimalPoint {
			continue
		}
		if text[index] < '0' || text[index] > '9' {
			return nil, fmt.Errorf("expected a canonical nonnegative ASCII decimal string")
		}
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("expected a canonical nonnegative decimal string")
	}
	return value, nil
}
