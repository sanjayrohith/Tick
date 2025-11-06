package store

import (
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestNewQueueStatsPrePopulatesEveryStatus(t *testing.T) {
	stats := NewQueueStats("emails")

	if stats.Queue != "emails" {
		t.Errorf("Queue = %q, want emails", stats.Queue)
	}
	// A missing key and a zero count mean different things to a metrics
	// consumer, so every status must be present even at zero.
	for _, s := range domain.AllStatuses() {
		if _, ok := stats.Depth[s]; !ok {
			t.Errorf("status %q is absent from Depth; a gauge would go missing", s)
		}
	}
	if got, want := len(stats.Depth), len(domain.AllStatuses()); got != want {
		t.Errorf("Depth has %d entries, want %d", got, want)
	}
}
