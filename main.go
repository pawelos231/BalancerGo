package main

import (
	"fmt"
	"load-balancer/algorithms"
	"load-balancer/balancer"
	"load-balancer/client"
	httpserver "load-balancer/http"
	"load-balancer/model"
	"load-balancer/server"
	"math/rand"
	"sync"
	"time"
)

const (
	nClients     = 100
	nServers     = nClients / 10
	packetsCount = 5000
	gap          = 100 * time.Millisecond
)

func main() {
	rand.Seed(time.Now().UnixNano())

	lb := setupLoadBalancer()
	hub := setupHttpServer()

	setupServers(lb, nServers)
	var wg sync.WaitGroup
	setupClients(lb, nClients, &wg)

	done := make(chan struct{})
	go runTicker(lb, hub, done, lb.RefreshInterval)

	wg.Wait()
	close(done)
	fmt.Println("ALL STREAMS COMPLETED")
}

func setupLoadBalancer() *balancer.LoadBalancer {
	return balancer.NewLoadBalancer(model.LBModeNAT, &algorithms.RoundRobin{}, time.Millisecond*500)
}

func setupHttpServer() *httpserver.Hub {
	hub := httpserver.NewHub()
	go hub.Run()
	go httpserver.StartServer(hub)
	return hub
}

func setupServers(lb *balancer.LoadBalancer, n int) {
	for i := 0; i < n; i++ {
		srv := server.NewServer(
			fmt.Sprintf("srv-%02d", i),
			fmt.Sprintf("backend-%02d", i),
			fmt.Sprintf("10.0.0.%d", i+10),
			80, 0, 1,
			make(chan model.Packet),
			make(chan model.Packet),
			100,
		)
		lb.AddServer(srv)

		go srv.HandlePacketStream(srv.DataIn)
		go srv.HandleHealthChecking()
	}
}

func setupClients(lb *balancer.LoadBalancer, n int, wg *sync.WaitGroup) {
	for i := 0; i < n; i++ {
		cl := client.NewClient(
			fmt.Sprintf("192.168.1.%d", i+1),
			int16(40000+i),
			"lb.local",
			80,
			"TCP",
			make(chan model.Packet),
			make(chan model.Packet),
		)
		lb.AddClient(cl)

		wg.Add(1)
		go func(c *client.Client) {
			defer wg.Done()
			c.ExpectResponse()
			c.Open()
			c.SendCompleteStream(packetsCount*model.SEGMENT_SIZE, gap)
		}(cl)
	}
}

func runTicker(lb *balancer.LoadBalancer, hub *httpserver.Hub, done chan struct{}, tickInterval time.Duration) {
	t := time.NewTicker(tickInterval)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			fmt.Println("LB: TICK")
			// Chaos: 1 in 10 chance to kill a random server
			if rand.Intn(10) == 0 {
				if killedServerID, err := lb.KillRandomServer(); err == nil {
					fmt.Printf("CHAOS: Killed server %s\n", killedServerID)
				}
			}

			// if rand.Intn(2) == 0 {
			// 	if killedClientID, err := lb.KillRandomClient(); err == nil {
			// 		fmt.Printf("CHAOS: Killed client %s\n", killedClientID)
			// 	}
			// }
			state := lb.Tick()
			hub.BroadcastState(state)
		case <-done:
			return
		}
	}
}
