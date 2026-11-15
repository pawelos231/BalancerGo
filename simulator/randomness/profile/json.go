package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// DecodeProfile reads one strict profile, including strict nested collections.
// A failed decode returns the zero profile so partial definitions cannot escape.
func DecodeProfile(data []byte) (Profile, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := expectDelimiter(decoder, '{', "profile"); err != nil {
		return Profile{}, err
	}

	var definition Profile
	fields := make(map[string]**string)
	for _, field := range scalarFields(&definition) {
		fields[field.name] = field.target
	}
	seen := make(map[string]bool)
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
		seen[name] = true

		path := "profile." + name
		switch name {
		case "entries":
			definition.Entries, err = decodeEntries(decoder)
		case "observations":
			definition.Observations, err = decodeObservations(decoder)
		case "kind", "unit":
			var value string
			value, err = decodeString(decoder, path)
			if name == "kind" {
				definition.Kind = Kind(value)
			} else {
				definition.Unit = Unit(value)
			}
		default:
			target, known := fields[name]
			if !known {
				return Profile{}, fmt.Errorf("%s: unknown field", path)
			}
			var value string
			value, err = decodeString(decoder, path)
			*target = &value
		}
		if err != nil {
			return Profile{}, err
		}
	}
	if err := expectDelimiter(decoder, '}', "profile"); err != nil {
		return Profile{}, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return Profile{}, fmt.Errorf("profile: trailing JSON: %w", err)
		}
		return Profile{}, fmt.Errorf("profile: trailing JSON after object")
	}
	if err := ValidateProfile(definition); err != nil {
		return Profile{}, err
	}
	return definition, nil
}

func decodeEntries(decoder *json.Decoder) ([]WeightedEntry, error) {
	if err := expectDelimiter(decoder, '[', "profile.entries"); err != nil {
		return nil, err
	}
	entries := make([]WeightedEntry, 0)
	for decoder.More() {
		if len(entries) == maxProfileItems {
			return nil, fmt.Errorf("profile.entries: accepts at most %d entries", maxProfileItems)
		}
		path := fmt.Sprintf("profile.entries[%d]", len(entries))
		entry, err := decodeEntry(decoder, path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := expectDelimiter(decoder, ']', "profile.entries"); err != nil {
		return nil, err
	}
	return entries, nil
}

func decodeEntry(decoder *json.Decoder, path string) (WeightedEntry, error) {
	if err := expectDelimiter(decoder, '{', path); err != nil {
		return WeightedEntry{}, err
	}
	var entry WeightedEntry
	seen := make(map[string]bool, 2)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return WeightedEntry{}, fmt.Errorf("%s: %w", path, err)
		}
		name, ok := token.(string)
		if !ok {
			return WeightedEntry{}, fmt.Errorf("%s: expected a field name", path)
		}
		if seen[name] {
			return WeightedEntry{}, fmt.Errorf("%s.%s: duplicate field", path, name)
		}
		if name != "value" && name != "weight" {
			return WeightedEntry{}, fmt.Errorf("%s.%s: unknown field", path, name)
		}
		seen[name] = true
		value, err := decodeString(decoder, path+"."+name)
		if err != nil {
			return WeightedEntry{}, err
		}
		if name == "value" {
			entry.Value = value
		} else {
			entry.Weight = value
		}
	}
	if err := expectDelimiter(decoder, '}', path); err != nil {
		return WeightedEntry{}, err
	}
	for _, name := range []string{"value", "weight"} {
		if !seen[name] {
			return WeightedEntry{}, fmt.Errorf("%s.%s: required decimal string", path, name)
		}
	}
	return entry, nil
}

func decodeObservations(decoder *json.Decoder) ([]string, error) {
	if err := expectDelimiter(decoder, '[', "profile.observations"); err != nil {
		return nil, err
	}
	observations := make([]string, 0)
	for decoder.More() {
		if len(observations) == maxProfileItems {
			return nil, fmt.Errorf("profile.observations: accepts at most %d observations", maxProfileItems)
		}
		value, err := decodeString(decoder, fmt.Sprintf("profile.observations[%d]", len(observations)))
		if err != nil {
			return nil, err
		}
		observations = append(observations, value)
	}
	if err := expectDelimiter(decoder, ']', "profile.observations"); err != nil {
		return nil, err
	}
	return observations, nil
}

func decodeString(decoder *json.Decoder, path string) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	value, ok := token.(string)
	if !ok {
		return "", fmt.Errorf("%s: expected a non-null string", path)
	}
	return value, nil
}

func expectDelimiter(decoder *json.Decoder, delimiter json.Delim, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if token != delimiter {
		return fmt.Errorf("%s: expected %q", path, delimiter)
	}
	return nil
}

// EncodeProfile validates and encodes without changing any decimal digits.
func EncodeProfile(definition Profile) ([]byte, error) {
	if err := ValidateProfile(definition); err != nil {
		return nil, err
	}
	return json.Marshal(definition)
}
