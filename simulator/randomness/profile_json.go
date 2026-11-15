package randomness

import "load-balancer/randomness/profile"

// DecodeProfile reads one strict profile object.
func DecodeProfile(data []byte) (Profile, error) {
	return profile.DecodeProfile(data)
}

// EncodeProfile validates and preserves every decimal digit.
func EncodeProfile(definition Profile) ([]byte, error) {
	return profile.EncodeProfile(definition)
}
