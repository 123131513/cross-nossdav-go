/*
 *	goDASH, golang client emulator for DASH video streaming
 *	Copyright (c) 2019, Jason Quinlan, Darijo Raca, University College Cork
 *											[j.quinlan,d.raca]@cs.ucc.ie)
 *                      Maëlle Manifacier, MISL Summer of Code 2019, UCC
 *	This program is free software; you can redistribute it and/or
 *	modify it under the terms of the GNU General Public License
 *	as published by the Free Software Foundation; either version 2
 *	of the License, or (at your option) any later version.
 *
 *	This program is distributed in the hope that it will be useful,
 *	but WITHOUT ANY WARRANTY; without even the implied warranty of
 *	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *	GNU General Public License for more details.
 *
 *	You should have received a copy of the GNU General Public License
 *	along with this program; if not, write to the Free Software
 *	Foundation, Inc., 51 Franklin Street, Fifth Floor, Boston, MA
 *	02110-1301, USA.
 */

package algorithms

import (
	"math"
	"sort"
	"strconv"
	"strings"

	godashhttp "github.com/uccmisl/godash/http"
)

const (
	robustMPCFutureChunkCount = 5
	robustMPCRebufPenalty     = 4.3
	robustMPCSmoothPenalty    = 1.0
	robustMPCEpsilon          = 1e-9
)

type robustMPCCandidate struct {
	index        int
	bandwidthBps int
	chunks       string
}

// RobustMPCState keeps the prediction state required by RobustMPC across segments.
type RobustMPCState struct {
	PastErrors             []float64
	PastBandwidthEstimates []float64
	PastThroughputsBps     []float64
}

// Reset clears per-stream RobustMPC state.
func (state *RobustMPCState) Reset() {
	state.PastErrors = nil
	state.PastBandwidthEstimates = nil
	state.PastThroughputsBps = nil
}

// RobustMPC ports the RobustMPC ABR decision from PiTree into GoDASH.
func RobustMPC(
	state *RobustMPCState,
	newThr int,
	currentRepRate int,
	bufferLevelMs int,
	segmentDurationMs int,
	segmentNumber int,
	mpd godashhttp.MPD,
	currentMPDRepAdaptSet int,
	bandwithList []int,
	lowestMPDrepRateIndex int,
) int {
	if state == nil {
		state = &RobustMPCState{}
	}
	if newThr <= 0 || len(bandwithList) == 0 || segmentDurationMs <= 0 {
		return robustMPCFallback(newThr, bandwithList, lowestMPDrepRateIndex, currentRepRate)
	}
	if currentMPDRepAdaptSet < 0 || len(mpd.Periods) == 0 || currentMPDRepAdaptSet >= len(mpd.Periods[0].AdaptationSet) {
		return robustMPCFallback(newThr, bandwithList, lowestMPDrepRateIndex, currentRepRate)
	}

	representationSet := mpd.Periods[0].AdaptationSet[currentMPDRepAdaptSet].Representation
	candidates := robustMPCCandidates(bandwithList, representationSet)
	if len(candidates) == 0 {
		return robustMPCFallback(newThr, bandwithList, lowestMPDrepRateIndex, currentRepRate)
	}

	futureLen := robustMPCFutureLengthFromCandidates(candidates, segmentNumber)
	if futureLen <= 0 {
		return currentRepRate
	}

	futureBandwidthBps := state.updateBandwidthPrediction(float64(newThr))
	if futureBandwidthBps <= 0 {
		return robustMPCFallback(newThr, bandwithList, lowestMPDrepRateIndex, currentRepRate)
	}

	bestReward := math.Inf(-1)
	bestFirstIndex := currentRepRate
	currentBufferSec := float64(bufferLevelMs) / 1000.0
	segmentDurationSec := float64(segmentDurationMs) / 1000.0
	currentBandwidthBps := robustMPCBandwidthAtIndex(bandwithList, currentRepRate)
	choice := make([]robustMPCCandidate, futureLen)

	var evaluate func(int)
	evaluate = func(pos int) {
		if pos == futureLen {
			reward := robustMPCReward(choice, segmentNumber+1, currentBufferSec, segmentDurationSec, futureBandwidthBps, currentBandwidthBps)
			if reward > bestReward+robustMPCEpsilon ||
				(math.Abs(reward-bestReward) <= robustMPCEpsilon && choice[0].bandwidthBps > robustMPCBandwidthAtIndex(bandwithList, bestFirstIndex)) {
				bestReward = reward
				bestFirstIndex = choice[0].index
			}
			return
		}
		for _, candidate := range candidates {
			choice[pos] = candidate
			evaluate(pos + 1)
		}
	}
	evaluate(0)

	if bestFirstIndex < 0 || bestFirstIndex >= len(bandwithList) {
		return robustMPCFallback(newThr, bandwithList, lowestMPDrepRateIndex, currentRepRate)
	}
	return bestFirstIndex
}

func (state *RobustMPCState) updateBandwidthPrediction(newThrBps float64) float64 {
	currError := 0.0
	if len(state.PastBandwidthEstimates) > 0 {
		lastEstimate := state.PastBandwidthEstimates[len(state.PastBandwidthEstimates)-1]
		currError = math.Abs(lastEstimate-newThrBps) / newThrBps
	}
	state.PastErrors = append(state.PastErrors, currError)
	state.PastThroughputsBps = append(state.PastThroughputsBps, newThrBps)

	recentThroughputs := robustMPCRecent(state.PastThroughputsBps, robustMPCFutureChunkCount)
	harmonicBandwidth := robustMPCHarmonicMean(recentThroughputs)
	if harmonicBandwidth <= 0 {
		return 0
	}
	state.PastBandwidthEstimates = append(state.PastBandwidthEstimates, harmonicBandwidth)

	recentErrors := robustMPCRecent(state.PastErrors, robustMPCFutureChunkCount)
	maxError := 0.0
	for _, err := range recentErrors {
		if err > maxError {
			maxError = err
		}
	}

	return harmonicBandwidth / (1 + maxError)
}

func robustMPCReward(choice []robustMPCCandidate, firstFutureSegment int, bufferSec float64, segmentDurationSec float64, futureBandwidthBps float64, lastBandwidthBps int) float64 {
	bitrateReward := 0.0
	rebufferSec := 0.0
	smoothnessPenalty := 0.0
	currentBufferSec := bufferSec
	previousBandwidthBps := lastBandwidthBps

	for pos, candidate := range choice {
		chunkBits, ok := robustMPCChunkBits(candidate.chunks, firstFutureSegment+pos)
		if !ok || chunkBits <= 0 {
			return math.Inf(-1)
		}

		downloadTimeSec := float64(chunkBits) / futureBandwidthBps
		if currentBufferSec < downloadTimeSec {
			rebufferSec += downloadTimeSec - currentBufferSec
			currentBufferSec = 0
		} else {
			currentBufferSec -= downloadTimeSec
		}
		currentBufferSec += segmentDurationSec

		bitrateReward += float64(candidate.bandwidthBps) / 1000000.0
		smoothnessPenalty += math.Abs(float64(candidate.bandwidthBps-previousBandwidthBps)) / 1000000.0
		previousBandwidthBps = candidate.bandwidthBps
	}

	return bitrateReward - robustMPCRebufPenalty*rebufferSec - robustMPCSmoothPenalty*smoothnessPenalty
}

func robustMPCCandidates(bandwithList []int, representationSet []godashhttp.Representation) []robustMPCCandidate {
	chunksByBandwidth := make(map[int][]string, len(representationSet))
	for _, representation := range representationSet {
		if representation.BandWidth <= 0 || representation.Chunks == "" {
			continue
		}
		chunksByBandwidth[representation.BandWidth] = append(chunksByBandwidth[representation.BandWidth], representation.Chunks)
	}

	usedByBandwidth := make(map[int]int, len(chunksByBandwidth))
	candidates := make([]robustMPCCandidate, 0, len(bandwithList))
	for i, bandwidth := range bandwithList {
		if bandwidth <= 0 {
			continue
		}
		chunkLists := chunksByBandwidth[bandwidth]
		used := usedByBandwidth[bandwidth]
		if used >= len(chunkLists) {
			continue
		}
		usedByBandwidth[bandwidth] = used + 1
		candidates = append(candidates, robustMPCCandidate{
			index:        i,
			bandwidthBps: bandwidth,
			chunks:       chunkLists[used],
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].bandwidthBps == candidates[j].bandwidthBps {
			return candidates[i].index < candidates[j].index
		}
		return candidates[i].bandwidthBps < candidates[j].bandwidthBps
	})
	return candidates
}

func robustMPCFutureLength(representationSet []godashhttp.Representation, segmentNumber int) int {
	maxChunks := 0
	for _, representation := range representationSet {
		chunkCount := robustMPCChunkCount(representation.Chunks)
		if chunkCount > maxChunks {
			maxChunks = chunkCount
		}
	}
	remainingChunks := maxChunks - segmentNumber
	if remainingChunks < robustMPCFutureChunkCount {
		return remainingChunks
	}
	return robustMPCFutureChunkCount
}

func robustMPCFutureLengthFromCandidates(candidates []robustMPCCandidate, segmentNumber int) int {
	maxChunks := 0
	for _, candidate := range candidates {
		chunkCount := robustMPCChunkCount(candidate.chunks)
		if chunkCount > maxChunks {
			maxChunks = chunkCount
		}
	}
	remainingChunks := maxChunks - segmentNumber
	if remainingChunks < robustMPCFutureChunkCount {
		return remainingChunks
	}
	return robustMPCFutureChunkCount
}

func robustMPCChunkCount(chunkList string) int {
	if strings.TrimSpace(chunkList) == "" {
		return 0
	}
	return len(strings.Split(chunkList, ","))
}

func robustMPCChunkBits(chunkList string, pos int) (int, bool) {
	if strings.TrimSpace(chunkList) == "" {
		return 0, false
	}
	chunks := strings.Split(chunkList, ",")
	pos--
	if pos < 0 || pos >= len(chunks) {
		return 0, false
	}
	chunkSize, err := strconv.Atoi(strings.TrimSpace(chunks[pos]))
	if err != nil {
		return 0, false
	}
	return chunkSize, true
}

func robustMPCHarmonicMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	denominator := 0.0
	for _, value := range values {
		if value <= 0 {
			return 0
		}
		denominator += 1.0 / value
	}
	return float64(len(values)) / denominator
}

func robustMPCRecent(values []float64, window int) []float64 {
	if len(values) <= window {
		return values
	}
	return values[len(values)-window:]
}

func robustMPCFallback(newThr int, bandwithList []int, lowestMPDrepRateIndex int, currentRepRate int) int {
	if len(bandwithList) == 0 {
		return currentRepRate
	}
	return SelectRepRateWithThroughtput(newThr, bandwithList, lowestMPDrepRateIndex)
}

func robustMPCBandwidthAtIndex(bandwithList []int, index int) int {
	if index < 0 || index >= len(bandwithList) {
		return 0
	}
	return bandwithList[index]
}
