package filesystem

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MockFileSystem is a mock implementation of FileSystemInterface for testing.
// Modeled after linuxptp-daemon's addons/intel/mock_test.go MockFileSystem.
type MockFileSystem struct {
	statCalls     []StatCall
	readDirCalls  []ReadDirCall
	readFileCalls []ReadFileCall

	currentStat     int
	currentReadDir  int
	currentReadFile int

	allowedStat     map[string]StatCall
	allowedReadDir  map[string]ReadDirCall
	allowedReadFile map[string]ReadFileCall
}

// SetupMockFS swaps the global filesystem implementation with a mock and returns a cleanup function.
func SetupMockFS() (*MockFileSystem, func()) {
	original := impl
	mock := &MockFileSystem{}
	impl = mock
	return mock, func() { impl = original }
}

// StatCall represents an expected or allowed Stat call
type StatCall struct {
	ExpectedPath string
	ReturnInfo   os.FileInfo
	ReturnError  error
}

// ReadDirCall represents an expected or allowed ReadDir call
type ReadDirCall struct {
	ExpectedPath string
	ReturnDirs   []os.DirEntry
	ReturnError  error
}

// ReadFileCall represents an expected or allowed ReadFile call
type ReadFileCall struct {
	ExpectedPath string
	ReturnData   []byte
	ReturnError  error
}

func (m *MockFileSystem) ExpectStat(path string, info os.FileInfo, err error) {
	m.statCalls = append(m.statCalls, StatCall{
		ExpectedPath: path,
		ReturnInfo:   info,
		ReturnError:  err,
	})
}

func (m *MockFileSystem) AllowStat(path string, info os.FileInfo, err error) {
	if m.allowedStat == nil {
		m.allowedStat = make(map[string]StatCall)
	}
	m.allowedStat[path] = StatCall{
		ExpectedPath: path,
		ReturnInfo:   info,
		ReturnError:  err,
	}
}

func (m *MockFileSystem) ExpectReadDir(path string, dirs []os.DirEntry, err error) {
	m.readDirCalls = append(m.readDirCalls, ReadDirCall{
		ExpectedPath: path,
		ReturnDirs:   dirs,
		ReturnError:  err,
	})
}

func (m *MockFileSystem) AllowReadDir(path string, dirs []os.DirEntry, err error) {
	if m.allowedReadDir == nil {
		m.allowedReadDir = make(map[string]ReadDirCall)
	}
	m.allowedReadDir[path] = ReadDirCall{
		ExpectedPath: path,
		ReturnDirs:   dirs,
		ReturnError:  err,
	}
}

func (m *MockFileSystem) ExpectReadFile(path string, data []byte, err error) {
	m.readFileCalls = append(m.readFileCalls, ReadFileCall{
		ExpectedPath: path,
		ReturnData:   data,
		ReturnError:  err,
	})
}

func (m *MockFileSystem) AllowReadFile(path string, data []byte, err error) {
	if m.allowedReadFile == nil {
		m.allowedReadFile = make(map[string]ReadFileCall)
	}
	m.allowedReadFile[path] = ReadFileCall{
		ExpectedPath: path,
		ReturnData:   data,
		ReturnError:  err,
	}
}

func (m *MockFileSystem) Stat(name string) (os.FileInfo, error) {
	if allowed, ok := m.allowedStat[name]; ok {
		return allowed.ReturnInfo, allowed.ReturnError
	}
	if m.currentStat >= len(m.statCalls) {
		return nil, fmt.Errorf("unexpected Stat call (%s)", name)
	}
	call := m.statCalls[m.currentStat]
	m.currentStat++
	if call.ExpectedPath != "" && call.ExpectedPath != name {
		return nil, fmt.Errorf("Stat called with unexpected path (%s), was expecting %s", name, call.ExpectedPath)
	}
	return call.ReturnInfo, call.ReturnError
}

func (m *MockFileSystem) ReadDir(dirname string) ([]os.DirEntry, error) {
	if allowed, ok := m.allowedReadDir[dirname]; ok {
		return allowed.ReturnDirs, allowed.ReturnError
	}
	if m.currentReadDir >= len(m.readDirCalls) {
		return nil, fmt.Errorf("unexpected ReadDir call (%s)", dirname)
	}
	call := m.readDirCalls[m.currentReadDir]
	m.currentReadDir++
	if call.ExpectedPath != "" && call.ExpectedPath != dirname {
		return nil, fmt.Errorf("ReadDir called with unexpected path (%s), was expecting %s", dirname, call.ExpectedPath)
	}
	return call.ReturnDirs, call.ReturnError
}

func (m *MockFileSystem) ReadFile(filename string) ([]byte, error) {
	if allowed, ok := m.allowedReadFile[filename]; ok {
		return allowed.ReturnData, allowed.ReturnError
	}
	if m.currentReadFile >= len(m.readFileCalls) {
		return nil, fmt.Errorf("unexpected ReadFile call (%s)", filename)
	}
	call := m.readFileCalls[m.currentReadFile]
	m.currentReadFile++
	if call.ExpectedPath != "" && call.ExpectedPath != filename {
		return nil, fmt.Errorf("ReadFile called with unexpected filename (%s), was expecting %s", filename, call.ExpectedPath)
	}
	return call.ReturnData, call.ReturnError
}

// VerifyAllCalls asserts that all expected calls were made
func (m *MockFileSystem) VerifyAllCalls(t *testing.T) {
	assert.Equal(t, len(m.statCalls), m.currentStat, "Not all expected Stat calls were made")
	assert.Equal(t, len(m.readDirCalls), m.currentReadDir, "Not all expected ReadDir calls were made")
	assert.Equal(t, len(m.readFileCalls), m.currentReadFile, "Not all expected ReadFile calls were made")
}

// MockDirEntry implements os.DirEntry for testing
type MockDirEntry struct {
	EntryName string
	Dir       bool
}

func (m MockDirEntry) Name() string               { return m.EntryName }
func (m MockDirEntry) IsDir() bool                { return m.Dir }
func (m MockDirEntry) Type() os.FileMode          { return 0 }
func (m MockDirEntry) Info() (os.FileInfo, error) { return nil, nil }
