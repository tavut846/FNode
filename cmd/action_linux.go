package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/tavut846/FNode/common/exec"
	"github.com/spf13/cobra"
)

var (
	startCommand = cobra.Command{
		Use:   "start",
		Short: "Start FNode service",
		Run:   startHandle,
	}
	stopCommand = cobra.Command{
		Use:   "stop",
		Short: "Stop FNode service",
		Run:   stopHandle,
	}
	restartCommand = cobra.Command{
		Use:   "restart",
		Short: "Restart FNode service",
		Run:   restartHandle,
	}
	logCommand = cobra.Command{
		Use:   "log",
		Short: "Output FNode log",
		Run: func(_ *cobra.Command, _ []string) {
			exec.RunCommandStd("journalctl", "-u", "FNode.service", "-e", "--no-pager", "-f")
		},
	}
	cleanLogCommand = cobra.Command{
		Use:     "clearlog",
		Aliases: []string{"cleanlog", "clean-log"},
		Short:   "Clean up FNode logs",
		Run:     cleanLogHandle,
	}
)

func init() {
	command.AddCommand(&startCommand)
	command.AddCommand(&stopCommand)
	command.AddCommand(&restartCommand)
	command.AddCommand(&logCommand)
	command.AddCommand(&cleanLogCommand)
}

func startHandle(_ *cobra.Command, _ []string) {
	r, err := checkRunning()
	if err != nil {
		fmt.Println(Err("check status error: ", err))
		fmt.Println(Err("FNode启动失败"))
		return
	}
	if r {
		fmt.Println(Ok("FNode已运行，无需再次启动，如需重启请选择重启"))
	}
	_, err = exec.RunCommandByShell("systemctl start FNode.service")
	if err != nil {
		fmt.Println(Err("exec start cmd error: ", err))
		fmt.Println(Err("FNode启动失败"))
		return
	}
	time.Sleep(time.Second * 3)
	r, err = checkRunning()
	if err != nil {
		fmt.Println(Err("check status error: ", err))
		fmt.Println(Err("FNode启动失败"))
	}
	if !r {
		fmt.Println(Err("FNode可能启动失败，请稍后使用 FNode log 查看日志信息"))
		return
	}
	fmt.Println(Ok("FNode 启动成功，请使用 FNode log 查看运行日志"))
}

func stopHandle(_ *cobra.Command, _ []string) {
	_, err := exec.RunCommandByShell("systemctl stop FNode.service")
	if err != nil {
		fmt.Println(Err("exec stop cmd error: ", err))
		fmt.Println(Err("FNode停止失败"))
		return
	}
	time.Sleep(2 * time.Second)
	r, err := checkRunning()
	if err != nil {
		fmt.Println(Err("check status error:", err))
		fmt.Println(Err("FNode停止失败"))
		return
	}
	if r {
		fmt.Println(Err("FNode停止失败，可能是因为停止时间超过了两秒，请稍后查看日志信息"))
		return
	}
	fmt.Println(Ok("FNode 停止成功"))
}

func restartHandle(_ *cobra.Command, _ []string) {
	_, err := exec.RunCommandByShell("systemctl restart FNode.service")
	if err != nil {
		fmt.Println(Err("exec restart cmd error: ", err))
		fmt.Println(Err("FNode重启失败"))
		return
	}
	r, err := checkRunning()
	if err != nil {
		fmt.Println(Err("check status error: ", err))
		fmt.Println(Err("FNode重启失败"))
		return
	}
	if !r {
		fmt.Println(Err("FNode可能启动失败，请稍后使用 FNode log 查看日志信息"))
		return
	}
	fmt.Println(Ok("FNode重启成功"))
}

func cleanLogHandle(_ *cobra.Command, _ []string) {
	fmt.Println("Cleaning up FNode logs...")
	_, _ = exec.RunCommandByShell("journalctl --rotate && journalctl --vacuum-time=1s --unit=FNode.service")
	_, _ = exec.RunCommandByShell("journalctl --vacuum-size=20M")

	commonLogs := []string{
		"/var/log/FNode.log",
		"/var/log/fnode.log",
		"/var/log/fnode.error.log",
		"/usr/local/FNode/box.log",
		"/usr/local/FNode/fnode.log",
		"/etc/FNode/box.log",
		"/etc/FNode/fnode.log",
	}

	for _, p := range commonLogs {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			if err := os.Truncate(p, 0); err == nil {
				fmt.Printf("Truncated log file: %s\n", p)
			}
		}
	}
	fmt.Println(Ok("FNode logs cleaned successfully"))
}
