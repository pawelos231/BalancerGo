package scenario

import (
	"fmt"
	"sort"
	"strings"

	"load-balancer/randomness"
)

// validateRandomProfiles checks every unbound definition, including unused profiles.
// They remain in the source document and do not enter the normalized runtime settings.
func validateRandomProfiles(profiles map[string]randomness.Profile) error {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := documentMapPath("fixed.randomProfiles", name)
		if err := validateID(name, path); err != nil {
			return err
		}
		if err := randomness.ValidateProfile(profiles[name]); err != nil {
			return randomProfileError(path, err)
		}
	}
	return nil
}

// randomProfileError replaces the standalone codec's root with the scenario location.
func randomProfileError(path string, err error) error {
	return fmt.Errorf("%s%s", path, strings.TrimPrefix(err.Error(), "profile"))
}
