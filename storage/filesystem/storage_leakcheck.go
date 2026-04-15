//go:build leakcheck

package filesystem

import (
	"fmt"
	"runtime"
	"strings"
)

// setupLeakCheck sets up leak detection for a storage
func setupLeakCheck(s *Storage) {
	// Capture stack trace at storage creation time
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	stack := string(buf[:n])

	runtime.SetFinalizer(s, func(storage *Storage) {
		if !storage.closed {
			// Extract just the relevant part of the stack (skip runtime internals)
			// Stack trace format: goroutine header\nfunction\n\tfile:line\nfunction\n\tfile:line...
			lines := strings.Split(stack, "\n")
			relevantStack := []string{}
			// Skip line 0 (goroutine header), then process pairs starting from line 1
			for i := 1; i < len(lines)-1; i += 2 {
				funcLine := lines[i]
				fileLine := ""
				if i+1 < len(lines) {
					fileLine = lines[i+1]
				}
				// Check if this stack frame is relevant (contains go-git or testing)
				if strings.Contains(funcLine, "go-git") || strings.Contains(fileLine, "go-git") ||
					strings.Contains(funcLine, "testing.") {
					relevantStack = append(relevantStack, funcLine)
					if fileLine != "" {
						relevantStack = append(relevantStack, fileLine)
					}
				}
			}

			panic(fmt.Sprintf("\n\n=== STORAGE LEAK DETECTED ===\n"+
				"Storage was garbage collected without Close() being called!\n"+
				"This will cause file handle leaks on Windows.\n"+
				"Always call defer func() { _ = storage.Close() }() after creating a storage.\n\n"+
				"Storage was created at:\n%s\n"+
				"=====================\n\n", strings.Join(relevantStack, "\n")))
		}
	})
}
