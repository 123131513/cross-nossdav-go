package algorithms

import (
	"bytes"
	"encoding/json"
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

type PensieveExternalClient struct {
	ServerURL         string
	HTTPClient        *http.Client
	TotalRebufferTime int
	ExpectedActions   int
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

type pensieveServiceMetadata struct {
	ActionDim            int   `json:"action_dim"`
	BitratesBpsAscending []int `json:"bitrates_bps_ascending"`
	StateHistoryLen      int   `json:"state_history_len"`
	TotalVideoChunks     int   `json:"total_video_chunks"`
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

// ValidateLadder checks a metadata-aware Pensieve service against the runtime MPD.
// Legacy six-action upstream services have no metadata endpoint and remain
// supported only for six-representation MPDs.
func (c *PensieveExternalClient) ValidateLadder(bandwithList []int) error {
	if len(bandwithList) < 2 {
		return fmt.Errorf("pensieve external model requires at least 2 representations, got %d", len(bandwithList))
	}
	req, err := http.NewRequest(http.MethodGet, c.ServerURL+"/metadata", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("query pensieve metadata: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read pensieve metadata: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if len(bandwithList) == 6 {
			c.ExpectedActions = 6
			return nil
		}
		return fmt.Errorf("pensieve service has no usable metadata for %d-action MPD: HTTP %d", len(bandwithList), resp.StatusCode)
	}
	var metadata pensieveServiceMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		if len(bandwithList) == 6 {
			c.ExpectedActions = 6
			return nil
		}
		return fmt.Errorf("pensieve service returned invalid metadata for %d-action MPD: %w", len(bandwithList), err)
	}
	if metadata.ActionDim != len(bandwithList) {
		return fmt.Errorf("pensieve service action_dim %d does not match MPD representations %d", metadata.ActionDim, len(bandwithList))
	}
	if metadata.StateHistoryLen > 0 && metadata.StateHistoryLen < metadata.ActionDim {
		return fmt.Errorf("pensieve service state_history_len %d is smaller than action_dim %d", metadata.StateHistoryLen, metadata.ActionDim)
	}
	if len(metadata.BitratesBpsAscending) > 0 {
		localAscending := make([]int, len(bandwithList))
		for quality, localIndex := range sortedRepIndicesAscending(bandwithList) {
			localAscending[quality] = bandwithList[localIndex]
		}
		if len(metadata.BitratesBpsAscending) != len(localAscending) {
			return fmt.Errorf("pensieve metadata bitrate count %d does not match MPD count %d", len(metadata.BitratesBpsAscending), len(localAscending))
		}
		for quality := range localAscending {
			if metadata.BitratesBpsAscending[quality] != localAscending[quality] {
				return fmt.Errorf(
					"pensieve metadata bitrate at quality %d is %d, MPD has %d",
					quality,
					metadata.BitratesBpsAscending[quality],
					localAscending[quality],
				)
			}
		}
	}
	c.ExpectedActions = metadata.ActionDim
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
	if len(bandwithList) < 2 {
		return 0, fmt.Errorf("pensieve external model requires at least 2 representations, got %d", len(bandwithList))
	}
	if c.ExpectedActions > 0 && len(bandwithList) != c.ExpectedActions {
		return 0, fmt.Errorf("pensieve external model expects %d representations, got %d", c.ExpectedActions, len(bandwithList))
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

	// The official Pensieve rl_server_no_training.py serves predictions on the root path.
	req, err := http.NewRequest(http.MethodPost, c.ServerURL, bytes.NewReader(body))
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
