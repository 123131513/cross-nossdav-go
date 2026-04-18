package algorithms

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const pensieveServiceTimeout = 2 * time.Second
const pensieveOfficialResetPath = "/reset"

var pensieveOfficialBitratesKbps = []int{300, 750, 1200, 1850, 2850, 4300}

type PensieveExternalClient struct {
	ServerURL         string
	HTTPClient        *http.Client
	TotalRebufferTime int
}

type pensievePredictionRequest struct {
	LastQuality         int     `json:"lastquality"`
	Buffer              float64 `json:"buffer"`
	RebufferTime        int     `json:"RebufferTime"`
	LastChunkFinishTime int     `json:"lastChunkFinishTime"`
	LastChunkStartTime  int     `json:"lastChunkStartTime"`
	LastChunkSize       int     `json:"lastChunkSize"`
	LastRequest         int     `json:"lastRequest"`
}

func NewPensieveExternalClient(serverURL string) *PensieveExternalClient {
	return &PensieveExternalClient{
		ServerURL: strings.TrimRight(serverURL, "/"),
		HTTPClient: &http.Client{
			Timeout: pensieveServiceTimeout,
		},
	}
}

func (c *PensieveExternalClient) Reset() error {
	c.TotalRebufferTime = 0

	req, err := http.NewRequest(http.MethodPost, c.ServerURL+pensieveOfficialResetPath, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	// The official Pensieve rl_server has no reset endpoint.
	// Treat reset as best-effort so the client can talk to the unmodified upstream server.
	if resp.StatusCode != http.StatusOK {
		_, _ = io.ReadAll(resp.Body)
		return nil
	}
	return nil
}

func (c *PensieveExternalClient) SelectBitrate(
	bandwithList []int,
	currentRepRate int,
	bufferLevelMs int,
	stallTimeMs int,
	segSizeBytes int,
	deliveryTimeMs int,
	lastRequestNumber int,
) (int, error) {
	if len(bandwithList) != 6 {
		return 0, errors.New("pensieve external model requires exactly 6 representations to match the official action space")
	}
	if err := validatePensieveOfficialBitrateLadder(bandwithList); err != nil {
		return 0, err
	}

	if stallTimeMs > 0 {
		c.TotalRebufferTime += stallTimeMs
	}

	ascendingLocalIndices := sortedRepIndicesAscending(bandwithList)
	serviceQuality, err := localRepToServiceQuality(ascendingLocalIndices, currentRepRate)
	if err != nil {
		return 0, err
	}

	payload := pensievePredictionRequest{
		LastQuality:         serviceQuality,
		Buffer:              float64(bufferLevelMs) / 1000.0,
		RebufferTime:        c.TotalRebufferTime,
		LastChunkFinishTime: deliveryTimeMs,
		LastChunkStartTime:  0,
		LastChunkSize:       segSizeBytes,
		LastRequest:         lastRequestNumber,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest(http.MethodPost, c.ServerURL+"/predict", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("pensieve predict failed: %s", strings.TrimSpace(string(respBody)))
	}

	responseText := strings.TrimSpace(string(respBody))
	if responseText == "REFRESH" {
		return currentRepRate, nil
	}

	serviceChoice, err := strconv.Atoi(responseText)
	if err != nil {
		return 0, err
	}
	if serviceChoice < 0 || serviceChoice >= len(ascendingLocalIndices) {
		return 0, fmt.Errorf("pensieve service returned invalid quality %d", serviceChoice)
	}

	return ascendingLocalIndices[serviceChoice], nil
}

func sortedRepIndicesAscending(bandwithList []int) []int {
	indices := make([]int, len(bandwithList))
	for i := range bandwithList {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		return bandwithList[indices[i]] < bandwithList[indices[j]]
	})
	return indices
}

func localRepToServiceQuality(ascendingLocalIndices []int, localRepRate int) (int, error) {
	for serviceQuality, localIdx := range ascendingLocalIndices {
		if localIdx == localRepRate {
			return serviceQuality, nil
		}
	}
	return 0, fmt.Errorf("local representation index %d not found in Pensieve quality mapping", localRepRate)
}

func validatePensieveOfficialBitrateLadder(bandwithList []int) error {
	ascendingLocalIndices := sortedRepIndicesAscending(bandwithList)
	for i, localIdx := range ascendingLocalIndices {
		if bandwithList[localIdx]/1000 != pensieveOfficialBitratesKbps[i] {
			return fmt.Errorf("pensieve official rl_server expects bitrate ladder %v Kbps, got %v Kbps", pensieveOfficialBitratesKbps, bitratesToKbpsAscending(bandwithList, ascendingLocalIndices))
		}
	}
	return nil
}

func bitratesToKbpsAscending(bandwithList []int, ascendingLocalIndices []int) []int {
	kbps := make([]int, len(ascendingLocalIndices))
	for i, localIdx := range ascendingLocalIndices {
		kbps[i] = bandwithList[localIdx] / 1000
	}
	return kbps
}
