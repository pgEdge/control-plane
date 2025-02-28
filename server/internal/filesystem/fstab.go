package filesystem

import (
	"bufio"
	"container/list"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/afero"
)

const (
	MountOptDefaults string = "defaults"
	MountOptNoAuto   string = "noauto"
	MountOptUser     string = "user"
	MountOptOwner    string = "owner"
	MountOptLoop     string = "loop"
	MountOptComment  string = "comment"
	MountOptNoFail   string = "nofail"
)

type FSTabEntry struct {
	// Spec is the fs_spec field: "This field describes the block special
	// device, remote filesystem or filesystem image for loop device to be
	// mounted or swap file or swap partition to be enabled."
	Spec string
	// File is the fs_file field: "This field describes the mount point (target)
	// for the filesystem.""
	File string
	// VFSType is the fs_vsftype field: "This field describes the type of the
	// filesystem."
	VFSType string
	// MountOps is the fs_mntops field: "This field describes the mount options
	// associated with the filesystem.""
	MountOps []string
	// Freq is the fs_freq field: "This field is used by dump(8) to determine
	// which filesystems need to be dumped."
	Freq int
	// PassNo is the fs_passno field: "This field is used by fsck(8) to
	// determine the order in which filesystem checks are done at boot time."
	PassNo int
}

func (e *FSTabEntry) String() string {
	return strings.Join([]string{
		e.Spec,
		e.File,
		e.VFSType,
		strings.Join(e.MountOps, ","),
		strconv.Itoa(e.Freq),
		strconv.Itoa(e.PassNo),
	}, "\t")
}

type FileWriter func(contents string) error

type FSTabManagerOptions struct {
	FS         afero.Fs
	FileWriter FileWriter
}

type FSTabManager interface {
	// Add adds the given entry to the fstab.
	Add(entry *FSTabEntry) error
	// Filter takes a callback function that is invoked on each entry. The
	// callback should return 'true' to keep the element, or 'false' to remove
	// the element.
	Filter(f func(FSTabEntry) bool) error
}

type fsTabManager struct {
	fstab *fsTab
	write FileWriter
	mu    sync.Mutex
}

func SudoWriter(contents string) error {
	cmd := exec.Command("sudo", "sh", "-c", "cat > /etc/fstab")
	cmd.Stdin = strings.NewReader(contents)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}

func NewFSTabManager(opts FSTabManagerOptions) (FSTabManager, error) {
	fs := opts.FS
	if fs == nil {
		fs = afero.NewOsFs()
	}
	write := opts.FileWriter
	if write == nil {
		write = SudoWriter
	}

	file, err := fs.Open("/etc/fstab")
	if err != nil {
		return nil, fmt.Errorf("failed to open fstab: %w", err)
	}
	defer file.Close()

	fstab, err := parseFSTab(file)
	if err != nil {
		return nil, err
	}

	return &fsTabManager{
		fstab: fstab,
		write: write,
	}, nil
}

func (m *fsTabManager) Filter(f func(FSTabEntry) bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.fstab.filter(f)
	return m.write(m.fstab.String())
}

func (m *fsTabManager) Add(entry *FSTabEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.fstab.add(entry)
	return m.write(m.fstab.String())
}

type fsTabLine struct {
	element *list.Element
	raw     string
	entry   *FSTabEntry
}

func (l *fsTabLine) String() string {
	if l.entry != nil {
		return l.entry.String()
	}
	return l.raw
}

func lineForEntry(entry *FSTabEntry) *fsTabLine {
	return &fsTabLine{
		raw:   entry.String(),
		entry: entry,
	}
}

func parseFSTabLine(line string) *fsTabLine {
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "#") {
		return &fsTabLine{raw: line}
	}

	fields := strings.Fields(line)
	if len(fields) != 6 {
		return &fsTabLine{raw: line}
	}

	freq, err := strconv.Atoi(fields[4])
	if err != nil {
		// Fallback to leaving the line untouched
		return &fsTabLine{raw: line}
	}
	passNo, err := strconv.Atoi(fields[5])
	if err != nil {
		return &fsTabLine{raw: line}
	}

	return &fsTabLine{
		raw: line,
		entry: &FSTabEntry{
			Spec:     fields[0],
			File:     fields[1],
			VFSType:  fields[2],
			MountOps: strings.Split(fields[3], ","),
			Freq:     freq,
			PassNo:   passNo,
		},
	}
}

type fsTab struct {
	lines              *list.List
	entryLinesByTarget map[string]*fsTabLine
}

func (t *fsTab) filter(f func(FSTabEntry) bool) {
	for target, line := range t.entryLinesByTarget {
		if !f(*line.entry) {
			t.lines.Remove(line.element)
			delete(t.entryLinesByTarget, target)
		}
	}
}

func (t *fsTab) add(entry *FSTabEntry) {
	line, ok := t.entryLinesByTarget[entry.File]
	if !ok {
		line = lineForEntry(entry)
		line.element = t.lines.PushBack(line)
		t.entryLinesByTarget[entry.File] = line
	} else {
		line.entry = entry
		line.raw = entry.String()
	}
}

func (t *fsTab) String() string {
	var lines []string

	previousLineBlank := false
	element := t.lines.Front()
	for element != nil {
		if v := element.Value; v != nil {
			if line, ok := v.(*fsTabLine); ok {
				s := line.String()
				// Prevent consecutive blank lines for tidier output
				if s != "" {
					previousLineBlank = false
					lines = append(lines, line.String())
				} else if !previousLineBlank {
					previousLineBlank = true
					lines = append(lines, line.String())
				}
			}
		}
		element = element.Next()
	}

	output := strings.Join(lines, "\n")
	// Ensure that we end with a newline
	if !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	return output
}

func parseFSTab(r io.Reader) (*fsTab, error) {
	lines := list.New()
	entryLinesByTarget := map[string]*fsTabLine{}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := parseFSTabLine(scanner.Text())
		if line.entry != nil {
			// Deduplicate entries by replacing the previous occurrence with the
			// most recent occurrence.
			if existing, ok := entryLinesByTarget[line.entry.File]; ok {
				line.element = lines.InsertBefore(line, existing.element)
				lines.Remove(existing.element)
			} else {
				line.element = lines.PushBack(line)
			}
			entryLinesByTarget[line.entry.File] = line
		} else {
			line.element = lines.PushBack(line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error while reading fstab: %w", err)
	}

	return &fsTab{
		lines:              lines,
		entryLinesByTarget: entryLinesByTarget,
	}, nil
}
