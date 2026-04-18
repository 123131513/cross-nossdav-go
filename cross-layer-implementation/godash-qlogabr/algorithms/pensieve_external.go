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

	otherhttp "github.com/uccmisl/godash/http"
)

const pensieveServiceTimeout = 2 * time.Second

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
	NextChunkSizes      []int   `json:"nextChunkSizes"`
	VideoChunkRemain    int     `json:"video_chunk_remain"`
	VideoBitRate        []int   `json:"videoBitRate"`
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

	req, err := http.NewRequest(http.MethodPost, c.ServerURL+"/reset", bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pensieve reset failed: %s", strings.TrimSpace(string(body)))
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
	nextSegmentNumber int,
	representations []otherhttp.Representation,
) (int, error) {
	if len(representations) != 6 || len(bandwithList) != 6 {
		return 0, errors.New("pensieve external model requires exactly 6 representations to match the official action space")
	}

	if stallTimeMs > 0 {
		c.TotalRebufferTime += stallTimeMs
	}

	remaining := getPensieveChunksRemaining(representations, nextSegmentNumber)
	if remaining <= 0 {
		return currentRepRate, nil
	}

	ascendingLocalIndices := sortedRepIndicesAscending(bandwithList)
	serviceQuality, err := localRepToServiceQuality(ascendingLocalIndices, currentRepRate)
	if err != nil {
		return 0, err
	}

	nextChunkSizesBytes, serviceBitratesKbps, err := buildPensieveServiceInputs(representations, bandwithList, ascendingLocalIndices, nextSegmentNumber)
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
		NextChunkSizes:      nextChunkSizesBytes,
		VideoChunkRemain:    remaining,
		VideoBitRate:        serviceBitratesKbps,
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

	serviceChoice, err := strconv.Atoi(strings.TrimSpace(string(respBody)))
	if err != nil {
		return 0, err
	}
	if serviceChoice < 0 || serviceChoice >= len(ascendingLocalIndices) {
		return 0, fmt.Errorf("pensieve service returned invalid quality %d", serviceChoice)
	}

	return ascendingLocalIndices[serviceChoice], nil
}

func buildPensieveServiceInputs(representations []otherhttp.Representation, bandwithList []int, ascendingLocalIndices []int, nextSegmentNumber int) ([]int, []int, error) {
	nextChunkSizesBytes := make([]int, len(ascendingLocalIndices))
	serviceBitratesKbps := make([]int, len(ascendingLocalIndices))

	for serviceIdx, localIdx := range ascendingLocalIndices {
		serviceBitratesKbps[serviceIdx] = bandwithList[localIdx] / 1000

		if representations[localIdx].Chunks == "" {
			return nil, nil, fmt.Errorf("representation %d has no chunk metadata; Pensieve service needs per-chunk sizes", localIdx)
		}

		chunkBits, ok := getPensieveChunkBits(representations[localIdx].Chunks, nextSegmentNumber)
		if !ok {
			return nil, nil, fmt.Errorf("missing chunk metadata for next segment %d in representation %d", nextSegmentNumber, localIdx)
		}
		nextChunkSizesBytes[serviceIdx] = chunkBits / 8
	}

	return nextChunkSizesBytes, serviceBitratesKbps, nil
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

func getPensieveChunkBits(chunkList string, nextSegmentNumber int) (int, bool) {
	chunks := strings.Split(chunkList, ",")
	index := nextSegmentNumber - 1
	if index < 0 || index >= len(chunks) {
		return 0, false
	}
	val, err := strconv.Atoi(chunks[index])
	if err != nil {
		return 0, false
	}
	return val, true
}

func getPensieveChunksRemaining(representations []otherhttp.Representation, nextSegmentNumber int) int {
	for _, representation := range representations {
		if representation.Chunks == "" {
			continue
		}
		chunks := strings.Split(representation.Chunks, ",")
		remaining := len(chunks) - (nextSegmentNumber - 1)
		if remaining < 0 {
			return 0
		}
		return remaining
	}
	return 0
}
