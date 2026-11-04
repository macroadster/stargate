package services

import (
	"errors"
	"testing"

	"stargate-backend/core"
)

type fakeScanner struct {
	info core.ScannerInfo
	init bool
}

func (f fakeScanner) GetScannerInfo() core.ScannerInfo { return f.info }
func (f fakeScanner) IsInitialized() bool              { return f.init }

type fakeNode struct {
	up     bool
	url    string
	height int
	err    error
}

func (f fakeNode) TestConnection() bool         { return f.up }
func (f fakeNode) GetNodeURL() string           { return f.url }
func (f fakeNode) GetBlockHeight() (int, error) { return f.height, f.err }

func TestGetHealthStatusUnwiredStaysEmpty(t *testing.T) {
	h := NewHealthService().GetHealthStatus()
	if h.Status != "healthy" {
		t.Fatalf("status=%q", h.Status)
	}
	if h.Scanner.ModelLoaded || h.Scanner.ModelPath != "" || h.Bitcoin.NodeConnected || h.Bitcoin.BlockHeight != 0 {
		t.Fatalf("unwired health should stay empty: %+v %+v", h.Scanner, h.Bitcoin)
	}
	if h.Version != "1.0.0" || h.Timestamp == "" {
		t.Fatalf("version/timestamp: %+v", h)
	}
}

func TestGetHealthStatusReportsScannerAndNode(t *testing.T) {
	svc := NewHealthService()
	svc.SetScanner(fakeScanner{
		init: true,
		info: core.ScannerInfo{
			ModelLoaded:  true,
			ModelVersion: "trin-pkg-starlight",
			ModelPath:    "/data/models/starlight.gguf",
			Device:       "cpu",
		},
	})
	svc.SetNode(fakeNode{up: true, url: "btcd://testnet4", height: 156124})

	h := svc.GetHealthStatus()
	if h.Status != "healthy" {
		t.Fatalf("status=%q", h.Status)
	}
	if !h.Scanner.ModelLoaded || h.Scanner.ModelPath != "/data/models/starlight.gguf" || h.Scanner.ModelVersion != "trin-pkg-starlight" {
		t.Fatalf("scanner: %+v", h.Scanner)
	}
	if !h.Bitcoin.NodeConnected || h.Bitcoin.NodeURL != "btcd://testnet4" || h.Bitcoin.BlockHeight != 156124 {
		t.Fatalf("bitcoin: %+v", h.Bitcoin)
	}
}

func TestGetHealthStatusDegradesWhenNodeOrScannerIsDown(t *testing.T) {
	svc := NewHealthService()
	svc.SetScanner(fakeScanner{init: false})
	svc.SetNode(fakeNode{up: false, url: "btcd://testnet4", height: 99, err: errors.New("down")})

	h := svc.GetHealthStatus()
	if h.Status != "degraded" {
		t.Fatalf("status=%q", h.Status)
	}
	if h.Scanner.ModelLoaded || h.Bitcoin.NodeConnected || h.Bitcoin.BlockHeight != 0 {
		t.Fatalf("down health: scanner=%+v bitcoin=%+v", h.Scanner, h.Bitcoin)
	}
	if h.Bitcoin.NodeURL != "btcd://testnet4" {
		t.Fatalf("node url dropped: %+v", h.Bitcoin)
	}
}
