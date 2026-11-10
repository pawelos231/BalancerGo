package sender

import (
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// Low-level callers get the same unsupported-profile guard as configuration users.
func TestECNRejectsNoCongestionControl(t *testing.T) {
	for _, probe := range []bool{false, true} {
		t.Run(map[bool]string{false: "connection", true: "probe"}[probe], func(t *testing.T) {
			sender := newTestSender(engine.New(time.Second))
			sender.SetECNEnabled(true)
			config := model.DefaultCongestionConfig()
			config.Mode = model.CongestionNone
			config.FastRetransmit = false
			sender.SetCongestionConfig(config)
			defer func() {
				if recover() == nil {
					t.Fatal("ECN with congestion control disabled did not fail before the SYN")
				}
			}()
			if probe {
				sender.SendSYNProbe()
			} else {
				sender.EstablishConnection()
			}
		})
	}
}
