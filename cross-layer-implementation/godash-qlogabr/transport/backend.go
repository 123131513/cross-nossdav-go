package transport

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	glob "github.com/uccmisl/godash/global"
	"github.com/uccmisl/godash/logging"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/qlog"

	xlayer "github.com/uccmisl/godash/crosslayer"
)

const (
	ModeDirect = "direct"
	ModeMasque = "masque"
)

type Config struct {
	Mode                string
	MasqueProxyTemplate string
	MasqueInsecure      bool
}

type Backend interface {
	Mode() string
	GetHTTPClient(quicBool bool, debugFile string, debugLog bool, useTestbedBool bool) (*http.Transport, *http.Client, *http3.Transport, error)
	ProtocolLabel(protocol string) string
	Close() error
}

type StateSnapshot struct {
	TunnelSetupTime time.Duration
	OuterProtocol   string
	LastError       string
}

type bufferedWriteCloser struct {
	*bufio.Writer
	io.Closer
}

func newBufferedWriteCloser(writer *bufio.Writer, closer io.Closer) io.WriteCloser {
	return &bufferedWriteCloser{Writer: writer, Closer: closer}
}

func (h bufferedWriteCloser) Close() error {
	if err := h.Writer.Flush(); err != nil {
		return err
	}
	return h.Closer.Close()
}

var accountant *xlayer.CrossLayerAccountant
var stateMu sync.RWMutex
var transportState StateSnapshot

func SetAccountant(acc *xlayer.CrossLayerAccountant) {
	accountant = acc
}

func ResetState() {
	stateMu.Lock()
	defer stateMu.Unlock()
	transportState = StateSnapshot{}
}

func SetTunnelSetupTime(d time.Duration) {
	stateMu.Lock()
	defer stateMu.Unlock()
	transportState.TunnelSetupTime = d
}

func SetOuterProtocol(protocol string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	transportState.OuterProtocol = protocol
}

func SetLastError(err error) {
	stateMu.Lock()
	defer stateMu.Unlock()
	if err == nil {
		transportState.LastError = ""
		return
	}
	transportState.LastError = err.Error()
}

func SnapshotState() StateSnapshot {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return transportState
}

func NewBackend(cfg Config) (Backend, error) {
	switch cfg.Mode {
	case "", ModeDirect:
		return NewDirectBackend(), nil
	case ModeMasque:
		if cfg.MasqueProxyTemplate == "" {
			return nil, fmt.Errorf("masque transport requires a MASQUE proxy template")
		}
		return NewMasqueBackend(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported transport mode: %s", cfg.Mode)
	}
}

func loadTestbedTLS(debugFile string, debugLog bool) (tls.Certificate, *x509.CertPool, error) {
	var cert tls.Certificate
	caCertPool := x509.NewCertPool()

	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		logging.DebugPrint(debugFile, debugLog, "DEBUG: ", "Unable to determine executable location for testbed server certs")
		return cert, nil, err
	}

	cert, err = tls.LoadX509KeyPair(dir+"/"+glob.HTTPcertLocation, dir+"/"+glob.HTTPkeyLocation)
	if err != nil {
		return cert, nil, err
	}

	caCert, err := ioutil.ReadFile(dir + "/" + glob.HTTPcertLocation)
	if err != nil {
		return cert, nil, err
	}
	if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
		logging.DebugPrint(debugFile, debugLog, "DEBUG: ", "No certs appended, using system certs only")
	}
	return cert, caCertPool, nil
}

func buildInnerQuicConfig() quic.Config {
	qconf := quic.Config{}
	qconf.Tracer = qlog.NewTracer(func(_ bool, connID []byte) io.WriteCloser {
		filename := fmt.Sprintf("logs/client_%x.qlog", connID)
		f, err := os.Create(filename)
		if err != nil {
			panic(err)
		}
		return newBufferedWriteCloser(bufio.NewWriter(f), f)
	}, nil)
	if accountant != nil {
		qconf.Tracer = qlog.NewTracer(func(_ bool, connID []byte) io.WriteCloser {
			filename := fmt.Sprintf("logs/client_%x.qlog", connID)
			f, err := os.Create(filename)
			if err != nil {
				panic(err)
			}
			return newBufferedWriteCloser(bufio.NewWriter(f), f)
		}, accountant.EventChannel)
	}
	return qconf
}

func buildOuterQuicConfig() quic.Config {
	qconf := quic.Config{
		EnableDatagrams: true,
	}
	qconf.Tracer = qlog.NewTracer(func(_ bool, connID []byte) io.WriteCloser {
		filename := fmt.Sprintf("logs/masque_outer_%x.qlog", connID)
		f, err := os.Create(filename)
		if err != nil {
			panic(err)
		}
		return newBufferedWriteCloser(bufio.NewWriter(f), f)
	}, nil)
	return qconf
}

func cloneTLSConfig(in *tls.Config) *tls.Config {
	if in == nil {
		return &tls.Config{}
	}
	return in.Clone()
}

func contextWithFallback(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}
