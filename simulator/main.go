package main

import (
	"context"
	"fmt"
	"load-balancer/algorithms"
	"load-balancer/balancer"
	"load-balancer/client"
	httpserver "load-balancer/http"
	"load-balancer/model"
	"load-balancer/server"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/trace"
	"sync"
	"syscall"
	"time"

	_ "net/http/pprof"
)

const (
	nClients     = 200                  // number of clients
	nServers     = 10                   // number of servers (1 server for every 10 clients)
	packetsCount = 10000                // number of packets per stream
	delay        = 0 * time.Millisecond // gap between packets
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	f, _ := os.Create("trace.out")
	trace.Start(f)
	defer trace.Stop()

	runtime.SetMutexProfileFraction(1)

	go func() {
		http.ListenAndServe(":6060", nil)
	}()

	rand.Seed(time.Now().UnixNano())

	lb := setupLoadBalancer()
	hub := setupHttpServer(lb)

	var wg sync.WaitGroup
	setupServers(lb, nServers)
	go runTicker(lb, hub, ctx.Done())
	go func() {
		setupClients(lb, nClients, &wg)
		wg.Wait()
		fmt.Println("ALL STREAMS COMPLETED")
	}()

	// Keep serving the visualizer after the simulated streams finish.
	<-ctx.Done()
}

func setupLoadBalancer() *balancer.LoadBalancer {
	return balancer.NewLoadBalancer(model.LBModeNAT, &algorithms.RoundRobin{}, time.Millisecond*100)
}

func setupHttpServer(lb *balancer.LoadBalancer) *httpserver.Hub {
	hub := httpserver.NewHub()
	go hub.Run(lb)
	go httpserver.StartServer(hub)
	return hub
}

func setupServers(lb *balancer.LoadBalancer, n int) {
	ready := make(chan struct{}, n*3)

	for i := 0; i < n; i++ {

		out := make(chan *model.Packet)
		in := make(chan *model.Packet)

		srv := server.NewServer(
			fmt.Sprintf("srv-%02d", i),
			fmt.Sprintf("backend-%02d", i),
			fmt.Sprintf("10.0.0.%d", i+10),
			80, 0, 1,
			out,
			in,
			100,
		)

		lb.AddServer(srv)

		go srv.HandlePacketStream(srv.DataIn, ready)
		go srv.CalculatePacketsPerSecond(ready)
		go srv.HandleHealthChecking(ready)
	}

	for i := 0; i < n*3; i++ {
		<-ready
		// fmt.Println("Server goroutine ready", i+1, "/", n*3)
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
			make(chan *model.Packet),
			make(chan *model.Packet),
		)
		lb.AddClient(cl)
		fmt.Println(i, n)

		cl.EstablishConnection()

		wg.Add(2)
		go func(c *client.Client) {
			defer wg.Done()
			c.ListenForResponse()
		}(cl)

		go func(c *client.Client) {
			defer wg.Done()
			c.SendCompleteStream(packetsCount*model.SEGMENT_SIZE, delay)
		}(cl)
	}
}

func runTicker(lb *balancer.LoadBalancer, hub *httpserver.Hub, done <-chan struct{}) {
	t := time.NewTicker(lb.RefreshInterval)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			// fmt.Println("LB: TICK")
			// Chaos: 1 in 10 chance to kill a random server
			// if rand.Intn(10) == 0 {
			// 	if killedServerID, err := lb.KillRandomServer(); err == nil {
			// 		fmt.Printf("CHAOS: Killed server %s\n", killedServerID)
			// 	}
			// }

			// if rand.Intn(2) == 0 {
			// 	if killedClientID, err := lb.KillRandomClient(); err == nil {
			// 		fmt.Printf("CHAOS: Killed client %s\n", killedClientID)
			// 	}
			// }
			state := lb.Tick()
			err := hub.BroadcastState(state)
			if err != nil {
				fmt.Println("Error broadcasting state to hub:", err)
				continue
			}
		case <-done:
			return
		}
	}
}
