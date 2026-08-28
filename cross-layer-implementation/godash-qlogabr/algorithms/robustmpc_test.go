package algorithms

import (
	"math"
	"testing"

	godashhttp "github.com/uccmisl/godash/http"
)

func TestRobustMPCSelectsValidRepresentationAndTracksState(t *testing.T) {
	mpd := robustMPCTestMPD([]int{300000, 750000, 1200000}, []string{
		"600000,600000,600000,600000,600000,600000",
		"1500000,1500000,1500000,1500000,1500000,1500000",
		"2400000,2400000,2400000,2400000,2400000,2400000",
	})
	state := RobustMPCState{}

	selected := RobustMPC(&state, 2500000, 0, 4000, 2000, 1, mpd, 0, []int{300000, 750000, 1200000}, 0)
	if selected < 0 || selected > 2 {
		t.Fatalf("selected representation out of range: %d", selected)
	}
	if len(state.PastThroughputsBps) != 1 {
		t.Fatalf("expected one throughput sample, got %d", len(state.PastThroughputsBps))
	}
	if len(state.PastBandwidthEstimates) != 1 {
		t.Fatalf("expected one bandwidth estimate, got %d", len(state.PastBandwidthEstimates))
	}
	if len(state.PastErrors) != 1 || state.PastErrors[0] != 0 {
		t.Fatalf("expected initial zero prediction error, got %#v", state.PastErrors)
	}

	RobustMPC(&state, 1800000, selected, 4000, 2000, 2, mpd, 0, []int{300000, 750000, 1200000}, 0)
	if len(state.PastErrors) != 2 {
		t.Fatalf("expected two prediction errors after second sample, got %d", len(state.PastErrors))
	}
}

func TestRobustMPCMatchesChunksByBandwidthNotSliceIndex(t *testing.T) {
	mpd := robustMPCTestMPD([]int{300000, 750000, 1200000}, []string{
		"600000,600000,600000,600000,600000,600000",
		"1500000,1500000,1500000,1500000,1500000,1500000",
		"40000000,40000000,40000000,40000000,40000000,40000000",
	})
	state := RobustMPCState{}

	selected := RobustMPC(&state, 1500000, 2, 2000, 2000, 1, mpd, 0, []int{1200000, 750000, 300000}, 2)
	if selected == 0 {
		t.Fatalf("selected highest representation using mismatched low-bitrate chunk sizes")
	}
}

func TestRobustMPCCandidatesBindChunksByBandwidth(t *testing.T) {
	representationSet := robustMPCTestMPD([]int{300000, 750000, 1200000}, []string{
		"low",
		"mid",
		"high",
	}).Periods[0].AdaptationSet[0].Representation

	candidates := robustMPCCandidates([]int{1200000, 300000, 750000}, representationSet)
	if len(candidates) != 3 {
		t.Fatalf("expected three candidates, got %d", len(candidates))
	}

	byIndex := map[int]robustMPCCandidate{}
	for _, candidate := range candidates {
		byIndex[candidate.index] = candidate
	}
	if byIndex[0].chunks != "high" || byIndex[1].chunks != "low" || byIndex[2].chunks != "mid" {
		t.Fatalf("candidate chunks were not matched by bandwidth: %#v", candidates)
	}
}

func TestRobustMPCFallsBackWithoutChunkMetadata(t *testing.T) {
	mpd := robustMPCTestMPD([]int{300000, 750000}, []string{"", ""})
	state := RobustMPCState{}

	selected := RobustMPC(&state, 800000, 0, 4000, 2000, 1, mpd, 0, []int{300000, 750000}, 0)
	if selected != SelectRepRateWithThroughtput(800000, []int{300000, 750000}, 0) {
		t.Fatalf("fallback selected %d", selected)
	}
}

func TestRobustMPCPredictionMatchesPiTreeHarmonicMaxError(t *testing.T) {
	state := RobustMPCState{}

	first := state.updateBandwidthPrediction(4000000)
	if math.Abs(first-4000000) > 1e-6 {
		t.Fatalf("first prediction = %v, want 4000000", first)
	}

	second := state.updateBandwidthPrediction(2000000)
	wantHarmonic := 2.0 / (1.0/4000000.0 + 1.0/2000000.0)
	wantPrediction := wantHarmonic / 2.0
	if math.Abs(second-wantPrediction) > 1e-6 {
		t.Fatalf("second prediction = %v, want %v", second, wantPrediction)
	}
	if math.Abs(state.PastBandwidthEstimates[1]-wantHarmonic) > 1e-6 {
		t.Fatalf("stored harmonic estimate = %v, want %v", state.PastBandwidthEstimates[1], wantHarmonic)
	}
	if math.Abs(state.PastErrors[1]-1.0) > 1e-9 {
		t.Fatalf("prediction error = %v, want 1", state.PastErrors[1])
	}
}

func TestRobustMPCAvoidsOversizedChunkWithLowPrediction(t *testing.T) {
	mpd := robustMPCTestMPD([]int{1000000, 4000000}, []string{
		"2000000,2000000,2000000,2000000,2000000,2000000",
		"8000000,8000000,8000000,8000000,8000000,8000000",
	})
	state := RobustMPCState{}

	selected := RobustMPC(&state, 1200000, 0, 0, 2000, 1, mpd, 0, []int{1000000, 4000000}, 0)
	if selected != 0 {
		t.Fatalf("selected %d, want lowest representation for oversized chunks at low prediction", selected)
	}
}

func robustMPCTestMPD(bandwidths []int, chunks []string) godashhttp.MPD {
	representations := make([]godashhttp.Representation, len(bandwidths))
	for i := range bandwidths {
		representations[i] = godashhttp.Representation{
			BandWidth: bandwidths[i],
			Chunks:    chunks[i],
		}
	}
	return godashhttp.MPD{
		Periods: []godashhttp.Period{
			{
				AdaptationSet: []godashhttp.AdaptationSet{
					{Representation: representations},
				},
			},
		},
	}
}
