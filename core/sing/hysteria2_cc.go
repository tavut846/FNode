package sing

import (
	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/conf"
)

// defaultMaxBrutalMbps caps the Brutal sender when the panel's up_mbps is 0.
const defaultMaxBrutalMbps = 100

// hy2Bandwidth is what a Hysteria2 inbound gets for its bandwidth options.
type hy2Bandwidth struct {
	UpMbps   int  // server -> client cap on the Brutal rate (sing-box SendBPS)
	DownMbps int  // client -> server rate announced to clients (ReceiveBPS)
	Ignore   bool // IgnoreClientBandwidth
	// Reason says why UpMbps differs from the panel's value, "" when it does not.
	Reason string
}

// resolveHysteria2Bandwidth decides the congestion control inputs.
//
// sing-quic picks the congestion control per client: when the client's auth
// request declares a receive rate (Rx > 0) the server sends with Brutal, a
// fixed rate that ignores loss, at min(Rx, UpMbps) and with no limit at all
// when UpMbps is 0; otherwise (or with IgnoreClientBandwidth and DownMbps 0)
// it uses BBR. Xboard sends only up_mbps and down_mbps (0 when the dashboard
// field is empty) and never ignore_client_bandwidth, so an unlimited panel
// plus a client declaring a rate far above its real link made the VPS blast
// UDP at that rate (lots of loss and resends, which providers flag as a flood).
//
//   - Hysteria2Config.IgnoreClientBandwidth (local) turns Brutal off for every
//     client: IgnoreClientBandwidth with DownMbps 0 (with DownMbps > 0 a client
//     that declares no rate would be rejected).
//   - Otherwise a panel up_mbps of 0 is capped at MaxBrutalMbps (default 100;
//     0 or -1 = unlimited), and a configured MaxBrutalMbps also lowers a larger
//     panel value.
func resolveHysteria2Bandwidth(h *panel.Hysteria2Node, cfg *conf.Hysteria2Config) hy2Bandwidth {
	bw := hy2Bandwidth{UpMbps: h.UpMbps, DownMbps: h.DownMbps, Ignore: h.Ignore_Client_Bandwidth}
	if cfg != nil && cfg.IgnoreClientBandwidth {
		bw.Ignore = true
		bw.DownMbps = 0
		bw.Reason = "IgnoreClientBandwidth: BBR for every client"
		return bw
	}
	limit, explicit := defaultMaxBrutalMbps, false
	if cfg != nil && cfg.MaxBrutalMbps != nil {
		limit, explicit = *cfg.MaxBrutalMbps, true
		if limit < 0 {
			limit = 0
		}
	}
	switch {
	case limit == 0:
		// unlimited by configuration
	case bw.UpMbps == 0:
		bw.UpMbps = limit
		bw.Reason = "panel up_mbps is 0 (unlimited)"
	case explicit && bw.UpMbps > limit:
		bw.UpMbps = limit
		bw.Reason = "MaxBrutalMbps is lower than the panel up_mbps"
	}
	return bw
}
