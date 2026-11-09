package scenario

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// encodeDocumentMap keeps required nil maps as empty JSON objects.
func encodeDocumentMap(value reflect.Value, path string) (any, error) {
	if value.Type().Key().Kind() != reflect.String {
		return nil, fmt.Errorf("%s: unsupported document map key type %s", path, value.Type().Key())
	}
	object := make(map[string]any, value.Len())
	entries := value.MapRange()
	for entries.Next() {
		name := entries.Key().String()
		encoded, err := encodeDocumentValue(entries.Value(), documentMapPath(path, name))
		if err != nil {
			return nil, err
		}
		object[name] = encoded
	}
	return object, nil
}

// decodeDocumentMap rejects decoded duplicate keys before reading their values.
func decodeDocumentMap(decoder *json.Decoder, target reflect.Value, path string) error {
	if target.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("%s: unsupported document map key type %s", path, target.Type().Key())
	}
	items := reflect.MakeMap(target.Type())
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("%s: expected an object field name", path)
		}
		itemPath := documentMapPath(path, name)
		if seen[name] {
			return fmt.Errorf("%s: duplicate key", itemPath)
		}
		seen[name] = true
		item := reflect.New(target.Type().Elem()).Elem()
		if err := decodeDocumentValue(decoder, item, itemPath); err != nil {
			return err
		}
		key := reflect.New(target.Type().Key()).Elem()
		key.SetString(name)
		items.SetMapIndex(key, item)
	}
	if err := closeDocumentContainer(decoder, '}', path); err != nil {
		return err
	}
	target.Set(items)
	return nil
}

// documentMapPath quotes map keys so errors stay readable for arbitrary strings.
func documentMapPath(path, key string) string {
	return fmt.Sprintf("%s[%q]", path, key)
}
