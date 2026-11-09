package scenario

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

// EncodeDocument preserves exact integers and checks document syntax.
// Timeline relationships and simulation settings are validated separately.
func EncodeDocument(input Scenario) ([]byte, error) {
	if err := checkDocument(input); err != nil {
		return nil, err
	}
	value, err := encodeDocumentValue(reflect.ValueOf(input), "scenario")
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// EncodeNormalizedDocument exposes effective states without expanding clients.
func EncodeNormalizedDocument(input Normalized) ([]byte, error) {
	value, err := encodeDocumentValue(reflect.ValueOf(input), "resolved")
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// DecodeDocument reads a strict scenario document without resolving its timeline.
// Ordinary missing fields retain their Go zero values for later validation.
func DecodeDocument(data []byte) (Scenario, error) {
	var input Scenario
	if err := decodeDocument(data, reflect.ValueOf(&input).Elem(), "scenario"); err != nil {
		return Scenario{}, err
	}
	if err := checkDocument(input); err != nil {
		return Scenario{}, err
	}
	return input, nil
}

// checkDocument limits format checks to the version and timestamp spelling.
func checkDocument(input Scenario) error {
	if input.ScenarioVersion != CurrentScenarioVersion {
		return fmt.Errorf("scenario.scenarioVersion: expected version %d, got %d", CurrentScenarioVersion, input.ScenarioVersion)
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
