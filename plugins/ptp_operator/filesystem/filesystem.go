package filesystem

import (
	"os"
)

// FileSystemInterface defines the interface for filesystem operations to enable mocking.
// Modeled after linuxptp-daemon's addons/intel/common.go FileSystemInterface.
type FileSystemInterface interface {
	Stat(name string) (os.FileInfo, error)
	ReadDir(dirname string) ([]os.DirEntry, error)
	ReadFile(filename string) ([]byte, error)
}

// RealFileSystem implements FileSystemInterface using real OS operations
type RealFileSystem struct{}

func (fs *RealFileSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (fs *RealFileSystem) ReadDir(dirname string) ([]os.DirEntry, error) {
	return os.ReadDir(dirname)
}

func (fs *RealFileSystem) ReadFile(filename string) ([]byte, error) {
	return os.ReadFile(filename)
}

var impl FileSystemInterface = &RealFileSystem{}

func Stat(name string) (os.FileInfo, error)         { return impl.Stat(name) }
func ReadDir(dirname string) ([]os.DirEntry, error) { return impl.ReadDir(dirname) }
func ReadFile(filename string) ([]byte, error)      { return impl.ReadFile(filename) }
