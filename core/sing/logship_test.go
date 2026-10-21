package sing

import (
	"context"
	"testing"
	"time"

	sblog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"

	"github.com/tavut846/FNode/common/logship"
)

type nopSender struct{}

func (nopSender) SendLogs(context.Context, []logship.Entry) (logship.Response, error) {
	return logship.Response{LogMode: "warn", FlushInterval: 10}, nil
}

// The sing-box logger level follows the panel's log mode but never drops below
// the configured one.
func TestSing_LogFloor(t *testing.T) {
	factory, err := sblog.New(sblog.Options{Context: context.Background(),
		Options: option.LogOptions{Level: "error", Output: "stderr"}})
	if err != nil {
		t.Fatal(err)
	}
	defer factory.Close()
	s := &Sing{logFactory: factory, baseLogVerbosity: verbError}

	s.setLogFloor(logship.Debug)
	if factory.Level() != sblog.LevelDebug {
		t.Errorf("debug mode: %v", factory.Level())
	}
	s.setLogFloor(logship.Warn)
	if factory.Level() != sblog.LevelWarn {
		t.Errorf("warn mode: %v", factory.Level())
	}
	s.setLogFloor(logship.Off)
	if factory.Level() != sblog.LevelError {
		t.Errorf("off restores the configured level: %v", factory.Level())
	}
	s.baseLogVerbosity = verbInfo // operator asked for info
	s.setLogFloor(logship.Error)
	if factory.Level() != sblog.LevelInfo {
		t.Errorf("the configured level is a floor: %v", factory.Level())
	}
}

// sing-box lines are attributed to the node by their inbound tag; probe
// rejections ship as debug.
func TestShipToPanel_AttributesNodeByInboundTag(t *testing.T) {
	prev := logship.Default
	hub := logship.NewHub()
	logship.Default = hub
	defer func() { logship.Default = prev }()

	s := logship.NewShipper(nopSender{})
	hub.Register(s)
	hub.MapNode("[https://p]-vless:493", 12, s)
	s.SetMode(0, logship.Debug)

	line, _ := parseLogLine("-0700 2026-10-19 10:00:00 ERROR [1 2ms] inbound/vless[[https://p]-vless:493]: process connection from 52.36.5.236:40000: TLS handshake: REALITY: processed invalid connection")
	shipToPanel(line, true)
	warn, _ := parseLogLine("-0700 2026-10-19 10:00:00 WARN outbound/socks[us]: dial failed")
	shipToPanel(warn, false)
	time.Sleep(10 * time.Millisecond)

	if q, _ := s.Buffered(); q != 2 {
		t.Fatalf("queued %d", q)
	}
}
