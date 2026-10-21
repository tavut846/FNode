package cmd

import (
	log "github.com/sirupsen/logrus"

	"github.com/tavut846/FNode/api/panel"
	_ "github.com/tavut846/FNode/core/imports"
	"github.com/spf13/cobra"
)

var command = &cobra.Command{
	Use: "FNode",
}

func init() {
	panel.Version = version
}

func Run() {
	err := command.Execute()
	if err != nil {
		log.WithField("err", err).Error("Execute command failed")
	}
}
