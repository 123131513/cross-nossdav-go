package transport

func NewRMDTBackend(cfg Config) Backend {
	b := NewMasqueBackend(cfg)
	b.mode = ModeRMDT
	b.enableRMDT = true
	return b
}

func NewRMDTBBRBackend(cfg Config) Backend {
	b := NewMasqueBackend(cfg)
	b.mode = ModeRMDTBBR
	b.enableRMDT = true
	b.tunnelCCEnabled = true
	return b
}
