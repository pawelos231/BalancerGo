package model

// TCPState describes the current stage of a TCP connection.
type TCPState string

const (
	TCPClosed      TCPState = "closed"      // Connection is inactive.
	TCPSynSent     TCPState = "synSent"     // Sent SYN; waiting for SYN+ACK.
	TCPSynReceived TCPState = "synReceived" // Sent SYN+ACK; waiting for ACK.
	TCPEstablished TCPState = "established" // Ready to exchange data.
	TCPFinWait1    TCPState = "finWait1"    // Sent FIN; waiting for its ACK.
	TCPFinWait2    TCPState = "finWait2"    // Our FIN was acknowledged; waiting for peer FIN.
	TCPCloseWait   TCPState = "closeWait"   // Received peer FIN; we can still send data.
	TCPClosing     TCPState = "closing"     // Both sent FIN; ours still needs an ACK.
	TCPLastAck     TCPState = "lastAck"     // Replied to peer FIN with FIN; waiting for our FIN's ACK.
	TCPTimeWait    TCPState = "timeWait"    // Keep state to ACK duplicate FINs.
)
