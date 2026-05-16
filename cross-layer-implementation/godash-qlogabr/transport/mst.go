package transport

func NewMSTBackend(cfg Config) Backend {
	b := NewMasqueBackend(cfg)
	b.mode = ModeMST
	b.enableMST = true
	return b
}
