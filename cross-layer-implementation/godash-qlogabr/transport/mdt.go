package transport

type MDTBackend struct {
	*MasqueBackend
}

func NewMDTBackend(cfg Config) *MDTBackend {
	return &MDTBackend{MasqueBackend: NewMasqueBackend(cfg)}
}

func (b *MDTBackend) Mode() string {
	return ModeMDT
}
