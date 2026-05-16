package transport

func NewTECCBackend(cfg Config) Backend {
	b := NewMasqueBackend(cfg)
	b.mode = ModeTECC
	b.enableRMDT = true
	b.enableFeedback = true
	b.tunnelCCEnabled = true
	return b
}
