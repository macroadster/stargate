package bitcoin

import (
	"testing"
	"time"
)

func TestSnapshotStatsCopiesMemory(t *testing.T) {
	bm := &BlockMonitor{
		isRunning:           true,
		currentHeight:       42,
		blocksProcessed:     7,
		totalInscriptions:   3,
		totalStegoContracts: 2,
		totalImages:         4,
		lastProcessTime:     time.Second,
	}
	st := bm.SnapshotStats()
	if !st.Running || st.CurrentHeight != 42 || st.BlocksProcessed != 7 ||
		st.TotalInscriptions != 3 || st.TotalStegoContracts != 2 ||
		st.TotalImages != 4 || st.LastProcess != time.Second {
		t.Fatalf("%+v", st)
	}
	if (&BlockMonitor{}).SnapshotStats().Running {
		t.Fatal("zero monitor reported running")
	}
	var nilBM *BlockMonitor
	if nilBM.SnapshotStats().CurrentHeight != 0 {
		t.Fatal("nil monitor")
	}
}
