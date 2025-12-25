package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/55gY/subs-check/core"
	"github.com/55gY/subs-check/util"
)

var Version = "dev"
var CurrentCommit = "unknown"

func main() {
	// 初始化日志系统
	util.InitLogger(core.TempLog())

	application := core.New(fmt.Sprintf("%s-%s", Version, CurrentCommit))
	slog.Info(fmt.Sprintf("当前版本: %s-%s", Version, CurrentCommit))

	if err := application.Initialize(); err != nil {
		slog.Error(fmt.Sprintf("初始化失败: %v", err))
		os.Exit(1)
	}

	application.Run()
}
