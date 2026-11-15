package profile_test

import (
	"reflect"
	"strings"
	"testing"

	"load-balancer/randomness/profile"
)

// New typed parameters cannot silently become valid on an unrelated family.
func TestProfileRejectsParametersOutsideItsFamily(t *testing.T) {
	value := "1"
	for _, field := range []string{
		"Mean", "Stddev", "Shape", "Scale", "Location", "DegreesOfFreedom",
		"Alpha", "Beta", "Median", "LogStddev", "Mode", "Probability",
	} {
		t.Run(field, func(t *testing.T) {
			definition := profile.Profile{Kind: profile.KindConstant, Unit: profile.UnitNumber, Value: &value}
			reflect.ValueOf(&definition).Elem().FieldByName(field).Set(reflect.ValueOf(&value))
			if err := profile.ValidateProfile(definition); err == nil {
				t.Fatal("unrelated typed parameter passed validation")
			}
			if encoded, err := profile.EncodeProfile(definition); err == nil || encoded != nil {
				t.Fatalf("unrelated typed parameter was encoded: %s, %v", encoded, err)
			}
		})
	}
	for _, definition := range []profile.Profile{
		{Kind: profile.KindConstant, Unit: profile.UnitNumber, Value: &value, Entries: []profile.WeightedEntry{}},
		{Kind: profile.KindConstant, Unit: profile.UnitNumber, Value: &value, Observations: []string{}},
	} {
		if err := profile.ValidateProfile(definition); err == nil {
			t.Fatal("an explicit empty collection escaped family field validation")
		}
	}
}

// Nested objects receive the same duplicate, presence, and string checks as scalars.
func TestProfileStrictCollectionsRejectAmbiguousJSON(t *testing.T) {
	for _, example := range []struct {
		collection string
		wantError  string
	}{
		{`"entries":null`, "profile.entries"},
		{`"entries":{}`, "profile.entries"},
		{`"entries":[null]`, "profile.entries[0]"},
		{`"entries":[{"value":"1","value":"2","weight":"1"}]`, "duplicate field"},
		{`"entries":[{"value":"1","\u0076alue":"2","weight":"1"}]`, "duplicate field"},
		{`"entries":[{"value":"1","weight":"1","weight":"2"}]`, "duplicate field"},
		{`"entries":[{"value":"1","weight":"1","extra":"2"}]`, "unknown field"},
		{`"entries":[{"value":null,"weight":"1"}]`, "profile.entries[0].value"},
		{`"entries":[{"value":"1","weight":1}]`, "profile.entries[0].weight"},
		{`"entries":[{"value":"1"}]`, "profile.entries[0].weight"},
		{`"entries":[{"weight":"1"}]`, "profile.entries[0].value"},
		{`"observations":null`, "profile.observations"},
		{`"observations":[1]`, "profile.observations[0]"},
		{`"observations":[null]`, "profile.observations[0]"},
		{`"observations":[{}]`, "profile.observations[0]"},
	} {
		t.Run(example.collection, func(t *testing.T) {
			document := `{"kind":"constant","unit":"number","value":"1",` + example.collection + `}`
			decoded, err := profile.DecodeProfile([]byte(document))
			if err == nil || !strings.Contains(err.Error(), example.wantError) {
				t.Fatalf("strict collection error = %v, want %q", err, example.wantError)
			}
			if !reflect.DeepEqual(decoded, profile.Profile{}) {
				t.Fatalf("failed collection decode leaked a partial definition: %+v", decoded)
			}
		})
	}
}

func TestProfileStrictCollectionsBoundInputSize(t *testing.T) {
	for _, collection := range []string{
		`"entries":[` + strings.Repeat(`{"value":"1","weight":"1"},`, 4096) + `{"value":"1","weight":"1"}]`,
		`"observations":[` + strings.Repeat(`"1",`, 4096) + `"1"]`,
	} {
		document := `{"kind":"constant","unit":"number","value":"1",` + collection + `}`
		decoded, err := profile.DecodeProfile([]byte(document))
		if err == nil || !strings.Contains(err.Error(), "at most 4096") {
			t.Fatalf("oversized collection error = %v", err)
		}
		if !reflect.DeepEqual(decoded, profile.Profile{}) {
			t.Fatal("oversized collection decode leaked a partial definition")
		}
	}
}
