package algorithms

import (
	"math"
	"testing"
)

func TestBOLASelectsLowestAtLowBuffer(t *testing.T) {
	mpd := robustMPCTestMPD([]int{300000, 750000, 1200000}, []string{
		"600000,600000,600000,600000,600000,600000",
		"1500000,1500000,1500000,1500000,1500000,1500000",
		"2400000,2400000,2400000,2400000,2400000,2400000",
	})

	selected := BOLA(0, 20, 2000, 1, mpd, 0, []int{300000, 750000, 1200000}, 0)
	if selected != 0 {
		t.Fatalf("expected lowest representation at empty buffer, got %d", selected)
	}
}

func TestBOLASelectsHigherRepresentationAtHighBuffer(t *testing.T) {
	mpd := robustMPCTestMPD([]int{300000, 750000, 1200000}, []string{
		"600000,600000,600000,600000,600000,600000",
		"1500000,1500000,1500000,1500000,1500000,1500000",
		"2400000,2400000,2400000,2400000,2400000,2400000",
	})

	selected := BOLA(20000, 20, 2000, 1, mpd, 0, []int{300000, 750000, 1200000}, 0)
	if selected != 2 {
		t.Fatalf("expected highest representation at full buffer, got %d", selected)
	}
}

func TestBOLACandidatesBindChunksByBandwidth(t *testing.T) {
	representationSet := robustMPCTestMPD([]int{300000, 750000, 1200000}, []string{
		"600000",
		"1500000",
		"2400000",
	}).Periods[0].AdaptationSet[0].Representation

	candidates := bolaCandidates([]int{1200000, 300000, 750000}, representationSet, 1, 2.0)
	if len(candidates) != 3 {
		t.Fatalf("expected three candidates, got %d", len(candidates))
	}

	byIndex := map[int]bolaCandidate{}
	for _, candidate := range candidates {
		byIndex[candidate.index] = candidate
	}
	if byIndex[0].chunks != "2400000" || byIndex[1].chunks != "600000" || byIndex[2].chunks != "1500000" {
		t.Fatalf("candidate chunks were not matched by bandwidth: %#v", candidates)
	}
}

func TestBOLASSIMDBMatchesPufferDefinition(t *testing.T) {
	tests := []struct {
		ssim float64
		want float64
	}{
		{ssim: 0.9, want: 10},
		{ssim: 0.99, want: 20},
		{ssim: 1, want: 60},
		{ssim: -0.5, want: 0},
	}
	for _, test := range tests {
		if got := bolaSSIMDB(test.ssim); math.Abs(got-test.want) > 1e-9 {
			t.Fatalf("bolaSSIMDB(%v) = %v, want %v", test.ssim, got, test.want)
		}
	}
}

func TestBOLASSIMProfileValidationAndSelection(t *testing.T) {
	profile := testBOLASSIMProfile(t)
	mpd := robustMPCTestMPD([]int{300000, 750000, 1200000}, []string{
		"600000,600000,600000,600000,600000,600000",
		"1500000,1500000,1500000,1500000,1500000,1500000",
		"2400000,2400000,2400000,2400000,2400000,2400000",
	})
	if err := profile.ValidateMPD(mpd); err != nil {
		t.Fatalf("valid SSIM profile rejected: %v", err)
	}

	selected := BOLASSIM(20000, 20, 2000, 1, mpd, 0, []int{300000, 750000, 1200000}, 0, profile)
	if selected != 2 {
		t.Fatalf("expected highest SSIM representation at full buffer, got %d", selected)
	}
}

func TestBOLASSIMUsesPerSegmentUtility(t *testing.T) {
	profile := testBOLASSIMProfile(t)
	representationSet := robustMPCTestMPD([]int{300000, 750000, 1200000}, []string{
		"600000,600000,600000,600000,600000,600000",
		"1500000,1500000,1500000,1500000,1500000,1500000",
		"2400000,2400000,2400000,2400000,2400000,2400000",
	}).Periods[0].AdaptationSet[0].Representation

	first, ok := bolaCandidatesWithUtility([]int{300000, 750000, 1200000}, representationSet, 1, 2, profile)
	if !ok {
		t.Fatal("failed to construct first-segment SSIM candidates")
	}
	second, ok := bolaCandidatesWithUtility([]int{300000, 750000, 1200000}, representationSet, 2, 2, profile)
	if !ok {
		t.Fatal("failed to construct second-segment SSIM candidates")
	}
	if first[1].utility == second[1].utility {
		t.Fatal("per-segment SSIM utility did not change between segments")
	}
	if first[1].paramUtility != second[1].paramUtility {
		t.Fatal("static parameter utility changed between segments")
	}
}

func TestBOLAObjectiveMatchesPufferReferenceFormula(t *testing.T) {
	params := bolaParams{Vp: 4, gp: 1}
	candidate := bolaCandidate{
		nextSizeBits: 2000000,
		utility:      2,
	}
	got := bolaObjective(candidate, params, 3, 2)
	want := ((params.Vp/2)*(candidate.utility+params.gp) - 3) / candidate.nextSizeBits
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("objective = %v, want %v", got, want)
	}
}

func TestBOLAParametersUseStaticParameterUtility(t *testing.T) {
	candidates := []bolaCandidate{
		{avgSizeBits: 1000, utility: 0, paramUtility: 1},
		{avgSizeBits: 2000, utility: 0, paramUtility: 2},
		{avgSizeBits: 3000, utility: 0, paramUtility: 3},
	}
	params, ok := bolaParameters(candidates, 15, 2)
	if !ok {
		t.Fatal("expected valid BOLA parameters from static utility ladder")
	}
	if params.Vp <= 0 {
		t.Fatalf("Vp = %v, want positive", params.Vp)
	}
}

func TestBOLASSIMProfileRejectsNonMonotonicLadder(t *testing.T) {
	_, err := ParseBOLASSIMProfile([]byte(`{
		"schema_version": 1,
		"utility": "ssim-db",
		"representations": [
			{"bandwidth_bps": 300000, "ssim": [0.90, 0.91]},
			{"bandwidth_bps": 750000, "ssim": [0.89, 0.95]}
		]
	}`))
	if err == nil {
		t.Fatal("non-monotonic SSIM ladder was accepted")
	}
}

func testBOLASSIMProfile(t *testing.T) *BOLASSIMProfile {
	t.Helper()
	profile, err := ParseBOLASSIMProfile([]byte(`{
		"schema_version": 1,
		"utility": "ssim-db",
		"reference": "unit-test-source",
		"representations": [
			{"bandwidth_bps": 300000, "ssim": [0.90, 0.91, 0.90, 0.91, 0.90, 0.91]},
			{"bandwidth_bps": 750000, "ssim": [0.95, 0.96, 0.95, 0.96, 0.95, 0.96]},
			{"bandwidth_bps": 1200000, "ssim": [0.99, 0.995, 0.99, 0.995, 0.99, 0.995]}
		]
	}`))
	if err != nil {
		t.Fatalf("parse test SSIM profile: %v", err)
	}
	return profile
}
