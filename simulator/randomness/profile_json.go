package randomness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// DecodeProfile reads one strict profile object, rejecting duplicate and unknown fields.
// A failed decode returns the zero profile so partial definitions cannot escape validation.
func DecodeProfile(data []byte) (Profile, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil {
		return Profile{}, fmt.Errorf("profile: %w", err)
	}
	if opening != json.Delim('{') {
		return Profile{}, fmt.Errorf("profile: expected an object")
	}
	var profile Profile
	seen := make(map[string]bool, 5)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return Profile{}, fmt.Errorf("profile: %w", err)
		}
		name, ok := token.(string)
		if !ok {
			return Profile{}, fmt.Errorf("profile: expected a field name")
		}
		if seen[name] {
			return Profile{}, fmt.Errorf("profile.%s: duplicate field", name)
		}
		switch name {
		case "kind", "unit", "value", "min", "max":
		default:
			return Profile{}, fmt.Errorf("profile.%s: unknown field", name)
		}
		seen[name] = true
		token, err = decoder.Token()
		if err != nil {
			return Profile{}, fmt.Errorf("profile.%s: %w", name, err)
		}
		value, ok := token.(string)
		if !ok {
			return Profile{}, fmt.Errorf("profile.%s: expected a non-null string", name)
		}
		switch name {
		case "kind":
			profile.Kind = Kind(value)
		case "unit":
			profile.Unit = Unit(value)
		case "value":
			profile.Value = &value
		case "min":
			profile.Min = &value
		case "max":
			profile.Max = &value
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return Profile{}, fmt.Errorf("profile: %w", err)
	}
	if closing != json.Delim('}') {
		return Profile{}, fmt.Errorf("profile: expected end of object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return Profile{}, fmt.Errorf("profile: trailing JSON: %w", err)
		}
		return Profile{}, fmt.Errorf("profile: trailing JSON after object")
	}
	if err := ValidateProfile(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// EncodeProfile validates and encodes a definition without changing any decimal digits.
func EncodeProfile(profile Profile) ([]byte, error) {
	if err := ValidateProfile(profile); err != nil {
		return nil, err
	}
	return json.Marshal(profile)
}
