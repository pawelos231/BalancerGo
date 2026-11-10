package receiver

import "load-balancer/model"

// SetECNEnabled enables classic RFC 3168 negotiation before traffic starts.
// ECN is off by default; capability is fixed by the first SYN of each connection.
func (r *Receiver) SetECNEnabled(enabled bool) {
	if r.trafficStarted || len(r.connections) != 0 {
		panic("tcp: ECN must be configured before traffic")
	}
	r.ecnState.Enabled = enabled
}

func (r *Receiver) GetECNState() model.ECNState {
	state := r.ecnState
	for _, conn := range r.connections {
		if conn.ecnNegotiated {
			state.Negotiated = true
			break
		}
	}
	return state
}

func synAckFlags(conn *receiveConnection) model.Flag {
	flags := model.FlagSYN | model.FlagACK
	if conn.ecnNegotiated {
		flags |= model.FlagECE
	}
	return flags
}

// observeECN runs only after normal TCP validity and receive-window checks.
// CWR acknowledges the old indication; a mark on that same packet starts a new one.
// RFC 3168 section 6.1.3: https://www.rfc-editor.org/rfc/rfc3168.html#section-6.1.3
func (r *Receiver) observeECN(conn *receiveConnection, pkt model.Packet) {
	if !conn.ecnNegotiated {
		return
	}
	if pkt.Flag&model.FlagCWR != 0 {
		conn.ecnEcho = false
	}
	if pkt.Size > 0 && pkt.ECN.Codepoint == model.CE {
		conn.ecnEcho = true
		r.ecnState.ReceivedCEPacketCount++
	}
}

// Every data ACK and FIN/ACK repeats pending feedback; SYN/ACK uses negotiation bits.
func (r *Receiver) ackFlags(conn *receiveConnection, flags model.Flag) model.Flag {
	if conn.ecnNegotiated && conn.ecnEcho {
		flags |= model.FlagECE
		r.ecnState.SentECECount++
	}
	return flags
}
