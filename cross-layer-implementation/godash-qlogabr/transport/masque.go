package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	masque "github.com/quic-go/masque-go"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/yosida95/uritemplate/v3"
)

const masqueInitialPacketSize = 1350
const masqueInnerInitialPacketSize = 800

const (
	masqueOuterHandshakeTimeout = 3 * time.Second
	masqueConnectUDPTimeout     = 5 * time.Second
)

type MasqueBackend struct {
	cfg           Config
	direct        *DirectBackend
	client        *http.Client
	trQuic        *http3.Transport
	masqueClient  *masque.Client
	proxyTemplate *uritemplate.Template
}

func NewMasqueBackend(cfg Config) *MasqueBackend {
	return &MasqueBackend{
		cfg:    cfg,
		direct: NewDirectBackend(),
	}
}

func (b *MasqueBackend) Mode() string {
	return ModeMasque
}

func (b *MasqueBackend) ProtocolLabel(protocol string) string {
	return protocol + "+MASQUE"
}

func (b *MasqueBackend) Close() error {
	if b.trQuic != nil {
		_ = b.trQuic.Close()
	}
	if b.masqueClient != nil {
		return b.masqueClient.Close()
	}
	return nil
}

func (b *MasqueBackend) GetHTTPClient(quicBool bool, debugFile string, debugLog bool, useTestbedBool bool) (*http.Transport, *http.Client, *http3.Transport, error) {
	if !quicBool {
		return b.direct.GetHTTPClient(quicBool, debugFile, debugLog, useTestbedBool)
	}
	if b.client != nil {
		return nil, b.client, b.trQuic, nil
	}

	tpl, err := uritemplate.New(b.cfg.MasqueProxyTemplate)
	if err != nil {
		SetLastError(fmt.Errorf("invalid MASQUE proxy template: %w", err))
		return nil, nil, nil, fmt.Errorf("invalid MASQUE proxy template: %w", err)
	}
	b.proxyTemplate = tpl

	innerTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{http3.NextProtoH3},
	}
	if useTestbedBool {
		cert, caCertPool, err := loadTestbedTLS(debugFile, debugLog)
		if err != nil {
			SetLastError(err)
			return nil, nil, nil, err
		}
		innerTLS = &tls.Config{
			InsecureSkipVerify: true,
			RootCAs:            caCertPool,
			Certificates:       []tls.Certificate{cert},
			NextProtos:         []string{http3.NextProtoH3},
		}
	}

	outerQconf := buildOuterQuicConfig()
	outerQconf.EnableDatagrams = true
	outerQconf.InitialPacketSize = masqueInitialPacketSize
	outerQconf.HandshakeIdleTimeout = masqueOuterHandshakeTimeout
	outerQconf.MaxIdleTimeout = masqueConnectUDPTimeout

	b.masqueClient = &masque.Client{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: b.cfg.MasqueInsecure,
			NextProtos:         []string{http3.NextProtoH3},
		},
		QUICConfig: &outerQconf,
	}

	innerQconf := buildInnerQuicConfig()
	innerQconf.EnableDatagrams = true
	innerQconf.InitialPacketSize = masqueInnerInitialPacketSize
	innerQconf.DisablePathMTUDiscovery = true
	b.trQuic = &http3.Transport{
		TLSClientConfig: innerTLS,
		QUICConfig:      &innerQconf,
	}
	b.trQuic.Dial = func(ctx context.Context, addr string, tlsConf *tls.Config, quicConf *quic.Config) (*quic.Conn, error) {
		tunnelStart := time.Now()
		raddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			SetLastError(fmt.Errorf("resolve target udp address failed: %w", err))
			return nil, err
		}

		pconn, rsp, err := b.masqueClient.Dial(context.Background(), b.proxyTemplate, raddr)
		if rsp != nil && rsp.Proto != "" {
			SetOuterProtocol(rsp.Proto + " CONNECT-UDP")
		}
		if err != nil {
			SetLastError(err)
			return nil, err
		}

		SetTunnelSetupTime(time.Since(tunnelStart))
		SetLastError(nil)
		if SnapshotState().OuterProtocol == "" {
			SetOuterProtocol("HTTP/3.0 CONNECT-UDP")
		}

		if quicConf == nil {
			quicConf = &quic.Config{}
		} else {
			quicConf = quicConf.Clone()
		}
		quicConf.InitialPacketSize = masqueInnerInitialPacketSize
		quicConf.DisablePathMTUDiscovery = true

		if tlsConf == nil {
			tlsConf = cloneTLSConfig(innerTLS)
		}

		return quic.DialEarly(ctx, pconn, raddr, tlsConf, quicConf)
	}

	b.client = &http.Client{Transport: b.trQuic}
	return nil, b.client, b.trQuic, nil
}
