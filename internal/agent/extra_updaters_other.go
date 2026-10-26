//go:build !linux

package agent

import (
	"context"
	"errors"
)

var errExtraUnsupported = errors.New("extra updaters are supported on Linux only")

func extraCapabilities(context.Context) ([]byte, error) { return nil, errExtraUnsupported }
func extraRootPolicy() ([]byte, error)                  { return nil, errExtraUnsupported }
func extraSignature() string                            { return "" }
