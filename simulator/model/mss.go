package model

// DefaultMSSBytes preserves the original TCP payload limit when MSS is omitted.
const DefaultMSSBytes = 1024

// MaxMSSBytes leaves room for the modeled IPv4 and TCP headers in a 65,535-byte packet.
const MaxMSSBytes = 65535 - 40

// PayloadUnitBytes is a fixed workload unit, independent of how TCP splits the data.
// Five units always mean 5120 bytes, even when MSS is 1460 bytes.
const PayloadUnitBytes = 1024

func ValidMSSBytes(bytes int) bool {
	return bytes >= 1 && bytes <= MaxMSSBytes
}
