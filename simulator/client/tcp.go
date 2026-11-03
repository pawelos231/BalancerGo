package client

import (
	"time"

	"load-balancer/model"
)

// Existing callers use the client API; protocol state stays inside its TCP sender.
func (c *Client) EstablishConnection()              { c.transport.EstablishConnection() }
func (c *Client) HandlePacket(packet model.Packet)  { c.transport.HandlePacket(packet) }
func (c *Client) SetSender(send func(model.Packet)) { c.transport.SetSender(send) }

func (c *Client) SetReceiveBufferBytes(bytes int)         { c.transport.SetReceiveBufferBytes(bytes) }
func (c *Client) SetSACKEnabled(enabled bool)             { c.transport.SetSACKEnabled(enabled) }
func (c *Client) SetRTOConfig(config model.RTOConfig)     { c.transport.SetRTOConfig(config) }
func (c *Client) SetCloseConfig(config model.CloseConfig) { c.transport.SetCloseConfig(config) }
func (c *Client) SetIdleTimeout(timeout time.Duration)    { c.transport.SetIdleTimeout(timeout) }

func (c *Client) SetCongestionConfig(config model.CongestionConfig) {
	c.transport.SetCongestionConfig(config)
}

func (c *Client) Key() model.ClientKey                  { return c.transport.Key() }
func (c *Client) IsHandshakeCompleted() bool            { return c.transport.IsHandshakeCompleted() }
func (c *Client) IsTerminated() bool                    { return c.transport.IsTerminated() }
func (c *Client) IsClosing() bool                       { return c.transport.IsClosing() }
func (c *Client) StreamCompleted() bool                 { return c.transport.StreamCompleted() }
func (c *Client) GetState() model.ClientState           { return c.transport.GetState() }
func (c *Client) GetConnectionState() model.CloseState  { return c.transport.GetConnectionState() }
func (c *Client) GetTransferState() model.TransferState { return c.transport.GetTransferState() }
func (c *Client) GetRTOState() model.RTOState           { return c.transport.GetRTOState() }

func (c *Client) GetCongestionState() model.CongestionState {
	return c.transport.GetCongestionState()
}
