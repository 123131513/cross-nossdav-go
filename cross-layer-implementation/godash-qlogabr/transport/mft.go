package transport

type MFTBackend struct {
	*DirectBackend
}

func NewMFTBackend(Config) Backend {
	return &MFTBackend{DirectBackend: NewDirectBackend()}
}

func (b *MFTBackend) Mode() string {
	return ModeMFT
}

func (b *MFTBackend) ProtocolLabel(protocol string) string {
	return protocol + "+MFT"
}
