package algorithms

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestPensieveExternalMapsHigh6LadderByBandwidth(t *testing.T) {
	// GoDASH uses representation indices from the MPD order. The Pensieve
	// service uses 0..5 quality indices in ascending bitrate order.
	bandwidths := []int{39549933, 24728255, 14817559, 4284105, 3834548, 3014681}
	ascending := sortedRepIndicesAscending(bandwidths)
	want := []int{5, 4, 3, 2, 1, 0}
	if !reflect.DeepEqual(ascending, want) {
		t.Fatalf("ascending mapping = %#v, want %#v", ascending, want)
	}

	quality, err := localRepToServiceQuality(ascending, 0)
	if err != nil {
		t.Fatalf("localRepToServiceQuality highest: %v", err)
	}
	if quality != 5 {
		t.Fatalf("highest local rep mapped to Pensieve quality %d, want 5", quality)
	}

	quality, err = localRepToServiceQuality(ascending, 5)
	if err != nil {
		t.Fatalf("localRepToServiceQuality lowest: %v", err)
	}
	if quality != 0 {
		t.Fatalf("lowest local rep mapped to Pensieve quality %d, want 0", quality)
	}
}

func TestPensieveExternalSelectBitrateAcceptsHigh6Ladder(t *testing.T) {
	bandwidths := []int{39549933, 24728255, 14817559, 4284105, 3834548, 3014681}
	var gotPayload pensievePredictionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metadata" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == pensieveOfficialResetPath {
			w.WriteHeader(http.StatusOK)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte("0"))
	}))
	defer server.Close()

	client := NewPensieveExternalClient(server.URL)
	if err := client.ValidateLadder(bandwidths); err != nil {
		t.Fatalf("ValidateLadder rejected legacy high6 service: %v", err)
	}
	selected, err := client.SelectBitrate(bandwidths, 0, 4500, 0, 1234, 321, 7)
	if err != nil {
		t.Fatalf("SelectBitrate rejected high6 ladder: %v", err)
	}
	if selected != 5 {
		t.Fatalf("service quality 0 selected local rep %d, want lowest local rep 5", selected)
	}
	if gotPayload.LastQuality != 5 {
		t.Fatalf("request LastQuality = %d, want 5 for current highest local rep", gotPayload.LastQuality)
	}
	if gotPayload.Buffer != 4.5 {
		t.Fatalf("request Buffer = %v, want 4.5", gotPayload.Buffer)
	}
}

func TestPensieveExternalValidatesUniform10Metadata(t *testing.T) {
	bandwidths := []int{
		40000000,
		35889000,
		31778000,
		27667000,
		23556000,
		19444000,
		15333000,
		11222000,
		7111000,
		3000000,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metadata":
			_ = json.NewEncoder(w).Encode(pensieveServiceMetadata{
				ActionDim:            10,
				BitratesBpsAscending: []int{3000000, 7111000, 11222000, 15333000, 19444000, 23556000, 27667000, 31778000, 35889000, 40000000},
				StateHistoryLen:      10,
				TotalVideoChunks:     70,
			})
		case pensieveOfficialResetPath:
			w.WriteHeader(http.StatusOK)
		default:
			_, _ = w.Write([]byte("9"))
		}
	}))
	defer server.Close()

	client := NewPensieveExternalClient(server.URL)
	if err := client.ValidateLadder(bandwidths); err != nil {
		t.Fatalf("ValidateLadder rejected uniform10 metadata: %v", err)
	}
	selected, err := client.SelectBitrate(bandwidths, 9, 5000, 0, 1000, 200, 1)
	if err != nil {
		t.Fatalf("SelectBitrate rejected uniform10 ladder: %v", err)
	}
	if selected != 0 {
		t.Fatalf("service quality 9 selected local rep %d, want highest local rep 0", selected)
	}
}

func TestPensieveExternalRejectsNonSixLadderWithoutMetadata(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := NewPensieveExternalClient(server.URL)
	err := client.ValidateLadder([]int{1000, 2000, 3000, 4000, 5000})
	if err == nil {
		t.Fatal("ValidateLadder accepted a non-six-action service without metadata")
	}
}

func TestPensieveExternalRejectsMetadataBitrateMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(pensieveServiceMetadata{
			ActionDim:            3,
			BitratesBpsAscending: []int{1000, 2000, 9999},
			StateHistoryLen:      8,
		})
	}))
	defer server.Close()
	client := NewPensieveExternalClient(server.URL)
	if err := client.ValidateLadder([]int{3000, 2000, 1000}); err == nil {
		t.Fatal("ValidateLadder accepted mismatched service bitrates")
	}
}
