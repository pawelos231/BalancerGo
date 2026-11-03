package model

// MaxSACKBlocks fits the TCP option area without timestamps or other options.
const MaxSACKBlocks = 4

// SACKBlock reports received bytes from Start up to, but excluding, End.
// For example, [1025,2049) can be received while ACK still asks for byte 1.
type SACKBlock struct {
	Start uint64
	End   uint64
}

// ValidSACKOptions checks option shape; the sender checks its own sequence bounds.
// SACK-permitted belongs to SYN, while range reports belong to later ACKs.
func (p Packet) ValidSACKOptions() bool {
	if p.SACKPermitted && p.Flag&FlagSYN == 0 {
		return false
	}
	if len(p.SACKBlocks) == 0 {
		return true
	}
	if len(p.SACKBlocks) > MaxSACKBlocks || p.Flag&FlagACK == 0 ||
		p.Flag&(FlagSYN|FlagRST) != 0 {
		return false
	}
	for _, block := range p.SACKBlocks {
		if block.Start >= block.End {
			return false
		}
	}
	return true
}
