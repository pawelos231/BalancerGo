package server

import (
	"context"
	"fmt"
	"load-balancer/model"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	alpha       = 0.002 // reaction to new measurements
	bytesPerCPU = 512.0 // cost of scaling in bytes per CPU usage
	maxCPU      = 10000 // says how many packets per second a server can handle at max
)

type Server struct {
	ID              string
	Name            string
	Address         string
	Distance        int16
	Weight          int16
	Active          bool
	Killed          bool
	Tags            []string
	Metadata        map[string]string
	Draining        bool
	HealthTCP       bool
	LatencyP95Milli uint32
	CpuUsage        atomic.Uint32

	MaxConnections      uint32
	ActiveConnections   uint32
	HalfOpenConnections uint32
	MaxHalfOpen         uint32
	DrainTimeout        time.Duration
	DrainStartedAt      time.Time

	Version       string
	ConfigVersion string
	LastUpdated   time.Time

	// internal
	DataIn                 chan model.Packet // channel for incoming packets from the LB
	DataOut                chan model.Packet // channel for outgoing packets to the LB
	Probe                  chan struct{}     // channel for health probes (e.g. TCP SYNs)
	ProbeAck               chan struct{}     // channel for health probe ACKs
	NumberOfHandledPackets atomic.Uint32
	Ctx                    context.Context
	cancel                 context.CancelFunc
	closeOnce              sync.Once
	wg                     sync.WaitGroup
}

func NewServer(id, name, address string, port, distance, weight int16,
	out chan model.Packet,
	in chan model.Packet, maxHalfOpen uint32) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	return &Server{
		ID:                     id,
		Name:                   name,
		Address:                address,
		Distance:               distance,
		Weight:                 weight,
		MaxHalfOpen:            maxHalfOpen,
		HealthTCP:              true, // default to healthy
		LatencyP95Milli:        0,    // default to 0 latency
		Active:                 true, // default to active
		Tags:                   []string{},
		Metadata:               make(map[string]string),
		DrainTimeout:           120 * time.Second,      // default drain timeout
		DataIn:                 in,                     // channel for incoming packets from the load balancer
		DataOut:                out,                    // channel for outgoing packets to the load balancer
		Probe:                  make(chan struct{}, 1), // buffered channel for health probes
		ProbeAck:               make(chan struct{}, 1), // buffered channel for health probe ACKs
		NumberOfHandledPackets: atomic.Uint32{},        // initialize handled packets count
		Ctx:                    ctx,                    // context for managing timeouts
		cancel:                 cancel,                 // function to cancel the context and signal shutdown
		closeOnce:              sync.Once{},            // ensures cleanup logic is executed only once
		wg:                     sync.WaitGroup{},
	}

}

func (s *Server) Shutdown() {
	s.cancel()
	s.wg.Wait()

	s.closeOnce.Do(func() {
		s.Killed = true
		s.Active = false
		s.HealthTCP = false
		close(s.Probe)
		close(s.ProbeAck)
	})
}

func (s *Server) MarkFailed() {
	fmt.Println("Marking server as FAILED:", s.ID, s.Name)
	s.Active = false
	s.HealthTCP = false
	atomic.StoreUint32(&s.LatencyP95Milli, math.MaxUint32)
	s.Draining = true
	s.DrainStartedAt = time.Now()
}

func (s *Server) MarkHealthy() {
	if s.Killed {
		fmt.Println("Cannot mark server as HEALTHY, it is KILLED:", s.ID, s.Name)
		return
	}
	fmt.Println("Marking server as HEALTHY:", s.ID, s.Name)
	s.Active = true
	s.HealthTCP = true
	atomic.StoreUint32(&s.LatencyP95Milli, 0)
	s.Draining = false
	s.DrainStartedAt = time.Time{}
}

func (s *Server) StartDrain() {
	s.Draining = true
	s.DrainStartedAt = time.Now()
}
func (s *Server) StopDrain() {
	s.Draining = false
	s.DrainStartedAt = time.Time{}
}

func (s *Server) HandleHealthChecking() {
	s.wg.Add(1)
	defer s.wg.Done()

	for {
		select {
		case <-s.Ctx.Done():
			return
		case _, ok := <-s.Probe:
			if !ok {
				return
			}
			select {
			case s.ProbeAck <- struct{}{}:
			case <-s.Ctx.Done():
				return
			}
		}
	}
}

func (s *Server) HandlePacketStream(stream <-chan model.Packet) {
	s.wg.Add(1)
	defer s.wg.Done()

	connStart := make(map[model.ClientKey]time.Time)
	const bucket = 100
	var latSamples [bucket]int64
	var sampleIdx int

	updateP95 := func(ms int64) {
		latSamples[sampleIdx%bucket] = ms
		sampleIdx++

		n := sampleIdx
		if n > bucket {
			n = bucket
		}

		tmp := make([]int64, n)
		copy(tmp, latSamples[:n])
		sort.Slice(tmp, func(i, j int) bool { return tmp[i] < tmp[j] })

		idx := int(math.Ceil(0.95*float64(n))) - 1
		if idx < 0 {
			idx = 0
		}

		p95 := tmp[idx]
		atomic.StoreUint32(&s.LatencyP95Milli, uint32(p95))
	}

	for {
		select {
		case <-s.Ctx.Done():
			fmt.Printf("Server %s: Shutting down packet handler.\n", s.ID)
			return
		case pkt, ok := <-stream:
			if !ok {
				return
			}

			ThreeWayHandshakeCompleted := pkt.Flag&model.FlagACK != 0 && pkt.Flag&model.FlagSYN == 0 && connStart[pkt.Key] != (time.Time{})
			InitiateThreeWayHandshake := pkt.Flag&model.FlagSYN != 0 && pkt.Flag&model.FlagACK == 0
			PushPacket := pkt.Flag&model.FlagPSH != 0
			FinPacket := pkt.Flag&model.FlagFIN != 0

			// hierachical combining in the future?
			s.NumberOfHandledPackets.Add(1)
			// fmt.Println(pkt)

			switch {
			case InitiateThreeWayHandshake:
				{
					key := pkt.Key
					atomic.AddUint32(&s.HalfOpenConnections, 1)
					connStart[key] = time.Now()

					resp := model.Packet{
						Key:           key,
						Flag:          model.FlagSYN | model.FlagACK, // SYN-ACK response
						PlaceInStream: pkt.PlaceInStream + 1,
						Size:          pkt.Size,
					}

					select {
					case s.DataOut <- resp:
					case <-s.Ctx.Done():
						return
					}
					break
				}

			case ThreeWayHandshakeCompleted:
				{
					atomic.AddUint32(&s.ActiveConnections, 1)
					atomic.AddUint32(&s.HalfOpenConnections, ^uint32(0)) // decrement half-open connections
					break
				}

			case PushPacket:
				{
					resp := model.Packet{
						Key:           pkt.Key,
						Flag:          model.FlagACK,
						PlaceInStream: pkt.PlaceInStream + 1,
						Size:          1024,
					}
					select {
					case s.DataOut <- resp:
					case <-s.Ctx.Done():
						return
					}
					break
				}
			case FinPacket:
				{
					atomic.AddUint32(&s.ActiveConnections, ^uint32(0)) // decrement active connections

					if _, ok := connStart[pkt.Key]; ok {
						t0 := connStart[pkt.Key]
						updateP95(time.Since(t0).Milliseconds())
						delete(connStart, pkt.Key)
					}

					resp := model.Packet{
						Key:           pkt.Key,
						Flag:          model.FlagFIN | model.FlagACK,
						PlaceInStream: pkt.PlaceInStream + 1,
						Size:          0,
					}
					select {
					case s.DataOut <- resp:
					case <-s.Ctx.Done():
						return
					}
					break
				}

			default:
				// ignore other packets
			}
		}
	}
}

func (s *Server) generateLatencyHistogram() []int32 {
	return []int32{}
}

func (s *Server) CanAcceptNewConnection() bool {
	if !s.Active || s.Draining && s.IsHealthy() {
		return false
	}
	if s.MaxConnections > 0 && s.ActiveConnections >= s.MaxConnections {
		return false // max connections reached
	}
	return true
}

func (s *Server) UpdateHealth(healthTCP bool, latencyP95Milli uint32) {
	s.HealthTCP = healthTCP
	atomic.StoreUint32(&s.LatencyP95Milli, latencyP95Milli)
}

func (s *Server) ShouldBeTerminated() bool {
	return s.Draining && s.ActiveConnections == 0 && s.HalfOpenConnections == 0
}

func (s *Server) IsHealthy() bool {
	if s.Draining &&
		time.Since(s.DrainStartedAt) > s.DrainTimeout &&
		atomic.LoadUint32(&s.ActiveConnections) == 0 {
		return false
	}
	return s.HealthTCP && s.Active && !s.Draining
}

func (s *Server) Score() float64 {
	if !s.IsHealthy() {
		return math.Inf(1)
	}

	drainingPenalty := 0.0
	if s.Draining {
		drainingPenalty = 10.0
	}

	return float64(s.LatencyP95Milli)*(1.0/float64(max(1, int(s.Weight)))) + drainingPenalty
}

func (s *Server) CalculatePacketsPerSecond() {
	before := s.NumberOfHandledPackets.Load()
	time.Sleep(1 * time.Second)
	after := s.NumberOfHandledPackets.Load()

	s.CpuUsage.Store(after - before)
	s.CalculatePacketsPerSecond()
}

func (s *Server) GetState() model.ServerState {
	return model.ServerState{
		ID:                     s.ID,
		Name:                   s.Name,
		Address:                s.Address,
		Active:                 s.Active,
		ActiveConnections:      atomic.LoadUint32(&s.ActiveConnections),
		HalfOpenConnections:    atomic.LoadUint32(&s.HalfOpenConnections),
		LatencyP95Milli:        atomic.LoadUint32(&s.LatencyP95Milli),
		CpuUsage:               float32(s.CpuUsage.Load()) / float32(maxCPU), // normalize to [0,1]
		NumberOfHandledPackets: s.NumberOfHandledPackets.Load(),
	}
}
