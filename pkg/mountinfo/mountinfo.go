// Copyright 2018 Authors of Cilium
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mountinfo

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	// FilesystemType superblock magic numbers for filesystems,
	// to be used for IsMountFS.
	FilesystemTypeBPFFS   = unix.BPF_FS_MAGIC
	FilesystemTypeCgroup2 = unix.CGROUP2_SUPER_MAGIC

	mountInfoFilepath = "/proc/self/mountinfo"
)

// MountInfo is a struct representing information from /proc/pid/mountinfo. More
// information about file syntax:
// https://www.kernel.org/doc/Documentation/filesystems/proc.txt
type MountInfo struct {
	MountID        int64
	ParentID       int64
	StDev          string
	Root           string
	MountPoint     string
	MountOptions   string
	OptionalFields []string
	FilesystemType string
	MountSource    string
	SuperOptions   string
}

// parseMountInfoFile returns a slice of *MountInfo with information parsed from
// the given reader
func parseMountInfoFile(r io.Reader) ([]*MountInfo, error) {
	var result []*MountInfo

	scanner := bufio.NewScanner(r)
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		mountInfoRaw := scanner.Text()

		// Optional fields (which are on the 7th position) are separated
		// from the rest of fields by "-" character. The number of
		// optional fields can be greater or equal to 1.
		mountInfoSeparated := strings.Split(mountInfoRaw, " - ")
		if len(mountInfoSeparated) != 2 {
			return nil, fmt.Errorf("invalid mountinfo entry which has more that one '-' separator: %s", mountInfoRaw)
		}

		// Extract fields from both sides of mountinfo
		mountInfoLeft := strings.Split(mountInfoSeparated[0], " ")
		mountInfoRight := strings.Split(mountInfoSeparated[1], " ")

		// Before '-' separator there should be 6 fields and unknown
		// number of optional fields
		if len(mountInfoLeft) < 6 {
			return nil, fmt.Errorf("invalid mountinfo entry: %s", mountInfoRaw)
		}
		// After '-' separator there should be 3 fields
		if len(mountInfoRight) != 3 {
			return nil, fmt.Errorf("invalid mountinfo entry: %s", mountInfoRaw)
		}

		mountID, err := strconv.ParseInt(mountInfoLeft[0], 10, 64)
		if err != nil {
			return nil, err
		}

		parentID, err := strconv.ParseInt(mountInfoLeft[1], 10, 64)
		if err != nil {
			return nil, err
		}

		// Extract optional fields, which start from 7th position
		var optionalFields []string
		for i := 6; i < len(mountInfoLeft); i++ {
			optionalFields = append(optionalFields, mountInfoLeft[i])
		}

		result = append(result, &MountInfo{
			MountID:        mountID,
			ParentID:       parentID,
			StDev:          mountInfoLeft[2],
			Root:           mountInfoLeft[3],
			MountPoint:     mountInfoLeft[4],
			MountOptions:   mountInfoLeft[5],
			OptionalFields: optionalFields,
			FilesystemType: mountInfoRight[0],
			MountSource:    mountInfoRight[1],
			SuperOptions:   mountInfoRight[2],
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// GetMountInfo returns a slice of *MountInfo with information parsed from
// /proc/self/mountinfo
func GetMountInfo() ([]*MountInfo, error) {
	fMounts, err := os.Open(mountInfoFilepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open mount information at %s: %s", mountInfoFilepath, err)
	}
	defer fMounts.Close()

	return parseMountInfoFile(fMounts)
}

// IsMountFS returns two boolean values, checking
//  - whether the path is a mount point;
//  - if yes, whether its filesystem type is mntType.
//
// Note that this function can not detect bind mounts,
// and is not working properly when path="/".
func IsMountFS(mntType int64, path string) (bool, bool, error) {
	var st, pst unix.Stat_t

	err := unix.Lstat(path, &st)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			// non-existent path can't be a mount point
			return false, false, nil
		}
		return false, false, &os.PathError{Op: "lstat", Path: path, Err: err}
	}

	parent := filepath.Dir(path)
	err = unix.Lstat(parent, &pst)
	if err != nil {
		return false, false, &os.PathError{Op: "lstat", Path: parent, Err: err}
	}
	if st.Dev == pst.Dev {
		// parent has the same dev -- not a mount point
		return false, false, nil
	}

	// Check the fstype
	fst := unix.Statfs_t{}
	err = unix.Statfs(path, &fst)
	if err != nil {
		return true, false, &os.PathError{Op: "statfs", Path: path, Err: err}
	}

	return true, fst.Type == mntType, nil

}
