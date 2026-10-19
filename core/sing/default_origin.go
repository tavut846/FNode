package sing

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
	logrus "github.com/sirupsen/logrus"
)

// DefaultOrigin is FNode's default sing_origin.json: direct (prefer IPv4),
// direct6 (prefer IPv6) and block outbounds, plus the default block rules
// (private IPs, BitTorrent, SMTP ports, abuse domains) ending in direct.
// install.sh / initconfig.sh write the same file to /etc/FNode/sing_origin.json.
//
//go:embed default_origin.json
var DefaultOrigin []byte

// loadOrigin reads the sing_origin.json at path. An empty path, a missing file
// or an invalid file falls back to DefaultOrigin (logged), so a bad or deleted
// origin file never keeps the node offline.
func loadOrigin(ctx context.Context, path string) (option.Options, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			options, perr := json.UnmarshalExtendedContext[option.Options](ctx, data)
			if perr == nil {
				return options, nil
			}
			logrus.WithFields(logrus.Fields{"path": path, "err": perr}).
				Error("sing_origin.json is invalid, using the FNode default outbounds and routes until it is fixed")
		case errors.Is(err, fs.ErrNotExist):
			logrus.WithField("path", path).Warn("sing_origin.json not found, using the FNode default outbounds and routes")
		default:
			logrus.WithFields(logrus.Fields{"path": path, "err": err}).
				Error("Read sing_origin.json failed, using the FNode default outbounds and routes")
		}
	}
	options, err := json.UnmarshalExtendedContext[option.Options](ctx, DefaultOrigin)
	if err != nil {
		return option.Options{}, fmt.Errorf("unmarshal default origin error: %s", err)
	}
	return options, nil
}
