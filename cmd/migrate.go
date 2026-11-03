package cmd

import (
	"errors"

	log "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/common/migrate"
	"github.com/tavut846/FNode/common/selfupdate"
	"github.com/tavut846/FNode/core/sing"
)

// migrateConfig upgrades the config files to what this build's sing-box core
// accepts (once: SchemaVersion in config.json), before the config is loaded.
// It returns false when this process must stop: the freshly updated binary
// failed its check and was rolled back, so the service is restarting into the
// previous binary and the config backup restored by migrate.Run.
func migrateConfig(configPath string) bool {
	selfupdate.ReportRollback()
	res, err := migrate.Run(configPath, sing.CheckConfig)
	switch {
	case err == nil:
		if res.Migrated {
			log.Infof("Config migrated to schema %d: %d change(s), backups %v", migrate.SchemaVersion, len(res.Changes), res.Backups)
		}
		selfupdate.Confirm()
		return true
	case errors.Is(err, migrate.ErrRolledBack) && selfupdate.Pending():
		reason := "config migration failed: " + err.Error()
		log.Error(reason)
		if rerr := selfupdate.Rollback(reason); rerr != nil {
			log.Errorf("Rollback to the previous FNode binary failed, staying on this one: %v", rerr)
			selfupdate.Confirm()
			return true
		}
		selfupdate.Restart()
		return false
	default:
		// not an update, or the config could not even be read: keep running with
		// the files as they are (migrate.Run restored them); FNode falls back to
		// its default origin for a file sing-box rejects
		log.Errorf("Config migration skipped: %v", err)
		selfupdate.Confirm()
		return true
	}
}
