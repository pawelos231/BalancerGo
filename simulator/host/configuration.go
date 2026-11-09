package host

// PreparedConfig binds checked settings to one host without replacing its state.
// Active jobs keep the completion deadline chosen when they started.
type PreparedConfig struct {
	host   *Host
	config Config
}

// PrepareConfig validates against the whole host lifetime, including earlier work.
func (h *Host) PrepareConfig(config Config) PreparedConfig {
	if err := config.Validate(h.engine.EndTime() - h.createdAt); err != nil {
		panic("host: " + err.Error())
	}
	return PreparedConfig{host: h, config: config}
}

// Apply only records elapsed time and installs settings. It never calls packet code.
func (update PreparedConfig) Apply() {
	if update.host.stopped {
		return
	}
	update.host.accountBusyTime()
	update.host.config = update.config
}

// Resume requests ordinary work after the complete configuration cohort is installed.
// Call it from an ordinary engine event, after Apply has run for every resource.
func (update PreparedConfig) Resume() {
	h := update.host
	if h.stopped || h.pumpEvent != 0 || h.queued() == 0 {
		return
	}
	h.pumpEvent = h.engine.ScheduleAt(h.engine.Now(), func() {
		h.pumpEvent = 0
		h.pump()
	})
}
