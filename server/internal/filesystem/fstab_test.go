package filesystem

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFSTabLine(t *testing.T) {
	for _, tc := range []struct {
		input    string
		expected *fsTabLine
	}{
		{
			input:    "",
			expected: &fsTabLine{},
		},
		{
			input:    "#",
			expected: &fsTabLine{raw: "#"},
		},
		{
			input:    "         # foo",
			expected: &fsTabLine{raw: "# foo"},
		},
		{
			input:    "UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a /                       xfs     defaults        0 ",
			expected: &fsTabLine{raw: "UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a /                       xfs     defaults        0"},
		},
		{
			input: "UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a /                       xfs     defaults        0 0",
			expected: &fsTabLine{
				raw: "UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a /                       xfs     defaults        0 0",
				entry: &FSTabEntry{
					Spec:     "UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a",
					File:     "/",
					VFSType:  "xfs",
					MountOps: []string{"defaults"},
					Freq:     0,
					PassNo:   0,
				},
			},
		},
		{
			input: "UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a\t/\txfs\tdefaults\t0\t0",
			expected: &fsTabLine{
				raw: "UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a\t/\txfs\tdefaults\t0\t0",
				entry: &FSTabEntry{
					Spec:     "UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a",
					File:     "/",
					VFSType:  "xfs",
					MountOps: []string{"defaults"},
					Freq:     0,
					PassNo:   0,
				},
			},
		},
		{
			input: "UUID=92FE-8E69          /boot/efi               vfat    defaults,uid=0,gid=0,umask=077,shortname=winnt 0 2",
			expected: &fsTabLine{
				raw: "UUID=92FE-8E69          /boot/efi               vfat    defaults,uid=0,gid=0,umask=077,shortname=winnt 0 2",
				entry: &FSTabEntry{
					Spec:     "UUID=92FE-8E69",
					File:     "/boot/efi",
					VFSType:  "vfat",
					MountOps: []string{"defaults", "uid=0", "gid=0", "umask=077", "shortname=winnt"},
					Freq:     0,
					PassNo:   2,
				},
			},
		},
		{
			input: "UUID=9fd1089e-d67a-4fd0-a6f0-20f4f3561619 /data ext4 discard,defaults,nofail 0 2",
			expected: &fsTabLine{
				raw: "UUID=9fd1089e-d67a-4fd0-a6f0-20f4f3561619 /data ext4 discard,defaults,nofail 0 2",
				entry: &FSTabEntry{
					Spec:     "UUID=9fd1089e-d67a-4fd0-a6f0-20f4f3561619",
					File:     "/data",
					VFSType:  "ext4",
					MountOps: []string{"discard", "defaults", "nofail"},
					Freq:     0,
					PassNo:   2,
				},
			},
		},
		{
			input: "/dev/loop0 /data/filesystems/074fe3ef-1e02-49d5-9b51-2b7c5c6b7d61 ext4 loop 0 0",
			expected: &fsTabLine{
				raw: "/dev/loop0 /data/filesystems/074fe3ef-1e02-49d5-9b51-2b7c5c6b7d61 ext4 loop 0 0",
				entry: &FSTabEntry{
					Spec:     "/dev/loop0",
					File:     "/data/filesystems/074fe3ef-1e02-49d5-9b51-2b7c5c6b7d61",
					VFSType:  "ext4",
					MountOps: []string{"loop"},
					Freq:     0,
					PassNo:   0,
				},
			},
		},
		{
			input: "/data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26.fs /data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26 ext4 loop,nofail 0 0",
			expected: &fsTabLine{
				raw: "/data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26.fs /data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26 ext4 loop,nofail 0 0",
				entry: &FSTabEntry{
					Spec:     "/data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26.fs",
					File:     "/data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26",
					VFSType:  "ext4",
					MountOps: []string{"loop", "nofail"},
					Freq:     0,
					PassNo:   0,
				},
			},
		},
	} {
		t.Run(tc.input, func(t *testing.T) {
			output := parseFSTabLine(tc.input)
			assert.Equal(t, tc.expected, output)
		})
	}
}

func TestFSTabEntry(t *testing.T) {
	t.Run("String()", func(t *testing.T) {
		for _, tc := range []struct {
			input    *FSTabEntry
			expected string
		}{
			{
				expected: "UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a\t/\txfs\tdefaults\t0\t0",
				input: &FSTabEntry{
					Spec:     "UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a",
					File:     "/",
					VFSType:  "xfs",
					MountOps: []string{"defaults"},
					Freq:     0,
					PassNo:   0,
				},
			},
			{
				expected: "UUID=92FE-8E69\t/boot/efi\tvfat\tdefaults,uid=0,gid=0,umask=077,shortname=winnt\t0\t2",
				input: &FSTabEntry{
					Spec:     "UUID=92FE-8E69",
					File:     "/boot/efi",
					VFSType:  "vfat",
					MountOps: []string{"defaults", "uid=0", "gid=0", "umask=077", "shortname=winnt"},
					Freq:     0,
					PassNo:   2,
				},
			},
			{
				expected: "UUID=9fd1089e-d67a-4fd0-a6f0-20f4f3561619\t/data\text4\tdiscard,defaults,nofail\t0\t2",
				input: &FSTabEntry{
					Spec:     "UUID=9fd1089e-d67a-4fd0-a6f0-20f4f3561619",
					File:     "/data",
					VFSType:  "ext4",
					MountOps: []string{"discard", "defaults", "nofail"},
					Freq:     0,
					PassNo:   2,
				},
			},
			{
				expected: "/dev/loop0\t/data/filesystems/074fe3ef-1e02-49d5-9b51-2b7c5c6b7d61\text4\tloop\t0\t0",
				input: &FSTabEntry{
					Spec:     "/dev/loop0",
					File:     "/data/filesystems/074fe3ef-1e02-49d5-9b51-2b7c5c6b7d61",
					VFSType:  "ext4",
					MountOps: []string{"loop"},
					Freq:     0,
					PassNo:   0,
				},
			},
			{
				expected: "/data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26.fs\t/data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26\text4\tloop,nofail\t0\t0",
				input: &FSTabEntry{
					Spec:     "/data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26.fs",
					File:     "/data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26",
					VFSType:  "ext4",
					MountOps: []string{"loop", "nofail"},
					Freq:     0,
					PassNo:   0,
				},
			},
		} {
			t.Run(tc.expected, func(t *testing.T) {
				output := tc.input.String()
				assert.Equal(t, tc.expected, output)
			})
		}
	})
}

var testFSTabContents = `
#
# /etc/fstab
# Created by anaconda on Wed Nov 15 22:41:55 2023
#
# Accessible filesystems, by reference, are maintained under '/dev/disk/'.
# See man pages fstab(5), findfs(8), mount(8) and/or blkid(8) for more info.
#
# After editing this file, run 'systemctl daemon-reload' to update systemd
# units generated from this file.
#
UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a /                       xfs     defaults        0 0
UUID=92FE-8E69          /boot/efi               vfat    defaults,uid=0,gid=0,umask=077,shortname=winnt 0 2
UUID=9fd1089e-d67a-4fd0-a6f0-20f4f3561619 /data ext4 discard,defaults,nofail 0 2

/dev/loop0 /data/filesystems/074fe3ef-1e02-49d5-9b51-2b7c5c6b7d61 ext4 loop 0 0

/dev/loop1 /data/filesystems/f0ad72e8-0b49-4479-94c9-518f2f0a9e4a ext4 loop 0 0

/my/target/is/a/duplicate /data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26 ext4 loop,nofail 0 0
/data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26.fs /data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26 ext4 loop,nofail 0 0
`

func TestFSTab(t *testing.T) {
	f, err := parseFSTab(strings.NewReader(testFSTabContents))

	assert.NoError(t, err)
	assert.Equal(t, len(f.entryLinesByTarget), 6)

	// Remove /dev/loop entries
	f.filter(func(entry FSTabEntry) bool {
		return !strings.HasPrefix(entry.Spec, "/dev/")
	})
	// Add some new entries
	f.add(&FSTabEntry{
		Spec:     "/data/filesystems/aa031a26-48f5-464c-8243-fd0a9f967894.fs",
		File:     "/data/filesystems/aa031a26-48f5-464c-8243-fd0a9f967894",
		VFSType:  "ext4",
		MountOps: []string{"loop", "nofail"},
		Freq:     0,
		PassNo:   0,
	})
	f.add(&FSTabEntry{
		Spec:     "/data/filesystems/71bbce54-4451-466f-84d4-39bef5d79b81.fs",
		File:     "/data/filesystems/71bbce54-4451-466f-84d4-39bef5d79b81",
		VFSType:  "ext4",
		MountOps: []string{"loop", "nofail"},
		Freq:     0,
		PassNo:   0,
	})

	expected := `
#
# /etc/fstab
# Created by anaconda on Wed Nov 15 22:41:55 2023
#
# Accessible filesystems, by reference, are maintained under '/dev/disk/'.
# See man pages fstab(5), findfs(8), mount(8) and/or blkid(8) for more info.
#
# After editing this file, run 'systemctl daemon-reload' to update systemd
# units generated from this file.
#
UUID=27d94d3c-eb42-4f3d-820d-c6f7035c0c9a	/	xfs	defaults	0	0
UUID=92FE-8E69	/boot/efi	vfat	defaults,uid=0,gid=0,umask=077,shortname=winnt	0	2
UUID=9fd1089e-d67a-4fd0-a6f0-20f4f3561619	/data	ext4	discard,defaults,nofail	0	2

/data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26.fs	/data/filesystems/5fea9988-1b97-4f85-8eaf-884dca87fc26	ext4	loop,nofail	0	0
/data/filesystems/aa031a26-48f5-464c-8243-fd0a9f967894.fs	/data/filesystems/aa031a26-48f5-464c-8243-fd0a9f967894	ext4	loop,nofail	0	0
/data/filesystems/71bbce54-4451-466f-84d4-39bef5d79b81.fs	/data/filesystems/71bbce54-4451-466f-84d4-39bef5d79b81	ext4	loop,nofail	0	0
`
	output := f.String()
	assert.Equal(t, expected, output)
}
