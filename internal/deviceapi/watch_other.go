//go:build !linux

package deviceapi

import "os"

func startWatch(string, func(bool, bool)) (func(), os.FileInfo, error) { return nil, nil, ErrWatch }
