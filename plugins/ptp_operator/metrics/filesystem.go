package metrics

import (
	"os"
)

type WFiles interface {
	WriteFile(name string, data []byte, perm os.FileMode) error
}

type OSFileSystem struct {
}

func (f OSFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

type MockFileSystem struct {
	WriteCount int
}

func (m *MockFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	m.WriteCount += 1
	return nil
}

func (m *MockFileSystem) Clear() {
	m.WriteCount = 0
}

var Filesystem WFiles = OSFileSystem{}
