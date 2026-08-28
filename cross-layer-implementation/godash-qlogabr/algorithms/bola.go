package algorithms

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	godashhttp "github.com/uccmisl/godash/http"
)

const (
	bolaDefaultMinBufferSec = 3.0
	bolaEpsilon             = 1e-9
	bolaSSIMDBMin           = 0.0
	bolaSSIMDBMax           = 60.0
)

const bolaSSIMProfileSchemaVersion = 1

type bolaCandidate struct {
	index        int
	bandwidthBps int
	chunks       string
	avgSizeBits  float64
	nextSizeBits float64
	utility      float64
	paramUtility float64
}

type bolaSSIMRepresentation struct {
	BandwidthBps int       `json:"bandwidth_bps"`
	SSIM         []float64 `json:"ssim"`
}

type bolaSSIMProfileFile struct {
	SchemaVersion   int                      `json:"schema_version"`
	Utility         string                   `json:"utility"`
	Reference       string                   `json:"reference,omitempty"`
	Representations []bolaSSIMRepresentation `json:"representations"`
}

// BOLASSIMProfile contains measured per-segment SSIM indices for bola-ssim.
type BOLASSIMProfile struct {
	reference       string
	segmentCount    int
	representations map[int]bolaSSIMRepresentation
}

// LoadBOLASSIMProfile loads and validates a versioned BOLA SSIM profile.
func LoadBOLASSIMProfile(path string) (*BOLASSIMProfile, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("bola-ssim requires a non-empty SSIM profile path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read BOLA SSIM profile: %w", err)
	}
	return ParseBOLASSIMProfile(data)
}

// ParseBOLASSIMProfile parses a BOLA SSIM profile without filesystem access.
func ParseBOLASSIMProfile(data []byte) (*BOLASSIMProfile, error) {
	var input bolaSSIMProfileFile
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("parse BOLA SSIM profile: %w", err)
	}
	if input.SchemaVersion != bolaSSIMProfileSchemaVersion {
		return nil, fmt.Errorf("unsupported BOLA SSIM profile schema_version %d", input.SchemaVersion)
	}
	if input.Utility != "ssim-db" {
		return nil, fmt.Errorf("BOLA SSIM profile utility must be ssim-db, got %q", input.Utility)
	}
	if strings.TrimSpace(input.Reference) == "" {
		return nil, fmt.Errorf("BOLA SSIM profile requires a non-empty reference identifier")
	}
	if len(input.Representations) < 2 {
		return nil, fmt.Errorf("BOLA SSIM profile requires at least two representations")
	}

	profile := &BOLASSIMProfile{
		reference:       input.Reference,
		representations: make(map[int]bolaSSIMRepresentation, len(input.Representations)),
	}
	ordered := append([]bolaSSIMRepresentation(nil), input.Representations...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].BandwidthBps < ordered[j].BandwidthBps })
	for index, representation := range ordered {
		if representation.BandwidthBps <= 0 {
			return nil, fmt.Errorf("BOLA SSIM profile has invalid bandwidth_bps %d", representation.BandwidthBps)
		}
		if _, exists := profile.representations[representation.BandwidthBps]; exists {
			return nil, fmt.Errorf("BOLA SSIM profile repeats bandwidth_bps %d", representation.BandwidthBps)
		}
		if len(representation.SSIM) == 0 {
			return nil, fmt.Errorf("BOLA SSIM profile bandwidth %d has no segment values", representation.BandwidthBps)
		}
		if index == 0 {
			profile.segmentCount = len(representation.SSIM)
		} else if len(representation.SSIM) != profile.segmentCount {
			return nil, fmt.Errorf("BOLA SSIM profile bandwidth %d has %d segments, expected %d", representation.BandwidthBps, len(representation.SSIM), profile.segmentCount)
		}
		for segmentIndex, value := range representation.SSIM {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < -1 || value > 1 {
				return nil, fmt.Errorf("BOLA SSIM profile bandwidth %d segment %d has invalid SSIM %v", representation.BandwidthBps, segmentIndex+1, value)
			}
			if index > 0 {
				previous := ordered[index-1].SSIM[segmentIndex]
				if value+bolaEpsilon < previous {
					return nil, fmt.Errorf("BOLA SSIM profile segment %d is not nondecreasing by bandwidth: %d=%v, %d=%v", segmentIndex+1, ordered[index-1].BandwidthBps, previous, representation.BandwidthBps, value)
				}
			}
		}
		profile.representations[representation.BandwidthBps] = representation
	}
	return profile, nil
}

// Reference describes the source used to produce the SSIM measurements.
func (profile *BOLASSIMProfile) Reference() string {
	if profile == nil {
		return ""
	}
	return profile.reference
}

// ValidateMPD requires exact representation and segment alignment.
func (profile *BOLASSIMProfile) ValidateMPD(mpd godashhttp.MPD) error {
	if profile == nil {
		return fmt.Errorf("BOLA SSIM profile is nil")
	}
	if len(mpd.Periods) == 0 {
		return fmt.Errorf("MPD has no periods")
	}
	for _, adaptationSet := range mpd.Periods[0].AdaptationSet {
		if len(adaptationSet.Representation) != len(profile.representations) {
			continue
		}
		matching := true
		for _, representation := range adaptationSet.Representation {
			if _, ok := profile.representations[representation.BandWidth]; !ok {
				matching = false
				break
			}
		}
		if !matching {
			continue
		}
		ordered := append([]godashhttp.Representation(nil), adaptationSet.Representation...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].BandWidth < ordered[j].BandWidth })
		for _, representation := range ordered {
			chunkCount := robustMPCChunkCount(representation.Chunks)
			if chunkCount != profile.segmentCount {
				return fmt.Errorf("BOLA SSIM profile bandwidth %d has %d SSIM values but MPD has %d chunks", representation.BandWidth, profile.segmentCount, chunkCount)
			}
		}
		for segment := 1; segment <= profile.segmentCount; segment++ {
			previousSize := 0
			previousSSIM := math.Inf(-1)
			for _, representation := range ordered {
				chunkBits, ok := robustMPCChunkBits(representation.Chunks, segment)
				if !ok || chunkBits <= 0 {
					return fmt.Errorf("MPD bandwidth %d segment %d has no valid chunk size", representation.BandWidth, segment)
				}
				ssim := profile.representations[representation.BandWidth].SSIM[segment-1]
				if chunkBits < previousSize || ssim+bolaEpsilon < previousSSIM {
					return fmt.Errorf("BOLA SSIM segment %d is not nondecreasing by encoded size and utility", segment)
				}
				previousSize = chunkBits
				previousSSIM = ssim
			}
		}
		return nil
	}
	return fmt.Errorf("BOLA SSIM profile representation ladder does not match the MPD")
}

// BOLA implements a BOLA-BASIC style selector for GoDASH.
//
// Puffer's reference implementation uses SSIM utility. GoDASH does not carry
// per-format SSIM metadata, so this port uses log bitrate utility and MPD chunk
// sizes. The selected value is a GoDASH representation index.
func BOLA(
	bufferLevelMs int,
	maxBufferSec int,
	segmentDurationMs int,
	segmentNumber int,
	mpd godashhttp.MPD,
	currentMPDRepAdaptSet int,
	bandwithList []int,
	lowestMPDrepRateIndex int,
) int {
	return bolaSelect(bufferLevelMs, maxBufferSec, segmentDurationMs, segmentNumber, mpd, currentMPDRepAdaptSet, bandwithList, lowestMPDrepRateIndex, nil)
}

// BOLASSIM implements BOLA-BASIC v1 with measured per-segment SSIMdB utility.
func BOLASSIM(
	bufferLevelMs int,
	maxBufferSec int,
	segmentDurationMs int,
	segmentNumber int,
	mpd godashhttp.MPD,
	currentMPDRepAdaptSet int,
	bandwithList []int,
	lowestMPDrepRateIndex int,
	profile *BOLASSIMProfile,
) int {
	if profile == nil {
		return lowestMPDrepRateIndex
	}
	return bolaSelect(bufferLevelMs, maxBufferSec, segmentDurationMs, segmentNumber, mpd, currentMPDRepAdaptSet, bandwithList, lowestMPDrepRateIndex, profile)
}

func bolaSelect(
	bufferLevelMs int,
	maxBufferSec int,
	segmentDurationMs int,
	segmentNumber int,
	mpd godashhttp.MPD,
	currentMPDRepAdaptSet int,
	bandwithList []int,
	lowestMPDrepRateIndex int,
	ssimProfile *BOLASSIMProfile,
) int {
	if len(bandwithList) == 0 || segmentDurationMs <= 0 || maxBufferSec <= 0 {
		return lowestMPDrepRateIndex
	}
	if currentMPDRepAdaptSet < 0 || len(mpd.Periods) == 0 || currentMPDRepAdaptSet >= len(mpd.Periods[0].AdaptationSet) {
		return lowestMPDrepRateIndex
	}

	representationSet := mpd.Periods[0].AdaptationSet[currentMPDRepAdaptSet].Representation
	candidates, validUtility := bolaCandidatesWithUtility(bandwithList, representationSet, segmentNumber+1, float64(segmentDurationMs)/1000.0, ssimProfile)
	if len(candidates) == 0 || !validUtility {
		return lowestMPDrepRateIndex
	}
	if len(candidates) == 1 {
		return candidates[0].index
	}

	params, ok := bolaParameters(candidates, float64(maxBufferSec), float64(segmentDurationMs)/1000.0)
	if !ok {
		return bolaLowestCandidate(candidates).index
	}

	bufferSec := math.Max(0, float64(bufferLevelMs)/1000.0)
	bufferChunks := bufferSec / (float64(segmentDurationMs) / 1000.0)
	chosen := candidates[0]
	bestObjective := math.Inf(-1)
	for _, candidate := range candidates {
		objective := bolaObjective(candidate, params, bufferChunks, float64(segmentDurationMs)/1000.0)
		if objective > bestObjective+bolaEpsilon ||
			(math.Abs(objective-bestObjective) <= bolaEpsilon && candidate.bandwidthBps > chosen.bandwidthBps) {
			bestObjective = objective
			chosen = candidate
		}
	}

	return chosen.index
}

type bolaParams struct {
	Vp float64
	gp float64
}

func bolaCandidates(bandwithList []int, representationSet []godashhttp.Representation, nextSegment int, segmentDurationSec float64) []bolaCandidate {
	candidates, _ := bolaCandidatesWithUtility(bandwithList, representationSet, nextSegment, segmentDurationSec, nil)
	return candidates
}

func bolaCandidatesWithUtility(bandwithList []int, representationSet []godashhttp.Representation, nextSegment int, segmentDurationSec float64, ssimProfile *BOLASSIMProfile) ([]bolaCandidate, bool) {
	chunksByBandwidth := make(map[int][]string, len(representationSet))
	for _, representation := range representationSet {
		if representation.BandWidth <= 0 {
			continue
		}
		chunksByBandwidth[representation.BandWidth] = append(chunksByBandwidth[representation.BandWidth], representation.Chunks)
	}

	usedByBandwidth := make(map[int]int, len(chunksByBandwidth))
	candidates := make([]bolaCandidate, 0, len(bandwithList))
	minBandwidth := bolaMinBandwidth(bandwithList)
	for index, bandwidth := range bandwithList {
		if bandwidth <= 0 {
			continue
		}

		chunkLists := chunksByBandwidth[bandwidth]
		used := usedByBandwidth[bandwidth]
		chunks := ""
		if used < len(chunkLists) {
			chunks = chunkLists[used]
			usedByBandwidth[bandwidth] = used + 1
		}

		avgSize := bolaAverageChunkBits(chunks)
		if avgSize <= 0 {
			avgSize = float64(bandwidth) * segmentDurationSec
		}
		nextSize := avgSize
		if chunkBits, ok := robustMPCChunkBits(chunks, nextSegment); ok && chunkBits > 0 {
			nextSize = float64(chunkBits)
		}

		utility := math.Log(float64(bandwidth) / float64(minBandwidth))
		paramUtility := utility
		if ssimProfile != nil {
			representation, ok := ssimProfile.representations[bandwidth]
			if !ok || nextSegment <= 0 || nextSegment > len(representation.SSIM) {
				return nil, false
			}
			utility = bolaSSIMDB(representation.SSIM[nextSegment-1])
			paramUtility = bolaSSIMDB(bolaMean(representation.SSIM))
		}

		candidates = append(candidates, bolaCandidate{
			index:        index,
			bandwidthBps: bandwidth,
			chunks:       chunks,
			avgSizeBits:  avgSize,
			nextSizeBits: nextSize,
			utility:      utility,
			paramUtility: paramUtility,
		})
	}

	return candidates, len(candidates) > 0
}

func bolaParameters(candidates []bolaCandidate, maxBufferSec float64, segmentDurationSec float64) (bolaParams, bool) {
	ordered := append([]bolaCandidate(nil), candidates...)
	sortBolaCandidatesByAverageSize(ordered)
	smallest := ordered[0]
	secondSmallest := ordered[1]
	largest := ordered[len(ordered)-1]

	sizeDelta := secondSmallest.avgSizeBits - smallest.avgSizeBits
	if sizeDelta <= 0 || largest.paramUtility <= 0 {
		return bolaParams{}, false
	}

	minBufferSec := math.Max(segmentDurationSec, bolaDefaultMinBufferSec)
	if minBufferSec >= maxBufferSec {
		minBufferSec = math.Max(segmentDurationSec, maxBufferSec-segmentDurationSec)
	}
	if minBufferSec <= 0 || minBufferSec >= maxBufferSec {
		return bolaParams{}, false
	}

	numerator := maxBufferSec*(secondSmallest.avgSizeBits*smallest.paramUtility-smallest.avgSizeBits*secondSmallest.paramUtility) -
		largest.paramUtility*minBufferSec*sizeDelta
	denominator := (minBufferSec - maxBufferSec) * sizeDelta
	gp := numerator / denominator
	if largest.paramUtility+gp <= 0 {
		return bolaParams{}, false
	}

	return bolaParams{
		Vp: maxBufferSec / (largest.paramUtility + gp),
		gp: gp,
	}, true
}

func bolaSSIMDB(ssim float64) float64 {
	if ssim >= 1 {
		return bolaSSIMDBMax
	}
	value := -10 * math.Log10(1-ssim)
	return math.Max(bolaSSIMDBMin, math.Min(bolaSSIMDBMax, value))
}

func bolaMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func bolaObjective(candidate bolaCandidate, params bolaParams, bufferChunks float64, segmentDurationSec float64) float64 {
	if candidate.nextSizeBits <= 0 || segmentDurationSec <= 0 {
		return math.Inf(-1)
	}
	V := params.Vp / segmentDurationSec
	return (V*(candidate.utility+params.gp) - bufferChunks) / candidate.nextSizeBits
}

func bolaLowestCandidate(candidates []bolaCandidate) bolaCandidate {
	lowest := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.bandwidthBps < lowest.bandwidthBps {
			lowest = candidate
		}
	}
	return lowest
}

func bolaMinBandwidth(bandwithList []int) int {
	minBandwidth := 0
	for _, bandwidth := range bandwithList {
		if bandwidth <= 0 {
			continue
		}
		if minBandwidth == 0 || bandwidth < minBandwidth {
			minBandwidth = bandwidth
		}
	}
	if minBandwidth <= 0 {
		return 1
	}
	return minBandwidth
}

func bolaAverageChunkBits(chunks string) float64 {
	if strings.TrimSpace(chunks) == "" {
		return 0
	}
	parts := strings.Split(chunks, ",")
	sum := 0
	count := 0
	for _, part := range parts {
		chunkBits, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || chunkBits <= 0 {
			continue
		}
		sum += chunkBits
		count++
	}
	if count == 0 {
		return 0
	}
	return float64(sum) / float64(count)
}

func sortBolaCandidatesByAverageSize(candidates []bolaCandidate) {
	for i := 1; i < len(candidates); i++ {
		current := candidates[i]
		j := i - 1
		for j >= 0 && candidates[j].avgSizeBits > current.avgSizeBits {
			candidates[j+1] = candidates[j]
			j--
		}
		candidates[j+1] = current
	}
}
