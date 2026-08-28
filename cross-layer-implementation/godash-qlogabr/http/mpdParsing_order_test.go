package http

import "testing"

func TestFileParserSortsRepresentationsDescendingByBandwidth(t *testing.T) {
	mpd := fileParser([]byte(`<?xml version="1.0" encoding="utf-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT6S" maxSegmentDuration="PT2S">
  <Period duration="PT6S">
    <AdaptationSet>
      <Representation id="low" bandwidth="1000" height="720" codecs="avc3">
        <SegmentTemplate duration="2" />
      </Representation>
      <Representation id="high" bandwidth="3000" height="720" codecs="avc3">
        <SegmentTemplate duration="2" />
      </Representation>
      <Representation id="mid" bandwidth="2000" height="720" codecs="avc3">
        <SegmentTemplate duration="2" />
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`))
	if len(mpd.Periods) != 1 || len(mpd.Periods[0].AdaptationSet) != 1 {
		t.Fatalf("unexpected MPD shape: %#v", mpd)
	}
	reps := mpd.Periods[0].AdaptationSet[0].Representation
	got := []int{reps[0].BandWidth, reps[1].BandWidth, reps[2].BandWidth}
	want := []int{3000, 2000, 1000}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("representation order = %v, want %v", got, want)
		}
	}
}
