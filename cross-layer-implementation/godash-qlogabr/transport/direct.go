package transport

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"

	glob "github.com/uccmisl/godash/global"
	"github.com/uccmisl/godash/logging"

	"github.com/quic-go/quic-go/http3"
)

type DirectBackend struct {
	client *http.Client
	tr     *http.Transport
	trQuic *http3.Transport
}

func NewDirectBackend() *DirectBackend {
	return &DirectBackend{}
}

func (b *DirectBackend) Mode() string {
	return ModeDirect
}

func (b *DirectBackend) ProtocolLabel(protocol string) string {
	return protocol
}

func (b *DirectBackend) Close() error {
	if b.trQuic != nil {
		return b.trQuic.Close()
	}
	return nil
}

func (b *DirectBackend) GetHTTPClient(quicBool bool, debugFile string, debugLog bool, useTestbedBool bool) (*http.Transport, *http.Client, *http3.Transport, error) {
	SetTunnelSetupTime(0)
	SetOuterProtocol("-")
	SetTunnelMetrics(TunnelMetrics{})
	SetLastError(nil)

	if b.client != nil {
		return b.tr, b.client, b.trQuic, nil
	}

	var cert tls.Certificate
	var caCertPool *x509.CertPool
	var config *tls.Config
	var quicConfig *tls.Config

	if useTestbedBool {
		logging.DebugPrint(debugFile, debugLog, "DEBUG: ", "Testbed in use")
		var err error
		cert, caCertPool, err = loadTestbedTLS(debugFile, debugLog)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	if quicBool {
		qconf := buildInnerQuicConfig()
		if !useTestbedBool {
			b.trQuic = &http3.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs:            caCertPool,
					InsecureSkipVerify: glob.InsecureSSL,
				},
				QUICConfig: &qconf,
			}
			b.client = &http.Client{Transport: b.trQuic}
		} else {
			logging.DebugPrint(debugFile, debugLog, "DEBUG: ", "creating tls config for quic")
			quicConfig = &tls.Config{
				InsecureSkipVerify: glob.InsecureSSL,
				RootCAs:            caCertPool,
				Certificates:       []tls.Certificate{cert},
			}
			b.trQuic = &http3.Transport{TLSClientConfig: quicConfig, QUICConfig: &qconf, DisableCompression: true}
			b.client = &http.Client{Transport: b.trQuic}
		}
		return b.tr, b.client, b.trQuic, nil
	}

	if useTestbedBool {
		logging.DebugPrint(debugFile, debugLog, "DEBUG: ", "creating tls config")
		config = &tls.Config{
			InsecureSkipVerify: glob.InsecureSSL,
			RootCAs:            caCertPool,
			Certificates:       []tls.Certificate{cert},
		}
		b.tr = &http.Transport{TLSClientConfig: config}
		b.client = &http.Client{Transport: b.tr}
	} else {
		config = &tls.Config{InsecureSkipVerify: glob.InsecureSSL}
		b.tr = &http.Transport{TLSClientConfig: config}
		b.client = &http.Client{Transport: b.tr}
	}
	return b.tr, b.client, b.trQuic, nil
}
