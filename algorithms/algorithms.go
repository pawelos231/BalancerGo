package algorithms

import (
	"load-balancer/server"
)

type Algorithm interface {
	Pick([]*server.Server) (*server.Server, error)
}
