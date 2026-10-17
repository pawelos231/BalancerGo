package model

type Entity interface {
	ExpectResponse()
	SendStream()
	Retransmit()
}
