package scenario

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

// EncodeDocumentV2 preserves exact integers and checks v2 document syntax.
// Timeline relationships and simulation settings are validated separately.
func EncodeDocumentV2(input ScenarioV2) ([]byte, error) {
	if err := checkDocumentV2(input); err != nil {
		return nil, err
	}
	value, err := encodeDocumentValue(reflect.ValueOf(input), "scenario")
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// EncodeNormalizedDocumentV2 exposes effective states without expanding clients.
func EncodeNormalizedDocumentV2(input NormalizedV2) ([]byte, error) {
	value, err := encodeDocumentValue(reflect.ValueOf(input), "resolved")
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// DecodeDocumentV2 reads a strict v2 document without resolving its timeline.
// Ordinary missing fields retain their Go zero values for later validation.
func DecodeDocumentV2(data []byte) (ScenarioV2, error) {
	var input ScenarioV2
	if err := decodeDocument(data, reflect.ValueOf(&input).Elem(), "scenario"); err != nil {
		return ScenarioV2{}, err
	}
	if err := checkDocumentV2(input); err != nil {
		return ScenarioV2{}, err
	}
	return input, nil
}

// checkDocumentV2 limits format checks to the version and timestamp spelling.
func checkDocumentV2(input ScenarioV2) error {
	if input.Version != VersionV2 {
		return fmt.Errorf("scenario.version: expected version %d, got %d", VersionV2, input.Version)
	}
	for key := range input.States {
		if !canonicalDocumentTimestamp(key) {
			path := documentMapPath("scenario.states", key)
			return fmt.Errorf("%s: expected a canonical nonnegative int64 nanosecond timestamp", path)
		}
	}
	return nil
}

// canonicalDocumentTimestamp accepts only ASCII decimal int64 nanoseconds.
func canonicalDocumentTimestamp(text string) bool {
	if text == "" || (len(text) > 1 && text[0] == '0') {
		return false
	}
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	_, err := strconv.ParseInt(text, 10, 64)
	return err == nil
}
