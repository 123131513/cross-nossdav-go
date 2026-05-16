package transport

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	ModeDirect  = "direct"
	ModeMasque  = "masque"
	ModeMDT     = "mdt"
	ModeMST     = "mst"
	ModeMFT     = "mft"
	ModeRMDT    = "rmdt"
	ModeRMDTBBR = "rmdt-bbr"
	ModeTECC    = "tecc"
)

type Config struct {
	Mode                   string
	MasqueProxyTemplate    string
	MasqueInsecure         bool
	TECCProxyDebugURL      string
	TECCFeedbackForwardURL string
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
	TunnelMetrics   TunnelMetrics
}

type TunnelMetrics struct {
	Retrans           string
	RetransRate       string
	QueueBytes        string
	BwEstimate        string
	TargetRate        string
	FeedbackRTT       string
	ServerSendRate    string
	TunnelForwardRate string
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

func SetTunnelMetrics(metrics TunnelMetrics) {
	stateMu.Lock()
	defer stateMu.Unlock()
	transportState.TunnelMetrics = metrics
}

func UpdateTunnelMetrics(update TunnelMetrics) {
	stateMu.Lock()
	defer stateMu.Unlock()
	if update.Retrans != "" {
		transportState.TunnelMetrics.Retrans = update.Retrans
	}
	if update.RetransRate != "" {
		transportState.TunnelMetrics.RetransRate = update.RetransRate
	}
	if update.QueueBytes != "" {
		transportState.TunnelMetrics.QueueBytes = update.QueueBytes
	}
	if update.BwEstimate != "" {
		transportState.TunnelMetrics.BwEstimate = update.BwEstimate
	}
	if update.TargetRate != "" {
		transportState.TunnelMetrics.TargetRate = update.TargetRate
	}
	if update.FeedbackRTT != "" {
		transportState.TunnelMetrics.FeedbackRTT = update.FeedbackRTT
	}
	if update.ServerSendRate != "" {
		transportState.TunnelMetrics.ServerSendRate = update.ServerSendRate
	}
	if update.TunnelForwardRate != "" {
		transportState.TunnelMetrics.TunnelForwardRate = update.TunnelForwardRate
	}
}

func ObserveHTTPResponse(resp *http.Response) {
	if resp == nil {
		return
	}
	rate := resp.Header.Get("X-TECC-Pacing-Rate-Bps")
	if rate == "" {
		return
	}
	UpdateTunnelMetrics(TunnelMetrics{
		ServerSendRate: rate,
		TargetRate:     rate,
	})
}

func RefreshTunnelMetricsFromProxyDebug(ctx context.Context, rawURL string) error {
	if rawURL == "" {
		return nil
	}
	qtr := &http3.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	defer qtr.Close()
	client := http.Client{Transport: qtr, Timeout: time.Second}
	req, err := http.NewRequestWithContext(contextWithFallback(ctx), http.MethodGet, rawURL, nil)
	if err != nil {
		SetLastError(err)
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		SetLastError(err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		err := fmt.Errorf("proxy debug status=%d", resp.StatusCode)
		SetLastError(err)
		return err
	}
	var debug map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&debug); err != nil {
		SetLastError(err)
		return err
	}
	UpdateTunnelMetrics(tunnelMetricsFromProxyDebug(debug))
	return nil
}

func tunnelMetricsFromProxyDebug(debug map[string]any) TunnelMetrics {
	if debug == nil {
		return TunnelMetrics{}
	}
	metrics, _ := debug["metrics"].(map[string]any)
	retrans := uint64FromAnyMap(metrics, "packets_retransmitted")
	sent := uint64FromAnyMap(metrics, "packets_sent")
	queuePackets := firstNonZeroUint64(
		uint64FromAnyMap(debug, "inflight_packets"),
		uint64FromAnyMap(debug, "queue_packets"),
	)
	out := TunnelMetrics{
		Retrans:    strconv.FormatUint(retrans, 10),
		QueueBytes: strconv.FormatUint(estimatedTunnelQueueBytes(queuePackets), 10),
	}
	if sent > 0 {
		out.RetransRate = fmt.Sprintf("%.6f", float64(retrans)/float64(sent))
	}
	if latest, _ := debug["latest_tecc_feedback"].(map[string]any); latest != nil {
		out.TunnelForwardRate = strconv.FormatUint(firstNonZeroUint64(
			uint64FromAnyMap(latest, "tr_t_bps"),
			uint64FromAnyMap(latest, "send_rate_bps"),
		), 10)
		out.BwEstimate = out.TunnelForwardRate
		out.TargetRate = strconv.FormatUint(uint64FromAnyMap(latest, "target_rate_bps"), 10)
		out.FeedbackRTT = strconv.FormatUint(firstNonZeroUint64(
			uint64FromAnyMap(latest, "t_t_micros"),
			uint64FromAnyMap(latest, "min_rtt_micros"),
		), 10)
		if out.RetransRate == "" {
			ppm := firstNonZeroUint64(
				uint64FromAnyMap(latest, "r_t_ppm"),
				uint64FromAnyMap(latest, "retransmission_rate_ppm"),
			)
			if ppm > 0 {
				out.RetransRate = fmt.Sprintf("%.6f", float64(ppm)/1_000_000.0)
			}
		}
		if out.QueueBytes == "0" {
			out.QueueBytes = strconv.FormatUint(estimatedTunnelQueueBytes(firstNonZeroUint64(
				uint64FromAnyMap(latest, "q_t_packets"),
				uint64FromAnyMap(latest, "queue_packets"),
			)), 10)
		}
	}
	return out
}

func uint64FromAnyMap(m map[string]any, key string) uint64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		if v > 0 {
			return uint64(v)
		}
	case uint64:
		return v
	case int:
		if v > 0 {
			return uint64(v)
		}
	case string:
		n, _ := strconv.ParseUint(v, 10, 64)
		return n
	}
	return 0
}

func firstNonZeroUint64(values ...uint64) uint64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func estimatedTunnelQueueBytes(queuePackets uint64) uint64 {
	return queuePackets * masqueInitialPacketSize
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
	case ModeMDT:
		if cfg.MasqueProxyTemplate == "" {
			return nil, fmt.Errorf("mdt transport requires a MASQUE proxy template")
		}
		return NewMDTBackend(cfg), nil
	case ModeMST:
		if cfg.MasqueProxyTemplate == "" {
			return nil, fmt.Errorf("mst transport requires a MASQUE proxy template")
		}
		return NewMSTBackend(cfg), nil
	case ModeMFT:
		return NewMFTBackend(cfg), nil
	case ModeRMDT:
		if cfg.MasqueProxyTemplate == "" {
			return nil, fmt.Errorf("rmdt transport requires a MASQUE proxy template")
		}
		return NewRMDTBackend(cfg), nil
	case ModeRMDTBBR:
		if cfg.MasqueProxyTemplate == "" {
			return nil, fmt.Errorf("rmdt-bbr transport requires a MASQUE proxy template")
		}
		return NewRMDTBBRBackend(cfg), nil
	case ModeTECC:
		if cfg.MasqueProxyTemplate == "" {
			return nil, fmt.Errorf("tecc transport requires a MASQUE proxy template")
		}
		return NewTECCBackend(cfg), nil
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
