//go:build !linux

package agent

import "fmt"

func secureDockerRead(path string) ([]byte, error) {
	return nil, fmt.Errorf("secure file reads require Linux")
}
