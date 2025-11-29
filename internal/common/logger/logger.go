package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

const (
	// MaxLogLines 最大日志行数
	MaxLogLines = 5000
	// TrimLines 达到最大行数时，保留的行数
	TrimLines = 4000
)

// RotatingWriter 实现日志轮转的Writer
type RotatingWriter struct {
	file      *os.File
	filePath  string
	lineCount int
	mu        sync.Mutex
}

// NewRotatingWriter 创建一个新的轮转日志Writer
func NewRotatingWriter(filePath string) (*RotatingWriter, error) {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// 计算现有行数
	lineCount, err := countLines(filePath)
	if err != nil {
		log.Printf("Warning: failed to count existing lines: %v", err)
		lineCount = 0
	}

	return &RotatingWriter{
		file:      file,
		filePath:  filePath,
		lineCount: lineCount,
	}, nil
}

// Write 实现io.Writer接口
func (w *RotatingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 写入日志
	n, err = w.file.Write(p)
	if err != nil {
		return n, err
	}

	// 计算新增的行数
	newLines := countNewlines(p)
	w.lineCount += newLines

	// 检查是否需要轮转
	if w.lineCount >= MaxLogLines {
		if err := w.rotate(); err != nil {
			log.Printf("Warning: failed to rotate log: %v", err)
		}
	}

	return n, nil
}

// rotate 执行日志轮转
func (w *RotatingWriter) rotate() error {
	// 关闭当前文件
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("failed to close log file: %w", err)
	}

	// 读取文件内容
	content, err := os.ReadFile(w.filePath)
	if err != nil {
		return fmt.Errorf("failed to read log file: %w", err)
	}

	// 找到要保留的内容（最后TrimLines行）
	lines := splitLines(content)
	if len(lines) > TrimLines {
		lines = lines[len(lines)-TrimLines:]
	}

	// 重新打开文件（截断模式）
	file, err := os.OpenFile(w.filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to reopen log file: %w", err)
	}

	// 写入保留的内容
	for _, line := range lines {
		if _, err := file.Write(line); err != nil {
			file.Close()
			return fmt.Errorf("failed to write log file: %w", err)
		}
	}

	w.file = file
	w.lineCount = len(lines)

	log.Printf("Log rotated: kept %d lines (removed %d lines)", w.lineCount, len(splitLines(content))-w.lineCount)
	return nil
}

// Close 关闭日志文件
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// countLines 计算文件的行数
func countLines(filePath string) (int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer file.Close()

	buf := make([]byte, 32*1024)
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := file.Read(buf)
		if err != nil && err != io.EOF {
			return count, err
		}

		for i := 0; i < c; i++ {
			if buf[i] == lineSep[0] {
				count++
			}
		}

		if err == io.EOF {
			break
		}
	}

	return count, nil
}

// countNewlines 计算字节数组中的换行符数量
func countNewlines(data []byte) int {
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	return count
}

// splitLines 将字节数组按行分割
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0

	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			lines = append(lines, data[start:i+1])
			start = i + 1
		}
	}

	// 添加最后一行（如果有）
	if start < len(data) {
		lines = append(lines, data[start:])
	}

	return lines
}

// SetupLogger 设置日志输出到文件和标准输出
func SetupLogger(logFilePath string) (*RotatingWriter, error) {
	if logFilePath == "" {
		// 如果没有指定日志文件，只输出到标准输出
		log.SetOutput(os.Stdout)
		return nil, nil
	}

	// 创建轮转日志Writer
	rotatingWriter, err := NewRotatingWriter(logFilePath)
	if err != nil {
		return nil, err
	}

	// 同时输出到文件和标准输出
	multiWriter := io.MultiWriter(os.Stdout, rotatingWriter)
	log.SetOutput(multiWriter)

	return rotatingWriter, nil
}
