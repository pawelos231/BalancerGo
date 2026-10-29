package scenario

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
)

const maxSafeDocumentInteger int64 = 1<<53 - 1

// EncodeDocument preserves integer precision when a browser reads a scenario.
// Unsafe integers become decimal strings; required nil collections become [].
func EncodeDocument(input Scenario) ([]byte, error) {
	value, err := encodeDocumentValue(reflect.ValueOf(input), "scenario")
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// EncodeNormalizedDocument exposes effective settings without expanding client groups.
// This view uses exact integers but is not an importable Scenario document.
func EncodeNormalizedDocument(input Normalized) ([]byte, error) {
	value, err := encodeDocumentValue(reflect.ValueOf(input), "resolved")
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// DecodeDocument reads one strict scenario document without normalizing it.
// Missing fields keep zero values; null is allowed only for optional pointer fields.
func DecodeDocument(data []byte) (Scenario, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var input Scenario
	target := reflect.ValueOf(&input).Elem()
	if err := decodeDocumentValue(decoder, target, "scenario"); err != nil {
		return Scenario{}, err
	}

	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return Scenario{}, fmt.Errorf("scenario: trailing JSON: %w", err)
		}
		return Scenario{}, fmt.Errorf("scenario: trailing JSON after the document")
	}
	return input, nil
}

// encodeDocumentValue follows Go field types so float settings stay JSON numbers.
func encodeDocumentValue(value reflect.Value, path string) (any, error) {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil, nil
		}
		return encodeDocumentValue(value.Elem(), path)

	case reflect.Struct:
		object := make(map[string]any)
		for index := 0; index < value.NumField(); index++ {
			name, omitEmpty := documentField(value.Type().Field(index))
			field := value.Field(index)
			if name == "-" || (omitEmpty && emptyDocumentValue(field)) {
				continue
			}
			encoded, err := encodeDocumentValue(field, path+"."+name)
			if err != nil {
				return nil, err
			}
			object[name] = encoded
		}
		return object, nil

	case reflect.Slice:
		items := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			encoded, err := encodeDocumentValue(value.Index(index), fmt.Sprintf("%s[%d]", path, index))
			if err != nil {
				return nil, err
			}
			items[index] = encoded
		}
		return items, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number := value.Int()
		if number < -maxSafeDocumentInteger || number > maxSafeDocumentInteger {
			return strconv.FormatInt(number, 10), nil
		}
		return number, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number := value.Uint()
		if number > uint64(maxSafeDocumentInteger) {
			return strconv.FormatUint(number, 10), nil
		}
		return number, nil

	case reflect.Float32, reflect.Float64:
		number := value.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("%s: must be a finite number", path)
		}
		return number, nil

	case reflect.String:
		return value.String(), nil
	case reflect.Bool:
		return value.Bool(), nil
	default:
		return nil, fmt.Errorf("%s: unsupported document type %s", path, value.Type())
	}
}

// documentField uses the same JSON field names and omission tags as Scenario.
func documentField(field reflect.StructField) (string, bool) {
	name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
	if name == "" {
		name = field.Name
	}
	return name, strings.Contains(","+options+",", ",omitempty,")
}

// emptyDocumentValue omits nil patches and empty optional names or collections.
func emptyDocumentValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Slice, reflect.String:
		return value.Len() == 0
	default:
		return value.IsZero()
	}
}

// decodeDocumentValue retains the original numeric text instead of using float64.
func decodeDocumentValue(decoder *json.Decoder, target reflect.Value, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return decodeDocumentToken(decoder, target, token, path)
}

// decodeDocumentToken accepts only the JSON shape required by this Go field.
func decodeDocumentToken(decoder *json.Decoder, target reflect.Value, token json.Token, path string) error {
	if target.Kind() == reflect.Pointer {
		if token == nil {
			target.SetZero()
			return nil
		}
		target.Set(reflect.New(target.Type().Elem()))
		return decodeDocumentToken(decoder, target.Elem(), token, path)
	}
	if token == nil {
		return fmt.Errorf("%s: must not be null", path)
	}

	switch target.Kind() {
	case reflect.Struct:
		if token != json.Delim('{') {
			return fmt.Errorf("%s: expected an object", path)
		}
		return decodeDocumentObject(decoder, target, path)

	case reflect.Slice:
		if token != json.Delim('[') {
			return fmt.Errorf("%s: expected an array", path)
		}
		return decodeDocumentArray(decoder, target, path)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return decodeDocumentInteger(target, token, path)

	case reflect.Float32, reflect.Float64:
		number, ok := token.(json.Number)
		if !ok {
			return fmt.Errorf("%s: expected a number", path)
		}
		value, err := number.Float64()
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || target.OverflowFloat(value) {
			return fmt.Errorf("%s: must be a finite number within %s range", path, target.Type())
		}
		target.SetFloat(value)
		return nil

	case reflect.String:
		value, ok := token.(string)
		if !ok {
			return fmt.Errorf("%s: expected a string", path)
		}
		target.SetString(value)
		return nil

	case reflect.Bool:
		value, ok := token.(bool)
		if !ok {
			return fmt.Errorf("%s: expected a boolean", path)
		}
		target.SetBool(value)
		return nil
	default:
		return fmt.Errorf("%s: unsupported document type %s", path, target.Type())
	}
}

// decodeDocumentObject rejects unknown and duplicate keys before assigning fields.
func decodeDocumentObject(decoder *json.Decoder, target reflect.Value, path string) error {
	fields := make(map[string]int)
	for index := 0; index < target.NumField(); index++ {
		name, _ := documentField(target.Type().Field(index))
		if name != "-" {
			fields[name] = index
		}
	}
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
		fieldPath := path + "." + name
		if seen[name] {
			return fmt.Errorf("%s: duplicate field", fieldPath)
		}
		index, known := fields[name]
		if !known {
			return fmt.Errorf("%s: unknown field", fieldPath)
		}
		seen[name] = true
		if err := decodeDocumentValue(decoder, target.Field(index), fieldPath); err != nil {
			return err
		}
	}
	return closeDocumentContainer(decoder, '}', path)
}

// decodeDocumentArray checks every element against the collection's Go type.
func decodeDocumentArray(decoder *json.Decoder, target reflect.Value, path string) error {
	items := reflect.MakeSlice(target.Type(), 0, 0)
	for decoder.More() {
		item := reflect.New(target.Type().Elem()).Elem()
		itemPath := fmt.Sprintf("%s[%d]", path, items.Len())
		if err := decodeDocumentValue(decoder, item, itemPath); err != nil {
			return err
		}
		items = reflect.Append(items, item)
	}
	if err := closeDocumentContainer(decoder, ']', path); err != nil {
		return err
	}
	target.Set(items)
	return nil
}

// closeDocumentContainer also catches truncated objects and arrays.
func closeDocumentContainer(decoder *json.Decoder, closing json.Delim, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if token != closing {
		return fmt.Errorf("%s: expected %q", path, closing)
	}
	return nil
}

// decodeDocumentInteger validates syntax, browser safety and the exact Go range.
func decodeDocumentInteger(target reflect.Value, token json.Token, path string) error {
	var text string
	quoted := false
	switch value := token.(type) {
	case json.Number:
		text = value.String()
	case string:
		text = value
		quoted = true
	default:
		return fmt.Errorf("%s: expected an integer number or decimal string", path)
	}
	if !decimalDocumentInteger(text) {
		return fmt.Errorf("%s: expected a base-10 integer without fractions or exponents", path)
	}
	if !quoted {
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil || number < -maxSafeDocumentInteger || number > maxSafeDocumentInteger {
			return fmt.Errorf("%s: unsafe JSON integer; use a decimal string outside ±(2^53-1)", path)
		}
	}

	unsigned := target.Kind() >= reflect.Uint && target.Kind() <= reflect.Uint64
	if unsigned {
		number, err := strconv.ParseUint(text, 10, target.Type().Bits())
		if err != nil {
			return fmt.Errorf("%s: integer outside %s range", path, target.Type())
		}
		target.SetUint(number)
	} else {
		number, err := strconv.ParseInt(text, 10, target.Type().Bits())
		if err != nil {
			return fmt.Errorf("%s: integer outside %s range", path, target.Type())
		}
		target.SetInt(number)
	}
	return nil
}

// decimalDocumentInteger keeps quoted integers unambiguous and locale-independent.
func decimalDocumentInteger(text string) bool {
	if strings.HasPrefix(text, "-") {
		text = text[1:]
	}
	if text == "" || (len(text) > 1 && text[0] == '0') {
		return false
	}
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
