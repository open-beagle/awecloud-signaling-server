package banner

import (
	"fmt"
	"strings"
)

// BuildInfo 构建信息
type BuildInfo struct {
	AppName   string
	Version   string
	GitCommit string
	BuildDate string
	GoVersion string
}

// Print 打印启动横幅
func Print(info BuildInfo) {
	width := 60

	// 顶部边框
	fmt.Println(strings.Repeat("=", width))

	// 应用名称（居中）
	printCentered(info.AppName, width)

	// 分隔线
	fmt.Println(strings.Repeat("-", width))

	// 版本信息（左对齐）
	printKeyValue("Version", info.Version, width)
	printKeyValue("Git Commit", info.GitCommit, width)
	printKeyValue("Build Date", info.BuildDate, width)
	printKeyValue("Go Version", info.GoVersion, width)

	// 底部边框
	fmt.Println(strings.Repeat("=", width))
	fmt.Println()
}

// printCentered 打印居中文本
func printCentered(text string, width int) {
	padding := (width - len(text)) / 2
	if padding < 0 {
		padding = 0
	}
	fmt.Printf("%s%s%s\n", strings.Repeat(" ", padding), text, strings.Repeat(" ", width-padding-len(text)))
}

// printKeyValue 打印键值对
func printKeyValue(key, value string, width int) {
	// 格式：  Key: Value
	line := fmt.Sprintf("  %s: %s", key, value)
	fmt.Println(line)
}
