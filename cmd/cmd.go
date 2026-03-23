package cmd

import (
	log "github.com/sirupsen/logrus"

	_ "github.com/InazumaV/FNode/core/imports"
	"github.com/spf13/cobra"
)

var command = &cobra.Command{
	Use: "FNode",
}

func Run() {
	err := command.Execute()
	if err != nil {
		log.WithField("err", err).Error("Execute command failed")
	}
}
